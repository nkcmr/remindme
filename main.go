package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	// "syscall"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-zoo/bone"
)

var cbseq uint64

type cb interface {
	execute() error
}

type callback struct {
	l         sync.Mutex
	ID        string
	Deadline  time.Time
	RemoteURL string
	Done      bool
	MarkDone  func() error
}

func nextseq() string {
	return fmt.Sprintf("%v", atomic.AddUint64(&cbseq, 1))
}

func (c *callback) execute(ctx context.Context) error {
	log.Printf("info: executing callback (%s) (id:%s)", c.RemoteURL, c.ID)
	_, err := http.Post(c.RemoteURL, "text/plain", nil)
	if err != nil {
		return err
	}
	return nil
}

type storage interface {
	subscribe(context.Context) (chan *callback, error)
	save(*callback) error
}

// memStorage is an in-memory storage of callbacks. it is forgetful though if
// the service crashes, making it only good for testing
type memStorage struct {
	sync.RWMutex
	store map[string]*callback
	c     chan *callback
}

func (m *memStorage) subscribe(ctx context.Context) (chan *callback, error) {
	return m.c, nil // only works for a single subscriber
}

func (m *memStorage) save(c *callback) error {
	m.Lock()
	defer m.Unlock()
	c.ID = nextseq()
	c.MarkDone = func() error {
		m.Lock()
		defer m.Unlock()
		c.Done = true
		delete(m.store, c.ID)
		return nil
	}
	m.store[c.ID] = c
	m.c <- c
	return nil
}

type scheduler struct {
	s storage
}

func newScheduler(s storage, done chan struct{}) *scheduler {
	schd := &scheduler{s: s}
	go schd.run(done)
	return schd
}

func (s *scheduler) run(done chan struct{}) {
	var (
		cbs    chan *callback
		err    error
		ctx    context.Context
		cancel context.CancelFunc
	)
	ctx, cancel = context.WithCancel(context.Background())
	for cbs == nil {
		cbs, err = s.s.subscribe(ctx)
		if err != nil {
			log.Printf("error: error occured while trying to subscribe to callbacks: %s", err.Error())
			time.Sleep(time.Duration(5) * time.Second)
			continue
		}
	}
	log.Println("info: subscribed to new callbacks, looping...")
	for {
		select {
		case <-done:
			cancel()
			return
		case cb := <-cbs:
			s.schedule(ctx, cb)
		}
	}
}

func (s *scheduler) schedule(ctx context.Context, cb *callback) {
	go func() {
		wait := time.Duration(cb.Deadline.UnixNano() - time.Now().UnixNano())
		<-time.After(wait)
		err := cb.execute(ctx)
		if err != nil {
			log.Printf(`warning: error reported in execute: "%s", retrying...`, err.Error())
			s.schedule(ctx, cb)
			return
		}
		i := 0
		for i <= 5 {
			err := cb.MarkDone()
			if err == nil {
				return
			}
			log.Printf("error: %s", err.Error())
			time.Sleep(time.Duration(5) * time.Second)
		}
		log.Println("warning: giving up on trying to mark callback as done")
	}()
}

func sigDone() chan struct{} {
	c := make(chan struct{})
	go func() {
		defer close(c)
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt)
		sig := <-c
		log.Printf("info: SIGNAL(%s)", sig)
	}()
	return c
}

func main() {
	done := sigDone()
	var s storage = &memStorage{c: make(chan *callback, 10), store: make(map[string]*callback)}
	newScheduler(s, done)

	s.save(&callback{
		Deadline:  time.Now().Add(time.Duration(5) * time.Second),
		RemoteURL: "https://webhook.site/6d44ca6e-1e43-435f-b445-ee2aeb3587f3",
	})

	r := bone.New()
	r.Post("/callback", handleNewCallback(s))

	srv := http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	go func() {
		err := srv.ListenAndServe()
		if err != nil {
			log.Printf("error: %s", err.Error())
		}
	}()

	<-done
	srv.Shutdown(context.Background())
}

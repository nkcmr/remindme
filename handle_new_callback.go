package main

import (
	"encoding/json"
	"fmt"
	// "log"
	"net/http"
	"net/url"
	"time"
)

type createCallbackRequest struct {
	RemoteURL string `json:"remote_url"`
	Duration  string `json:"in"`
}

type response struct {
	Message string `json:"message"`
}

type createCallbackResponse struct {
	*response
	CallbackID string `json:"callback_id"`
}

func badRequest(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, response{Message: fmt.Sprintf("bad request: %s", err.Error())})
	}
}

func internalServerError(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, response{Message: fmt.Sprintf("internal error: %s", err.Error())})
	}
}

func writeJSON(w http.ResponseWriter, res interface{}) {
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func handleNewCallback(s storage) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req createCallbackRequest
		json.NewDecoder(r.Body).Decode(&req)
		_, err := url.ParseRequestURI(req.RemoteURL)
		if err != nil {
			badRequest(w, r, fmt.Errorf(`invalid url: "%s"`, req.RemoteURL))
			return
		}
		dur, err := time.ParseDuration(req.Duration)
		if err != nil {
			badRequest(w, r, fmt.Errorf(`invalid duration: "%s"`, req.Duration))
			return
		}
		if dur < time.Duration(1)*time.Minute {
			badRequest(w, r, fmt.Errorf(`invalid duration: "%s" is less than a minute in the future`, req.Duration))
			return
		}
		cb := &callback{
			RemoteURL: req.RemoteURL,
			Deadline:  time.Now().Add(dur),
		}
		err = s.save(cb)
		if err != nil {
			internalServerError(w, r, err)
			return
		}
		writeJSON(w, &createCallbackResponse{
			response:   &response{Message: "ok"},
			CallbackID: cb.ID,
		})
	})
}

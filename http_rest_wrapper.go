package main

import (
	"net/http"
	"encoding/json"
)

type restHandler func(http.ResponseWriter, *http.Request) (interface{}, error)

func (fn restHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	responseInterface, err := fn(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonPayload, err := json.Marshal(responseInterface)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(jsonPayload)
}
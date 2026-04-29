package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

type discoveryResponse struct {
	Status    string   `json:"status"`
	Timestamp string   `json:"timestamp"`
	Endpoints []string `json:"endpoints"`
}

func Discovery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := discoveryResponse{
			Status:    "ok",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Endpoints: []string{
				"POST /signal",
				"POST /config/reload",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

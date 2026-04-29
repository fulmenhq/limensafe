package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	apperrors "github.com/fulmenhq/limensafe/internal/errors"
)

type signalRequest struct {
	Signal    string `json:"signal"`
	Reason    string `json:"reason,omitempty"`
	Requester string `json:"requester,omitempty"`
}

var allowedSignals = map[string]struct{}{
	"SIGHUP":  {},
	"SIGTERM": {},
}

func Signal(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req signalRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apperrors.RespondWithError(w, r, apperrors.WrapInvalidInput(r.Context(), err, "invalid request body"))
			return
		}

		canonical, err := normalizeSignal(req.Signal)
		if err != nil {
			apperrors.RespondWithError(w, r, apperrors.NewInvalidInputError(err.Error()))
			return
		}
		if _, ok := allowedSignals[canonical]; !ok {
			apperrors.RespondWithError(w, r, apperrors.NewInvalidInputError("signal "+canonical+" is not allowed"))
			return
		}

		// Strip client-controlled grace period by construction.
		forward := signalRequest{Signal: canonical, Reason: req.Reason, Requester: req.Requester}
		body, err := json.Marshal(forward)
		if err != nil {
			apperrors.RespondWithError(w, r, apperrors.WrapInternal(r.Context(), err, "failed to encode signal request"))
			return
		}

		r2 := r.Clone(r.Context())
		r2.Body = io.NopCloser(bytes.NewReader(body))
		r2.ContentLength = int64(len(body))
		r2.Header.Set("Content-Type", "application/json")
		next.ServeHTTP(w, r2)
	}
}

func Reload(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := json.Marshal(signalRequest{Signal: "SIGHUP"})
		if err != nil {
			apperrors.RespondWithError(w, r, apperrors.WrapInternal(r.Context(), err, "failed to encode reload request"))
			return
		}
		r2 := r.Clone(r.Context())
		r2.Body = io.NopCloser(bytes.NewReader(body))
		r2.ContentLength = int64(len(body))
		r2.Header.Set("Content-Type", "application/json")
		next.ServeHTTP(w, r2)
	}
}

func normalizeSignal(s string) (string, error) {
	name := strings.TrimSpace(s)
	if name == "" {
		return "", errors.New("signal field is required")
	}

	upper := strings.ToUpper(name)
	if strings.HasPrefix(upper, "SIG") {
		return upper, nil
	}

	return "SIG" + upper, nil
}

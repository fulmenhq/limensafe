package handlers

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignalHandler_StripsGraceAndNormalizes(t *testing.T) {
	var captured []byte
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		captured = b
		w.WriteHeader(http.StatusOK)
	})

	h := Signal(inner)

	body := `{"signal":"term","reason":"testing","grace_period_seconds":1}`
	req := httptest.NewRequest(http.MethodPost, "http://example.test/control/signal", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(string(captured), `"signal":"SIGTERM"`) {
		t.Fatalf("expected canonical SIGTERM, got: %s", string(captured))
	}
	if strings.Contains(string(captured), "grace_period_seconds") {
		t.Fatalf("expected grace_period_seconds stripped, got: %s", string(captured))
	}
}

func TestSignalHandler_RejectsDisallowedSignal(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := Signal(inner)
	body := `{"signal":"SIGQUIT"}`
	req := httptest.NewRequest(http.MethodPost, "http://example.test/control/signal", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusBadRequest)
	}
}

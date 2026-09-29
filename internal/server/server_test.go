package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/fulmenhq/limensafe/internal/errors"
)

func TestServerUsesStandardErrorHandlers(t *testing.T) {
	srv := New("127.0.0.1", 0)

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}

	var body apperrors.HTTPErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if body.Error.Code != "NOT_FOUND" {
		t.Fatalf("expected error code NOT_FOUND, got %s", body.Error.Code)
	}
}

func TestServerPreservesTransportPeerDespiteForwardedHeaders(t *testing.T) {
	srv := New("127.0.0.1", 0)
	const peer = "192.0.2.10:41234"
	var observedPeer string
	// Probe the same router middleware stack as the registered data-plane routes.
	srv.router.Get("/peer", func(w http.ResponseWriter, r *http.Request) {
		observedPeer = r.RemoteAddr
		w.WriteHeader(http.StatusNoContent)
	})

	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{"X-Forwarded-For", map[string]string{"X-Forwarded-For": "198.51.100.42"}},
		{"X-Real-IP", map[string]string{"X-Real-IP": "198.51.100.42"}},
		{"True-Client-IP", map[string]string{"True-Client-IP": "198.51.100.42"}},
		{"all headers", map[string]string{
			"X-Forwarded-For": "198.51.100.42, 203.0.113.7",
			"X-Real-IP":       "198.51.100.43",
			"True-Client-IP":  "198.51.100.44",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/peer", nil)
			req.RemoteAddr = peer
			for name, value := range tc.headers {
				req.Header.Set(name, value)
			}
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
			}
			if observedPeer != peer {
				t.Errorf("RemoteAddr = %q, want transport peer %q", observedPeer, peer)
			}
		})
	}
}

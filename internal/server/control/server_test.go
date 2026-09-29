package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fulmenhq/limensafe/internal/config"
)

func TestControlPlane_RequiresAuthWhenTokenConfigured(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := New(config.ControlPlaneConfig{
		Enabled:     true,
		Host:        "127.0.0.1",
		Port:        0,
		BasePath:    "/control",
		BearerToken: "secret",
	}, inner)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/control/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/control/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestControlPlanePreservesTransportPeerDespiteForwardedHeaders(t *testing.T) {
	const peer = "192.0.2.10:41234"
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
			var observedPeer string
			srv := New(config.ControlPlaneConfig{
				BasePath:    "/control",
				BearerToken: "test-token",
			}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observedPeer = r.RemoteAddr
				w.WriteHeader(http.StatusNoContent)
			}))
			// The signal endpoint exercises the control router, auth and handler chain.
			req := httptest.NewRequest(http.MethodPost, "/control/signal", strings.NewReader(`{"signal":"SIGHUP"}`))
			req.RemoteAddr = peer
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("Content-Type", "application/json")
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

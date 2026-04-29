package control

import (
	"net/http"
	"net/http/httptest"
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

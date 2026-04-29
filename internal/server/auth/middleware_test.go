package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulmenhq/limensafe/internal/config"
	apperrors "github.com/fulmenhq/limensafe/internal/errors"
)

func TestMiddleware_Bearer_ProtectedRequiresAuth(t *testing.T) {
	cfg := config.DataPlaneAuthConfig{
		Enabled:     true,
		Mode:        "bearerToken",
		BearerToken: "secret",
		RoutePolicy: []config.RoutePolicyRule{{Prefix: "/", Category: "protected"}},
	}

	responder := func(w http.ResponseWriter, r *http.Request, err error) {
		apperrors.RespondWithError(w, r, err)
	}

	h := Middleware(cfg, responder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/secret", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_Bearer_AllowsWhenAuthenticatedAndSetsContext(t *testing.T) {
	cfg := config.DataPlaneAuthConfig{
		Enabled:     true,
		Mode:        "bearerToken",
		BearerToken: "secret",
		RoutePolicy: []config.RoutePolicyRule{{Prefix: "/", Category: "protected"}},
	}

	responder := func(w http.ResponseWriter, r *http.Request, err error) {
		apperrors.RespondWithError(w, r, err)
	}

	h := Middleware(cfg, responder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := Get(r.Context())
		if !info.Authenticated {
			t.Fatalf("expected authenticated")
		}
		if info.Principal != "bearer" {
			t.Fatalf("got principal %q", info.Principal)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/secret", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMiddleware_DenyReturnsNotFound(t *testing.T) {
	cfg := config.DataPlaneAuthConfig{
		Enabled:     true,
		Mode:        "bearerToken",
		BearerToken: "secret",
		RoutePolicy: []config.RoutePolicyRule{{Prefix: "/admin", Category: "deny"}},
	}

	responder := func(w http.ResponseWriter, r *http.Request, err error) {
		apperrors.RespondWithError(w, r, err)
	}

	h := Middleware(cfg, responder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/admin/secret", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want %d", w.Code, http.StatusNotFound)
	}
}

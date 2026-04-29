package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	apperrors "github.com/fulmenhq/limensafe/internal/errors"

	"github.com/fulmenhq/limensafe/internal/config"
)

type ctxKey struct{}

type Info struct {
	Authenticated bool
	Category      RouteCategory
	Principal     string
}

func Get(ctx context.Context) Info {
	if ctx == nil {
		return Info{}
	}
	if v := ctx.Value(ctxKey{}); v != nil {
		if info, ok := v.(Info); ok {
			return info
		}
	}
	return Info{}
}

func With(ctx context.Context, info Info) context.Context {
	return context.WithValue(ctx, ctxKey{}, info)
}

type ErrorResponder func(w http.ResponseWriter, r *http.Request, err error)

// Middleware enforces starter auth on the data plane.
func Middleware(cfg config.DataPlaneAuthConfig, responder ErrorResponder) func(http.Handler) http.Handler {
	rules := normalizePolicy(cfg.RoutePolicy)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cat := CategoryForPath(r.URL.Path, rules)

			// Deny category: hide existence regardless of auth.
			if cat == RouteCategoryDeny {
				responder(w, r, apperrors.NewNotFoundError("The requested resource was not found"))
				return
			}

			// Auth disabled: attach context and allow.
			if !cfg.Enabled {
				r = r.WithContext(With(r.Context(), Info{Authenticated: false, Category: cat}))
				next.ServeHTTP(w, r)
				return
			}

			authenticated, principal := authenticate(cfg, r)
			info := Info{Authenticated: authenticated, Category: cat, Principal: principal}
			r = r.WithContext(With(r.Context(), info))

			if cat == RouteCategoryProtected && !authenticated {
				responder(w, r, apperrors.NewUnauthorizedError("authentication required"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func normalizePolicy(in []config.RoutePolicyRule) []RouteRule {
	rules := make([]RouteRule, 0, len(in))
	for _, r := range in {
		pfx := strings.TrimSpace(r.Prefix)
		cat := RouteCategory(strings.TrimSpace(r.Category))
		if pfx == "" || cat == "" {
			continue
		}
		rules = append(rules, RouteRule{Prefix: pfx, Category: cat})
	}
	return rules
}

func authenticate(cfg config.DataPlaneAuthConfig, r *http.Request) (bool, string) {
	mode := strings.TrimSpace(cfg.Mode)
	switch mode {
	case "bearerToken":
		return authenticateBearer(cfg.BearerToken, r.Header.Get("Authorization"))
	case "basicAuth":
		return authenticateBasic(cfg.BasicAuth.Username, cfg.BasicAuth.Password, r)
	default:
		return false, ""
	}
}

func authenticateBearer(expected, authHeader string) (bool, string) {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false, ""
	}

	fields := strings.Fields(authHeader)
	if len(fields) != 2 {
		return false, ""
	}
	if !strings.EqualFold(fields[0], "Bearer") {
		return false, ""
	}

	provided := fields[1]
	if len(provided) != len(expected) {
		return false, ""
	}

	if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		return false, ""
	}

	return true, "bearer"
}

func authenticateBasic(username, password string, r *http.Request) (bool, string) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return false, ""
	}

	providedUser, providedPass, ok := r.BasicAuth()
	if !ok {
		return false, ""
	}

	if len(providedUser) != len(username) || subtle.ConstantTimeCompare([]byte(providedUser), []byte(username)) != 1 {
		return false, ""
	}
	if len(providedPass) != len(password) || subtle.ConstantTimeCompare([]byte(providedPass), []byte(password)) != 1 {
		return false, ""
	}

	return true, username
}

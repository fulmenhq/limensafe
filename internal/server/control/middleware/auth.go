package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	apperrors "github.com/fulmenhq/limensafe/internal/errors"
)

// BearerAuth enforces `Authorization: Bearer <token>`.
// If token is empty, this middleware is a no-op.
func BearerAuth(token string) func(http.Handler) http.Handler {
	if strings.TrimSpace(token) == "" {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	expected := strings.TrimSpace(token)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !bearerTokenMatches(r.Header.Get("Authorization"), expected) {
				apperrors.RespondWithError(w, r, apperrors.NewUnauthorizedError("authentication failed"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerTokenMatches(authHeader, expectedToken string) bool {
	fields := strings.Fields(authHeader)
	if len(fields) != 2 {
		return false
	}

	if !strings.EqualFold(fields[0], "Bearer") {
		return false
	}

	provided := fields[1]
	if len(provided) != len(expectedToken) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(provided), []byte(expectedToken)) == 1
}

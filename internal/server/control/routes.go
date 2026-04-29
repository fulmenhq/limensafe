package control

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	apperrors "github.com/fulmenhq/limensafe/internal/errors"
	"github.com/fulmenhq/limensafe/internal/server/control/handlers"
	"github.com/fulmenhq/limensafe/internal/server/control/middleware"
)

func (s *Server) registerRoutes() {
	base := s.basePath
	if base == "" {
		base = "/control"
	}

	s.router.Route(base, func(r chi.Router) {
		// Control plane auth applies to all endpoints.
		r.Use(middleware.BearerAuth(s.bearerToken))

		r.Get("/", handlers.Discovery())

		// Signal injection (restricted allowlist + stripped grace period) via gofulmen/signals handler.
		r.Post("/signal", handlers.Signal(s.signalHandler))

		// Convenience endpoint for config reload (always triggers SIGHUP).
		r.Post("/config/reload", handlers.Reload(s.signalHandler))
	})

	// For safety, return 404 on old admin endpoint.
	s.router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		apperrors.RespondWithError(w, r, apperrors.NewNotFoundError("The requested resource was not found"))
	})
}

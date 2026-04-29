package server

import (
	"github.com/fulmenhq/limensafe/internal/server/handlers"
)

// registerRoutes registers all HTTP routes
func (s *Server) registerRoutes() {
	// Standard health endpoints per Workhorse §9
	s.router.Get("/health", handlers.HealthHandler)
	s.router.Get("/health/live", handlers.LivenessHandler)
	s.router.Get("/health/ready", handlers.ReadinessHandler)
	s.router.Get("/health/startup", handlers.StartupHandler)

	// Version endpoint
	s.router.Get("/version", handlers.VersionHandler)

	// Metrics endpoint (in server package to access HandleError)
	s.router.Get("/metrics", MetricsHandler)
}

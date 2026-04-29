package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	"github.com/fulmenhq/limensafe/internal/config"
	apperrors "github.com/fulmenhq/limensafe/internal/errors"
	"github.com/fulmenhq/limensafe/internal/observability"
	"github.com/fulmenhq/limensafe/internal/server/auth"
	"github.com/fulmenhq/limensafe/internal/server/handlers"
	servermw "github.com/fulmenhq/limensafe/internal/server/middleware"
)

// Server represents the HTTP server
type Server struct {
	router *chi.Mux
	server *http.Server
	host   string
	port   int

	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration

	authConfig config.DataPlaneAuthConfig
}

type Option func(*Server)

func WithTimeouts(cfg config.ServerConfig) Option {
	return func(s *Server) {
		if cfg.ReadTimeout > 0 {
			s.readTimeout = cfg.ReadTimeout
		}
		if cfg.WriteTimeout > 0 {
			s.writeTimeout = cfg.WriteTimeout
		}
		if cfg.IdleTimeout > 0 {
			s.idleTimeout = cfg.IdleTimeout
		}
	}
}

func WithDataPlaneAuth(cfg config.DataPlaneAuthConfig) Option {
	return func(s *Server) {
		s.authConfig = cfg
	}
}

// New creates a new HTTP server instance.
func New(host string, port int, opts ...Option) *Server {
	r := chi.NewRouter()

	s := &Server{
		router:       r,
		host:         host,
		port:         port,
		readTimeout:  30 * time.Second,
		writeTimeout: 30 * time.Second,
		idleTimeout:  120 * time.Second,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	// Standard chi middleware
	r.Use(middleware.RealIP)

	// Our middleware order (RequestID → Metrics → Auth → Recovery)
	r.Use(servermw.RequestID)      // 1. Request ID (early for correlation)
	r.Use(servermw.RequestMetrics) // 2. Metrics (measure everything)

	// 3. Starter auth (optional)
	if s.authConfig.Enabled && len(s.authConfig.RoutePolicy) == 0 {
		s.authConfig.RoutePolicy = []config.RoutePolicyRule{
			{Prefix: "/health", Category: string(auth.RouteCategoryPublic)},
			{Prefix: "/version", Category: string(auth.RouteCategoryPublic)},
			{Prefix: "/metrics", Category: string(auth.RouteCategoryConditional)},
			{Prefix: "/", Category: string(auth.RouteCategoryProtected)},
		}
	}
	r.Use(auth.Middleware(s.authConfig, HandleError))

	r.Use(servermw.Recovery)

	// Chi's Recoverer is redundant since we have our own Recovery middleware
	// r.Use(middleware.Recoverer)

	// Standardized error responses using centralized HandleError
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		err := apperrors.NewNotFoundError("The requested resource was not found")
		HandleError(w, req, err)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		err := apperrors.NewMethodNotAllowedError("The requested method is not allowed for this resource")
		HandleError(w, req, err)
	})

	// Ensure handlers use the centralized error responder
	handlers.SetHTTPErrorResponder(HandleError)

	// Register routes
	s.registerRoutes()

	return s
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  s.readTimeout,
		WriteTimeout: s.writeTimeout,
		IdleTimeout:  s.idleTimeout,
	}

	observability.ServerLogger.Info("Starting HTTP server",
		zap.String("host", s.host),
		zap.Int("port", s.port),
		zap.String("addr", addr))

	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	observability.ServerLogger.Info("Shutting down HTTP server")
	return s.server.Shutdown(ctx)
}

// Handler exposes the underlying router for testing and instrumentation
func (s *Server) Handler() http.Handler {
	return s.router
}

// Port returns the server port for testing
func (s *Server) Port() int {
	return s.port
}

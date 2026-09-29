package control

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/fulmenhq/limensafe/internal/config"
	"github.com/fulmenhq/limensafe/internal/observability"
	servermw "github.com/fulmenhq/limensafe/internal/server/middleware"
)

// Server is the operational control plane HTTP server.
// It is intended to bind to loopback by default.
type Server struct {
	router *chi.Mux
	server *http.Server

	host        string
	port        int
	basePath    string
	bearerToken string

	signalHandler http.Handler
}

func New(cfg config.ControlPlaneConfig, signalHandler http.Handler) *Server {
	r := chi.NewRouter()
	// Keep the transport peer in RemoteAddr; forwarded headers are not trusted.
	r.Use(servermw.RequestID)
	r.Use(servermw.RequestMetrics)
	r.Use(servermw.Recovery)

	s := &Server{
		router:        r,
		host:          cfg.Host,
		port:          cfg.Port,
		basePath:      cfg.BasePath,
		bearerToken:   cfg.BearerToken,
		signalHandler: signalHandler,
	}

	s.registerRoutes()
	return s
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	observability.ServerLogger.Info("Starting control plane HTTP server",
		zap.String("host", s.host),
		zap.Int("port", s.port),
		zap.String("addr", addr),
		zap.String("base_path", s.basePath),
		zap.Bool("token_auth", s.bearerToken != ""),
	)

	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	observability.ServerLogger.Info("Shutting down control plane HTTP server")
	return s.server.Shutdown(ctx)
}

func (s *Server) Handler() http.Handler {
	return s.router
}

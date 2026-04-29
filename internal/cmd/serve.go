package cmd

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/fulmenhq/gofulmen/signals"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/fulmenhq/limensafe/internal/config"
	errwrap "github.com/fulmenhq/limensafe/internal/errors"
	"github.com/fulmenhq/limensafe/internal/observability"
	"github.com/fulmenhq/limensafe/internal/server"
	"github.com/fulmenhq/limensafe/internal/server/control"
	"github.com/fulmenhq/limensafe/internal/server/handlers"
)

var (
	serverPort int
	serverHost string
)

// signalHealthChecker implements HealthChecker for signal system
type signalHealthChecker struct{}

func (s signalHealthChecker) CheckHealth(ctx context.Context) error {
	// Check if signal system is responsive
	// This is a basic check - in production you might want more sophisticated checks
	return nil // Signal handlers are registered and ready
}

// telemetryHealthChecker ensures telemetry system and exporter are available
type telemetryHealthChecker struct{}

func (telemetryHealthChecker) CheckHealth(ctx context.Context) error {
	if observability.TelemetrySystem == nil || observability.PrometheusExporter == nil {
		return errwrap.NewInternalError("telemetry system not initialized")
	}
	return nil
}

// identityHealthChecker validates app identity metadata
type identityHealthChecker struct {
	binaryName string
	envPrefix  string
	configName string
}

func (i identityHealthChecker) CheckHealth(ctx context.Context) error {
	switch {
	case i.binaryName == "":
		return errwrap.NewConfigInvalidError("app identity missing binary name")
	case i.envPrefix == "":
		return errwrap.NewConfigInvalidError("app identity missing env prefix")
	case i.configName == "":
		return errwrap.NewConfigInvalidError("app identity missing config name")
	}
	return nil
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server",
	Long: `Start the HTTP server with graceful shutdown support.

Signal Handling:
  • Ctrl+C (SIGINT) or SIGTERM: Graceful shutdown
  • Ctrl+C twice within 2s: Force quit
  • SIGHUP: Config reload (placeholder - restart recommended)

The server will cleanly shut down the HTTP server and flush logs on shutdown.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		runtimeOverrides := map[string]any{}
		serverOverrides := map[string]any{}
		if cmd.Flags().Changed("host") {
			serverOverrides["host"] = serverHost
		}
		if cmd.Flags().Changed("port") {
			serverOverrides["port"] = serverPort
		}
		if len(serverOverrides) > 0 {
			runtimeOverrides["server"] = serverOverrides
		}

		loadOpts := config.LoadOptions{}
		if strings.TrimSpace(cfgFile) != "" {
			loadOpts.UserPaths = []string{cfgFile}
		}

		cfg, err := config.LoadWithOptions(cmd.Context(), loadOpts, runtimeOverrides)
		if err != nil {
			return errwrap.WrapConfigInvalid(cmd.Context(), err, "failed to load configuration")
		}

		// Get app identity for telemetry namespace
		identity := GetAppIdentity()
		namespace := identity.TelemetryNamespace()

		// Initialize server logger with namespace
		observability.InitServerLogger(identity.BinaryName, cfg.Logging.Level, namespace)

		metricsPort := cfg.Metrics.Port

		// Initialize metrics with namespace
		if cfg.Metrics.Enabled {
			if err := observability.InitMetrics(identity.BinaryName, metricsPort, namespace); err != nil {
				observability.ServerLogger.Error("Failed to initialize metrics",
					zap.Error(err))
				return errwrap.WrapInternal(cmd.Context(), err, "metrics initialization failed")
			}
		}

		observability.ServerLogger.Info("Initializing server",
			zap.String("service", identity.BinaryName),
			zap.String("namespace", namespace),
			zap.String("version", versionInfo.Version),
			zap.String("host", cfg.Server.Host),
			zap.Int("port", cfg.Server.Port),
			zap.Int("metrics_port", metricsPort))

		// Initialize health manager
		handlers.InitHealthManager(versionInfo.Version)
		hm := handlers.GetHealthManager()
		hm.RegisterChecker("signal_handlers", signalHealthChecker{})
		hm.RegisterChecker("telemetry", telemetryHealthChecker{})
		hm.RegisterChecker("app_identity", identityHealthChecker{
			binaryName: identity.BinaryName,
			envPrefix:  identity.EnvPrefix,
			configName: identity.ConfigName,
		})

		// Create data plane server
		if err := validateDataPlaneAuthConfig(cfg.DataPlaneAuth); err != nil {
			return errwrap.WrapConfigInvalid(cmd.Context(), err, "invalid data plane auth configuration")
		}
		srv := server.New(cfg.Server.Host, cfg.Server.Port,
			server.WithTimeouts(cfg.Server),
			server.WithDataPlaneAuth(cfg.DataPlaneAuth),
		)

		// Set app identity for handlers
		handlers.SetAppIdentity(identity)

		// Get shutdown timeout from config
		shutdownTimeout := cfg.Server.ShutdownTimeout
		if shutdownTimeout == 0 {
			shutdownTimeout = 10 * time.Second
		}

		// Control plane setup
		var controlSrv *control.Server
		if cfg.ControlPlane.Enabled {
			if err := validateControlPlaneConfig(cfg.Server.Port, cfg.ControlPlane); err != nil {
				return errwrap.WrapConfigInvalid(cmd.Context(), err, "invalid control plane configuration")
			}

			basePath := strings.TrimSpace(cfg.ControlPlane.BasePath)
			if basePath == "" {
				basePath = "/control"
			}
			if !strings.HasPrefix(basePath, "/") {
				basePath = "/" + basePath
			}
			cfg.ControlPlane.BasePath = basePath

			// Use gofulmen signals handler for dispatch and rate limiting.
			// Auth is enforced by control plane middleware.
			signalHandler := signals.NewHTTPHandler(signals.HTTPConfig{
				RateLimit:              10,
				RateBurst:              5,
				AllowedSignals:         []string{"SIGHUP", "SIGTERM"},
				AllowClientGracePeriod: false,
			})
			controlSrv = control.New(cfg.ControlPlane, signalHandler)
		}

		// Register graceful shutdown handlers (LIFO order - last registered, first executed)
		// Handler 1: Flush logger (executed last)
		signals.OnShutdown(func(ctx context.Context) error {
			observability.ServerLogger.Info("Flushing logger...")
			if err := observability.ServerLogger.Sync(); err != nil {
				// Sync errors are often benign (stdout/stderr already closed)
				observability.ServerLogger.Warn("Logger sync returned error (may be benign)",
					zap.Error(err))
			}
			return nil
		})

		// Handler 2: Shutdown data plane HTTP server
		signals.OnShutdown(func(ctx context.Context) error {
			observability.ServerLogger.Info("Shutting down HTTP server...")
			shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
			defer cancel()

			if err := srv.Shutdown(shutdownCtx); err != nil {
				return errwrap.WrapInternal(ctx, err, "server shutdown failed")
			}

			observability.ServerLogger.Info("HTTP server stopped gracefully")
			return nil
		})

		// Handler 3: Shutdown control plane HTTP server
		if controlSrv != nil {
			signals.OnShutdown(func(ctx context.Context) error {
				observability.ServerLogger.Info("Shutting down control plane HTTP server...")
				shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
				defer cancel()
				if err := controlSrv.Shutdown(shutdownCtx); err != nil {
					return errwrap.WrapInternal(ctx, err, "control plane shutdown failed")
				}
				return nil
			})
		}

		// Register config reload handler (SIGHUP)
		signals.OnReload(func(ctx context.Context) error {
			observability.ServerLogger.Info("Received SIGHUP: attempting config reload")

			if _, err := config.LoadWithOptions(ctx, loadOpts, runtimeOverrides); err != nil {
				observability.ServerLogger.Error("Config reload failed", zap.Error(err))
				return errwrap.WrapConfigInvalid(ctx, err, "config reload failed")
			}
			observability.ServerLogger.Info("Configuration reloaded successfully")

			// TODO: Add hooks for components that need to react to config changes
			// - Update log levels if changed
			// - Update metrics configuration if changed
			// - Notify other components of config changes

			return nil
		})

		// Enable double-tap force quit (Ctrl+C within 2 seconds)
		if err := signals.EnableDoubleTap(signals.DoubleTapConfig{
			Window:  2 * time.Second,
			Message: "Press Ctrl+C again within 2 seconds to force quit",
		}); err != nil {
			observability.ServerLogger.Warn("Failed to enable double-tap force quit",
				zap.Error(err))
		}

		// Start servers and signal listener in background
		errChan := make(chan error, 3)
		go func() {
			observability.ServerLogger.Info("Starting HTTP server...",
				zap.String("host", cfg.Server.Host),
				zap.Int("port", cfg.Server.Port))
			err := srv.Start()
			if err == http.ErrServerClosed {
				err = nil
			}
			errChan <- err
		}()

		if controlSrv != nil {
			go func() {
				err := controlSrv.Start()
				if err == http.ErrServerClosed {
					err = nil
				}
				errChan <- err
			}()
		}

		// Start signal listener in background
		go func() {
			err := signals.Listen(cmd.Context())
			// Always forward result so serve can exit cleanly on shutdown.
			errChan <- err
		}()

		// Wait for first completion/error.
		if err := <-errChan; err != nil {
			return errwrap.WrapInternal(cmd.Context(), err, "server error")
		}
		return nil
	},
}

func validateControlPlaneConfig(dataPlanePort int, cfg config.ControlPlaneConfig) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		host = "127.0.0.1"
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return errors.New("controlPlane.port must be between 1 and 65535")
	}
	if cfg.Port == dataPlanePort {
		return errors.New("controlPlane.port must differ from server.port")
	}

	if !isLoopbackHost(host) {
		if strings.TrimSpace(cfg.BearerToken) == "" {
			return errors.New("controlPlane.bearerToken is required when controlPlane.host is not loopback")
		}
	}
	return nil
}

func validateDataPlaneAuthConfig(cfg config.DataPlaneAuthConfig) error {
	if !cfg.Enabled {
		return nil
	}

	mode := strings.TrimSpace(cfg.Mode)
	switch mode {
	case "bearerToken":
		if strings.TrimSpace(cfg.BearerToken) == "" {
			return errors.New("dataPlaneAuth.bearerToken is required when dataPlaneAuth.mode is bearerToken")
		}
	case "basicAuth":
		if strings.TrimSpace(cfg.BasicAuth.Username) == "" || strings.TrimSpace(cfg.BasicAuth.Password) == "" {
			return errors.New("dataPlaneAuth.basicAuth.username and dataPlaneAuth.basicAuth.password are required when dataPlaneAuth.mode is basicAuth")
		}
	default:
		return errors.New("dataPlaneAuth.mode must be bearerToken or basicAuth when dataPlaneAuth.enabled is true")
	}

	for _, rule := range cfg.RoutePolicy {
		cat := strings.TrimSpace(rule.Category)
		switch cat {
		case "deny", "public", "conditional", "protected":
			// ok
		default:
			if cat != "" {
				return errors.New("invalid dataPlaneAuth.routePolicy category: " + cat)
			}
		}
	}

	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().StringVar(&serverHost, "host", "localhost", "server host")
	serveCmd.Flags().IntVarP(&serverPort, "port", "p", 8080, "server port")

	_ = viper.BindPFlag("server.host", serveCmd.Flags().Lookup("host"))
	_ = viper.BindPFlag("server.port", serveCmd.Flags().Lookup("port"))
}

package cmd

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/fulmenhq/limensafe/internal/config"
	gfconfig "github.com/fulmenhq/gofulmen/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/fulmenhq/limensafe/internal/observability"
	"github.com/fulmenhq/gofulmen/crucible"
)

var envInfoCmd = &cobra.Command{
	Use:   "envinfo",
	Short: "Display environment information",
	Long:  "Display comprehensive environment, configuration, and version information.",
	Run: func(cmd *cobra.Command, args []string) {
		version := crucible.GetVersion()

		observability.CLILogger.Info("=== Limensafe Environment Information ===")
		observability.CLILogger.Info("")

		// Application Info
		identity := GetAppIdentity()
		observability.CLILogger.Info("Application:")
		observability.CLILogger.Info("  Name:       " + identity.BinaryName)
		observability.CLILogger.Info("  Version:    " + versionInfo.Version)
		observability.CLILogger.Info("  Commit:     " + versionInfo.Commit)
		observability.CLILogger.Info("  Built:      " + versionInfo.BuildDate)
		observability.CLILogger.Info("")

		// SSOT Info
		observability.CLILogger.Info("SSOT:")
		observability.CLILogger.Info("  Gofulmen:   "+version.Gofulmen, zap.String("gofulmen_version", version.Gofulmen))
		observability.CLILogger.Info("  Crucible:   "+version.Crucible, zap.String("crucible_version", version.Crucible))
		observability.CLILogger.Info("")

		// Runtime Info
		observability.CLILogger.Info("Runtime:")
		observability.CLILogger.Info("  Go Version: "+runtime.Version(), zap.String("go_version", runtime.Version()))
		observability.CLILogger.Info("  GOOS:       "+runtime.GOOS, zap.String("goos", runtime.GOOS))
		observability.CLILogger.Info("  GOARCH:     "+runtime.GOARCH, zap.String("goarch", runtime.GOARCH))
		observability.CLILogger.Info(fmt.Sprintf("  NumCPU:     %d", runtime.NumCPU()), zap.Int("num_cpu", runtime.NumCPU()))
		observability.CLILogger.Info("")

		// Configuration
		loadOpts := config.LoadOptions{}
		if strings.TrimSpace(cfgFile) != "" {
			loadOpts.UserPaths = []string{cfgFile}
		}
		cfg, err := config.LoadWithOptions(cmd.Context(), loadOpts)
		if err != nil {
			observability.CLILogger.Warn("Could not load layered config (showing viper values)", zap.Error(err))
		}

		observability.CLILogger.Info("Configuration:")
		if cfg != nil {
			observability.CLILogger.Info("  Server Host:    "+cfg.Server.Host, zap.String("host", cfg.Server.Host))
			observability.CLILogger.Info(fmt.Sprintf("  Server Port:    %d", cfg.Server.Port), zap.Int("port", cfg.Server.Port))
			observability.CLILogger.Info("  Log Level:      "+cfg.Logging.Level, zap.String("log_level", cfg.Logging.Level))
			observability.CLILogger.Info("  Log Profile:    "+cfg.Logging.Profile, zap.String("log_profile", cfg.Logging.Profile))
			observability.CLILogger.Info(fmt.Sprintf("  Metrics Port:   %d", cfg.Metrics.Port), zap.Int("metrics_port", cfg.Metrics.Port))
		} else {
			observability.CLILogger.Info("  Server Host:    "+viper.GetString("server.host"), zap.String("host", viper.GetString("server.host")))
			observability.CLILogger.Info(fmt.Sprintf("  Server Port:    %d", viper.GetInt("server.port")), zap.Int("port", viper.GetInt("server.port")))
			observability.CLILogger.Info("  Log Level:      "+viper.GetString("logging.level"), zap.String("log_level", viper.GetString("logging.level")))
			observability.CLILogger.Info("  Log Profile:    "+viper.GetString("logging.profile"), zap.String("log_profile", viper.GetString("logging.profile")))
			observability.CLILogger.Info(fmt.Sprintf("  Metrics Port:   %d", viper.GetInt("metrics.port")), zap.Int("metrics_port", viper.GetInt("metrics.port")))
		}
		configFile := viper.ConfigFileUsed()
		if configFile == "" {
			observability.CLILogger.Info("  Config File:    (using defaults and environment variables)")
		} else {
			observability.CLILogger.Info("  Config File:    "+configFile, zap.String("config_file", configFile))
		}
		observability.CLILogger.Info("")

		// Environment Variables
		observability.CLILogger.Info("Environment Variables:")

		mappings := config.EnvVarMappings(identity)
		specs := config.EnvVarSpecs(identity)
		report, repErr := gfconfig.LoadEnvOverridesWithReport(specs)
		if repErr != nil {
			observability.CLILogger.Warn("Could not analyze environment variables", zap.Error(repErr))
			observability.CLILogger.Info("")
			observability.CLILogger.Info("=== End Environment Information ===")
			return
		}

		appliedBySpec := make(map[string]gfconfig.EnvVarApplied, len(report.Applied))
		for _, a := range report.Applied {
			appliedBySpec[a.SpecName] = a
		}

		// Sort by canonical name for stable display.
		sort.Slice(mappings, func(i, j int) bool { return mappings[i].Canonical < mappings[j].Canonical })

		observability.CLILogger.Info("  Canonical                              Alias                              Effective             Source")
		observability.CLILogger.Info("  -----------------------------------------------------------------------------------------------")
		for _, m := range mappings {
			alias := "-"
			if len(m.Aliases) > 0 {
				alias = m.Aliases[0]
			}

			applied, ok := appliedBySpec[m.Canonical]
			source := "-"
			if ok {
				source = string(applied.Source)
			}

			effective := envinfoEffectiveValue(cfg, m.Path)
			// If the key is sensitive, mask the effective value when non-empty.
			if config.MaskEnvValue(m.Canonical, "x") == "[set]" {
				if strings.TrimSpace(effective) != "" && effective != "false" && effective != "0" {
					effective = "[set]"
				}
			}

			observability.CLILogger.Info(fmt.Sprintf("  %-36s %-34s %-20s %s", m.Canonical, alias, effective, source))
		}

		if len(report.Conflicts) > 0 {
			observability.CLILogger.Info("")
			observability.CLILogger.Warn("Environment variable conflicts detected:")
			for _, c := range report.Conflicts {
				observability.CLILogger.Warn(fmt.Sprintf("  %s=%s (canonical) vs %s=%s (alias) -> using %s", c.CanonicalName, c.Canonical, c.AliasName, c.Alias, c.ChosenName))
			}
		}

		// Also show a quick hint for consumers.
		observability.CLILogger.Info("")
		observability.CLILogger.Info("Legend: [set] = value masked (sensitive)")

		observability.CLILogger.Info("=== End Environment Information ===")
	},
}

func envinfoEffectiveValue(cfg *config.Config, path []string) string {
	if cfg == nil {
		return ""
	}

	switch strings.Join(path, ".") {
	case "server.host":
		return cfg.Server.Host
	case "server.port":
		return fmt.Sprintf("%d", cfg.Server.Port)
	case "server.read_timeout":
		return cfg.Server.ReadTimeout.String()
	case "server.write_timeout":
		return cfg.Server.WriteTimeout.String()
	case "server.idle_timeout":
		return cfg.Server.IdleTimeout.String()
	case "server.shutdown_timeout":
		return cfg.Server.ShutdownTimeout.String()
	case "logging.level":
		return cfg.Logging.Level
	case "logging.profile":
		return cfg.Logging.Profile
	case "metrics.enabled":
		return fmt.Sprintf("%t", cfg.Metrics.Enabled)
	case "metrics.port":
		return fmt.Sprintf("%d", cfg.Metrics.Port)
	case "health.enabled":
		return fmt.Sprintf("%t", cfg.Health.Enabled)
	case "debug.enabled":
		return fmt.Sprintf("%t", cfg.Debug.Enabled)
	case "debug.pprof_enabled":
		return fmt.Sprintf("%t", cfg.Debug.PprofEnabled)
	case "controlPlane.enabled":
		return fmt.Sprintf("%t", cfg.ControlPlane.Enabled)
	case "controlPlane.host":
		return cfg.ControlPlane.Host
	case "controlPlane.port":
		return fmt.Sprintf("%d", cfg.ControlPlane.Port)
	case "controlPlane.basePath":
		return cfg.ControlPlane.BasePath
	case "controlPlane.bearerToken":
		if strings.TrimSpace(cfg.ControlPlane.BearerToken) == "" {
			return ""
		}
		return "[set]"
	case "dataPlaneAuth.enabled":
		return fmt.Sprintf("%t", cfg.DataPlaneAuth.Enabled)
	case "dataPlaneAuth.mode":
		return cfg.DataPlaneAuth.Mode
	case "dataPlaneAuth.bearerToken":
		if strings.TrimSpace(cfg.DataPlaneAuth.BearerToken) == "" {
			return ""
		}
		return "[set]"
	case "dataPlaneAuth.basicAuth.username":
		return cfg.DataPlaneAuth.BasicAuth.Username
	case "dataPlaneAuth.basicAuth.password":
		if strings.TrimSpace(cfg.DataPlaneAuth.BasicAuth.Password) == "" {
			return ""
		}
		return "[set]"
	case "workers":
		return fmt.Sprintf("%d", cfg.Workers)
	default:
		return ""
	}
}

func init() {
	rootCmd.AddCommand(envInfoCmd)
}

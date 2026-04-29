package config

import (
	"strings"

	"github.com/fulmenhq/gofulmen/appidentity"
	gfconfig "github.com/fulmenhq/gofulmen/config"
	"github.com/fulmenhq/gofulmen/logging"
)

type EnvVarMapping struct {
	Canonical   string
	Aliases     []string
	Path        []string
	Type        gfconfig.EnvVarType
	IsSensitive bool
}

func EnvVarMappings(identity *appidentity.Identity) []EnvVarMapping {
	if identity == nil {
		return nil
	}

	prefix := identity.EnvPrefix
	if prefix == "" {
		prefix = "LIMENSAFE_"
	}
	if !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}

	// Canonical names are nested (SERVER_PORT, LOGGING_LEVEL, CONTROL_PLANE_PORT, ...)
	// Aliases preserve existing shorter env vars for backward compatibility.
	return []EnvVarMapping{
		{Canonical: prefix + "SERVER_HOST", Aliases: []string{prefix + "HOST"}, Path: []string{"server", "host"}, Type: gfconfig.EnvString},
		{Canonical: prefix + "SERVER_PORT", Aliases: []string{prefix + "PORT"}, Path: []string{"server", "port"}, Type: gfconfig.EnvInt},
		{Canonical: prefix + "SERVER_READ_TIMEOUT", Aliases: []string{prefix + "READ_TIMEOUT"}, Path: []string{"server", "read_timeout"}, Type: gfconfig.EnvString},
		{Canonical: prefix + "SERVER_WRITE_TIMEOUT", Aliases: []string{prefix + "WRITE_TIMEOUT"}, Path: []string{"server", "write_timeout"}, Type: gfconfig.EnvString},
		{Canonical: prefix + "SERVER_IDLE_TIMEOUT", Aliases: []string{prefix + "IDLE_TIMEOUT"}, Path: []string{"server", "idle_timeout"}, Type: gfconfig.EnvString},
		{Canonical: prefix + "SERVER_SHUTDOWN_TIMEOUT", Aliases: []string{prefix + "SHUTDOWN_TIMEOUT"}, Path: []string{"server", "shutdown_timeout"}, Type: gfconfig.EnvString},

		{Canonical: prefix + "LOGGING_LEVEL", Aliases: []string{prefix + "LOG_LEVEL"}, Path: []string{"logging", "level"}, Type: gfconfig.EnvString},
		{Canonical: prefix + "LOGGING_PROFILE", Aliases: []string{prefix + "LOG_PROFILE"}, Path: []string{"logging", "profile"}, Type: gfconfig.EnvString},

		{Canonical: prefix + "METRICS_ENABLED", Aliases: nil, Path: []string{"metrics", "enabled"}, Type: gfconfig.EnvBool},
		{Canonical: prefix + "METRICS_PORT", Aliases: nil, Path: []string{"metrics", "port"}, Type: gfconfig.EnvInt},

		{Canonical: prefix + "HEALTH_ENABLED", Aliases: nil, Path: []string{"health", "enabled"}, Type: gfconfig.EnvBool},

		{Canonical: prefix + "DEBUG_ENABLED", Aliases: []string{prefix + "DEBUG"}, Path: []string{"debug", "enabled"}, Type: gfconfig.EnvBool},
		{Canonical: prefix + "DEBUG_PPROF_ENABLED", Aliases: []string{prefix + "PPROF_ENABLED"}, Path: []string{"debug", "pprof_enabled"}, Type: gfconfig.EnvBool},

		{Canonical: prefix + "CONTROL_PLANE_ENABLED", Aliases: []string{prefix + "CONTROLPLANE_ENABLED"}, Path: []string{"controlPlane", "enabled"}, Type: gfconfig.EnvBool},
		{Canonical: prefix + "CONTROL_PLANE_HOST", Aliases: []string{prefix + "CONTROLPLANE_HOST"}, Path: []string{"controlPlane", "host"}, Type: gfconfig.EnvString},
		{Canonical: prefix + "CONTROL_PLANE_PORT", Aliases: []string{prefix + "CONTROLPLANE_PORT"}, Path: []string{"controlPlane", "port"}, Type: gfconfig.EnvInt},
		{Canonical: prefix + "CONTROL_PLANE_BASE_PATH", Aliases: []string{prefix + "CONTROLPLANE_BASEPATH"}, Path: []string{"controlPlane", "basePath"}, Type: gfconfig.EnvString},
		{Canonical: prefix + "CONTROL_PLANE_BEARER_TOKEN", Aliases: []string{prefix + "CONTROLPLANE_BEARERTOKEN"}, Path: []string{"controlPlane", "bearerToken"}, Type: gfconfig.EnvString, IsSensitive: true},

		{Canonical: prefix + "DATA_PLANE_AUTH_ENABLED", Aliases: []string{prefix + "DATAPLANEAUTH_ENABLED"}, Path: []string{"dataPlaneAuth", "enabled"}, Type: gfconfig.EnvBool},
		{Canonical: prefix + "DATA_PLANE_AUTH_MODE", Aliases: []string{prefix + "DATAPLANEAUTH_MODE"}, Path: []string{"dataPlaneAuth", "mode"}, Type: gfconfig.EnvString},
		{Canonical: prefix + "DATA_PLANE_AUTH_BEARER_TOKEN", Aliases: []string{prefix + "DATAPLANEAUTH_BEARERTOKEN"}, Path: []string{"dataPlaneAuth", "bearerToken"}, Type: gfconfig.EnvString, IsSensitive: true},
		{Canonical: prefix + "DATA_PLANE_AUTH_BASIC_USERNAME", Aliases: []string{prefix + "DATAPLANEAUTH_BASIC_USERNAME"}, Path: []string{"dataPlaneAuth", "basicAuth", "username"}, Type: gfconfig.EnvString},
		{Canonical: prefix + "DATA_PLANE_AUTH_BASIC_PASSWORD", Aliases: []string{prefix + "DATAPLANEAUTH_BASIC_PASSWORD"}, Path: []string{"dataPlaneAuth", "basicAuth", "password"}, Type: gfconfig.EnvString, IsSensitive: true},

		{Canonical: prefix + "WORKERS", Aliases: nil, Path: []string{"workers"}, Type: gfconfig.EnvInt},
	}
}

func EnvVarSpecs(identity *appidentity.Identity) []gfconfig.EnvVarSpecWithAliases {
	mappings := EnvVarMappings(identity)
	if len(mappings) == 0 {
		return nil
	}

	specs := make([]gfconfig.EnvVarSpecWithAliases, 0, len(mappings))
	for _, m := range mappings {
		specs = append(specs, gfconfig.EnvVarSpecWithAliases{
			Name:    m.Canonical,
			Aliases: append([]string(nil), m.Aliases...),
			Path:    append([]string(nil), m.Path...),
			Type:    m.Type,
		})
	}
	return specs
}

func MaskEnvValue(envName, raw string) string {
	if logging.IsSensitiveKey(envName) {
		if strings.TrimSpace(raw) == "" {
			return ""
		}
		return "[set]"
	}
	return strings.TrimSpace(raw)
}

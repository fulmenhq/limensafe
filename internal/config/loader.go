// Package config provides centralized configuration management for the Limensafe service.
// It implements the three-layer config pattern using gofulmen/config:
// Layer 1: Crucible defaults (config/limensafe/v1.0.0/limensafe-defaults.yaml)
// Layer 2: User overrides (discovered via app identity)
// Layer 3: Environment variables and runtime overrides
package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fulmenhq/gofulmen/appidentity"
	gfconfig "github.com/fulmenhq/gofulmen/config"

	"github.com/fulmenhq/gofulmen/pathfinder"
	"github.com/fulmenhq/gofulmen/schema"
	"github.com/fulmenhq/limensafe/internal/appid"
	"github.com/go-viper/mapstructure/v2"
)

var (
	// appConfig holds the current application configuration
	appConfig   *Config
	configMu    sync.RWMutex
	appIdentity *appidentity.Identity
)

// findProjectRoot walks up from the current working directory to find the project root.
// It looks for project markers like go.mod or .git directory.
// This ensures config paths work correctly regardless of where the process is run from.
//
// This now uses gofulmen/pathfinder.FindRepositoryRoot() which provides:
// - Security boundaries (home directory ceiling, max depth protection)
// - Symlink loop detection
// - Cross-platform compatibility
// - Performance optimized (<30µs)
func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	markers := []string{"go.mod", ".git"}

	// CI-only boundary hint pattern:
	// - Treat CI workspace env vars as a boundary hint, not "the root".
	// - Still require repository markers.
	isCI := strings.EqualFold(strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")), "true") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("CI")), "true")
	if isCI {
		boundaryKeys := []string{"FULMEN_WORKSPACE_ROOT", "GITHUB_WORKSPACE", "CI_PROJECT_DIR", "WORKSPACE"}
		for _, key := range boundaryKeys {
			boundary := strings.TrimSpace(os.Getenv(key))
			if boundary == "" {
				continue
			}
			boundary = filepath.Clean(boundary)
			if !filepath.IsAbs(boundary) {
				continue
			}
			st, err := os.Stat(boundary)
			if err != nil || !st.IsDir() {
				continue
			}
			// Only accept a boundary that contains the start path.
			if rel, err := filepath.Rel(boundary, cwd); err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			rootPath, err := pathfinder.FindRepositoryRoot(cwd, markers,
				pathfinder.WithBoundary(boundary),
				pathfinder.WithMaxDepth(20),
			)
			if err == nil {
				return rootPath, nil
			}
		}
	}

	rootPath, err := pathfinder.FindRepositoryRoot(cwd, markers, pathfinder.WithMaxDepth(10))
	if err != nil {
		return "", fmt.Errorf("project root not found: %w", err)
	}

	return rootPath, nil
}

// Environment variable types
const (
	EnvString = gfconfig.EnvString
	EnvInt    = gfconfig.EnvInt
	EnvBool   = gfconfig.EnvBool
)

type LoadOptions struct {
	// UserPaths optionally overrides the set of config file paths checked for the
	// user override layer.
	//
	// If empty, Load uses XDG-derived paths based on app identity.
	UserPaths []string
}

// Load loads configuration using the three-layer pattern:
// 1. Crucible defaults from config/limensafe-defaults.yaml
// 2. User overrides from XDG config paths
// 3. Environment variables and runtime overrides
//
// This function is safe to call multiple times (e.g., for config reload)
func Load(ctx context.Context, runtimeOverrides ...map[string]any) (*Config, error) {
	return LoadWithOptions(ctx, LoadOptions{}, runtimeOverrides...)
}

// LoadWithOptions loads configuration like Load but allows overriding certain
// discovery behaviors (e.g., user config paths).
func LoadWithOptions(ctx context.Context, loadOpts LoadOptions, runtimeOverrides ...map[string]any) (*Config, error) {
	// Get app identity if not already loaded
	if appIdentity == nil {
		identity, err := appid.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load app identity: %w", err)
		}
		appIdentity = identity
	}

	// Find project root for absolute paths
	// This ensures config loading works from any working directory (including tests)
	projectRoot, err := findProjectRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to find project root: %w", err)
	}

	// Build layered config options
	// Limensafe uses its own schema (not from Crucible) located in schemas/limensafe/
	// Defaults are in config/limensafe/v1.0.0/limensafe-defaults.yaml
	// Using absolute paths ensures this works from any working directory
	catalog := schema.NewCatalog(filepath.Join(projectRoot, "schemas"))
	userPaths := loadOpts.UserPaths
	if len(userPaths) == 0 {
		userPaths = getUserConfigPaths()
	}

	opts := gfconfig.LayeredConfigOptions{
		Category:     "limensafe",
		Version:      "v1.0.0",
		DefaultsFile: "limensafe-defaults.yaml",
		SchemaID:     "limensafe/v1.0.0/config",
		UserPaths:    userPaths,
		Catalog:      catalog,
		DefaultsRoot: filepath.Join(projectRoot, "config"), // Absolute path for Layer 2 template
	}

	// Load environment variable overrides (canonical + aliases) with conflict diagnostics.
	envReport, err := gfconfig.LoadEnvOverridesWithEnvelopeAndReport(EnvVarSpecs(appIdentity), "")
	if err != nil {
		return nil, fmt.Errorf("failed to load environment overrides: %w", err)
	}
	envOverrides := envReport.Overrides
	for _, c := range envReport.Conflicts {
		fmt.Printf("WARN: Environment variable conflict detected: %s=%s (canonical) vs %s=%s (alias); using %s\n",
			c.CanonicalName, c.Canonical, c.AliasName, c.Alias, c.ChosenName,
		)
	}

	// Combine environment overrides with runtime overrides
	allOverrides := []map[string]any{envOverrides}
	allOverrides = append(allOverrides, runtimeOverrides...)

	// Load layered configuration
	merged, diagnostics, err := gfconfig.LoadLayeredConfig(opts, allOverrides...)
	if err != nil {
		return nil, fmt.Errorf("failed to load layered config: %w", err)
	}

	// Log validation diagnostics (warnings/errors from schema validation)
	// Note: We log these but don't fail hard to maintain flexibility
	for _, diag := range diagnostics {
		// TODO: Use logger when available
		fmt.Printf("Config validation: %s: %s\n", diag.Pointer, diag.Message)
	}

	// Unmarshal into typed config struct
	cfg := &Config{}
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           cfg,
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create decoder: %w", err)
	}

	if err := decoder.Decode(merged); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Store the loaded config
	setConfig(cfg)

	return cfg, nil
}

// GetConfig returns the current application configuration (thread-safe)
func GetConfig() *Config {
	configMu.RLock()
	defer configMu.RUnlock()
	return appConfig
}

// setConfig updates the current configuration (thread-safe)
func setConfig(cfg *Config) {
	configMu.Lock()
	defer configMu.Unlock()
	appConfig = cfg
}

// getUserConfigPaths returns the list of user config file paths to check
// Uses gofulmen/config for XDG-compliant path discovery
func getUserConfigPaths() []string {
	if appIdentity == nil {
		return []string{}
	}

	// Get standard config paths from gofulmen
	standardPaths := gfconfig.GetConfigPaths()

	// Convert generic paths to app-specific paths
	// Example: ~/.config/gofulmen/config.yaml → ~/.config/limensafe/config.yaml
	var paths []string
	for _, p := range standardPaths {
		// Replace "gofulmen" with our app's config name
		customPath := strings.ReplaceAll(p, "gofulmen", appIdentity.ConfigName)
		paths = append(paths, customPath)
	}

	return paths
}

// getEnvSpecs is retained for backward compatibility with older tests.
// Prefer EnvVarMappings/EnvVarSpecs.
func getEnvSpecs() []gfconfig.EnvVarSpec {
	if appIdentity == nil {
		return nil
	}

	mappings := EnvVarMappings(appIdentity)
	specs := make([]gfconfig.EnvVarSpec, 0, len(mappings))
	for _, m := range mappings {
		// Keep the historical short alias as the spec name for the basic loader.
		name := m.Canonical
		if len(m.Aliases) > 0 {
			name = m.Aliases[0]
		}
		specs = append(specs, gfconfig.EnvVarSpec{Name: name, Path: m.Path, Type: m.Type})
	}
	return specs
}

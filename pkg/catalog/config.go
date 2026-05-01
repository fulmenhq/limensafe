package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RepoConfig is the public/repo-safe configuration loaded from
// .limensafe/config.yaml (or the --config-file flag). It declares the
// repo's identity, the catalogs to load, and policy defaults. It MUST
// NOT contain raw protected vocabulary — only catalog references by
// ID. See docs/design/catalog-schema.md "Repo Config Schema".
type RepoConfig struct {
	SchemaURL     string       `yaml:"$schema"`
	SchemaVersion string       `yaml:"schema_version"`
	Repo          RepoIdentity `yaml:"repo"`
	Catalogs      []CatalogRef `yaml:"catalogs"`
	Policy        Policy       `yaml:"policy"`
}

// RepoIdentity describes the repo being scanned. The id field is
// emitted in scan output and is therefore subject to the ID-safety
// rule (no protected vocabulary).
type RepoIdentity struct {
	ID           string `yaml:"id"`
	Visibility   string `yaml:"visibility"`
	EngagementID string `yaml:"engagement_id"`
	Description  string `yaml:"description"`
}

// CatalogRef points at a catalog by ID with an explicit source. v0
// supports source.kind = "file" and "env"; profile and url are
// reserved for v0.x / v2 respectively.
type CatalogRef struct {
	CatalogID string        `yaml:"catalog_id"`
	Source    CatalogSource `yaml:"source"`
	Optional  bool          `yaml:"optional"`
}

// CatalogSource is a tagged union over file / env / profile / url. v0
// implements file and env; the others return an "unsupported" error
// at resolve time so the field shape can land now without forcing
// implementation.
type CatalogSource struct {
	Kind string `yaml:"kind"`
	Path string `yaml:"path,omitempty"`
	Var  string `yaml:"var,omitempty"`
	Name string `yaml:"name,omitempty"`
	URL  string `yaml:"url,omitempty"`
}

// Policy carries defaults applied when individual entities/rules don't
// override. v0 honors block_threshold via decisionForSeverity in the
// engine; the rest are documented in catalog-schema.md and will land
// in the engine's policy package as v0.x scope expands.
type Policy struct {
	DefaultSeverity              string `yaml:"default_severity"`
	BlockThreshold               string `yaml:"block_threshold"`
	RequireReplacementSuggestion bool   `yaml:"require_replacement_suggestion"`
	RedactionSafeOutput          bool   `yaml:"redaction_safe_output"`
	CoOccurrenceEnabled          bool   `yaml:"co_occurrence_enabled"`
}

// LoadConfigFile reads + validates a repo config from disk.
func LoadConfigFile(path string) (*RepoConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	cfg, err := LoadConfigBytes(data)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}

// LoadConfigBytes parses + validates a repo config from raw bytes.
func LoadConfigBytes(data []byte) (*RepoConfig, error) {
	var cfg RepoConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	return &cfg, nil
}

// Validate enforces v0 minimum-viable schema rules for repo configs.
func (cfg *RepoConfig) Validate() error {
	if cfg.SchemaVersion == "" {
		return fmt.Errorf("schema_version is required")
	}
	if cfg.Repo.ID == "" {
		return fmt.Errorf("repo.id is required")
	}
	if cfg.Repo.Visibility == "" {
		return fmt.Errorf("repo.visibility is required")
	}
	if len(cfg.Catalogs) == 0 {
		return fmt.Errorf("at least one catalog reference is required")
	}

	seen := map[string]bool{}
	for i, ref := range cfg.Catalogs {
		if ref.CatalogID == "" {
			return fmt.Errorf("catalogs[%d]: catalog_id is required", i)
		}
		if seen[ref.CatalogID] {
			return fmt.Errorf("catalogs[%d] (%s): duplicate catalog_id", i, ref.CatalogID)
		}
		seen[ref.CatalogID] = true
		if ref.Source.Kind == "" {
			return fmt.Errorf("catalogs[%d] (%s): source.kind is required", i, ref.CatalogID)
		}
		switch ref.Source.Kind {
		case "file":
			if ref.Source.Path == "" {
				return fmt.Errorf("catalogs[%d] (%s): file source requires path", i, ref.CatalogID)
			}
		case "env":
			if ref.Source.Var == "" {
				return fmt.Errorf("catalogs[%d] (%s): env source requires var", i, ref.CatalogID)
			}
		case "profile", "url":
			// recognized but not implemented in v0
		default:
			return fmt.Errorf("catalogs[%d] (%s): unsupported source.kind %q", i, ref.CatalogID, ref.Source.Kind)
		}
	}

	return nil
}

// CatalogResolution reports the outcome of resolving one catalog
// reference. The output formatter uses LoadStatus to populate
// scan_metadata.catalogs_loaded[].load_status.
type CatalogResolution struct {
	CatalogID  string
	Catalog    *Catalog // nil when not loaded
	LoadStatus string   // "ok" | "absent_optional"
	Source     string   // resolved path (file kind) or env var name (env kind)
	Err        error    // populated on resolution error (e.g., missing_required)
}

// ResolveCatalogs reads each catalog reference and loads the catalogs.
// configPath is the absolute path to the config file being processed
// (used to resolve relative file paths). For env sources, missing env
// vars produce LoadStatus="absent_optional" if Optional, error if not.
//
// Returns one CatalogResolution per reference; the slice order
// matches cfg.Catalogs. The first hard error (missing required
// catalog, malformed YAML) returns immediately with err non-nil; the
// returned slice contains all successfully resolved entries plus an
// Err on the failing one.
func (cfg *RepoConfig) ResolveCatalogs(configPath string) ([]CatalogResolution, error) {
	configDir := filepath.Dir(configPath)
	resolutions := make([]CatalogResolution, 0, len(cfg.Catalogs))

	for _, ref := range cfg.Catalogs {
		res := CatalogResolution{CatalogID: ref.CatalogID}

		switch ref.Source.Kind {
		case "file":
			path := ref.Source.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(configDir, path)
			}
			res.Source = path
			c, err := LoadFile(path)
			if err != nil {
				if ref.Optional {
					res.LoadStatus = "absent_optional"
					resolutions = append(resolutions, res)
					continue
				}
				res.Err = fmt.Errorf("required catalog %s: %w", ref.CatalogID, err)
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			if c.CatalogID != ref.CatalogID {
				res.Err = fmt.Errorf("catalog id mismatch: config refers to %q, file declares %q", ref.CatalogID, c.CatalogID)
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			res.Catalog = c
			res.LoadStatus = "ok"

		case "env":
			path := os.Getenv(ref.Source.Var)
			res.Source = ref.Source.Var
			if path == "" {
				if ref.Optional {
					res.LoadStatus = "absent_optional"
					resolutions = append(resolutions, res)
					continue
				}
				res.Err = fmt.Errorf("required catalog %s: env var %s is unset", ref.CatalogID, ref.Source.Var)
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			c, err := LoadFile(path)
			if err != nil {
				if ref.Optional {
					res.LoadStatus = "absent_optional"
					resolutions = append(resolutions, res)
					continue
				}
				res.Err = fmt.Errorf("required catalog %s (from %s=%s): %w", ref.CatalogID, ref.Source.Var, path, err)
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			if c.CatalogID != ref.CatalogID {
				res.Err = fmt.Errorf("catalog id mismatch: config refers to %q, file declares %q", ref.CatalogID, c.CatalogID)
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			res.Catalog = c
			res.LoadStatus = "ok"

		case "profile", "url":
			// Recognized but not implemented in v0. Treat as
			// absent_optional regardless of the Optional flag — these
			// source kinds will land in v0.x / v2 with proper handling.
			res.LoadStatus = "absent_optional"
			res.Source = ref.Source.Kind + " (not implemented in v0)"
			if !ref.Optional {
				res.Err = fmt.Errorf("required catalog %s: source.kind %q not implemented in v0", ref.CatalogID, ref.Source.Kind)
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}

		default:
			res.Err = fmt.Errorf("catalog %s: unsupported source.kind %q", ref.CatalogID, ref.Source.Kind)
			resolutions = append(resolutions, res)
			return resolutions, res.Err
		}

		resolutions = append(resolutions, res)
	}

	return resolutions, nil
}

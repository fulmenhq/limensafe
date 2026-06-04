package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	PrivateCatalogMissingSilent = "silent"
	PrivateCatalogMissingWarn   = "warn"
	PrivateCatalogMissingError  = "error"

	LoadStatusOK             = "ok"
	LoadStatusAbsentOptional = "absent_optional"
	LoadStatusMissingWarn    = "missing_warn"
	LoadStatusMissingError   = "missing_error"
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
// supports source.kind = "file", "env", and "builtin"; profile and
// url are reserved for v0.x / v2 respectively.
type CatalogRef struct {
	CatalogID string        `yaml:"catalog_id"`
	Source    CatalogSource `yaml:"source"`
	Optional  bool          `yaml:"optional"`
}

// CatalogSource is a tagged union over file / env / builtin / profile / url.
// v0 implements file, env, and builtin; profile/url return an
// "unsupported" error at resolve time so the field shape can land now
// without forcing implementation.
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
	PrivateCatalogMissing        string `yaml:"private_catalog_missing"`
	RequireReplacementSuggestion bool   `yaml:"require_replacement_suggestion"`
	RedactionSafeOutput          bool   `yaml:"redaction_safe_output"`
	CoOccurrenceEnabled          bool   `yaml:"co_occurrence_enabled"`
}

// ResolveOptions configures repo catalog resolution. PrivateCatalogMissing
// applies to optional catalog references that cannot be loaded.
type ResolveOptions struct {
	PrivateCatalogMissing string
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
			return fmt.Errorf("catalogs[%d]: duplicate catalog_id", i)
		}
		seen[ref.CatalogID] = true
		if ref.Source.Kind == "" {
			return fmt.Errorf("catalogs[%d]: source.kind is required", i)
		}
		switch ref.Source.Kind {
		case "file":
			if ref.Source.Path == "" {
				return fmt.Errorf("catalogs[%d]: file source requires path", i)
			}
		case "env":
			if ref.Source.Var == "" {
				return fmt.Errorf("catalogs[%d]: env source requires var", i)
			}
		case "builtin":
			if ref.Source.Name == "" {
				return fmt.Errorf("catalogs[%d]: builtin source requires name", i)
			}
		case "profile", "url":
			// recognized but not implemented in v0
		default:
			return fmt.Errorf("catalogs[%d]: unsupported source.kind", i)
		}
	}

	if cfg.Policy.PrivateCatalogMissing != "" && !ValidPrivateCatalogMissing(cfg.Policy.PrivateCatalogMissing) {
		return fmt.Errorf("policy.private_catalog_missing must be one of silent, warn, error")
	}

	return nil
}

func ValidPrivateCatalogMissing(value string) bool {
	switch value {
	case PrivateCatalogMissingSilent, PrivateCatalogMissingWarn, PrivateCatalogMissingError:
		return true
	default:
		return false
	}
}

// CatalogResolution reports the outcome of resolving one catalog
// reference. The output formatter uses LoadStatus to populate
// scan_metadata.catalogs_loaded[].load_status.
type CatalogResolution struct {
	CatalogID     string
	Catalog       *Catalog // nil when not loaded
	LoadStatus    string   // "ok" | "absent_optional" | "missing_warn" | "missing_error"
	Source        string   // resolved path (file kind) or env var name (env kind); never emitted directly
	SourceKind    string
	Optional      bool
	MissingReason string // sanitized machine reason when the catalog is absent
	Err           error  // populated on resolution error (e.g., missing_required)
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
	return cfg.ResolveCatalogsWithOptions(configPath, ResolveOptions{})
}

func (cfg *RepoConfig) ResolveCatalogsWithOptions(configPath string, opts ResolveOptions) ([]CatalogResolution, error) {
	configDir := filepath.Dir(configPath)
	resolutions := make([]CatalogResolution, 0, len(cfg.Catalogs))
	missingPosture := opts.PrivateCatalogMissing
	if missingPosture == "" {
		missingPosture = PrivateCatalogMissingSilent
	}
	if !ValidPrivateCatalogMissing(missingPosture) {
		return nil, fmt.Errorf("policy.private_catalog_missing must be one of silent, warn, error")
	}

	for _, ref := range cfg.Catalogs {
		res := CatalogResolution{
			CatalogID:  ref.CatalogID,
			SourceKind: ref.Source.Kind,
			Optional:   ref.Optional,
		}

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
					if done, resolveErr := appendMissingOptional(&resolutions, res, missingPosture, "file_unavailable"); done {
						if resolveErr != nil {
							return resolutions, resolveErr
						}
						continue
					}
				}
				res.MissingReason = "file_unavailable"
				res.Err = fmt.Errorf("required catalog: source kind file unavailable")
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			if c.CatalogID != ref.CatalogID {
				res.Err = fmt.Errorf("catalog id mismatch: source kind file declares a different catalog_id")
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			res.Catalog = c
			res.LoadStatus = LoadStatusOK

		case "env":
			path := os.Getenv(ref.Source.Var)
			res.Source = ref.Source.Var
			if path == "" {
				if ref.Optional {
					if done, resolveErr := appendMissingOptional(&resolutions, res, missingPosture, "env_unset"); done {
						if resolveErr != nil {
							return resolutions, resolveErr
						}
						continue
					}
				}
				res.MissingReason = "env_unset"
				res.Err = fmt.Errorf("required catalog: source kind env unavailable")
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			c, err := LoadFile(path)
			if err != nil {
				if ref.Optional {
					if done, resolveErr := appendMissingOptional(&resolutions, res, missingPosture, "env_file_unavailable"); done {
						if resolveErr != nil {
							return resolutions, resolveErr
						}
						continue
					}
				}
				res.MissingReason = "env_file_unavailable"
				res.Err = fmt.Errorf("required catalog: source kind env unavailable")
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			if c.CatalogID != ref.CatalogID {
				res.Err = fmt.Errorf("catalog id mismatch: source kind env declares a different catalog_id")
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			res.Catalog = c
			res.LoadStatus = LoadStatusOK

		case "builtin":
			res.Source = ref.Source.Name
			c, err := LoadBuiltin(ref.Source.Name)
			if err != nil {
				if ref.Optional {
					if done, resolveErr := appendMissingOptional(&resolutions, res, missingPosture, "builtin_unavailable"); done {
						if resolveErr != nil {
							return resolutions, resolveErr
						}
						continue
					}
				}
				res.MissingReason = "builtin_unavailable"
				res.Err = fmt.Errorf("required catalog: source kind builtin unavailable")
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			if c.CatalogID != ref.CatalogID {
				res.Err = fmt.Errorf("catalog id mismatch: source kind builtin declares a different catalog_id")
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}
			res.Catalog = c
			res.LoadStatus = LoadStatusOK

		case "profile", "url":
			// Recognized but not implemented in v0. Treat as
			// an absent optional source when optional. These source kinds
			// will land in v0.x / v2 with proper handling.
			res.Source = ref.Source.Kind + " (not implemented in v0)"
			if ref.Optional {
				if done, resolveErr := appendMissingOptional(&resolutions, res, missingPosture, "source_kind_unimplemented"); done {
					if resolveErr != nil {
						return resolutions, resolveErr
					}
					continue
				}
			}
			if !ref.Optional {
				res.MissingReason = "source_kind_unimplemented"
				res.Err = fmt.Errorf("required catalog: source kind not implemented in v0")
				resolutions = append(resolutions, res)
				return resolutions, res.Err
			}

		default:
			res.Err = fmt.Errorf("catalog: unsupported source.kind")
			resolutions = append(resolutions, res)
			return resolutions, res.Err
		}

		resolutions = append(resolutions, res)
	}

	return resolutions, nil
}

func appendMissingOptional(resolutions *[]CatalogResolution, res CatalogResolution, posture, reason string) (bool, error) {
	res.MissingReason = reason
	switch posture {
	case PrivateCatalogMissingSilent:
		res.LoadStatus = LoadStatusAbsentOptional
		*resolutions = append(*resolutions, res)
		return true, nil
	case PrivateCatalogMissingWarn:
		res.LoadStatus = LoadStatusMissingWarn
		*resolutions = append(*resolutions, res)
		return true, nil
	case PrivateCatalogMissingError:
		res.LoadStatus = LoadStatusMissingError
		res.Err = fmt.Errorf("catalog missing and policy.private_catalog_missing=error")
		*resolutions = append(*resolutions, res)
		return true, res.Err
	default:
		return false, fmt.Errorf("policy.private_catalog_missing must be one of silent, warn, error")
	}
}

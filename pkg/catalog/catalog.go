// Package catalog loads and validates limensafe vocabulary bundles
// (private catalogs) and repo configs.
//
// The two-layer separation is documented in
// docs/design/catalog-schema.md:
//
//   - Vocabulary bundle (private, "catalog"): raw protected vocabulary;
//     never lives in a public repo. Loaded via Load* functions in this
//     package.
//   - Repo config (public): references catalogs by ID and declares
//     repo-local policy. Loaded by pkg/catalog/config.go (forthcoming).
//
// V0 implements the minimum viable schema. Forward-compatibility is
// preserved: unknown fields cause a warning but not a load error
// (gracefully ignored).
package catalog

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/fulmenhq/limensafe/pkg/output"
	"gopkg.in/yaml.v3"
)

// Catalog is a vocabulary bundle. See docs/design/catalog-schema.md
// "Vocabulary Bundle Schema" for the canonical field list.
type Catalog struct {
	SchemaURL         string             `yaml:"$schema"`
	CatalogID         string             `yaml:"catalog_id"`
	SchemaVersion     string             `yaml:"schema_version"`
	Description       string             `yaml:"description"`
	DefaultSeverity   string             `yaml:"default_severity"`
	FingerprintSalt   string             `yaml:"fingerprint_salt"`
	Entities          []Entity           `yaml:"entities"`
	CoOccurrenceRules []CoOccurrenceRule `yaml:"co_occurrence_rules"`
	Warnings          []string           `yaml:"-"`
}

// Entity is one protected entity in a catalog.
type Entity struct {
	ID                    string         `yaml:"id"`
	Class                 string         `yaml:"class"`
	Aliases               []string       `yaml:"aliases"`
	Variants              EntityVariants `yaml:"variants"`
	Tokens                []string       `yaml:"tokens"`
	RegexPatterns         []string       `yaml:"regex_patterns"`
	ReplacementFor        string         `yaml:"replacement_for"`
	ReplacementSuggestion string         `yaml:"replacement_suggestion"`
	AllowedIn             []string       `yaml:"allowed_in"`
	BlockedIn             []string       `yaml:"blocked_in"`
	VisibilityScope       string         `yaml:"visibility_scope"`
	SeverityOverride      string         `yaml:"severity_override"`
	DisclosureSafe        bool           `yaml:"disclosure_safe"`
	Notes                 string         `yaml:"notes"`
}

// EntityVariants control auto-expansion of an entity's aliases at
// matcher-build time. The catalog package does not expand variants;
// that is the engine's job. The flags are surfaced here so the engine
// can read them.
type EntityVariants struct {
	CaseInsensitive bool `yaml:"case_insensitive"`
	Slug            bool `yaml:"slug"`
	Pluralize       bool `yaml:"pluralize"`
	PathSegments    bool `yaml:"path_segments"`
	WholeWord       bool `yaml:"whole_word"`
	WholeWordSet    bool `yaml:"-"`
}

func (v *EntityVariants) UnmarshalYAML(value *yaml.Node) error {
	type rawVariants struct {
		CaseInsensitive bool  `yaml:"case_insensitive"`
		Slug            bool  `yaml:"slug"`
		Pluralize       bool  `yaml:"pluralize"`
		PathSegments    bool  `yaml:"path_segments"`
		WholeWord       *bool `yaml:"whole_word"`
	}

	var raw rawVariants
	if err := value.Decode(&raw); err != nil {
		return err
	}

	v.CaseInsensitive = raw.CaseInsensitive
	v.Slug = raw.Slug
	v.Pluralize = raw.Pluralize
	v.PathSegments = raw.PathSegments
	v.WholeWordSet = raw.WholeWord != nil
	if raw.WholeWord != nil {
		v.WholeWord = *raw.WholeWord
	}
	return nil
}

// CoOccurrenceRule fires when its constituent terms appear within the
// declared window. See docs/design/catalog-schema.md for window_kind
// semantics.
type CoOccurrenceRule struct {
	RuleID           string   `yaml:"rule_id"`
	Description      string   `yaml:"description"`
	Terms            []string `yaml:"terms"`
	WindowKind       string   `yaml:"window_kind"`
	WindowSize       int      `yaml:"window_size"`
	SeverityOverride string   `yaml:"severity_override"`
	ApplyIn          []string `yaml:"apply_in"`
}

// LoadFile reads a catalog YAML file from disk and returns the parsed
// + validated Catalog. The file's directory is irrelevant to the
// returned struct; the loader does not resolve paths in catalog source
// references — that is the repo config's job.
func LoadFile(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("catalog: read %s: %w", path, err)
	}
	c, err := LoadBytes(data)
	if err != nil {
		return nil, fmt.Errorf("catalog: %s: %w", path, err)
	}
	return c, nil
}

// LoadBytes parses and validates a catalog from raw bytes. Used by
// LoadFile and by tests / in-memory loaders.
func LoadBytes(data []byte) (*Catalog, error) {
	var c Catalog
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	c.Warnings = c.collectWarnings()
	return &c, nil
}

// Validate enforces v0 minimum-viable schema rules. The full schema
// validation (JSON Schema 2020-12) is owned by entarch's
// schemas/v1/catalog.schema.json and will be wired in once authored;
// this method covers the structural invariants v0 needs.
func (c *Catalog) Validate() error {
	if c.CatalogID == "" {
		return fmt.Errorf("catalog_id is required")
	}
	if c.SchemaVersion == "" {
		return fmt.Errorf("schema_version is required")
	}
	if len(c.Entities) == 0 {
		return fmt.Errorf("at least one entity is required")
	}

	seen := map[string]bool{}
	for i, e := range c.Entities {
		if e.ID == "" {
			return fmt.Errorf("entity[%d]: id is required", i)
		}
		if seen[e.ID] {
			return fmt.Errorf("entity[%d] (%s): duplicate id", i, e.ID)
		}
		seen[e.ID] = true

		if e.Class == "" {
			return fmt.Errorf("entity %s: class is required", e.ID)
		}
		if len(e.Aliases) == 0 && len(e.RegexPatterns) == 0 {
			return fmt.Errorf("entity %s: must have at least one alias or regex_pattern", e.ID)
		}
	}

	rseen := map[string]bool{}
	for i, r := range c.CoOccurrenceRules {
		if r.RuleID == "" {
			return fmt.Errorf("co_occurrence_rule[%d]: rule_id is required", i)
		}
		if rseen[r.RuleID] {
			return fmt.Errorf("co_occurrence_rule[%d] (%s): duplicate rule_id", i, r.RuleID)
		}
		rseen[r.RuleID] = true

		if len(r.Terms) < 2 {
			return fmt.Errorf("co_occurrence_rule %s: requires at least 2 terms", r.RuleID)
		}
		for _, term := range r.Terms {
			if !seen[term] {
				return fmt.Errorf("co_occurrence_rule %s: unknown term %q (no entity with that id)", r.RuleID, term)
			}
		}
		if r.WindowKind == "" {
			return fmt.Errorf("co_occurrence_rule %s: window_kind is required", r.RuleID)
		}
	}

	return nil
}

func (c *Catalog) collectWarnings() []string {
	var warnings []string
	for _, e := range c.Entities {
		if effectiveWholeWord(e) && e.Variants.CaseInsensitive {
			warnings = append(warnings,
				fmt.Sprintf("entity %s: whole_word=true + case_insensitive=true — boundary applies to the lowercased token; verify this is intended", e.ID))
		}
	}
	return warnings
}

func effectiveWholeWord(e Entity) bool {
	if e.Variants.WholeWordSet {
		return e.Variants.WholeWord
	}
	for _, alias := range e.Aliases {
		if utf8.RuneCountInString(strings.TrimSpace(alias)) < 4 {
			return true
		}
	}
	return false
}

// ToOutputAliases flattens the catalog's literal aliases into the form
// the output.Redactor expects. Variant expansion (slug, pluralize,
// path_segments) is NOT performed here — that lives in the engine,
// which builds the actual matcher. The redactor only needs the literal
// alias strings tagged with their entity ID.
//
// CaseInsensitive on each alias is taken from the entity's
// variants.case_insensitive flag.
func (c *Catalog) ToOutputAliases() []output.Alias {
	var aliases []output.Alias
	for _, e := range c.Entities {
		ci := e.Variants.CaseInsensitive
		for _, a := range e.Aliases {
			if a == "" {
				continue
			}
			aliases = append(aliases, output.Alias{
				Pattern:         a,
				EntityID:        e.ID,
				CaseInsensitive: ci,
			})
		}
	}
	return aliases
}

// MergeAliases combines aliases from multiple catalogs. Used at scan
// start when the repo config has loaded N catalogs and we need a
// single Redactor for output.
func MergeAliases(catalogs []*Catalog) []output.Alias {
	var all []output.Alias
	for _, c := range catalogs {
		if c == nil {
			continue
		}
		all = append(all, c.ToOutputAliases()...)
	}
	return all
}

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
// V0 implements the minimum viable schema. The embedded JSON Schema is the
// structural contract; unknown fields are rejected before load.
package catalog

import (
	"fmt"
	"os"
	"regexp"
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
	Allowlist         []AllowlistEntry   `yaml:"allowlist"`
	Warnings          []string           `yaml:"-"`
}

// AllowlistEntry is one catalog-level allowlist rule (internal-brief). A match
// suppresses any finding whose matched span it fully covers, regardless of the
// producing entity — the "subtract the allowlist, then match" rule. The Pattern
// is catalog-private and never emitted; only the alias-safe ID is
// output-visible (it surfaces in suppression accounting and --explain).
type AllowlistEntry struct {
	ID       string            `yaml:"id"`
	Kind     string            `yaml:"kind"`
	Pattern  string            `yaml:"pattern"`
	Variants AllowlistVariants `yaml:"variants"`
	Reason   string            `yaml:"reason"`
}

// AllowlistVariants control case/word-boundary matching for an allowlist entry.
// Allowlist entries get the same case_insensitive and whole_word flags aliases
// get; generative variants (slug, pluralize, path_segments) do not apply.
// WholeWord follows the alias default (true for literals shorter than four
// characters unless explicitly set); WholeWordSet records whether the author
// supplied an explicit value, mirroring EntityVariants.
type AllowlistVariants struct {
	CaseInsensitive bool `yaml:"case_insensitive"`
	WholeWord       bool `yaml:"whole_word"`
	WholeWordSet    bool `yaml:"-"`
}

const (
	// AllowlistKindLiteral matches an exact string (with optional variant flags).
	AllowlistKindLiteral = "literal"
	// AllowlistKindRegex matches an RE2 pattern.
	AllowlistKindRegex = "regex"
)

func (v *AllowlistVariants) UnmarshalYAML(value *yaml.Node) error {
	type rawVariants struct {
		CaseInsensitive bool  `yaml:"case_insensitive"`
		WholeWord       *bool `yaml:"whole_word"`
	}
	var raw rawVariants
	if err := value.Decode(&raw); err != nil {
		return err
	}
	v.CaseInsensitive = raw.CaseInsensitive
	v.WholeWordSet = raw.WholeWord != nil
	if raw.WholeWord != nil {
		v.WholeWord = *raw.WholeWord
	}
	return nil
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
//
// Validation is layered: schema_version compatibility policy, then the
// embedded JSON Schema (structural contract, redaction-safe diagnostics),
// then the Go-level invariants in Validate (duplicate ids, output-visible
// ID alias-safety) that the schema does not express. Compatibility advisories
// (forward minor/patch, omitted $schema) surface as LoadWarnings, not errors.
func LoadBytes(data []byte) (*Catalog, error) {
	var c Catalog
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	// schema_version policy first, so a major mismatch or malformed version
	// yields an actionable message rather than a raw schema pointer. An empty
	// version falls through to the schema's required-field check below.
	var versionWarning string
	if strings.TrimSpace(c.SchemaVersion) != "" {
		w, err := schemaVersionPolicy(c.SchemaVersion)
		if err != nil {
			return nil, fmt.Errorf("validate: %w", err)
		}
		versionWarning = w
	}
	if err := validateKnownCatalogAuthoringMistakes(data); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	// Structural contract against the embedded JSON Schema. Diagnostics carry
	// only JSON-pointers + keywords, never instance content (ADR-0003).
	if err := validateAgainstSchema(data); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	c.Warnings = c.collectWarnings()
	if w := schemaURIWarning(c.SchemaURL); w != "" {
		c.Warnings = append(c.Warnings, w)
	}
	if versionWarning != "" {
		c.Warnings = append(c.Warnings, versionWarning)
	}
	return &c, nil
}

// Validate enforces the Go-level catalog invariants that the JSON Schema does
// not express: duplicate entity/rule ids and output-visible ID alias-safety.
// The structural shape contract is enforced by the embedded JSON Schema in
// LoadBytes (see validateAgainstSchema); this method is the semantic-safety
// layer and remains a defense-in-depth check for direct callers.
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
			return fmt.Errorf("entity[%d]: duplicate id", i)
		}
		seen[e.ID] = true

		if e.Class == "" {
			return fmt.Errorf("entity[%d]: class is required", i)
		}
		if len(e.Aliases) == 0 && len(e.Tokens) == 0 && len(e.RegexPatterns) == 0 {
			return fmt.Errorf("entity[%d]: must have at least one alias, token, or regex_patterns", i)
		}
	}
	if err := c.validateEntityIDAliasSafety(); err != nil {
		return err
	}

	rseen := map[string]bool{}
	for i, r := range c.CoOccurrenceRules {
		if r.RuleID == "" {
			return fmt.Errorf("co_occurrence_rule[%d]: rule_id is required", i)
		}
		if rseen[r.RuleID] {
			return fmt.Errorf("co_occurrence_rule[%d]: duplicate rule_id", i)
		}
		rseen[r.RuleID] = true

		if len(r.Terms) < 2 {
			return fmt.Errorf("co_occurrence_rule[%d]: requires at least 2 terms", i)
		}
		for _, term := range r.Terms {
			if !seen[term] {
				return fmt.Errorf("co_occurrence_rule[%d]: unknown term", i)
			}
		}
		if r.WindowKind == "" {
			return fmt.Errorf("co_occurrence_rule[%d]: window_kind is required", i)
		}
	}

	if err := c.validateAllowlist(); err != nil {
		return err
	}

	return nil
}

// validateAllowlist enforces the Go-level allowlist invariants the JSON Schema
// does not express: duplicate ids, valid kind, regex compilability, and
// output-visible ID alias-safety. Diagnostics are value-free — they never echo
// the allowlist pattern (which is catalog-private and may itself be protected
// vocabulary, e.g. a codename that is also a public repo).
func (c *Catalog) validateAllowlist() error {
	aliases := c.ToOutputAliases()
	seen := map[string]bool{}
	for i, a := range c.Allowlist {
		if a.ID == "" {
			return fmt.Errorf("allowlist[%d]: id is required", i)
		}
		if seen[a.ID] {
			return fmt.Errorf("allowlist[%d]: duplicate id", i)
		}
		seen[a.ID] = true

		switch a.Kind {
		case AllowlistKindLiteral, AllowlistKindRegex:
		default:
			return fmt.Errorf("allowlist[%d]: kind must be %q or %q", i, AllowlistKindLiteral, AllowlistKindRegex)
		}
		if a.Pattern == "" {
			return fmt.Errorf("allowlist[%d]: pattern is required", i)
		}
		if a.Kind == AllowlistKindRegex {
			if _, err := regexp.Compile(a.Pattern); err != nil {
				// Value-free: an invalid regex error from the stdlib quotes the
				// offending pattern, which is catalog-private (ADR-0003). Report
				// only the index and structural reason.
				return fmt.Errorf("allowlist[%d]: regex pattern does not compile", i)
			}
		}
		for _, alias := range aliases {
			if alias.Pattern == "" {
				continue
			}
			if containsAliasSubstring(a.ID, alias) {
				return fmt.Errorf("allowlist[%d]: id contains a protected alias substring", i)
			}
		}
	}
	return nil
}

func (c *Catalog) validateEntityIDAliasSafety() error {
	aliases := c.ToOutputAliases()
	for i, e := range c.Entities {
		for _, alias := range aliases {
			if alias.Pattern == "" {
				continue
			}
			if containsAliasSubstring(e.ID, alias) {
				return fmt.Errorf("entity[%d]: id contains a protected alias substring", i)
			}
		}
	}
	return nil
}

func containsAliasSubstring(value string, alias output.Alias) bool {
	if alias.CaseInsensitive {
		return strings.Contains(strings.ToLower(value), strings.ToLower(alias.Pattern))
	}
	return strings.Contains(value, alias.Pattern)
}

func (c *Catalog) collectWarnings() []string {
	var warnings []string
	for _, e := range c.Entities {
		if effectiveWholeWord(e) && e.Variants.CaseInsensitive {
			warnings = append(warnings,
				"entity: whole_word=true + case_insensitive=true — boundary applies to the lowercased token; verify this is intended")
		}
	}
	if len(c.Allowlist) > 0 {
		// The allowlist primitive was added in catalog schema_version 1.1.0.
		// A catalog that uses it while still declaring a 1.0.x version loads
		// (the structural schema is shared across 1.x), but the version no
		// longer describes the contract the catalog relies on. Advise the bump.
		if _, minor, _, err := parseSemverCore(c.SchemaVersion); err == nil && minor < 1 {
			warnings = append(warnings,
				"catalog declares an allowlist but schema_version predates 1.1.0 (the allowlist primitive); declare schema_version 1.1.0")
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

// ToOutputAliases flattens the catalog's literal aliases and tokens into the form
// the output.Redactor expects. Variant expansion (slug, pluralize,
// path_segments) is NOT performed here — that lives in the engine,
// which builds the actual matcher. The redactor only needs the literal
// protected strings tagged with their entity ID.
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
		for _, a := range e.Tokens {
			if a == "" {
				continue
			}
			aliases = append(aliases, output.Alias{
				Pattern:         a,
				EntityID:        e.ID,
				CaseInsensitive: false,
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

// MergeAllowlist combines allowlist entries from multiple catalogs. Used at
// scan start so a single suppression pass (internal-brief) covers the merged set.
func MergeAllowlist(catalogs []*Catalog) []AllowlistEntry {
	var all []AllowlistEntry
	for _, c := range catalogs {
		if c == nil {
			continue
		}
		all = append(all, c.Allowlist...)
	}
	return all
}

// AllowlistOutputIDs returns the output-visible allowlist entry IDs across the
// given catalogs. The scan command validates these against the merged alias set
// (the same ID-safety gate entity/rule IDs pass) before emission.
func AllowlistOutputIDs(catalogs []*Catalog) []string {
	var ids []string
	for _, c := range catalogs {
		if c == nil {
			continue
		}
		for _, a := range c.Allowlist {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

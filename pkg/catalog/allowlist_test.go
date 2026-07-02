package catalog

import (
	"strings"
	"testing"
)

const baseAllowlistCatalog = `$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-1
    class: codename
    aliases: ["Tilden"]
`

func TestAllowlistLoadsAndParses(t *testing.T) {
	c, err := LoadBytes([]byte(baseAllowlistCatalog + `allowlist:
  - id: al-tool
    kind: literal
    pattern: "goneat"
    variants: { case_insensitive: true, whole_word: true }
    reason: "public OSS tool"
  - id: al-host
    kind: regex
    pattern: "[a-z]+\\.example\\.dev"
`))
	if err != nil {
		t.Fatalf("expected valid allowlist catalog to load, got: %v", err)
	}
	if len(c.Allowlist) != 2 {
		t.Fatalf("allowlist len = %d, want 2", len(c.Allowlist))
	}
	lit := c.Allowlist[0]
	if lit.ID != "al-tool" || lit.Kind != AllowlistKindLiteral || lit.Pattern != "goneat" {
		t.Errorf("literal entry parsed wrong: %+v", lit)
	}
	if !lit.Variants.CaseInsensitive || !lit.Variants.WholeWord || !lit.Variants.WholeWordSet {
		t.Errorf("literal variants parsed wrong: %+v", lit.Variants)
	}
	if c.Allowlist[1].Kind != AllowlistKindRegex {
		t.Errorf("regex entry kind = %q, want regex", c.Allowlist[1].Kind)
	}
}

func TestAllowlistDuplicateIDRejected(t *testing.T) {
	_, err := LoadBytes([]byte(baseAllowlistCatalog + `allowlist:
  - id: al-dup
    kind: literal
    pattern: "alpha"
  - id: al-dup
    kind: literal
    pattern: "beta"
`))
	if err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("expected duplicate id error, got: %v", err)
	}
}

func TestAllowlistBadRegexRejectedValueFree(t *testing.T) {
	_, err := LoadBytes([]byte(baseAllowlistCatalog + `allowlist:
  - id: al-bad
    kind: regex
    pattern: "(unclosed-SECRET"
`))
	if err == nil {
		t.Fatal("expected invalid regex to be rejected")
	}
	// Zero-leak: the offending pattern must not appear in the diagnostic.
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "unclosed") {
		t.Errorf("regex error leaked the pattern: %v", err)
	}
}

func TestAllowlistIDAliasSafetyEnforced(t *testing.T) {
	// The allowlist id embeds the protected alias "Tilden" — must be rejected.
	_, err := LoadBytes([]byte(baseAllowlistCatalog + `allowlist:
  - id: al-Tilden-public
    kind: literal
    pattern: "alpha"
`))
	if err == nil || !strings.Contains(err.Error(), "protected alias substring") {
		t.Fatalf("expected alias-safety rejection of allowlist id, got: %v", err)
	}
}

func TestAllowlistUnknownVariantRejected(t *testing.T) {
	// slug is an entity variant, not an allowlist variant — the schema's closed
	// allowlistVariants must reject it.
	_, err := LoadBytes([]byte(baseAllowlistCatalog + `allowlist:
  - id: al-x
    kind: literal
    pattern: "alpha"
    variants: { slug: true }
`))
	if err == nil {
		t.Fatal("expected schema to reject an unknown allowlist variant flag")
	}
}

func TestAllowlistMissingPatternRejected(t *testing.T) {
	_, err := LoadBytes([]byte(baseAllowlistCatalog + `allowlist:
  - id: al-x
    kind: literal
`))
	if err == nil {
		t.Fatal("expected missing pattern to be rejected")
	}
}

func TestAllowlistSchemaVersionAdvisory(t *testing.T) {
	// An allowlist on a 1.0.x catalog loads but warns to declare 1.1.0.
	src := strings.Replace(baseAllowlistCatalog, `schema_version: "1.1.0"`, `schema_version: "1.0.0"`, 1)
	c, err := LoadBytes([]byte(src + `allowlist:
  - id: al-x
    kind: literal
    pattern: "alpha"
`))
	if err != nil {
		t.Fatalf("1.0.0 catalog with allowlist should load (with a warning), got: %v", err)
	}
	found := false
	for _, w := range c.Warnings {
		if strings.Contains(w, "1.1.0") && strings.Contains(w, "allowlist") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an allowlist schema-version advisory warning, got: %v", c.Warnings)
	}
}

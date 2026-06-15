package catalog

import (
	"strings"
	"testing"
)

func TestLoadBytes_MinimalValid(t *testing.T) {
	yaml := `
catalog_id: test-catalog
schema_version: "1.0.0"
entities:
  - id: e-1
    class: client_identity
    aliases: ["acme"]
`
	c, err := LoadBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.CatalogID != "test-catalog" {
		t.Errorf("catalog_id: got %q", c.CatalogID)
	}
	if len(c.Entities) != 1 {
		t.Fatalf("expected 1 entity; got %d", len(c.Entities))
	}
	if c.Entities[0].ID != "e-1" {
		t.Errorf("entity[0].id: got %q", c.Entities[0].ID)
	}
}

func TestLoadBytes_TokenOnlyEntityValid(t *testing.T) {
	yaml := `
catalog_id: token-test
schema_version: "1.0.0"
entities:
  - id: e-token-1
    class: operational_pattern
    tokens: ["ILT"]
`
	c, err := LoadBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := c.Entities[0].Tokens; len(got) != 1 || got[0] != "ILT" {
		t.Fatalf("tokens = %#v, want [ILT]", got)
	}
}

func TestLoadBytes_WholeWordVariantTracksExplicitPresence(t *testing.T) {
	yaml := `
catalog_id: test-catalog
schema_version: "1.0.0"
entities:
  - id: e-1
    class: operational_pattern
    aliases: ["ILT"]
    variants:
      whole_word: false
  - id: e-2
    class: operational_pattern
    aliases: ["CBT"]
`
	c, err := LoadBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.Entities[0].Variants.WholeWordSet {
		t.Fatal("expected explicit whole_word=false to be tracked")
	}
	if c.Entities[0].Variants.WholeWord {
		t.Fatal("expected whole_word=false")
	}
	if c.Entities[1].Variants.WholeWordSet {
		t.Fatal("expected absent whole_word to remain unset")
	}
}

func TestLoadBytes_WholeWordCaseInsensitiveWarning(t *testing.T) {
	yaml := `
catalog_id: test-catalog
schema_version: "1.0.0"
entities:
  - id: e-1
    class: operational_pattern
    aliases: ["ILT"]
    variants:
      whole_word: true
      case_insensitive: true
`
	c, err := LoadBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Warnings) != 1 {
		t.Fatalf("warnings = %d, want 1 (%#v)", len(c.Warnings), c.Warnings)
	}
	if strings.Contains(c.Warnings[0], "e-1") ||
		!strings.Contains(c.Warnings[0], "whole_word=true + case_insensitive=true") {
		t.Fatalf("unexpected warning: %q", c.Warnings[0])
	}
}

func TestLoadBytes_ShortAliasAutoWholeWordWarning(t *testing.T) {
	yaml := `
catalog_id: test-catalog
schema_version: "1.0.0"
entities:
  - id: e-1
    class: operational_pattern
    aliases: ["ILT"]
    variants:
      case_insensitive: true
`
	c, err := LoadBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Warnings) != 1 {
		t.Fatalf("warnings = %d, want 1 (%#v)", len(c.Warnings), c.Warnings)
	}
	if strings.Contains(c.Warnings[0], "e-1") ||
		!strings.Contains(c.Warnings[0], "whole_word=true + case_insensitive=true") {
		t.Fatalf("unexpected warning: %q", c.Warnings[0])
	}
}

func TestLoadBytes_NoWarningWithoutWholeWordCaseInsensitivePairing(t *testing.T) {
	yaml := `
catalog_id: test-catalog
schema_version: "1.0.0"
entities:
  - id: e-1
    class: operational_pattern
    aliases: ["LONG"]
    variants:
      case_insensitive: true
  - id: e-2
    class: operational_pattern
    aliases: ["ILT"]
    variants:
      case_insensitive: true
      whole_word: false
`
	c, err := LoadBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", c.Warnings)
	}
}

func TestLoadBytes_MissingCatalogID(t *testing.T) {
	yaml := `
schema_version: "1.0.0"
entities:
  - {id: e-1, class: client_identity, aliases: [acme]}
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "catalog_id") {
		t.Errorf("expected catalog_id error, got: %v", err)
	}
}

func TestLoadBytes_NoEntities(t *testing.T) {
	yaml := `
catalog_id: empty
schema_version: "1.0.0"
entities: []
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "entity") {
		t.Errorf("expected entity error, got: %v", err)
	}
}

func TestLoadBytes_DuplicateEntityID(t *testing.T) {
	yaml := `
catalog_id: dup
schema_version: "1.0.0"
entities:
  - {id: e-1, class: client_identity, aliases: [a]}
  - {id: e-1, class: codename, aliases: [b]}
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}

func TestLoadBytes_EntityIDContainingAliasRejected(t *testing.T) {
	yaml := `
catalog_id: unsafe
schema_version: "1.0.0"
entities:
  - id: e-acme-1
    class: client_identity
    aliases: [acme]
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected alias-bearing entity id to be rejected")
	}
	if strings.Contains(err.Error(), "e-acme-1") || strings.Contains(err.Error(), "acme") {
		t.Fatalf("validation error leaked unsafe id or alias: %v", err)
	}
	if !strings.Contains(err.Error(), "protected alias substring") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadBytes_EntityIDContainingCaseInsensitiveAliasRejected(t *testing.T) {
	yaml := `
catalog_id: unsafe
schema_version: "1.0.0"
entities:
  - id: e-acme-1
    class: client_identity
    aliases: [Acme]
    variants:
      case_insensitive: true
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected case-insensitive alias-bearing entity id to be rejected")
	}
	if strings.Contains(err.Error(), "e-acme-1") || strings.Contains(err.Error(), "Acme") {
		t.Fatalf("validation error leaked unsafe id or alias: %v", err)
	}
	if !strings.Contains(err.Error(), "protected alias substring") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadBytes_EntityWithoutAliasesOrRegex(t *testing.T) {
	yaml := `
catalog_id: bare
schema_version: "1.0.0"
entities:
  - {id: e-1, class: codename}
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "alias") {
		t.Errorf("expected alias error, got: %v", err)
	}
}

func TestLoadBytes_EntityIDContainingTokenRejected(t *testing.T) {
	yaml := `
catalog_id: unsafe
schema_version: "1.0.0"
entities:
  - id: e-ILT-1
    class: operational_pattern
    tokens: [ILT]
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected token-bearing entity id to be rejected")
	}
	if strings.Contains(err.Error(), "e-ILT-1") || strings.Contains(err.Error(), "ILT") {
		t.Fatalf("validation error leaked unsafe id or token: %v", err)
	}
	if !strings.Contains(err.Error(), "protected alias substring") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadBytes_UnsafeEntityIDWithMissingClassDoesNotLeak(t *testing.T) {
	yaml := `
catalog_id: malformed
schema_version: "1.0.0"
entities:
  - id: e-acme-1
    aliases: [Acme]
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected missing class error")
	}
	for _, leak := range []string{"e-acme-1", "Acme", "acme"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("validation error leaked %q: %v", leak, err)
		}
	}
	if !strings.Contains(err.Error(), "class is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadBytes_UnsafeDuplicateEntityIDDoesNotLeak(t *testing.T) {
	yaml := `
catalog_id: malformed
schema_version: "1.0.0"
entities:
  - id: e-acme-1
    class: client_identity
    aliases: [SafeOne]
  - id: e-acme-1
    class: codename
    aliases: [SafeTwo]
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected duplicate id error")
	}
	for _, leak := range []string{"e-acme-1", "acme"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("validation error leaked %q: %v", leak, err)
		}
	}
	if !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadBytes_CoOccurrenceUnknownTerm(t *testing.T) {
	yaml := `
catalog_id: co
schema_version: "1.0.0"
entities:
  - {id: e-1, class: client_identity, aliases: [a]}
co_occurrence_rules:
  - rule_id: r-1
    terms: [e-1, e-9]
    window_kind: file
    severity_override: critical
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown term") {
		t.Errorf("expected unknown term error, got: %v", err)
	}
}

func TestLoadBytes_CoOccurrenceRequiresTwoTerms(t *testing.T) {
	yaml := `
catalog_id: co
schema_version: "1.0.0"
entities:
  - {id: e-1, class: client_identity, aliases: [a]}
co_occurrence_rules:
  - rule_id: r-1
    terms: [e-1]
    window_kind: file
    severity_override: critical
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "2 terms") {
		t.Errorf("expected two-terms error, got: %v", err)
	}
}

// Loads the actual synthetic-acme tier-2 catalog from testdata. This
// is the v0 spike's primary fixture; the loader must accept it
// unchanged.
func TestLoadFile_SyntheticAcmePrivate(t *testing.T) {
	c, err := LoadFile("../../testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if c.CatalogID != "cs-spike-private-v0" {
		t.Errorf("catalog_id: got %q, want cs-spike-private-v0", c.CatalogID)
	}
	if len(c.Entities) != 4 {
		t.Errorf("expected 4 entities; got %d", len(c.Entities))
	}
	if len(c.CoOccurrenceRules) != 1 {
		t.Errorf("expected 1 co-occurrence rule; got %d", len(c.CoOccurrenceRules))
	}

	// Spot-check entity IDs match the opaque-naming convention
	wantIDs := map[string]bool{"e-client-1": false, "e-codename-1": false, "e-codename-2": false, "e-format-1": false}
	for _, e := range c.Entities {
		if _, ok := wantIDs[e.ID]; ok {
			wantIDs[e.ID] = true
		} else {
			t.Errorf("unexpected entity id: %s", e.ID)
		}
	}
	for id, found := range wantIDs {
		if !found {
			t.Errorf("expected entity id %s not found", id)
		}
	}
}

func TestLoadFile_SyntheticAcmePublic(t *testing.T) {
	c, err := LoadFile("../../testdata/synthetic-acme/catalog/synthetic-acme-public.catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if c.CatalogID != "cs-spike-public-v0" {
		t.Errorf("catalog_id: got %q, want cs-spike-public-v0", c.CatalogID)
	}
	if len(c.Entities) != 3 {
		t.Errorf("expected 3 entities; got %d", len(c.Entities))
	}
}

func TestCatalog_ToOutputAliases(t *testing.T) {
	c, err := LoadFile("../../testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}

	aliases := c.ToOutputAliases()
	if len(aliases) == 0 {
		t.Fatal("expected aliases; got 0")
	}

	// Verify entity IDs are propagated correctly
	idSet := map[string]bool{}
	for _, a := range aliases {
		idSet[a.EntityID] = true
		if a.Pattern == "" {
			t.Errorf("empty pattern in alias for entity %s", a.EntityID)
		}
	}
	for _, want := range []string{"e-client-1", "e-codename-1", "e-codename-2"} {
		if !idSet[want] {
			t.Errorf("expected entity %s in alias output; not found", want)
		}
	}

	// Most aliases from synthetic-acme.catalog.yaml have
	// variants.case_insensitive: true; e-format-1 is the whole-word,
	// case-sensitive short-acronym regression fixture.
	for _, a := range aliases {
		if a.EntityID != "e-format-1" && !a.CaseInsensitive {
			t.Errorf("expected CaseInsensitive=true for synthetic-acme alias %q", a.Pattern)
		}
	}
}

func TestCatalog_ToOutputAliasesIncludesTokens(t *testing.T) {
	c, err := LoadBytes([]byte(`
catalog_id: token-test
schema_version: "1.0.0"
entities:
  - id: e-token-1
    class: operational_pattern
    tokens: ["ILT"]
`))
	if err != nil {
		t.Fatal(err)
	}
	aliases := c.ToOutputAliases()
	if len(aliases) != 1 {
		t.Fatalf("aliases = %#v, want one token alias", aliases)
	}
	if aliases[0].Pattern != "ILT" || aliases[0].EntityID != "e-token-1" || aliases[0].CaseInsensitive {
		t.Fatalf("unexpected token alias: %#v", aliases[0])
	}
}

func TestMergeAliases_TwoCatalogs(t *testing.T) {
	priv, err := LoadFile("../../testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	pub, err := LoadFile("../../testdata/synthetic-acme/catalog/synthetic-acme-public.catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}

	merged := MergeAliases([]*Catalog{priv, pub})
	if len(merged) < len(priv.ToOutputAliases()) {
		t.Errorf("merged should contain all private aliases; got %d, private has %d",
			len(merged), len(priv.ToOutputAliases()))
	}
}

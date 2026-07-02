package engine

import (
	"testing"

	"github.com/fulmenhq/limensafe/pkg/catalog"
	"github.com/fulmenhq/limensafe/pkg/extractor"
)

// allowlistScanner builds a scanner from an inline catalog so each allowlist
// test states exactly the entities + allowlist it exercises.
func allowlistScanner(t *testing.T, yamlSrc string) *Scanner {
	t.Helper()
	c, err := catalog.LoadBytes([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("load inline catalog: %v", err)
	}
	s, err := NewScanner([]*catalog.Catalog{c}, "public_oss")
	if err != nil {
		t.Fatalf("new scanner: %v", err)
	}
	return s
}

func contentUnit(content string) extractor.InputUnit {
	return extractor.InputUnit{
		SourceID:   "docs/readme.md",
		SourceKind: "file",
		Content:    []byte(content),
		Encoding:   "utf-8",
	}
}

func entityIDs(findings []Finding) map[string]bool {
	out := map[string]bool{}
	for _, f := range findings {
		out[f.EntityID] = true
	}
	return out
}

// TestAllowlistLiteralBeatsEntityMatch is the keystone: an allowlist literal
// whose span covers an entity match suppresses the finding and records a
// reconcilable suppression instead.
func TestAllowlistLiteralBeatsEntityMatch(t *testing.T) {
	s := allowlistScanner(t, `
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-tool
    class: codename
    aliases: ["goneat"]
    variants: { whole_word: true }
allowlist:
  - id: al-public-tool
    kind: literal
    pattern: "goneat"
    variants: { whole_word: true }
    reason: "public OSS tool"
`)
	res := s.ScanUnitResult(contentUnit("we use goneat daily\n"))
	if len(res.Findings) != 0 {
		t.Fatalf("expected 0 findings (allowlist subtracts the match), got %#v", res.Findings)
	}
	if len(res.Suppressions) != 1 {
		t.Fatalf("expected 1 suppression, got %#v", res.Suppressions)
	}
	sup := res.Suppressions[0]
	if sup.AllowlistID != "al-public-tool" || sup.EntityID != "e-tool" || sup.Surface != SurfaceContent {
		t.Fatalf("suppression = %+v, want al-public-tool/e-tool/content", sup)
	}
}

// TestAllowlistRegexSuppresses covers regex allowlist entries.
func TestAllowlistRegexSuppresses(t *testing.T) {
	s := allowlistScanner(t, `
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-host
    class: hostname
    aliases: ["api.example.dev"]
allowlist:
  - id: al-public-host
    kind: regex
    pattern: "[a-z]+\\.example\\.dev"
`)
	res := s.ScanUnitResult(contentUnit("reach api.example.dev now\n"))
	if len(res.Findings) != 0 {
		t.Fatalf("expected regex allowlist to suppress, got %#v", res.Findings)
	}
	if len(res.Suppressions) != 1 || res.Suppressions[0].AllowlistID != "al-public-host" {
		t.Fatalf("expected 1 suppression by al-public-host, got %#v", res.Suppressions)
	}
}

// TestAllowlistRegexCaseInsensitive proves a regex allowlist entry honors the
// case_insensitive variant (devrev P2): "public-[a-z]+" with case_insensitive
// suppresses "PUBLIC-TOOL", and without it the same regex does not.
func TestAllowlistRegexCaseInsensitive(t *testing.T) {
	base := `
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-tool
    class: codename
    aliases: ["PUBLIC-TOOL"]
allowlist:
  - id: al-ci-regex
    kind: regex
    pattern: "public-[a-z]+"
`
	// With case_insensitive → the regex folds case and suppresses.
	ci := allowlistScanner(t, base+"    variants: { case_insensitive: true }\n")
	res := ci.ScanUnitResult(contentUnit("we ship PUBLIC-TOOL today\n"))
	if len(res.Findings) != 0 {
		t.Fatalf("case_insensitive regex should suppress PUBLIC-TOOL, got %#v", res.Findings)
	}
	if len(res.Suppressions) != 1 || res.Suppressions[0].AllowlistID != "al-ci-regex" {
		t.Fatalf("expected 1 suppression by al-ci-regex, got %#v", res.Suppressions)
	}

	// Control: without the flag, the case-sensitive regex does not match the
	// uppercase form, so the entity flags — proving the flag did the work.
	noci := allowlistScanner(t, base)
	res2 := noci.ScanUnitResult(contentUnit("we ship PUBLIC-TOOL today\n"))
	if !entityIDs(res2.Findings)["e-tool"] {
		t.Errorf("without case_insensitive the regex must not suppress; want e-tool flagged, got %#v", res2.Findings)
	}
}

// TestAllowlistRegexWholeWord proves a regex allowlist entry honors the
// whole_word variant (devrev P2): with whole_word the match must be
// boundary-bounded, so a match embedded inside a larger word does not suppress.
func TestAllowlistRegexWholeWord(t *testing.T) {
	base := `
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-host
    class: hostname
    aliases: ["api.dev"]
allowlist:
  - id: al-ww-regex
    kind: regex
    pattern: "[a-z]+\\.dev"
`
	// Embedded in a larger word ("xapi.devy"): with whole_word the allowlist
	// span is rejected (its end is not a word boundary), so the entity flags.
	ww := allowlistScanner(t, base+"    variants: { whole_word: true }\n")
	res := ww.ScanUnitResult(contentUnit("host xapi.devy here\n"))
	if !entityIDs(res.Findings)["e-host"] {
		t.Fatalf("whole_word regex must not suppress an embedded match; want e-host flagged, got %#v", res.Findings)
	}

	// Control: without whole_word, the same regex span covers the entity match
	// and suppresses it.
	noww := allowlistScanner(t, base)
	res2 := noww.ScanUnitResult(contentUnit("host xapi.devy here\n"))
	if len(res2.Findings) != 0 {
		t.Errorf("without whole_word the regex should suppress the embedded match, got %#v", res2.Findings)
	}
}

// TestAllowlistReferenceVsDisclosure is the brief's headline case: allowlist the
// literal filename so legitimate *mentions* of it are not flagged, while the
// body content (a different entity) is still caught.
func TestAllowlistReferenceVsDisclosure(t *testing.T) {
	s := allowlistScanner(t, `
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-fileref
    class: system
    aliases: ["AGENTS.local.md"]
  - id: e-secret
    class: client_identity
    aliases: ["super-secret-client"]
allowlist:
  - id: al-filename
    kind: literal
    pattern: "AGENTS.local.md"
    reason: "filename mention is safe; its contents are caught by other entities"
`)
	res := s.ScanUnitResult(contentUnit("see AGENTS.local.md; client super-secret-client\n"))
	ids := entityIDs(res.Findings)
	if ids["e-fileref"] {
		t.Errorf("filename mention should be allowlisted, but e-fileref flagged: %#v", res.Findings)
	}
	if !ids["e-secret"] {
		t.Errorf("body content (e-secret) must still flag despite the filename allowlist: %#v", res.Findings)
	}
	if len(res.Suppressions) != 1 || res.Suppressions[0].EntityID != "e-fileref" {
		t.Fatalf("expected 1 suppression for e-fileref, got %#v", res.Suppressions)
	}
}

// TestAllowlistPartialOverlapDoesNotSuppress locks the full-span coverage
// contract: an allowlist span that covers only part of the entity match does
// not suppress.
func TestAllowlistPartialOverlapDoesNotSuppress(t *testing.T) {
	s := allowlistScanner(t, `
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-1
    class: codename
    aliases: ["acmecorp"]
allowlist:
  - id: al-partial
    kind: literal
    pattern: "acme"
    variants: { whole_word: false }
`)
	res := s.ScanUnitResult(contentUnit("acmecorp leak\n"))
	if len(res.Findings) != 1 || res.Findings[0].EntityID != "e-1" {
		t.Fatalf("partial overlap must not suppress; want 1 finding for e-1, got %#v", res.Findings)
	}
	if len(res.Suppressions) != 0 {
		t.Fatalf("expected no suppressions on partial overlap, got %#v", res.Suppressions)
	}
}

// TestAllowlistSubtractThenMatch proves the "subtract, then match" semantics:
// an allowlisted occurrence does not mask a later genuine occurrence of the
// same entity.
func TestAllowlistSubtractThenMatch(t *testing.T) {
	s := allowlistScanner(t, `
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-tool
    class: codename
    aliases: ["acme"]
    variants: { whole_word: true }
allowlist:
  - id: al-prefixed
    kind: regex
    pattern: "ok-acme"
`)
	res := s.ScanUnitResult(contentUnit("ok-acme then real acme here\n"))
	if len(res.Findings) != 1 || res.Findings[0].EntityID != "e-tool" {
		t.Fatalf("expected the un-allowlisted second occurrence to flag, got %#v", res.Findings)
	}
	// The finding must point at the second occurrence (column 19), not the
	// allowlisted first one.
	if res.Findings[0].Column != 19 {
		t.Errorf("finding column = %d, want 19 (the non-allowlisted occurrence)", res.Findings[0].Column)
	}
	if len(res.Suppressions) != 0 {
		t.Fatalf("entity produced a finding, so no suppression should be recorded, got %#v", res.Suppressions)
	}
}

// TestAllowlistSubtractsCoOccurrenceConstituent verifies allowlist subtraction
// happens before co-occurrence evaluation: allowlisting one constituent means
// the rule never fires.
func TestAllowlistSubtractsCoOccurrenceConstituent(t *testing.T) {
	s := allowlistScanner(t, `
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-client
    class: client_identity
    aliases: ["Acme"]
    variants: { case_insensitive: true }
  - id: e-codename
    class: codename
    aliases: ["Horizon"]
    variants: { case_insensitive: true }
co_occurrence_rules:
  - rule_id: r-co
    description: "client + codename together"
    terms: [e-client, e-codename]
    window_kind: file
    severity_override: critical
allowlist:
  - id: al-client
    kind: literal
    pattern: "Acme"
    variants: { case_insensitive: true }
`)
	res := s.ScanUnitResult(contentUnit("Acme ships Horizon\n"))
	for _, f := range res.Findings {
		if f.DetectorID == DetectorCoOccurrence {
			t.Fatalf("co-occurrence must not fire when a constituent is allowlisted, got %#v", f)
		}
		if f.EntityID == "e-client" {
			t.Fatalf("e-client should be allowlisted, got %#v", f)
		}
	}
	if !entityIDs(res.Findings)["e-codename"] {
		t.Errorf("e-codename should still flag, got %#v", res.Findings)
	}
	if len(res.Suppressions) != 1 || res.Suppressions[0].EntityID != "e-client" {
		t.Fatalf("expected 1 suppression for e-client, got %#v", res.Suppressions)
	}
}

// TestAllowlistSuppressesPathSurface covers allowlist subtraction on the path
// surface (a path-segment finding the allowlist covers).
func TestAllowlistSuppressesPathSurface(t *testing.T) {
	s := allowlistScanner(t, `
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-client
    class: client_identity
    aliases: ["acme"]
    variants: { case_insensitive: true }
allowlist:
  - id: al-path
    kind: literal
    pattern: "acme"
    variants: { case_insensitive: true }
`)
	res := s.ScanUnitResult(extractor.InputUnit{
		SourceID:   "internal/acme/data.go",
		SourceKind: "file",
		Content:    []byte("package data\n"),
		Encoding:   "utf-8",
	})
	if len(res.Findings) != 0 {
		t.Fatalf("expected path finding to be suppressed, got %#v", res.Findings)
	}
	if len(res.Suppressions) != 1 || res.Suppressions[0].Surface != SurfacePath {
		t.Fatalf("expected 1 path suppression, got %#v", res.Suppressions)
	}
}

// TestAllowlistWorkedExampleFixture loads the committed synthetic worked-example
// catalog and proves the reference-vs-disclosure split end-to-end through the
// engine, keeping the fixture honest.
func TestAllowlistWorkedExampleFixture(t *testing.T) {
	c, err := catalog.LoadFile("../../testdata/synthetic-acme/catalog/synthetic-acme-allowlist.catalog.yaml")
	if err != nil {
		t.Fatalf("load worked-example fixture: %v", err)
	}
	s, err := NewScanner([]*catalog.Catalog{c}, "public_oss")
	if err != nil {
		t.Fatalf("new scanner: %v", err)
	}
	res := s.ScanUnitResult(contentUnit("See AGENTS.local.md for the Acme Corp engagement.\n"))
	ids := entityIDs(res.Findings)
	if ids["e-fileref"] {
		t.Errorf("filename reference should be allowlisted; e-fileref flagged: %#v", res.Findings)
	}
	if !ids["e-client-1"] {
		t.Errorf("client identity in the body must still flag: %#v", res.Findings)
	}
	if len(res.Suppressions) != 1 || res.Suppressions[0].AllowlistID != "al-agents-local-filename" {
		t.Fatalf("expected 1 suppression by al-agents-local-filename, got %#v", res.Suppressions)
	}
}

// TestAllowlistNoCatalogAllowlistIsNoop confirms catalogs without an allowlist
// behave exactly as before (no suppressions, findings unchanged).
func TestAllowlistAbsentIsNoop(t *testing.T) {
	s := syntheticScanner(t)
	res := s.ScanUnitResult(extractor.InputUnit{
		SourceID:   "internal/cmd/profile_test.go",
		SourceKind: "file",
		Content:    []byte("profile acme-dev\n"),
		Encoding:   "utf-8",
	})
	if len(res.Findings) == 0 {
		t.Fatal("expected findings with no allowlist")
	}
	if len(res.Suppressions) != 0 {
		t.Fatalf("no allowlist means no suppressions, got %#v", res.Suppressions)
	}
}

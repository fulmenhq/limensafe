package catalog

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// errReader fails on first Read with a caller-controlled message, standing in
// for a custom io.Reader whose error could carry a path or protected content.
type errReader struct{ msg string }

func (e errReader) Read([]byte) (int, error) { return 0, errors.New(e.msg) }

// TestBuildCatalogFromTermList_ReaderErrorIsRedactionSafe: BuildCatalogFromTermList
// is exported, so an arbitrary reader error must not be echoed verbatim — it
// could carry protected content (ADR-0003).
func TestBuildCatalogFromTermList_ReaderErrorIsRedactionSafe(t *testing.T) {
	const protected = "SUPERSECRETCODENAME"
	_, err := BuildCatalogFromTermList(errReader{msg: protected + " device boom"}, TermListOptions{CatalogID: "tl-rerr"})
	if err == nil {
		t.Fatal("expected a reader error")
	}
	if strings.Contains(err.Error(), protected) {
		t.Fatalf("reader error leaked content: %v", err)
	}
}

// TestBuildCatalogFromTermList_GoldenWorkedExample rebuilds the committed
// synthetic worked-example catalog from its term-list and asserts it is
// semantically equal to the golden file (compared through the loader, so
// the comparison is immune to YAML cosmetic formatting — the committed
// golden is goneat-formatted, which differs from yaml.Marshal indentation).
// Byte-level determinism of the builder itself is pinned separately by
// TestBuildCatalogFromTermList_Idempotent. Regenerate the golden after an
// intentional change with:
//
//	limensafe catalog build \
//	  --from-termlist testdata/synthetic-acme/termlist/synthetic-acme.termlist.txt \
//	  --out testdata/synthetic-acme/termlist/synthetic-acme.from-termlist.catalog.yaml \
//	  --catalog-id cs-synthetic-acme-from-termlist --default-severity medium
//
// then run `make fmt` so the committed file carries repo formatting.
func TestBuildCatalogFromTermList_GoldenWorkedExample(t *testing.T) {
	const dir = "../../testdata/synthetic-acme/termlist/"
	src, err := os.ReadFile(dir + "synthetic-acme.termlist.txt")
	if err != nil {
		t.Fatalf("read term-list fixture: %v", err)
	}
	goldenBytes, err := os.ReadFile(dir + "synthetic-acme.from-termlist.catalog.yaml")
	if err != nil {
		t.Fatalf("read golden catalog: %v", err)
	}
	gotBytes, err := BuildCatalogFromTermList(strings.NewReader(string(src)), TermListOptions{
		CatalogID:       "cs-synthetic-acme-from-termlist",
		DefaultSeverity: "medium",
	})
	if err != nil {
		t.Fatalf("build worked-example catalog: %v", err)
	}
	golden, err := LoadBytes(goldenBytes)
	if err != nil {
		t.Fatalf("golden catalog failed to load: %v", err)
	}
	got, err := LoadBytes(gotBytes)
	if err != nil {
		t.Fatalf("generated catalog failed to load: %v", err)
	}
	if !reflect.DeepEqual(got, golden) {
		t.Fatalf("generated catalog differs from golden fixture; regenerate per the doc comment.\n--- got ---\n%s", gotBytes)
	}
}

func buildTermList(t *testing.T, src string, opts TermListOptions) []byte {
	t.Helper()
	if opts.CatalogID == "" {
		opts.CatalogID = "tl-test"
	}
	data, err := BuildCatalogFromTermList(strings.NewReader(src), opts)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	return data
}

// TestBuildCatalogFromTermList_GroupsAndValidates is the happy path: synthetic
// terms group by replacement, the output loads + schema-validates, and ids are
// the opaque hash-derived form.
func TestBuildCatalogFromTermList_GroupsAndValidates(t *testing.T) {
	src := `
# synthetic term-list (acme/horizon vocabulary)
AcmeCorp==>ClientAlpha
Acme==>ClientAlpha
Horizon==>ProjectBeta
`
	data := buildTermList(t, src, TermListOptions{CatalogID: "tl-synth"})

	c, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("generated catalog failed to load: %v", err)
	}
	// AcmeCorp + Acme share replacement ClientAlpha -> one entity, two aliases.
	if len(c.Entities) != 2 {
		t.Fatalf("entities = %d, want 2 (one per replacement group)", len(c.Entities))
	}
	for _, e := range c.Entities {
		if !strings.HasPrefix(e.ID, "e-tl-") {
			t.Fatalf("entity id %q is not the opaque hash form", e.ID)
		}
		if !e.Variants.WholeWord {
			t.Fatalf("expected whole_word default on entity %s", e.ID)
		}
	}
	var clientAlpha *Entity
	for i := range c.Entities {
		if c.Entities[i].ReplacementSuggestion == "ClientAlpha" {
			clientAlpha = &c.Entities[i]
		}
	}
	if clientAlpha == nil || len(clientAlpha.Aliases) != 2 {
		t.Fatalf("ClientAlpha group should have 2 aliases, got %+v", clientAlpha)
	}
}

func TestBuildCatalogFromTermList_LiteralOnlyByteCompatible(t *testing.T) {
	src := `AcmeCorp==>ClientAlpha  # class=client_identity severity=high
Acme==>ClientAlpha
Horizon==>ProjectBeta
Tilden==>PersonGamma  # class=person severity=critical
`
	got := buildTermList(t, src, TermListOptions{
		CatalogID:       "cs-synthetic-acme-from-termlist",
		DefaultSeverity: "medium",
	})
	const want = `$schema: https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json
catalog_id: cs-synthetic-acme-from-termlist
schema_version: 1.1.0
default_severity: medium
entities:
    - id: e-tl-4810b253545a
      class: client_identity
      aliases:
        - Acme
        - AcmeCorp
      variants:
        case_insensitive: true
        slug: true
        whole_word: true
      replacement_suggestion: ClientAlpha
      severity_override: high
    - id: e-tl-b1733246bc58
      class: codename
      aliases:
        - Horizon
      variants:
        case_insensitive: true
        slug: true
        whole_word: true
      replacement_suggestion: ProjectBeta
    - id: e-tl-e52562165584
      class: person
      aliases:
        - Tilden
      variants:
        case_insensitive: true
        slug: true
        whole_word: true
      replacement_suggestion: PersonGamma
      severity_override: critical
`
	if string(got) != want {
		t.Fatalf("literal-only output changed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBuildCatalogFromTermList_MixedLiteralRegexAndAllowlist(t *testing.T) {
	src := `
Acme==>ClientAlpha  # class=client_identity severity=high
regex:\bHRZN-[0-9]{4}\b  # class=operational_pattern severity=critical
allowlist:literal:HRZN-0000 # case_insensitive=true whole_word=true
allowlist:regex:\bHRZN-9[0-9]{3}\b # case_insensitive=true whole_word=true
`
	data := buildTermList(t, src, TermListOptions{CatalogID: "tl-structured"})
	c, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("generated structured catalog failed to load: %v\n%s", err, data)
	}
	if len(c.Entities) != 2 {
		t.Fatalf("entities = %d, want literal + regex", len(c.Entities))
	}
	var sawRegex bool
	for _, e := range c.Entities {
		if len(e.RegexPatterns) == 1 {
			sawRegex = true
			if e.Class != "operational_pattern" {
				t.Fatalf("regex class = %q, want operational_pattern", e.Class)
			}
			if e.SeverityOverride != "critical" {
				t.Fatalf("regex severity = %q, want critical", e.SeverityOverride)
			}
			if strings.Contains(e.ID, "HRZN") {
				t.Fatalf("regex entity id leaked pattern content: %q", e.ID)
			}
		}
	}
	if !sawRegex {
		t.Fatalf("missing regex entity in generated catalog: %+v", c.Entities)
	}
	if len(c.Allowlist) != 2 {
		t.Fatalf("allowlist len = %d, want 2", len(c.Allowlist))
	}
	for _, a := range c.Allowlist {
		if !strings.HasPrefix(a.ID, termListAllowlistIDPrefix) {
			t.Fatalf("allowlist id %q does not use structured-corpus prefix", a.ID)
		}
		if strings.Contains(a.ID, "HRZN") {
			t.Fatalf("allowlist id leaked pattern content: %q", a.ID)
		}
		if !a.Variants.CaseInsensitive || !a.Variants.WholeWord {
			t.Fatalf("allowlist variants not preserved: %+v", a)
		}
	}
}

func TestBuildCatalogFromTermList_PrefixLookingLiteralMappingsStayLiteral(t *testing.T) {
	src := `
regex:customer-id==>SafeReplacement
allowlist:literal:codename==>SafeReplacement
`
	data := buildTermList(t, src, TermListOptions{CatalogID: "tl-prefix-literals"})
	c, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("generated catalog failed to load: %v\n%s", err, data)
	}
	if len(c.Allowlist) != 0 {
		t.Fatalf("allowlist len = %d, want 0 for prefix-looking literal mappings", len(c.Allowlist))
	}
	if len(c.Entities) != 1 {
		t.Fatalf("entities = %d, want 1 literal replacement group", len(c.Entities))
	}
	e := c.Entities[0]
	if len(e.RegexPatterns) != 0 {
		t.Fatalf("regex_patterns = %v, want none for prefix-looking literal mappings", e.RegexPatterns)
	}
	if e.ReplacementSuggestion != "SafeReplacement" {
		t.Fatalf("replacement_suggestion = %q, want SafeReplacement", e.ReplacementSuggestion)
	}
	wantAliases := []string{"allowlist:literal:codename", "regex:customer-id"}
	if !reflect.DeepEqual(e.Aliases, wantAliases) {
		t.Fatalf("aliases = %v, want %v", e.Aliases, wantAliases)
	}
}

func TestBuildCatalogFromTermList_StructuredCorpusFixture(t *testing.T) {
	src, err := os.ReadFile("../../testdata/synthetic-acme/termlist/synthetic-acme.structured-corpus.txt")
	if err != nil {
		t.Fatalf("read structured corpus fixture: %v", err)
	}
	data := buildTermList(t, string(src), TermListOptions{
		CatalogID:       "cs-structured-corpus-demo",
		DefaultSeverity: "medium",
	})
	c, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("generated structured fixture catalog failed to load: %v\n%s", err, data)
	}
	if len(c.Entities) != 4 {
		t.Fatalf("entities = %d, want 4 (2 literal groups + 2 regex patterns)", len(c.Entities))
	}
	if len(c.Allowlist) != 2 {
		t.Fatalf("allowlist len = %d, want 2", len(c.Allowlist))
	}
}

// TestBuildCatalogFromTermList_Idempotent confirms byte-identical output on
// repeated builds (stable entity/alias ordering and ids).
func TestBuildCatalogFromTermList_Idempotent(t *testing.T) {
	src := `
Zeta==>ProjectOne
Acme==>ClientTwo
Horizon==>ClientTwo
`
	a := buildTermList(t, src, TermListOptions{CatalogID: "tl-idem"})
	b := buildTermList(t, src, TermListOptions{CatalogID: "tl-idem"})
	if string(a) != string(b) {
		t.Fatalf("output not byte-identical across builds:\n--a--\n%s\n--b--\n%s", a, b)
	}
}

// TestBuildCatalogFromTermList_DirectiveOverride exercises per-line class and
// severity overrides.
func TestBuildCatalogFromTermList_DirectiveOverride(t *testing.T) {
	src := `Acme==>ClientAlpha  # class=client_identity severity=critical`
	data := buildTermList(t, src, TermListOptions{CatalogID: "tl-dir"})
	c, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Entities[0].Class != "client_identity" {
		t.Fatalf("class = %q, want client_identity", c.Entities[0].Class)
	}
	if c.Entities[0].SeverityOverride != "critical" {
		t.Fatalf("severity_override = %q, want critical", c.Entities[0].SeverityOverride)
	}
}

// TestBuildCatalogFromTermList_DirectiveInheritedWithinGroup: a class/severity
// directive on one line of a replacement group applies to the whole group;
// undirected sibling lines inherit it rather than forcing the default and
// conflicting. Order-independent: the directive may sit on any line.
func TestBuildCatalogFromTermList_DirectiveInheritedWithinGroup(t *testing.T) {
	// Directive on the first line; the second (undirected) line inherits it.
	src := "AcmeCorp==>ClientAlpha  # class=client_identity severity=high\nAcme==>ClientAlpha\n"
	data := buildTermList(t, src, TermListOptions{CatalogID: "tl-inherit"})
	c, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(c.Entities) != 1 {
		t.Fatalf("entities = %d, want 1 (one replacement group)", len(c.Entities))
	}
	if c.Entities[0].Class != "client_identity" {
		t.Fatalf("class = %q, want client_identity inherited by the group", c.Entities[0].Class)
	}
	if c.Entities[0].SeverityOverride != "high" {
		t.Fatalf("severity_override = %q, want high inherited by the group", c.Entities[0].SeverityOverride)
	}
	if len(c.Entities[0].Aliases) != 2 {
		t.Fatalf("aliases = %d, want 2", len(c.Entities[0].Aliases))
	}
}

// TestBuildCatalogFromTermList_ErrorsAreRedactionSafe: a malformed line must
// fail by line number without echoing the protected content on the line.
func TestBuildCatalogFromTermList_ErrorsAreRedactionSafe(t *testing.T) {
	const protected = "SUPERSECRETCODENAME"
	src := "Acme==>ClientAlpha\n" + protected + " has no separator\n"
	_, err := BuildCatalogFromTermList(strings.NewReader(src), TermListOptions{CatalogID: "tl-bad"})
	if err == nil {
		t.Fatal("expected error on malformed line")
	}
	if strings.Contains(err.Error(), protected) || strings.Contains(err.Error(), strings.ToLower(protected)) {
		t.Fatalf("error leaked protected content: %v", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected line-numbered error, got: %v", err)
	}
}

// TestBuildCatalogFromTermList_ConflictingDirectiveErrors: the same replacement
// group cannot carry conflicting class directives.
func TestBuildCatalogFromTermList_ConflictingDirectiveErrors(t *testing.T) {
	src := "Acme==>ClientAlpha  # class=client_identity\nHorizon==>ClientAlpha  # class=codename\n"
	_, err := BuildCatalogFromTermList(strings.NewReader(src), TermListOptions{CatalogID: "tl-conflict"})
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected line-2 conflict error, got: %v", err)
	}
}

// TestBuildCatalogFromTermList_WholeWordDefaultsOn pins the catalog-B default:
// the zero-value option (no WholeWord field set) must generate whole_word: true.
func TestBuildCatalogFromTermList_WholeWordDefaultsOn(t *testing.T) {
	data := buildTermList(t, "Acme==>ClientAlpha\n", TermListOptions{CatalogID: "tl-ww-default"})
	c, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !c.Entities[0].Variants.WholeWord {
		t.Fatal("whole_word should default on when DisableWholeWord is unset")
	}
}

// TestBuildCatalogFromTermList_WholeWordOptOut exercises the explicit opt-out
// path (the --no-whole-word flag will set DisableWholeWord).
func TestBuildCatalogFromTermList_WholeWordOptOut(t *testing.T) {
	data := buildTermList(t, "Acme==>ClientAlpha\n", TermListOptions{CatalogID: "tl-ww-off", DisableWholeWord: true})
	c, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Entities[0].Variants.WholeWord {
		t.Fatal("whole_word should be off when DisableWholeWord is set")
	}
}

// TestBuildCatalogFromTermList_UnknownDirectiveKeyIsRedactionSafe: an unknown
// directive key is operator-controlled content and could itself be a protected
// token. The diagnostic must report the structural reason by line number and
// never echo the key (ADR-0003 applies to malformed-input diagnostics too).
func TestBuildCatalogFromTermList_UnknownDirectiveKeyIsRedactionSafe(t *testing.T) {
	const protected = "SUPERSECRETCODENAME"
	src := "Acme==>ClientAlpha  # " + protected + "=high\n"
	_, err := BuildCatalogFromTermList(strings.NewReader(src), TermListOptions{CatalogID: "tl-badkey"})
	if err == nil {
		t.Fatal("expected error on unknown directive key")
	}
	if strings.Contains(err.Error(), protected) || strings.Contains(err.Error(), strings.ToLower(protected)) {
		t.Fatalf("error leaked protected directive key: %v", err)
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("expected line-numbered error, got: %v", err)
	}
}

func TestBuildCatalogFromTermList_InvalidRegexIsRedactionSafe(t *testing.T) {
	const protected = "SUPERSECRETCODENAME"
	src := "regex:(" + protected + "\n"
	_, err := BuildCatalogFromTermList(strings.NewReader(src), TermListOptions{CatalogID: "tl-badrx"})
	if err == nil {
		t.Fatal("expected invalid regex error")
	}
	if strings.Contains(err.Error(), protected) || strings.Contains(err.Error(), strings.ToLower(protected)) {
		t.Fatalf("error leaked protected regex content: %v", err)
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("expected line-numbered diagnostic, got: %v", err)
	}
}

func TestBuildCatalogFromTermList_InvalidAllowlistRegexIsRedactionSafe(t *testing.T) {
	const protected = "SUPERSECRETCODENAME"
	src := "Acme==>ClientAlpha\nallowlist:regex:(" + protected + "\n"
	_, err := BuildCatalogFromTermList(strings.NewReader(src), TermListOptions{CatalogID: "tl-badalrx"})
	if err == nil {
		t.Fatal("expected invalid allowlist regex error")
	}
	if strings.Contains(err.Error(), protected) || strings.Contains(err.Error(), strings.ToLower(protected)) {
		t.Fatalf("error leaked protected allowlist regex content: %v", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected line-numbered diagnostic, got: %v", err)
	}
}

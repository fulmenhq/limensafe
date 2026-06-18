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

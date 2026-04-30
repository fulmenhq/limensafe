package output

import (
	"strings"
	"testing"
)

func TestRedactor_LiteralAlias(t *testing.T) {
	r, err := NewRedactor([]Alias{
		{Pattern: "acme", EntityID: "e-client-1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := r.Redact("scanning profile acme-dev")
	want := "scanning profile <r:e-client-1>-dev"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRedactor_MultipleAliases(t *testing.T) {
	r, err := NewRedactor([]Alias{
		{Pattern: "acme", EntityID: "e-client-1"},
		{Pattern: "horizon", EntityID: "e-codename-1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := r.Redact("acme uses horizon database")
	want := "<r:e-client-1> uses <r:e-codename-1> database"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRedactor_CaseInsensitive(t *testing.T) {
	r, err := NewRedactor([]Alias{
		{Pattern: "Acme", EntityID: "e-client-1", CaseInsensitive: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, in := range []string{"acme", "ACME", "Acme", "AcMe"} {
		got := r.Redact(in)
		if got != "<r:e-client-1>" {
			t.Errorf("input %q: got %q, want <r:e-client-1>", in, got)
		}
	}
}

func TestRedactor_NoMatch(t *testing.T) {
	r, err := NewRedactor([]Alias{
		{Pattern: "acme", EntityID: "e-client-1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := r.Redact("hello world")
	if got != "hello world" {
		t.Errorf("got %q, want unchanged", got)
	}
}

func TestRedactor_LongestWins(t *testing.T) {
	r, err := NewRedactor([]Alias{
		{Pattern: "acme", EntityID: "e-client-1"},
		{Pattern: "acme-corp", EntityID: "e-client-2"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := r.Redact("hello acme-corp world")
	want := "hello <r:e-client-2> world"
	if got != want {
		t.Errorf("got %q, want %q (longer pattern should win)", got, want)
	}
}

func TestRedactor_PathSegment(t *testing.T) {
	r, err := NewRedactor([]Alias{
		{Pattern: "acme", EntityID: "e-client-1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := r.Redact("internal/clients/acme/data.go")
	want := "internal/clients/<r:e-client-1>/data.go"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRedactor_RegexQuoteMetaSafety(t *testing.T) {
	// Aliases containing regex metacharacters must be treated as literals.
	r, err := NewRedactor([]Alias{
		{Pattern: "a.cme", EntityID: "e-client-1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := r.Redact("a.cme aXcme")
	// Only "a.cme" (the literal) should redact; "aXcme" must not match
	// (which it would if the dot were treated as regex).
	want := "<r:e-client-1> aXcme"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRedactor_NilRedactorPassthrough(t *testing.T) {
	var r *Redactor
	if got := r.Redact("anything"); got != "anything" {
		t.Errorf("nil redactor should pass through; got %q", got)
	}
}

func TestRedactor_EmptyAliasesError(t *testing.T) {
	_, err := NewRedactor(nil)
	if err == nil {
		t.Error("expected error for empty alias list")
	}
	_, err = NewRedactor([]Alias{{Pattern: "", EntityID: "e-1"}})
	if err == nil {
		t.Error("expected error when all patterns are empty")
	}
}

// Acceptance-style test: against the synthetic-acme alias set, verify
// the zero-leak invariant holds — protected substrings must not appear
// in any output. This is the core property the redactor exists to
// enforce; T3/T4/T5/T6 in v0-spike-plan.md depend on it.
func TestRedactor_ZeroLeak_SyntheticAcmeAliases(t *testing.T) {
	aliases := []Alias{
		{Pattern: "Acme", EntityID: "e-client-1", CaseInsensitive: true},
		{Pattern: "Acme Corp", EntityID: "e-client-1", CaseInsensitive: true},
		{Pattern: "AcmeCorp", EntityID: "e-client-1", CaseInsensitive: true},
		{Pattern: "acme-corp", EntityID: "e-client-1", CaseInsensitive: true},
		{Pattern: "horizon", EntityID: "e-codename-1", CaseInsensitive: true},
		{Pattern: "project-horizon", EntityID: "e-codename-1", CaseInsensitive: true},
		{Pattern: "HorizonDB", EntityID: "e-codename-1", CaseInsensitive: true},
		{Pattern: "tilden", EntityID: "e-codename-2", CaseInsensitive: true},
		{Pattern: "tilden-svc", EntityID: "e-codename-2", CaseInsensitive: true},
		{Pattern: "tilden_db", EntityID: "e-codename-2", CaseInsensitive: true},
	}
	r, err := NewRedactor(aliases)
	if err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"Acme Corp uses horizon database",
		"scanning profile acme-dev",
		"acme-horizon-dev triangulation",
		"feat/acme-redash-fix",
		"tilden-svc deployed to tilden_db cluster",
		"internal/clients/acme/data.go",
	}

	leakPatterns := []string{"acme", "Acme", "ACME", "horizon", "Horizon", "HORIZON", "tilden", "Tilden", "TILDEN"}

	for _, in := range cases {
		out := r.Redact(in)
		for _, leak := range leakPatterns {
			if strings.Contains(out, leak) {
				t.Errorf("zero-leak violation: input=%q output=%q contains %q", in, out, leak)
			}
		}
	}
}

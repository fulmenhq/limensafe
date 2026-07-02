package catalog

import (
	"context"
	"strings"
	"testing"
)

type fakeResolver struct {
	verdicts map[string]RepoVisibility
}

func (f fakeResolver) Resolve(_ context.Context, repo string) RepoVisibility {
	if v, ok := f.verdicts[repo]; ok {
		return v
	}
	return RepoUnresolved
}

const visBaseCatalog = `$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-vis-base
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-keep
    class: codename
    aliases: ["Horizon"]
    notes: "must survive enrichment"
`

func buildVis(t *testing.T, base string, mappings string, verdicts map[string]RepoVisibility) ([]byte, VisibilitySummary) {
	t.Helper()
	out, summary, err := BuildVisibilityAllowlist(
		context.Background(),
		[]byte(base),
		strings.NewReader(mappings),
		fakeResolver{verdicts: verdicts},
	)
	if err != nil {
		t.Fatalf("BuildVisibilityAllowlist: %v", err)
	}
	return out, summary
}

func TestVisibilityAllowlistPublicIsAllowlisted(t *testing.T) {
	out, summary := buildVis(t,
		visBaseCatalog,
		"Tilden==>acme/tilden\nHorizon==>acme/horizon\n",
		map[string]RepoVisibility{
			"acme/tilden":  RepoPublic,
			"acme/horizon": RepoNotPublic,
		},
	)
	if summary.Public != 1 || summary.Added != 1 || summary.NotPublic != 1 {
		t.Fatalf("summary = %+v, want Public=1 Added=1 NotPublic=1", summary)
	}
	c, err := LoadBytes(out)
	if err != nil {
		t.Fatalf("enriched catalog invalid: %v", err)
	}
	if len(c.Allowlist) != 1 {
		t.Fatalf("allowlist len = %d, want 1", len(c.Allowlist))
	}
	if c.Allowlist[0].Pattern != "Tilden" || c.Allowlist[0].Kind != AllowlistKindLiteral {
		t.Errorf("allowlist entry = %+v, want literal Tilden", c.Allowlist[0])
	}
	// Base entity (and its note) must survive enrichment.
	if len(c.Entities) != 1 || c.Entities[0].ID != "e-keep" || !strings.Contains(c.Entities[0].Notes, "survive") {
		t.Errorf("base entity not preserved: %+v", c.Entities)
	}
	// The generated id must be opaque (no protected codename inside it).
	if strings.Contains(c.Allowlist[0].ID, "Tilden") {
		t.Errorf("allowlist id leaked the codename: %q", c.Allowlist[0].ID)
	}
}

func TestVisibilityAllowlistFailsSafeOnUnresolved(t *testing.T) {
	out, summary := buildVis(t,
		visBaseCatalog,
		"Tilden==>acme/tilden\n",
		map[string]RepoVisibility{
			// No verdict for acme/tilden → resolver returns RepoUnresolved.
		},
	)
	if summary.Unresolved != 1 || summary.Added != 0 {
		t.Fatalf("summary = %+v, want Unresolved=1 Added=0", summary)
	}
	c, err := LoadBytes(out)
	if err != nil {
		t.Fatalf("catalog invalid: %v", err)
	}
	if len(c.Allowlist) != 0 {
		t.Errorf("unresolved repo must not be allowlisted, got %+v", c.Allowlist)
	}
}

func TestVisibilityAllowlistIsIdempotent(t *testing.T) {
	mappings := "Tilden==>acme/tilden\n"
	verdicts := map[string]RepoVisibility{"acme/tilden": RepoPublic}
	first, _ := buildVis(t, visBaseCatalog, mappings, verdicts)
	// Re-running over the already-enriched catalog must not duplicate the entry.
	second, summary := buildVis(t, string(first), mappings, verdicts)
	if summary.Added != 0 {
		t.Errorf("re-run Added = %d, want 0 (idempotent)", summary.Added)
	}
	c, err := LoadBytes(second)
	if err != nil {
		t.Fatalf("catalog invalid: %v", err)
	}
	if len(c.Allowlist) != 1 {
		t.Errorf("re-run produced %d allowlist entries, want 1", len(c.Allowlist))
	}
}

func TestVisibilityMapParseErrorsValueFree(t *testing.T) {
	_, _, err := BuildVisibilityAllowlist(
		context.Background(),
		[]byte(visBaseCatalog),
		strings.NewReader("SECRETcodename-no-separator\n"),
		fakeResolver{},
	)
	if err == nil {
		t.Fatal("expected a parse error for a line without the separator")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("map parse error leaked content: %v", err)
	}
}

// TestVisibilityConflictingDuplicateFailsClosed is the secrev regression: the
// same codename mapped to a public repo AND a private/unresolved repo must not
// allowlist off the first public result — it is an ambiguity and must fail
// closed with a value-free error.
func TestVisibilityConflictingDuplicateFailsClosed(t *testing.T) {
	out, _, err := BuildVisibilityAllowlist(
		context.Background(),
		[]byte(visBaseCatalog),
		strings.NewReader("Tilden==>acme/tilden-public\nTilden==>acme/tilden-private\n"),
		fakeResolver{verdicts: map[string]RepoVisibility{
			"acme/tilden-public":  RepoPublic,
			"acme/tilden-private": RepoNotPublic,
		}},
	)
	if err == nil {
		t.Fatalf("conflicting duplicate codename must fail closed; got no error (out=%q)", out)
	}
	if out != nil {
		t.Errorf("no catalog should be produced on a conflicting-duplicate error, got %d bytes", len(out))
	}
	// Value-free: neither the codename nor either repo may appear in the error.
	for _, leak := range []string{"Tilden", "tilden-public", "tilden-private", "acme"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("conflict error leaked %q: %v", leak, err)
		}
	}
}

// TestVisibilityExactDuplicateCollapses proves an EXACT duplicate line (same
// codename → same repo) is benign: it collapses to a single allowlist entry
// rather than erroring.
func TestVisibilityExactDuplicateCollapses(t *testing.T) {
	out, summary := buildVis(t,
		visBaseCatalog,
		"Tilden==>acme/tilden\nTilden==>acme/tilden\n",
		map[string]RepoVisibility{"acme/tilden": RepoPublic},
	)
	if summary.Total != 1 || summary.Added != 1 {
		t.Fatalf("exact duplicate should collapse to one mapping; summary=%+v", summary)
	}
	c, err := LoadBytes(out)
	if err != nil {
		t.Fatalf("catalog invalid: %v", err)
	}
	if len(c.Allowlist) != 1 {
		t.Errorf("exact duplicate produced %d allowlist entries, want 1", len(c.Allowlist))
	}
}

func TestVisibilityNoPublicIsNoop(t *testing.T) {
	out, summary := buildVis(t,
		visBaseCatalog,
		"Tilden==>acme/tilden\n",
		map[string]RepoVisibility{"acme/tilden": RepoNotPublic},
	)
	if summary.Added != 0 {
		t.Errorf("Added = %d, want 0", summary.Added)
	}
	// No-op returns the base bytes unchanged.
	if string(out) != visBaseCatalog {
		t.Errorf("expected unchanged base bytes on no-op enrichment")
	}
}

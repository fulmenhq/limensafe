package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fulmenhq/limensafe/pkg/extractor"
	"github.com/fulmenhq/limensafe/pkg/output"
	"github.com/fulmenhq/limensafe/pkg/publish"
)

func TestEmitLeakVectorWarnings_LoudCopyAndRedaction(t *testing.T) {
	// A name-pattern leak vector whose ref name embeds a protected alias.
	redactor, err := output.NewRedactor([]output.Alias{{Pattern: "acme", EntityID: "e-client-1", CaseInsensitive: true}})
	if err != nil {
		t.Fatalf("redactor: %v", err)
	}
	verdict := publish.SurfaceVerdict{
		Refs: []publish.RefVerdict{
			{
				Ref:             extractor.RemoteRef{Name: "backup/acme-pre-rewrite", Kind: extractor.RefKindBranch},
				LeakVector:      true,
				Reasons:         []string{publish.ReasonNamePattern},
				SuggestedAction: "git push origin --delete backup/acme-pre-rewrite",
			},
			{
				Ref:             extractor.RemoteRef{Name: "feature/diverged", Kind: extractor.RefKindBranch},
				LeakVector:      true,
				Reasons:         []string{publish.ReasonDivergesFromPrimary},
				SuggestedAction: "git push origin --delete feature/diverged",
			},
			{Ref: extractor.RemoteRef{Name: "main"}, LeakVector: false},
		},
	}

	var buf bytes.Buffer
	emitLeakVectorWarnings(&buf, verdict, redactor)
	got := buf.String()

	// Zero-leak: the protected alias must not appear despite being in the ref
	// name and suggested_action.
	if strings.Contains(strings.ToLower(got), "acme") {
		t.Fatalf("warning leaked protected alias:\n%s", got)
	}
	// The LOUD copy fires for the name-pattern ref.
	if !strings.Contains(got, "LEAK-VECTOR REF DETECTED") || !strings.Contains(got, "DEFEATS the rewrite") {
		t.Fatalf("expected the loud backup-defeat copy:\n%s", got)
	}
	// The divergence-only ref gets the concise directive, not the loud copy.
	if !strings.Contains(got, "feature/diverged carries protected content") {
		t.Fatalf("expected concise divergence warning:\n%s", got)
	}
	// Exactly the two leak-vector refs are warned; the clean ref is not.
	if n := strings.Count(got, "⚠️"); n != 2 {
		t.Fatalf("expected exactly 2 warning blocks, got %d:\n%s", n, got)
	}
}

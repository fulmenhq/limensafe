package extractor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// Builds a repo where main has a.txt, and backup/old branches from main and
// adds b.txt. The blob for b.txt is reachable only from backup/old; a.txt's
// blob is reachable from both. This is the attribution the divergence detector
// relies on.
func newDivergentRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "a.txt", "shared clean content\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-q", "-m", "main: a")
	git(t, dir, "branch", "backup/old")
	git(t, dir, "checkout", "-q", "backup/old")
	writeFile(t, dir, "b.txt", "divergent content only on backup\n")
	git(t, dir, "add", "b.txt")
	git(t, dir, "commit", "-q", "-m", "backup: b")
	git(t, dir, "checkout", "-q", "main")
	return dir
}

func TestPublishSurface_BlobAttributionAcrossRefs(t *testing.T) {
	dir := newDivergentRepo(t)
	ctx := context.Background()

	refs, err := EnumerateRefs(ctx, dir, RefEnumerationOptions{IncludeLocal: true, Branches: true, Tags: true})
	if err != nil {
		t.Fatalf("enumerate refs: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs = %d, want 2 (main, backup/old)", len(refs))
	}
	for _, r := range refs {
		if !r.LocalObjects {
			t.Fatalf("local ref %s should have local objects", r.Name)
		}
	}

	ex, err := NewPublishSurfaceExtractor(dir, refs, 0)
	if err != nil {
		t.Fatalf("new extractor: %v", err)
	}
	if err := ex.Enumerate(ctx); err != nil {
		t.Fatalf("enumerate blobs: %v", err)
	}

	// Index the per-path attributions across all blobs.
	pathToRefs := map[string][]string{}
	for _, b := range ex.Blobs() {
		for _, pr := range b.Paths {
			pathToRefs[pr.Path] = pr.Refs
		}
	}
	if got := pathToRefs["a.txt"]; len(got) != 2 {
		t.Fatalf("a.txt refs = %v, want both main and backup/old", got)
	}
	if got := pathToRefs["b.txt"]; len(got) != 1 || got[0] != "backup/old" {
		t.Fatalf("b.txt refs = %v, want [backup/old] only (divergent blob)", got)
	}

	st := ex.Stats()
	if st.RefsTotal != 2 || st.RefsScanned != 2 {
		t.Fatalf("stats refs = %+v", st)
	}
	if st.BlobsScanned != 2 {
		t.Fatalf("expected 2 unique scannable blobs, got %d", st.BlobsScanned)
	}
}

// devrev P1 regression: the SAME blob content is exposed at a different
// (protected-looking) path only on the backup ref. A single representative
// path would collapse this and lose the per-ref/per-path attribution; the
// per-path model must retain both paths with their distinct ref sets so the
// path-segment finding can attribute to backup/old alone.
func TestPublishSurface_SameBlobDistinctPathsPerRef(t *testing.T) {
	dir := t.TempDir()
	const shared = "identical bytes, clean content\n"
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "safe.txt", shared)
	git(t, dir, "add", "safe.txt")
	git(t, dir, "commit", "-q", "-m", "main: safe.txt")
	git(t, dir, "checkout", "-q", "-b", "backup/old")
	// Same content -> same blob SHA, but at a path that only backup/old carries.
	if err := os.MkdirAll(filepath.Join(dir, "clients", "acme"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, filepath.Join("clients", "acme", "notes.txt"), shared)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "backup: same blob at protected path")
	git(t, dir, "checkout", "-q", "main")

	ctx := context.Background()
	refs, err := EnumerateRefs(ctx, dir, RefEnumerationOptions{IncludeLocal: true, Branches: true, Tags: true})
	if err != nil {
		t.Fatalf("enumerate refs: %v", err)
	}
	ex, err := NewPublishSurfaceExtractor(dir, refs, 0)
	if err != nil {
		t.Fatalf("new extractor: %v", err)
	}
	if err := ex.Enumerate(ctx); err != nil {
		t.Fatalf("enumerate: %v", err)
	}

	// One unique blob (shared content), two path attributions.
	blobs := ex.Blobs()
	if len(blobs) != 1 {
		t.Fatalf("expected 1 unique blob (deduped content), got %d", len(blobs))
	}
	pathToRefs := map[string][]string{}
	for _, pr := range blobs[0].Paths {
		pathToRefs[pr.Path] = pr.Refs
	}
	if len(pathToRefs) != 2 {
		t.Fatalf("expected 2 distinct path attributions, got %v", pathToRefs)
	}
	protected := filepath.ToSlash(filepath.Join("clients", "acme", "notes.txt"))
	if got := pathToRefs[protected]; len(got) != 1 || got[0] != "backup/old" {
		t.Fatalf("%s refs = %v, want [backup/old] only — the path-only leak vector", protected, got)
	}
	if got := pathToRefs["safe.txt"]; len(got) != 2 {
		t.Fatalf("safe.txt refs = %v, want both refs", got)
	}
}

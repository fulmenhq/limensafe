package extractor

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func collectRun(t *testing.T, e Extractor) ([]InputUnit, []SkipEvent, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := make(chan InputUnit, 64)
	skips := make(chan SkipEvent, 64)

	errCh := make(chan error, 1)
	go func() {
		errCh <- e.Run(ctx, out, skips)
	}()

	var units []InputUnit
	var skipEvents []SkipEvent
	for out != nil || skips != nil {
		select {
		case u, ok := <-out:
			if !ok {
				out = nil
				continue
			}
			units = append(units, u)
		case s, ok := <-skips:
			if !ok {
				skips = nil
				continue
			}
			skipEvents = append(skipEvents, s)
		case <-ctx.Done():
			t.Fatal("test timed out")
		}
	}
	return units, skipEvents, <-errCh
}

func TestFilesystemExtractor_NewRequiresValidRoot(t *testing.T) {
	if _, err := NewFilesystemExtractor("", 0); err == nil {
		t.Error("expected error for empty root")
	}
	if _, err := NewFilesystemExtractor("/nonexistent/path/xyz12345", 0); err == nil {
		t.Error("expected error for nonexistent root")
	}

	tmp := t.TempDir()
	file := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesystemExtractor(file, 0); err == nil {
		t.Error("expected error when root is a file, not a directory")
	}
}

func TestFilesystemExtractor_WalksFiles(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, "a.go"), "package a")
	mustWrite(t, filepath.Join(tmp, "sub", "b.txt"), "hello")
	mustWrite(t, filepath.Join(tmp, "sub", "deep", "c.md"), "# md")

	e, err := NewFilesystemExtractor(tmp, 0)
	if err != nil {
		t.Fatal(err)
	}
	units, skips, err := collectRun(t, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(skips) != 0 {
		t.Errorf("unexpected skips: %v", skips)
	}

	gotPaths := make([]string, len(units))
	for i, u := range units {
		gotPaths[i] = u.SourceID
	}
	sort.Strings(gotPaths)
	wantPaths := []string{"a.go", "sub/b.txt", "sub/deep/c.md"}
	if !equalStringSlices(gotPaths, wantPaths) {
		t.Errorf("got %v, want %v", gotPaths, wantPaths)
	}

	for _, u := range units {
		if u.SourceKind != "file" {
			t.Errorf("expected source_kind=file; got %q", u.SourceKind)
		}
		if u.Encoding != "utf-8" {
			t.Errorf("expected encoding=utf-8; got %q", u.Encoding)
		}
	}
}

func TestFilesystemExtractor_SkipsDotGit(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, "a.go"), "package a")
	mustWrite(t, filepath.Join(tmp, ".git", "HEAD"), "ref: refs/heads/main")
	mustWrite(t, filepath.Join(tmp, ".git", "objects", "pack", "anything.pack"), "binary stuff")

	e, _ := NewFilesystemExtractor(tmp, 0)
	units, _, err := collectRun(t, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, u := range units {
		if strings.Contains(u.SourceID, ".git") {
			t.Errorf(".git tree should be skipped; got %q", u.SourceID)
		}
	}
}

func TestFilesystemExtractor_SizeCapEmitsSkipEvent(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, "small.txt"), "ok")
	big := strings.Repeat("x", 200)
	mustWrite(t, filepath.Join(tmp, "big.log"), big)

	e, _ := NewFilesystemExtractor(tmp, 100)
	units, skips, err := collectRun(t, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// small.txt should emit; big.log should be skipped
	if len(units) != 1 || units[0].SourceID != "small.txt" {
		t.Errorf("expected only small.txt; got %v", unitIDs(units))
	}

	foundSkip := false
	for _, s := range skips {
		if s.SourceID == "big.log" && s.Reason == SkipFileTooLarge {
			foundSkip = true
		}
	}
	if !foundSkip {
		t.Errorf("expected SkipFileTooLarge for big.log; got %v", skips)
	}
}

func TestFilesystemExtractor_BinaryExtSkipped(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, "doc.md"), "# md")
	mustWrite(t, filepath.Join(tmp, "image.png"), "fake png")
	mustWrite(t, filepath.Join(tmp, "lib.so"), "fake binary")

	e, _ := NewFilesystemExtractor(tmp, 0)
	units, skips, err := collectRun(t, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(units) != 1 || units[0].SourceID != "doc.md" {
		t.Errorf("expected only doc.md; got %v", unitIDs(units))
	}

	skipExt := map[string]bool{}
	for _, s := range skips {
		if s.Reason == SkipBinaryDetected {
			skipExt[filepath.Ext(s.SourceID)] = true
		}
	}
	if !skipExt[".png"] || !skipExt[".so"] {
		t.Errorf("expected .png and .so skipped; got %v", skipExt)
	}
}

func TestFilesystemExtractor_RootIgnoreFiles(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, ".gitignore"), "gitignored.log\n")
	mustWrite(t, filepath.Join(tmp, ".limensafeignore"), "generated/\nvendor/\n!generated/keep.go\n*.secret\n!keep.secret\n")
	mustWrite(t, filepath.Join(tmp, "keep.go"), "package keep")
	mustWrite(t, filepath.Join(tmp, "gitignored.log"), "drop")
	mustWrite(t, filepath.Join(tmp, "generated", "drop.go"), "drop")
	mustWrite(t, filepath.Join(tmp, "generated", "keep.go"), "package keep")
	mustWrite(t, filepath.Join(tmp, "nested", "drop.secret"), "drop")
	mustWrite(t, filepath.Join(tmp, "vendor", "drop.go"), "drop")
	mustWrite(t, filepath.Join(tmp, "keep.secret"), "keep")

	e, err := NewFilesystemExtractor(tmp, 0)
	if err != nil {
		t.Fatal(err)
	}
	units, skips, err := collectRun(t, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	gotPaths := unitIDs(units)
	sort.Strings(gotPaths)
	wantPaths := []string{".gitignore", ".limensafeignore", "generated/keep.go", "keep.go", "keep.secret"}
	if !equalStringSlices(gotPaths, wantPaths) {
		t.Errorf("got units %v, want %v", gotPaths, wantPaths)
	}

	reasons := map[string]SkipEvent{}
	for _, s := range skips {
		reasons[s.SourceID] = s
	}
	for _, path := range []string{"gitignored.log", "generated/drop.go", "nested/drop.secret", "vendor/drop.go"} {
		s, ok := reasons[path]
		if !ok {
			t.Fatalf("expected ignored skip for %s; skips=%v", path, skips)
		}
		if s.Reason != SkipIgnored {
			t.Fatalf("skip reason for %s = %s, want ignored", path, s.Reason)
		}
	}
	if reasons["vendor/drop.go"].IsDirectory {
		t.Fatalf("expected vendor/drop.go file skip, got %#v", reasons["vendor/drop.go"])
	}
}

func TestFilesystemExtractor_PrunesIgnoredDirectoryWhenNoNegationCanReinclude(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, ".limensafeignore"), "vendor/\n")
	mustWrite(t, filepath.Join(tmp, "vendor", "drop.go"), "drop")

	e, err := NewFilesystemExtractor(tmp, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, skips, err := collectRun(t, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(skips) != 1 {
		t.Fatalf("expected one directory prune skip, got %v", skips)
	}
	if skips[0].SourceID != "vendor" || !skips[0].IsDirectory || skips[0].Reason != SkipIgnored {
		t.Fatalf("expected ignored vendor directory prune, got %#v", skips[0])
	}
}

func TestFilesystemExtractor_IncludeIgnored(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, ".limensafeignore"), "ignored.txt\n")
	mustWrite(t, filepath.Join(tmp, "ignored.txt"), "scan me")

	e, err := NewFilesystemExtractorWithOptions(tmp, 0, FilesystemOptions{IncludeIgnored: true})
	if err != nil {
		t.Fatal(err)
	}
	units, skips, err := collectRun(t, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(skips) != 0 {
		t.Fatalf("expected no skips with IncludeIgnored; got %v", skips)
	}
	if !containsString(unitIDs(units), "ignored.txt") {
		t.Fatalf("expected ignored.txt to be scanned; units=%v", unitIDs(units))
	}
}

func TestFilesystemExtractor_SyntheticAcmeRepo(t *testing.T) {
	root := "../../testdata/synthetic-acme/repo"
	if _, err := os.Stat(root); err != nil {
		t.Skipf("corpus not available: %v", err)
	}

	e, _ := NewFilesystemExtractor(root, 0)
	units, _, err := collectRun(t, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Expect: profile_test.go, redact_test.go, loader_test.go,
	// data.go, usage.md (5 leak-bearing fixtures from the corpus)
	if len(units) < 5 {
		t.Errorf("expected ≥5 units from synthetic-acme/repo; got %d (%v)", len(units), unitIDs(units))
	}

	// Spot-check the path-only-leak fixture is present
	found := false
	for _, u := range units {
		if strings.Contains(u.SourceID, "internal/clients/acme/data.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected internal/clients/acme/data.go in units; got %v", unitIDs(units))
	}
}

func TestFilesystemExtractor_ContextCancel(t *testing.T) {
	tmp := t.TempDir()
	for i := 0; i < 50; i++ {
		mustWrite(t, filepath.Join(tmp, "f"+string(rune('a'+i%26))+".txt"), "x")
	}

	e, _ := NewFilesystemExtractor(tmp, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	out := make(chan InputUnit, 64)
	skips := make(chan SkipEvent, 64)
	err := e.Run(ctx, out, skips)
	if err == nil {
		t.Error("expected error for cancelled context; got nil")
	}
}

// helpers

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func unitIDs(units []InputUnit) []string {
	out := make([]string, len(units))
	for i, u := range units {
		out[i] = u.SourceID
	}
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

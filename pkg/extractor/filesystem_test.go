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

// fileSkipTotal models the scan.go consumer: files_skipped is the count of
// per-file skips plus the represented-file count carried by directory prunes
// (internal-brief). It returns the total and the reason breakdown so tests can assert
// reconciliation regardless of which skip path the walker took.
func fileSkipTotal(skips []SkipEvent) (int, map[string]int) {
	total := 0
	byReason := map[string]int{}
	for _, s := range skips {
		if s.IsDirectory {
			if s.RepresentedFiles > 0 {
				total += s.RepresentedFiles
				byReason[s.Reason.String()] += s.RepresentedFiles
			}
			continue
		}
		total++
		byReason[s.Reason.String()]++
	}
	return total, byReason
}

// TestFilesystemExtractor_PruneReportsRepresentedFileCount verifies a
// directory prune carries the count of files behind it (nested included,
// nested .git excluded) so files_skipped stays a faithful total (internal-brief).
func TestFilesystemExtractor_PruneReportsRepresentedFileCount(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, ".limensafeignore"), "generated/\n")
	mustWrite(t, filepath.Join(tmp, "generated", "a.txt"), "a")
	mustWrite(t, filepath.Join(tmp, "generated", "b.txt"), "b")
	mustWrite(t, filepath.Join(tmp, "generated", "sub", "c.txt"), "c")
	// A nested .git tree must not inflate the represented count.
	mustWrite(t, filepath.Join(tmp, "generated", ".git", "config"), "x")
	mustWrite(t, filepath.Join(tmp, "keep.txt"), "scan me")

	e, err := NewFilesystemExtractor(tmp, 0)
	if err != nil {
		t.Fatal(err)
	}
	units, skips, err := collectRun(t, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(skips) != 1 || !skips[0].IsDirectory || skips[0].SourceID != "generated" {
		t.Fatalf("expected one generated/ directory prune; got %#v", skips)
	}
	if skips[0].RepresentedFiles != 3 {
		t.Errorf("RepresentedFiles = %d, want 3 (a.txt, b.txt, sub/c.txt; .git excluded)", skips[0].RepresentedFiles)
	}
	if !containsString(unitIDs(units), "keep.txt") {
		t.Errorf("expected keep.txt scanned; units=%v", unitIDs(units))
	}
}

// TestFilesystemExtractor_EmptyIgnoredDirRepresentsZero verifies an empty
// ignored directory still emits a directory prune but represents 0 files —
// directories_skipped:1, files_skipped += 0 (internal-brief).
func TestFilesystemExtractor_EmptyIgnoredDirRepresentsZero(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, ".limensafeignore"), "empty/\n")
	if err := os.MkdirAll(filepath.Join(tmp, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	e, err := NewFilesystemExtractor(tmp, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, skips, err := collectRun(t, e)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(skips) != 1 || !skips[0].IsDirectory || skips[0].SourceID != "empty" {
		t.Fatalf("expected one empty/ directory prune; got %#v", skips)
	}
	if skips[0].RepresentedFiles != 0 {
		t.Errorf("RepresentedFiles = %d, want 0 for empty directory", skips[0].RepresentedFiles)
	}
	total, byReason := fileSkipTotal(skips)
	if total != 0 || len(byReason) != 0 {
		t.Errorf("empty ignored dir should add 0 to files_skipped and no reason key; got total=%d byReason=%v", total, byReason)
	}
}

// TestFilesystemExtractor_SkipAccountingReconcilesAcrossShapes is the core
// internal-brief guarantee: the same set of files, hidden via a directory prune in
// one tree and matched file-by-file in another, yields the same file-skip
// total and reason breakdown — no scope-dependent flip that loses information.
func TestFilesystemExtractor_SkipAccountingReconcilesAcrossShapes(t *testing.T) {
	build := func(t *testing.T, ignore string) []SkipEvent {
		t.Helper()
		tmp := t.TempDir()
		mustWrite(t, filepath.Join(tmp, ".limensafeignore"), ignore)
		mustWrite(t, filepath.Join(tmp, "generated", "a.txt"), "a")
		mustWrite(t, filepath.Join(tmp, "generated", "b.txt"), "b")
		mustWrite(t, filepath.Join(tmp, "generated", "sub", "c.txt"), "c")
		e, err := NewFilesystemExtractor(tmp, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, skips, err := collectRun(t, e)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		return skips
	}

	// Prune shape: the directory rule prunes generated/ wholesale.
	pruneTotal, pruneByReason := fileSkipTotal(build(t, "generated/\n"))
	// Per-file shape: a file glob matches each .txt individually, no prune.
	perFileTotal, perFileByReason := fileSkipTotal(build(t, "**/*.txt\n"))

	if pruneTotal != 3 {
		t.Errorf("prune shape files_skipped = %d, want 3", pruneTotal)
	}
	if perFileTotal != 3 {
		t.Errorf("per-file shape files_skipped = %d, want 3", perFileTotal)
	}
	if pruneTotal != perFileTotal {
		t.Errorf("file-skip total differs by tree shape: prune=%d per-file=%d", pruneTotal, perFileTotal)
	}
	if pruneByReason["ignored"] != 3 || perFileByReason["ignored"] != 3 {
		t.Errorf("reason breakdown differs by shape: prune=%v per-file=%v", pruneByReason, perFileByReason)
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

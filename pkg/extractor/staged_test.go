package extractor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// initTestRepo bootstraps a git working tree with the supplied files
// (path → content) staged. Returns the absolute path to the repo root.
func initTestRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()

	mustGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	mustGit("init", "-q", "-b", "main")
	mustGit("config", "user.email", "test@example.com")
	mustGit("config", "user.name", "test")

	// Empty initial commit so HEAD exists; staging runs against it.
	mustGit("commit", "--allow-empty", "-q", "-m", "init")

	for path, content := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustGit("add", "-A")

	return root
}

func collectStaged(t *testing.T, e *StagedExtractor) ([]InputUnit, []SkipEvent, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan InputUnit, 64)
	skips := make(chan SkipEvent, 64)
	errCh := make(chan error, 1)
	go func() { errCh <- e.Run(ctx, out, skips) }()

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
		}
	}
	return units, skipEvents, <-errCh
}

func TestStagedExtractor_NewRequiresGitRepo(t *testing.T) {
	tmp := t.TempDir()
	if _, err := NewStagedExtractor(tmp, 0); err == nil {
		t.Error("expected error for non-git directory")
	}
}

func TestStagedExtractor_EmitsStagedFiles(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"a.go":       "package a",
		"sub/b.txt":  "hello",
		"sub/c/c.md": "# md",
	})

	e, err := NewStagedExtractor(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	units, skips, err := collectStaged(t, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(skips) != 0 {
		t.Errorf("unexpected skips: %v", skips)
	}

	got := make([]string, len(units))
	for i, u := range units {
		got[i] = u.SourceID
	}
	sort.Strings(got)
	want := []string{"a.go", "sub/b.txt", "sub/c/c.md"}
	if !equalStringSlices(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestStagedExtractor_StagedContentNotWorkingTree(t *testing.T) {
	// Staging a file with content X, then editing the working tree to Y,
	// the extractor must emit X (the staged blob), not Y.
	root := initTestRepo(t, map[string]string{
		"file.txt": "STAGED",
	})

	// Edit working tree without re-staging.
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("WORKING-TREE-EDIT"), 0o644); err != nil {
		t.Fatal(err)
	}

	e, _ := NewStagedExtractor(root, 0)
	units, _, err := collectStaged(t, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}
	if string(units[0].Content) != "STAGED" {
		t.Errorf("expected staged content %q, got %q", "STAGED", string(units[0].Content))
	}
}

func TestStagedExtractor_BinaryExtSkipped(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"doc.md":    "# md",
		"image.png": "fakepng",
		"lib.so":    "fakebin",
	})
	e, _ := NewStagedExtractor(root, 0)
	units, skips, err := collectStaged(t, e)
	if err != nil {
		t.Fatal(err)
	}

	if len(units) != 1 || units[0].SourceID != "doc.md" {
		ids := make([]string, len(units))
		for i, u := range units {
			ids[i] = u.SourceID
		}
		t.Errorf("expected only doc.md; got %v", ids)
	}

	skipExts := map[string]bool{}
	for _, s := range skips {
		if s.Reason == SkipBinaryDetected {
			skipExts[filepath.Ext(s.SourceID)] = true
		}
	}
	if !skipExts[".png"] || !skipExts[".so"] {
		t.Errorf("expected .png and .so skipped; got %v", skipExts)
	}
}

func TestStagedExtractor_SizeCapEmitsSkip(t *testing.T) {
	big := strings.Repeat("x", 200)
	root := initTestRepo(t, map[string]string{
		"small.txt": "ok",
		"big.log":   big,
	})
	e, _ := NewStagedExtractor(root, 100)
	units, skips, err := collectStaged(t, e)
	if err != nil {
		t.Fatal(err)
	}

	if len(units) != 1 || units[0].SourceID != "small.txt" {
		t.Errorf("expected only small.txt; got %d units", len(units))
	}

	foundSkip := false
	for _, s := range skips {
		if s.SourceID == "big.log" && s.Reason == SkipFileTooLarge {
			foundSkip = true
		}
	}
	if !foundSkip {
		t.Errorf("expected big.log skipped for size; got %v", skips)
	}
}

func TestStagedExtractor_NoStagedFiles(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = root
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "test")
	cmd.Dir = root
	cmd.Run()
	cmd = exec.Command("git", "commit", "--allow-empty", "-q", "-m", "init")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	e, err := NewStagedExtractor(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	units, skips, err := collectStaged(t, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 0 {
		t.Errorf("expected no units; got %d", len(units))
	}
	if len(skips) != 0 {
		t.Errorf("expected no skips; got %d", len(skips))
	}
}

func TestStagedExtractor_PathsWithSpacesAndUnicode(t *testing.T) {
	root := initTestRepo(t, map[string]string{
		"my dir/file with spaces.txt": "spaces ok",
		"日本語/ファイル.txt":                "unicode ok",
	})
	e, _ := NewStagedExtractor(root, 0)
	units, _, err := collectStaged(t, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		ids := make([]string, len(units))
		for i, u := range units {
			ids[i] = u.SourceID
		}
		t.Errorf("expected 2 units; got %d (%v)", len(units), ids)
	}
}

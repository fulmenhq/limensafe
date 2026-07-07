package extractor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitArchiveToTempExtractsAndCleans(t *testing.T) {
	root := initCommittedArchiveRepo(t, map[string]string{
		"a.txt":       "hello\n",
		"nested/b.md": "# doc\n",
	})

	tmpdir, cleanup, err := GitArchiveToTemp(context.Background(), root, "HEAD", GitArchiveOptions{})
	if err != nil {
		t.Fatalf("GitArchiveToTemp: %v", err)
	}
	defer cleanup()

	if _, err := os.Stat(filepath.Join(tmpdir, "a.txt")); err != nil {
		t.Fatalf("expected a.txt in archive tempdir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpdir, "nested", "b.md")); err != nil {
		t.Fatalf("expected nested/b.md in archive tempdir: %v", err)
	}

	cleanup()
	if _, err := os.Stat(tmpdir); !os.IsNotExist(err) {
		t.Fatalf("cleanup left tempdir behind: %v", err)
	}
}

func TestGitArchiveToTempExtractsBareMirror(t *testing.T) {
	root := initCommittedArchiveRepo(t, map[string]string{
		"a.txt":       "hello\n",
		"nested/b.md": "# doc\n",
	})
	parent := t.TempDir()
	mirror := filepath.Join(parent, "repo.git")
	runArchiveGit(t, parent, "clone", "--mirror", root, mirror)

	tmpdir, cleanup, err := GitArchiveToTemp(context.Background(), mirror, "HEAD", GitArchiveOptions{})
	if err != nil {
		t.Fatalf("GitArchiveToTemp bare mirror: %v", err)
	}
	defer cleanup()

	if _, err := os.Stat(filepath.Join(tmpdir, "a.txt")); err != nil {
		t.Fatalf("expected a.txt in archive tempdir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpdir, "nested", "b.md")); err != nil {
		t.Fatalf("expected nested/b.md in archive tempdir: %v", err)
	}
}

func TestGitArchiveToTempCleansOnGitFailure(t *testing.T) {
	root := initCommittedArchiveRepo(t, map[string]string{"a.txt": "hello\n"})
	parent := t.TempDir()
	ref := "definitely-not-a-ref"

	_, _, err := GitArchiveToTemp(context.Background(), root, ref, GitArchiveOptions{TempDirParent: parent})
	if err == nil {
		t.Fatal("expected git archive failure")
	}
	if strings.Contains(err.Error(), ref) {
		t.Fatalf("error leaked ref: %v", err)
	}
	entries, readErr := os.ReadDir(parent)
	if readErr != nil {
		t.Fatalf("read temp parent: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temp parent empty after failure, got %d entries", len(entries))
	}
}

func TestGitArchiveToTempCleansOnTarFailure(t *testing.T) {
	root := initCommittedArchiveRepo(t, map[string]string{"a.txt": "hello\n"})
	parent := t.TempDir()

	_, _, err := GitArchiveToTemp(context.Background(), root, "HEAD", GitArchiveOptions{
		TempDirParent: parent,
		TarPath:       "false",
	})
	if err == nil {
		t.Fatal("expected tar failure")
	}
	if strings.Contains(err.Error(), parent) {
		t.Fatalf("error leaked temp parent path: %v", err)
	}
	entries, readErr := os.ReadDir(parent)
	if readErr != nil {
		t.Fatalf("read temp parent: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temp parent empty after failure, got %d entries", len(entries))
	}
}

func initCommittedArchiveRepo(t *testing.T, files map[string]string) string {
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
	mustGit("commit", "-q", "-m", "fixture")

	return root
}

func runArchiveGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

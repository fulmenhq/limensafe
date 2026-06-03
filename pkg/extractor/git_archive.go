package extractor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// GitArchiveOptions configures GitArchiveToTemp. Empty command paths use
// PATH lookup. TempDirParent is primarily for tests and defaults to os.TempDir.
type GitArchiveOptions struct {
	GitPath       string
	TarPath       string
	TempDirParent string
}

// GitArchiveToTemp extracts `git archive <ref>` into a fresh temporary
// directory and returns that directory plus an idempotent cleanup function.
// Callers must invoke cleanup when done. On any failure after tempdir creation,
// the tempdir is removed before the error is returned.
func GitArchiveToTemp(ctx context.Context, repoRoot, ref string, opts GitArchiveOptions) (string, func(), error) {
	if repoRoot == "" {
		return "", nil, invalidConfig("git archive: repo root is required")
	}
	if ref == "" {
		ref = "HEAD"
	}

	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", nil, fmt.Errorf("git archive: abs repo root: %w", err)
	}
	tempParent := opts.TempDirParent
	if tempParent == "" {
		tempParent = os.TempDir()
	}
	tmpdir, err := os.MkdirTemp(tempParent, "limensafe-git-archive-*")
	if err != nil {
		return "", nil, fmt.Errorf("git archive: create temp dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpdir)
	}
	cleanupOnErr := true
	defer func() {
		if cleanupOnErr {
			cleanup()
		}
	}()

	gitPath := opts.GitPath
	if gitPath == "" {
		gitPath = "git"
	}
	tarPath := opts.TarPath
	if tarPath == "" {
		tarPath = "tar"
	}

	gitCmd := exec.CommandContext(ctx, gitPath, "archive", ref)
	gitCmd.Dir = abs
	archiveReader, err := gitCmd.StdoutPipe()
	if err != nil {
		return "", nil, fmt.Errorf("git archive: stdout pipe: %w", err)
	}

	tarCmd := exec.CommandContext(ctx, tarPath, "-x", "-C", tmpdir)
	tarCmd.Stdin = archiveReader

	if err := gitCmd.Start(); err != nil {
		return "", nil, fmt.Errorf("git archive: start: %w", err)
	}
	if err := tarCmd.Start(); err != nil {
		_ = gitCmd.Wait()
		return "", nil, fmt.Errorf("tar extract: start: %w", err)
	}

	gitErr := gitCmd.Wait()
	tarErr := tarCmd.Wait()
	if gitErr != nil {
		return "", nil, fmt.Errorf("git archive failed: %w", gitErr)
	}
	if tarErr != nil {
		return "", nil, fmt.Errorf("tar extract git archive failed: %w", tarErr)
	}

	cleanupOnErr = false
	return tmpdir, cleanup, nil
}

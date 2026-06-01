package extractor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// StagedExtractor produces one InputUnit per staged file (added or
// modified in the git index, not yet committed). This is the
// pre-commit gate's primary surface — `git archive HEAD` misses
// staged content, so a sound pre-commit hook needs the index-level
// view this extractor provides.
//
// V0 semantics:
//   - File set: `git diff --cached --name-only --diff-filter=AM`
//     (added + modified). Renames and deletions are deliberately
//     excluded for v0 — renames split into delete+add and the new
//     path appears as Added; deletions can't leak content.
//   - Content: `git show :<path>` reads the staged blob (the index
//     content), independent of working-tree edits. So a developer
//     can edit a file post-stage and the scan still reflects what
//     would be committed.
//   - Skip rules match FilesystemExtractor: size cap, binary
//     extensions, and root .gitignore/.limensafeignore files. .git/
//     never appears in `git diff --cached` output so no special-case
//     skip needed.
//
// Concurrency: Run is the only goroutine entry; the extractor is
// not safe for concurrent Run calls (it's a one-shot per scan).
type StagedExtractor struct {
	RepoRoot       string
	MaxFileSize    int64
	IncludeIgnored bool
	SkipBinaryExt  map[string]bool
	IgnoreMatcher  *IgnoreMatcher
}

// StagedOptions configures optional staged extraction behavior.
type StagedOptions struct {
	IncludeIgnored bool
}

// NewStagedExtractor builds an extractor rooted at repoRoot. If
// repoRoot is empty, cwd is used. Returns an error if the directory
// is not a git working tree.
func NewStagedExtractor(repoRoot string, maxFileSize int64) (*StagedExtractor, error) {
	return NewStagedExtractorWithOptions(repoRoot, maxFileSize, StagedOptions{})
}

// NewStagedExtractorWithOptions builds a configured staged extractor.
func NewStagedExtractorWithOptions(repoRoot string, maxFileSize int64, opts StagedOptions) (*StagedExtractor, error) {
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("extractor: getwd: %w", err)
		}
		repoRoot = cwd
	}
	if maxFileSize <= 0 {
		maxFileSize = DefaultMaxFileSize
	}

	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("extractor: abs %q: %w", repoRoot, err)
	}

	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = abs
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("extractor: %q is not a git working tree: %s", abs, strings.TrimSpace(string(out)))
	}

	var ignoreMatcher *IgnoreMatcher
	if !opts.IncludeIgnored {
		ignoreMatcher, err = LoadRootIgnoreMatcher(abs)
		if err != nil {
			return nil, err
		}
	}
	return &StagedExtractor{
		RepoRoot:       abs,
		MaxFileSize:    maxFileSize,
		IncludeIgnored: opts.IncludeIgnored,
		SkipBinaryExt:  DefaultBinaryExtensions(),
		IgnoreMatcher:  ignoreMatcher,
	}, nil
}

// Run lists staged files via `git diff --cached --name-only` and
// emits one InputUnit per file. SkipEvents are emitted for files
// over the size cap or with binary extensions. Both channels are
// closed before Run returns.
func (e *StagedExtractor) Run(ctx context.Context, out chan<- InputUnit, skips chan<- SkipEvent) error {
	defer close(out)
	defer close(skips)

	files, err := e.listStaged(ctx)
	if err != nil {
		return err
	}

	for _, path := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if ignored, source := e.IgnoreMatcher.Match(path, false); ignored {
			if err := emitSkip(ctx, skips, SkipEvent{
				SourceID:     path,
				LocationHint: path,
				Reason:       SkipIgnored,
				Detail:       "matched " + source,
			}); err != nil {
				return err
			}
			continue
		}

		// Binary-extension skip — checked on path before reading content
		// so we don't pay git-show cost for known-binary files.
		ext := strings.ToLower(filepath.Ext(path))
		if e.SkipBinaryExt[ext] {
			if err := emitSkip(ctx, skips, SkipEvent{
				SourceID:     path,
				LocationHint: path,
				Reason:       SkipBinaryDetected,
				Detail:       "extension " + ext,
			}); err != nil {
				return err
			}
			continue
		}

		content, err := e.readStaged(ctx, path)
		if err != nil {
			if err := emitSkip(ctx, skips, SkipEvent{
				SourceID:     path,
				LocationHint: path,
				Reason:       SkipUnreadable,
				Detail:       err.Error(),
			}); err != nil {
				return err
			}
			continue
		}

		if int64(len(content)) > e.MaxFileSize {
			if err := emitSkip(ctx, skips, SkipEvent{
				SourceID:     path,
				LocationHint: path,
				Reason:       SkipFileTooLarge,
				Detail:       fmt.Sprintf("size %d exceeds cap %d", len(content), e.MaxFileSize),
			}); err != nil {
				return err
			}
			continue
		}

		unit := InputUnit{
			SourceID:     path,
			SourceKind:   "file",
			LocationHint: path,
			Content:      content,
			Encoding:     "utf-8",
			Metadata: map[string]string{
				"stage":     "index",
				"scan_root": e.RepoRoot,
			},
		}
		select {
		case out <- unit:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// listStaged returns the set of staged file paths (added + modified)
// relative to repo root, with no trailing newlines. Renames and
// deletions are excluded.
func (e *StagedExtractor) listStaged(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--name-only", "--diff-filter=AM", "-z")
	cmd.Dir = e.RepoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("extractor: git diff --cached: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	if len(out) == 0 {
		return nil, nil
	}
	parts := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		files = append(files, string(p))
	}
	return files, nil
}

// readStaged reads the staged blob content for path. Equivalent to
// `git show :<path>`. Returns an error if the path is not staged
// (e.g., race with index changes).
func (e *StagedExtractor) readStaged(ctx context.Context, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "show", ":"+path)
	cmd.Dir = e.RepoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func emitSkip(ctx context.Context, skips chan<- SkipEvent, event SkipEvent) error {
	select {
	case skips <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

package extractor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// GitDiffExtractor emits one InputUnit per contiguous added hunk in a git diff
// against BaseRef. It is intentionally an introduced-content surface:
// unchanged and removed lines are never scanned.
type GitDiffExtractor struct {
	RepoRoot string
	BaseRef  string
}

type GitDiffError struct {
	BaseRef string
	Detail  string
}

func (e *GitDiffError) Error() string {
	if e == nil {
		return "git diff failed"
	}
	if e.Detail == "" {
		return fmt.Sprintf("git diff %s...HEAD failed", e.BaseRef)
	}
	return fmt.Sprintf("git diff %s...HEAD failed: %s", e.BaseRef, e.Detail)
}

// NewGitDiffExtractor builds a diff extractor rooted at repoRoot. If repoRoot
// is empty, cwd is used. If baseRef is empty, origin/main is used.
func NewGitDiffExtractor(repoRoot, baseRef string) (*GitDiffExtractor, error) {
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("extractor: getwd: %w", err)
		}
		repoRoot = cwd
	}
	if baseRef == "" {
		baseRef = "origin/main"
	}
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("extractor: abs %q: %w", repoRoot, err)
	}
	if err := ensureGitWorkTree(abs); err != nil {
		return nil, err
	}
	return &GitDiffExtractor{RepoRoot: abs, BaseRef: baseRef}, nil
}

// Run parses a zero-context diff and emits only post-image added lines.
func (e *GitDiffExtractor) Run(ctx context.Context, out chan<- InputUnit, skips chan<- SkipEvent) error {
	defer close(out)
	defer close(skips)

	diff, err := e.diff(ctx)
	if err != nil {
		return err
	}
	return emitAddedLineUnits(ctx, out, diff, e.BaseRef, e.RepoRoot)
}

func (e *GitDiffExtractor) diff(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--unified=0", "--diff-filter=AM", "-M", e.BaseRef+"...HEAD")
	cmd.Dir = e.RepoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, &GitDiffError{BaseRef: e.BaseRef, Detail: strings.TrimSpace(stderr.String())}
	}
	return out, nil
}

var diffHunkRE = regexp.MustCompile(`@@ -[0-9]+(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@`)

func emitAddedLineUnits(ctx context.Context, out chan<- InputUnit, diff []byte, baseRef, repoRoot string) error {
	scanner := bufio.NewScanner(bytes.NewReader(diff))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var path string
	postLine := 0
	var hunk strings.Builder
	hunkStart := 0
	flush := func() error {
		if path == "" || hunkStart == 0 || hunk.Len() == 0 {
			hunk.Reset()
			hunkStart = 0
			return nil
		}
		unit := InputUnit{
			SourceID:     path,
			SourceKind:   "git_diff",
			LocationHint: path,
			Content:      []byte(hunk.String()),
			Encoding:     "utf-8",
			Metadata: map[string]string{
				"base_ref":  baseRef,
				"line":      strconv.Itoa(hunkStart),
				"scan_root": repoRoot,
			},
		}
		select {
		case out <- unit:
		case <-ctx.Done():
			return ctx.Err()
		}
		hunk.Reset()
		hunkStart = 0
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if strings.HasPrefix(line, "+++ ") {
			if err := flush(); err != nil {
				return err
			}
			path = parseDiffPath(line[4:])
			continue
		}
		if strings.HasPrefix(line, "@@ ") {
			if err := flush(); err != nil {
				return err
			}
			start, ok := parseHunkStart(line)
			if ok {
				postLine = start
			}
			continue
		}
		if path == "" || postLine == 0 {
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			if hunkStart == 0 {
				hunkStart = postLine
			}
			hunk.WriteString(strings.TrimPrefix(line, "+"))
			hunk.WriteByte('\n')
			postLine++
			continue
		}
		if strings.HasPrefix(line, " ") {
			if err := flush(); err != nil {
				return err
			}
			postLine++
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("extractor: parse git diff: %w", err)
	}
	return flush()
}

func parseDiffPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(raw, "b/") {
		return raw[2:]
	}
	return raw
}

func parseHunkStart(line string) (int, bool) {
	matches := diffHunkRE.FindStringSubmatch(line)
	if len(matches) != 2 {
		return 0, false
	}
	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

func ensureGitWorkTree(repoRoot string) error {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("extractor: %q is not a git working tree: %s", repoRoot, strings.TrimSpace(string(out)))
	}
	return nil
}

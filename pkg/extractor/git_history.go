package extractor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	SourceKindGitHistoryBlob   = "git_history_blob"
	SourceKindGitCommitMessage = "git_commit_message"
)

// GitBlobAttribution records one commit:path reference to a historical blob.
type GitBlobAttribution struct {
	Commit string
	Path   string
}

// GitHistoryStats reports enumeration counts for scan metadata.
type GitHistoryStats struct {
	HistoryBlobsScanned   int
	HistoryCommitsScanned int
	HistoryUniqueBlobs    int
}

// GitHistoryExtractor emits unique historical blob contents and/or commit
// messages for a local git repository. Blob findings are expanded back to
// commit:path attributions by the scan command after each unique blob is
// scanned once.
type GitHistoryExtractor struct {
	RepoRoot              string
	MaxFileSize           int64
	IncludeBlobs          bool
	IncludeCommitMessages bool
	SkipBinaryExt         map[string]bool

	attributions map[string][]GitBlobAttribution
	stats        GitHistoryStats
}

// GitHistoryOptions configures history extraction.
type GitHistoryOptions struct {
	MaxFileSize           int64
	IncludeBlobs          bool
	IncludeCommitMessages bool
}

// NewGitHistoryExtractor builds a git-history extractor rooted at repoRoot.
// If repoRoot is empty, cwd is used.
func NewGitHistoryExtractor(repoRoot string, opts GitHistoryOptions) (*GitHistoryExtractor, error) {
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("extractor: getwd: %w", err)
		}
		repoRoot = cwd
	}
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = DefaultMaxFileSize
	}
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("extractor: abs %q: %w", repoRoot, err)
	}
	if err := ensureGitRepository(abs); err != nil {
		return nil, err
	}
	return &GitHistoryExtractor{
		RepoRoot:              abs,
		MaxFileSize:           opts.MaxFileSize,
		IncludeBlobs:          opts.IncludeBlobs,
		IncludeCommitMessages: opts.IncludeCommitMessages,
		SkipBinaryExt:         DefaultBinaryExtensions(),
		attributions:          map[string][]GitBlobAttribution{},
	}, nil
}

func ensureGitRepository(repoRoot string) error {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("extractor: %q is not a git repository: %s", repoRoot, strings.TrimSpace(string(out)))
	}
	return nil
}

// Stats returns the latest enumeration statistics. It is meaningful after Run
// has completed.
func (e *GitHistoryExtractor) Stats() GitHistoryStats {
	return e.stats
}

// Run emits unique historical blob units and commit-message units.
func (e *GitHistoryExtractor) Run(ctx context.Context, out chan<- InputUnit, skips chan<- SkipEvent) error {
	defer close(out)
	defer close(skips)

	commits, err := e.listCommits(ctx)
	if err != nil {
		return err
	}
	e.stats.HistoryCommitsScanned = len(commits)

	if e.IncludeBlobs {
		if err := e.enumerateBlobAttributions(ctx, commits); err != nil {
			return err
		}
		e.stats.HistoryUniqueBlobs = len(e.attributions)
		if err := e.emitBlobUnits(ctx, out, skips); err != nil {
			return err
		}
	}
	if e.IncludeCommitMessages {
		if err := e.emitCommitMessageUnits(ctx, out, commits); err != nil {
			return err
		}
	}
	return nil
}

func (e *GitHistoryExtractor) listCommits(ctx context.Context) ([]string, error) {
	out, err := gitOutputBytes(ctx, e.RepoRoot, "rev-list", "--all")
	if err != nil {
		return nil, fmt.Errorf("extractor: git rev-list --all: %w", err)
	}
	commits := strings.Fields(string(out))
	sort.Strings(commits)
	return commits, nil
}

func (e *GitHistoryExtractor) enumerateBlobAttributions(ctx context.Context, commits []string) error {
	attributions := map[string][]GitBlobAttribution{}
	for _, commit := range commits {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		out, err := gitOutputBytes(ctx, e.RepoRoot, "ls-tree", "-r", "-z", "--full-tree", commit)
		if err != nil {
			return fmt.Errorf("extractor: git ls-tree %s: %w", commit, err)
		}
		records := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
		for _, record := range records {
			if len(record) == 0 {
				continue
			}
			blob, path, ok := parseLSTreeRecord(record)
			if !ok {
				continue
			}
			attributions[blob] = append(attributions[blob], GitBlobAttribution{
				Commit: commit,
				Path:   path,
			})
		}
	}
	for blob := range attributions {
		sort.Slice(attributions[blob], func(i, j int) bool {
			if attributions[blob][i].Commit != attributions[blob][j].Commit {
				return attributions[blob][i].Commit < attributions[blob][j].Commit
			}
			return attributions[blob][i].Path < attributions[blob][j].Path
		})
	}
	e.attributions = attributions
	return nil
}

func (e *GitHistoryExtractor) emitBlobUnits(ctx context.Context, out chan<- InputUnit, skips chan<- SkipEvent) error {
	blobs := make([]string, 0, len(e.attributions))
	for blob := range e.attributions {
		blobs = append(blobs, blob)
	}
	sort.Strings(blobs)

	for _, blob := range blobs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		attrs := e.attributions[blob]
		if len(attrs) == 0 {
			continue
		}
		eligible := make([]GitBlobAttribution, 0, len(attrs))
		for _, attr := range attrs {
			ext := strings.ToLower(filepath.Ext(attr.Path))
			if e.SkipBinaryExt[ext] {
				if err := emitSkip(ctx, skips, SkipEvent{
					SourceID:     attr.Path,
					LocationHint: attr.Path,
					Reason:       SkipBinaryDetected,
					Detail:       "extension " + ext,
				}); err != nil {
					return err
				}
				continue
			}
			eligible = append(eligible, attr)
		}
		if len(eligible) == 0 {
			continue
		}
		path := eligible[0].Path
		size, err := e.blobSize(ctx, blob)
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
		if size > e.MaxFileSize {
			if err := emitSkip(ctx, skips, SkipEvent{
				SourceID:     path,
				LocationHint: path,
				Reason:       SkipFileTooLarge,
				Detail:       fmt.Sprintf("size %d exceeds cap %d", size, e.MaxFileSize),
			}); err != nil {
				return err
			}
			continue
		}
		content, err := e.readBlob(ctx, blob)
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
		e.stats.HistoryBlobsScanned++
		unit := InputUnit{
			SourceID:            blob,
			SourceKind:          SourceKindGitHistoryBlob,
			LocationHint:        path,
			Content:             content,
			Encoding:            "utf-8",
			GitBlobAttributions: append([]GitBlobAttribution(nil), eligible...),
			Metadata: map[string]string{
				"blob_sha":      blob,
				"scan_root":     e.RepoRoot,
				"size_bytes":    strconv.FormatInt(size, 10),
				"suppress_path": "true",
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

func (e *GitHistoryExtractor) emitCommitMessageUnits(ctx context.Context, out chan<- InputUnit, commits []string) error {
	for _, commit := range commits {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		content, err := gitOutputBytes(ctx, e.RepoRoot, "log", "-1", "--format=%B", commit)
		if err != nil {
			return fmt.Errorf("extractor: git log %s: %w", commit, err)
		}
		unit := InputUnit{
			SourceID:     "commit_message",
			SourceKind:   SourceKindGitCommitMessage,
			LocationHint: commit,
			Content:      content,
			Encoding:     "utf-8",
			Metadata: map[string]string{
				"git_ref":   commit,
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

func (e *GitHistoryExtractor) blobSize(ctx context.Context, blob string) (int64, error) {
	out, err := gitOutputBytes(ctx, e.RepoRoot, "cat-file", "-s", blob)
	if err != nil {
		return 0, fmt.Errorf("git cat-file -s: %w", err)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse blob size: %w", err)
	}
	return size, nil
}

func (e *GitHistoryExtractor) readBlob(ctx context.Context, blob string) ([]byte, error) {
	return gitOutputBytes(ctx, e.RepoRoot, "cat-file", "blob", blob)
}

func parseLSTreeRecord(record []byte) (blob, path string, ok bool) {
	tab := bytes.IndexByte(record, '\t')
	if tab < 0 {
		return "", "", false
	}
	meta := string(record[:tab])
	fields := strings.Fields(meta)
	if len(fields) < 3 || fields[1] != "blob" {
		return "", "", false
	}
	path = string(record[tab+1:])
	if path == "" {
		return "", "", false
	}
	return fields[2], path, true
}

func gitOutputBytes(ctx context.Context, repoRoot string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

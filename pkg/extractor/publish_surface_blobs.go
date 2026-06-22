package extractor

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Publish-surface blob units reuse SourceKindGitHistoryBlob: they are
// historical blobs scanned the same way internal-brief scans them (content once with
// the path suppressed, path segments per attribution), so the engine path gate
// accepts them unchanged. The ref dimension is carried via attribution at the
// command layer, not a new source kind.

// PublishSurfaceStats reports enumeration counts for the audit report metadata.
type PublishSurfaceStats struct {
	RefsTotal      int
	RefsScanned    int
	CommitsScanned int
	UniqueBlobs    int
	BlobsScanned   int
}

// PublishPathRef pairs an eligible (non-binary) path with the refs whose
// history reaches this blob at that path. Path-segment findings for this path
// attribute to exactly these refs — preserving the per-path attribution that a
// single representative path would lose (a blob with clean bytes can still be a
// leak vector via a protected-looking path on a backup ref).
type PublishPathRef struct {
	Path string
	Refs []string
}

// PublishBlob is one unique blob on the publish surface. Content is scanned
// once and its findings attribute to every ref in Refs (the union); path
// findings are scanned per Paths entry and attribute to that entry's refs.
type PublishBlob struct {
	SHA   string
	Paths []PublishPathRef // eligible paths, sorted; per-path ref attribution
	Refs  []string         // union of Paths[].Refs — refs reaching this content
}

// LocationHint returns a deterministic representative path for redaction-safe
// logging of the content unit. Path attribution itself uses Paths, not this.
func (b PublishBlob) LocationHint() string {
	if len(b.Paths) == 0 {
		return b.SHA
	}
	return b.Paths[0].Path
}

// PublishSurfaceExtractor enumerates, per ref, the unique blobs reachable from
// that ref's tip, deduped into one union set scanned once. It composes the
// internal-brief unique-blob model and lifts it to the ref set, recording which refs
// reach each blob so the command can attribute findings back to refs.
//
// Only refs whose tip objects are present locally can be content-scanned; pass
// just those (RemoteRef.LocalObjects == true). Refs that exist on the remote
// but are not fetched are handled by the caller via name-pattern flagging.
type PublishSurfaceExtractor struct {
	RepoRoot      string
	MaxFileSize   int64
	Refs          []RemoteRef
	SkipBinaryExt map[string]bool

	blobs []PublishBlob
	skips []SkipEvent
	stats PublishSurfaceStats
}

// NewPublishSurfaceExtractor builds an extractor for the given scannable refs.
func NewPublishSurfaceExtractor(repoRoot string, refs []RemoteRef, maxFileSize int64) (*PublishSurfaceExtractor, error) {
	if repoRoot == "" {
		return nil, fmt.Errorf("publish-surface: repo root is required")
	}
	if maxFileSize <= 0 {
		maxFileSize = DefaultMaxFileSize
	}
	if err := ensureGitWorkTree(repoRoot); err != nil {
		return nil, err
	}
	return &PublishSurfaceExtractor{
		RepoRoot:      repoRoot,
		MaxFileSize:   maxFileSize,
		Refs:          refs,
		SkipBinaryExt: DefaultBinaryExtensions(),
	}, nil
}

// Enumerate builds the deduped blob→refs attribution and the scannable-blob
// list. It runs `rev-list` per ref and `ls-tree` per unique commit, so each
// unique commit's tree is read once and each unique blob is scanned once.
func (e *PublishSurfaceExtractor) Enumerate(ctx context.Context) error {
	e.stats.RefsTotal = len(e.Refs)

	// commitRefs: commit SHA -> set of ref names whose history includes it.
	commitRefs := map[string]map[string]bool{}
	for _, ref := range e.Refs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		commits, err := e.revList(ctx, ref.Tip)
		if err != nil {
			return err
		}
		e.stats.RefsScanned++
		for _, c := range commits {
			set := commitRefs[c]
			if set == nil {
				set = map[string]bool{}
				commitRefs[c] = set
			}
			set[ref.Name] = true
		}
	}
	e.stats.CommitsScanned = len(commitRefs)

	// Walk each unique commit's tree once. Accumulate, per blob, the set of refs
	// that reach it AT EACH path (blob -> path -> ref set). Retaining the path
	// dimension is what lets path-segment findings attribute to the right refs.
	blobPathRefs := map[string]map[string]map[string]bool{}
	for _, commit := range sortedKeys(commitRefs) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		entries, err := e.lsTree(ctx, commit)
		if err != nil {
			return err
		}
		for blob, paths := range entries {
			pathRefs := blobPathRefs[blob]
			if pathRefs == nil {
				pathRefs = map[string]map[string]bool{}
				blobPathRefs[blob] = pathRefs
			}
			for _, p := range paths {
				refSet := pathRefs[p]
				if refSet == nil {
					refSet = map[string]bool{}
					pathRefs[p] = refSet
				}
				for r := range commitRefs[commit] {
					refSet[r] = true
				}
			}
		}
	}
	e.stats.UniqueBlobs = len(blobPathRefs)

	// Filter to scannable blobs (≥1 non-binary path, within the size cap) and
	// record skips. Deterministic ordering by blob SHA, then path.
	for _, blob := range sortedKeys(blobPathRefs) {
		pathRefs := blobPathRefs[blob]
		var paths []PublishPathRef
		unionRefs := map[string]bool{}
		for _, p := range sortedKeys(pathRefs) {
			if e.SkipBinaryExt[strings.ToLower(filepath.Ext(p))] {
				continue // binary path: not a scan surface
			}
			refs := sortedKeys(pathRefs[p])
			paths = append(paths, PublishPathRef{Path: p, Refs: refs})
			for _, r := range refs {
				unionRefs[r] = true
			}
		}
		if len(paths) == 0 {
			e.skips = append(e.skips, SkipEvent{
				SourceID:     blob,
				LocationHint: blob,
				Reason:       SkipBinaryDetected,
				Detail:       "all paths binary",
			})
			continue
		}
		hint := paths[0].Path
		size, err := e.blobSize(ctx, blob)
		if err != nil {
			e.skips = append(e.skips, SkipEvent{SourceID: hint, LocationHint: hint, Reason: SkipUnreadable, Detail: err.Error()})
			continue
		}
		if size > e.MaxFileSize {
			e.skips = append(e.skips, SkipEvent{
				SourceID:     hint,
				LocationHint: hint,
				Reason:       SkipFileTooLarge,
				Detail:       fmt.Sprintf("size %d exceeds cap %d", size, e.MaxFileSize),
			})
			continue
		}
		e.blobs = append(e.blobs, PublishBlob{SHA: blob, Paths: paths, Refs: sortedKeys(unionRefs)})
	}
	e.stats.BlobsScanned = len(e.blobs)
	return nil
}

// Blobs returns the scannable unique blobs (after Enumerate).
func (e *PublishSurfaceExtractor) Blobs() []PublishBlob { return e.blobs }

// Skips returns the skip events recorded during enumeration.
func (e *PublishSurfaceExtractor) Skips() []SkipEvent { return e.skips }

// Stats returns enumeration counts.
func (e *PublishSurfaceExtractor) Stats() PublishSurfaceStats { return e.stats }

// ReadBlob returns the content of a blob for scanning.
func (e *PublishSurfaceExtractor) ReadBlob(ctx context.Context, sha string) ([]byte, error) {
	return gitOutputBytes(ctx, e.RepoRoot, "cat-file", "blob", sha)
}

func (e *PublishSurfaceExtractor) revList(ctx context.Context, tip string) ([]string, error) {
	out, err := gitOutputBytes(ctx, e.RepoRoot, "rev-list", tip)
	if err != nil {
		return nil, fmt.Errorf("publish-surface: git rev-list %s: %w", tip, err)
	}
	return strings.Fields(string(out)), nil
}

func (e *PublishSurfaceExtractor) lsTree(ctx context.Context, commit string) (map[string][]string, error) {
	out, err := gitOutputBytes(ctx, e.RepoRoot, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return nil, fmt.Errorf("publish-surface: git ls-tree %s: %w", commit, err)
	}
	entries := map[string][]string{}
	for _, record := range bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0}) {
		if len(record) == 0 {
			continue
		}
		blob, path, ok := parseLSTreeRecord(record)
		if !ok {
			continue
		}
		entries[blob] = append(entries[blob], path)
	}
	return entries, nil
}

func (e *PublishSurfaceExtractor) blobSize(ctx context.Context, blob string) (int64, error) {
	out, err := gitOutputBytes(ctx, e.RepoRoot, "cat-file", "-s", blob)
	if err != nil {
		return 0, fmt.Errorf("git cat-file -s: %w", err)
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

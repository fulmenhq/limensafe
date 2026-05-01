package extractor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DefaultMaxFileSize is the default per-file cap for v0. Files
// exceeding this emit a SkipEvent with reason SkipFileTooLarge.
// 10MB strikes the balance between catching realistic source/doc
// files (typical max ~100KB) and avoiding OOM on giant logs/binaries.
// v0.x will add chunk-streaming for files between ~1MB and the cap.
const DefaultMaxFileSize int64 = 10 * 1024 * 1024

// FilesystemExtractor walks a directory tree and emits one InputUnit
// per readable file. It does not parse content — that's the engine's
// job — but it does enforce size caps and basic skip rules.
//
// V0 SKIP RULES (deliberately minimal):
//   - directories are not emitted (only their files)
//   - the .git/ tree is always skipped (vcs internals)
//   - files exceeding MaxFileSize emit a SkipEvent and continue
//   - symlinks are not followed (FollowSymlinks=false default)
//   - obvious binary extensions are skipped (.exe, .bin, .so, ...)
//
// .gitignore / .contextsafeignore / .limensafeignore awareness lives
// in v0.x — for the spike, walk everything not matched by the static
// skip rules.
type FilesystemExtractor struct {
	Root           string
	MaxFileSize    int64
	FollowSymlinks bool
	// SkipBinaryExt is a closed extension set; default covers the
	// common offenders. Caller can override to nil to disable.
	SkipBinaryExt map[string]bool
}

// DefaultBinaryExtensions returns the v0 closed-set of file extensions
// the filesystem extractor skips. Conservative by design; a full
// content-sniff is v0.x.
func DefaultBinaryExtensions() map[string]bool {
	return map[string]bool{
		".exe": true, ".bin": true, ".so": true, ".dylib": true,
		".dll": true, ".class": true, ".jar": true, ".o": true, ".a": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
		".ico": true, ".pdf": true, ".zip": true, ".tar": true, ".gz": true,
		".tgz": true, ".bz2": true, ".7z": true, ".mp4": true, ".mp3": true,
		".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	}
}

// NewFilesystemExtractor returns a configured filesystem extractor.
// Returns an error if root is empty or does not exist.
func NewFilesystemExtractor(root string, maxFileSize int64) (*FilesystemExtractor, error) {
	if root == "" {
		return nil, invalidConfig("filesystem: root is required")
	}
	if maxFileSize <= 0 {
		maxFileSize = DefaultMaxFileSize
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("extractor: stat root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, invalidConfig("filesystem: root %q is not a directory", root)
	}
	return &FilesystemExtractor{
		Root:          root,
		MaxFileSize:   maxFileSize,
		SkipBinaryExt: DefaultBinaryExtensions(),
	}, nil
}

// Run walks the tree under Root, emitting InputUnits to out and
// SkipEvents to skips. Both channels are closed before Run returns.
// Returns the first fatal walk error encountered (channel callers
// should still drain whatever was emitted).
func (e *FilesystemExtractor) Run(ctx context.Context, out chan<- InputUnit, skips chan<- SkipEvent) error {
	defer close(out)
	defer close(skips)

	rootAbs, err := filepath.Abs(e.Root)
	if err != nil {
		return fmt.Errorf("extractor: abs root: %w", err)
	}

	return filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		// Honor cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if walkErr != nil {
			// Surface as skip but keep walking.
			rel := relOrBase(rootAbs, path)
			select {
			case skips <- SkipEvent{
				SourceID: rel, LocationHint: rel,
				Reason: SkipUnreadable, Detail: walkErr.Error(),
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}

		// Skip the .git tree wholesale.
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// Symlinks: respect FollowSymlinks.
		if d.Type()&fs.ModeSymlink != 0 && !e.FollowSymlinks {
			rel := relOrBase(rootAbs, path)
			select {
			case skips <- SkipEvent{SourceID: rel, LocationHint: rel, Reason: SkipIgnored, Detail: "symlink"}:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}

		// Binary-extension skip.
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if e.SkipBinaryExt[ext] {
			rel := relOrBase(rootAbs, path)
			select {
			case skips <- SkipEvent{SourceID: rel, LocationHint: rel, Reason: SkipBinaryDetected, Detail: "extension " + ext}:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			rel := relOrBase(rootAbs, path)
			select {
			case skips <- SkipEvent{SourceID: rel, LocationHint: rel, Reason: SkipUnreadable, Detail: err.Error()}:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}

		// Size cap (Dave's streaming caution).
		if info.Size() > e.MaxFileSize {
			rel := relOrBase(rootAbs, path)
			select {
			case skips <- SkipEvent{
				SourceID: rel, LocationHint: rel,
				Reason: SkipFileTooLarge,
				Detail: fmt.Sprintf("size %d exceeds cap %d", info.Size(), e.MaxFileSize),
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			rel := relOrBase(rootAbs, path)
			select {
			case skips <- SkipEvent{SourceID: rel, LocationHint: rel, Reason: SkipUnreadable, Detail: err.Error()}:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}

		rel := relOrBase(rootAbs, path)
		unit := InputUnit{
			SourceID:     rel,
			SourceKind:   "file",
			LocationHint: rel,
			Content:      content,
			Encoding:     "utf-8", // v0 assumption; v0.x sniffs
			Metadata: map[string]string{
				"size_bytes": fmt.Sprintf("%d", info.Size()),
				"scan_root":  rootAbs,
			},
		}

		select {
		case out <- unit:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
}

// relOrBase returns a clean scan-root-relative path. If filepath.Rel
// fails (path outside root), falls back to the file's base name.
func relOrBase(rootAbs, path string) string {
	rel, err := filepath.Rel(rootAbs, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Base(path)
	}
	return rel
}

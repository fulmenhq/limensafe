// Package extractor produces InputUnits from various sources for the
// limensafe engine to scan. v0 implementations: filesystem walker.
// Planned: git (staged diff, branch, commit-msg), structured-text
// (JSON/YAML/Markdown), notebooks, office docs, etc.
//
// All extractors emit a stream of InputUnits over a channel so the
// engine can fan out to a bounded worker pool per entarch's
// concurrency model (architecture.md "Source-Agnostic Input Contract").
package extractor

import (
	"context"
	"fmt"
)

// InputUnit is the engine's read unit. One per file (filesystem),
// hunk (git diff), branch ref (git surface), commit message, etc.
//
// V0: defined here as the canonical interchange. When entarch's
// pkg/engine commits its own InputUnit type, this becomes a
// type alias or this struct moves to pkg/engine and is imported here.
// The fields and semantics are stable; the package home is the only
// open question.
type InputUnit struct {
	// SourceID uniquely identifies this unit within the run. For
	// filesystem extractor this is the scan-root-relative path; for
	// git surfaces it's a synthetic identifier like "branch_name" or
	// "commit_message".
	SourceID string

	// SourceKind names the producing extractor: "file", "git_diff",
	// "branch_name", "commit_message", "stdin", "api_body", ...
	SourceKind string

	// LocationHint is a human-readable source label used in non-redacted
	// internal logs. The output formatter applies redaction to anything
	// derived from this before serialization.
	LocationHint string

	// Content is the bytes to scan. For v0, the filesystem extractor
	// reads the whole file (capped by MaxFileSize) into Content. v0.x
	// will introduce streaming for larger files.
	Content []byte

	// Encoding hints at how to interpret Content: "utf-8", "binary",
	// "extracted-text", etc. v0 only emits utf-8 text from filesystem.
	Encoding string

	// Metadata carries extractor-specific context: "scan_root",
	// "file_mode", "size_bytes", etc. Free-form string map.
	Metadata map[string]string
}

// SkipReason explains why an extractor skipped a candidate. Used to
// emit warnings without producing a normal InputUnit.
type SkipReason int

const (
	SkipUnknown SkipReason = iota
	SkipFileTooLarge
	SkipBinaryDetected
	SkipUnreadable
	SkipIgnored
)

// String renders a SkipReason for log/output use.
func (s SkipReason) String() string {
	switch s {
	case SkipFileTooLarge:
		return "file_too_large"
	case SkipBinaryDetected:
		return "binary_detected"
	case SkipUnreadable:
		return "unreadable"
	case SkipIgnored:
		return "ignored"
	default:
		return "unknown"
	}
}

// SkipEvent is emitted when an extractor decides not to produce an
// InputUnit for a candidate source. The scan summary aggregates these
// (FilesSkipped count + reason breakdown).
type SkipEvent struct {
	SourceID     string
	LocationHint string
	Reason       SkipReason
	Detail       string
}

// Extractor is the common interface. Run drives a producer goroutine
// that emits InputUnits to out and SkipEvents to skips, then closes
// both channels when done. Errors returned indicate fatal extractor
// failure (cannot proceed); per-unit issues are reported via
// SkipEvents.
//
// Cancellation: respect ctx.Done() to support clean shutdown.
type Extractor interface {
	Run(ctx context.Context, out chan<- InputUnit, skips chan<- SkipEvent) error
}

// errInvalidConfig is returned when an extractor is constructed with
// inputs that cannot produce any work.
type errInvalidConfig struct{ msg string }

func (e *errInvalidConfig) Error() string { return e.msg }

func invalidConfig(format string, args ...any) error {
	return &errInvalidConfig{msg: fmt.Sprintf("extractor: %s", fmt.Sprintf(format, args...))}
}

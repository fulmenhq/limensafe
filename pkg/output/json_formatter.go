package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

// SchemaVersion is the semantic version of the scan output JSON Schema
// (schemas/limensafe/v1.1.0/scan-output.schema.json) this build emits,
// surfaced on every document as scan_metadata.output_schema_version.
//
// This is the authoritative parser discriminator for "which output shape
// am I reading?" (internal-brief). Consumers branch on output_schema_version —
// NOT on tool_version (the CLI build version, which moves independently)
// nor the coarse top-level `version` ("v0", retained for back-compat).
// A field add/rename/retype bumps this per semver. See CONTRIBUTING.md
// "Scan output contract".
//
// 1.1.0 (internal-brief): adds allowlist suppression accounting
// (allowlist_suppressions + allowlist_suppressions_by_entry) to scan_metadata.
// Minor, additive bump; maps to schemas/limensafe/v1.1.0/scan-output.schema.json.
const SchemaVersion = "1.1.0"

// Output is the top-level JSON output contract. The full document is
// pinned by schemas/limensafe/v1.1.0/scan-output.schema.json and
// versioned via scan_metadata.output_schema_version (internal-brief).
//
// The top-level `version` field ("v0") is the original coarse generation
// marker, kept stable for back-compat; output_schema_version is the
// precise semver discriminator that maps to the schema directory.
type Output struct {
	Version      string       `json:"version"`
	ScanMetadata ScanMetadata `json:"scan_metadata"`
	Summary      ScanSummary  `json:"summary"`
	Findings     []Finding    `json:"findings"`
}

// ScanMetadata describes the run that produced the findings.
//
// Nullability (internal-brief): the core scan counters (worker_count,
// files_scanned, bytes_scanned, files_skipped, directories_skipped),
// files_skipped_by_reason, and the internal-brief allowlist suppression counters
// (allowlist_suppressions, allowlist_suppressions_by_entry) are emitted
// present-with-zero/empty rather than omitted, so jq/CI consumers read a
// stable integer (or {}) instead of null on a clean scan. Mode-specific
// fields (scan_root_kind, git_ref, history_*, private_catalogs_status) stay
// omitempty by design.
type ScanMetadata struct {
	OutputSchemaVersion       string                 `json:"output_schema_version"`
	ToolVersion               string                 `json:"tool_version"`
	StartedAt                 time.Time              `json:"started_at"`
	DurationMS                int64                  `json:"duration_ms"`
	ScanRoot                  string                 `json:"scan_root"`
	ScanRootKind              string                 `json:"scan_root_kind,omitempty"`
	GitRef                    string                 `json:"git_ref,omitempty"`
	Visibility                string                 `json:"visibility"`
	WorkerCount               int                    `json:"worker_count"`
	FilesScanned              int                    `json:"files_scanned"`
	BytesScanned              int64                  `json:"bytes_scanned"`
	FilesSkipped              int                    `json:"files_skipped"`
	DirsSkipped               int                    `json:"directories_skipped"`
	SkippedByReason           map[string]int         `json:"files_skipped_by_reason"`
	AllowlistSuppressions     int                    `json:"allowlist_suppressions"`
	AllowlistSuppressionsByID map[string]int         `json:"allowlist_suppressions_by_entry"`
	HistoryBlobsScanned       int                    `json:"history_blobs_scanned,omitempty"`
	HistoryCommitsScanned     int                    `json:"history_commits_scanned,omitempty"`
	HistoryUniqueBlobs        int                    `json:"history_unique_blobs,omitempty"`
	CatalogsLoaded            []CatalogLoadStatus    `json:"catalogs_loaded"`
	PrivateCatalogsStatus     []PrivateCatalogStatus `json:"private_catalogs_status,omitempty"`
}

// CatalogLoadStatus reports per-catalog load outcome. Catalog source
// paths are deliberately excluded — only IDs and load status emit.
type CatalogLoadStatus struct {
	CatalogID  string `json:"catalog_id"`
	LoadStatus string `json:"load_status"`
	SourceKind string `json:"source_kind,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// PrivateCatalogStatus reports optional/private catalog posture without
// emitting local paths, env values, or other operator-private source detail.
type PrivateCatalogStatus struct {
	CatalogID  string `json:"catalog_id"`
	SourceKind string `json:"source_kind"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
}

// ScanSummary is the aggregate counts.
type ScanSummary struct {
	FindingsTotal int            `json:"findings_total"`
	BySeverity    map[string]int `json:"by_severity"`
	BySurface     map[string]int `json:"by_surface"`
}

// Finding is the redaction-safe shape emitted in v0 JSON output. The
// fields and their semantics match docs/design/architecture.md
// "Finding Model" + ADR-0003 "Replacement marker form".
//
// IMPORTANT: Finding's string fields are passed through the Redactor
// before emission. Catalog/entity/rule/replacement IDs are guaranteed
// alias-safe by the ID-safety rule (see catalog-schema.md), so they
// emit unchanged. Path and message fields may contain alias substrings
// from the underlying scan target and are redacted.
type Finding struct {
	// Kind is the finding discriminator and is always emitted ("detection"
	// or "config-warning") — the schema requires it (internal-brief). No omitempty:
	// a finding that reached output without a kind is a contract regression,
	// and emitting "" makes it fail schema validation loudly rather than
	// silently dropping the discriminator.
	Kind          string   `json:"kind"`
	ID            string   `json:"id"`
	Fingerprint   string   `json:"fingerprint"`
	Severity      string   `json:"severity"`
	Confidence    string   `json:"confidence"`
	Decision      string   `json:"decision"`
	EntityID      string   `json:"entity_id,omitempty"`
	EntityClass   string   `json:"entity_class"`
	DetectorID    string   `json:"detector_id"`
	RuleID        string   `json:"rule_id,omitempty"`
	SourceKind    string   `json:"source_kind"`
	Surface       string   `json:"surface"`
	Location      Location `json:"location"`
	ReplacementID string   `json:"replacement_id,omitempty"`
	EvidenceShape string   `json:"evidence_shape"`
	Message       string   `json:"message"`
}

// Location records where the finding was observed. For surfaces other
// than file content (branch_name, commit_message, ...), Path is empty
// and SourceID identifies the surface.
type Location struct {
	Path        string `json:"path,omitempty"`
	SourceID    string `json:"source_id,omitempty"`
	Line        int    `json:"line,omitempty"`
	Column      int    `json:"column,omitempty"`
	GitRef      string `json:"git_ref,omitempty"`
	SurfaceKind string `json:"surface_kind,omitempty"`
}

// JSONFormatter implements the redaction-safe JSON output writer per
// ADR-0003. Construct with NewJSONFormatter; call Emit to write a
// complete Output document. The formatter uses the supplied Redactor
// for every string field on the path between caller and json.Encoder.
//
// Concurrency: a JSONFormatter is not safe for concurrent Emit calls.
// Callers should construct one per output stream.
type JSONFormatter struct {
	w        io.Writer
	redactor *Redactor
}

// NewJSONFormatter wraps w with a redactor. If redactor is nil, the
// formatter passes string fields through unchanged — tests and dev
// pipelines may opt into this; CI must not. Production callers
// always pass a non-nil Redactor.
func NewJSONFormatter(w io.Writer, redactor *Redactor) *JSONFormatter {
	return &JSONFormatter{w: w, redactor: redactor}
}

// Emit writes out as JSON with deterministic indentation and finding
// order. Findings are sorted by (path, surface, line, column,
// detector_id) so concurrent scans produce reproducible output.
func (f *JSONFormatter) Emit(out Output) error {
	if f == nil || f.w == nil {
		return fmt.Errorf("output: nil JSONFormatter")
	}

	redacted := f.redactOutput(out)
	sortFindings(redacted.Findings)

	enc := json.NewEncoder(f.w)
	enc.SetIndent("", "  ")
	// Disable HTML escaping so redaction markers (`<r:entity_id>`) render
	// as literal angle brackets rather than </>. Output goes
	// to terminals/JSON files, never directly to a browser context.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(redacted); err != nil {
		return fmt.Errorf("output: encode: %w", err)
	}
	return nil
}

func (f *JSONFormatter) redactOutput(out Output) Output {
	r := f.redactor
	red := func(s string) string {
		if r == nil {
			return s
		}
		return r.Redact(s)
	}

	findings := make([]Finding, len(out.Findings))
	for i, fnd := range out.Findings {
		findings[i] = Finding{
			Kind:        fnd.Kind,
			ID:          fnd.ID,
			Fingerprint: fnd.Fingerprint,
			Severity:    fnd.Severity,
			Confidence:  fnd.Confidence,
			Decision:    fnd.Decision,
			EntityID:    fnd.EntityID,
			EntityClass: fnd.EntityClass,
			DetectorID:  fnd.DetectorID,
			RuleID:      fnd.RuleID,
			SourceKind:  fnd.SourceKind,
			Surface:     fnd.Surface,
			Location: Location{
				Path:        red(fnd.Location.Path),
				SourceID:    red(fnd.Location.SourceID),
				Line:        fnd.Location.Line,
				Column:      fnd.Location.Column,
				GitRef:      red(fnd.Location.GitRef),
				SurfaceKind: fnd.Location.SurfaceKind,
			},
			ReplacementID: fnd.ReplacementID,
			EvidenceShape: fnd.EvidenceShape,
			Message:       red(fnd.Message),
		}
	}

	return Output{
		Version: out.Version,
		ScanMetadata: ScanMetadata{
			OutputSchemaVersion:       out.ScanMetadata.OutputSchemaVersion,
			ToolVersion:               out.ScanMetadata.ToolVersion,
			StartedAt:                 out.ScanMetadata.StartedAt,
			DurationMS:                out.ScanMetadata.DurationMS,
			ScanRoot:                  red(out.ScanMetadata.ScanRoot),
			ScanRootKind:              out.ScanMetadata.ScanRootKind,
			GitRef:                    red(out.ScanMetadata.GitRef),
			Visibility:                out.ScanMetadata.Visibility,
			WorkerCount:               out.ScanMetadata.WorkerCount,
			FilesScanned:              out.ScanMetadata.FilesScanned,
			BytesScanned:              out.ScanMetadata.BytesScanned,
			FilesSkipped:              out.ScanMetadata.FilesSkipped,
			DirsSkipped:               out.ScanMetadata.DirsSkipped,
			SkippedByReason:           copyStringIntMap(out.ScanMetadata.SkippedByReason),
			AllowlistSuppressions:     out.ScanMetadata.AllowlistSuppressions,
			AllowlistSuppressionsByID: copyStringIntMap(out.ScanMetadata.AllowlistSuppressionsByID),
			HistoryBlobsScanned:       out.ScanMetadata.HistoryBlobsScanned,
			HistoryCommitsScanned:     out.ScanMetadata.HistoryCommitsScanned,
			HistoryUniqueBlobs:        out.ScanMetadata.HistoryUniqueBlobs,
			CatalogsLoaded:            out.ScanMetadata.CatalogsLoaded,
			PrivateCatalogsStatus: copyPrivateCatalogStatus(
				out.ScanMetadata.PrivateCatalogsStatus,
			),
		},
		Summary:  out.Summary,
		Findings: findings,
	}
}

func copyPrivateCatalogStatus(in []PrivateCatalogStatus) []PrivateCatalogStatus {
	if len(in) == 0 {
		return nil
	}
	out := make([]PrivateCatalogStatus, len(in))
	copy(out, in)
	return out
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Location.GitRef != findings[j].Location.GitRef {
			return findings[i].Location.GitRef < findings[j].Location.GitRef
		}
		if findings[i].Location.Path != findings[j].Location.Path {
			return findings[i].Location.Path < findings[j].Location.Path
		}
		if findings[i].Location.SurfaceKind != findings[j].Location.SurfaceKind {
			return findings[i].Location.SurfaceKind < findings[j].Location.SurfaceKind
		}
		if findings[i].Location.SourceID != findings[j].Location.SourceID {
			return findings[i].Location.SourceID < findings[j].Location.SourceID
		}
		if findings[i].Surface != findings[j].Surface {
			return findings[i].Surface < findings[j].Surface
		}
		if findings[i].Location.Line != findings[j].Location.Line {
			return findings[i].Location.Line < findings[j].Location.Line
		}
		if findings[i].Location.Column != findings[j].Location.Column {
			return findings[i].Location.Column < findings[j].Location.Column
		}
		return findings[i].DetectorID < findings[j].DetectorID
	})
}

// copyStringIntMap returns a defensive copy. It always returns a
// non-nil map so files_skipped_by_reason serializes as `{}` (not null)
// on a clean scan — the stable present-with-empty contract (internal-brief).
func copyStringIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

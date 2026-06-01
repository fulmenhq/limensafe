package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

// Output is the top-level v0 JSON output contract. Shape matches
// docs/design/v0-spike-plan.md §3.
//
// The fields here are the surface limensafe agrees to keep stable
// for v0; additional fields land in v0.x with explicit semver bumps
// and forward-compatibility guarantees.
type Output struct {
	Version      string       `json:"version"`
	ScanMetadata ScanMetadata `json:"scan_metadata"`
	Summary      ScanSummary  `json:"summary"`
	Findings     []Finding    `json:"findings"`
}

// ScanMetadata describes the run that produced the findings.
type ScanMetadata struct {
	ToolVersion     string              `json:"tool_version"`
	StartedAt       time.Time           `json:"started_at"`
	DurationMS      int64               `json:"duration_ms"`
	ScanRoot        string              `json:"scan_root"`
	Visibility      string              `json:"visibility"`
	WorkerCount     int                 `json:"worker_count,omitempty"`
	FilesScanned    int                 `json:"files_scanned,omitempty"`
	BytesScanned    int64               `json:"bytes_scanned,omitempty"`
	FilesSkipped    int                 `json:"files_skipped,omitempty"`
	DirsSkipped     int                 `json:"directories_skipped,omitempty"`
	SkippedByReason map[string]int      `json:"files_skipped_by_reason,omitempty"`
	CatalogsLoaded  []CatalogLoadStatus `json:"catalogs_loaded"`
}

// CatalogLoadStatus reports per-catalog load outcome. Catalog source
// paths are deliberately excluded — only IDs and load status emit.
type CatalogLoadStatus struct {
	CatalogID  string `json:"catalog_id"`
	LoadStatus string `json:"load_status"`
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
	ID            string   `json:"id"`
	Fingerprint   string   `json:"fingerprint"`
	Severity      string   `json:"severity"`
	Confidence    string   `json:"confidence"`
	Decision      string   `json:"decision"`
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
	Path     string `json:"path,omitempty"`
	SourceID string `json:"source_id,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
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
			ID:          fnd.ID,
			Fingerprint: fnd.Fingerprint,
			Severity:    fnd.Severity,
			Confidence:  fnd.Confidence,
			Decision:    fnd.Decision,
			EntityClass: fnd.EntityClass,
			DetectorID:  fnd.DetectorID,
			RuleID:      fnd.RuleID,
			SourceKind:  fnd.SourceKind,
			Surface:     fnd.Surface,
			Location: Location{
				Path:     red(fnd.Location.Path),
				SourceID: red(fnd.Location.SourceID),
				Line:     fnd.Location.Line,
				Column:   fnd.Location.Column,
			},
			ReplacementID: fnd.ReplacementID,
			EvidenceShape: fnd.EvidenceShape,
			Message:       red(fnd.Message),
		}
	}

	return Output{
		Version: out.Version,
		ScanMetadata: ScanMetadata{
			ToolVersion:     out.ScanMetadata.ToolVersion,
			StartedAt:       out.ScanMetadata.StartedAt,
			DurationMS:      out.ScanMetadata.DurationMS,
			ScanRoot:        red(out.ScanMetadata.ScanRoot),
			Visibility:      out.ScanMetadata.Visibility,
			WorkerCount:     out.ScanMetadata.WorkerCount,
			FilesScanned:    out.ScanMetadata.FilesScanned,
			BytesScanned:    out.ScanMetadata.BytesScanned,
			FilesSkipped:    out.ScanMetadata.FilesSkipped,
			DirsSkipped:     out.ScanMetadata.DirsSkipped,
			SkippedByReason: copyStringIntMap(out.ScanMetadata.SkippedByReason),
			CatalogsLoaded:  out.ScanMetadata.CatalogsLoaded,
		},
		Summary:  out.Summary,
		Findings: findings,
	}
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Location.Path != findings[j].Location.Path {
			return findings[i].Location.Path < findings[j].Location.Path
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

func copyStringIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

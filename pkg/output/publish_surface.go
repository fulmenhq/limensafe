package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/fulmenhq/limensafe/pkg/coverage"
)

// PublishSurfaceOutputSchemaVersion is the version of the publish-surface
// output document (distinct from the scan-output schema). Bump on any
// structural change; consumers parse this as the discriminator.
const PublishSurfaceOutputSchemaVersion = "1.1.0"

// PublishSurfaceOutput is the audit-publish report document written to stdout.
// It is a distinct schema from the scan Output: the unit is ref topology, not a
// content surface, and the headline is a go/no-go publish verdict.
type PublishSurfaceOutput struct {
	Version               string                 `json:"version"`
	OutputSchemaVersion   string                 `json:"output_schema_version"`
	ToolVersion           string                 `json:"tool_version"`
	StartedAt             time.Time              `json:"started_at"`
	DurationMS            int64                  `json:"duration_ms"`
	Remote                string                 `json:"remote"`
	PrimaryRef            string                 `json:"primary_ref"`
	Visibility            string                 `json:"visibility"`
	CatalogsLoaded        []CatalogLoadStatus    `json:"catalogs_loaded"`
	PrivateCatalogsStatus []PrivateCatalogStatus `json:"private_catalogs_status,omitempty"`
	Surface               PublishSurfaceCounts   `json:"surface"`
	Summary               PublishSummary         `json:"summary"`
	Refs                  []PublishRef           `json:"refs"`
}

// PublishSurfaceCounts reports enumeration metadata for the audit.
type PublishSurfaceCounts struct {
	RefsTotal       int  `json:"refs_total"`
	RefsScanned     int  `json:"refs_scanned"`
	PrimaryScanned  bool `json:"primary_scanned"`
	LeakVectorCount int  `json:"leak_vector_count"`
	UniqueBlobs     int  `json:"unique_blobs"`
	BlobsScanned    int  `json:"blobs_scanned"`
	CommitsScanned  int  `json:"commits_scanned"`
}

// PublishSummary is the go/no-go headline.
type PublishSummary struct {
	Coverage       coverage.Summary `json:"coverage"`
	PublishSafe    bool             `json:"publish_safe"`
	LeakVectorRefs []string         `json:"leak_vector_refs"`
	FindingsTotal  int              `json:"findings_total"`
	BySeverity     map[string]int   `json:"by_severity"`
}

// PublishRef is the per-ref entry of the publish surface.
type PublishRef struct {
	Name               string         `json:"name"`
	Kind               string         `json:"kind"`
	Tip                string         `json:"tip"`
	Scanned            bool           `json:"scanned"`
	LeakVector         bool           `json:"leak_vector"`
	Reasons            []string       `json:"reasons,omitempty"`
	SuggestedAction    string         `json:"suggested_action,omitempty"`
	FindingsBySeverity map[string]int `json:"findings_by_severity"`
	Findings           []Finding      `json:"findings,omitempty"`
}

// PublishSurfaceFormatter writes a PublishSurfaceOutput as redaction-safe JSON.
// Like JSONFormatter it routes every string field through the Redactor at the
// emit boundary (ADR-0003) — including ref names and suggested_action, which
// are operator-controlled and may embed protected vocabulary.
type PublishSurfaceFormatter struct {
	w        io.Writer
	redactor *Redactor
}

// NewPublishSurfaceFormatter wraps w with a redactor (nil = passthrough for
// tests/dev only; production always passes a non-nil Redactor).
func NewPublishSurfaceFormatter(w io.Writer, redactor *Redactor) *PublishSurfaceFormatter {
	return &PublishSurfaceFormatter{w: w, redactor: redactor}
}

// Emit writes out as deterministic, redaction-safe JSON.
func (f *PublishSurfaceFormatter) Emit(out PublishSurfaceOutput) error {
	if f == nil || f.w == nil {
		return fmt.Errorf("output: nil PublishSurfaceFormatter")
	}
	redacted := f.redact(out)
	enc := json.NewEncoder(f.w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(redacted); err != nil {
		return fmt.Errorf("output: encode publish-surface: %w", err)
	}
	return nil
}

func (f *PublishSurfaceFormatter) redact(out PublishSurfaceOutput) PublishSurfaceOutput {
	red := func(s string) string {
		if f.redactor == nil {
			return s
		}
		return f.redactor.Redact(s)
	}

	leakRefs := make([]string, len(out.Summary.LeakVectorRefs))
	for i, r := range out.Summary.LeakVectorRefs {
		leakRefs[i] = red(r)
	}

	refs := make([]PublishRef, len(out.Refs))
	for i, ref := range out.Refs {
		findings := make([]Finding, len(ref.Findings))
		for j, fnd := range ref.Findings {
			findings[j] = redactFinding(fnd, red)
		}
		sortFindings(findings)
		refs[i] = PublishRef{
			Name:               red(ref.Name),
			Kind:               ref.Kind,
			Tip:                ref.Tip,
			Scanned:            ref.Scanned,
			LeakVector:         ref.LeakVector,
			Reasons:            ref.Reasons,
			SuggestedAction:    red(ref.SuggestedAction),
			FindingsBySeverity: ref.FindingsBySeverity,
			Findings:           findings,
		}
	}
	// Refs are emitted in a stable order: leak vectors first, then by name.
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].LeakVector != refs[j].LeakVector {
			return refs[i].LeakVector
		}
		return refs[i].Name < refs[j].Name
	})

	out.Summary.LeakVectorRefs = leakRefs
	out.Refs = refs
	out.PrimaryRef = red(out.PrimaryRef)
	// remote is operator-controlled (--remote) and may carry protected
	// vocabulary; redact it like every other string field (ADR-0003).
	out.Remote = red(out.Remote)
	return out
}

// redactFinding applies red() to the user-controlled string fields of a
// Finding, matching JSONFormatter.redactOutput's per-finding handling.
func redactFinding(fnd Finding, red func(string) string) Finding {
	fnd.Location = Location{
		Path:        red(fnd.Location.Path),
		SourceID:    red(fnd.Location.SourceID),
		Line:        fnd.Location.Line,
		Column:      fnd.Location.Column,
		GitRef:      red(fnd.Location.GitRef),
		SurfaceKind: fnd.Location.SurfaceKind,
	}
	fnd.Message = red(fnd.Message)
	return fnd
}

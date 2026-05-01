package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func newTestRedactor(t *testing.T) *Redactor {
	t.Helper()
	r, err := NewRedactor([]Alias{
		{Pattern: "Acme", EntityID: "e-client-1", CaseInsensitive: true},
		{Pattern: "Acme Corp", EntityID: "e-client-1", CaseInsensitive: true},
		{Pattern: "AcmeCorp", EntityID: "e-client-1", CaseInsensitive: true},
		{Pattern: "horizon", EntityID: "e-codename-1", CaseInsensitive: true},
		{Pattern: "tilden", EntityID: "e-codename-2", CaseInsensitive: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func sampleOutput() Output {
	return Output{
		Version: "v0",
		ScanMetadata: ScanMetadata{
			ToolVersion: "0.0.1",
			StartedAt:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
			DurationMS:  42,
			ScanRoot:    "/tmp/test-scan",
			Visibility:  "public_oss",
			CatalogsLoaded: []CatalogLoadStatus{
				{CatalogID: "cs-spike-public-v0", LoadStatus: "ok"},
				{CatalogID: "cs-spike-private-v0", LoadStatus: "absent_optional"},
			},
		},
		Summary: ScanSummary{
			FindingsTotal: 2,
			BySeverity:    map[string]int{"critical": 1, "high": 1},
			BySurface:     map[string]int{"content": 1, "path": 1},
		},
		Findings: []Finding{
			{
				ID:            "f-0001",
				Fingerprint:   "abc123",
				Severity:      "critical",
				Confidence:    "high",
				Decision:      "block",
				EntityClass:   "client_identity",
				DetectorID:    "literal",
				SourceKind:    "file",
				Surface:       "content",
				Location:      Location{Path: "internal/cmd/profile_test.go", Line: 12, Column: 22},
				ReplacementID: "tenant-1",
				EvidenceShape: "literal",
				Message:       "Confidential client identity detected at internal/clients/acme/data.go (catalog: cs-spike-private-v0)",
			},
			{
				ID:            "f-0002",
				Fingerprint:   "def456",
				Severity:      "high",
				Confidence:    "high",
				Decision:      "block",
				EntityClass:   "client_identity",
				DetectorID:    "path-segment",
				SourceKind:    "file",
				Surface:       "path",
				Location:      Location{Path: "internal/clients/acme/data.go", Line: 0},
				ReplacementID: "tenant-1",
				EvidenceShape: "literal",
				Message:       "Confidential client identity detected in path segment",
			},
		},
	}
}

func TestJSONFormatter_Emit_RedactsPathSegments(t *testing.T) {
	var buf bytes.Buffer
	f := NewJSONFormatter(&buf, newTestRedactor(t))

	if err := f.Emit(sampleOutput()); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	for _, leak := range []string{"acme", "Acme", "ACME", "horizon", "Horizon", "tilden", "Tilden"} {
		if strings.Contains(out, leak) {
			t.Errorf("zero-leak violation: output contains %q\nOutput:\n%s", leak, out)
		}
	}
}

func TestJSONFormatter_Emit_RedactsMessageField(t *testing.T) {
	var buf bytes.Buffer
	f := NewJSONFormatter(&buf, newTestRedactor(t))

	if err := f.Emit(sampleOutput()); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// The message field for f-0001 contains "internal/clients/acme/data.go"
	// which should be redacted to "<r:e-client-1>" replacing "acme".
	if !strings.Contains(out, "<r:e-client-1>") {
		t.Errorf("expected redaction marker in output; got:\n%s", out)
	}
}

func TestJSONFormatter_Emit_RedactsSourceID(t *testing.T) {
	var buf bytes.Buffer
	f := NewJSONFormatter(&buf, newTestRedactor(t))

	if err := f.Emit(sampleOutput()); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if strings.Contains(out, `"source_id": "internal/clients/acme/data.go"`) {
		t.Errorf("source_id should be redacted; got:\n%s", out)
	}
}

func TestJSONFormatter_Emit_PreservesIDs(t *testing.T) {
	var buf bytes.Buffer
	f := NewJSONFormatter(&buf, newTestRedactor(t))

	if err := f.Emit(sampleOutput()); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// Catalog IDs and entity IDs are alias-safe by ID-safety rule and
	// should pass through unchanged.
	expected := []string{
		`"catalog_id": "cs-spike-public-v0"`,
		`"catalog_id": "cs-spike-private-v0"`,
		`"replacement_id": "tenant-1"`,
		`"id": "f-0001"`,
		`"id": "f-0002"`,
	}
	for _, want := range expected {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output; got:\n%s", want, out)
		}
	}
}

func TestJSONFormatter_Emit_ValidJSON(t *testing.T) {
	var buf bytes.Buffer
	f := NewJSONFormatter(&buf, newTestRedactor(t))

	if err := f.Emit(sampleOutput()); err != nil {
		t.Fatal(err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Errorf("emitted JSON is not valid: %v\nOutput:\n%s", err, buf.String())
	}

	if parsed["version"] != "v0" {
		t.Errorf("expected version v0; got %v", parsed["version"])
	}
}

func TestJSONFormatter_Emit_DeterministicFindingOrder(t *testing.T) {
	in := sampleOutput()
	// Reverse the input order; output must still be sorted by location.
	in.Findings[0], in.Findings[1] = in.Findings[1], in.Findings[0]

	var buf bytes.Buffer
	f := NewJSONFormatter(&buf, newTestRedactor(t))
	if err := f.Emit(in); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// Sorted by path: "internal/clients/acme/data.go" comes before
	// "internal/cmd/profile_test.go" alphabetically — but the path is
	// redacted before sort? No: sort happens after redact in Emit. So
	// the redacted paths sort by their post-redaction strings.
	// "internal/clients/<r:e-client-1>/data.go" vs
	// "internal/cmd/profile_test.go" — clients < cmd, so f-0002 (path
	// surface) comes first.
	idx0001 := strings.Index(out, `"id": "f-0001"`)
	idx0002 := strings.Index(out, `"id": "f-0002"`)
	if idx0002 == -1 || idx0001 == -1 {
		t.Fatalf("missing finding IDs in output:\n%s", out)
	}
	if idx0002 > idx0001 {
		t.Errorf("expected f-0002 (path surface) before f-0001 (content surface) by path sort; got reversed\nOutput:\n%s", out)
	}
}

func TestJSONFormatter_Emit_EmptyFindings(t *testing.T) {
	var buf bytes.Buffer
	f := NewJSONFormatter(&buf, newTestRedactor(t))
	out := sampleOutput()
	out.Findings = nil
	out.Summary.FindingsTotal = 0
	out.Summary.BySeverity = map[string]int{}
	out.Summary.BySurface = map[string]int{}

	if err := f.Emit(out); err != nil {
		t.Fatal(err)
	}

	body := buf.String()
	// Empty findings should still produce a valid JSON array, not null.
	if !strings.Contains(body, `"findings": []`) {
		t.Errorf("expected empty findings array; got:\n%s", body)
	}
}

func TestJSONFormatter_Emit_NilRedactor_PassesThrough(t *testing.T) {
	var buf bytes.Buffer
	f := NewJSONFormatter(&buf, nil)

	if err := f.Emit(sampleOutput()); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// With no redactor, raw values appear (used for tests / dev only).
	if !strings.Contains(out, "acme") {
		t.Errorf("expected raw passthrough with nil redactor; got:\n%s", out)
	}
}

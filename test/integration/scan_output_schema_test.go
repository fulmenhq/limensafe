package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fulmenhq/limensafe/pkg/output"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// TestScanOutputSchemaContract locks the scan output JSON Schema (internal-brief,
// internal-brief). It validates real emitted scan documents — clean, blocking, and
// skip-heavy — against schemas/limensafe/v1.1.0/scan-output.schema.json, plus a
// constructed maximal document that exercises every modeled field including
// the config-warning finding shape. Because the schema declares
// additionalProperties:false on every fixed object, a Go struct field added
// to pkg/output without a matching schema update makes the maximal-document
// case fail here — the drift sentinel the brief requires.
//
// The schema is the published contract adopters parse (CI wrappers, jq,
// partner-integration); this test is the CI gate that keeps the Go output and the schema
// from drifting apart.
func TestScanOutputSchemaContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("output-schema contract test is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	repoRoot := repoRootFromGoMod(t)
	builtinCatalog := filepath.Join(repoRoot, "pkg", "catalog", "builtin", "public-baseline.yaml")
	seededFixture := filepath.Join(repoRoot, "testdata", "builtin-baseline")
	seededFixtureConfig := filepath.Join(seededFixture, ".limensafe", "config.yaml")

	sch := compileScanOutputSchema(t, repoRoot)

	cleanDir := buildCleanFixture(t)
	skipDir := buildSkipHeavyFixture(t)

	t.Run("clean_scan_validates", func(t *testing.T) {
		doc := runScanJSON(t, bin, []string{"scan", cleanDir, "--catalog", builtinCatalog, "--visibility", "public_oss"})
		validateDoc(t, sch, doc)
	})

	t.Run("blocking_scan_validates", func(t *testing.T) {
		doc := runScanJSON(t, bin, []string{"scan", seededFixture, "--config-file", seededFixtureConfig, "--visibility", "public_oss"})
		validateDoc(t, sch, doc)
		// Real detection findings must all carry the kind discriminator.
		assertFindingsHaveKind(t, doc, "detection")
	})

	t.Run("config_warning_scan_validates", func(t *testing.T) {
		// A live config-warning finding (missing optional private catalog,
		// warn posture) exercises the second finding shape on the real
		// emission path — not just the constructed maximal document.
		parent := t.TempDir()
		fixture := filepath.Join(parent, "repo")
		mustMkdir(t, fixture)
		configPath := filepath.Join(parent, "config.yaml")
		mustWrite(t, configPath, missingPrivateConfigYAML("warn"))
		t.Setenv("LIMENSAFE_PRIVATE_TEST_CATALOG", "")

		doc := runScanJSON(t, bin, []string{"scan", fixture, "--config-file", configPath})
		validateDoc(t, sch, doc)
		assertFindingsHaveKind(t, doc, "config-warning")
	})

	t.Run("skip_heavy_scan_validates_and_reconciles", func(t *testing.T) {
		doc := runScanJSON(t, bin, []string{"scan", skipDir, "--catalog", builtinCatalog, "--visibility", "public_oss"})
		validateDoc(t, sch, doc)

		meta, ok := doc["scan_metadata"].(map[string]interface{})
		if !ok {
			t.Fatalf("scan_metadata missing or wrong type")
		}
		filesSkipped := jsonInt(t, meta, "files_skipped")
		dirsSkipped := jsonInt(t, meta, "directories_skipped")
		byReason, ok := meta["files_skipped_by_reason"].(map[string]interface{})
		if !ok {
			t.Fatalf("files_skipped_by_reason missing or wrong type")
		}

		// internal-brief: the represented files behind the pruned generated/ tree
		// (a.txt, b.txt, sub/c.txt) plus the per-file skip.log must all land
		// in files_skipped, and directories_skipped is the additional roll-up.
		if filesSkipped != 4 {
			t.Errorf("files_skipped = %d, want 4 (3 pruned + 1 per-file)", filesSkipped)
		}
		if dirsSkipped != 1 {
			t.Errorf("directories_skipped = %d, want 1", dirsSkipped)
		}

		// files_skipped_by_reason must reconcile with files_skipped.
		sum := 0
		for k, v := range byReason {
			n, ok := v.(float64)
			if !ok {
				t.Fatalf("files_skipped_by_reason[%q] not a number: %T", k, v)
			}
			sum += int(n)
		}
		if sum != filesSkipped {
			t.Errorf("files_skipped_by_reason sums to %d, want files_skipped=%d", sum, filesSkipped)
		}
		if got := byReason["ignored"]; got != float64(4) {
			t.Errorf("files_skipped_by_reason[ignored] = %v, want 4", got)
		}
	})

	t.Run("maximal_document_validates", func(t *testing.T) {
		doc := emitMaximalDocument(t)
		validateDoc(t, sch, doc)
	})

	t.Run("finding_without_kind_is_rejected", func(t *testing.T) {
		// Proves the drift sentinel is real: kind is required, so a
		// regression that dropped the finding discriminator fails schema
		// validation rather than silently passing (devrev finding).
		doc := emitMaximalDocument(t)
		findings := doc["findings"].([]interface{})
		first := findings[0].(map[string]interface{})
		delete(first, "kind")
		if err := sch.Validate(doc); err == nil {
			t.Error("expected schema validation to reject a finding missing kind, but it passed")
		}
	})
}

// TestScanSkipStderrReconciles verifies the stderr directory skip event
// carries a files=<n> count so operators can reconcile stderr with
// scan_metadata without walking the tree (internal-brief) — and that it never
// enumerates the hidden descendant paths (zero-leak: the pruned subtree may
// hold protected vocabulary that was deliberately never scanned).
func TestScanSkipStderrReconciles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip-stderr test is unix-focused")
	}
	bin := buildLimensafeBinary(t)
	repoRoot := repoRootFromGoMod(t)
	builtinCatalog := filepath.Join(repoRoot, "pkg", "catalog", "builtin", "public-baseline.yaml")
	skipDir := buildSkipHeavyFixture(t)

	cmd := exec.Command(bin, "scan", skipDir, "--catalog", builtinCatalog, "--visibility", "public_oss")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run() // exit 0 expected; we only assert stderr content

	out := stderr.String()
	// generated/ prunes 3 files (a.txt, b.txt, sub/c.txt).
	if !bytes.Contains(stderr.Bytes(), []byte("kind=directory")) ||
		!bytes.Contains(stderr.Bytes(), []byte("files=3")) {
		t.Errorf("expected a directory skip event carrying files=3; stderr:\n%s", out)
	}
	// Zero-leak: the pruned descendant filenames must never appear in stderr.
	for _, leaked := range []string{"a.txt", "b.txt", "c.txt", "sub"} {
		if bytes.Contains(stderr.Bytes(), []byte(leaked)) {
			t.Errorf("stderr enumerated a pruned descendant path %q — zero-leak violation:\n%s", leaked, out)
		}
	}
}

// emitMaximalDocument constructs an Output that populates every modeled
// field — including a config-warning finding and the git-history /
// private-catalog metadata — emits it through the real JSONFormatter, and
// returns the decoded document. Synthetic placeholders only (no protected
// vocabulary). With additionalProperties:false everywhere, any unmodeled
// emitted field fails schema validation in the caller.
func emitMaximalDocument(t *testing.T) map[string]interface{} {
	t.Helper()

	out := output.Output{
		Version: "v0",
		ScanMetadata: output.ScanMetadata{
			OutputSchemaVersion: output.SchemaVersion,
			ToolVersion:         "0.1.1",
			StartedAt:           time.Unix(1_700_000_000, 0).UTC(),
			DurationMS:          42,
			ScanRoot:            "acme/repo",
			ScanRootKind:        "git_archive",
			GitRef:              "refs/heads/horizon",
			Visibility:          "engagement_private",
			WorkerCount:         8,
			FilesScanned:        12,
			BytesScanned:        34567,
			FilesSkipped:        5,
			DirsSkipped:         2,
			SkippedByReason: map[string]int{
				"ignored":         3,
				"binary_detected": 1,
				"file_too_large":  1,
			},
			// internal-brief: exercise the allowlist suppression accounting fields with
			// non-zero values so the drift sentinel covers them.
			AllowlistSuppressions: 4,
			AllowlistSuppressionsByID: map[string]int{
				"al-public-tool": 3,
				"al-public-host": 1,
			},
			HistoryBlobsScanned:   9,
			HistoryCommitsScanned: 4,
			HistoryUniqueBlobs:    7,
			// Cover every load_status enum value.
			CatalogsLoaded: []output.CatalogLoadStatus{
				{CatalogID: "cs-acme-public", LoadStatus: "ok", SourceKind: "builtin"},
				{CatalogID: "cs-acme-opt", LoadStatus: "absent_optional", SourceKind: "env"},
				{CatalogID: "cs-acme-private", LoadStatus: "missing_warn", SourceKind: "env", Reason: "absent"},
				{CatalogID: "cs-acme-required", LoadStatus: "missing_error", SourceKind: "path", Reason: "absent"},
			},
			PrivateCatalogsStatus: []output.PrivateCatalogStatus{
				{CatalogID: "cs-acme-private", SourceKind: "env", Status: "missing", Reason: "absent"},
			},
		},
		Summary: output.ScanSummary{
			FindingsTotal: 5,
			// Cover every severity enum value.
			BySeverity: map[string]int{"info": 1, "low": 1, "medium": 1, "high": 1, "critical": 1},
			// Cover every detection-surface enum value.
			BySurface: map[string]int{"content": 1, "path": 1, "branch_name": 1, "commit_message": 1, "diff": 1},
		},
		Findings: maximalFindings(),
	}

	var buf bytes.Buffer
	f := output.NewJSONFormatter(&buf, nil)
	if err := f.Emit(out); err != nil {
		t.Fatalf("emit maximal document: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("decode maximal document: %v", err)
	}
	return doc
}

// maximalFindings returns findings that collectively exercise every enum
// value the engine can emit across finding fields — source_kind, surface,
// surface_kind, severity, decision, kind — plus the config-warning shape.
// Kept in sync with the emitting code so the schema's closed enums and
// additionalProperties:false stay honest: a new emitted value that the
// schema lacks fails validation here.
func maximalFindings() []output.Finding {
	base := output.Finding{
		Kind:          "detection",
		Confidence:    "high",
		EntityClass:   "client_identity",
		DetectorID:    "literal",
		EvidenceShape: "literal",
		Message:       "Confidential context detected",
	}
	mk := func(id, sev, dec, srcKind, surface, surfaceKind string, loc output.Location) output.Finding {
		f := base
		f.ID = id
		f.Fingerprint = id + "00000000"
		f.Severity = sev
		f.Decision = dec
		f.SourceKind = srcKind
		f.Surface = surface
		loc.SurfaceKind = surfaceKind
		f.Location = loc
		return f
	}
	return []output.Finding{
		mk("f-0001", "critical", "block", "file", "content", "working_tree",
			output.Location{Path: "docs/horizon.md", SourceID: "docs/horizon.md", Line: 3, Column: 54}),
		mk("f-0002", "high", "block", "file", "path", "staged_index",
			output.Location{Path: "internal/acme/data.go", SourceID: "internal/acme/data.go"}),
		mk("f-0003", "medium", "warn", "git_diff", "diff", "diff",
			output.Location{SourceID: "diff", GitRef: "origin/main...HEAD"}),
		mk("f-0004", "low", "allow", "git_history_blob", "content", "blob",
			output.Location{Path: "old/tilden.txt", SourceID: "old/tilden.txt", GitRef: "abc123"}),
		mk("f-0005", "info", "allow", "branch_name", "branch_name", "branch_name",
			output.Location{SourceID: "branch_name"}),
		mk("f-0006", "medium", "warn", "commit_message", "commit_message", "commit_message",
			output.Location{SourceID: "commit_message"}),
		mk("f-0007", "high", "block", "git_commit_message", "commit_message", "commit_message",
			output.Location{SourceID: "commit_message", GitRef: "abc123"}),
		{
			Kind:          "config-warning",
			ID:            "cw-0001",
			Fingerprint:   "0011223344556677",
			Severity:      "medium",
			Confidence:    "high",
			Decision:      "warn",
			EntityClass:   "configuration",
			DetectorID:    "private-catalog-missing",
			SourceKind:    "catalog_config",
			Surface:       "config",
			Location:      output.Location{SourceID: "cs-acme-private"},
			EvidenceShape: "missing_private_catalog",
			Message:       "Private catalog missing: catalog_id=cs-acme-private source_kind=env reason=absent",
		},
	}
}

// buildSkipHeavyFixture creates a tree exercising both skip-accounting
// paths (internal-brief): a directory-pruned subtree (generated/ holds 3 files
// across two levels) and a per-file ignore (*.log), alongside a scanned
// sibling. The .limensafeignore drives both.
func buildSkipHeavyFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "generated", "sub"))
	mustMkdir(t, filepath.Join(dir, "keep"))
	mustWrite(t, filepath.Join(dir, "generated", "a.txt"), "a\n")
	mustWrite(t, filepath.Join(dir, "generated", "b.txt"), "b\n")
	mustWrite(t, filepath.Join(dir, "generated", "sub", "c.txt"), "c\n")
	mustWrite(t, filepath.Join(dir, "keep", "k.txt"), "ok\n")
	mustWrite(t, filepath.Join(dir, "skip.log"), "log\n")
	mustWrite(t, filepath.Join(dir, ".limensafeignore"), "generated/\n*.log\n")
	return dir
}

func compileScanOutputSchema(t *testing.T, repoRoot string) *jsonschema.Schema {
	t.Helper()
	schemaPath := filepath.Join(repoRoot, "schemas", "limensafe", "v1.1.0", "scan-output.schema.json")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("scan-output.schema.json", bytes.NewReader(data)); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	sch, err := c.Compile("scan-output.schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return sch
}

func runScanJSON(t *testing.T, bin string, args []string) map[string]interface{} {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// Exit codes 0 (clean) and 1 (blocking) both emit a full document; only
	// fail on config/runtime errors (2/3).
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if code := exitErr.ExitCode(); code != 0 && code != 1 {
				t.Fatalf("scan exited %d (args %v); stdout: %s", code, args, truncate(stdout.String(), 400))
			}
		} else {
			t.Fatalf("scan run: %v", err)
		}
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("decode scan output: %v; raw: %s", err, truncate(stdout.String(), 400))
	}
	return doc
}

func validateDoc(t *testing.T, sch *jsonschema.Schema, doc map[string]interface{}) {
	t.Helper()
	if err := sch.Validate(doc); err != nil {
		t.Errorf("scan output failed schema validation: %v", err)
	}
}

// assertFindingsHaveKind verifies every emitted finding carries the kind
// discriminator (a contract regression that dropped it would now fail the
// schema's required check too — this is the matching real-path assertion).
// If wantKind is non-empty, it also asserts at least one finding has it, so
// the test actually exercised the expected shape rather than passing
// vacuously on an empty findings list.
func assertFindingsHaveKind(t *testing.T, doc map[string]interface{}, wantKind string) {
	t.Helper()
	findings, _ := doc["findings"].([]interface{})
	sawWant := false
	for i, f := range findings {
		m, ok := f.(map[string]interface{})
		if !ok {
			t.Fatalf("finding[%d] is not an object: %T", i, f)
		}
		kind, ok := m["kind"].(string)
		if !ok || kind == "" {
			t.Errorf("finding[%d] missing non-empty kind discriminator: %v", i, m)
			continue
		}
		if kind == wantKind {
			sawWant = true
		}
	}
	if wantKind != "" && !sawWant {
		t.Fatalf("expected at least one finding with kind=%q; got %d findings", wantKind, len(findings))
	}
}

func jsonInt(t *testing.T, m map[string]interface{}, key string) int {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("metadata key %q absent", key)
	}
	n, ok := v.(float64)
	if !ok {
		t.Fatalf("metadata key %q is %T, want number", key, v)
	}
	return int(n)
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

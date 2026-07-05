package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fulmenhq/limensafe/pkg/catalog"
)

// allowlistCatalog is a synthetic catalog (placeholder vocabulary only) that
// exercises the internal-brief allowlist primitive end-to-end: the filename entity is
// allowlisted (a legitimate reference), while the codename entity is not.
const allowlistCatalog = `$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: cs-allow-it
schema_version: "1.1.0"
default_severity: high
entities:
  - id: e-fileref
    class: system
    aliases: ["AGENTS.local.md"]
    severity_override: high
  - id: e-secret
    class: codename
    aliases: ["Tilden"]
    variants: { case_insensitive: true }
    severity_override: critical
allowlist:
  - id: al-filename
    kind: literal
    pattern: "AGENTS.local.md"
    reason: "filename mention is safe; contents are caught by other entities"
`

// TestScanAllowlistSuppressionReconciles drives a real scan against a catalog
// with an allowlist and asserts the internal-brief contract end-to-end: the
// allowlisted filename reference is suppressed (not a finding), the codename
// still flags (reference-vs-disclosure), and the suppression accounting in
// scan_metadata reconciles.
func TestScanAllowlistSuppressionReconciles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("allowlist integration test is unix-focused")
	}
	bin := buildLimensafeBinary(t)

	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	mustMkdir(t, filepath.Join(repo, "docs"))
	mustWrite(t, filepath.Join(repo, "docs", "guide.md"),
		"Refer to AGENTS.local.md for setup. Codename Tilden ships soon.\n")
	catalogPath := filepath.Join(parent, "cat.yaml")
	mustWrite(t, catalogPath, allowlistCatalog)

	doc := runScanJSON(t, bin, []string{"scan", repo, "--catalog", catalogPath, "--visibility", "public_oss"})

	// Reference-vs-disclosure: the codename flags, the filename mention does not.
	ids := findingEntityIDs(doc)
	if !ids["e-secret"] {
		t.Errorf("codename e-secret should still flag despite the allowlist; findings=%v", ids)
	}
	if ids["e-fileref"] {
		t.Errorf("filename reference e-fileref should be allowlist-suppressed, but it flagged; findings=%v", ids)
	}

	meta, ok := doc["scan_metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("scan_metadata missing")
	}
	total := jsonInt(t, meta, "allowlist_suppressions")
	if total != 1 {
		t.Errorf("allowlist_suppressions = %d, want 1", total)
	}
	byEntry, ok := meta["allowlist_suppressions_by_entry"].(map[string]interface{})
	if !ok {
		t.Fatalf("allowlist_suppressions_by_entry missing or wrong type")
	}
	// by-entry must reconcile with the total.
	sum := 0
	for _, v := range byEntry {
		n, ok := v.(float64)
		if !ok {
			t.Fatalf("by_entry value not a number: %T", v)
		}
		sum += int(n)
	}
	if sum != total {
		t.Errorf("allowlist_suppressions_by_entry sums to %d, want total=%d", sum, total)
	}
	if got := byEntry["al-filename"]; got != float64(1) {
		t.Errorf("allowlist_suppressions_by_entry[al-filename] = %v, want 1", got)
	}
}

// TestScanAllowlistExplainIsRedactionSafe verifies --explain prints the
// allowlist entry/entity ids but never the catalog-private allowlist pattern or
// the matched text — the zero-leak boundary (ADR-0003) extends to the audit
// trail.
func TestScanAllowlistExplainIsRedactionSafe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("allowlist explain test is unix-focused")
	}
	bin := buildLimensafeBinary(t)

	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	mustMkdir(t, filepath.Join(repo, "docs"))
	mustWrite(t, filepath.Join(repo, "docs", "guide.md"),
		"Refer to AGENTS.local.md for setup. Codename Tilden ships soon.\n")
	catalogPath := filepath.Join(parent, "cat.yaml")
	mustWrite(t, catalogPath, allowlistCatalog)

	cmd := exec.Command(bin, "scan", repo, "--catalog", catalogPath, "--visibility", "public_oss", "--explain")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run() // exit 1 (codename blocks) is expected; we assert stderr content

	out := stderr.String()
	if !strings.Contains(out, "allowlist suppression:") {
		t.Fatalf("--explain produced no suppression line; stderr:\n%s", out)
	}
	if !strings.Contains(out, "allowlist_id=al-filename") || !strings.Contains(out, "entity_id=e-fileref") {
		t.Errorf("--explain line missing alias-safe ids; stderr:\n%s", out)
	}
	// Zero-leak: the allowlist PATTERN must never appear on stderr.
	if strings.Contains(out, "AGENTS.local.md") {
		t.Errorf("--explain leaked the allowlist pattern (zero-leak violation); stderr:\n%s", out)
	}
}

func TestScanStructuredCatalogBuildFixtureScans(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("structured catalog integration test is unix-focused")
	}
	bin := buildLimensafeBinary(t)
	repoRoot := repoRootFromGoMod(t)

	src, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "synthetic-acme", "termlist", "synthetic-acme.structured-corpus.txt"))
	if err != nil {
		t.Fatalf("read structured corpus fixture: %v", err)
	}
	catalogBytes, err := catalog.BuildCatalogFromTermList(bytes.NewReader(src), catalog.TermListOptions{
		CatalogID:       "cs-structured-corpus-demo",
		DefaultSeverity: "medium",
	})
	if err != nil {
		t.Fatalf("build structured catalog fixture: %v", err)
	}

	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	mustMkdir(t, filepath.Join(repo, "docs"))
	mustWrite(t, filepath.Join(repo, "docs", "regex-hit.md"), "ticket HRZN-1234 must block\n")
	mustWrite(t, filepath.Join(repo, "docs", "allowlisted.md"), "fixture HRZN-0000 is carved out\n")
	catalogPath := filepath.Join(parent, "structured.catalog.yaml")
	mustWrite(t, catalogPath, string(catalogBytes))

	doc := runScanJSON(t, bin, []string{"scan", repo, "--catalog", catalogPath, "--visibility", "public_oss"})
	findings, _ := doc["findings"].([]interface{})
	sawRegexFinding := false
	for _, f := range findings {
		m, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := m["entity_id"].(string); ok && strings.HasPrefix(id, "e-rx-") {
			sawRegexFinding = true
			break
		}
	}
	if !sawRegexFinding {
		t.Fatalf("expected at least one generated regex finding, got %#v", findings)
	}

	meta, ok := doc["scan_metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("scan_metadata missing")
	}
	total := jsonInt(t, meta, "allowlist_suppressions")
	if total != 1 {
		t.Fatalf("allowlist_suppressions = %d, want 1", total)
	}
	byEntry, ok := meta["allowlist_suppressions_by_entry"].(map[string]interface{})
	if !ok {
		t.Fatalf("allowlist_suppressions_by_entry missing or wrong type")
	}
	sawGeneratedAllowlist := false
	for id, v := range byEntry {
		n, ok := v.(float64)
		if strings.HasPrefix(id, "al-tl-") && ok && int(n) == 1 {
			sawGeneratedAllowlist = true
		}
	}
	if !sawGeneratedAllowlist {
		t.Fatalf("expected generated al-tl-* suppression entry, got %#v", byEntry)
	}
}

func findingEntityIDs(doc map[string]interface{}) map[string]bool {
	out := map[string]bool{}
	findings, _ := doc["findings"].([]interface{})
	for _, f := range findings {
		m, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := m["entity_id"].(string); ok && id != "" {
			out[id] = true
		}
	}
	return out
}

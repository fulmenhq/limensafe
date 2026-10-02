package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// auditGit runs a git command in dir, failing the test on error.
func auditGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func auditWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func syntheticCatalog(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRootFromGoMod(t), "testdata", "synthetic-acme", "catalog", "synthetic-acme.catalog.yaml")
}

// runAuditPublish runs the command and returns the exit code and parsed JSON.
// Exit 0/1 both emit a full document; 2/3 are hard errors.
func runAuditPublish(t *testing.T, bin string, args []string) (int, map[string]interface{}) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run audit-publish %v: %v\n%s", args, err, stderr.String())
		}
		code = ee.ExitCode()
	}
	var doc map[string]interface{}
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil && code < 2 {
			t.Fatalf("decode output: %v; raw: %s", err, truncate(stdout.String(), 400))
		}
	}
	return code, doc
}

func compilePublishSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	path := filepath.Join(repoRootFromGoMod(t), "schemas", "limensafe", "v1.1.0", "publish-surface-output.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(path, bytes.NewReader(data)); err != nil {
		t.Fatalf("add schema: %v", err)
	}
	sch, err := c.Compile(path)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return sch
}

// divergentRepo: main is clean; backup/old (a danger-name ref) carries a
// protected client identity the primary does not.
func divergentRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	auditGit(t, dir, "init", "-q", "-b", "main")
	auditWrite(t, dir, "a.txt", "shared clean content\n")
	auditGit(t, dir, "add", "a.txt")
	auditGit(t, dir, "commit", "-q", "-m", "main")
	auditGit(t, dir, "checkout", "-q", "-b", "backup/old")
	auditWrite(t, dir, "leak.txt", "internal client Acme Corp project notes\n")
	auditGit(t, dir, "add", "leak.txt")
	auditGit(t, dir, "commit", "-q", "-m", "backup")
	auditGit(t, dir, "checkout", "-q", "main")
	return dir
}

func TestAuditPublish_DivergentBackupBranchBlocks(t *testing.T) {
	bin := buildLimensafeBinary(t)
	repo := divergentRepo(t)
	code, doc := runAuditPublish(t, bin, []string{
		"audit-publish", repo, "--local-refs",
		// Protected token in --remote exercises top-level remote redaction
		// (devrev CP2 P1b) via the zero-leak grep below.
		"--remote", "acme-mirror",
		"--catalog", syntheticCatalog(t), "--visibility", "public_oss",
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (not publish-safe)", code)
	}
	summary := doc["summary"].(map[string]interface{})
	if summary["publish_safe"].(bool) {
		t.Fatal("publish_safe must be false")
	}
	leakRefs := summary["leak_vector_refs"].([]interface{})
	if len(leakRefs) != 1 || leakRefs[0].(string) != "backup/old" {
		t.Fatalf("leak_vector_refs = %v, want [backup/old]", leakRefs)
	}
	// Schema conformance.
	if err := compilePublishSchema(t).Validate(doc); err != nil {
		t.Errorf("output failed schema validation: %v", err)
	}
	// Zero-leak: no protected substring anywhere in the emitted document.
	raw, _ := json.Marshal(doc)
	for _, term := range []string{"acme", "Acme", "ACME"} {
		if bytes.Contains(raw, []byte(term)) {
			t.Fatalf("zero-leak violation: %q present in output", term)
		}
	}
}

func TestAuditPublish_AllCleanIsPublishSafe(t *testing.T) {
	bin := buildLimensafeBinary(t)
	dir := t.TempDir()
	auditGit(t, dir, "init", "-q", "-b", "main")
	auditWrite(t, dir, "ok.txt", "nothing sensitive here\n")
	auditGit(t, dir, "add", "ok.txt")
	auditGit(t, dir, "commit", "-q", "-m", "main")

	code, doc := runAuditPublish(t, bin, []string{
		"audit-publish", dir, "--local-refs",
		"--catalog", syntheticCatalog(t), "--visibility", "public_oss",
	})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (publish-safe)", code)
	}
	if !doc["summary"].(map[string]interface{})["publish_safe"].(bool) {
		t.Fatal("publish_safe must be true for an all-clean repo")
	}
}

func TestAuditPublish_NamesOnlyFlagsWithoutCatalog(t *testing.T) {
	bin := buildLimensafeBinary(t)
	dir := t.TempDir()
	auditGit(t, dir, "init", "-q", "-b", "main")
	auditWrite(t, dir, "a.txt", "clean\n")
	auditGit(t, dir, "add", "a.txt")
	auditGit(t, dir, "commit", "-q", "-m", "main")
	auditGit(t, dir, "branch", "wip/experiment")

	// No --catalog: names-only does not require one.
	code, doc := runAuditPublish(t, bin, []string{"audit-publish", dir, "--local-refs", "--names-only"})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (wip/* is a leak vector by name)", code)
	}
	summary := doc["summary"].(map[string]interface{})
	leakRefs := summary["leak_vector_refs"].([]interface{})
	if len(leakRefs) != 1 || leakRefs[0].(string) != "wip/experiment" {
		t.Fatalf("leak_vector_refs = %v, want [wip/experiment]", leakRefs)
	}
	if doc["surface"].(map[string]interface{})["refs_scanned"].(float64) != 0 {
		t.Fatal("names-only must not scan content")
	}
}

// devrev CP2 P1 regression: --tags-only must still scan the primary BRANCH for
// the divergence baseline. A low-severity entity present on both main and a tag
// (below the block threshold, so block-tier cannot mask the bug) must NOT be
// flagged diverges_from_primary, and the repo must be publish-safe.
func TestAuditPublish_TagsOnlyDoesNotFalselyDiverge(t *testing.T) {
	bin := buildLimensafeBinary(t)
	dir := t.TempDir()

	// Minimal low-severity catalog: a match below the default 'high' block
	// threshold, so any exit 1 here would come from false divergence, not a
	// block-tier finding.
	catalog := filepath.Join(dir, "low.catalog.yaml")
	auditWrite(t, dir, "low.catalog.yaml", `$schema: https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json
catalog_id: cs-lowsev-test
schema_version: "1.0.0"
default_severity: low
entities:
  - id: e-lowsev-1
    class: operational_pattern
    aliases: [widgetcorp]
    variants: {case_insensitive: true, slug: true, whole_word: true}
    blocked_in: [public_oss, unlisted_oss]
    severity_override: low
    replacement_suggestion: tenant-x
`)

	auditGit(t, dir, "init", "-q", "-b", "main")
	auditWrite(t, dir, "notes.txt", "mentions widgetcorp in passing\n")
	auditGit(t, dir, "add", "notes.txt")
	auditGit(t, dir, "commit", "-q", "-m", "main")
	auditGit(t, dir, "tag", "v1.0.0") // tag at the same commit as main

	code, doc := runAuditPublish(t, bin, []string{
		"audit-publish", dir, "--local-refs", "--tags-only",
		"--catalog", catalog, "--visibility", "public_oss",
	})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; a tag sharing the primary's content must not falsely diverge. summary=%v", code, doc["summary"])
	}
	surface := doc["surface"].(map[string]interface{})
	if !surface["primary_scanned"].(bool) {
		t.Fatal("primary_scanned must be true: the primary branch baseline should be scanned even under --tags-only")
	}
	summary := doc["summary"].(map[string]interface{})
	if !summary["publish_safe"].(bool) {
		t.Fatalf("publish_safe must be true; leak_vector_refs=%v", summary["leak_vector_refs"])
	}
}

// devrev + secrev round-2 P1: a runtime error from ref enumeration must not
// leak the operator-controlled --remote value (which can contain protected
// vocabulary) onto stderr. The error boundary redacts via the loaded redactor.
func TestAuditPublish_RemoteEnumerationErrorRedactsProtectedRemote(t *testing.T) {
	bin := buildLimensafeBinary(t)
	dir := t.TempDir()
	auditGit(t, dir, "init", "-q", "-b", "main")
	auditWrite(t, dir, "a.txt", "clean\n")
	auditGit(t, dir, "add", "a.txt")
	auditGit(t, dir, "commit", "-q", "-m", "main")

	// Remote name embeds a protected synthetic alias ("acme") and does not
	// exist, forcing a git ls-remote failure after the redactor is loaded.
	cmd := exec.Command(bin, "audit-publish", dir,
		"--remote", "acme-nonexistent-remote",
		"--catalog", syntheticCatalog(t), "--visibility", "public_oss")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 3 {
		t.Fatalf("expected runtime exit 3, got err=%v stderr=%s", err, truncate(stderr.String(), 300))
	}
	for _, term := range []string{"acme", "Acme", "ACME"} {
		if bytes.Contains(stderr.Bytes(), []byte(term)) {
			t.Fatalf("zero-leak violation: %q present in error stderr:\n%s", term, stderr.String())
		}
	}
}

// internal-brief rewrite-completion contract, end-to-end against a real remote: a
// bare remote carries main (clean) plus a backup/* ref with synthetic
// vocabulary. A fresh clone + audit-publish exits 1 (the backup ref is a leak
// vector); after the backup ref is deleted from the remote, a fresh re-clone +
// audit exits 0. This exercises the documented fresh-clone verification flow
// over remote ref enumeration, not --local-refs.
func TestAuditPublish_FreshCloneVerificationFlow(t *testing.T) {
	bin := buildLimensafeBinary(t)
	base := t.TempDir()
	cat := syntheticCatalog(t)

	// Bare publishable remote.
	auditGit(t, base, "init", "--bare", "-q", "remote.git")
	remote := filepath.Join(base, "remote.git")

	// Work repo: clean main + a backup ref carrying protected vocabulary, both
	// pushed to the remote.
	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	auditGit(t, work, "init", "-q", "-b", "main")
	auditWrite(t, work, "a.txt", "shared clean content\n")
	auditGit(t, work, "add", "a.txt")
	auditGit(t, work, "commit", "-q", "-m", "main")
	auditGit(t, work, "remote", "add", "origin", remote)
	auditGit(t, work, "push", "-q", "-u", "origin", "main")
	auditGit(t, work, "checkout", "-q", "-b", "backup/main-pre-rewrite")
	auditWrite(t, work, "leak.txt", "internal client Acme Corp notes\n")
	auditGit(t, work, "add", "leak.txt")
	auditGit(t, work, "commit", "-q", "-m", "backup")
	auditGit(t, work, "push", "-q", "origin", "backup/main-pre-rewrite")

	auditFreshClone := func(label string) (int, map[string]interface{}) {
		clone := filepath.Join(base, label)
		auditGit(t, base, "clone", "-q", remote, label)
		return runAuditPublish(t, bin, []string{
			"audit-publish", clone, "--remote", "origin",
			"--catalog", cat, "--visibility", "public_oss",
		})
	}

	// Before remediation: the backup ref defeats publication.
	code, doc := auditFreshClone("clone-before")
	if code != 1 {
		t.Fatalf("pre-remediation exit = %d, want 1", code)
	}
	summary := doc["summary"].(map[string]interface{})
	if summary["publish_safe"].(bool) {
		t.Fatal("publish_safe must be false while the backup ref is on the remote")
	}
	leak := summary["leak_vector_refs"].([]interface{})
	if len(leak) != 1 || leak[0].(string) != "backup/main-pre-rewrite" {
		t.Fatalf("leak_vector_refs = %v, want [backup/main-pre-rewrite]", leak)
	}

	// Remediate: remove the backup ref from the publishable remote.
	auditGit(t, work, "push", "-q", "origin", "--delete", "backup/main-pre-rewrite")

	// After remediation: a FRESH clone audits clean.
	code, doc = auditFreshClone("clone-after")
	if code != 0 {
		t.Fatalf("post-remediation exit = %d, want 0; summary=%v", code, doc["summary"])
	}
	if !doc["summary"].(map[string]interface{})["publish_safe"].(bool) {
		t.Fatal("publish_safe must be true after the backup ref is removed")
	}
}

func TestAuditPublish_BranchesOnlyTagsOnlyMutuallyExclusive(t *testing.T) {
	bin := buildLimensafeBinary(t)
	dir := t.TempDir()
	auditGit(t, dir, "init", "-q", "-b", "main")
	auditWrite(t, dir, "a.txt", "clean\n")
	auditGit(t, dir, "add", "a.txt")
	auditGit(t, dir, "commit", "-q", "-m", "main")

	code, _ := runAuditPublish(t, bin, []string{
		"audit-publish", dir, "--local-refs", "--names-only",
		"--branches-only", "--tags-only",
	})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (config error: mutually exclusive flags)", code)
	}
}

package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulmenhq/limensafe/pkg/coverage"
	"github.com/fulmenhq/limensafe/pkg/output"
)

func runCoverageScan(t *testing.T, bin string, args []string, wantExit int) output.Output {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		e, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		exit = e.ExitCode()
	}
	if exit != wantExit {
		t.Fatalf("exit=%d want=%d stdout=%s stderr=%s", exit, wantExit, stdout.String(), stderr.String())
	}
	if wantExit > 1 {
		if stdout.Len() != 0 {
			t.Fatalf("error emitted document: %s", stdout.String())
		}
		return output.Output{}
	}
	var result output.Output
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ScanMetadata.OutputSchemaVersion != "1.2.0" {
		t.Fatal(result.ScanMetadata.OutputSchemaVersion)
	}
	if err := result.Summary.Coverage.Validate(); err != nil {
		t.Fatalf("coverage=%+v: %v", result.Summary.Coverage, err)
	}
	if strings.Contains(stderr.String(), "Command execution failed") {
		t.Fatal(stderr.String())
	}
	return result
}

func TestReleaseCoverageCeilingsAndModeBoundary(t *testing.T) {
	bin := buildLimensafeBinary(t)
	cat := filepath.Join(repoRootFromGoMod(t), "pkg", "catalog", "builtin", "public-baseline.yaml")
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "small.txt"), "ok")
	mustWrite(t, filepath.Join(root, "large.txt"), strings.Repeat("x", 100))
	base := []string{"scan", root, "--catalog", cat, "--max-file-size", "8"}
	for _, tc := range []struct {
		mode, allowance, status string
		exit                    int
	}{
		{"release", "", "incomplete", 1},
		{"release", "file_too_large=0", "incomplete", 1},
		{"release", "file_too_large=1", "acknowledged", 0},
		{"release", "file_too_large=2", "acknowledged", 0},
		{"release", "binary_detected=100", "incomplete", 1},
		{"local", "", "incomplete", 0},
		{"ci", "", "incomplete", 0},
	} {
		args := append(append([]string{}, base...), "--mode", tc.mode)
		if tc.allowance != "" {
			args = append(args, "--allow-skip-reason", tc.allowance)
		}
		out := runCoverageScan(t, bin, args, tc.exit)
		if out.Summary.Coverage.Status != tc.status || out.Summary.Coverage.BlockingSkipsByReason["file_too_large"] != 1 || out.Summary.FindingsTotal != 0 {
			t.Fatal(out)
		}
	}
	for _, value := range []string{"other=1", "ignored=1", "unknown=-1", "unknown=1.2", "unknown=2147483648"} {
		runCoverageScan(t, bin, append(append([]string{}, base...), "--allow-skip-reason", value), 2)
	}
	if err := os.Remove(filepath.Join(root, "large.txt")); err != nil {
		t.Fatal(err)
	}
	out := runCoverageScan(t, bin, append(base, "--mode", "release", "--allow-skip-reason", "file_too_large=2"), 0)
	if out.Summary.Coverage.Status != "complete" || out.Summary.Coverage.AllowancesByReason["file_too_large"] != 2 {
		t.Fatal(out)
	}
}

func TestReleaseCoverageSymlinkLegacyAndIntentionalExclusion(t *testing.T) {
	bin := buildLimensafeBinary(t)
	cat := filepath.Join(repoRootFromGoMod(t), "pkg", "catalog", "builtin", "public-baseline.yaml")
	root := t.TempDir()
	if err := os.Symlink("missing", filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	args := []string{"scan", root, "--catalog", cat, "--mode", "release", "--allow-skip-reason", "unknown=100"}
	out := runCoverageScan(t, bin, args, 1)
	if out.Summary.Coverage.NonBudgetableGapsByCode[coverage.UnfollowedSymlink] != 1 || out.Summary.Coverage.SkipsByReason["ignored"] != 0 || out.ScanMetadata.SkippedByReason["ignored"] != 1 {
		t.Fatal(out)
	}
	mustWrite(t, filepath.Join(root, ".limensafeignore"), "link\n")
	out = runCoverageScan(t, bin, args, 0)
	if out.Summary.Coverage.Status != "complete" || out.Summary.Coverage.SkipsByReason["ignored"] != 1 {
		t.Fatal(out)
	}
}

func TestReleasePublishCoverageAndNamesOnly(t *testing.T) {
	bin := buildLimensafeBinary(t)
	cat := filepath.Join(repoRootFromGoMod(t), "pkg", "catalog", "builtin", "public-baseline.yaml")
	root := t.TempDir()
	auditGit(t, root, "init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(root, "one.bin"), "shared clean bytes")
	mustWrite(t, filepath.Join(root, "two.bin"), "shared clean bytes")
	auditGit(t, root, "add", ".")
	auditGit(t, root, "commit", "-q", "-m", "fixture")
	auditGit(t, root, "branch", "other")
	base := []string{"audit-publish", root, "--local-refs", "--primary-ref", "main", "--catalog", cat}
	for _, tc := range []struct {
		mode, allowance, status string
		exit                    int
		safe                    bool
	}{
		{"release", "", "incomplete", 1, false},
		{"release", "binary_detected=1", "acknowledged", 0, true},
		{"local", "", "incomplete", 0, true},
		{"ci", "", "incomplete", 0, true},
	} {
		args := append(append([]string{}, base...), "--mode", tc.mode)
		if tc.allowance != "" {
			args = append(args, "--allow-skip-reason", tc.allowance)
		}
		cmd := exec.Command(bin, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if tc.exit == 0 && err != nil {
			t.Fatalf("%v: %s", err, stderr.String())
		}
		if tc.exit == 1 && (err == nil || err.(*exec.ExitError).ExitCode() != 1) {
			t.Fatalf("err=%v", err)
		}
		var out output.PublishSurfaceOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.OutputSchemaVersion != "1.1.0" || out.Summary.PublishSafe != tc.safe || out.Summary.Coverage.Status != tc.status || out.Summary.Coverage.SkipsByReason["binary_detected"] != 1 {
			t.Fatal(out)
		}
		if err := out.Summary.Coverage.Validate(); err != nil {
			t.Fatal(err)
		}
		for _, ref := range out.Refs {
			if ref.Scanned {
				t.Fatal("skipped content reported fully scanned")
			}
		}
		var doc map[string]interface{}
		if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		validateDoc(t, compilePublishSchema(t), doc)
		// Historical publish fixture remains valid after dispatch to its own schema.
		delete(doc["summary"].(map[string]interface{}), "coverage")
		doc["output_schema_version"] = "1.0.0"
		old, err := dispatchedSchema(repoRootFromGoMod(t), "publish-surface", "1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		validateDoc(t, old, doc)
	}
	cmd := exec.Command(bin, append(base, "--mode", "release", "--names-only")...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err == nil || err.(*exec.ExitError).ExitCode() != 2 || stdout.Len() != 0 {
		t.Fatalf("err=%v stdout=%s", err, stdout.String())
	}
}

func TestReleaseAttestBareArchiveRefusalAndAllowance(t *testing.T) {
	bin := buildLimensafeBinary(t)
	cat := filepath.Join(repoRootFromGoMod(t), "pkg", "catalog", "builtin", "public-baseline.yaml")
	root := t.TempDir()
	auditGit(t, root, "init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(root, "large.txt"), strings.Repeat("x", 10*1024*1024+1))
	auditGit(t, root, "add", ".")
	auditGit(t, root, "commit", "-q", "-m", "fixture")
	bare := filepath.Join(t.TempDir(), "bare.git")
	auditGit(t, root, "clone", "-q", "--bare", root, bare)
	dest := filepath.Join(t.TempDir(), "attestation.json")
	base := []string{"attest", bare, "--catalog", cat, "--mode", "release", "--output", dest}
	cmd := exec.Command(bin, base...)
	out, err := cmd.CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte("release coverage incomplete")) {
		t.Fatalf("err=%v output=%s", err, out)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("refusal created file: %v", err)
	}
	if err := os.WriteFile(dest, []byte("existing record"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(bin, base...).Run(); err == nil {
		t.Fatal("expected repeat refusal")
	}
	old, err := os.ReadFile(dest)
	if err != nil || string(old) != "existing record" {
		t.Fatalf("old=%s err=%v", old, err)
	}
	cmd = exec.Command(bin, append(base, "--allow-skip-reason", "file_too_large=1")...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("allowance: %v %s", err, out)
	}
	record, err := os.ReadFile(dest)
	if err != nil || !json.Valid(record) {
		t.Fatalf("record=%s err=%v", record, err)
	}
}

func TestReleaseCoverageGitSurfaces(t *testing.T) {
	bin := buildLimensafeBinary(t)
	cat := filepath.Join(repoRootFromGoMod(t), "pkg", "catalog", "builtin", "public-baseline.yaml")
	root := t.TempDir()
	auditGit(t, root, "init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(root, "large.txt"), strings.Repeat("x", 100))
	auditGit(t, root, "add", ".")
	auditGit(t, root, "commit", "-q", "-m", "fixture")
	for _, tc := range []struct {
		flags []string
		unit  string
	}{
		{[]string{"--git-archive=HEAD"}, "file"},
		{[]string{"--git-history"}, "unique_blob"},
	} {
		args := append([]string{"scan", root, "--catalog", cat, "--mode", "release", "--max-file-size", "8"}, tc.flags...)
		out := runCoverageScan(t, bin, args, 1)
		if out.Summary.Coverage.SkipUnit != tc.unit || out.Summary.Coverage.SkipsByReason["file_too_large"] != 1 {
			t.Fatal(out)
		}
		out = runCoverageScan(t, bin, append(args, "--allow-skip-reason", "file_too_large=2"), 0)
		if out.Summary.Coverage.Status != "acknowledged" {
			t.Fatal(out)
		}
	}
	mustWrite(t, filepath.Join(root, "staged.txt"), strings.Repeat("y", 100))
	auditGit(t, root, "add", "staged.txt")
	out := runCoverageScan(t, bin, []string{"scan", root, "--staged", "--catalog", cat, "--mode", "release", "--max-file-size", "8"}, 1)
	if out.Summary.Coverage.SkipUnit != "file" || out.Summary.Coverage.SkipsByReason["file_too_large"] != 1 {
		t.Fatal(out)
	}
	// Fatal extraction retains exit 3 even with valid allowances configured.
	runCoverageScan(t, bin, []string{"scan", filepath.Join(root, "absent"), "--catalog", cat, "--mode", "release", "--allow-skip-reason", "unreadable=100"}, 3)
}

func TestReleasePublishMissingObjectsGap(t *testing.T) {
	bin := buildLimensafeBinary(t)
	cat := filepath.Join(repoRootFromGoMod(t), "pkg", "catalog", "builtin", "public-baseline.yaml")
	remote := t.TempDir()
	auditGit(t, remote, "init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(remote, "clean.txt"), "ok")
	auditGit(t, remote, "add", ".")
	auditGit(t, remote, "commit", "-q", "-m", "fixture")
	local := filepath.Join(t.TempDir(), "clone")
	auditGit(t, remote, "clone", "-q", remote, local)
	auditGit(t, remote, "checkout", "-q", "-b", "unfetched")
	mustWrite(t, filepath.Join(remote, "new.txt"), "new clean bytes")
	auditGit(t, remote, "add", ".")
	auditGit(t, remote, "commit", "-q", "-m", "new fixture")
	for _, mode := range []string{"release", "local", "ci"} {
		cmd := exec.Command(bin, "audit-publish", local, "--remote", "origin", "--primary-ref", "main", "--catalog", cat, "--mode", mode, "--allow-skip-reason", "unknown=100")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if mode == "release" {
			if err == nil || err.(*exec.ExitError).ExitCode() != 1 {
				t.Fatalf("err=%v stderr=%s", err, stderr.String())
			}
		} else if err != nil {
			t.Fatalf("err=%v stderr=%s", err, stderr.String())
		}
		var out output.PublishSurfaceOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Summary.Coverage.NonBudgetableGapsByCode[coverage.RefMissingLocalObjects] != 1 || out.Summary.Coverage.Status != "incomplete" || out.Summary.PublishSafe != (mode != "release") {
			t.Fatal(out)
		}
	}
	for _, namesOnly := range []bool{false, true} {
		args := []string{"audit-publish", local, "--remote", "origin", "--primary-ref", "absent", "--catalog", cat, "--mode", "local"}
		if namesOnly {
			args = append(args, "--names-only")
		}
		cmd := exec.Command(bin, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %s", err, stderr.String())
		}
		var out output.PublishSurfaceOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if namesOnly {
			if out.Summary.Coverage.NonBudgetableGapsByCode[coverage.NamesOnlyContentOmitted] != 1 || len(out.Summary.Coverage.NonBudgetableGapsByCode) != 1 {
				t.Fatal(out)
			}
		} else if out.Summary.Coverage.NonBudgetableGapsByCode[coverage.PrimaryBaselineUnscanned] != 1 {
			t.Fatal(out)
		}
	}
}

func TestReleaseCoverageIndependentOfDetectionThreshold(t *testing.T) {
	bin := buildLimensafeBinary(t)
	root, inputs := t.TempDir(), t.TempDir()
	cat := filepath.Join(inputs, "catalog.yaml")
	// Existing release posture pins the detector threshold to medium; a low
	// finding exercises the below-threshold case without changing that macro.
	mustWrite(t, cat, strings.Replace(archiveCatalogYAML("acme"), "severity_override: high", "severity_override: low", 1))
	config := filepath.Join(inputs, "config.yaml")
	mustWrite(t, config, strings.Replace(archiveConfigYAML(cat), "block_threshold: high", "block_threshold: critical", 1))
	mustWrite(t, filepath.Join(root, "small.txt"), "acme")
	mustWrite(t, filepath.Join(root, "large.txt"), strings.Repeat("x", 100))
	base := []string{"scan", root, "--config-file", config, "--mode", "release", "--max-file-size", "8"}
	out := runCoverageScan(t, bin, base, 1)
	if out.Summary.FindingsTotal != 1 || out.Summary.Coverage.Status != "incomplete" || out.Findings[0].Decision == "block" {
		t.Fatal(out)
	}
	out = runCoverageScan(t, bin, append(append([]string{}, base...), "--allow-skip-reason", "file_too_large=1"), 0)
	if out.Summary.Coverage.Status != "acknowledged" || out.Summary.FindingsTotal != 1 {
		t.Fatal(out)
	}
	if err := os.Remove(filepath.Join(root, "large.txt")); err != nil {
		t.Fatal(err)
	}
	out = runCoverageScan(t, bin, base, 0)
	if out.Summary.Coverage.Status != "complete" || out.Summary.FindingsTotal != 1 {
		t.Fatal(out)
	}
	mustWrite(t, config, archiveConfigYAML(cat))
	mustWrite(t, cat, archiveCatalogYAML("acme"))
	mustWrite(t, filepath.Join(root, "large.txt"), strings.Repeat("x", 100))
	out = runCoverageScan(t, bin, append(append([]string{}, base...), "--allow-skip-reason", "file_too_large=1"), 1)
	if out.Summary.Coverage.Status != "acknowledged" || out.Findings[0].Decision != "block" {
		t.Fatal(out)
	}
	if err := os.Remove(filepath.Join(root, "large.txt")); err != nil {
		t.Fatal(err)
	}
	out = runCoverageScan(t, bin, base, 1)
	if out.Summary.Coverage.Status != "complete" || out.Findings[0].Decision != "block" {
		t.Fatal(out)
	}
}

func TestReleaseCoverageBinaryPathsStillDetected(t *testing.T) {
	bin := buildLimensafeBinary(t)
	for _, mixed := range []bool{false, true} {
		root := t.TempDir()
		cat := filepath.Join(t.TempDir(), "catalog.yaml")
		mustWrite(t, cat, archiveCatalogYAML("acme"))
		auditGit(t, root, "init", "-q", "-b", "main")
		mustWrite(t, filepath.Join(root, "acme.bin"), "clean bytes")
		if mixed {
			mustWrite(t, filepath.Join(root, "safe.txt"), "clean bytes")
		}
		auditGit(t, root, "add", ".")
		auditGit(t, root, "commit", "-q", "-m", "fixture")
		args := []string{"scan", root, "--git-history", "--catalog", cat, "--mode", "release", "--allow-skip-reason", "binary_detected=1"}
		out := runCoverageScan(t, bin, args, 1)
		if out.Summary.BySurface["path"] < 1 || out.ScanMetadata.SkippedByReason["binary_detected"] != 1 {
			t.Fatal(out)
		}
		want := "acknowledged"
		if mixed {
			want = "complete"
		}
		if out.Summary.Coverage.Status != want {
			t.Fatal(out)
		}
		cmd := exec.Command(bin, "audit-publish", root, "--local-refs", "--primary-ref", "main", "--catalog", cat, "--mode", "release", "--allow-skip-reason", "binary_detected=1")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		err := cmd.Run()
		if err == nil || err.(*exec.ExitError).ExitCode() != 1 {
			t.Fatalf("err=%v", err)
		}
		var pub output.PublishSurfaceOutput
		if err := json.Unmarshal(stdout.Bytes(), &pub); err != nil {
			t.Fatal(err)
		}
		if pub.Summary.Coverage.Status != want || pub.Summary.FindingsTotal < 1 || pub.Summary.PublishSafe {
			t.Fatal(pub)
		}
	}
}

func TestReleasePublishDistinctSkippedBlobCeilings(t *testing.T) {
	bin := buildLimensafeBinary(t)
	cat := filepath.Join(repoRootFromGoMod(t), "pkg", "catalog", "builtin", "public-baseline.yaml")
	for _, mixedReasons := range []bool{false, true} {
		root := t.TempDir()
		auditGit(t, root, "init", "-q", "-b", "main")
		mustWrite(t, filepath.Join(root, "one.bin"), "first clean bytes")
		secondPath := "two.bin"
		if mixedReasons {
			secondPath = "large.txt"
		}
		mustWrite(t, filepath.Join(root, secondPath), "distinct clean bytes")
		auditGit(t, root, "add", ".")
		auditGit(t, root, "commit", "-q", "-m", "fixture")
		auditGit(t, root, "branch", "other")
		cases := []struct {
			allowances []string
			exit       int
			status     string
		}{
			{[]string{"binary_detected=1"}, 1, "incomplete"},
			{[]string{"binary_detected=2"}, 0, "acknowledged"},
		}
		if mixedReasons {
			cases = []struct {
				allowances []string
				exit       int
				status     string
			}{
				{[]string{"binary_detected=2"}, 1, "incomplete"},
				{[]string{"file_too_large=2"}, 1, "incomplete"},
				{[]string{"binary_detected=1", "file_too_large=1"}, 0, "acknowledged"},
			}
		}
		for _, tc := range cases {
			args := []string{"audit-publish", root, "--local-refs", "--primary-ref", "main", "--catalog", cat, "--mode", "release", "--max-file-size", "8"}
			for _, allowance := range tc.allowances {
				args = append(args, "--allow-skip-reason", allowance)
			}
			cmd := exec.Command(bin, args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			exit := 0
			if err != nil {
				e, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				exit = e.ExitCode()
			}
			if exit != tc.exit {
				t.Fatalf("exit=%d want=%d stderr=%s", exit, tc.exit, stderr.String())
			}
			var out output.PublishSurfaceOutput
			if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			c := out.Summary.Coverage
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			if c.SkipUnit != "unique_blob" || c.Status != tc.status || out.Summary.PublishSafe != (tc.exit == 0) || out.Summary.FindingsTotal != 0 || len(c.NonBudgetableGapsByCode) != 0 {
				t.Fatal(out)
			}
			if mixedReasons {
				if c.SkipsByReason["binary_detected"] != 1 || c.SkipsByReason["file_too_large"] != 1 {
					t.Fatal(c)
				}
			} else if c.SkipsByReason["binary_detected"] != 2 || len(c.SkipsByReason) != 1 {
				t.Fatal(c)
			}
		}
	}
}

func TestReleasePublishMissingPrimaryIsNonBudgetable(t *testing.T) {
	bin := buildLimensafeBinary(t)
	cat := filepath.Join(repoRootFromGoMod(t), "pkg", "catalog", "builtin", "public-baseline.yaml")
	root := t.TempDir()
	auditGit(t, root, "init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(root, "clean.txt"), "ok")
	auditGit(t, root, "add", ".")
	auditGit(t, root, "commit", "-q", "-m", "fixture")
	cmd := exec.Command(bin, "audit-publish", root, "--local-refs", "--primary-ref", "absent", "--catalog", cat, "--mode", "release", "--allow-skip-reason", "unknown=100", "--allow-skip-reason", "unreadable=100")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 1 {
		t.Fatalf("err=%v stderr=%s", err, stderr.String())
	}
	var out output.PublishSurfaceOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	c := out.Summary.Coverage
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Status != "incomplete" || c.NonBudgetableGapsByCode[coverage.PrimaryBaselineUnscanned] != 1 || len(c.NonBudgetableGapsByCode) != 1 || len(c.SkipsByReason) != 0 || out.Summary.PublishSafe || out.Summary.FindingsTotal != 0 {
		t.Fatal(out)
	}
}

func TestReleaseAttestRefusalPreservesWorktreeAndIndex(t *testing.T) {
	bin := buildLimensafeBinary(t)
	root := t.TempDir()
	cat := filepath.Join(t.TempDir(), "catalog.yaml")
	mustWrite(t, cat, archiveCatalogYAML("acme"))
	auditGit(t, root, "init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(root, "clean.txt"), "ok")
	auditGit(t, root, "add", ".")
	auditGit(t, root, "commit", "-q", "-m", "base")
	auditGit(t, root, "branch", "base")
	mustWrite(t, filepath.Join(root, "new.txt"), "acme")
	auditGit(t, root, "add", ".")
	auditGit(t, root, "commit", "-q", "-m", "fixture")
	dest := filepath.Join(root, ".limensafe", "scan-attestation.json")
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dest, "existing record")
	auditGit(t, root, "add", ".limensafe/scan-attestation.json")
	indexBytes := func() []byte {
		cmd := exec.Command("git", "diff", "--cached", "--binary")
		cmd.Dir = root
		data, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	before := indexBytes()
	base := []string{"attest", root, "--catalog", cat, "--mode", "release", "--diff-base", "base", "--output", dest}
	for _, flags := range [][]string{nil, {"--allow-skip-reason", "ignored=1"}} {
		cmd := exec.Command(bin, append(append([]string{}, base...), flags...)...)
		if err := cmd.Run(); err == nil {
			t.Fatal("expected refusal")
		}
		data, err := os.ReadFile(dest)
		if err != nil || string(data) != "existing record" || !bytes.Equal(before, indexBytes()) {
			t.Fatalf("refusal changed state: %s %v", data, err)
		}
	}
}

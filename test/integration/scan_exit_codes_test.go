package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestScanExitCodeContract locks the documented `scan` subcommand exit-code
// contract that CI wrappers (Make, hooks, GitHub Actions) consume:
//
//	0 — scan succeeded; no findings at or above block threshold
//	1 — scan succeeded; one or more findings have decision=block
//	2 — config / catalog validation error
//	3 — runtime error: I/O, extractor init failure, malformed input,
//	      stdin/stdout failure, working-directory resolution failure
//
// Documented in `limensafe scan --help`, README.md, and CONTRIBUTING.md.
// The contract is implemented via sentinel errors in internal/cmd/scan.go
// (ErrFindingsBlocked, ErrConfigInvalid, ErrRuntime) and dispatched in
// cmd/limensafe/main.go.
//
// Each subtest exercises one branch of the contract by running the built
// binary as an external process and asserting the exit code only — output
// content is locked elsewhere (TestScanOutputStream, JSONFormatter tests).
//
// Origin: partner-integration devlead live-validation 2026-05-08 — india's CI
// contract feedback was the trigger for making the documented contract
// match runtime behaviour.
func TestScanExitCodeContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exit-code contract test is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	repoRoot := repoRootFromGoMod(t)
	builtinCatalog := filepath.Join(repoRoot, "pkg", "catalog", "builtin", "public-baseline.yaml")
	seededFixture := filepath.Join(repoRoot, "testdata", "builtin-baseline")
	seededFixtureConfig := filepath.Join(seededFixture, ".limensafe", "config.yaml")

	cleanDir := buildCleanFixture(t)

	cases := []struct {
		name     string
		args     []string
		stdin    string
		wantExit int
	}{
		{
			name:     "exit_0_clean_scan",
			args:     []string{"scan", cleanDir, "--catalog", builtinCatalog, "--visibility", "public_oss"},
			wantExit: 0,
		},
		{
			name: "exit_1_blocking_findings",
			// testdata/builtin-baseline/repo/leak.go is deliberately seeded
			// with a sentinel marker the public-baseline catalog catches.
			args:     []string{"scan", seededFixture, "--config-file", seededFixtureConfig, "--visibility", "public_oss"},
			wantExit: 1,
		},
		{
			name:     "exit_2_mutually_exclusive_flags",
			args:     []string{"scan", "--staged", "--branch-name", "-"},
			wantExit: 2,
		},
		{
			name:     "exit_2_missing_config_file",
			args:     []string{"scan", cleanDir, "--config-file", "/definitely-not-a-config.yaml"},
			wantExit: 2,
		},
		{
			name:     "exit_2_no_catalog_provided",
			args:     []string{"scan", cleanDir},
			wantExit: 2,
		},
		{
			name:     "exit_2_invalid_arg_shape",
			args:     []string{"scan", "-", "--branch-name", "--commit-msg", "--catalog", builtinCatalog},
			wantExit: 2,
		},
		{
			name:     "exit_2_git_archive_mutually_exclusive_with_staged",
			args:     []string{"scan", "--git-archive", "HEAD", "--staged", "--catalog", builtinCatalog},
			wantExit: 2,
		},
		{
			name:     "exit_2_git_archive_mutually_exclusive_with_branch_name",
			args:     []string{"scan", "-", "--git-archive", "HEAD", "--branch-name", "--catalog", builtinCatalog},
			wantExit: 2,
		},
		{
			name:     "exit_2_git_archive_mutually_exclusive_with_commit_msg",
			args:     []string{"scan", "-", "--git-archive", "HEAD", "--commit-msg", "--catalog", builtinCatalog},
			wantExit: 2,
		},
		{
			name:     "exit_2_diff_base_without_diff",
			args:     []string{"scan", cleanDir, "--diff-base", "origin/main", "--catalog", builtinCatalog},
			wantExit: 2,
		},
		{
			name:     "exit_2_git_history_mutually_exclusive_with_diff",
			args:     []string{"scan", cleanDir, "--git-history", "--diff", "--catalog", builtinCatalog},
			wantExit: 2,
		},
		{
			name:     "exit_3_path_does_not_exist",
			args:     []string{"scan", "/definitely-not-a-real-path", "--catalog", builtinCatalog, "--visibility", "public_oss"},
			wantExit: 3,
		},
	}

	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	defer devnull.Close()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, tc.args...)
			if tc.stdin != "" {
				cmd.Stdin = strings.NewReader(tc.stdin)
			}
			// Suppress stdout/stderr to keep test output tidy; we only
			// care about the exit code here.
			cmd.Stdout = devnull
			cmd.Stderr = devnull
			err := cmd.Run()
			gotExit := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("unexpected error type from cmd.Run: %T %v", err, err)
				}
				gotExit = exitErr.ExitCode()
			}
			if gotExit != tc.wantExit {
				t.Errorf("exit code = %d, want %d (args: %v)", gotExit, tc.wantExit, tc.args)
			}
		})
	}
}

// buildLimensafeBinary compiles the limensafe binary into a temp directory
// and returns the absolute path. Shared with TestScanOutputStreamContract.
func buildLimensafeBinary(t *testing.T) string {
	t.Helper()
	repoRoot := repoRootFromGoMod(t)

	buildDir := t.TempDir()
	binaryPath := filepath.Join(buildDir, "limensafe")

	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/limensafe")
	build.Dir = repoRoot
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, string(out))
	}
	return binaryPath
}

// repoRootFromGoMod returns the repository root by asking `go env GOMOD`
// and stripping the trailing "/go.mod". Robust to wherever the test is run.
func repoRootFromGoMod(t *testing.T) string {
	t.Helper()
	goModPathBytes, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	goModPath := strings.TrimSpace(string(goModPathBytes))
	if goModPath == "" {
		t.Fatalf("go env GOMOD returned empty")
	}
	return filepath.Dir(goModPath)
}

// buildCleanFixture creates a temp directory containing a single
// minimal Go file with no leak markers, suitable as a clean scan target.
func buildCleanFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := []byte("package x\n\nfunc Hello() string { return \"clean\" }\n")
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), source, 0o644); err != nil {
		t.Fatalf("write clean fixture: %v", err)
	}
	return dir
}

// TestScanOutputStreamContract locks india's stream-separation contract:
// scan output JSON goes to stdout; diagnostics/progress logs go to stderr.
// CI wrappers tee/archive the JSON payload from stdout without mixing log
// lines. This test asserts:
//
//   - clean non-verbose scan: stdout is valid JSON; stderr is empty
//   - verbose (-v) scan: stdout still pure JSON; stderr has log lines
//   - blocking-findings scan: stdout still pure JSON; stderr empty
//
// Origin: partner-integration devlead live-validation feedback, 2026-05-08.
func TestScanOutputStreamContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stream-separation test is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	repoRoot := repoRootFromGoMod(t)
	builtinCatalog := filepath.Join(repoRoot, "pkg", "catalog", "builtin", "public-baseline.yaml")
	seededFixture := filepath.Join(repoRoot, "testdata", "builtin-baseline")
	seededFixtureConfig := filepath.Join(seededFixture, ".limensafe", "config.yaml")

	cleanDir := buildCleanFixture(t)

	type streamCase struct {
		name              string
		args              []string
		wantExit          int
		wantStdoutJSON    bool
		wantStderrNonZero bool
	}

	cases := []streamCase{
		{
			name:              "clean_nonverbose_stdout_json_stderr_empty",
			args:              []string{"scan", cleanDir, "--catalog", builtinCatalog, "--visibility", "public_oss"},
			wantExit:          0,
			wantStdoutJSON:    true,
			wantStderrNonZero: false,
		},
		{
			name:              "clean_verbose_stdout_json_stderr_has_logs",
			args:              []string{"scan", cleanDir, "--catalog", builtinCatalog, "--visibility", "public_oss", "-v"},
			wantExit:          0,
			wantStdoutJSON:    true,
			wantStderrNonZero: true,
		},
		{
			name:              "blocking_findings_stdout_json_stderr_empty",
			args:              []string{"scan", seededFixture, "--config-file", seededFixtureConfig, "--visibility", "public_oss"},
			wantExit:          1,
			wantStdoutJSON:    true,
			wantStderrNonZero: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, tc.args...)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatalf("stdout pipe: %v", err)
			}
			stderr, err := cmd.StderrPipe()
			if err != nil {
				t.Fatalf("stderr pipe: %v", err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatalf("start: %v", err)
			}

			stdoutBytes, _ := io_ReadAll(stdout)
			stderrBytes, _ := io_ReadAll(stderr)

			runErr := cmd.Wait()
			gotExit := 0
			if runErr != nil {
				exitErr, ok := runErr.(*exec.ExitError)
				if !ok {
					t.Fatalf("unexpected error type from cmd.Wait: %T %v", runErr, runErr)
				}
				gotExit = exitErr.ExitCode()
			}

			if gotExit != tc.wantExit {
				t.Errorf("exit code = %d, want %d", gotExit, tc.wantExit)
			}

			if tc.wantStdoutJSON {
				// Cheap JSON-validity check: starts with '{' and ends with '}'.
				trimmed := strings.TrimSpace(string(stdoutBytes))
				if len(trimmed) == 0 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
					t.Errorf("stdout is not a JSON object; first 200 bytes: %q",
						truncate(string(stdoutBytes), 200))
				}
				// Confirm stderr did not leak into stdout: no INFO/DEBUG/FATAL prefixes.
				for _, marker := range []string{"INFO\t", "DEBUG\t", "FATAL", "WARN\t", "ERROR\t"} {
					if strings.Contains(string(stdoutBytes), marker) {
						t.Errorf("stdout contains stderr-like log marker %q — stream separation broken", marker)
					}
				}
			}

			stderrLen := len(strings.TrimSpace(string(stderrBytes)))
			if tc.wantStderrNonZero && stderrLen == 0 {
				t.Errorf("expected stderr to have log content; got empty")
			}
			if !tc.wantStderrNonZero && stderrLen > 0 {
				t.Errorf("expected stderr empty; got %d bytes: %q",
					stderrLen, truncate(string(stderrBytes), 200))
			}
		})
	}
}

func TestScanCatalogWarningStderrContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stream-separation test is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	cleanDir := buildCleanFixture(t)
	catalogPath := filepath.Join(t.TempDir(), "catalog.yaml")
	catalogYAML := `
catalog_id: test-catalog
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-format-1
    class: operational_pattern
    aliases: ["ILT"]
    variants:
      whole_word: true
      case_insensitive: true
`
	if err := os.WriteFile(catalogPath, []byte(catalogYAML), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	cmd := exec.Command(bin, "scan", cleanDir, "--catalog", catalogPath, "--visibility", "public_oss")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("scan with warning: %v", err)
	}
	if trimmed := strings.TrimSpace(stdout.String()); len(trimmed) == 0 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		t.Fatalf("stdout is not JSON object; stdout=%q", truncate(stdout.String(), 200))
	}
	gotStderr := stderr.String()
	if !strings.Contains(gotStderr, "catalog warning: entity: whole_word=true + case_insensitive=true") {
		t.Fatalf("expected catalog warning on stderr, got %q", gotStderr)
	}
	for _, leak := range []string{"e-format-1", "ILT"} {
		if strings.Contains(gotStderr, leak) {
			t.Fatalf("warning stderr leaked %q: %q", leak, gotStderr)
		}
	}
}

func TestScanLimensafeIgnoreStderrAndMetadataContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stream-separation test is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	catalogPath := filepath.Join(t.TempDir(), "catalog.yaml")
	catalogYAML := `
catalog_id: ignore-test-catalog
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-ignore-test-1
    class: operational_pattern
    aliases: ["TEMP_ALIAS_FOR_IGNORE_TEST"]
    blocked_in: [public_oss]
    severity_override: high
`
	if err := os.WriteFile(catalogPath, []byte(catalogYAML), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, ".limensafeignore"), []byte("leak.txt\n"), 0o644); err != nil {
		t.Fatalf("write ignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "leak.txt"), []byte("TEMP_ALIAS_FOR_IGNORE_TEST\n"), 0o644); err != nil {
		t.Fatalf("write leak: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "clean.txt"), []byte("clean\n"), 0o644); err != nil {
		t.Fatalf("write clean: %v", err)
	}

	cmd := exec.Command(bin, "scan", fixture, "--catalog", catalogPath, "--visibility", "public_oss")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("scan with ignore should exit 0: %v\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}

	var payload struct {
		ScanMetadata struct {
			FilesSkipped         int            `json:"files_skipped"`
			FilesSkippedByReason map[string]int `json:"files_skipped_by_reason"`
		} `json:"scan_metadata"`
		Summary struct {
			FindingsTotal int `json:"findings_total"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
		t.Fatalf("stdout JSON: %v\n%s", err, stdout.String())
	}
	if payload.Summary.FindingsTotal != 0 {
		t.Fatalf("expected ignored leak to produce no findings, got %d", payload.Summary.FindingsTotal)
	}
	if payload.ScanMetadata.FilesSkipped != 1 {
		t.Fatalf("files_skipped = %d, want 1", payload.ScanMetadata.FilesSkipped)
	}
	if payload.ScanMetadata.FilesSkippedByReason["ignored"] != 1 {
		t.Fatalf("ignored skip count = %d, want 1", payload.ScanMetadata.FilesSkippedByReason["ignored"])
	}
	gotStderr := stderr.String()
	if !strings.Contains(gotStderr, "scan skip: kind=file reason=ignored source=leak.txt") {
		t.Fatalf("expected ignored skip on stderr, got %q", gotStderr)
	}
	if strings.Contains(gotStderr, "TEMP_ALIAS_FOR_IGNORE_TEST") {
		t.Fatalf("skip stderr leaked protected content: %q", gotStderr)
	}

	cmd = exec.Command(bin, "scan", fixture, "--catalog", catalogPath, "--visibility", "public_oss", "--include-ignored")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	stdout.Reset()
	stderr.Reset()
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("scan --include-ignored exit = %v, want exit 1\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}
}

func TestScanGitArchiveMetadataAndRelativeConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git archive integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	privateCatalogDir := filepath.Join(parent, "private-catalogs")
	if err := os.MkdirAll(privateCatalogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(privateCatalogDir, "archive.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML("ARCHIVE_ALIAS_TEST")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	initCommittedGitRepo(t, repo, map[string]string{
		".limensafe/config.yaml": archiveConfigYAML("../../private-catalogs/archive.catalog.yaml"),
		"leak.txt":               "ARCHIVE_ALIAS_TEST\n",
	})

	cmd := exec.Command(bin, "scan", "--git-archive", "HEAD", "--config-file", ".limensafe/config.yaml")
	cmd.Dir = repo
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("scan exit = %v, want exit 1\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), os.TempDir()) || strings.Contains(stderr.String(), os.TempDir()) {
		t.Fatalf("git archive scan leaked temp path\nstderr=%s\nstdout=%s", stderr.String(), stdout.String())
	}

	var payload struct {
		ScanMetadata struct {
			ScanRoot     string `json:"scan_root"`
			ScanRootKind string `json:"scan_root_kind"`
			GitRef       string `json:"git_ref"`
		} `json:"scan_metadata"`
		Summary struct {
			FindingsTotal int `json:"findings_total"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
		t.Fatalf("stdout JSON: %v\n%s", err, stdout.String())
	}
	if payload.ScanMetadata.ScanRoot != "HEAD" {
		t.Fatalf("scan_root = %q, want HEAD", payload.ScanMetadata.ScanRoot)
	}
	if payload.ScanMetadata.ScanRootKind != "git-archive" {
		t.Fatalf("scan_root_kind = %q, want git-archive", payload.ScanMetadata.ScanRootKind)
	}
	if payload.ScanMetadata.GitRef != "HEAD" {
		t.Fatalf("git_ref = %q, want HEAD", payload.ScanMetadata.GitRef)
	}
	if payload.Summary.FindingsTotal != 1 {
		t.Fatalf("findings_total = %d, want 1", payload.Summary.FindingsTotal)
	}

	bare := exec.Command(bin, "scan", "--git-archive", "--config-file", ".limensafe/config.yaml")
	bare.Dir = repo
	stdout.Reset()
	stderr.Reset()
	bare.Stdout = &stdout
	bare.Stderr = &stderr
	err = bare.Run()
	exitErr, ok = err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("bare --git-archive exit = %v, want exit 1\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}
	if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
		t.Fatalf("bare --git-archive stdout JSON: %v\n%s", err, stdout.String())
	}
	if payload.ScanMetadata.ScanRoot != "HEAD" || payload.ScanMetadata.GitRef != "HEAD" {
		t.Fatalf("bare --git-archive metadata scan_root=%q git_ref=%q, want HEAD/HEAD", payload.ScanMetadata.ScanRoot, payload.ScanMetadata.GitRef)
	}

	twoArg := runScanForJSON(t, bin, parent, []string{
		"scan", repo, "--git-archive", "main",
		"--catalog", catalogPath,
		"--visibility", "public_oss",
	}, 1)
	if twoArg.ScanMetadata.ScanRoot != "main" {
		t.Fatalf("two-arg --git-archive scan_root = %q, want main", twoArg.ScanMetadata.ScanRoot)
	}
	if twoArg.ScanMetadata.ScanRootKind != "git-archive" {
		t.Fatalf("two-arg --git-archive scan_root_kind = %q, want git-archive", twoArg.ScanMetadata.ScanRootKind)
	}
	if twoArg.ScanMetadata.GitRef != "main" {
		t.Fatalf("two-arg --git-archive git_ref = %q, want main", twoArg.ScanMetadata.GitRef)
	}
	if twoArg.Summary.FindingsTotal != 1 {
		t.Fatalf("two-arg --git-archive findings_total = %d, want 1", twoArg.Summary.FindingsTotal)
	}

	invalidStdout, invalidStderr := runScanExpectExit(t, bin, parent, []string{
		"scan", "missing-repo", "--git-archive", "main",
		"--catalog", catalogPath,
		"--visibility", "public_oss",
	}, 2)
	if invalidStdout != "" {
		t.Fatalf("invalid two-arg --git-archive wrote stdout: %s", invalidStdout)
	}
	if !strings.Contains(invalidStderr, "scan <repo> --git-archive <ref>") {
		t.Fatalf("invalid two-arg --git-archive stderr missing actionable form: %s", invalidStderr)
	}
}

func TestScanGitArchiveInvalidRefRedactsProtectedRef(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git archive integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	protectedRef := "SECRET_REF_ALIAS"
	catalogPath := filepath.Join(parent, "archive.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML(protectedRef)), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	initCommittedGitRepo(t, repo, map[string]string{"clean.txt": "clean\n"})

	cmd := exec.Command(bin, "scan", "--git-archive", protectedRef, "--catalog", catalogPath, "--visibility", "public_oss")
	cmd.Dir = repo
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 3 {
		t.Fatalf("scan invalid archive ref exit = %v, want exit 3\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), protectedRef) || strings.Contains(stderr.String(), protectedRef) {
		t.Fatalf("invalid git archive ref leaked protected text\nstderr=%s\nstdout=%s", stderr.String(), stdout.String())
	}
}

func TestScanGitArchiveMatchesManualArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git archive integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	catalogPath := filepath.Join(parent, "archive.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML("ARCHIVE_COMPARE_ALIAS")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	initCommittedGitRepo(t, repo, map[string]string{
		"clean.txt": "clean\n",
		"leak.txt":  "ARCHIVE_COMPARE_ALIAS\n",
	})

	manualDir := filepath.Join(parent, "manual")
	if err := os.MkdirAll(manualDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "archive", "--format=tar", "HEAD", "-o", filepath.Join(parent, "archive.tar"))
	tarCmd := exec.Command("tar", "-x", "-C", manualDir, "-f", filepath.Join(parent, "archive.tar"))
	if out, err := tarCmd.CombinedOutput(); err != nil {
		t.Fatalf("tar extract: %v\n%s", err, out)
	}

	manual := runScanForJSON(t, bin, repo, []string{"scan", manualDir, "--catalog", catalogPath, "--visibility", "public_oss"}, 1)
	native := runScanForJSON(t, bin, repo, []string{"scan", "--git-archive", "HEAD", "--catalog", catalogPath, "--visibility", "public_oss"}, 1)

	normalizeVolatileScanMetadata(manual)
	normalizeVolatileScanMetadata(native)
	if manual.Summary.FindingsTotal != native.Summary.FindingsTotal {
		t.Fatalf("findings_total manual=%d native=%d", manual.Summary.FindingsTotal, native.Summary.FindingsTotal)
	}
	if len(manual.Findings) != len(native.Findings) {
		t.Fatalf("findings len manual=%d native=%d", len(manual.Findings), len(native.Findings))
	}
	for i := range manual.Findings {
		if manual.Findings[i].Location.Path != native.Findings[i].Location.Path {
			t.Fatalf("finding[%d] path manual=%q native=%q", i, manual.Findings[i].Location.Path, native.Findings[i].Location.Path)
		}
		if manual.Findings[i].DetectorID != native.Findings[i].DetectorID ||
			manual.Findings[i].Decision != native.Findings[i].Decision ||
			manual.Findings[i].Severity != native.Findings[i].Severity {
			t.Fatalf("finding[%d] mismatch manual=%+v native=%+v", i, manual.Findings[i], native.Findings[i])
		}
	}
}

func TestGitArchiveHistoryAndAttestSupportBareMirror(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git archive integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	mirror := filepath.Join(parent, "repo.git")
	catalogPath := filepath.Join(parent, "bare-mirror.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML("BARE_MIRROR_ALIAS")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	initCommittedGitRepo(t, repo, map[string]string{
		"clean.txt": "clean\n",
	})
	runGit(t, parent, "clone", "--mirror", repo, mirror)

	worktreeArchive := runScanForJSON(t, bin, parent, []string{"scan", repo, "--git-archive=HEAD", "--catalog", catalogPath, "--visibility", "public_oss"}, 0)
	mirrorArchive := runScanForJSON(t, bin, parent, []string{"scan", mirror, "--git-archive=HEAD", "--catalog", catalogPath, "--visibility", "public_oss"}, 0)
	if worktreeArchive.ScanMetadata.ScanRootKind != "git-archive" || mirrorArchive.ScanMetadata.ScanRootKind != "git-archive" {
		t.Fatalf("scan_root_kind worktree=%q mirror=%q, want git-archive", worktreeArchive.ScanMetadata.ScanRootKind, mirrorArchive.ScanMetadata.ScanRootKind)
	}
	if worktreeArchive.ScanMetadata.GitRef != "HEAD" || mirrorArchive.ScanMetadata.GitRef != "HEAD" {
		t.Fatalf("git_ref worktree=%q mirror=%q, want HEAD", worktreeArchive.ScanMetadata.GitRef, mirrorArchive.ScanMetadata.GitRef)
	}
	if worktreeArchive.Summary.FindingsTotal != mirrorArchive.Summary.FindingsTotal ||
		worktreeArchive.ScanMetadata.FilesScanned != mirrorArchive.ScanMetadata.FilesScanned ||
		worktreeArchive.ScanMetadata.BytesScanned != mirrorArchive.ScanMetadata.BytesScanned {
		t.Fatalf("mirror archive mismatch: worktree findings/files/bytes=%d/%d/%d mirror=%d/%d/%d",
			worktreeArchive.Summary.FindingsTotal, worktreeArchive.ScanMetadata.FilesScanned, worktreeArchive.ScanMetadata.BytesScanned,
			mirrorArchive.Summary.FindingsTotal, mirrorArchive.ScanMetadata.FilesScanned, mirrorArchive.ScanMetadata.BytesScanned)
	}

	mirrorHistory := runScanForJSON(t, bin, parent, []string{"scan", mirror, "--git-history-all", "--catalog", catalogPath, "--visibility", "public_oss"}, 0)
	if mirrorHistory.ScanMetadata.ScanRootKind != "git-history" {
		t.Fatalf("history scan_root_kind = %q, want git-history", mirrorHistory.ScanMetadata.ScanRootKind)
	}
	if mirrorHistory.ScanMetadata.HistoryCommitsScanned == 0 {
		t.Fatal("mirror history scanned zero commits")
	}

	runAttestExpectExit(t, bin, repo, []string{"attest", ".", "--catalog", catalogPath, "--visibility", "public_oss", "--diff-base", "HEAD"}, 0)
	bareAttestation := filepath.Join(parent, "bare-attestation.json")
	runAttestExpectExit(t, bin, parent, []string{"attest", mirror, "--catalog", catalogPath, "--visibility", "public_oss", "--output", bareAttestation}, 0)
	var att struct {
		CommitSHA    string `json:"commit_sha"`
		ExitCode     int    `json:"exit_code"`
		ScanMetadata struct {
			FilesScanned int `json:"files_scanned"`
		} `json:"scan_metadata"`
	}
	data, err := os.ReadFile(bareAttestation)
	if err != nil {
		t.Fatalf("read bare attestation: %v", err)
	}
	if err := json.Unmarshal(data, &att); err != nil {
		t.Fatalf("parse bare attestation: %v\n%s", err, data)
	}
	if att.CommitSHA != strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD")) {
		t.Fatalf("bare attestation commit_sha = %q, want repo HEAD", att.CommitSHA)
	}
	if att.ExitCode != 0 || att.ScanMetadata.FilesScanned == 0 {
		t.Fatalf("bare attestation exit/files = %d/%d, want 0/nonzero", att.ExitCode, att.ScanMetadata.FilesScanned)
	}
}

func TestScanGitArchiveHonorsLimensafeIgnore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git archive integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	catalogPath := filepath.Join(parent, "ignore.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML("ARCHIVE_IGNORE_ALIAS")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	initCommittedGitRepo(t, repo, map[string]string{
		".limensafeignore": "leak.txt\n",
		"clean.txt":        "clean\n",
		"leak.txt":         "ARCHIVE_IGNORE_ALIAS\n",
	})

	clean := runScanForJSON(t, bin, repo, []string{"scan", "--git-archive", "HEAD", "--catalog", catalogPath, "--visibility", "public_oss"}, 0)
	if clean.Summary.FindingsTotal != 0 {
		t.Fatalf("ignored archive leak produced %d findings", clean.Summary.FindingsTotal)
	}
	if clean.ScanMetadata.FilesSkipped != 1 || clean.ScanMetadata.FilesSkippedByReason["ignored"] != 1 {
		t.Fatalf("ignored skip metadata = files=%d reasons=%v", clean.ScanMetadata.FilesSkipped, clean.ScanMetadata.FilesSkippedByReason)
	}

	included := runScanForJSON(t, bin, repo, []string{"scan", "--git-archive", "HEAD", "--catalog", catalogPath, "--visibility", "public_oss", "--include-ignored"}, 1)
	if included.Summary.FindingsTotal != 1 {
		t.Fatalf("--include-ignored findings_total = %d, want 1", included.Summary.FindingsTotal)
	}
}

func TestScanGitDiffIntroducedLinesOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git diff integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	catalogPath := filepath.Join(parent, "diff.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML("DIFF_ONLY_ALIAS")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	initCommittedGitRepo(t, repo, map[string]string{
		"leak.txt": "DIFF_ONLY_ALIAS pre-existing\nclean\n",
	})
	base := strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(repo, "leak.txt"), []byte("DIFF_ONLY_ALIAS pre-existing\nclean\nDIFF_ONLY_ALIAS introduced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "leak.txt")
	runGit(t, repo, "commit", "-q", "-m", "introduce diff line")

	payload := runScanForJSON(t, bin, repo, []string{"scan", repo, "--diff", "--diff-base", base, "--catalog", catalogPath, "--visibility", "public_oss"}, 1)
	if payload.Summary.FindingsTotal != 1 {
		t.Fatalf("findings_total = %d, want 1", payload.Summary.FindingsTotal)
	}
	finding := payload.Findings[0]
	if finding.Location.Path != "leak.txt" {
		t.Fatalf("finding path = %q, want leak.txt", finding.Location.Path)
	}
	if finding.Location.Line != 3 {
		t.Fatalf("finding line = %d, want 3", finding.Location.Line)
	}
	if finding.Location.SurfaceKind != "diff" {
		t.Fatalf("surface_kind = %q, want diff", finding.Location.SurfaceKind)
	}
	if payload.ScanMetadata.ScanRootKind != "git-diff" {
		t.Fatalf("scan_root_kind = %q, want git-diff", payload.ScanMetadata.ScanRootKind)
	}
}

func TestScanGitDiffInvalidBaseIsConfigError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git diff integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	protectedRef := "DIFF_ONLY_ALIAS_bad_ref"
	catalogPath := filepath.Join(parent, "diff.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML("DIFF_ONLY_ALIAS")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	initCommittedGitRepo(t, repo, map[string]string{"clean.txt": "clean\n"})

	stdout, stderr := runScanExpectExit(t, bin, repo, []string{"scan", repo, "--diff", "--diff-base", protectedRef, "--catalog", catalogPath, "--visibility", "public_oss"}, 2)
	if stdout != "" {
		t.Fatalf("invalid diff base wrote stdout: %s", stdout)
	}
	if strings.Contains(stderr, protectedRef) || strings.Contains(stderr, "DIFF_ONLY_ALIAS") {
		t.Fatalf("invalid diff base leaked protected ref\nstderr=%s", stderr)
	}
}

func TestScanGitHistoryFindsDeletedBlob(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git history integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	catalogPath := filepath.Join(parent, "history.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML("HISTORY_ONLY_ALIAS")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	initCommittedGitRepo(t, repo, map[string]string{
		"secret.txt": "HISTORY_ONLY_ALIAS deleted later\n",
		"clean.txt":  "clean\n",
	})
	leakCommit := strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD"))
	runGit(t, repo, "rm", "-q", "secret.txt")
	runGit(t, repo, "commit", "-q", "-m", "remove historical leak")

	clean := runScanForJSON(t, bin, repo, []string{"scan", repo, "--catalog", catalogPath, "--visibility", "public_oss"}, 0)
	if clean.Summary.FindingsTotal != 0 {
		t.Fatalf("working-tree scan findings_total = %d, want 0", clean.Summary.FindingsTotal)
	}

	payload := runScanForJSON(t, bin, repo, []string{"scan", repo, "--git-history", "--catalog", catalogPath, "--visibility", "public_oss"}, 1)
	if payload.Summary.FindingsTotal != 1 {
		t.Fatalf("history findings_total = %d, want 1", payload.Summary.FindingsTotal)
	}
	finding := payload.Findings[0]
	if finding.Location.Path != "secret.txt" {
		t.Fatalf("history finding path = %q, want secret.txt", finding.Location.Path)
	}
	if finding.Location.GitRef != leakCommit {
		t.Fatalf("history finding git_ref = %q, want %q", finding.Location.GitRef, leakCommit)
	}
	if finding.Location.SurfaceKind != "blob" {
		t.Fatalf("history surface_kind = %q, want blob", finding.Location.SurfaceKind)
	}
	if payload.ScanMetadata.ScanRootKind != "git-history" || payload.ScanMetadata.GitRef != "--all" {
		t.Fatalf("history metadata scan_root_kind=%q git_ref=%q, want git-history/--all", payload.ScanMetadata.ScanRootKind, payload.ScanMetadata.GitRef)
	}
	if payload.ScanMetadata.HistoryUniqueBlobs == 0 || payload.ScanMetadata.HistoryBlobsScanned == 0 || payload.ScanMetadata.HistoryCommitsScanned != 2 {
		t.Fatalf("history metadata unique=%d blobs=%d commits=%d, want nonzero/nonzero/2",
			payload.ScanMetadata.HistoryUniqueBlobs,
			payload.ScanMetadata.HistoryBlobsScanned,
			payload.ScanMetadata.HistoryCommitsScanned,
		)
	}
}

func TestScanGitCommitMessagesFindsHistoricalMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git history integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	catalogPath := filepath.Join(parent, "history-msg.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML("HISTORY_MSG_ALIAS")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	initCommittedGitRepo(t, repo, map[string]string{"clean.txt": "clean\n"})
	runGit(t, repo, "commit", "--allow-empty", "-q", "-m", "subject", "-m", "body contains HISTORY_MSG_ALIAS")
	commit := strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD"))

	payload := runScanForJSON(t, bin, repo, []string{"scan", repo, "--git-commit-messages", "--catalog", catalogPath, "--visibility", "public_oss"}, 1)
	if payload.Summary.FindingsTotal != 1 {
		t.Fatalf("commit-message findings_total = %d, want 1", payload.Summary.FindingsTotal)
	}
	finding := payload.Findings[0]
	if finding.Location.Path != "" {
		t.Fatalf("commit-message path = %q, want empty", finding.Location.Path)
	}
	if finding.Location.SourceID != "commit_message" {
		t.Fatalf("commit-message source_id = %q, want commit_message", finding.Location.SourceID)
	}
	if finding.Location.GitRef != commit {
		t.Fatalf("commit-message git_ref = %q, want %q", finding.Location.GitRef, commit)
	}
	if finding.Location.SurfaceKind != "commit_message" {
		t.Fatalf("commit-message surface_kind = %q, want commit_message", finding.Location.SurfaceKind)
	}
	if payload.ScanMetadata.HistoryCommitsScanned != 2 {
		t.Fatalf("history_commits_scanned = %d, want 2", payload.ScanMetadata.HistoryCommitsScanned)
	}
}

func TestScanGitHistoryInitErrorRedactsProtectedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git history integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	protected := "HISTORY_INIT_ALIAS"
	nonGit := filepath.Join(parent, protected+"-repo")
	if err := os.MkdirAll(nonGit, 0o755); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(parent, "history-init.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML(protected)), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	stdout, stderr := runScanExpectExit(t, bin, nonGit, []string{"scan", nonGit, "--git-history", "--catalog", catalogPath, "--visibility", "public_oss"}, 3)
	if stdout != "" {
		t.Fatalf("history init error wrote stdout: %s", stdout)
	}
	if strings.Contains(stderr, protected) {
		t.Fatalf("history init error leaked protected path\nstderr=%s", stderr)
	}
}

func TestScanGitHistorySameBlobMultipleExtensionsOnlyExpandsEligiblePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git history integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	catalogPath := filepath.Join(parent, "history-ext.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML("HISTORY_EXT_ALIAS")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	initCommittedGitRepo(t, repo, map[string]string{
		"a.png": "HISTORY_EXT_ALIAS same blob\n",
		"b.txt": "HISTORY_EXT_ALIAS same blob\n",
	})

	payload := runScanForJSON(t, bin, repo, []string{"scan", repo, "--git-history", "--catalog", catalogPath, "--visibility", "public_oss"}, 1)
	if payload.Summary.FindingsTotal != 1 {
		t.Fatalf("history findings_total = %d, want 1", payload.Summary.FindingsTotal)
	}
	finding := payload.Findings[0]
	if finding.Location.Path != "b.txt" {
		t.Fatalf("history finding path = %q, want b.txt", finding.Location.Path)
	}
	if payload.ScanMetadata.FilesSkippedByReason["binary_detected"] != 1 {
		t.Fatalf("binary skip count = %d, want 1", payload.ScanMetadata.FilesSkippedByReason["binary_detected"])
	}
}

func TestScanGitHistoryFindsDeletedPathSegment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git history integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	protected := "HISTORY_PATH_ALIAS"
	catalogPath := filepath.Join(parent, "history-path.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML(protected)), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	initCommittedGitRepo(t, repo, map[string]string{
		filepath.Join(protected+"-dir", "clean.txt"): "clean content\n",
	})
	pathCommit := strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD"))
	runGit(t, repo, "rm", "-qr", protected+"-dir")
	runGit(t, repo, "commit", "-q", "-m", "remove historical path")

	payload := runScanForJSON(t, bin, repo, []string{"scan", repo, "--git-history", "--catalog", catalogPath, "--visibility", "public_oss"}, 1)
	if payload.Summary.FindingsTotal != 1 {
		t.Fatalf("history path findings_total = %d, want 1", payload.Summary.FindingsTotal)
	}
	finding := payload.Findings[0]
	if finding.DetectorID != "path-segment" {
		t.Fatalf("detector_id = %q, want path-segment", finding.DetectorID)
	}
	if finding.Location.GitRef != pathCommit {
		t.Fatalf("path finding git_ref = %q, want %q", finding.Location.GitRef, pathCommit)
	}
	if finding.Location.SurfaceKind != "blob" {
		t.Fatalf("path finding surface_kind = %q, want blob", finding.Location.SurfaceKind)
	}
	if strings.Contains(finding.Location.Path, protected) {
		t.Fatalf("path finding leaked protected path: %q", finding.Location.Path)
	}
}

func TestScanGitHistoryParallelBlobAttribution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git history integration is unix-focused")
	}

	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	protected := "HISTORY_PARALLEL_ALIAS"
	catalogPath := filepath.Join(parent, "history-parallel.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(archiveCatalogYAML(protected)), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	files := map[string]string{"clean.txt": "clean\n"}
	for i := 0; i < 40; i++ {
		files[fmt.Sprintf("historical-%02d.txt", i)] = fmt.Sprintf("%s unique historical content %02d\n", protected, i)
	}
	initCommittedGitRepo(t, repo, files)
	runGit(t, repo, "rm", "-q", "historical-*.txt")
	runGit(t, repo, "commit", "-q", "-m", "remove historical parallel fixtures")

	payload := runScanForJSON(t, bin, repo, []string{
		"scan", repo,
		"--git-history",
		"--workers", "8",
		"--catalog", catalogPath,
		"--visibility", "public_oss",
	}, 1)
	if payload.Summary.FindingsTotal != 40 {
		t.Fatalf("history findings_total = %d, want 40", payload.Summary.FindingsTotal)
	}
	if payload.ScanMetadata.HistoryBlobsScanned < 40 {
		t.Fatalf("history_blobs_scanned = %d, want at least 40", payload.ScanMetadata.HistoryBlobsScanned)
	}
}

func TestScanMissingPrivateCatalogWarnMode(t *testing.T) {
	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	fixture := filepath.Join(parent, "repo")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "clean.txt"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(parent, "config.yaml")
	if err := os.WriteFile(configPath, []byte(missingPrivateConfigYAML("warn")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIMENSAFE_PRIVATE_TEST_CATALOG", "")

	payload, stdout, stderr := runScanForJSONWithStreams(t, bin, fixture, []string{"scan", fixture, "--config-file", configPath}, 0)
	if stderr != "" {
		t.Fatalf("warn mode wrote stderr diagnostics: %q", stderr)
	}
	if strings.Contains(stdout, "LIMENSAFE_PRIVATE_TEST_CATALOG") || strings.Contains(stderr, "LIMENSAFE_PRIVATE_TEST_CATALOG") {
		t.Fatalf("warn mode leaked env var name\nstderr=%s\nstdout=%s", stderr, stdout)
	}
	if payload.Summary.FindingsTotal != 0 {
		t.Fatalf("findings_total = %d, want 0 for config warning", payload.Summary.FindingsTotal)
	}
	if len(payload.Findings) != 1 {
		t.Fatalf("findings len = %d, want one config warning", len(payload.Findings))
	}
	warning := payload.Findings[0]
	if warning.Kind != "config-warning" || warning.DetectorID != "private-catalog-missing" || warning.Decision != "warn" {
		t.Fatalf("warning finding = %+v", warning)
	}
	if warning.Location.SourceID != "private-test" {
		t.Fatalf("warning source_id = %q, want private-test", warning.Location.SourceID)
	}
	if len(payload.ScanMetadata.PrivateCatalogsStatus) != 1 {
		t.Fatalf("private_catalogs_status = %+v", payload.ScanMetadata.PrivateCatalogsStatus)
	}
	privateStatus := payload.ScanMetadata.PrivateCatalogsStatus[0]
	if privateStatus.CatalogID != "private-test" || privateStatus.SourceKind != "env" ||
		privateStatus.Status != "missing-warn" || privateStatus.Reason != "env_unset" {
		t.Fatalf("private catalog status = %+v", privateStatus)
	}
}

func TestScanMissingPrivateCatalogWarnModePrivateOnly(t *testing.T) {
	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	fixture := filepath.Join(parent, "repo")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "clean.txt"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(parent, "config.yaml")
	if err := os.WriteFile(configPath, []byte(privateOnlyMissingConfigYAML("warn")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIMENSAFE_PRIVATE_TEST_CATALOG", "")

	payload, stdout, stderr := runScanForJSONWithStreams(t, bin, fixture, []string{"scan", fixture, "--config-file", configPath}, 0)
	if stderr != "" {
		t.Fatalf("private-only warn mode wrote stderr diagnostics: %q", stderr)
	}
	if strings.Contains(stdout, "LIMENSAFE_PRIVATE_TEST_CATALOG") || strings.Contains(stderr, "LIMENSAFE_PRIVATE_TEST_CATALOG") {
		t.Fatalf("private-only warn mode leaked env var name\nstderr=%s\nstdout=%s", stderr, stdout)
	}
	if payload.Summary.FindingsTotal != 0 {
		t.Fatalf("findings_total = %d, want 0 for private-only config warning", payload.Summary.FindingsTotal)
	}
	if len(payload.Findings) != 1 {
		t.Fatalf("findings len = %d, want one config warning", len(payload.Findings))
	}
	warning := payload.Findings[0]
	if warning.Kind != "config-warning" || warning.DetectorID != "private-catalog-missing" || warning.Decision != "warn" {
		t.Fatalf("warning finding = %+v", warning)
	}
	if warning.Location.SourceID != "private-test" {
		t.Fatalf("warning source_id = %q, want private-test", warning.Location.SourceID)
	}
}

func TestScanMissingPrivateCatalogErrorMode(t *testing.T) {
	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	fixture := filepath.Join(parent, "repo")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(parent, "config.yaml")
	if err := os.WriteFile(configPath, []byte(missingPrivateConfigYAML("error")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIMENSAFE_PRIVATE_TEST_CATALOG", "")

	stdout, stderr := runScanExpectExit(t, bin, fixture, []string{"scan", fixture, "--config-file", configPath}, 2)
	if stdout != "" {
		t.Fatalf("error mode wrote stdout: %s", stdout)
	}
	if strings.Contains(stderr, "LIMENSAFE_PRIVATE_TEST_CATALOG") {
		t.Fatalf("error mode leaked env var name: %s", stderr)
	}
}

func TestScanModeAndPrivateCatalogMissingOverridePrecedence(t *testing.T) {
	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	fixture := filepath.Join(parent, "repo")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(parent, "config.yaml")
	if err := os.WriteFile(configPath, []byte(missingPrivateConfigYAML("silent")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIMENSAFE_PRIVATE_TEST_CATALOG", "")

	_, stderr := runScanExpectExit(t, bin, fixture, []string{"scan", fixture, "--config-file", configPath, "--mode", "ci"}, 2)
	if strings.Contains(stderr, "LIMENSAFE_PRIVATE_TEST_CATALOG") {
		t.Fatalf("--mode ci leaked env var name: %s", stderr)
	}

	payload, _, stderr := runScanForJSONWithStreams(t, bin, fixture, []string{
		"scan", fixture, "--config-file", configPath,
		"--mode", "ci",
		"--private-catalog-missing", "warn",
	}, 0)
	if stderr != "" {
		t.Fatalf("explicit warn override wrote stderr: %q", stderr)
	}
	if len(payload.Findings) != 1 || payload.Findings[0].Kind != "config-warning" {
		t.Fatalf("expected explicit override warning, got %+v", payload.Findings)
	}
}

func TestScanModeReleaseUsesMediumBlockThreshold(t *testing.T) {
	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	fixture := filepath.Join(parent, "repo")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "leak.txt"), []byte("MEDIUM_MODE_ALIAS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(parent, "medium.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(mediumCatalogYAML()), 0o644); err != nil {
		t.Fatal(err)
	}

	local := runScanForJSON(t, bin, fixture, []string{"scan", fixture, "--catalog", catalogPath, "--visibility", "public_oss"}, 0)
	if local.Summary.FindingsTotal != 1 || local.Findings[0].Decision != "warn" {
		t.Fatalf("default threshold payload = %+v", local)
	}

	release := runScanForJSON(t, bin, fixture, []string{"scan", fixture, "--catalog", catalogPath, "--visibility", "public_oss", "--mode", "release"}, 1)
	if release.Summary.FindingsTotal != 1 || release.Findings[0].Decision != "block" {
		t.Fatalf("release threshold payload = %+v", release)
	}
}

func TestScanFindingsIncludeEntityID(t *testing.T) {
	bin := buildLimensafeBinary(t)
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "main.go"), []byte("package main\n\nconst codename = \"Horizon\"\nconst other = \"Tilden\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(fixture, "entity-id.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(entityIDCatalogYAML()), 0o644); err != nil {
		t.Fatal(err)
	}

	payload := runScanForJSON(t, bin, fixture, []string{"scan", fixture, "--catalog", catalogPath, "--visibility", "public_oss"}, 1)
	if payload.Summary.FindingsTotal < 2 {
		t.Fatalf("findings_total = %d, want at least 2", payload.Summary.FindingsTotal)
	}
	got := map[string]bool{}
	for _, finding := range payload.Findings {
		if finding.Kind == "" || finding.Kind == "detection" {
			if finding.EntityID == "" {
				t.Fatalf("detection finding missing entity_id: %+v", finding)
			}
		}
		got[finding.EntityID] = true
	}
	for _, want := range []string{"e-codename-1", "e-codename-2"} {
		if !got[want] {
			t.Fatalf("entity_id %q missing from findings: %+v", want, payload.Findings)
		}
	}
}

func TestScanRejectsAliasBearingEntityIDWithoutLeak(t *testing.T) {
	bin := buildLimensafeBinary(t)
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "main.go"), []byte("package main\n\nconst client = \"Acme\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(fixture, "unsafe.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(aliasBearingEntityIDCatalogYAML()), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := runScanExpectExit(t, bin, fixture, []string{"scan", fixture, "--catalog", catalogPath, "--visibility", "public_oss"}, 2)
	if stdout != "" {
		t.Fatalf("unsafe catalog wrote stdout: %s", stdout)
	}
	for _, leak := range []string{"e-acme-1", "Acme", "acme"} {
		if strings.Contains(stdout, leak) || strings.Contains(stderr, leak) {
			t.Fatalf("unsafe catalog error leaked %q\nstderr=%s\nstdout=%s", leak, stderr, stdout)
		}
	}
	if !strings.Contains(stderr, "protected alias substring") {
		t.Fatalf("stderr missing sanitized validation reason: %s", stderr)
	}
}

func TestScanRejectsEntityIDContainingAliasFromAnotherCatalogWithoutLeak(t *testing.T) {
	bin := buildLimensafeBinary(t)
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "main.go"), []byte("package main\n\nconst leak = \"FooLeak\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogAPath := filepath.Join(fixture, "catalog-a.yaml")
	if err := os.WriteFile(catalogAPath, []byte(crossCatalogUnsafeEntityIDCatalogAYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogBPath := filepath.Join(fixture, "catalog-b.yaml")
	if err := os.WriteFile(catalogBPath, []byte(crossCatalogUnsafeEntityIDCatalogBYAML()), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := runScanExpectExit(t, bin, fixture, []string{
		"scan", fixture,
		"--catalog", catalogAPath,
		"--catalog", catalogBPath,
		"--visibility", "public_oss",
	}, 2)
	if stdout != "" {
		t.Fatalf("unsafe cross-catalog scan wrote stdout: %s", stdout)
	}
	for _, leak := range []string{"e-acme-1", "acme", "FooLeak"} {
		if strings.Contains(stdout, leak) || strings.Contains(stderr, leak) {
			t.Fatalf("cross-catalog safety error leaked %q\nstderr=%s\nstdout=%s", leak, stderr, stdout)
		}
	}
	if !strings.Contains(stderr, "protected alias substring") {
		t.Fatalf("stderr missing sanitized validation reason: %s", stderr)
	}
}

func TestScanMalformedUnsafeEntityIDErrorDoesNotLeak(t *testing.T) {
	bin := buildLimensafeBinary(t)
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "main.go"), []byte("package main\n\nconst client = \"Acme\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(fixture, "malformed.yaml")
	if err := os.WriteFile(catalogPath, []byte(malformedUnsafeEntityIDCatalogYAML()), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := runScanExpectExit(t, bin, fixture, []string{"scan", fixture, "--catalog", catalogPath, "--visibility", "public_oss"}, 2)
	if stdout != "" {
		t.Fatalf("malformed unsafe catalog wrote stdout: %s", stdout)
	}
	for _, leak := range []string{"e-acme-1", "Acme", "acme"} {
		if strings.Contains(stdout, leak) || strings.Contains(stderr, leak) {
			t.Fatalf("malformed catalog error leaked %q\nstderr=%s\nstdout=%s", leak, stderr, stdout)
		}
	}
	// The catalog JSON Schema is the front gate: a missing required field
	// surfaces as a redaction-safe pointer+keyword reason, never the unsafe id.
	if !strings.Contains(stderr, "/entities/0") || !strings.Contains(stderr, "required") {
		t.Fatalf("stderr missing sanitized validation reason: %s", stderr)
	}
}

func TestScanCatalogMatchKeyDiagnosticIsRedactionSafe(t *testing.T) {
	bin := buildLimensafeBinary(t)
	fixture := t.TempDir()
	protected := "SECRET_MATCH_ALIAS_123"
	if err := os.WriteFile(filepath.Join(fixture, "main.go"), []byte("package main\n\nconst client = \"clean\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(fixture, "match-shape.yaml")
	if err := os.WriteFile(catalogPath, []byte(matchKeyCatalogYAML(protected)), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := runScanExpectExit(t, bin, fixture, []string{"scan", fixture, "--catalog", catalogPath, "--visibility", "public_oss"}, 2)
	if stdout != "" {
		t.Fatalf("match-key catalog wrote stdout: %s", stdout)
	}
	if !strings.Contains(stderr, "unknown field match") ||
		!strings.Contains(stderr, "did you mean regex_patterns") {
		t.Fatalf("stderr missing safe match-key guidance: %s", stderr)
	}
	for _, leak := range []string{protected, "SECRET_MATCH_ALIAS", "SECRET_MATCH_ALIAS_[0-9]+"} {
		if strings.Contains(stdout, leak) || strings.Contains(stderr, leak) {
			t.Fatalf("match-key catalog diagnostic leaked %q\nstderr=%s\nstdout=%s", leak, stderr, stdout)
		}
	}
}

func TestScanCatalogIDMismatchDoesNotLeakConfigIDAlias(t *testing.T) {
	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	fixture := filepath.Join(parent, "repo")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "main.go"), []byte("package main\n\nconst client = \"clean\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(parent, "safe-declared.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(catalogIDMismatchLoadedCatalogYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(parent, "config.yaml")
	if err := os.WriteFile(configPath, []byte(catalogIDMismatchConfigYAML(catalogPath)), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := runScanExpectExit(t, bin, fixture, []string{"scan", fixture, "--config-file", configPath}, 2)
	if stdout != "" {
		t.Fatalf("catalog mismatch wrote stdout: %s", stdout)
	}
	for _, leak := range []string{"expected-acme-id", "safe-declared-id", "acme"} {
		if strings.Contains(stdout, leak) || strings.Contains(stderr, leak) {
			t.Fatalf("catalog mismatch error leaked %q\nstderr=%s\nstdout=%s", leak, stderr, stdout)
		}
	}
	if !strings.Contains(stderr, "catalog id mismatch") {
		t.Fatalf("stderr missing sanitized mismatch reason: %s", stderr)
	}
}

func TestScanRequiredMissingCatalogDoesNotLeakPriorAlias(t *testing.T) {
	bin := buildLimensafeBinary(t)
	parent := t.TempDir()
	fixture := filepath.Join(parent, "repo")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "main.go"), []byte("package main\n\nconst client = \"clean\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(parent, "safe-declared.catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(catalogIDMismatchLoadedCatalogYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(parent, "config.yaml")
	if err := os.WriteFile(configPath, []byte(requiredMissingCatalogAfterLoadedAliasConfigYAML(catalogPath)), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := runScanExpectExit(t, bin, fixture, []string{"scan", fixture, "--config-file", configPath}, 2)
	if stdout != "" {
		t.Fatalf("missing required catalog wrote stdout: %s", stdout)
	}
	for _, leak := range []string{"expected-acme-id", "safe-declared-id", "acme"} {
		if strings.Contains(stdout, leak) || strings.Contains(stderr, leak) {
			t.Fatalf("missing required catalog error leaked %q\nstderr=%s\nstdout=%s", leak, stderr, stdout)
		}
	}
	if !strings.Contains(stderr, "required catalog: source kind file unavailable") {
		t.Fatalf("stderr missing sanitized required-catalog reason: %s", stderr)
	}
}

type scanJSONPayload struct {
	ScanMetadata struct {
		StartedAt             string         `json:"started_at"`
		DurationMS            int64          `json:"duration_ms"`
		ScanRoot              string         `json:"scan_root"`
		ScanRootKind          string         `json:"scan_root_kind"`
		GitRef                string         `json:"git_ref"`
		FilesSkipped          int            `json:"files_skipped"`
		FilesSkippedByReason  map[string]int `json:"files_skipped_by_reason"`
		DirsSkipped           int            `json:"directories_skipped"`
		WorkerCount           int            `json:"worker_count"`
		FilesScanned          int            `json:"files_scanned"`
		BytesScanned          int64          `json:"bytes_scanned"`
		HistoryBlobsScanned   int            `json:"history_blobs_scanned"`
		HistoryCommitsScanned int            `json:"history_commits_scanned"`
		HistoryUniqueBlobs    int            `json:"history_unique_blobs"`
		PrivateCatalogsStatus []struct {
			CatalogID  string `json:"catalog_id"`
			SourceKind string `json:"source_kind"`
			Status     string `json:"status"`
			Reason     string `json:"reason"`
		} `json:"private_catalogs_status"`
	} `json:"scan_metadata"`
	Summary struct {
		FindingsTotal int `json:"findings_total"`
	} `json:"summary"`
	Findings []struct {
		Kind       string `json:"kind"`
		EntityID   string `json:"entity_id"`
		DetectorID string `json:"detector_id"`
		Severity   string `json:"severity"`
		Decision   string `json:"decision"`
		Location   struct {
			Path        string `json:"path"`
			SourceID    string `json:"source_id"`
			Line        int    `json:"line"`
			GitRef      string `json:"git_ref"`
			SurfaceKind string `json:"surface_kind"`
		} `json:"location"`
	} `json:"findings"`
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

func runScanForJSON(t *testing.T, bin, dir string, args []string, wantExit int) scanJSONPayload {
	t.Helper()
	payload, _, _ := runScanForJSONWithStreams(t, bin, dir, args, wantExit)
	return payload
}

func runScanForJSONWithStreams(t *testing.T, bin, dir string, args []string, wantExit int) (scanJSONPayload, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	gotExit := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("unexpected scan error: %T %v\nstderr=%s\nstdout=%s", err, err, stderr.String(), stdout.String())
		}
		gotExit = exitErr.ExitCode()
	}
	if gotExit != wantExit {
		t.Fatalf("scan exit = %d, want %d\nstderr=%s\nstdout=%s", gotExit, wantExit, stderr.String(), stdout.String())
	}
	var payload scanJSONPayload
	if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
		t.Fatalf("stdout JSON: %v\n%s", err, stdout.String())
	}
	return payload, stdout.String(), stderr.String()
}

func runScanExpectExit(t *testing.T, bin, dir string, args []string, wantExit int) (string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	gotExit := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("unexpected scan error: %T %v\nstderr=%s\nstdout=%s", err, err, stderr.String(), stdout.String())
		}
		gotExit = exitErr.ExitCode()
	}
	if gotExit != wantExit {
		t.Fatalf("scan exit = %d, want %d\nstderr=%s\nstdout=%s", gotExit, wantExit, stderr.String(), stdout.String())
	}
	return stdout.String(), stderr.String()
}

func runAttestExpectExit(t *testing.T, bin, dir string, args []string, wantExit int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	gotExit := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("unexpected attest error: %T %v\nstderr=%s\nstdout=%s", err, err, stderr.String(), stdout.String())
		}
		gotExit = exitErr.ExitCode()
	}
	if gotExit != wantExit {
		t.Fatalf("attest exit = %d, want %d\nstderr=%s\nstdout=%s", gotExit, wantExit, stderr.String(), stdout.String())
	}
}

func normalizeVolatileScanMetadata(payload scanJSONPayload) scanJSONPayload {
	payload.ScanMetadata.StartedAt = ""
	payload.ScanMetadata.DurationMS = 0
	payload.ScanMetadata.ScanRoot = ""
	payload.ScanMetadata.ScanRootKind = ""
	payload.ScanMetadata.GitRef = ""
	return payload
}

func initCommittedGitRepo(t *testing.T, root string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-q", "-b", "main")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	for path, content := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "fixture")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func archiveCatalogYAML(alias string) string {
	return `
catalog_id: archive-test-catalog
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-archive-test-1
    class: operational_pattern
    aliases: ["` + alias + `"]
    blocked_in: [public_oss]
    severity_override: high
`
}

func archiveConfigYAML(catalogPath string) string {
	return `
schema_version: "1.0.0"
repo:
  id: archive-fixture
  visibility: public_oss
catalogs:
  - catalog_id: archive-test-catalog
    source:
      kind: file
      path: ` + catalogPath + `
    optional: false
policy:
  default_severity: high
  block_threshold: high
  redaction_safe_output: true
  co_occurrence_enabled: false
`
}

func missingPrivateConfigYAML(posture string) string {
	return `
schema_version: "1.0.0"
repo:
  id: private-missing-fixture
  visibility: public_oss
catalogs:
  - catalog_id: limensafe-public-baseline-v0
    source:
      kind: builtin
      name: public-baseline
    optional: false
  - catalog_id: private-test
    source:
      kind: env
      var: LIMENSAFE_PRIVATE_TEST_CATALOG
    optional: true
policy:
  default_severity: high
  block_threshold: high
  private_catalog_missing: ` + posture + `
  redaction_safe_output: true
  co_occurrence_enabled: false
`
}

func privateOnlyMissingConfigYAML(posture string) string {
	return `
schema_version: "1.0.0"
repo:
  id: private-only-missing-fixture
  visibility: public_oss
catalogs:
  - catalog_id: private-test
    source:
      kind: env
      var: LIMENSAFE_PRIVATE_TEST_CATALOG
    optional: true
policy:
  default_severity: high
  block_threshold: high
  private_catalog_missing: ` + posture + `
  redaction_safe_output: true
  co_occurrence_enabled: false
`
}

func entityIDCatalogYAML() string {
	return `
catalog_id: entity-id-test
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-codename-1
    class: codename
    aliases: ["Horizon"]
    variants:
      case_insensitive: true
    blocked_in: [public_oss]
  - id: e-codename-2
    class: codename
    aliases: ["Tilden"]
    variants:
      case_insensitive: true
    blocked_in: [public_oss]
`
}

func aliasBearingEntityIDCatalogYAML() string {
	return `
catalog_id: unsafe-entity-id-test
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-acme-1
    class: client_identity
    aliases: ["Acme"]
    variants:
      case_insensitive: true
    blocked_in: [public_oss]
`
}

func crossCatalogUnsafeEntityIDCatalogAYAML() string {
	return `
catalog_id: cross-catalog-a
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-acme-1
    class: client_identity
    aliases: ["FooLeak"]
    blocked_in: [public_oss]
`
}

func crossCatalogUnsafeEntityIDCatalogBYAML() string {
	return `
catalog_id: cross-catalog-b
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-other-1
    class: client_identity
    aliases: ["acme"]
    variants:
      case_insensitive: true
    blocked_in: [public_oss]
`
}

func malformedUnsafeEntityIDCatalogYAML() string {
	return `
catalog_id: malformed-unsafe-entity-id-test
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-acme-1
    aliases: ["Acme"]
    variants:
      case_insensitive: true
    blocked_in: [public_oss]
`
}

func matchKeyCatalogYAML(protected string) string {
	return `
catalog_id: match-key-test
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-pattern-1
    class: operational_pattern
    match:
      regex: "` + protected + `"
    blocked_in: [public_oss]
`
}

func catalogIDMismatchLoadedCatalogYAML() string {
	return `
catalog_id: safe-declared-id
schema_version: "1.0.0"
default_severity: high
entities:
  - id: e-client-1
    class: client_identity
    aliases: ["acme"]
    variants:
      case_insensitive: true
    blocked_in: [public_oss]
`
}

func catalogIDMismatchConfigYAML(catalogPath string) string {
	return `
schema_version: "1.0.0"
repo:
  id: mismatch-fixture
  visibility: public_oss
catalogs:
  - catalog_id: expected-acme-id
    source:
      kind: file
      path: ` + catalogPath + `
    optional: false
policy:
  default_severity: high
  block_threshold: high
  redaction_safe_output: true
  co_occurrence_enabled: false
`
}

func requiredMissingCatalogAfterLoadedAliasConfigYAML(catalogPath string) string {
	return `
schema_version: "1.0.0"
repo:
  id: missing-required-fixture
  visibility: public_oss
catalogs:
  - catalog_id: safe-declared-id
    source:
      kind: file
      path: ` + catalogPath + `
    optional: false
  - catalog_id: expected-acme-id
    source:
      kind: file
      path: missing-private.catalog.yaml
    optional: false
policy:
  default_severity: high
  block_threshold: high
  redaction_safe_output: true
  co_occurrence_enabled: false
`
}

func mediumCatalogYAML() string {
	return `
catalog_id: medium-mode-test
schema_version: "1.0.0"
default_severity: medium
entities:
  - id: e-medium-mode-test
    class: operational_pattern
    aliases: ["MEDIUM_MODE_ALIAS"]
    blocked_in: [public_oss]
`
}

// io_ReadAll wraps io.ReadAll without adding an import alias above. Local
// shim so this file stays clean of unrelated imports.
func io_ReadAll(r interface {
	Read([]byte) (int, error)
}) ([]byte, error) {
	buf := make([]byte, 0, 8192)
	chunk := make([]byte, 4096)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, err
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

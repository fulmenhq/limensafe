package integration

import (
	"encoding/json"
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
	if !strings.Contains(gotStderr, "catalog warning: entity e-format-1") {
		t.Fatalf("expected catalog warning on stderr, got %q", gotStderr)
	}
	if strings.Contains(gotStderr, "ILT") {
		t.Fatalf("warning stderr leaked raw alias: %q", gotStderr)
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

type scanJSONPayload struct {
	ScanMetadata struct {
		StartedAt            string         `json:"started_at"`
		DurationMS           int64          `json:"duration_ms"`
		ScanRoot             string         `json:"scan_root"`
		ScanRootKind         string         `json:"scan_root_kind"`
		GitRef               string         `json:"git_ref"`
		FilesSkipped         int            `json:"files_skipped"`
		FilesSkippedByReason map[string]int `json:"files_skipped_by_reason"`
		DirsSkipped          int            `json:"directories_skipped"`
		WorkerCount          int            `json:"worker_count"`
		FilesScanned         int            `json:"files_scanned"`
		BytesScanned         int64          `json:"bytes_scanned"`
	} `json:"scan_metadata"`
	Summary struct {
		FindingsTotal int `json:"findings_total"`
	} `json:"summary"`
	Findings []struct {
		DetectorID string `json:"detector_id"`
		Severity   string `json:"severity"`
		Decision   string `json:"decision"`
		Location   struct {
			Path string `json:"path"`
		} `json:"location"`
	} `json:"findings"`
}

func runScanForJSON(t *testing.T, bin, dir string, args []string, wantExit int) scanJSONPayload {
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
	return payload
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

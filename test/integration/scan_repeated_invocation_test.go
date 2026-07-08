package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// internal-brief: git-archive / git-history scan reliability under repeated invocation.
//
// The locked scan CLI contract (exit codes 0/1/2/3 + stdout-JSON separation)
// must hold on EVERY invocation, not just in isolation. A dogfood run once saw
// a git-archive/git-history-all scan exit with a non-contract code and an empty
// stdout under rapid repeated invocation. Root cause was not a temp-extraction
// race (git-archive already extracts into a unique os.MkdirTemp per call and
// git-history reads blobs into memory) but SIGPIPE: when a downstream consumer
// of stdout/stderr closes its read end early — ubiquitous in CI pipelines
// (`| head`, `| jq`, `| grep`, a log collector that restarts) — the next write
// to fd 1/2 terminated the process with SIGPIPE (exit 141) and empty/truncated
// stdout, a code a leak gate cannot distinguish from "clean" or "blocked".
//
// These tests lock the fix: SIGPIPE is neutralized (Execute) and stderr
// diagnostics are best-effort, so a broken diagnostic pipe never fails an
// otherwise-successful scan, while a broken stdout consumer surfaces as a
// classified runtime error (exit 3) — never SIGPIPE.

// scanOutcome is the contract-relevant projection of one scan run: the exit
// code and the stable (volatile-metadata-normalized) result identity.
type scanOutcome struct {
	exit          int
	findingsTotal int
	filesScanned  int
}

// repeatedInvocationFixture builds a committed git repo with a few commits and a
// binary blob in history. The binary blob forces git-history-all to emit skip
// events to stderr on every run, exercising the diagnostic emit path that the
// SIGPIPE fix protects.
func repeatedInvocationFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"a.go":      "package x\n\nfunc A() string { return \"clean-a\" }\n",
		"docs/b.md": "# clean doc\n\nno protected vocabulary here.\n",
	}
	initCommittedGitRepo(t, root, files)
	// Add a binary blob (skipped by extension) plus a second commit so history
	// traversal has depth and at least one skip event per history scan.
	if err := os.WriteFile(filepath.Join(root, "asset.png"), bytes.Repeat([]byte{0x89, 0x50, 0x4e, 0x47}, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "add asset")
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package x\n\nfunc A() string { return \"clean-a2\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "edit a")
	return root
}

// runScanContract executes one scan and returns its contract projection,
// failing the test if the exit code is outside {0,1} or stdout is not a
// well-formed scan document. stderr is routed to a normal buffer here (the
// broken-pipe cases are covered by the dedicated tests below).
func runScanContract(t *testing.T, bin string, args ...string) scanOutcome {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("unexpected error type from scan: %T %v\nstderr=%s", err, err, stderr.String())
		}
		exit = exitErr.ExitCode()
	}
	if exit != 0 && exit != 1 {
		t.Fatalf("scan exit = %d, want 0 or 1 (args=%v)\nstdout_bytes=%d\nstderr=%s",
			exit, args, stdout.Len(), stderr.String())
	}
	var payload struct {
		ScanMetadata struct {
			OutputSchemaVersion string `json:"output_schema_version"`
			FilesScanned        int    `json:"files_scanned"`
		} `json:"scan_metadata"`
		Summary struct {
			FindingsTotal int `json:"findings_total"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not well-formed scan JSON (args=%v): %v\nstdout=%q", args, err, stdout.String())
	}
	if payload.ScanMetadata.OutputSchemaVersion == "" {
		t.Fatalf("stdout JSON missing output_schema_version (args=%v)\nstdout=%s", args, stdout.String())
	}
	return scanOutcome{
		exit:          exit,
		findingsTotal: payload.Summary.FindingsTotal,
		filesScanned:  payload.ScanMetadata.FilesScanned,
	}
}

// TestScanGitSurfacesDeterministicUnderRepetition drives one built binary
// through repeated subprocess invocations — sequential AND parallel — over both
// --git-archive and --git-history-all, asserting a contract-correct exit code
// and well-formed stdout JSON on every run, and a stable (normalized) result.
func TestScanGitSurfacesDeterministicUnderRepetition(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("repeated-invocation subprocess test is unix-focused")
	}
	bin := buildLimensafeBinary(t)
	repo := repeatedInvocationFixture(t)
	catalog := filepath.Join(t.TempDir(), "catalog.yaml")
	if err := os.WriteFile(catalog, []byte(archiveCatalogYAML("NEVER_APPEARS_ALIAS_LIM036")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	const iterations = 24 // >= 20 per the acceptance criteria, per surface
	surfaces := map[string][]string{
		"git-archive":     {"scan", repo, "--git-archive=HEAD", "--catalog", catalog, "--visibility", "public_oss"},
		"git-history-all": {"scan", repo, "--git-history-all", "--catalog", catalog, "--visibility", "public_oss"},
	}

	for name, args := range surfaces {
		name, args := name, args
		t.Run("sequential/"+name, func(t *testing.T) {
			var baseline *scanOutcome
			for i := 0; i < iterations; i++ {
				got := runScanContract(t, bin, args...)
				if baseline == nil {
					b := got
					baseline = &b
					continue
				}
				if got.findingsTotal != baseline.findingsTotal || got.filesScanned != baseline.filesScanned {
					t.Fatalf("run %d drifted: got %+v, baseline %+v", i, got, *baseline)
				}
			}
		})
	}

	for name, args := range surfaces {
		name, args := name, args
		t.Run("parallel/"+name, func(t *testing.T) {
			var wg sync.WaitGroup
			outcomes := make([]scanOutcome, iterations)
			wg.Add(iterations)
			for i := 0; i < iterations; i++ {
				go func(idx int) {
					defer wg.Done()
					outcomes[idx] = runScanContract(t, bin, args...)
				}(i)
			}
			wg.Wait()
			if t.Failed() {
				return
			}
			for i := 1; i < iterations; i++ {
				if outcomes[i].findingsTotal != outcomes[0].findingsTotal || outcomes[i].filesScanned != outcomes[0].filesScanned {
					t.Fatalf("parallel run %d drifted: got %+v, baseline %+v", i, outcomes[i], outcomes[0])
				}
			}
		})
	}
}

// TestScanGitHistoryStderrConsumerClosedEarly locks the internal-brief fix: when the
// stderr consumer closes its read end before the scan finishes writing its skip
// diagnostics, the scan MUST still complete with a contract exit code (0/1) and
// a well-formed stdout JSON document — not die from SIGPIPE (exit 141) with
// empty stdout. Diagnostics are advisory; the JSON result is the contract.
func TestScanGitHistoryStderrConsumerClosedEarly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("broken-pipe test is unix-focused")
	}
	bin := buildLimensafeBinary(t)
	repo := repeatedInvocationFixture(t)
	catalog := filepath.Join(t.TempDir(), "catalog.yaml")
	if err := os.WriteFile(catalog, []byte(archiveCatalogYAML("NEVER_APPEARS_ALIAS_LIM036")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	// Repeat: whether a stderr write lands before/after the reader closes is
	// timing-dependent, so exercise the window several times.
	for i := 0; i < 10; i++ {
		cmd := exec.Command(bin, "scan", repo, "--git-history-all", "--catalog", catalog, "--visibility", "public_oss")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout

		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		cmd.Stderr = pw
		if err := cmd.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
		// Parent drops its write-end copy; then close the read end so every
		// child stderr write hits a broken pipe (EPIPE).
		_ = pw.Close()
		_ = pr.Close()

		err = cmd.Wait()
		exit := 0
		if err != nil {
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("iter %d: unexpected error type: %T %v", i, err, err)
			}
			exit = exitErr.ExitCode()
		}
		if exit != 0 && exit != 1 {
			t.Fatalf("iter %d: exit = %d, want 0 or 1 (SIGPIPE/141 would mean the fix regressed)\nstdout_bytes=%d",
				i, exit, stdout.Len())
		}
		if !json.Valid(stdout.Bytes()) || stdout.Len() == 0 {
			t.Fatalf("iter %d: stdout not a complete JSON document (len=%d): %q", i, stdout.Len(), stdout.String())
		}
	}
}

// TestScanGitHistoryStdoutConsumerClosedEarly asserts that a broken STDOUT
// consumer never produces a SIGPIPE death (exit 141) or truncated stdout that
// still reads as success. With SIGPIPE neutralized, a write to a broken stdout
// pipe surfaces as a classified runtime error (exit 3); a small document that
// fits the pipe buffer may still complete (exit 0/1). Either way the code stays
// inside the locked 0/1/2/3 contract and is never a signal death.
func TestScanGitHistoryStdoutConsumerClosedEarly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("broken-pipe test is unix-focused")
	}
	bin := buildLimensafeBinary(t)
	repo := repeatedInvocationFixture(t)
	catalog := filepath.Join(t.TempDir(), "catalog.yaml")
	if err := os.WriteFile(catalog, []byte(archiveCatalogYAML("NEVER_APPEARS_ALIAS_LIM036")), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	for i := 0; i < 10; i++ {
		cmd := exec.Command(bin, "scan", repo, "--git-history-all", "--catalog", catalog, "--visibility", "public_oss")
		cmd.Stderr = nil

		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		cmd.Stdout = pw
		if err := cmd.Start(); err != nil {
			t.Fatalf("start: %v", err)
		}
		_ = pw.Close()
		_ = pr.Close() // reader gone: stdout writes may EPIPE

		err = cmd.Wait()
		exit := 0
		if err != nil {
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("iter %d: unexpected error type: %T %v", i, err, err)
			}
			exit = exitErr.ExitCode()
		}
		switch exit {
		case 0, 1, 3:
			// contract-correct: completed, blocked, or classified runtime error
		default:
			t.Fatalf("iter %d: exit = %d, want one of {0,1,3}; 137/141 would mean SIGKILL/SIGPIPE leaked", i, exit)
		}
	}
}

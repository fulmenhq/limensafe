package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/fulmenhq/limensafe/pkg/catalog"
	"github.com/fulmenhq/limensafe/pkg/engine"
	"github.com/fulmenhq/limensafe/pkg/extractor"
	"github.com/fulmenhq/limensafe/pkg/output"
)

// V0 scan subcommand. Wires together pkg/catalog, pkg/engine,
// pkg/extractor, and pkg/output for the synthetic corpus spike.

var (
	scanCatalogs       []string
	scanVisibility     string
	scanFormat         string
	scanMaxBytes       int64
	scanBranchName     bool
	scanCommitMsg      bool
	scanConfigFile     string
	scanWorkers        int
	scanStaged         bool
	scanIncludeIgnored bool
	scanGitArchiveRef  string
	scanDiff           bool
	scanDiffBase       string
	scanGitHistory     bool
	scanCommitMessages bool
	scanGitHistoryAll  bool
	scanMode           string
	scanPrivateMissing string
	scanExplain        bool
)

var scanCmd = &cobra.Command{
	Use:   "scan [path|-]",
	Short: "Scan a directory tree for Confidential Context Leakage",
	Long: `Scan a path for CCL leakage using the loaded vocabulary catalogs.

V0 spike: filesystem extraction, deterministic detection, and
redaction-safe JSON output.

Use --branch-name - or --commit-msg - to scan non-file git surfaces from stdin.
Use --git-archive <ref> to scan the tracked tree at a git ref without
including local ignored files or working-tree scratch.
Use --diff to scan only lines introduced by HEAD relative to --diff-base.
Use --git-history, --git-commit-messages, or --git-history-all for
pre-rewrite audits across committed history.
Use --mode local|ci|release to apply scan posture defaults; explicit flags
such as --private-catalog-missing win over the mode macro.

When a catalog declares an allowlist (internal-brief), matches it covers are
subtracted before findings are finalized; the counts ride
scan_metadata.allowlist_suppressions(_by_entry). Use --explain to print, on
stderr, which allowlist entry suppressed each match (redaction-safe).

Exit codes:
  0 — scan succeeded; no findings at or above block threshold
  1 — scan succeeded; one or more findings have decision=block
  2 — config / catalog validation error
  3 — runtime error (I/O, malformed input)`,
	Args:          validateScanArgs,
	RunE:          runScan,
	SilenceErrors: true, // ErrFindingsBlocked is a normal outcome; let main map it to exit 1
	SilenceUsage:  true, // findings-blocked is not a usage error
}

func init() {
	// Shared catalog/posture flags (--catalog, --config-file, --visibility,
	// --mode, --workers, --max-file-size, --private-catalog-missing) — see
	// flags.go / ADR-0007. scan is the canonical definition.
	registerScanCatalogFlags(scanCmd)

	// Surface-specific flags below are unique to scan.
	scanCmd.Flags().StringVar(&scanFormat, "format", "json",
		"Output format (json|human; v0 emits json regardless)")
	scanCmd.Flags().BoolVar(&scanBranchName, "branch-name", false,
		"Treat stdin as a branch name surface; path argument must be -")
	scanCmd.Flags().BoolVar(&scanCommitMsg, "commit-msg", false,
		"Treat stdin as a commit message surface; path argument must be -")
	scanCmd.Flags().BoolVar(&scanStaged, "staged", false,
		"Scan files in the git staging index (added or modified) — sound pre-commit gate")
	scanCmd.Flags().BoolVar(&scanIncludeIgnored, "include-ignored", false,
		"Scan files even when root .gitignore or .limensafeignore would skip them")
	scanCmd.Flags().StringVar(&scanGitArchiveRef, "git-archive", "",
		"Scan a git archive for ref (default when flag is bare: HEAD)")
	if flag := scanCmd.Flags().Lookup("git-archive"); flag != nil {
		flag.NoOptDefVal = "HEAD"
	}
	scanCmd.Flags().BoolVar(&scanDiff, "diff", false,
		"Scan only lines introduced by HEAD relative to --diff-base")
	scanCmd.Flags().StringVar(&scanDiffBase, "diff-base", "origin/main",
		"Base ref for --diff introduced-lines scans")
	scanCmd.Flags().BoolVar(&scanGitHistory, "git-history", false,
		"Scan unique historical blobs reachable from all refs")
	scanCmd.Flags().BoolVar(&scanCommitMessages, "git-commit-messages", false,
		"Scan commit messages reachable from all refs")
	scanCmd.Flags().BoolVar(&scanGitHistoryAll, "git-history-all", false,
		"Scan both historical blobs and commit messages reachable from all refs")
	scanCmd.Flags().BoolVar(&scanExplain, "explain", false,
		"Print, to stderr, which allowlist entry suppressed each subtracted match (redaction-safe: entry id, entity id, surface, location — never the pattern or matched text)")
	rootCmd.AddCommand(scanCmd)
}

func validateScanArgs(cmdObj *cobra.Command, args []string) error {
	stdinSurface := scanBranchName || scanCommitMsg
	gitArchive := cmdObj.Flags().Changed("git-archive")
	diffScan := scanDiff
	historyScan := scanGitHistory || scanCommitMessages || scanGitHistoryAll

	// Mutual exclusion: at most one of --staged, --git-archive,
	// --branch-name, --commit-msg.
	if scanStaged && stdinSurface {
		return fmt.Errorf("%w: --staged is mutually exclusive with --branch-name and --commit-msg", ErrConfigInvalid)
	}
	if gitArchive && scanStaged {
		return fmt.Errorf("%w: --git-archive is mutually exclusive with --staged", ErrConfigInvalid)
	}
	if gitArchive && stdinSurface {
		return fmt.Errorf("%w: --git-archive is mutually exclusive with --branch-name and --commit-msg", ErrConfigInvalid)
	}
	if diffScan && scanStaged {
		return fmt.Errorf("%w: --diff is mutually exclusive with --staged", ErrConfigInvalid)
	}
	if diffScan && gitArchive {
		return fmt.Errorf("%w: --diff is mutually exclusive with --git-archive", ErrConfigInvalid)
	}
	if diffScan && stdinSurface {
		return fmt.Errorf("%w: --diff is mutually exclusive with --branch-name and --commit-msg", ErrConfigInvalid)
	}
	if historyScan && scanStaged {
		return fmt.Errorf("%w: history scan flags are mutually exclusive with --staged", ErrConfigInvalid)
	}
	if historyScan && gitArchive {
		return fmt.Errorf("%w: history scan flags are mutually exclusive with --git-archive", ErrConfigInvalid)
	}
	if historyScan && diffScan {
		return fmt.Errorf("%w: history scan flags are mutually exclusive with --diff", ErrConfigInvalid)
	}
	if historyScan && stdinSurface {
		return fmt.Errorf("%w: history scan flags are mutually exclusive with --branch-name and --commit-msg", ErrConfigInvalid)
	}
	if scanBranchName && scanCommitMsg {
		return fmt.Errorf("%w: --branch-name and --commit-msg are mutually exclusive", ErrConfigInvalid)
	}
	if scanMode != "" && !validScanMode(scanMode) {
		return fmt.Errorf("%w: --mode must be one of local, ci, release", ErrConfigInvalid)
	}
	if scanPrivateMissing != "" && !catalog.ValidPrivateCatalogMissing(scanPrivateMissing) {
		return fmt.Errorf("%w: --private-catalog-missing must be one of silent, warn, error", ErrConfigInvalid)
	}
	if cmdObj.Flags().Changed("diff-base") && !diffScan {
		return fmt.Errorf("%w: --diff-base requires --diff", ErrConfigInvalid)
	}

	// History scans: repository root is cwd or optional path argument.
	if historyScan {
		if len(args) > 1 {
			return fmt.Errorf("%w: history scan accepts at most one path argument (the repo root)", ErrConfigInvalid)
		}
		if len(args) == 1 && args[0] == "-" {
			return fmt.Errorf("%w: history scan requires a git worktree path, not -", ErrConfigInvalid)
		}
		return nil
	}

	// --git-archive: ref arg is optional; defaults to HEAD. The git
	// repository root is the cwd, and output metadata uses the ref.
	if gitArchive {
		if len(args) > 1 {
			return fmt.Errorf("%w: --git-archive accepts at most one ref argument", ErrConfigInvalid)
		}
		if len(args) == 1 && args[0] == "-" {
			return fmt.Errorf("%w: --git-archive requires a git ref, not -", ErrConfigInvalid)
		}
		return nil
	}

	// --diff: base ref comes from --diff-base. The repository root is
	// the cwd or optional path argument.
	if diffScan {
		if len(args) > 1 {
			return fmt.Errorf("%w: --diff accepts at most one path argument (the repo root)", ErrConfigInvalid)
		}
		if len(args) == 1 && args[0] == "-" {
			return fmt.Errorf("%w: --diff requires a git worktree path, not -", ErrConfigInvalid)
		}
		return nil
	}

	// --staged: path arg is optional; defaults to cwd.
	if scanStaged {
		if len(args) > 1 {
			return fmt.Errorf("%w: --staged accepts at most one path argument (the repo root)", ErrConfigInvalid)
		}
		return nil
	}

	// stdin / filesystem: exactly one arg required (path or "-").
	if len(args) != 1 {
		return fmt.Errorf("%w: expected exactly one path argument or - for stdin surfaces", ErrConfigInvalid)
	}
	if stdinSurface && args[0] != "-" {
		return fmt.Errorf("%w: --branch-name/--commit-msg require path argument -", ErrConfigInvalid)
	}
	if !stdinSurface && args[0] == "-" {
		return fmt.Errorf("%w: stdin scan requires --branch-name or --commit-msg", ErrConfigInvalid)
	}
	return nil
}

func runScan(cmdObj *cobra.Command, args []string) error {
	scanRoot := ""
	if len(args) > 0 {
		scanRoot = args[0]
	}
	gitArchive := cmdObj.Flags().Changed("git-archive")
	diffScan := scanDiff
	historyScan := scanGitHistory || scanCommitMessages || scanGitHistoryAll
	if gitArchive {
		if len(args) > 0 {
			scanGitArchiveRef = args[0]
		}
		scanRoot = ""
	}
	if (scanStaged || gitArchive || diffScan || historyScan) && scanRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("%w: getwd: %w", ErrRuntime, err)
		}
		scanRoot = cwd
	}
	started := time.Now()

	cats, statuses, privateStatuses, configVisibility, blockThreshold, err := loadCatalogsAndStatuses(cmdObj)
	if err != nil {
		return err
	}
	if len(cats) == 0 && !canScanWithoutLoadedCatalogs(statuses) {
		return fmt.Errorf("%w: at least one catalog must be loaded (use --config-file or --catalog)", ErrConfigInvalid)
	}

	// Visibility precedence: --visibility flag (if non-default) > config.repo.visibility > default
	flagSet := cmdObj.Flags().Changed("visibility")
	if !flagSet && configVisibility != "" {
		scanVisibility = configVisibility
	}

	// Build redactor from merged catalog aliases.
	var redactor *output.Redactor
	if len(cats) > 0 {
		aliases := catalog.MergeAliases(cats)
		redactor, err = output.NewRedactor(aliases)
		if err != nil {
			return fmt.Errorf("%w: build redactor: %w", ErrConfigInvalid, err)
		}
	}
	if err := emitCatalogWarnings(cats, redactor); err != nil {
		return err
	}

	// Build detector engine from loaded catalogs.
	scanner, err := engine.NewScannerWithOptions(cats, scanVisibility, engine.ScannerOptions{
		BlockThreshold: blockThreshold,
	})
	if err != nil {
		return fmt.Errorf("%w: build engine: %w", ErrConfigInvalid, err)
	}

	ctx, cancel := context.WithCancel(cmdObj.Context())
	defer cancel()

	var filesScanned, filesSkipped int
	var dirsSkipped int
	var bytesScanned int64
	skippedByReason := map[string]int{}
	var findings []engine.Finding
	var suppressions []engine.Suppression
	workers := 1
	outputScanRoot := scanRoot
	scanRootKind := ""
	gitRef := ""
	var historyStats extractor.GitHistoryStats

	switch {
	case scanBranchName || scanCommitMsg:
		unit, err := stdinUnit(os.Stdin, scanBranchName)
		if err != nil {
			return err
		}
		filesScanned = 1
		bytesScanned = int64(len(unit.Content))
		res := scanner.ScanUnitResult(unit)
		findings = res.Findings
		suppressions = res.Suppressions
	case scanStaged:
		stScanned, stSkipped, stDirsSkipped, stSkippedByReason, stBytes, stFindings, stSuppressions, stWorkers, err := scanStagedIndex(ctx, scanRoot, scanMaxBytes, scanWorkers, scanner, redactor, scanIncludeIgnored)
		if err != nil {
			return err
		}
		filesScanned = stScanned
		filesSkipped = stSkipped
		dirsSkipped = stDirsSkipped
		skippedByReason = stSkippedByReason
		bytesScanned = stBytes
		findings = stFindings
		suppressions = stSuppressions
		workers = stWorkers
	case historyScan:
		outputScanRoot = scanRoot
		scanRootKind = "git-history"
		gitRef = "--all"
		includeBlobs := scanGitHistory || scanGitHistoryAll
		includeMessages := scanCommitMessages || scanGitHistoryAll
		hScanned, hSkipped, hDirsSkipped, hSkippedByReason, hBytes, hFindings, hSuppressions, hWorkers, hStats, err := scanGitHistorySurfaces(
			ctx, scanRoot, scanMaxBytes, scanWorkers, scanner, redactor, includeBlobs, includeMessages,
		)
		if err != nil {
			return err
		}
		filesScanned = hScanned
		filesSkipped = hSkipped
		dirsSkipped = hDirsSkipped
		skippedByReason = hSkippedByReason
		bytesScanned = hBytes
		findings = hFindings
		suppressions = hSuppressions
		workers = hWorkers
		historyStats = hStats
	case diffScan:
		outputScanRoot = scanRoot
		scanRootKind = "git-diff"
		gitRef = scanDiffBase + "...HEAD"
		dfScanned, dfSkipped, dfDirsSkipped, dfSkippedByReason, dfBytes, dfFindings, dfSuppressions, dfWorkers, err := scanGitDiff(ctx, scanRoot, scanDiffBase, scanner, redactor)
		if err != nil {
			return err
		}
		filesScanned = dfScanned
		filesSkipped = dfSkipped
		dirsSkipped = dfDirsSkipped
		skippedByReason = dfSkippedByReason
		bytesScanned = dfBytes
		findings = dfFindings
		suppressions = dfSuppressions
		workers = dfWorkers
	case gitArchive:
		if scanGitArchiveRef == "" {
			scanGitArchiveRef = "HEAD"
		}
		outputScanRoot = scanGitArchiveRef
		scanRootKind = "git-archive"
		gitRef = scanGitArchiveRef

		archiveCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		tmpdir, cleanup, err := extractor.GitArchiveToTemp(archiveCtx, scanRoot, scanGitArchiveRef, extractor.GitArchiveOptions{})
		if err != nil {
			return fmt.Errorf("%w: git archive: %s", ErrRuntime, redactor.Redact(err.Error()))
		}
		defer cleanup()

		fsScanned, fsSkipped, fsDirsSkipped, fsSkippedByReason, fsBytes, fsFindings, fsSuppressions, fsWorkers, err := scanFilesystem(archiveCtx, tmpdir, scanMaxBytes, scanWorkers, scanner, redactor, scanIncludeIgnored)
		if err != nil {
			return fmt.Errorf("%w: git archive scan: %w", ErrRuntime, err)
		}
		filesScanned = fsScanned
		filesSkipped = fsSkipped
		dirsSkipped = fsDirsSkipped
		skippedByReason = fsSkippedByReason
		bytesScanned = fsBytes
		findings = fsFindings
		suppressions = fsSuppressions
		workers = fsWorkers
	default:
		fsScanned, fsSkipped, fsDirsSkipped, fsSkippedByReason, fsBytes, fsFindings, fsSuppressions, fsWorkers, err := scanFilesystem(ctx, scanRoot, scanMaxBytes, scanWorkers, scanner, redactor, scanIncludeIgnored)
		if err != nil {
			return err
		}
		filesScanned = fsScanned
		filesSkipped = fsSkipped
		dirsSkipped = fsDirsSkipped
		skippedByReason = fsSkippedByReason
		bytesScanned = fsBytes
		findings = fsFindings
		suppressions = fsSuppressions
		workers = fsWorkers
	}

	// internal-brief: allowlist suppression accounting. emitExplainSuppressions writes
	// the per-suppression breakdown to stderr when --explain is set (before the
	// JSON document goes to stdout), and the totals ride scan_metadata so "0
	// findings" is always reconcilable against what the allowlist subtracted.
	if err := emitExplainSuppressions(suppressions, redactor); err != nil {
		return err
	}
	suppressionTotal, suppressionsByID := suppressionAccounting(suppressions)

	outFindings := append(toOutputFindings(findings), configWarningFindings(statuses)...)
	detectionOnly := filterDetectionFindings(outFindings)
	out := output.Output{
		Version: "v0",
		ScanMetadata: output.ScanMetadata{
			OutputSchemaVersion:       output.SchemaVersion,
			ToolVersion:               versionInfo.Version,
			StartedAt:                 started,
			DurationMS:                time.Since(started).Milliseconds(),
			ScanRoot:                  outputScanRoot,
			ScanRootKind:              scanRootKind,
			GitRef:                    gitRef,
			Visibility:                scanVisibility,
			WorkerCount:               workers,
			FilesScanned:              filesScanned,
			BytesScanned:              bytesScanned,
			FilesSkipped:              filesSkipped,
			DirsSkipped:               dirsSkipped,
			SkippedByReason:           skippedByReason,
			AllowlistSuppressions:     suppressionTotal,
			AllowlistSuppressionsByID: suppressionsByID,
			HistoryBlobsScanned:       historyStats.HistoryBlobsScanned,
			HistoryCommitsScanned:     historyStats.HistoryCommitsScanned,
			HistoryUniqueBlobs:        historyStats.HistoryUniqueBlobs,
			CatalogsLoaded:            statuses,
			PrivateCatalogsStatus:     privateStatuses,
		},
		Summary: output.ScanSummary{
			FindingsTotal: len(detectionOnly),
			BySeverity:    countBySeverity(detectionOnly),
			BySurface:     countBySurface(detectionOnly),
		},
		Findings: outFindings,
	}

	formatter := output.NewJSONFormatter(os.Stdout, redactor)
	if err := formatter.Emit(out); err != nil {
		return fmt.Errorf("%w: emit: %w", ErrRuntime, err)
	}

	// Exit-code policy per v0-spike-plan §2:
	//   0 = no findings at/above block threshold
	//   1 = findings at/above block threshold (the "fail the gate" case)
	// Any finding with decision=block triggers a non-zero exit. Output is
	// already flushed; CI / make sanitize-check / pre-commit hooks rely on
	// this signal to gate downstream actions.
	for _, f := range detectionOnly {
		if f.Decision == "block" {
			return ErrFindingsBlocked
		}
	}
	return nil
}

// Exit-code sentinel errors for scan-command dispatch in main.go.
//
// Limensafe's `scan` subcommand exposes a 4-way contract documented in
// `scan --help` and CONTRIBUTING.md, designed to be consumed by CI
// wrappers (Make, hooks, GitHub Actions):
//
//   0 — scan succeeded; no findings at or above block threshold
//   1 — scan succeeded; one or more findings have decision=block
//         (signaled via ErrFindingsBlocked)
//   2 — config / catalog validation error (signaled via ErrConfigInvalid)
//   3 — runtime error: I/O, extractor init failure, malformed input,
//         stdin read failure, output write failure (signaled via ErrRuntime)
//
// Any error returned from this package that wraps one of these sentinels
// is dispatched to the matching exit code by `cmd/limensafe/main.go`
// using `errors.Is`. Wrap with `fmt.Errorf("%w: …: %w", ErrConfigInvalid, err)`
// (Go 1.20+ multi-wrap) so both the sentinel and the underlying cause
// remain reachable via `errors.Is`/`errors.Unwrap`.

// ErrFindingsBlocked signals that the scan completed normally and emitted
// output, but at least one finding has decision=block. main.go maps this
// to exit code 1 without printing the "Command execution failed" prefix
// (since the JSON output already explains the situation).
var ErrFindingsBlocked = fmt.Errorf("limensafe: findings at or above block threshold")

// ErrConfigInvalid signals a configuration- or catalog-shaped failure:
// invalid flag combinations, missing required args, malformed config
// YAML, catalog loader errors, catalog-build failures. main.go maps to
// exit code 2.
var ErrConfigInvalid = fmt.Errorf("limensafe: config or catalog validation error")

// ErrRuntime signals a runtime- or I/O-shaped failure: filesystem
// extractor failures, git-index access failures, stdin read errors,
// stdout/stderr write failures, working-directory resolution failures.
// main.go maps to exit code 3.
var ErrRuntime = fmt.Errorf("limensafe: runtime or I/O error")

func stdinUnit(r io.Reader, branch bool) (extractor.InputUnit, error) {
	content, err := io.ReadAll(r)
	if err != nil {
		return extractor.InputUnit{}, fmt.Errorf("%w: read stdin: %w", ErrRuntime, err)
	}
	sourceKind := "commit_message"
	sourceID := "commit_message"
	if branch {
		sourceKind = "branch_name"
		sourceID = "branch_name"
	}
	return extractor.InputUnit{
		SourceID:     sourceID,
		SourceKind:   sourceKind,
		LocationHint: sourceID,
		Content:      content,
		Encoding:     "utf-8",
		Metadata: map[string]string{
			"source": "stdin",
		},
	}, nil
}

func scanFilesystem(ctx context.Context, scanRoot string, maxBytes int64, requestedWorkers int, scanner *engine.Scanner, redactor *output.Redactor, includeIgnored bool) (int, int, int, map[string]int, int64, []engine.Finding, []engine.Suppression, int, error) {
	ext, err := extractor.NewFilesystemExtractorWithOptions(scanRoot, maxBytes, extractor.FilesystemOptions{IncludeIgnored: includeIgnored})
	if err != nil {
		return 0, 0, 0, nil, 0, nil, nil, 0, fmt.Errorf("%w: init extractor: %w", ErrRuntime, err)
	}

	units := make(chan extractor.InputUnit, 64)
	skips := make(chan extractor.SkipEvent, 64)
	results := make(chan scanResult, 64)

	extErrCh := make(chan error, 1)
	go func() { extErrCh <- ext.Run(ctx, units, skips) }()

	workers := effectiveWorkers(requestedWorkers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for u := range units {
				sr := scanner.ScanUnitResult(u)
				res := scanResult{
					bytes:        int64(len(u.Content)),
					findings:     sr.Findings,
					suppressions: sr.Suppressions,
				}
				select {
				case results <- res:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var filesScanned, filesSkipped, dirsSkipped int
	skippedByReason := map[string]int{}
	var bytesScanned int64
	var findings []engine.Finding
	var suppressions []engine.Suppression

	for results != nil || skips != nil {
		select {
		case res, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			filesScanned++
			bytesScanned += res.bytes
			findings = append(findings, res.findings...)
			suppressions = append(suppressions, res.suppressions...)
		case s, ok := <-skips:
			if !ok {
				skips = nil
				continue
			}
			if err := emitSkipWarning(s, redactor); err != nil {
				return 0, 0, 0, nil, 0, nil, nil, workers, err
			}
			if s.IsDirectory {
				// internal-brief: directories_skipped is an additional structural
				// roll-up; files_skipped stays the stable total of file units
				// not scanned. A pruned directory folds the files it represents
				// into files_skipped and the reason breakdown so the same ignore
				// rule reconciles whether the walker pruned wholesale or matched
				// file-by-file. An empty ignored directory adds 0 (no reason key
				// inflation).
				dirsSkipped++
				if s.RepresentedFiles > 0 {
					filesSkipped += s.RepresentedFiles
					skippedByReason[s.Reason.String()] += s.RepresentedFiles
				}
				continue
			}
			filesSkipped++
			skippedByReason[s.Reason.String()]++
		case <-ctx.Done():
			return 0, 0, 0, nil, 0, nil, nil, workers, ctx.Err()
		}
	}

	if err := <-extErrCh; err != nil {
		return 0, 0, 0, nil, 0, nil, nil, workers, fmt.Errorf("%w: extractor: %w", ErrRuntime, err)
	}

	return filesScanned, filesSkipped, dirsSkipped, skippedByReason, bytesScanned, findings, suppressions, workers, nil
}

// scanStagedIndex is the staged-tree analog of scanFilesystem. It uses
// StagedExtractor to read content from the git index (not the working
// tree, not HEAD), so a pre-commit gate sees the exact bytes about to
// be committed even if the developer has subsequently edited the
// working copy. Concurrency model matches scanFilesystem's bounded
// worker pool over the unit channel.
func scanStagedIndex(ctx context.Context, scanRoot string, maxBytes int64, requestedWorkers int, scanner *engine.Scanner, redactor *output.Redactor, includeIgnored bool) (int, int, int, map[string]int, int64, []engine.Finding, []engine.Suppression, int, error) {
	ext, err := extractor.NewStagedExtractorWithOptions(scanRoot, maxBytes, extractor.StagedOptions{IncludeIgnored: includeIgnored})
	if err != nil {
		return 0, 0, 0, nil, 0, nil, nil, 0, fmt.Errorf("%w: init staged extractor: %w", ErrRuntime, err)
	}

	units := make(chan extractor.InputUnit, 64)
	skips := make(chan extractor.SkipEvent, 64)
	results := make(chan scanResult, 64)

	extErrCh := make(chan error, 1)
	go func() { extErrCh <- ext.Run(ctx, units, skips) }()

	workers := effectiveWorkers(requestedWorkers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for u := range units {
				sr := scanner.ScanUnitResult(u)
				res := scanResult{
					bytes:        int64(len(u.Content)),
					findings:     sr.Findings,
					suppressions: sr.Suppressions,
				}
				select {
				case results <- res:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var filesScanned, filesSkipped, dirsSkipped int
	skippedByReason := map[string]int{}
	var bytesScanned int64
	var findings []engine.Finding
	var suppressions []engine.Suppression

	for results != nil || skips != nil {
		select {
		case res, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			filesScanned++
			bytesScanned += res.bytes
			findings = append(findings, res.findings...)
			suppressions = append(suppressions, res.suppressions...)
		case s, ok := <-skips:
			if !ok {
				skips = nil
				continue
			}
			if err := emitSkipWarning(s, redactor); err != nil {
				return 0, 0, 0, nil, 0, nil, nil, workers, err
			}
			if s.IsDirectory {
				// internal-brief: directories_skipped is an additional structural
				// roll-up; files_skipped stays the stable total of file units
				// not scanned. A pruned directory folds the files it represents
				// into files_skipped and the reason breakdown so the same ignore
				// rule reconciles whether the walker pruned wholesale or matched
				// file-by-file. An empty ignored directory adds 0 (no reason key
				// inflation).
				dirsSkipped++
				if s.RepresentedFiles > 0 {
					filesSkipped += s.RepresentedFiles
					skippedByReason[s.Reason.String()] += s.RepresentedFiles
				}
				continue
			}
			filesSkipped++
			skippedByReason[s.Reason.String()]++
		case <-ctx.Done():
			return 0, 0, 0, nil, 0, nil, nil, workers, ctx.Err()
		}
	}

	if err := <-extErrCh; err != nil {
		return 0, 0, 0, nil, 0, nil, nil, workers, fmt.Errorf("%w: staged extractor: %w", ErrRuntime, err)
	}

	return filesScanned, filesSkipped, dirsSkipped, skippedByReason, bytesScanned, findings, suppressions, workers, nil
}

func scanGitDiff(ctx context.Context, scanRoot, baseRef string, scanner *engine.Scanner, redactor *output.Redactor) (int, int, int, map[string]int, int64, []engine.Finding, []engine.Suppression, int, error) {
	ext, err := extractor.NewGitDiffExtractor(scanRoot, baseRef)
	if err != nil {
		return 0, 0, 0, nil, 0, nil, nil, 0, fmt.Errorf("%w: init diff extractor: %w", ErrRuntime, err)
	}

	units := make(chan extractor.InputUnit, 64)
	skips := make(chan extractor.SkipEvent, 64)
	results := make(chan scanResult, 64)

	extErrCh := make(chan error, 1)
	go func() { extErrCh <- ext.Run(ctx, units, skips) }()

	workers := 1
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for u := range units {
				sr := scanner.ScanUnitResult(u)
				res := scanResult{
					sourceID:     u.SourceID,
					bytes:        int64(len(u.Content)),
					findings:     sr.Findings,
					suppressions: sr.Suppressions,
				}
				select {
				case results <- res:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var filesScanned, filesSkipped, dirsSkipped int
	skippedByReason := map[string]int{}
	var bytesScanned int64
	var findings []engine.Finding
	var suppressions []engine.Suppression
	scannedPaths := map[string]bool{}

	for results != nil || skips != nil {
		select {
		case res, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			if res.sourceID != "" {
				scannedPaths[res.sourceID] = true
			}
			bytesScanned += res.bytes
			findings = append(findings, res.findings...)
			suppressions = append(suppressions, res.suppressions...)
		case s, ok := <-skips:
			if !ok {
				skips = nil
				continue
			}
			if err := emitSkipWarning(s, redactor); err != nil {
				return 0, 0, 0, nil, 0, nil, nil, workers, err
			}
			if s.IsDirectory {
				// internal-brief: directories_skipped is an additional structural
				// roll-up; files_skipped stays the stable total of file units
				// not scanned. A pruned directory folds the files it represents
				// into files_skipped and the reason breakdown so the same ignore
				// rule reconciles whether the walker pruned wholesale or matched
				// file-by-file. An empty ignored directory adds 0 (no reason key
				// inflation).
				dirsSkipped++
				if s.RepresentedFiles > 0 {
					filesSkipped += s.RepresentedFiles
					skippedByReason[s.Reason.String()] += s.RepresentedFiles
				}
				continue
			}
			filesSkipped++
			skippedByReason[s.Reason.String()]++
		case <-ctx.Done():
			return 0, 0, 0, nil, 0, nil, nil, workers, ctx.Err()
		}
	}

	if err := <-extErrCh; err != nil {
		var diffErr *extractor.GitDiffError
		if errors.As(err, &diffErr) {
			detail := diffErr.Error()
			if redactor != nil {
				detail = redactor.Redact(detail)
			}
			return 0, 0, 0, nil, 0, nil, nil, workers, fmt.Errorf("%w: diff extractor: %s", ErrConfigInvalid, detail)
		}
		return 0, 0, 0, nil, 0, nil, nil, workers, fmt.Errorf("%w: diff extractor: %w", ErrRuntime, err)
	}

	filesScanned = len(scannedPaths)
	return filesScanned, filesSkipped, dirsSkipped, skippedByReason, bytesScanned, findings, suppressions, workers, nil
}

func scanGitHistorySurfaces(
	ctx context.Context,
	scanRoot string,
	maxBytes int64,
	requestedWorkers int,
	scanner *engine.Scanner,
	redactor *output.Redactor,
	includeBlobs bool,
	includeMessages bool,
) (int, int, int, map[string]int, int64, []engine.Finding, []engine.Suppression, int, extractor.GitHistoryStats, error) {
	ext, err := extractor.NewGitHistoryExtractor(scanRoot, extractor.GitHistoryOptions{
		MaxFileSize:           maxBytes,
		IncludeBlobs:          includeBlobs,
		IncludeCommitMessages: includeMessages,
	})
	if err != nil {
		detail := err.Error()
		if redactor != nil {
			detail = redactor.Redact(detail)
		}
		return 0, 0, 0, nil, 0, nil, nil, 0, extractor.GitHistoryStats{}, fmt.Errorf("%w: init history extractor: %s", ErrRuntime, detail)
	}

	if verbose {
		root := scanRoot
		if redactor != nil {
			root = redactor.Redact(root)
		}
		if _, err := fmt.Fprintf(os.Stderr, "history scan: root=%s blobs=%t commit_messages=%t\n", root, includeBlobs, includeMessages); err != nil {
			return 0, 0, 0, nil, 0, nil, nil, 0, extractor.GitHistoryStats{}, fmt.Errorf("%w: write history progress: %w", ErrRuntime, err)
		}
	}

	units := make(chan extractor.InputUnit, 64)
	skips := make(chan extractor.SkipEvent, 64)
	results := make(chan scanResult, 64)

	extErrCh := make(chan error, 1)
	go func() { extErrCh <- ext.Run(ctx, units, skips) }()

	workers := effectiveWorkers(requestedWorkers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for u := range units {
				sr := scanner.ScanUnitResult(u)
				res := scanResult{
					sourceID:     u.SourceID,
					sourceKind:   u.SourceKind,
					blobSHA:      u.Metadata["blob_sha"],
					bytes:        int64(len(u.Content)),
					findings:     sr.Findings,
					suppressions: sr.Suppressions,
					attributions: append([]extractor.GitBlobAttribution(nil), u.GitBlobAttributions...),
				}
				select {
				case results <- res:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var filesScanned, filesSkipped, dirsSkipped int
	skippedByReason := map[string]int{}
	var bytesScanned int64
	var findings []engine.Finding
	var suppressions []engine.Suppression

	for results != nil || skips != nil {
		select {
		case res, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			filesScanned++
			bytesScanned += res.bytes
			suppressions = append(suppressions, res.suppressions...)
			if res.sourceKind == extractor.SourceKindGitHistoryBlob {
				findings = append(findings, expandBlobFindings(res.findings, res.attributions)...)
				pathFindings, pathSuppressions := historyPathFindings(scanner, res.blobSHA, res.attributions)
				findings = append(findings, pathFindings...)
				suppressions = append(suppressions, pathSuppressions...)
				continue
			}
			findings = append(findings, res.findings...)
		case s, ok := <-skips:
			if !ok {
				skips = nil
				continue
			}
			if err := emitSkipWarning(s, redactor); err != nil {
				return 0, 0, 0, nil, 0, nil, nil, workers, extractor.GitHistoryStats{}, err
			}
			if s.IsDirectory {
				// internal-brief: directories_skipped is an additional structural
				// roll-up; files_skipped stays the stable total of file units
				// not scanned. A pruned directory folds the files it represents
				// into files_skipped and the reason breakdown so the same ignore
				// rule reconciles whether the walker pruned wholesale or matched
				// file-by-file. An empty ignored directory adds 0 (no reason key
				// inflation).
				dirsSkipped++
				if s.RepresentedFiles > 0 {
					filesSkipped += s.RepresentedFiles
					skippedByReason[s.Reason.String()] += s.RepresentedFiles
				}
				continue
			}
			filesSkipped++
			skippedByReason[s.Reason.String()]++
		case <-ctx.Done():
			return 0, 0, 0, nil, 0, nil, nil, workers, extractor.GitHistoryStats{}, ctx.Err()
		}
	}

	if err := <-extErrCh; err != nil {
		detail := err.Error()
		if redactor != nil {
			detail = redactor.Redact(detail)
		}
		return 0, 0, 0, nil, 0, nil, nil, workers, extractor.GitHistoryStats{}, fmt.Errorf("%w: history extractor: %s", ErrRuntime, detail)
	}

	stats := ext.Stats()
	if verbose {
		if _, err := fmt.Fprintf(
			os.Stderr,
			"history scan complete: unique_blobs=%d blobs_scanned=%d commits_scanned=%d\n",
			stats.HistoryUniqueBlobs,
			stats.HistoryBlobsScanned,
			stats.HistoryCommitsScanned,
		); err != nil {
			return 0, 0, 0, nil, 0, nil, nil, workers, extractor.GitHistoryStats{}, fmt.Errorf("%w: write history progress: %w", ErrRuntime, err)
		}
	}
	return filesScanned, filesSkipped, dirsSkipped, skippedByReason, bytesScanned, findings, suppressions, workers, stats, nil
}

func historyPathFindings(scanner *engine.Scanner, blobSHA string, attributions []extractor.GitBlobAttribution) ([]engine.Finding, []engine.Suppression) {
	var findings []engine.Finding
	var suppressions []engine.Suppression
	for _, attr := range attributions {
		unit := extractor.InputUnit{
			SourceID:     attr.Path,
			SourceKind:   extractor.SourceKindGitHistoryBlob,
			LocationHint: attr.Path,
			Content:      nil,
			Encoding:     "utf-8",
			Metadata: map[string]string{
				"blob_sha": blobSHA,
				"git_ref":  attr.Commit,
			},
		}
		res := scanner.ScanUnitResult(unit)
		findings = append(findings, res.Findings...)
		suppressions = append(suppressions, res.Suppressions...)
	}
	return findings, suppressions
}

func expandBlobFindings(findings []engine.Finding, attributions []extractor.GitBlobAttribution) []engine.Finding {
	if len(findings) == 0 || len(attributions) == 0 {
		return nil
	}
	out := make([]engine.Finding, 0, len(findings)*len(attributions))
	for _, f := range findings {
		for _, attr := range attributions {
			expanded := f
			expanded.Path = attr.Path
			expanded.SourceID = attr.Path
			expanded.GitRef = attr.Commit
			expanded.SurfaceKind = "blob"
			expanded = engine.RefreshFingerprint(expanded)
			out = append(out, expanded)
		}
	}
	return out
}

type scanResult struct {
	sourceID     string
	sourceKind   string
	blobSHA      string
	bytes        int64
	findings     []engine.Finding
	suppressions []engine.Suppression
	attributions []extractor.GitBlobAttribution
}

func effectiveWorkers(requested int) int {
	if requested > 0 {
		return requested
	}
	workers := runtime.NumCPU()
	if workers < 1 {
		return 1
	}
	return workers
}

// loadCatalogsAndStatuses resolves catalogs from --config-file (when
// set) and/or --catalog flags. Returns the loaded catalogs, the
// per-catalog load statuses for output, and the repo visibility
// declared in the config (empty if no config). The returned slices
// are aligned: statuses[i].CatalogID corresponds to cats with
// matching catalog_id.
//
// When --config-file is set, --catalog flags are additive: the config
// resolves first, then any extra --catalog paths are appended.
func loadCatalogsAndStatuses(cmdObj *cobra.Command) ([]*catalog.Catalog, []output.CatalogLoadStatus, []output.PrivateCatalogStatus, string, string, error) {
	var cats []*catalog.Catalog
	var statuses []output.CatalogLoadStatus
	var privateStatuses []output.PrivateCatalogStatus
	var visibility string
	blockThreshold := effectiveBlockThreshold(cmdObj, "")

	if scanConfigFile != "" {
		cfg, err := catalog.LoadConfigFile(scanConfigFile)
		if err != nil {
			return nil, nil, nil, "", "", fmt.Errorf("%w: load config %s: %w", ErrConfigInvalid, scanConfigFile, err)
		}
		visibility = cfg.Repo.Visibility
		privateMissing := effectivePrivateCatalogMissing(cmdObj, cfg.Policy.PrivateCatalogMissing)
		blockThreshold = effectiveBlockThreshold(cmdObj, cfg.Policy.BlockThreshold)

		resolutions, err := cfg.ResolveCatalogsWithOptions(scanConfigFile, catalog.ResolveOptions{
			PrivateCatalogMissing: privateMissing,
		})
		if err != nil {
			return nil, nil, nil, "", "", fmt.Errorf("%w: resolve catalogs: %w", ErrConfigInvalid, err)
		}
		for _, r := range resolutions {
			statuses = append(statuses, output.CatalogLoadStatus{
				CatalogID:  r.CatalogID,
				LoadStatus: r.LoadStatus,
				SourceKind: r.SourceKind,
				Reason:     r.MissingReason,
			})
			if r.Optional {
				privateStatuses = append(privateStatuses, output.PrivateCatalogStatus{
					CatalogID:  r.CatalogID,
					SourceKind: r.SourceKind,
					Status:     privateCatalogStatus(r.LoadStatus),
					Reason:     r.MissingReason,
				})
			}
			if r.Catalog != nil {
				cats = append(cats, r.Catalog)
			}
		}
	}

	for _, path := range scanCatalogs {
		c, err := catalog.LoadFile(path)
		if err != nil {
			return nil, nil, nil, "", "", fmt.Errorf("%w: load catalog %s: %w", ErrConfigInvalid, path, err)
		}
		cats = append(cats, c)
		statuses = append(statuses, output.CatalogLoadStatus{
			CatalogID:  c.CatalogID,
			LoadStatus: catalog.LoadStatusOK,
		})
	}
	if err := validateLoadedOutputIDSafety(cats, statuses, privateStatuses); err != nil {
		return nil, nil, nil, "", "", fmt.Errorf("%w: output id safety: %w", ErrConfigInvalid, err)
	}

	return cats, statuses, privateStatuses, visibility, blockThreshold, nil
}

func validateLoadedOutputIDSafety(cats []*catalog.Catalog, statuses []output.CatalogLoadStatus, privateStatuses []output.PrivateCatalogStatus) error {
	aliases := catalog.MergeAliases(cats)
	if len(aliases) == 0 {
		return nil
	}
	for i, c := range cats {
		if c == nil {
			continue
		}
		if outputIDContainsAlias(c.CatalogID, aliases) {
			return fmt.Errorf("catalog[%d]: catalog_id contains a protected alias substring", i)
		}
		for j, e := range c.Entities {
			if outputIDContainsAlias(e.ID, aliases) {
				return fmt.Errorf("catalog[%d].entity[%d]: id contains a protected alias substring", i, j)
			}
			if outputIDContainsAlias(e.ReplacementSuggestion, aliases) {
				return fmt.Errorf("catalog[%d].entity[%d]: replacement_suggestion contains a protected alias substring", i, j)
			}
		}
		for j, r := range c.CoOccurrenceRules {
			if outputIDContainsAlias(r.RuleID, aliases) {
				return fmt.Errorf("catalog[%d].co_occurrence_rule[%d]: rule_id contains a protected alias substring", i, j)
			}
		}
		for j, a := range c.Allowlist {
			// Allowlist ids are output-visible (suppression accounting, --explain),
			// so they pass the same cross-catalog alias-safety gate as entity and
			// rule ids. The single-catalog check lives in catalog.validateAllowlist.
			if outputIDContainsAlias(a.ID, aliases) {
				return fmt.Errorf("catalog[%d].allowlist[%d]: id contains a protected alias substring", i, j)
			}
		}
	}
	for i, status := range statuses {
		if outputIDContainsAlias(status.CatalogID, aliases) {
			return fmt.Errorf("catalog_status[%d]: catalog_id contains a protected alias substring", i)
		}
	}
	for i, status := range privateStatuses {
		if outputIDContainsAlias(status.CatalogID, aliases) {
			return fmt.Errorf("private_catalog_status[%d]: catalog_id contains a protected alias substring", i)
		}
	}
	return nil
}

func outputIDContainsAlias(value string, aliases []output.Alias) bool {
	if value == "" {
		return false
	}
	for _, alias := range aliases {
		if alias.Pattern == "" {
			continue
		}
		if alias.CaseInsensitive {
			if strings.Contains(strings.ToLower(value), strings.ToLower(alias.Pattern)) {
				return true
			}
			continue
		}
		if strings.Contains(value, alias.Pattern) {
			return true
		}
	}
	return false
}

func effectivePrivateCatalogMissing(cmdObj *cobra.Command, configured string) string {
	if cmdObj.Flags().Changed("private-catalog-missing") {
		return scanPrivateMissing
	}
	if cmdObj.Flags().Changed("mode") {
		switch scanMode {
		case "local":
			return catalog.PrivateCatalogMissingWarn
		case "ci", "release":
			return catalog.PrivateCatalogMissingError
		}
	}
	if configured != "" {
		return configured
	}
	return catalog.PrivateCatalogMissingSilent
}

func effectiveBlockThreshold(cmdObj *cobra.Command, configured string) string {
	if cmdObj.Flags().Changed("mode") {
		switch scanMode {
		case "release":
			return "medium"
		case "local", "ci":
			return "high"
		}
	}
	if configured != "" {
		return configured
	}
	return "high"
}

func validScanMode(value string) bool {
	switch value {
	case "local", "ci", "release":
		return true
	default:
		return false
	}
}

func privateCatalogStatus(loadStatus string) string {
	switch loadStatus {
	case catalog.LoadStatusOK:
		return "loaded"
	case catalog.LoadStatusMissingWarn:
		return "missing-warn"
	case catalog.LoadStatusMissingError:
		return "missing-error"
	default:
		return "missing-silent"
	}
}

func canScanWithoutLoadedCatalogs(statuses []output.CatalogLoadStatus) bool {
	if len(statuses) == 0 {
		return false
	}
	for _, status := range statuses {
		switch status.LoadStatus {
		case catalog.LoadStatusAbsentOptional, catalog.LoadStatusMissingWarn:
			continue
		default:
			return false
		}
	}
	return true
}

func configWarningFindings(statuses []output.CatalogLoadStatus) []output.Finding {
	var warnings []output.Finding
	for _, status := range statuses {
		if status.LoadStatus != catalog.LoadStatusMissingWarn {
			continue
		}
		warnings = append(warnings, output.Finding{
			Kind:          "config-warning",
			ID:            fmt.Sprintf("cw-%04d", len(warnings)+1),
			Fingerprint:   configWarningFingerprint(status),
			Severity:      "medium",
			Confidence:    "high",
			Decision:      "warn",
			EntityClass:   "configuration",
			DetectorID:    "private-catalog-missing",
			SourceKind:    "catalog_config",
			Surface:       "config",
			Location:      output.Location{SourceID: status.CatalogID},
			EvidenceShape: "missing_private_catalog",
			Message: fmt.Sprintf(
				"Private catalog missing: catalog_id=%s source_kind=%s reason=%s",
				status.CatalogID,
				status.SourceKind,
				status.Reason,
			),
		})
	}
	return warnings
}

func configWarningFingerprint(status output.CatalogLoadStatus) string {
	h := sha256.New()
	_, _ = h.Write([]byte("config-warning"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(status.CatalogID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(status.SourceKind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(status.Reason))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func emitCatalogWarnings(cats []*catalog.Catalog, redactor *output.Redactor) error {
	for _, c := range cats {
		if c == nil {
			continue
		}
		for _, warning := range c.Warnings {
			if _, err := fmt.Fprintf(os.Stderr, "catalog warning: %s\n", redactor.Redact(warning)); err != nil {
				return fmt.Errorf("%w: write catalog warning: %w", ErrRuntime, err)
			}
		}
	}
	return nil
}

func emitSkipWarning(skip extractor.SkipEvent, redactor *output.Redactor) error {
	source := skip.LocationHint
	if source == "" {
		source = skip.SourceID
	}
	detail := skip.Detail
	if detail == "" {
		detail = "n/a"
	}
	kind := "file"
	if skip.IsDirectory {
		kind = "directory"
	}
	if redactor != nil {
		source = redactor.Redact(source)
		detail = redactor.Redact(detail)
	}
	if skip.IsDirectory {
		// internal-brief: a pruned directory carries the count of files it represents
		// so operators can reconcile stderr with scan_metadata without walking
		// the tree. Count only — the descendant paths are never enumerated
		// (zero-leak: the pruned subtree may hold unscanned protected vocab).
		if _, err := fmt.Fprintf(os.Stderr, "scan skip: kind=%s reason=%s source=%s files=%d detail=%s\n", kind, skip.Reason.String(), source, skip.RepresentedFiles, detail); err != nil {
			return fmt.Errorf("%w: write skip warning: %w", ErrRuntime, err)
		}
		return nil
	}
	if _, err := fmt.Fprintf(os.Stderr, "scan skip: kind=%s reason=%s source=%s detail=%s\n", kind, skip.Reason.String(), source, detail); err != nil {
		return fmt.Errorf("%w: write skip warning: %w", ErrRuntime, err)
	}
	return nil
}

func toOutputFindings(findings []engine.Finding) []output.Finding {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].GitRef != findings[j].GitRef {
			return findings[i].GitRef < findings[j].GitRef
		}
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		if findings[i].SurfaceKind != findings[j].SurfaceKind {
			return findings[i].SurfaceKind < findings[j].SurfaceKind
		}
		if findings[i].Surface != findings[j].Surface {
			return findings[i].Surface < findings[j].Surface
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		if findings[i].Column != findings[j].Column {
			return findings[i].Column < findings[j].Column
		}
		if findings[i].DetectorID != findings[j].DetectorID {
			return findings[i].DetectorID < findings[j].DetectorID
		}
		return findings[i].Fingerprint < findings[j].Fingerprint
	})
	out := make([]output.Finding, 0, len(findings))
	for i, f := range findings {
		out = append(out, output.Finding{
			Kind:        "detection",
			ID:          fmt.Sprintf("f-%04d", i+1),
			Fingerprint: f.Fingerprint,
			Severity:    f.Severity,
			Confidence:  f.Confidence,
			Decision:    f.Decision,
			EntityID:    f.EntityID,
			EntityClass: f.EntityClass,
			DetectorID:  f.DetectorID,
			RuleID:      f.RuleID,
			SourceKind:  f.SourceKind,
			Surface:     f.Surface,
			Location: output.Location{
				Path:        f.Path,
				SourceID:    f.SourceID,
				Line:        f.Line,
				Column:      f.Column,
				GitRef:      f.GitRef,
				SurfaceKind: f.SurfaceKind,
			},
			ReplacementID: f.ReplacementID,
			EvidenceShape: f.EvidenceShape,
			Message:       f.Message,
		})
	}
	return out
}

// suppressionAccounting reduces the per-unit allowlist suppressions to the
// reconcilable totals that ride scan_metadata (internal-brief): the overall count and
// the by-entry breakdown keyed by the alias-safe allowlist entry id. The map is
// always non-nil so it serializes as {} (not null) on a clean scan, matching
// the present-with-empty contract used for files_skipped_by_reason.
func suppressionAccounting(suppressions []engine.Suppression) (int, map[string]int) {
	byID := map[string]int{}
	for _, s := range suppressions {
		byID[s.AllowlistID]++
	}
	return len(suppressions), byID
}

// emitExplainSuppressions writes one redaction-safe line per allowlist
// suppression to stderr when --explain is set. It emits only alias-safe ids and
// the redacted location — never the allowlist pattern or matched text — so the
// audit trail upholds the zero-leak boundary (ADR-0003). It is deterministic:
// suppressions are sorted before emission.
func emitExplainSuppressions(suppressions []engine.Suppression, redactor *output.Redactor) error {
	if !scanExplain || len(suppressions) == 0 {
		return nil
	}
	sorted := append([]engine.Suppression(nil), suppressions...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].AllowlistID != sorted[j].AllowlistID {
			return sorted[i].AllowlistID < sorted[j].AllowlistID
		}
		if sorted[i].EntityID != sorted[j].EntityID {
			return sorted[i].EntityID < sorted[j].EntityID
		}
		if sorted[i].Surface != sorted[j].Surface {
			return sorted[i].Surface < sorted[j].Surface
		}
		return sorted[i].SourceID < sorted[j].SourceID
	})
	for _, s := range sorted {
		source := s.SourceID
		gitRef := s.GitRef
		if redactor != nil {
			source = redactor.Redact(source)
			gitRef = redactor.Redact(gitRef)
		}
		if source == "" {
			source = s.Surface
		}
		line := fmt.Sprintf("allowlist suppression: allowlist_id=%s entity_id=%s surface=%s source=%s",
			s.AllowlistID, s.EntityID, s.Surface, source)
		if gitRef != "" {
			line += " git_ref=" + gitRef
		}
		if _, err := fmt.Fprintln(os.Stderr, line); err != nil {
			return fmt.Errorf("%w: write allowlist explain: %w", ErrRuntime, err)
		}
	}
	return nil
}

func filterDetectionFindings(findings []output.Finding) []output.Finding {
	out := make([]output.Finding, 0, len(findings))
	for _, finding := range findings {
		if finding.Kind == "" || finding.Kind == "detection" {
			out = append(out, finding)
		}
	}
	return out
}

func countBySeverity(findings []output.Finding) map[string]int {
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	return counts
}

func countBySurface(findings []output.Finding) map[string]int {
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Surface]++
	}
	return counts
}

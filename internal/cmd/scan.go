package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"sort"
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
	scanCmd.Flags().StringSliceVar(&scanCatalogs, "catalog", nil,
		"Path to a catalog YAML file (repeatable for layered catalogs)")
	scanCmd.Flags().StringVar(&scanVisibility, "visibility", "public_oss",
		"Repo visibility scope (public_oss, unlisted_oss, internal, engagement_private, local_only)")
	scanCmd.Flags().StringVar(&scanFormat, "format", "json",
		"Output format (json|human; v0 emits json regardless)")
	scanCmd.Flags().Int64Var(&scanMaxBytes, "max-file-size", extractor.DefaultMaxFileSize,
		"Per-file size cap; files exceeding this emit a skip event")
	scanCmd.Flags().BoolVar(&scanBranchName, "branch-name", false,
		"Treat stdin as a branch name surface; path argument must be -")
	scanCmd.Flags().BoolVar(&scanCommitMsg, "commit-msg", false,
		"Treat stdin as a commit message surface; path argument must be -")
	scanCmd.Flags().StringVar(&scanConfigFile, "config-file", "",
		"Path to .limensafe/config.yaml; resolves catalogs by reference and supplies repo visibility")
	scanCmd.Flags().IntVar(&scanWorkers, "workers", 0,
		"Number of worker goroutines for filesystem scans (default: runtime.NumCPU())")
	scanCmd.Flags().BoolVar(&scanStaged, "staged", false,
		"Scan files in the git staging index (added or modified) — sound pre-commit gate")
	scanCmd.Flags().BoolVar(&scanIncludeIgnored, "include-ignored", false,
		"Scan files even when root .gitignore or .limensafeignore would skip them")
	scanCmd.Flags().StringVar(&scanGitArchiveRef, "git-archive", "",
		"Scan a git archive for ref (default when flag is bare: HEAD)")
	if flag := scanCmd.Flags().Lookup("git-archive"); flag != nil {
		flag.NoOptDefVal = "HEAD"
	}
	rootCmd.AddCommand(scanCmd)
}

func validateScanArgs(cmdObj *cobra.Command, args []string) error {
	stdinSurface := scanBranchName || scanCommitMsg
	gitArchive := cmdObj.Flags().Changed("git-archive")

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
	if scanBranchName && scanCommitMsg {
		return fmt.Errorf("%w: --branch-name and --commit-msg are mutually exclusive", ErrConfigInvalid)
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
	if gitArchive {
		if len(args) > 0 {
			scanGitArchiveRef = args[0]
		}
		scanRoot = ""
	}
	if (scanStaged || gitArchive) && scanRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("%w: getwd: %w", ErrRuntime, err)
		}
		scanRoot = cwd
	}
	started := time.Now()

	cats, statuses, configVisibility, err := loadCatalogsAndStatuses()
	if err != nil {
		return err
	}
	if len(cats) == 0 {
		return fmt.Errorf("%w: at least one catalog must be loaded (use --config-file or --catalog)", ErrConfigInvalid)
	}

	// Visibility precedence: --visibility flag (if non-default) > config.repo.visibility > default
	flagSet := cmdObj.Flags().Changed("visibility")
	if !flagSet && configVisibility != "" {
		scanVisibility = configVisibility
	}

	// Build redactor from merged catalog aliases.
	aliases := catalog.MergeAliases(cats)
	redactor, err := output.NewRedactor(aliases)
	if err != nil {
		return fmt.Errorf("%w: build redactor: %w", ErrConfigInvalid, err)
	}
	if err := emitCatalogWarnings(cats, redactor); err != nil {
		return err
	}

	// Build detector engine from loaded catalogs.
	scanner, err := engine.NewScanner(cats, scanVisibility)
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
	workers := 1
	outputScanRoot := scanRoot
	scanRootKind := ""
	gitRef := ""

	switch {
	case scanBranchName || scanCommitMsg:
		unit, err := stdinUnit(os.Stdin, scanBranchName)
		if err != nil {
			return err
		}
		filesScanned = 1
		bytesScanned = int64(len(unit.Content))
		findings = scanner.ScanUnit(unit)
	case scanStaged:
		stScanned, stSkipped, stDirsSkipped, stSkippedByReason, stBytes, stFindings, stWorkers, err := scanStagedIndex(ctx, scanRoot, scanMaxBytes, scanWorkers, scanner, redactor, scanIncludeIgnored)
		if err != nil {
			return err
		}
		filesScanned = stScanned
		filesSkipped = stSkipped
		dirsSkipped = stDirsSkipped
		skippedByReason = stSkippedByReason
		bytesScanned = stBytes
		findings = stFindings
		workers = stWorkers
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

		fsScanned, fsSkipped, fsDirsSkipped, fsSkippedByReason, fsBytes, fsFindings, fsWorkers, err := scanFilesystem(archiveCtx, tmpdir, scanMaxBytes, scanWorkers, scanner, redactor, scanIncludeIgnored)
		if err != nil {
			return fmt.Errorf("%w: git archive scan: %w", ErrRuntime, err)
		}
		filesScanned = fsScanned
		filesSkipped = fsSkipped
		dirsSkipped = fsDirsSkipped
		skippedByReason = fsSkippedByReason
		bytesScanned = fsBytes
		findings = fsFindings
		workers = fsWorkers
	default:
		fsScanned, fsSkipped, fsDirsSkipped, fsSkippedByReason, fsBytes, fsFindings, fsWorkers, err := scanFilesystem(ctx, scanRoot, scanMaxBytes, scanWorkers, scanner, redactor, scanIncludeIgnored)
		if err != nil {
			return err
		}
		filesScanned = fsScanned
		filesSkipped = fsSkipped
		dirsSkipped = fsDirsSkipped
		skippedByReason = fsSkippedByReason
		bytesScanned = fsBytes
		findings = fsFindings
		workers = fsWorkers
	}

	outFindings := toOutputFindings(findings)
	out := output.Output{
		Version: "v0",
		ScanMetadata: output.ScanMetadata{
			ToolVersion:     versionInfo.Version,
			StartedAt:       started,
			DurationMS:      time.Since(started).Milliseconds(),
			ScanRoot:        outputScanRoot,
			ScanRootKind:    scanRootKind,
			GitRef:          gitRef,
			Visibility:      scanVisibility,
			WorkerCount:     workers,
			FilesScanned:    filesScanned,
			BytesScanned:    bytesScanned,
			FilesSkipped:    filesSkipped,
			DirsSkipped:     dirsSkipped,
			SkippedByReason: skippedByReason,
			CatalogsLoaded:  statuses,
		},
		Summary: output.ScanSummary{
			FindingsTotal: len(outFindings),
			BySeverity:    countBySeverity(outFindings),
			BySurface:     countBySurface(outFindings),
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
	for _, f := range outFindings {
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

func scanFilesystem(ctx context.Context, scanRoot string, maxBytes int64, requestedWorkers int, scanner *engine.Scanner, redactor *output.Redactor, includeIgnored bool) (int, int, int, map[string]int, int64, []engine.Finding, int, error) {
	ext, err := extractor.NewFilesystemExtractorWithOptions(scanRoot, maxBytes, extractor.FilesystemOptions{IncludeIgnored: includeIgnored})
	if err != nil {
		return 0, 0, 0, nil, 0, nil, 0, fmt.Errorf("%w: init extractor: %w", ErrRuntime, err)
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
				res := scanResult{
					bytes:    int64(len(u.Content)),
					findings: scanner.ScanUnit(u),
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
		case s, ok := <-skips:
			if !ok {
				skips = nil
				continue
			}
			if err := emitSkipWarning(s, redactor); err != nil {
				return 0, 0, 0, nil, 0, nil, workers, err
			}
			if s.IsDirectory {
				dirsSkipped++
				continue
			}
			filesSkipped++
			skippedByReason[s.Reason.String()]++
		case <-ctx.Done():
			return 0, 0, 0, nil, 0, nil, workers, ctx.Err()
		}
	}

	if err := <-extErrCh; err != nil {
		return 0, 0, 0, nil, 0, nil, workers, fmt.Errorf("%w: extractor: %w", ErrRuntime, err)
	}

	return filesScanned, filesSkipped, dirsSkipped, skippedByReason, bytesScanned, findings, workers, nil
}

// scanStagedIndex is the staged-tree analog of scanFilesystem. It uses
// StagedExtractor to read content from the git index (not the working
// tree, not HEAD), so a pre-commit gate sees the exact bytes about to
// be committed even if the developer has subsequently edited the
// working copy. Concurrency model matches scanFilesystem's bounded
// worker pool over the unit channel.
func scanStagedIndex(ctx context.Context, scanRoot string, maxBytes int64, requestedWorkers int, scanner *engine.Scanner, redactor *output.Redactor, includeIgnored bool) (int, int, int, map[string]int, int64, []engine.Finding, int, error) {
	ext, err := extractor.NewStagedExtractorWithOptions(scanRoot, maxBytes, extractor.StagedOptions{IncludeIgnored: includeIgnored})
	if err != nil {
		return 0, 0, 0, nil, 0, nil, 0, fmt.Errorf("%w: init staged extractor: %w", ErrRuntime, err)
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
				res := scanResult{
					bytes:    int64(len(u.Content)),
					findings: scanner.ScanUnit(u),
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
		case s, ok := <-skips:
			if !ok {
				skips = nil
				continue
			}
			if err := emitSkipWarning(s, redactor); err != nil {
				return 0, 0, 0, nil, 0, nil, workers, err
			}
			if s.IsDirectory {
				dirsSkipped++
				continue
			}
			filesSkipped++
			skippedByReason[s.Reason.String()]++
		case <-ctx.Done():
			return 0, 0, 0, nil, 0, nil, workers, ctx.Err()
		}
	}

	if err := <-extErrCh; err != nil {
		return 0, 0, 0, nil, 0, nil, workers, fmt.Errorf("%w: staged extractor: %w", ErrRuntime, err)
	}

	return filesScanned, filesSkipped, dirsSkipped, skippedByReason, bytesScanned, findings, workers, nil
}

type scanResult struct {
	bytes    int64
	findings []engine.Finding
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
func loadCatalogsAndStatuses() ([]*catalog.Catalog, []output.CatalogLoadStatus, string, error) {
	var cats []*catalog.Catalog
	var statuses []output.CatalogLoadStatus
	var visibility string

	if scanConfigFile != "" {
		cfg, err := catalog.LoadConfigFile(scanConfigFile)
		if err != nil {
			return nil, nil, "", fmt.Errorf("%w: load config %s: %w", ErrConfigInvalid, scanConfigFile, err)
		}
		visibility = cfg.Repo.Visibility

		resolutions, err := cfg.ResolveCatalogs(scanConfigFile)
		if err != nil {
			return nil, nil, "", fmt.Errorf("%w: resolve catalogs: %w", ErrConfigInvalid, err)
		}
		for _, r := range resolutions {
			statuses = append(statuses, output.CatalogLoadStatus{
				CatalogID:  r.CatalogID,
				LoadStatus: r.LoadStatus,
			})
			if r.Catalog != nil {
				cats = append(cats, r.Catalog)
			}
		}
	}

	for _, path := range scanCatalogs {
		c, err := catalog.LoadFile(path)
		if err != nil {
			return nil, nil, "", fmt.Errorf("%w: load catalog %s: %w", ErrConfigInvalid, path, err)
		}
		cats = append(cats, c)
		statuses = append(statuses, output.CatalogLoadStatus{
			CatalogID:  c.CatalogID,
			LoadStatus: "ok",
		})
	}

	return cats, statuses, visibility, nil
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
	if _, err := fmt.Fprintf(os.Stderr, "scan skip: kind=%s reason=%s source=%s detail=%s\n", kind, skip.Reason.String(), source, detail); err != nil {
		return fmt.Errorf("%w: write skip warning: %w", ErrRuntime, err)
	}
	return nil
}

func toOutputFindings(findings []engine.Finding) []output.Finding {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
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
			ID:          fmt.Sprintf("f-%04d", i+1),
			Fingerprint: f.Fingerprint,
			Severity:    f.Severity,
			Confidence:  f.Confidence,
			Decision:    f.Decision,
			EntityClass: f.EntityClass,
			DetectorID:  f.DetectorID,
			RuleID:      f.RuleID,
			SourceKind:  f.SourceKind,
			Surface:     f.Surface,
			Location: output.Location{
				Path:     f.Path,
				SourceID: f.SourceID,
				Line:     f.Line,
				Column:   f.Column,
			},
			ReplacementID: f.ReplacementID,
			EvidenceShape: f.EvidenceShape,
			Message:       f.Message,
		})
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

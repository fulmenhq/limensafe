package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"sync"
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
	scanCatalogs   []string
	scanVisibility string
	scanFormat     string
	scanMaxBytes   int64
	scanBranchName bool
	scanCommitMsg  bool
	scanConfigFile string
	scanWorkers    int
)

var scanCmd = &cobra.Command{
	Use:   "scan <path|-",
	Short: "Scan a directory tree for Confidential Context Leakage",
	Long: `Scan a path for CCL leakage using the loaded vocabulary catalogs.

V0 spike: filesystem extraction, deterministic detection, and
redaction-safe JSON output.

Use --branch-name - or --commit-msg - to scan non-file git surfaces from stdin.`,
	Args: validateScanArgs,
	RunE: runScan,
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
	rootCmd.AddCommand(scanCmd)
}

func validateScanArgs(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("expected exactly one path argument or - for stdin surfaces")
	}
	if scanBranchName && scanCommitMsg {
		return fmt.Errorf("--branch-name and --commit-msg are mutually exclusive")
	}
	if (scanBranchName || scanCommitMsg) && args[0] != "-" {
		return fmt.Errorf("--branch-name/--commit-msg require path argument -")
	}
	if !(scanBranchName || scanCommitMsg) && args[0] == "-" {
		return fmt.Errorf("stdin scan requires --branch-name or --commit-msg")
	}
	return nil
}

func runScan(cmdObj *cobra.Command, args []string) error {
	scanRoot := args[0]
	started := time.Now()

	cats, statuses, configVisibility, err := loadCatalogsAndStatuses()
	if err != nil {
		return err
	}
	if len(cats) == 0 {
		return fmt.Errorf("at least one catalog must be loaded (use --config-file or --catalog)")
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
		return fmt.Errorf("build redactor: %w", err)
	}

	// Build detector engine from loaded catalogs.
	scanner, err := engine.NewScanner(cats, scanVisibility)
	if err != nil {
		return fmt.Errorf("build engine: %w", err)
	}

	ctx, cancel := context.WithCancel(cmdObj.Context())
	defer cancel()

	var filesScanned, filesSkipped int
	var bytesScanned int64
	var findings []engine.Finding
	workers := 1

	if scanBranchName || scanCommitMsg {
		unit, err := stdinUnit(os.Stdin, scanBranchName)
		if err != nil {
			return err
		}
		filesScanned = 1
		bytesScanned = int64(len(unit.Content))
		findings = scanner.ScanUnit(unit)
	} else {
		fsScanned, fsSkipped, fsBytes, fsFindings, fsWorkers, err := scanFilesystem(ctx, scanRoot, scanMaxBytes, scanWorkers, scanner)
		if err != nil {
			return err
		}
		filesScanned = fsScanned
		filesSkipped = fsSkipped
		bytesScanned = fsBytes
		findings = fsFindings
		workers = fsWorkers
	}

	outFindings := toOutputFindings(findings)
	out := output.Output{
		Version: "v0",
		ScanMetadata: output.ScanMetadata{
			ToolVersion:    versionInfo.Version,
			StartedAt:      started,
			DurationMS:     time.Since(started).Milliseconds(),
			ScanRoot:       scanRoot,
			Visibility:     scanVisibility,
			WorkerCount:    workers,
			FilesScanned:   filesScanned,
			BytesScanned:   bytesScanned,
			FilesSkipped:   filesSkipped,
			CatalogsLoaded: statuses,
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
		return fmt.Errorf("emit: %w", err)
	}

	return nil
}

func stdinUnit(r io.Reader, branch bool) (extractor.InputUnit, error) {
	content, err := io.ReadAll(r)
	if err != nil {
		return extractor.InputUnit{}, fmt.Errorf("read stdin: %w", err)
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

func scanFilesystem(ctx context.Context, scanRoot string, maxBytes int64, requestedWorkers int, scanner *engine.Scanner) (int, int, int64, []engine.Finding, int, error) {
	ext, err := extractor.NewFilesystemExtractor(scanRoot, maxBytes)
	if err != nil {
		return 0, 0, 0, nil, 0, fmt.Errorf("init extractor: %w", err)
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

	var filesScanned, filesSkipped int
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
			filesSkipped++
			_ = s
		case <-ctx.Done():
			return 0, 0, 0, nil, workers, ctx.Err()
		}
	}

	if err := <-extErrCh; err != nil {
		return 0, 0, 0, nil, workers, fmt.Errorf("extractor: %w", err)
	}

	return filesScanned, filesSkipped, bytesScanned, findings, workers, nil
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
			return nil, nil, "", fmt.Errorf("load config %s: %w", scanConfigFile, err)
		}
		visibility = cfg.Repo.Visibility

		resolutions, err := cfg.ResolveCatalogs(scanConfigFile)
		if err != nil {
			return nil, nil, "", fmt.Errorf("resolve catalogs: %w", err)
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
			return nil, nil, "", fmt.Errorf("load catalog %s: %w", path, err)
		}
		cats = append(cats, c)
		statuses = append(statuses, output.CatalogLoadStatus{
			CatalogID:  c.CatalogID,
			LoadStatus: "ok",
		})
	}

	return cats, statuses, visibility, nil
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

package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/fulmenhq/limensafe/pkg/catalog"
	"github.com/fulmenhq/limensafe/pkg/extractor"
	"github.com/fulmenhq/limensafe/pkg/output"
)

// V0 scan subcommand. Wires together pkg/catalog, pkg/extractor, and
// pkg/output. The detection step is a placeholder until entarch's
// pkg/engine commits — the scan currently produces empty findings
// while still exercising the full I/O path (catalog load, redactor
// build, filesystem walk, JSON emit). This is sufficient to validate
// the zero-leak invariant on the redactor + formatter pipeline.

var (
	scanCatalogs   []string
	scanVisibility string
	scanFormat     string
	scanMaxBytes   int64
)

var scanCmd = &cobra.Command{
	Use:   "scan <path>",
	Short: "Scan a directory tree for Confidential Context Leakage",
	Long: `Scan a path for CCL leakage using the loaded vocabulary catalogs.

V0 spike: filesystem extraction + redaction-safe JSON output.
The detection layer (pkg/engine) is implemented separately; without it,
scan runs end-to-end but emits empty findings.`,
	Args: cobra.ExactArgs(1),
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
	rootCmd.AddCommand(scanCmd)
}

func runScan(cmdObj *cobra.Command, args []string) error {
	scanRoot := args[0]
	started := time.Now()

	if len(scanCatalogs) == 0 {
		return fmt.Errorf("at least one --catalog is required (v0; repo --config support pending)")
	}

	// Load catalogs.
	cats := make([]*catalog.Catalog, 0, len(scanCatalogs))
	statuses := make([]output.CatalogLoadStatus, 0, len(scanCatalogs))
	for _, path := range scanCatalogs {
		c, err := catalog.LoadFile(path)
		if err != nil {
			return fmt.Errorf("load catalog %s: %w", path, err)
		}
		cats = append(cats, c)
		statuses = append(statuses, output.CatalogLoadStatus{
			CatalogID:  c.CatalogID,
			LoadStatus: "ok",
		})
	}

	// Build redactor from merged catalog aliases.
	aliases := catalog.MergeAliases(cats)
	redactor, err := output.NewRedactor(aliases)
	if err != nil {
		return fmt.Errorf("build redactor: %w", err)
	}

	// Walk the filesystem.
	ext, err := extractor.NewFilesystemExtractor(scanRoot, scanMaxBytes)
	if err != nil {
		return fmt.Errorf("init extractor: %w", err)
	}

	ctx, cancel := context.WithCancel(cmdObj.Context())
	defer cancel()

	units := make(chan extractor.InputUnit, 64)
	skips := make(chan extractor.SkipEvent, 64)

	extErrCh := make(chan error, 1)
	go func() { extErrCh <- ext.Run(ctx, units, skips) }()

	var filesScanned, filesSkipped int
	var bytesScanned int64

	for units != nil || skips != nil {
		select {
		case u, ok := <-units:
			if !ok {
				units = nil
				continue
			}
			filesScanned++
			bytesScanned += int64(len(u.Content))
			// TODO(phase1): when pkg/engine lands, run detector here:
			//   findings = engine.Scan(u, matcher) → append to findings list
			_ = u
		case s, ok := <-skips:
			if !ok {
				skips = nil
				continue
			}
			filesSkipped++
			_ = s
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := <-extErrCh; err != nil {
		return fmt.Errorf("extractor: %w", err)
	}

	out := output.Output{
		Version: "v0",
		ScanMetadata: output.ScanMetadata{
			ToolVersion:    versionInfo.Version,
			StartedAt:      started,
			DurationMS:     time.Since(started).Milliseconds(),
			ScanRoot:       scanRoot,
			Visibility:     scanVisibility,
			FilesScanned:   filesScanned,
			BytesScanned:   bytesScanned,
			FilesSkipped:   filesSkipped,
			CatalogsLoaded: statuses,
		},
		Summary: output.ScanSummary{
			FindingsTotal: 0,
			BySeverity:    map[string]int{},
			BySurface:     map[string]int{},
		},
		Findings: nil,
	}

	formatter := output.NewJSONFormatter(os.Stdout, redactor)
	if err := formatter.Emit(out); err != nil {
		return fmt.Errorf("emit: %w", err)
	}

	return nil
}

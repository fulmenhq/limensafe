package cmd

import (
	"github.com/spf13/cobra"

	"github.com/fulmenhq/limensafe/pkg/extractor"
)

// registerScanCatalogFlags registers the catalog/scan-posture flag set that is
// shared, identically, by every command that loads catalogs and runs the
// detector — today `scan` and `audit-publish`. Defining them once here is the
// single source of truth for their names, defaults, usage strings, and the
// package-level vars they bind to, so the commands cannot drift apart (see
// ADR-0007). The bound vars are read by loadCatalogsAndStatuses and the
// effective{BlockThreshold,PrivateCatalogMissing} helpers; because at most one
// command runs per invocation, sharing the storage is safe.
//
// Commands with genuinely different flag semantics (e.g. `attest`, whose
// --mode defaults to "local" and whose --config-file is reserved) deliberately
// do NOT use this helper — unifying them would change their behavior. The
// principle is to centralize flags that are the same, not to force flags that
// merely share a name.
func registerScanCatalogFlags(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&scanCatalogs, "catalog", nil,
		"Path to a catalog YAML file (repeatable for layered catalogs)")
	cmd.Flags().StringVar(&scanConfigFile, "config-file", "",
		"Path to .limensafe/config.yaml; resolves catalogs by reference and supplies repo visibility")
	cmd.Flags().StringVar(&scanVisibility, "visibility", "public_oss",
		"Repo visibility scope (public_oss, unlisted_oss, internal, engagement_private, local_only)")
	cmd.Flags().StringVar(&scanMode, "mode", "",
		"Scan posture macro (local|ci|release); explicit posture flags win")
	cmd.Flags().IntVar(&scanWorkers, "workers", 0,
		"Number of worker goroutines for filesystem scans (default: runtime.NumCPU())")
	cmd.Flags().Int64Var(&scanMaxBytes, "max-file-size", extractor.DefaultMaxFileSize,
		"Per-file size cap; files exceeding this emit a skip event")
	cmd.Flags().StringVar(&scanPrivateMissing, "private-catalog-missing", "",
		"Missing optional private catalog posture (silent|warn|error)")
}

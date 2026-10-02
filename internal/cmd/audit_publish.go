package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/fulmenhq/limensafe/pkg/catalog"
	"github.com/fulmenhq/limensafe/pkg/coverage"
	"github.com/fulmenhq/limensafe/pkg/engine"
	"github.com/fulmenhq/limensafe/pkg/extractor"
	"github.com/fulmenhq/limensafe/pkg/output"
	"github.com/fulmenhq/limensafe/pkg/publish"
)

// audit-publish own flags. The catalog/posture flags are registered onto the
// same package-level scan* vars (see init) so the shared catalog-load + engine
// config logic is reused verbatim; internal-brief centralizes the flag *definitions*.
var (
	auditRemote         string
	auditLocalRefs      bool
	auditNamesOnly      bool
	auditBranchesOnly   bool
	auditTagsOnly       bool
	auditPrimaryRef     string
	auditDangerPatterns []string
)

var auditPublishCmd = &cobra.Command{
	Use:   "audit-publish [repo-path]",
	Short: "Audit every ref a repo would expose when made public; flag leak-vector refs",
	Long: `Audit the publish surface of a repository before making it public.

Unlike scan (which audits one surface you name), audit-publish enumerates
EVERY ref the repository would expose when published — all branches and tags
on the publish remote — scans each, and flags leak-vector refs:

  - refs that carry protected entities the primary ref does not
    (diverges_from_primary) — the high-signal detector that catches a backup
    branch a history rewrite left behind; and
  - refs whose names match danger patterns (backup/*, *pre-rewrite*, *-bak,
    wip/*) — flagged even without scanning content.

It produces a single go/no-go report: summary.publish_safe plus the list of
leak_vector refs and a suggested_action for each. Detection and advice only —
limensafe never deletes or rewrites refs.

Ref content is scanned from local objects. Refs that exist on the remote but
are not fetched locally are still name-pattern checked; run 'git fetch --all'
first for full content coverage.

Release mode requires complete selected-scope coverage or explicit reason
ceilings (--allow-skip-reason REASON=N). Missing local objects or an unscanned
primary baseline are not budgetable. --names-only conflicts with release mode.

Exit codes (the locked scan contract):
  0 — publish_safe: no leak-vector ref and no block-tier finding
  1 — not safe: leak-vector ref, block-tier finding, or incomplete release coverage
  2 — config / catalog validation error
  3 — runtime error (git enumeration or I/O failure)`,
	Args:          cobra.MaximumNArgs(1),
	RunE:          runAuditPublish,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	// Shared catalog/posture flags (--catalog, --config-file, --visibility,
	// --mode, --workers, --max-file-size, --private-catalog-missing) — the same
	// definitions scan uses, bound to the scan* globals so
	// loadCatalogsAndStatuses and the threshold helpers are reused unchanged
	// (flags.go / ADR-0007).
	registerScanCatalogFlags(auditPublishCmd)

	// audit-publish-specific flags.
	auditPublishCmd.Flags().StringVar(&auditRemote, "remote", "origin",
		"Publish remote to inventory")
	auditPublishCmd.Flags().BoolVar(&auditLocalRefs, "local-refs", false,
		"Inventory local refs instead of the remote (default: the remote, what actually goes public)")
	auditPublishCmd.Flags().BoolVar(&auditNamesOnly, "names-only", false,
		"Flag danger-pattern refs by name without scanning blob content (fast pre-flight)")
	auditPublishCmd.Flags().BoolVar(&auditBranchesOnly, "branches-only", false,
		"Audit branches only (tags are part of the publish surface and scanned by default)")
	auditPublishCmd.Flags().BoolVar(&auditTagsOnly, "tags-only", false,
		"Audit tags only")
	auditPublishCmd.Flags().StringVar(&auditPrimaryRef, "primary-ref", "",
		"Ref divergence is measured against (default: the remote's HEAD, falling back to main)")
	auditPublishCmd.Flags().StringSliceVar(&auditDangerPatterns, "danger-pattern", nil,
		"Override the default danger-name globs (backup/*, *pre-rewrite*, *-snapshot-*, archive/*, *-bak, wip/*)")

	rootCmd.AddCommand(auditPublishCmd)
}

func runAuditPublish(cmdObj *cobra.Command, args []string) error {
	allowances, err := coverage.ParseAllowances(scanSkipAllowances)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrConfigInvalid, err)
	}
	if scanMode == "release" && auditNamesOnly {
		return fmt.Errorf("%w: --mode release conflicts with --names-only", ErrConfigInvalid)
	}
	coverageSummary := coverage.New("unique_blob", allowances)
	repoRoot := "."
	if len(args) == 1 {
		repoRoot = args[0]
	}
	if auditBranchesOnly && auditTagsOnly {
		return fmt.Errorf("%w: --branches-only and --tags-only are mutually exclusive", ErrConfigInvalid)
	}
	dangerPatterns := extractor.DefaultDangerPatterns
	if len(auditDangerPatterns) > 0 {
		dangerPatterns = auditDangerPatterns
	}
	started := time.Now()

	cats, statuses, privateStatuses, configVisibility, blockThreshold, err := loadCatalogsAndStatuses(cmdObj)
	if err != nil {
		return err
	}
	// Content scanning needs a catalog; --names-only does not (it only matches
	// ref names structurally).
	if !auditNamesOnly && len(cats) == 0 && !canScanWithoutLoadedCatalogs(statuses) {
		return fmt.Errorf("%w: at least one catalog must be loaded (use --config-file or --catalog), or pass --names-only", ErrConfigInvalid)
	}

	// Visibility precedence: --visibility flag (if set) > config > default.
	if !cmdObj.Flags().Changed("visibility") && configVisibility != "" {
		scanVisibility = configVisibility
	}

	var redactor *output.Redactor
	if len(cats) > 0 {
		aliases := catalog.MergeAliases(cats)
		redactor, err = output.NewRedactor(aliases)
		if err != nil {
			return fmt.Errorf("%w: build redactor: %w", ErrConfigInvalid, err)
		}
	}
	emitCatalogWarnings(cats, redactor)

	scanner, err := engine.NewScannerWithOptions(cats, scanVisibility, engine.ScannerOptions{BlockThreshold: blockThreshold})
	if err != nil {
		return fmt.Errorf("%w: build engine: %w", ErrConfigInvalid, err)
	}

	ctx, cancel := context.WithCancel(cmdObj.Context())
	defer cancel()

	// Enumerate branches AND tags for discovery so the primary ref can always
	// be located for the divergence baseline, even when the emitted surface is
	// filtered (e.g. --tags-only). The surface (what goes in refs[]) is the
	// filtered subset.
	allRefs, err := extractor.EnumerateRefs(ctx, repoRoot, extractor.RefEnumerationOptions{
		Remote:       auditRemote,
		IncludeLocal: auditLocalRefs,
		Branches:     true,
		Tags:         true,
	})
	if err != nil {
		// Redact: the error embeds the operator-controlled remote name (and raw
		// git args/stderr), which may contain protected vocabulary (ADR-0003).
		return fmt.Errorf("%w: enumerate refs: %s", ErrRuntime, redactRuntime(redactor, err))
	}
	var surfaceRefs []extractor.RemoteRef
	for _, r := range allRefs {
		if (r.Kind == extractor.RefKindBranch && auditTagsOnly) || (r.Kind == extractor.RefKindTag && auditBranchesOnly) {
			continue
		}
		surfaceRefs = append(surfaceRefs, r)
		if !auditNamesOnly && !r.LocalObjects {
			coverageSummary.AddGap(coverage.RefMissingLocalObjects)
		}
	}
	primary, err := extractor.ResolvePrimaryRef(ctx, repoRoot, auditRemote, auditPrimaryRef)
	if err != nil {
		return fmt.Errorf("%w: resolve primary ref: %s", ErrRuntime, redactRuntime(redactor, err))
	}
	var primaryRef *extractor.RemoteRef
	for i := range allRefs {
		if allRefs[i].Name == primary {
			primaryRef = &allRefs[i]
			break
		}
	}

	refEngineFindings := map[string][]engine.Finding{}
	refEntitySet := map[string]map[string]bool{}
	refBlockTier := map[string]bool{}
	refScanned := map[string]bool{}
	var stats extractor.PublishSurfaceStats
	primaryBaselineScanned := false

	if !auditNamesOnly {
		// Scan set = surface refs with local objects, PLUS the primary ref
		// (even when it is outside the surface) so its baseline is established.
		seen := map[string]bool{}
		var scanSet []extractor.RemoteRef
		add := func(r extractor.RemoteRef) {
			if r.LocalObjects && !seen[r.Name] {
				seen[r.Name] = true
				scanSet = append(scanSet, r)
			}
		}
		for _, r := range surfaceRefs {
			add(r)
		}
		if primaryRef != nil {
			add(*primaryRef)
		}
		ex, err := extractor.NewPublishSurfaceExtractor(repoRoot, scanSet, scanMaxBytes)
		if err != nil {
			return fmt.Errorf("%w: init publish-surface extractor: %s", ErrRuntime, redactRuntime(redactor, err))
		}
		if err := ex.Enumerate(ctx); err != nil {
			return fmt.Errorf("%w: enumerate publish surface: %s", ErrRuntime, redactRuntime(redactor, err))
		}
		stats = ex.Stats()
		for _, r := range scanSet {
			refScanned[r.Name] = true
		}
		primaryBaselineScanned = primaryRef != nil && primaryRef.LocalObjects
		if !primaryBaselineScanned {
			coverageSummary.AddGap(coverage.PrimaryBaselineUnscanned)
			// Explicit boundary: without a scannable primary baseline,
			// divergence detection is skipped (name-pattern flagging still
			// applies). Message is value-free.
			_, _ = fmt.Fprintln(os.Stderr, "audit-publish: primary ref baseline could not be scanned (not fetched locally); divergence detection skipped — name-pattern flagging only. Run 'git fetch --all' for full coverage.")
		}
		if err := scanPublishBlobs(ctx, ex, scanner, redactor, refEngineFindings, refEntitySet, refBlockTier); err != nil {
			return err
		}
		for _, skip := range ex.Skips() {
			accountCoverageSkip(&coverageSummary, skip)
			emitSkipWarning(skip, redactor)
		}
		for _, blob := range ex.SkippedBlobs() {
			for _, ref := range blob.Refs {
				refScanned[ref] = false
			}
		}
	} else {
		coverageSummary.AddGap(coverage.NamesOnlyContentOmitted)
	}

	refEntities := map[string][]string{}
	for ref, set := range refEntitySet {
		refEntities[ref] = sortedStringSet(set)
	}

	verdict := publish.Evaluate(publish.EvaluateInput{
		Refs:                   surfaceRefs,
		Primary:                primary,
		RefEntities:            refEntities,
		RefScanned:             refScanned,
		RefBlockTier:           refBlockTier,
		PrimaryBaselineScanned: primaryBaselineScanned,
		DangerPatterns:         dangerPatterns,
		Remote:                 auditRemote,
	})

	out := buildPublishOutput(verdict, surfaceRefs, refEngineFindings, statuses, privateStatuses, stats, started, primaryBaselineScanned)
	out.Summary.Coverage = coverageSummary
	if scanMode == "release" && coverageSummary.Status == "incomplete" {
		out.Summary.PublishSafe = false
	}

	formatter := output.NewPublishSurfaceFormatter(os.Stdout, redactor)
	if err := formatter.Emit(out); err != nil {
		return fmt.Errorf("%w: emit: %s", ErrRuntime, redactRuntime(redactor, err))
	}

	// Human-readable, redaction-safe leak-vector warnings on stderr (the
	// internal-brief rewrite-hygiene copy). JSON stays on stdout, diagnostics on
	// stderr — stream contract unchanged.
	emitLeakVectorWarnings(os.Stderr, verdict, redactor)

	// Exit 1 on any leak-vector ref OR any block-tier finding (== !publish_safe).
	if !out.Summary.PublishSafe {
		return ErrFindingsBlocked
	}
	return nil
}

// scanPublishBlobs scans each unique blob once for content, scans each of its
// paths for path-segment findings, and attributes both to the refs that reach
// the blob at each path — mirroring the internal-brief history model lifted to refs.
func scanPublishBlobs(
	ctx context.Context,
	ex *extractor.PublishSurfaceExtractor,
	scanner *engine.Scanner,
	redactor *output.Redactor,
	refEngineFindings map[string][]engine.Finding,
	refEntitySet map[string]map[string]bool,
	refBlockTier map[string]bool,
) error {
	attribute := func(ref, path string, f engine.Finding) {
		f.Path = path
		f.SourceID = path
		f.GitRef = ref
		f.SurfaceKind = "blob"
		f = engine.RefreshFingerprint(f)
		refEngineFindings[ref] = append(refEngineFindings[ref], f)
		set := refEntitySet[ref]
		if set == nil {
			set = map[string]bool{}
			refEntitySet[ref] = set
		}
		if f.EntityID != "" {
			set[f.EntityID] = true
		}
		if f.Decision == "block" {
			refBlockTier[ref] = true
		}
	}

	for _, blob := range ex.Blobs() {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: publish scan canceled", ErrRuntime)
		default:
		}
		content, err := ex.ReadBlob(ctx, blob.SHA)
		if err != nil {
			return fmt.Errorf("%w: read blob: %s", ErrRuntime, redactRuntime(redactor, err))
		}
		// Content findings: scanned once, path suppressed.
		contentFindings := scanner.ScanUnit(extractor.InputUnit{
			SourceID:   blob.SHA,
			SourceKind: extractor.SourceKindGitHistoryBlob,
			Content:    content,
			Encoding:   "utf-8",
			Metadata:   map[string]string{"blob_sha": blob.SHA, "suppress_path": "true"},
		})
		for _, pr := range blob.Paths {
			// Path-segment findings for this path.
			pathFindings := scanner.ScanUnit(extractor.InputUnit{
				SourceID:     pr.Path,
				SourceKind:   extractor.SourceKindGitHistoryBlob,
				LocationHint: pr.Path,
				Encoding:     "utf-8",
				Metadata:     map[string]string{"blob_sha": blob.SHA},
			})
			for _, ref := range pr.Refs {
				for _, f := range contentFindings {
					attribute(ref, pr.Path, f)
				}
				for _, f := range pathFindings {
					attribute(ref, pr.Path, f)
				}
			}
		}
	}
	for _, blob := range ex.SkippedBlobs() {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: publish scan canceled", ErrRuntime)
		default:
		}
		for _, pr := range blob.Paths {
			pathFindings := scanner.ScanUnit(extractor.InputUnit{
				SourceID: pr.Path, SourceKind: extractor.SourceKindGitHistoryBlob,
				LocationHint: pr.Path, Encoding: "utf-8",
				Metadata: map[string]string{"blob_sha": blob.SHA},
			})
			for _, ref := range pr.Refs {
				for _, finding := range pathFindings {
					attribute(ref, pr.Path, finding)
				}
			}
		}
	}
	return nil
}

// buildPublishOutput assembles the report document from the verdict and the
// per-ref findings.
func buildPublishOutput(
	verdict publish.SurfaceVerdict,
	refs []extractor.RemoteRef,
	refEngineFindings map[string][]engine.Finding,
	statuses []output.CatalogLoadStatus,
	privateStatuses []output.PrivateCatalogStatus,
	stats extractor.PublishSurfaceStats,
	started time.Time,
	primaryScanned bool,
) output.PublishSurfaceOutput {
	verdictByName := map[string]publish.RefVerdict{}
	for _, v := range verdict.Refs {
		verdictByName[v.Ref.Name] = v
	}

	var outRefs []output.PublishRef
	totalFindings := 0
	surfaceScanned := 0
	bySeverity := map[string]int{}
	for _, ref := range refs {
		v := verdictByName[ref.Name]
		if v.Scanned {
			surfaceScanned++
		}
		findings := toOutputFindings(refEngineFindings[ref.Name])
		refBySeverity := countBySeverity(findings)
		for sev, n := range refBySeverity {
			bySeverity[sev] += n
		}
		totalFindings += len(findings)
		outRefs = append(outRefs, output.PublishRef{
			Name:               ref.Name,
			Kind:               ref.Kind,
			Tip:                ref.Tip,
			Scanned:            v.Scanned,
			LeakVector:         v.LeakVector,
			Reasons:            v.Reasons,
			SuggestedAction:    v.SuggestedAction,
			FindingsBySeverity: refBySeverity,
			Findings:           findings,
		})
	}

	return output.PublishSurfaceOutput{
		Version:               "v0",
		OutputSchemaVersion:   output.PublishSurfaceOutputSchemaVersion,
		ToolVersion:           versionInfo.Version,
		StartedAt:             started,
		DurationMS:            time.Since(started).Milliseconds(),
		Remote:                auditRemote,
		PrimaryRef:            verdict.PrimaryRef,
		Visibility:            scanVisibility,
		CatalogsLoaded:        statuses,
		PrivateCatalogsStatus: privateStatuses,
		Surface: output.PublishSurfaceCounts{
			RefsTotal:       len(refs),
			RefsScanned:     surfaceScanned,
			PrimaryScanned:  primaryScanned,
			LeakVectorCount: len(verdict.LeakVectorRefs),
			UniqueBlobs:     stats.UniqueBlobs,
			BlobsScanned:    stats.BlobsScanned,
			CommitsScanned:  stats.CommitsScanned,
		},
		Summary: output.PublishSummary{
			PublishSafe:    verdict.PublishSafe,
			LeakVectorRefs: verdict.LeakVectorRefs,
			FindingsTotal:  totalFindings,
			BySeverity:     bySeverity,
		},
		Refs: outRefs,
	}
}

// emitLeakVectorWarnings prints the internal-brief rewrite-hygiene copy to w for each
// leak-vector ref. Ref names and suggested actions route through the redactor
// (when available) so a ref named after protected vocabulary cannot leak
// (ADR-0003). Name-pattern refs get the EXTREMELY-LOUD backup-defeat copy;
// divergence-only refs get a concise removal directive.
func emitLeakVectorWarnings(w io.Writer, verdict publish.SurfaceVerdict, redactor *output.Redactor) {
	red := func(s string) string {
		if redactor == nil {
			return s
		}
		return redactor.Redact(s)
	}
	for _, v := range verdict.Refs {
		if !v.LeakVector {
			continue
		}
		name := red(v.Ref.Name)
		action := red(v.SuggestedAction)
		var msg string
		if hasReason(v.Reasons, publish.ReasonNamePattern) {
			// EXTREMELY-LOUD backup-defeat copy (internal-brief).
			msg = fmt.Sprintf("\n⚠️  LEAK-VECTOR REF DETECTED — %s\n\n"+
				"    This ref's name claims to preserve a pre-action snapshot. If a history\n"+
				"    rewrite was performed, it likely contains exactly what was scrubbed.\n"+
				"    Leaving it on a publishable remote DEFEATS the rewrite.\n\n"+
				"      Remove:  %s\n\n"+
				"    If DR value remains, retain the ref in operator-private storage off this\n"+
				"    remote. This is part of completing the rewrite, not optional cleanup.\n", name, action)
		} else {
			// Divergence-only leak vector.
			msg = fmt.Sprintf("\n⚠️  LEAK-VECTOR REF — %s carries protected content the primary does not.\n"+
				"      Remove before publishing:  %s\n", name, action)
		}
		// Best-effort diagnostic; a stderr write failure must not change the
		// exit code, so the error is deliberately ignored.
		_, _ = io.WriteString(w, msg)
	}
}

func hasReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

// redactRuntime renders a runtime error's text with protected vocabulary
// masked, for inclusion in a fatal diagnostic. audit-publish errors can embed
// operator-controlled values (remote name, git args/stderr, paths) that may
// contain protected aliases; main.go prints runtime errors verbatim, so they
// must be redacted at this boundary (ADR-0003), mirroring scan's git-error
// handling. When no redactor is loaded (e.g. --names-only with no catalog),
// there is no protected vocabulary defined and the text passes through.
func redactRuntime(redactor *output.Redactor, err error) string {
	if redactor != nil {
		return redactor.Redact(err.Error())
	}
	return err.Error()
}

func sortedStringSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

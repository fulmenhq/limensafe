package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fulmenhq/limensafe/pkg/catalog"
)

var (
	visCatalogIn  string
	visMap        string
	visOut        string
	visAllowError bool
)

var catalogVisibilityCmd = &cobra.Command{
	Use:   "visibility-allowlist --catalog <file> --map <file> --out <file>",
	Short: "Enrich a catalog with allowlist entries for codenames whose repo is now public",
	Long: `Resolve codename→repository visibility and allowlist the public ones (internal-brief Part B).

Reads a base catalog and a flat mapping file:

  CODENAME==>owner/repo

For each mapping, the current repository visibility is resolved via the forge
(` + "`gh repo view <repo> --json visibility`" + ` semantics). Codenames whose backing
repository resolves as PUBLIC gain a literal allowlist entry in the output
catalog; every other outcome is left blocked.

Fail-safe by construction: a 404, auth failure, rate limit, or a missing gh CLI
resolves as "unresolved" and never produces an allowlist entry — an ambiguous
answer can never demote a still-private codename. Duplicate codenames are
fail-closed: an exact-duplicate line is collapsed, but the same codename mapped
to a different repo is rejected as an ambiguity (exit 2). Pass
--require-resolution to abort (before writing --out) when any mapping is
unresolved (a stricter CI posture).

The base catalog is preserved faithfully (entities, rules, notes, salt) and the
generated allowlist ids are opaque, hash-derived values that never carry the
protected codename. The output contains protected vocabulary by construction,
so --out is required and is created with 0600 permissions; the two-layer
catalog rule applies (keep real catalogs out of the repo tree).

Diagnostics and the summary are redaction-safe: only counts are reported, never
codenames or repository names (a non-public repo name may itself be sensitive).

Exit codes:
  0 — catalog enriched and written
  2 — invalid base catalog, mapping, or options
  3 — runtime error (input read or output write failure)`,
	Args:          cobra.NoArgs,
	RunE:          runCatalogVisibilityAllowlist,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	catalogVisibilityCmd.Flags().StringVar(&visCatalogIn, "catalog", "",
		"Path to the base catalog YAML to enrich (required)")
	catalogVisibilityCmd.Flags().StringVar(&visMap, "map", "",
		"Path to the CODENAME==>owner/repo mapping file (required; use - for stdin)")
	catalogVisibilityCmd.Flags().StringVar(&visOut, "out", "",
		"Path to write the enriched catalog YAML (required; output holds protected vocabulary, so there is no stdout default)")
	catalogVisibilityCmd.Flags().BoolVar(&visAllowError, "require-resolution", false,
		"Abort (before writing --out) if any mapping could not be resolved (stricter CI posture; default leaves unresolved codenames blocked and writes the enriched catalog)")
	catalogCmd.AddCommand(catalogVisibilityCmd)
}

func runCatalogVisibilityAllowlist(cmdObj *cobra.Command, _ []string) error {
	if visCatalogIn == "" {
		return fmt.Errorf("%w: visibility-allowlist: --catalog is required", ErrConfigInvalid)
	}
	if visMap == "" {
		return fmt.Errorf("%w: visibility-allowlist: --map is required", ErrConfigInvalid)
	}
	if visOut == "" {
		return fmt.Errorf("%w: visibility-allowlist: --out is required (output holds protected vocabulary; there is no stdout default)", ErrConfigInvalid)
	}

	baseBytes, err := os.ReadFile(visCatalogIn)
	if err != nil {
		// Value-free: the path is operator-controlled and may embed a protected
		// name (ADR-0003). The operator knows the path they passed.
		return fmt.Errorf("%w: visibility-allowlist: read base catalog failed", ErrRuntime)
	}
	mapBytes, err := readTermListSource(visMap)
	if err != nil {
		return fmt.Errorf("%w: visibility-allowlist: read mapping file failed", ErrRuntime)
	}

	enriched, summary, err := catalog.BuildVisibilityAllowlist(
		cmdObj.Context(), baseBytes, bytes.NewReader(mapBytes), ghVisibilityResolver{},
	)
	if err != nil {
		// Builder errors are redaction-safe (value-free) and describe an invalid
		// base catalog, mapping, or options → config error (exit 2).
		return fmt.Errorf("%w: visibility-allowlist: %w", ErrConfigInvalid, err)
	}

	// Value-free summary: counts only, never codenames or repo names. Emitted
	// before any strict-mode abort so the operator still sees the tally.
	fmt.Fprintf(os.Stderr,
		"visibility-allowlist: mappings=%d public=%d (added=%d) not_public=%d unresolved=%d\n",
		summary.Total, summary.Public, summary.Added, summary.NotPublic, summary.Unresolved)

	// Strict mode aborts BEFORE writing (entarch): --require-resolution must not
	// leave a partially-enriched catalog on disk when the run failed its own gate.
	if visAllowError && summary.Unresolved > 0 {
		return fmt.Errorf("%w: visibility-allowlist: %d mapping(s) unresolved with --require-resolution set", ErrRuntime, summary.Unresolved)
	}

	// 0o600 on create: the enriched catalog is protected vocabulary by
	// construction, so a newly-created output must not be group/world-readable.
	// (WriteFile only applies the mode on create; an existing file keeps its
	// permissions — same posture as `catalog build`.)
	if err := os.WriteFile(visOut, enriched, 0o600); err != nil {
		return fmt.Errorf("%w: visibility-allowlist: write output catalog failed", ErrRuntime)
	}
	return nil
}

// ghVisibilityResolver resolves repository visibility via the GitHub CLI. It
// fails safe: any error (missing gh, 404, auth failure, rate limit, parse
// failure) yields RepoUnresolved, never RepoPublic.
type ghVisibilityResolver struct{}

func (ghVisibilityResolver) Resolve(ctx context.Context, repo string) catalog.RepoVisibility {
	cmd := exec.CommandContext(ctx, "gh", "repo", "view", repo, "--json", "visibility")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil // never surface forge stderr (may carry the private repo name)
	if err := cmd.Run(); err != nil {
		return catalog.RepoUnresolved
	}
	var resp struct {
		Visibility string `json:"visibility"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return catalog.RepoUnresolved
	}
	return classifyForgeVisibility(resp.Visibility)
}

// classifyForgeVisibility maps a forge visibility string to a verdict, failing
// safe on anything that is not an explicit "public". gh reports visibility as
// public/private/internal (case varies by API surface).
func classifyForgeVisibility(v string) catalog.RepoVisibility {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "public":
		return catalog.RepoPublic
	case "private", "internal":
		return catalog.RepoNotPublic
	default:
		return catalog.RepoUnresolved
	}
}

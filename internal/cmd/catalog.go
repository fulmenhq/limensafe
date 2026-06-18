package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/fulmenhq/limensafe/pkg/catalog"
)

var (
	catalogBuildFromTermList string
	catalogBuildOut          string
	catalogBuildID           string
	catalogBuildClass        string
	catalogBuildSeverity     string
	catalogBuildNoWholeWord  bool
)

var catalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "Build and manage vocabulary catalogs",
	Long: `Build and manage limensafe vocabulary catalogs.

Catalogs hold the protected vocabulary a scan matches against. Real
organizational catalogs live outside the repository tree and are passed to
scan by path; see the two-layer catalog rule in the docs.`,
}

var catalogBuildCmd = &cobra.Command{
	Use:   "build --from-termlist <file> --out <file> --catalog-id <id>",
	Short: "Build a conformant catalog from a flat PROTECTED==>replacement term-list",
	Long: `Build a schema-conformant catalog YAML from a flat term-list.

Each input line maps a protected term to its replacement:

  PROTECTED==>replacement

Terms that share a replacement collapse into one entity (the replacement
becomes its replacement_suggestion). Blank lines and lines beginning with '#'
are ignored. A trailing " # class=... severity=..." directive overrides the
per-entity class and severity for that replacement group:

  AcmeCorp==>ClientAlpha   # class=client_identity severity=critical

Generated entities default to the validated catalog-B posture
(case_insensitive + slug + whole_word). Whole-word matching is on by default
because it removes the substring false-positive class; pass --no-whole-word to
opt out. Entity ids are stable, opaque, hash-derived values — never positional
and never derived from protected text — so editing the term-list never
renumbers unrelated entities.

The output contains protected vocabulary by construction, so --out is required
and there is no stdout default; treat the generated file as sensitive (the
two-layer catalog rule applies — keep real catalogs out of the repo tree).

Diagnostics are redaction-safe: malformed input is reported by line number and
structural reason only, never by echoing the protected term or replacement.

Exit codes:
  0 — catalog generated and written
  2 — invalid term-list or options (validation error)
  3 — runtime error (input read or output write failure)`,
	Args:          cobra.NoArgs,
	RunE:          runCatalogBuild,
	SilenceErrors: true, // main maps the sentinels; avoid double-printing
	SilenceUsage:  true, // a bad term-list is an operational error, not misuse
}

func init() {
	catalogBuildCmd.Flags().StringVar(&catalogBuildFromTermList, "from-termlist", "",
		"Path to the flat term-list source (PROTECTED==>replacement per line); use - for stdin")
	catalogBuildCmd.Flags().StringVar(&catalogBuildOut, "out", "",
		"Path to write the generated catalog YAML (required; output holds protected vocabulary, so there is no stdout default)")
	catalogBuildCmd.Flags().StringVar(&catalogBuildID, "catalog-id", "",
		"catalog_id for the generated catalog (required)")
	catalogBuildCmd.Flags().StringVar(&catalogBuildClass, "default-class", "codename",
		"Class for entities without a per-line class directive; prefer a specific class (client_identity, person, ...) when the term's class is known")
	catalogBuildCmd.Flags().StringVar(&catalogBuildSeverity, "default-severity", "",
		"catalog-level default_severity (optional)")
	catalogBuildCmd.Flags().BoolVar(&catalogBuildNoWholeWord, "no-whole-word", false,
		"Disable whole-word matching for generated entities (whole-word is on by default and removes the substring false-positive class)")

	catalogCmd.AddCommand(catalogBuildCmd)
	rootCmd.AddCommand(catalogCmd)
}

func runCatalogBuild(_ *cobra.Command, _ []string) error {
	// Validate required options before touching the filesystem. Missing/empty
	// required input is a configuration error (exit 2), not an I/O fault.
	if catalogBuildFromTermList == "" {
		return fmt.Errorf("%w: catalog build: --from-termlist is required", ErrConfigInvalid)
	}
	if catalogBuildOut == "" {
		return fmt.Errorf("%w: catalog build: --out is required (output holds protected vocabulary; there is no stdout default)", ErrConfigInvalid)
	}
	if catalogBuildID == "" {
		return fmt.Errorf("%w: catalog build: --catalog-id is required", ErrConfigInvalid)
	}

	// Read the whole term-list at the command boundary so input-side I/O faults
	// classify as runtime errors (exit 3). Passing the bytes to the builder
	// means every error it returns is purely about term-list content/shape,
	// which classifies cleanly as a validation error (exit 2). Term-lists are
	// flat and small, so a full read is appropriate.
	src, err := readTermListSource(catalogBuildFromTermList)
	if err != nil {
		// Value-free: a filesystem path is operator-controlled and may itself
		// embed a protected engagement/client name (ADR-0003). Do not echo the
		// path or wrap the raw *os.PathError. The operator knows the path they
		// passed; the structural reason is enough.
		return fmt.Errorf("%w: catalog build: read term-list source failed", ErrRuntime)
	}

	data, err := catalog.BuildCatalogFromTermList(bytes.NewReader(src), catalog.TermListOptions{
		CatalogID:        catalogBuildID,
		DefaultClass:     catalogBuildClass,
		DefaultSeverity:  catalogBuildSeverity,
		DisableWholeWord: catalogBuildNoWholeWord,
	})
	if err != nil {
		// Builder errors are redaction-safe (line-numbered, value-free) and
		// describe an invalid term-list or options → config error (exit 2).
		return fmt.Errorf("%w: catalog build: %w", ErrConfigInvalid, err)
	}

	// 0o600: the output is protected vocabulary by construction, so a
	// newly-created catalog must not be group/world-readable. (WriteFile only
	// applies the mode on create; an existing file keeps its permissions.)
	if err := os.WriteFile(catalogBuildOut, data, 0o600); err != nil {
		return fmt.Errorf("%w: catalog build: write output catalog failed", ErrRuntime)
	}

	// Redaction-safe success line on stderr: an entity count only. The --out
	// path is operator-controlled and is not echoed (it may carry a protected
	// name), consistent with the value-free error paths above.
	c, err := catalog.LoadBytes(data)
	if err != nil {
		// Should not happen: BuildCatalogFromTermList round-trips through the
		// loader before returning. Treat as runtime rather than silently lying.
		return fmt.Errorf("%w: catalog build: re-load generated catalog failed", ErrRuntime)
	}
	fmt.Fprintf(os.Stderr, "catalog written (%d entities)\n", len(c.Entities))
	return nil
}

// readTermListSource reads the term-list from a file path, or from stdin when
// the path is "-".
func readTermListSource(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

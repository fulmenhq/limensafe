package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/fulmenhq/gofulmen/foundry"

	"github.com/fulmenhq/limensafe/internal/cmd"
	"github.com/fulmenhq/limensafe/internal/server/handlers"
)

// Version information set via ldflags during build
// Example: go build -ldflags="-X main.version=1.0.0 -X main.commit=abc123 -X main.buildDate=2025-10-28"
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	// Set version info for commands to access
	cmd.SetVersionInfo(version, commit, buildDate)

	// Set version info for HTTP handlers
	handlers.SetVersionInfo(version, commit, buildDate)

	// Execute root command
	if err := cmd.Execute(); err != nil {
		// Scan-command exit-code dispatch — see internal/cmd/scan.go for
		// the sentinel definitions and CONTRIBUTING.md for the contract.
		//
		// ErrFindingsBlocked is the "scan completed; gate failed" outcome.
		// Output is already on stdout; exit 1 without printing "Command
		// execution failed" so make sanitize-check / pre-commit gates can
		// rely on the JSON report as the explanation.
		switch {
		case errors.Is(err, cmd.ErrFindingsBlocked):
			os.Exit(1)
		case errors.Is(err, cmd.ErrConfigInvalid):
			// Config / catalog validation error → exit 2 per scan CLI
			// contract (documented in `scan --help` + CONTRIBUTING.md).
			fmt.Fprintf(os.Stderr, "config error: %v\n", err)
			os.Exit(2)
		case errors.Is(err, cmd.ErrRuntime):
			// Runtime / I/O error → exit 3 per scan CLI contract.
			fmt.Fprintf(os.Stderr, "runtime error: %v\n", err)
			os.Exit(3)
		default:
			// Unclassified errors fall through to foundry's generic
			// failure code (1). This is the safety net for anything not
			// explicitly tagged with a scan sentinel — typically commands
			// outside the scan flow (health, doctor, serve, etc.) and
			// any future scan errors not yet wrapped.
			cmd.ExitWithCodeStderr(foundry.ExitFailure, "Command execution failed", err)
		}
	}
}

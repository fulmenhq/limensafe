package main

import (
	"errors"
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
		// ErrFindingsBlocked is the "scan completed; gate failed" outcome.
		// Output is already on stdout; exit 1 without printing "Command
		// execution failed" so make sanitize-check / pre-commit gates can
		// rely on the JSON report as the explanation.
		if errors.Is(err, cmd.ErrFindingsBlocked) {
			os.Exit(1)
		}
		// Other errors are real failures; print + exit per the existing
		// foundry convention.
		cmd.ExitWithCodeStderr(foundry.ExitFailure, "Command execution failed", err)
	}
}

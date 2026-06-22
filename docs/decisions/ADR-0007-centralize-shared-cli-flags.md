# ADR-0007: Centralize shared CLI flags

**Status**: Accepted
**Date**: 2026-06-22
**Deciders**: @3leapsdave
**Role**: devlead

## Context

Several commands need the same catalog/scan-posture flags: `--catalog`,
`--config-file`, `--visibility`, `--mode`, `--workers`, `--max-file-size`,
`--private-catalog-missing`. `scan` defined them first; when `audit-publish`
(internal-brief) needed the same set, it re-registered its own copies — initially with
slightly different usage strings (e.g. "Per-blob size cap" vs scan's "Per-file
size cap"). That is a drift hazard: a default, an enum, or a usage string can
diverge between commands so that what operators reasonably treat as "the same
flag" behaves differently depending on which command they invoke. internal-brief
accepted that duplication deliberately rather than expanding its scope into a
cross-command refactor (the underlying catalog-load and engine-config _logic_
was already shared at the function level, so runtime behavior did not drift —
only the flag _definitions_). internal-brief pays that down.

## Decision

**When a CLI flag is meaningful to more than one command, define it once in a
shared registration helper; do not reimplement the same flag in multiple
places.**

- The shared catalog/posture flag set is registered by
  `registerScanCatalogFlags(cmd *cobra.Command)` in
  `internal/cmd/flags.go`. It is the single source of truth for those flags'
  names, defaults, usage strings, and the package-level vars they bind to.
  `scan` (the canonical definition) and `audit-publish` both call it.
- A flag-parity test (`TestSharedScanCatalogFlags_ParityAcrossCommands`)
  asserts the shared flags are byte-identical (name, default, usage) across the
  consuming commands, so drift fails CI rather than shipping.
- Command-specific flags stay local to their command (e.g. `scan --diff-base`,
  `audit-publish --remote` / `--names-only` / `--danger-pattern`). The helper
  covers only the genuinely-shared set.

### The boundary: centralize what is the _same_, not what merely shares a name

The principle is "when appropriate." A command whose flag has genuinely
different semantics is **not** folded in, because unifying it would change its
behavior:

- **`attest`** registers `--catalog`, `--config-file`, `--visibility`, `--mode`
  too, but with different contracts: its `--mode` defaults to `local` (not the
  empty default scan uses), its `--config-file` is reserved/unsupported, and
  its `--catalog` describes the catalogs used _for the attested scan_. Forcing
  it onto the shared helper would silently change those defaults and help text.
  `attest` therefore keeps its own registration; reconciling it (e.g. via an
  options-bearing helper variant) is future work only if its semantics
  converge.

Sharing storage across commands is safe because at most one command runs per
process invocation; the bound vars are populated by whichever command parsed.

## Consequences

- **Positive**: one place to change a shared flag; impossible for `scan` and
  `audit-publish` to drift on the shared set (test-enforced); new commands that
  need the set opt in with one call.
- **Neutral**: `audit-publish`'s `--workers`/`--max-file-size` usage strings now
  read with scan's canonical wording rather than their internal-brief phrasing — a
  cosmetic `--help` unification, no behavior change.
- **Negative / accepted**: the helper binds to `scan*` package globals, so it
  is not reusable by a command that wants independent storage without
  refactoring to an options struct. That is acceptable for the current command
  set and revisited only if a third consumer with different storage needs
  appears.

## References

- internal-brief (this change) — `internal/cmd/flags.go`, `flags_test.go`.
- internal-brief (`audit-publish`) — introduced the second consumer and the accepted
  duplication.
- `CONTRIBUTING.md` §Scan CLI contract — the flag/exit contract these commands
  share.

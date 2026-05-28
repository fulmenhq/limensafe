# Contributing to limensafe

Thanks for your interest in working on limensafe. This guide covers the
mechanics: build/test/lint, the scan CLI contract that users depend on,
commit attribution, and the v0 → handoff release sequencing.

> **Ownership note.** Through v0.0.3, limensafe is shepherded by
> cxotech with entarch covering release
> signing. From v0.0.4 onward, the **the maintainer team** (DX-tools cluster) owns
> the repo. See [`MAINTAINERS.md`](MAINTAINERS.md).

## Quick start

```bash
git clone git@github-for-3leapsdave-fulmen:fulmenhq/limensafe.git
cd limensafe
make bootstrap     # installs go tooling deps + hooks
make build         # builds bin/limensafe
make test          # full Go test suite
make check-all     # fmt + verify-embedded-identity + verify-version-alignment + lint + test
```

`make install` drops the binary at `~/.local/bin/limensafe` (or
`$USERPROFILE/bin/limensafe.exe` on Windows). Use that for hooks and
local smoke tests against other repos.

## Scan CLI contract (DO NOT BREAK)

The `scan` subcommand is the integration surface. CI wrappers (Make
recipes, pre-commit hooks, GitHub Actions) depend on these guarantees.

### Exit codes

| Code | Meaning                                                    | Sentinel error (`internal/cmd/scan.go`) |
| ---- | ---------------------------------------------------------- | --------------------------------------- |
| `0`  | scan succeeded; no findings at or above block threshold    | (success — no error returned)           |
| `1`  | scan succeeded; one or more findings have `decision=block` | `ErrFindingsBlocked`                    |
| `2`  | config / catalog validation error                          | `ErrConfigInvalid`                      |
| `3`  | runtime / I/O error                                        | `ErrRuntime`                            |

Dispatched in `cmd/limensafe/main.go` via `errors.Is`. When introducing
new error paths inside the scan flow, wrap with the right sentinel:

```go
// Config-shaped error (input was bad: flag combo, YAML parse, missing catalog)
return fmt.Errorf("%w: load config %s: %w", ErrConfigInvalid, path, err)

// Runtime-shaped error (I/O, extractor init, stdin/stdout failure)
return fmt.Errorf("%w: init extractor: %w", ErrRuntime, err)
```

Multi-`%w` (Go 1.20+) preserves both the sentinel-for-dispatch and the
underlying cause for diagnostics. Plain `fmt.Errorf` without a sentinel
falls through to the generic-failure exit code (`1` via foundry) — this
is intentionally the safety net for errors outside the scan flow
(health, doctor, serve, etc.).

### Output streams

| Stream   | Content                                                                                                                                                                   |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `stdout` | Scan-result JSON, always. Valid JSON even on exit `1`. Never mixed with log lines.                                                                                        |
| `stderr` | Diagnostics and progress. Empty in non-verbose happy-path runs. Verbose (`-v`) writes DEBUG/INFO. Fatal errors emit a one-line `config error:` / `runtime error:` prefix. |

CI wrappers tee/archive stdout JSON cleanly. Tests in
`test/integration/scan_exit_codes_test.go::TestScanOutputStreamContract`
assert no stream leakage.

### Adding new scan errors

When you add a new error path inside `internal/cmd/scan.go`:

1. Decide whether it's config-shaped (user provided bad input) or
   runtime-shaped (the OS or one of our subsystems failed).
2. Wrap with `ErrConfigInvalid` or `ErrRuntime` accordingly.
3. Add a subtest to `TestScanExitCodeContract` for the new path.
4. If the error has a new failure mode for stdout/stderr separation
   (e.g., partial writes before failure), update
   `TestScanOutputStreamContract`.

## Build, test, lint

| Target                            | What it does                                                                                |
| --------------------------------- | ------------------------------------------------------------------------------------------- |
| `make build`                      | builds `bin/limensafe` for the current platform                                             |
| `make build-all`                  | builds 5-platform dev binaries to `bin/` (release path is `make release-build`)             |
| `make test`                       | full Go test suite, including integration                                                   |
| `make test-cov`                   | tests with coverage                                                                         |
| `make lint`                       | `golangci-lint` + project rules                                                             |
| `make fmt`                        | gofmt + goimports + project formatters                                                      |
| `make verify-embedded-identity`   | confirms `.fulmen/app.yaml` matches `internal/assets/appidentity/app.yaml`                  |
| `make verify-version-alignment`   | confirms `VERSION`, `.fulmen/app.yaml`, embedded copy all agree                             |
| `make bootstrap-smoke`            | end-to-end CLI smoke (5 checks per partner-integration devlead spec)                               |
| `make perf-smoke`                 | scans a large local repo and prints timings (requires `PERF_SMOKE_ROOT`)                    |
| `make check-all`                  | full quality gate — fmt + verify-embedded-identity + verify-version-alignment + lint + test |
| `make precommit` / `make prepush` | hook entrypoints (also run version-alignment)                                               |

`check-all` is the gate for landing changes. CI re-runs it on every push.

### Version-alignment discipline

`VERSION`, `.fulmen/app.yaml`, and `internal/assets/appidentity/app.yaml`
must agree. Edit `VERSION` only via `make version-set VERSION=X.Y.Z` or
`make version-bump-{patch,minor,major}` — these call
`scripts/sync-version.sh` which propagates atomically. Manual edits to
the YAMLs will fail `make verify-version-alignment` at precommit/prepush.

## Hooks

The repo ships with `make hooks-ensure` which wires:

- `pre-commit` → `make precommit` (format + lint + version alignment)
- `pre-push` → `make prepush` (license audit + lint/security + version alignment + embedded identity)

Hooks live under `goneat`-managed paths; see `Makefile` for the
canonical wiring.

## Commit attribution (3 Leaps standard)

All AI-authored commits MUST use 3 Leaps attribution conventions —
**never** an external no-reply domain.

### Required trailers

```
<type>(<scope>): <subject line>

<body — what and why>

Co-Authored-By: <Model display name> <noreply@3leaps.net>
Role: <role>
```

Optional but encouraged:

- `Generated by <Model> via <Interface> under supervision of @3leapsdave`
  on its own line above the trailers (for transparency about generation
  source).
- `Committer-of-Record: Dave Thompson <dave.thompson@3leaps.net> [@3leapsdave]`
  for accountability on supervised commits.
- `Reviewed-by: <role>` when another agent reviewed before commit.
- `Integrated-By: <bot-handle>` when one agent commits another agent's
  work (e.g., maintainer applying a patch).

### Commit message style

Use the conventional-commits prefix (`feat`, `fix`, `chore`, `docs`,
`refactor`, `test`, `ci`, etc.). Keep the subject under 72 chars.
Body wraps at 72. List concrete changes in a `Changes:` block when the
commit touches several distinct files or surfaces.

Examples are in the git log — `git log --oneline -20` shows the
established cadence.

### Limensafe self-scan attestation (interim)

Until internal-brief ships, the limensafe-on-limensafe discipline is
honor-system via a commit-message trailer. **Before pushing**:

1. Run `limensafe scan` against the repo per the standard workflow:

   ```
   limensafe scan . --catalog <path-to-catalog-file> --visibility public_oss
   ```

   `<path-to-catalog-file>` is a direct path to a local catalog YAML
   file you control — typically a private organization catalog (e.g.,
   `$HOME/.config/limensafe/profiles/fulmenhq.yaml`) or a temp
   catalog per the README's CI Integration Patterns. Note: `--catalog`
   takes a direct file path; the `profile` source kind that would
   resolve a profile name to a file is internal-brief territory and not yet
   implemented. The vendored public-baseline catalog
   (`pkg/catalog/builtin/public-baseline.yaml`) is also acceptable for
   sentinel-marker coverage. A `--staged` scan is the most rigorous
   pre-push variant.

2. Confirm exit code 0 (or, if pre-existing fixture noise is expected,
   exit 1 with the noise sources documented in the PR body).

3. Add a `Limensafe-Scan:` trailer to your final pre-push commit:

   ```
   Limensafe-Scan: 2026-05-23T13:42:00Z catalog=fulmenhq-private exit=0
   ```

   Format: `Limensafe-Scan: <ISO 8601 UTC timestamp> catalog=<tag> exit=<0|1>`.
   `<catalog-tag>` is a human-friendly label (e.g., `fulmenhq-private`,
   `public-baseline-only`); NEVER include actual catalog content.

The trailer is honor-system but auditable in `git log`. When internal-brief
ships, the trailer pattern carries forward — the trailer can reference
the attestation file or be replaced entirely.

## Release process

Releases follow [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md). High
level:

1. `make check-all` clean on `main`
2. `make verify-version-alignment` passes
3. `make bootstrap-smoke` passes (end-to-end CLI proof)
4. Tag — `git tag -a v<version> -m "..."` and push
5. CI publishes a **draft** GitHub release with 6-platform binaries +
   SHA256SUMS/SHA512SUMS
6. Sign locally per goneat's canonical signing flow (
   [`~/dev/goneat/RELEASE_CHECKLIST.md`](../goneat/RELEASE_CHECKLIST.md)
   ) — keys live at `$HOME/.minisign/fulmenhq-release.{key,pub}` and
   `security@fulmenhq.dev` in `$HOME/.gnupg/`
7. Upload signatures + public keys as provenance assets via
   `make release-upload`
8. Flip draft → published in the GitHub UI

CI signing (vs manual) is a future automation enhancement — see
`.github/workflows/release.yml` header comment for the wiring path.

## Issue, PR, and review norms

- Branch naming: `feat/<short-name>`, `fix/<short-name>`, `chore/<short-name>`.
  No client-specific or codename branches.
- One concern per PR. Coordinate larger work-streams via
  `the internal coordination channel` (the persistent ops channel) or a brief-specific
  `the brief channel` channel.
- PRs against `main` require `make check-all` green and a one-line
  rationale for any deferral (e.g., feature flagged behind v0.0.4).
- Reviewer cadence: at least one agent-devrev review for non-trivial
  surfaces; cxotech/entarch/the maintainer team-devlead self-merge for chores during
  v0 bootstrap (post-v0.0.3 we tighten to one-approval-required).

## Where to read next

- [`README.md`](README.md) — user-facing overview, CI integration
  patterns, scan contract.
- [`docs/architecture/`](docs/architecture) — engine, extractor,
  catalog, output module designs.
- [`docs/decisions/`](docs/decisions) — ADRs (zero-leak invariant,
  ID-safety rule, two-layer catalog, etc.).
- [`MAINTAINERS.md`](MAINTAINERS.md) — current ownership and contacts.
- [`HANDOFF.md`](HANDOFF.md) — architecture tour, open decisions with
  rationale, beta-tester relationships, backlog priorities (landing
  with v0.0.3).

# limensafe — Testing Guide

How the test suite is organized and how to run it. Companion to
[`CONTRIBUTING.md`](CONTRIBUTING.md) (build/lint/scan-contract) and
[`AGENTS.md`](AGENTS.md) (operational discipline).

## Quick reference

```bash
make test              # full Go test suite, including integration
make test-cov          # tests + coverage report
make check-all         # full quality gate (fmt + verify-* + lint + test)
make bootstrap-smoke   # end-to-end CLI smoke (5 checks per partner-integration spec)
make perf-smoke        # scan a large local repo and print timings
                       # (skips with a warning unless PERF_SMOKE_ROOT is set)
```

Targeted test runs:

```bash
# A single test in a single package
go test -v -run TestRedactor_ZeroLeak_SyntheticAcmeAliases ./pkg/output/...

# Whole package
go test -v ./pkg/engine/...

# Integration tests only
go test -v ./test/integration/...
```

## Test suite layout

Tests live alongside the code they cover (Go convention). Integration
tests that span multiple packages live under `test/integration/`.

```
.
├── internal/
│   ├── appid/                 appid_test.go              identity wrapper
│   ├── cmd/                   appidentity_test.go        cobra appidentity helpers
│   ├── config/                loader_test.go             config layering
│   │                          schema_flavors_test.go     schema variant handling
│   ├── observability/         gofulmen_test.go           logger integration
│   └── server/                multiple                   HTTP server (Q1 in HANDOFF.md)
├── pkg/
│   ├── catalog/               catalog_test.go            catalog loader
│   │                          config_test.go             config-file parsing
│   ├── engine/                engine_test.go             detection unit tests
│   ├── extractor/             filesystem_test.go         filesystem walk
│   │                          staged_test.go             git-staged extraction
│   └── output/                json_formatter_test.go     JSON output
│                              redactor_test.go           Redactor (zero-leak)
├── test/integration/          metrics_test.go            Prometheus metrics
│                              scan_exit_codes_test.go    contract lock (see below)
│                              standalone_binary_test.go  binary smoke
└── testdata/
    ├── builtin-baseline/      Vendored public-baseline catalog
    ├── schemas/               JSON Schema fixtures
    └── synthetic-acme/        T1–T9 acceptance corpus (acme/horizon/tilden placeholders)
```

## Contract tests (DO NOT BREAK)

These tests lock external contracts that adopters (CI wrappers,
pre-commit hooks, downstream integrations like DataWidget partner-integration)
depend on. Changes to the locked behavior require updates here
plus updates to [`CONTRIBUTING.md`](CONTRIBUTING.md#scan-cli-contract-do-not-break).

### `TestScanExitCodeContract` — `test/integration/scan_exit_codes_test.go`

Asserts the 4-way exit-code contract:

| Code | Meaning                                   | Sentinel error       |
| ---- | ----------------------------------------- | -------------------- |
| `0`  | scan succeeded; no findings ≥ threshold   | (no error)           |
| `1`  | scan succeeded; one or more `block`-level | `ErrFindingsBlocked` |
| `2`  | config / catalog validation error         | `ErrConfigInvalid`   |
| `3`  | runtime / I/O error                       | `ErrRuntime`         |

Add a subtest here when introducing a new error path inside the scan
flow.

### `TestScanOutputStreamContract` — `test/integration/scan_exit_codes_test.go`

Asserts the stdout/stderr separation:

- `stdout`: scan-result JSON, always; valid JSON even on exit `1`; never
  mixed with log lines
- `stderr`: diagnostics, progress, fatal `config error:` / `runtime
error:` one-line prefixes

Add a subtest here when introducing a new path that emits to either
stream.

### `TestRedactor_ZeroLeak_SyntheticAcmeAliases` — `pkg/output/redactor_test.go`

Asserts the zero-leak invariant ([ADR-0003](docs/decisions/ADR-0003-redaction-safe-output.md))
holds against the synthetic-acme alias set: every emitted byte across
stdout/stderr/JSON for every input file in the corpus contains zero
protected substrings.

This is the load-bearing test for the Redactor. If you change the
Redactor, the JSONFormatter, or any emit path, this test must continue
to pass.

## Acceptance corpus (T1–T9)

`testdata/synthetic-acme/` is the v0 acceptance corpus with synthetic
placeholder vocabulary:

- `acme` — client placeholder
- `horizon` — codename placeholder
- `tilden` — sanctioned codename (`allowed_in: [engagement_private, internal]`)

The T1–T9 acceptance tests cover the design invariants (literal
detection, slug variants, path-segment match, regex, co-occurrence,
allowed-in scope, fingerprint stability, redactor zero-leak,
exit-code contract). They run as part of `make test`; see the
HANDOFF.md architecture tour for the conceptual map.

**Treat the synthetic-acme corpus as the regression bar.** If you touch
the engine or extractor, run `go test -v ./pkg/engine/... ./pkg/output/...`
plus `go test -v ./test/integration/...` and confirm all of the above
continue to pass.

## CLI smoke tests

### `make bootstrap-smoke`

End-to-end CLI smoke per the partner-integration devlead spec. Runs the
freshly-built `bin/limensafe` through five checks:

1. `limensafe version` — outputs the expected version string
2. `limensafe scan --help` — documents the locked exit codes
3. `limensafe scan testdata/builtin-baseline` — clean scan, exit `0`
4. `limensafe scan testdata/synthetic-acme/leaky-fixtures --catalog testdata/synthetic-acme/catalog.yaml` —
   finds expected findings, exit `1`
5. `limensafe scan /nonexistent-path` — config error, exit `2`

Driver: [`scripts/bootstrap-smoke.sh`](scripts/bootstrap-smoke.sh).
Run before any release tag and after CLI-surface changes.

### `make perf-smoke`

Scans a large local repo and prints per-stage timings. Requires
`PERF_SMOKE_ROOT` to point at a real repo (e.g.,
`PERF_SMOKE_ROOT=~/dev/gohugoio/hugo make perf-smoke`); warn-skips
otherwise.

Useful for catching catalog-size regressions before adopters notice.

## Verify-\* gates

These are not tests in the `go test` sense but contract checks that
run as part of `make check-all`:

- **`make verify-version-alignment`** — `VERSION`, `.fulmen/app.yaml`,
  and `internal/assets/appidentity/app.yaml` must agree. Drift blocks
  precommit/prepush. Use `make version-set VERSION=X.Y.Z` (atomic) to
  bump versions.
- **`make verify-embedded-identity`** — `internal/assets/appidentity/app.yaml`
  must match `.fulmen/app.yaml`. Use `make sync-embedded-identity` to
  resync after editing the source.

## Test conventions

### Naming

- Test files: `*_test.go` alongside the package
- Test functions: `TestFeature` or `TestFeature_SpecificCase` for
  named cases; subtests via `t.Run("description", ...)` for
  table-driven scenarios

### Package naming

- White-box tests (access to unexported names): `package <name>`
- Black-box tests (consume only the public API): `package <name>_test`

### Structure

```go
func TestFeature(t *testing.T) {
    t.Run("specific case", func(t *testing.T) {
        // setup
        // execute
        // verify
        if got != want {
            t.Errorf("got %v, want %v", got, want)
        }
    })
}
```

### Adding tests

- Every behavior change ships with a test. No exceptions.
- For scan CLI changes, add a subtest to `TestScanExitCodeContract`
  and (if streams change) `TestScanOutputStreamContract`.
- For redaction-adjacent changes (new emit path, new output format,
  catalog change), confirm `TestRedactor_ZeroLeak_SyntheticAcmeAliases`
  still passes.
- For new acceptance scenarios, add a fixture to
  `testdata/synthetic-acme/` and a corresponding T-numbered test
  reference in the test name.

## CI

GitHub Actions ([`.github/workflows/ci.yml`](.github/workflows/ci.yml))
runs on every push and PR to `main`:

- `make check-all` (full quality gate)
- `make bootstrap-smoke` (CLI smoke)
- 5-platform release build dry-run (via `make build-all`)

Release builds ([`.github/workflows/release.yml`](.github/workflows/release.yml))
trigger on `v*` tag push; produce 6-platform binaries + checksum
manifests; publish a draft GitHub Release for the manual signing flow.

**Required**: all CI checks must pass before merge to `main`.

## Troubleshooting

### Tests fail after dependency update

1. Check `go.mod` for unexpected version drift
2. `go mod tidy` to normalize
3. Clear module cache if needed: `go clean -modcache`
4. Rebuild from scratch: `make clean && make build && make test`

### `TestScanExitCodeContract` fails after CLI change

Most common cause: a new error path wraps with `fmt.Errorf` directly
instead of with `ErrConfigInvalid` or `ErrRuntime`. See
[`CONTRIBUTING.md` §Scan CLI contract](CONTRIBUTING.md#scan-cli-contract-do-not-break)
for the wrapping pattern.

### `TestRedactor_ZeroLeak_SyntheticAcmeAliases` fails

Something emits user content without routing through the Redactor.
Search for new `fmt.Println`, `log.Printf("%s", ...)`, or output
templates that take user input directly. Route through the Redactor
or emit opaque IDs / counts only.

### `verify-embedded-identity` fails at precommit

`.fulmen/app.yaml` was edited without resyncing the embedded copy.
Run `make sync-embedded-identity` and re-stage.

### Server tests fail or flake

The HTTP server under `internal/server/` is the workhorse-template
inheritance that is **not currently exposed via the CLI** — see
HANDOFF.md Q1 (open question: keep / strip / refactor into a
companion). If the server tests are flaking and you are not actively
working on the server, the safe action is to leave them alone and
flag the flake in `the internal coordination channel`.

## Critical tests

Tests that must never break (rerun on every commit that touches the
adjacent surface):

| Test                                         | Location                                   | Why critical                             |
| -------------------------------------------- | ------------------------------------------ | ---------------------------------------- |
| `TestScanExitCodeContract`                   | `test/integration/scan_exit_codes_test.go` | External integration contract            |
| `TestScanOutputStreamContract`               | `test/integration/scan_exit_codes_test.go` | External integration contract            |
| `TestRedactor_ZeroLeak_SyntheticAcmeAliases` | `pkg/output/redactor_test.go`              | Zero-leak invariant (ADR-0003)           |
| `TestGet_EmbeddedIdentityWinsOverForeignCWD` | `internal/appid/appid_test.go`             | Locks the gofulmen v0.3.5 precedence fix |
| Engine `TestScanUnit*`                       | `pkg/engine/engine_test.go`                | Core detection correctness               |

## References

- [`CONTRIBUTING.md`](CONTRIBUTING.md) — Scan CLI contract, commit standard
- [`docs/decisions/ADR-0003-redaction-safe-output.md`](docs/decisions/ADR-0003-redaction-safe-output.md) — Zero-leak invariant
- [`HANDOFF.md`](HANDOFF.md) — Architecture tour, open questions
- [`AGENTS.md`](AGENTS.md) — Operational discipline (DO / DO NOT)
- [`scripts/bootstrap-smoke.sh`](scripts/bootstrap-smoke.sh) — End-to-end CLI smoke driver

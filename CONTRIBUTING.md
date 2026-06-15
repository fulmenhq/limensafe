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
make fmt           # mutating formatter for the dev fix loop
make format-check  # verify-only formatter; matches CI format-check
make check-all     # format-check + verify-embedded-identity + verify-version-alignment + lint + test
make prepush       # CI-aligned local pre-push gate
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

| Stream   | Content                                                                                                                                                                                                                     |
| -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `stdout` | Scan-result JSON, always. Valid JSON even on exit `1`. Never mixed with log lines.                                                                                                                                          |
| `stderr` | Diagnostics and progress. Empty in non-verbose happy-path runs without skips. Skip events, catalog hygiene warnings, verbose (`-v`) DEBUG/INFO, and fatal error prefixes are written here; skip paths/details are redacted. |

CI wrappers tee/archive stdout JSON cleanly. Tests in
`test/integration/scan_exit_codes_test.go::TestScanOutputStreamContract`
assert no stream leakage.

Missing optional private catalogs in `warn` posture emit
`kind: "config-warning"` records in stdout JSON. They are deliberately
not stderr diagnostics, do not increment `summary.findings_total`, and
do not trigger exit code `1`.

Detection findings include `entity_id`. This field is an opaque,
redaction-safe catalog entity identifier, not the matched text. Catalog
validation rejects entity IDs that contain protected alias substrings, and
scan startup rejects output-visible IDs that collide with the merged loaded
alias set before any scan output is emitted.

### Scan metadata

Filesystem scans report `scan_metadata.scan_root` as the path argument.
`--git-archive <ref>` scans an extracted temporary copy of the tracked
tree, but stdout never reports that machine-local temp path. Instead,
metadata reports the original ref:

```json
{
  "scan_root": "HEAD",
  "scan_root_kind": "git-archive",
  "git_ref": "HEAD"
}
```

The git-archive path is scan-contract-adjacent: invalid flag combinations
wrap `ErrConfigInvalid` and exit 2; git/archive/tar/tempdir failures wrap
`ErrRuntime` and exit 3.

`--diff --diff-base <ref>` scans only lines introduced by `HEAD` relative
to the merge-base form `<ref>...HEAD`. It reports
`scan_metadata.scan_root_kind: "git-diff"`, `scan_metadata.git_ref:
"<ref>...HEAD"`, and detection findings with
`location.surface_kind: "diff"`. Invalid flag combinations and invalid
base refs are config-shaped errors and exit 2 after redaction.

`--git-history`, `--git-commit-messages`, and `--git-history-all` scan
committed history reachable from all refs. History scans report
`scan_metadata.scan_root_kind: "git-history"` and
`scan_metadata.git_ref: "--all"`, plus `history_blobs_scanned`,
`history_commits_scanned`, and `history_unique_blobs`. Historical blob
findings emit `source_kind: "git_history_blob"` and
`location.surface_kind: "blob"`; historical commit-message findings emit
`source_kind: "git_commit_message"` and
`location.surface_kind: "commit_message"`. In both cases,
`location.git_ref` is the commit SHA, never a blob SHA. History mode
scans unique blobs once, then expands content findings to eligible
commit/path attributions. Historical path-segment findings are generated
per commit/path attribution; they do not co-occur with blob-content
findings. History flags are mutually exclusive with `--staged`,
`--git-archive`, `--diff`, `--branch-name`, and `--commit-msg`.

### Catalog schema contract (JSON Schema)

Vocabulary catalogs are pinned structurally by a published JSON Schema:
[`schemas/limensafe/v1/catalog.schema.json`](schemas/limensafe/v1/catalog.schema.json).
The hosted URI is
`https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json`, which is
the value catalog authors should put in top-level `$schema`.

This schema validates **shape only**: required fields, allowed structural
objects, enum values, `variants.whole_word`, and co-occurrence window
requirements. Loader/linter code remains responsible for semantic checks
that require catalog context, including alias-safe IDs, duplicate IDs,
regex compilation, co-occurrence term referential integrity, broad-alias
hygiene, and redaction-safe diagnostics.

`schema_version` is required and must be v1 semver. Missing `$schema`
catalogs remain valid during the v0.1.x compatibility window so existing
catalogs can load with a warning; v0.2.0 is expected to make `$schema`
mandatory. Major-version mismatches are config-shaped failures and should
map to scan exit code 2 when runtime validation is wired.

### Scan output contract (JSON Schema)

The full stdout document — `version`, `scan_metadata`, `summary`, and
`findings[]` — is pinned by a published, versioned JSON Schema:
[`schemas/limensafe/v1.0.0/scan-output.schema.json`](schemas/limensafe/v1.0.0/scan-output.schema.json).
This is the contract adopters parse (CI wrappers, `jq` aggregations,
partner-integration). It carries the same **do not break without versioning** weight as
the exit-code/stream contract above: a field rename, retype, or enum change
breaks downstream consumers.

**Version discriminator.** Three version-ish signals appear in the output;
exactly one is authoritative for "which output shape am I parsing?":

| Field                                 | Meaning                                      | Use as parse discriminator? |
| ------------------------------------- | -------------------------------------------- | --------------------------- |
| `scan_metadata.output_schema_version` | Semver of the output contract (`1.0.0`)      | **Yes — authoritative**     |
| `version` (top-level)                 | Coarse generation marker (`v0`), back-compat | No                          |
| `scan_metadata.tool_version`          | CLI build version; moves independently       | No                          |

`output_schema_version` maps to the schema directory (`v1.0.0/`). Any
additive/breaking field change bumps it per semver; the constant lives at
`output.SchemaVersion`.

**Nullability — present-with-zero, not omitted/null.** The core scan
counters (`worker_count`, `files_scanned`, `bytes_scanned`, `files_skipped`,
`directories_skipped`) and `files_skipped_by_reason` are always emitted —
`0` and `{}` rather than absent — so a `jq`/CI reader never has to
distinguish "zero" from "missing". `summary.by_severity` / `by_surface`
likewise emit `{}` when empty. Mode-specific fields (`scan_root_kind`,
`git_ref`, `history_*`, `private_catalogs_status`) stay omitted when not
applicable — that omission is intentional and is part of the contract.

**Skip accounting (internal-brief).** `files_skipped` is the **stable total of
file units not scanned**, including files behind a directory that was
pruned wholesale. A pruned directory increments `directories_skipped += 1`
**and** `files_skipped += <files behind it>` **and**
`files_skipped_by_reason["ignored"] += <files behind it>`; an empty ignored
directory reports `directories_skipped: 1`, `files_skipped += 0`.
`directories_skipped` is an **additional structural roll-up, never a
substitute** for the file count, and `files_skipped_by_reason` always sums
to `files_skipped`. A consumer reads one integer (`files_skipped`) for
"how many files were not scanned" regardless of tree shape. The matching
stderr directory skip event carries a `files=<n>` count for reconciliation
— **counts only; the pruned descendant paths are never enumerated**, since
that subtree may hold protected vocabulary that was deliberately never
scanned (zero-leak invariant, ADR-0003).

**Enum / `additionalProperties` policy.** Fixed objects set
`additionalProperties: false`; only the genuine count maps (`by_severity`,
`by_surface`, `files_skipped_by_reason`) carry dynamic keys. Enums are
closed and grounded in emitted values for the engine-controlled fields
(`severity`, `decision`, `confidence`, `source_kind`, `surface`,
`surface_kind`, `visibility`, `load_status`, `kind`). Catalog-driven or
detector-derived fields (`entity_class`, `detector_id`, `evidence_shape`)
stay open strings — they evolve with catalogs, not the schema.

**Enforcement.** `make meta-validate-schemas` checks the schema is itself
valid; `TestScanOutputSchemaContract`
(`test/integration/scan_output_schema_test.go`) validates real emitted
documents (clean, blocking, skip-heavy) plus a maximal document covering
every enum against the schema. Because the maximal document populates every
field and the fixed objects are closed, **a Go `pkg/output` struct change
that drifts from the schema fails CI**. When you change the output shape:

1. Update `pkg/output` and the schema together.
2. Bump `output.SchemaVersion` (and add a new `schemas/limensafe/<ver>/`
   directory for a breaking change) per semver.
3. Update the maximal document / fixtures in the conformance test.
4. Update this section.

The fingerprint composition (`finding.fingerprint`) is locked in code and
tests, not the schema — a schema can constrain shape but cannot prove a
hash. Don't change fingerprint inputs without updating the fingerprint
tests.

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

| Target                          | What it does                                                                                                                                                       |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `make build`                    | builds `bin/limensafe` for the current platform                                                                                                                    |
| `make build-all`                | builds 5-platform dev binaries to `bin/` (release path is `make release-build`)                                                                                    |
| `make test`                     | full Go test suite, including integration                                                                                                                          |
| `make test-cov`                 | tests with coverage                                                                                                                                                |
| `make lint`                     | `golangci-lint` + project rules                                                                                                                                    |
| `make fmt`                      | mutating formatter for the local fix loop                                                                                                                          |
| `make format-check`             | verify-only formatter; matches CI's `goneat format --check`                                                                                                        |
| `make verify-embedded-identity` | confirms `.fulmen/app.yaml` matches `internal/assets/appidentity/app.yaml`                                                                                         |
| `make verify-version-alignment` | confirms `VERSION`, `.fulmen/app.yaml`, embedded copy all agree                                                                                                    |
| `make bootstrap-smoke`          | end-to-end CLI smoke (5 checks per partner-integration devlead spec)                                                                                                      |
| `make perf-smoke`               | scans a large local repo and prints timings (requires `PERF_SMOKE_ROOT`)                                                                                           |
| `make check-all`                | fast quality gate — format-check + verify-embedded-identity + verify-version-alignment + lint + test                                                               |
| `make prepush`                  | local pre-push gate aligned with CI: scan attestation verification, format-check, mutating fmt + diff check, lint, test, build, standalone binary, bootstrap smoke |
| `make pr-final`                 | final PR validation: prepush plus format-check negative fixture and version/identity verification                                                                  |

Use `make fmt` when you want the toolchain to rewrite files, then stage the
result. Use `make format-check`, `make check-all`, or `make prepush` when
green must mean "no formatter changes are pending." This split matters because
CI runs `goneat format --check`, which fails instead of rewriting files.
`make prepush` is the local gate to run before pushing; `make pr-final` is the
final gate before requesting review.

### Version-alignment discipline

`VERSION`, `.fulmen/app.yaml`, and `internal/assets/appidentity/app.yaml`
must agree. Edit `VERSION` only via `make version-set VERSION=X.Y.Z` or
`make version-bump-{patch,minor,major}` — these call
`scripts/sync-version.sh` which propagates atomically. Manual edits to
the YAMLs will fail `make verify-version-alignment` at precommit/prepush.

## Hooks

The repo ships with `make hooks-ensure` which wires:

- `pre-commit` → `make precommit` (format + lint + version alignment)
- `pre-push` → `make prepush` (CI-aligned local gates)

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

limensafe-on-limensafe proof is file-based. **Before pushing**:

1. Commit the source and documentation changes you intend to push.

2. Run:

   ```bash
   make limensafe-attest
   ```

   This runs `limensafe attest` with the public-baseline catalog and
   writes `.limensafe/scan-attestation.json` after a successful
   `scan --diff --diff-base ${LIMENSAFE_DIFF_BASE:-origin/main}`.
   `attest` currently requires explicit `--catalog` paths; it rejects
   `--config-file` until resolver-backed private catalog content hashing
   exists.

3. Commit only `.limensafe/scan-attestation.json`:

   ```bash
   git commit -m "chore(attest): refresh limensafe scan attestation"
   ```

4. Run:

   ```bash
   make limensafe-verify
   ```

The verifier reads the attestation from the committed blob at
`HEAD:.limensafe/scan-attestation.json`, not from the working tree.
It accepts `commit_sha == HEAD`, or `commit_sha == HEAD~1` only when
`HEAD` changes only the attestation file. This avoids the impossible
self-reference while still proving no source or docs changed after the
scan.

For release/tag preparation, `make release-prepare` runs
`limensafe verify-attestation --mode tag`. Tag mode uses a 60-minute
freshness window and checks `catalog_id_hash` against
`.limensafe/known-catalog-hashes.txt`, unless
`LIMENSAFE_RELEASE_CATALOG_OK=1` is deliberately set for an approved
alternate catalog.

## Release process

Releases follow [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md). High
level:

1. `make prepush` clean on `main`
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
- PRs against `main` require `make prepush` green, `make pr-final` before
  final review, and a one-line rationale for any deferral (e.g., feature
  flagged behind v0.0.4).
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

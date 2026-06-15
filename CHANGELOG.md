# Changelog

All notable changes to limensafe are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and limensafe adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
For the forward-looking plan see [`docs/roadmap.md`](docs/roadmap.md).

## [Unreleased]

### Added

- **Catalog JSON Schema contract (internal-brief).**
  `schemas/limensafe/v1/catalog.schema.json` now pins the structural shape
  of vocabulary catalogs: top-level identity/version fields, entities,
  variants including `whole_word`, regex-backed entities, visibility/severity
  enums, and co-occurrence rule windows. The schema is draft 2020-12, uses
  the hosted URI `https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json`,
  and is covered by meta-validation plus fixture conformance tests against the
  built-in and synthetic catalogs. It deliberately validates structure only;
  alias-safety, regex compilation, referential integrity, and semantic hygiene
  remain loader/linter responsibilities.
- **Published, versioned scan output JSON Schema (internal-brief).**
  `schemas/limensafe/v1.0.0/scan-output.schema.json` now pins the full
  stdout document (`version`, `scan_metadata`, `summary`, `findings[]`) —
  the surface adopters parse (CI wrappers, `jq`, partner-integration). Fixed objects are
  closed (`additionalProperties: false`); only the genuine count maps carry
  dynamic keys; enums are closed for engine-controlled fields and left open
  for catalog/detector-driven ones. The contract is versioned by a new
  authoritative discriminator, `scan_metadata.output_schema_version`
  (`1.0.0`), distinct from the coarse top-level `version` and the
  independently-moving `tool_version`. `make meta-validate-schemas` covers
  the schema and `TestScanOutputSchemaContract` validates real emitted
  documents (clean / blocking / skip-heavy) plus a maximal all-enum
  document against it, so a `pkg/output` struct change that drifts from the
  schema fails CI. Documented in CONTRIBUTING §"Scan output contract".

### Changed

- **Stable, reconcilable `.limensafeignore` skip accounting (internal-brief).**
  `scan_metadata.files_skipped` is now the stable total of file units not
  scanned **regardless of tree shape** — a wholesale directory prune folds
  the files it represents into `files_skipped` and
  `files_skipped_by_reason["ignored"]` instead of hiding them behind a bare
  `directories_skipped` count. `directories_skipped` remains an additional
  structural roll-up (never a substitute), `files_skipped_by_reason` always
  sums to `files_skipped`, and the matching stderr directory skip event
  carries a `files=<n>` count for reconciliation (counts only — pruned
  descendant paths are never enumerated, per the zero-leak invariant).
- **Core scan counters emit present-with-zero (internal-brief).** `worker_count`,
  `files_scanned`, `bytes_scanned`, `files_skipped`, `directories_skipped`,
  and `files_skipped_by_reason` are now always present (`0` / `{}`) rather
  than omitted via `omitempty`, so `jq`/CI consumers read a stable integer
  or object instead of `null` on a clean scan. Mode-specific fields stay
  omitted by design.

### Fixed

- **`.limensafeignore` skip counters no longer flip shape by scope
  (internal-brief).** Previously the same ignore rule reported skips two
  incompatible ways depending on surrounding tree structure (per-file
  `files_skipped` vs per-directory `directories_skipped` with the file
  count hidden), so a consumer could not read a single reliable
  "files not scanned" total. The directory-prune path now reports the
  files it represents, making both shapes reconcile.

## [v0.1.0] — 2026-06-07

**Theme**: First MVP cut. The full pre-rewrite-remediation surface —
native git-history audit, the diff-introduced-lines gate for PR /
pre-push review, the scan-attestation file that turns the local check
into a verifiable artifact, and a mode-aware private-catalog posture
that matches how operators actually run gates locally vs in CI vs at
release — all land on a single tag. v0.0.x cuts were internal
scaffolding by explicit principle; v0.1.0 is the release v0.0.x was
preparing for.

### Added

- **Native git-history audit surfaces.** `scan --git-history` walks
  unique historical blobs reachable from all refs and reports findings
  deduplicated by blob SHA, then expands hits to every eligible
  commit/path attribution with the commit SHA in `location.git_ref`.
  `scan --git-commit-messages` adds the commit-message surface that
  exposes branch slugs, codenames, and operator notes that never landed
  in any blob. `scan --git-history-all` runs both in a single pass for
  pre-rewrite remediation audits. History scans report
  `history_blobs_scanned`, `history_commits_scanned`, and
  `history_unique_blobs` in scan metadata. History findings carry
  `source_kind: "git_history_blob"` / `surface_kind: "blob"` and
  `source_kind: "git_commit_message"` / `surface_kind: "commit_message"`
  so downstream consumers can distinguish history-derived findings
  from working-tree findings. (PR #18)

- **PR-diff introduced-lines gate.** `scan --diff` with
  `--diff-base <ref>` (default `origin/main`) scans only lines
  introduced by `HEAD` relative to the base ref, so PR and pre-push
  gates do not re-flag pre-existing matches in files touched by the
  change. Diff findings carry `surface_kind: "diff"`. Invalid
  `--diff-base` refs return exit 2 with the protected ref text
  redacted at the scan boundary before stderr emission. (PR #14)

- **Scan-attestation gate.** `limensafe attest` and
  `limensafe verify-attestation` produce and verify a committed
  `.limensafe/scan-attestation.json` proof file plus the
  `Limensafe-Scan:` commit-trailer convention, with push-mode and
  tag-mode verifier semantics. The verifier reads the attestation and
  the known-catalog-hash allowlist from committed `HEAD:` blobs, not
  from the working tree. Parent-bound attestations are accepted only
  when `HEAD` changes exactly `.limensafe/scan-attestation.json`
  (push-mode and tag-mode). Tag-mode known-catalog-hash approval
  comes only from committed `.limensafe/known-catalog-hashes.txt` or
  the documented explicit override `LIMENSAFE_RELEASE_CATALOG_OK=1`.
  Timestamps more than ~5 minutes in the future are rejected as
  malformed. `make limensafe-attest`, `make limensafe-verify`, and
  `make limensafe-verify-tag` wrap the workflow. The public commitment
  around this mechanism — _commit the proof, not the corpus_ — lives
  in [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md) §Final Validation.
  (PR #14; see also
  [ADR-0005](docs/decisions/ADR-0005-scan-attestation-gate.md))

- **Tracked-tree scan surface.** `scan --git-archive <ref>` (default
  `HEAD` when the flag is bare) extracts Git's tracked tree to a
  temporary directory, scans it, removes the temp, and reports
  `scan_root: "HEAD"`, `scan_root_kind: "git-archive"`, and
  `git_ref` in metadata — replacing the manual `git archive | tar -x`
  CI recipe while keeping machine-local temp paths out of stdout /
  stderr. (PR #10)

- **Mode-aware private-catalog posture.** `scan --mode {local|ci|release}`
  selects the default missing-optional-private-catalog posture and (in
  `release` mode) the medium block threshold; `--private-catalog-missing
{silent|warn|error}` explicitly overrides per-invocation. Warn mode
  emits a `kind: "config-warning"` JSON finding without affecting
  detection counts or exit-1 gating; CI and release modes fail closed
  with exit 2. `policy.block_threshold` from repo config is now honored
  by the scanner, including the release-mode macro's medium threshold;
  previously hardcoded. (PR #11)

- **`.limensafeignore` + skip-visibility.** `scan` honors root-level
  `.gitignore` and `.limensafeignore` files for filesystem and
  `--staged` scans. `--include-ignored` disables the matcher for
  deliberate local hygiene scans. Skip diagnostics emit on stderr
  after redaction. Scan metadata adds per-reason file-skip counts and
  ignored directory-prune counts. `.limensafeignore` is documented as
  a hygiene escape hatch, **not a confidentiality boundary** — see
  [`docs/guides/authoring-a-catalog.md`](docs/guides/authoring-a-catalog.md).
  (PR #7)

- **Whole-word matching for catalog aliases.** Catalog entities
  support `variants.whole_word` to require literal matches to be
  bounded by start/end of input or non-word characters. Aliases
  shorter than four characters default to `whole_word: true` unless
  `whole_word: false` is explicitly set — closes the short-acronym
  false-positive class where aliases such as `ILT` matched `built`,
  `split`, `rebuilt`, or random checksum substrings. Catalog loading
  records non-fatal warnings (e.g. `whole_word: true` paired with
  `case_insensitive: true`) and surfaces them through `scan` stderr
  after redaction. (PR #5)

- **`entity_id` in findings + tightened ID-safety.** Detection
  findings include the redaction-safe `entity_id` in the JSON output
  contract, enabling downstream `jq` aggregation by protected entity
  without disclosing matched text. Catalog loading rejects entity IDs
  that contain protected alias substrings. Scan startup checks
  output-visible IDs (catalog IDs, entity IDs, rule IDs, replacement
  IDs) against the merged loaded alias set and refuses to start if
  any collide. (PR #12)

- **Finding `location` adds `surface_kind` and reserves `git_ref`.**
  Finding `location` now includes `surface_kind` (`"working_tree"`,
  `"staged_index"`, `"diff"`, `"blob"`, `"commit_message"`,
  `"branch_name"`) and reserves `git_ref` for git-derived surfaces.
  See **Changed** below for the corresponding fingerprint-input
  change. (PR #14 + PR #18; entarch pre-freeze ask)

- **Developer tooling — verify-only format gates.**
  `make format-check` (verify-only), `make prepush` (CI-aligned
  pre-push), and `make pr-final` (full pre-review quality gate) close
  the recurring CI red where `make check-all`'s auto-fix masked the
  format drift that `goneat format --check` catches in CI.
  `.goneat/assess.yaml` adds explicit format scoping for Markdown,
  JSON, and YAML. (PR #8)

### Changed

- **Output contract — versioned fingerprint shift.** Finding
  fingerprints now include `location.surface_kind` as a normalized
  input. **All v0.1.0 finding fingerprints differ from v0.0.x** —
  not only for the git-derived surfaces introduced in this release.
  Pre-existing working-tree, staged-index, branch-name, and stdin-
  commit-message findings will all compute new fingerprint values
  because `surface_kind` is now part of the hash for every finding.
  Dedupe consumers keying on legacy fingerprints should expect
  cross-surface churn and re-baseline accordingly. The v0.1.0 output-
  contract lock — including the formal `surface_kind` enum and the
  fingerprint-input contract — lands under the planned v0.1.x output
  JSON Schema work.

### Removed

- **`LIMENSAFE_KNOWN_CATALOG_HASHES` env merge in tag-mode
  attestation verification.** The env-var bypass into the tag-mode
  catalog-hash allowlist has been removed. Tag-mode approval now
  reads only the committed `.limensafe/known-catalog-hashes.txt`
  file or the documented explicit override
  `LIMENSAFE_RELEASE_CATALOG_OK=1`. (PR #14; entarch pre-freeze
  pass)

## [v0.0.3] — 2026-05-20

**Theme**: Pre-handoff shakeout. CICD pipeline, release signing, 5-platform
build matrix, CLI exit-code + stream contract implementation, and the
handoff doc slate landing in `main` before the repo transfers to the maintainer team
team stewardship.

### Added

- **5-platform release build** — `make release-build` now produces
  `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`,
  `windows/amd64`, and `windows/arm64` binaries with SHA256SUMS +
  SHA512SUMS manifests. Tag-triggered `release.yml` publishes draft
  GitHub Release for local signing before flip-to-published.
- **Release signing hardening** — minisign now required (was optional
  fall-through); `make release-verify-signatures` is a hard gate on
  upload chains; new `scripts/verify-signatures.sh`; app-prefixed env
  vars (`LIMENSAFE_MINISIGN_KEY`) consistent across all signing
  scripts; TTY guard fix for CI non-interactive contexts. Aligned
  with goneat's canonical fulmenhq signing flow.
- **Bootstrap smoke** — `make bootstrap-smoke` + dedicated CI job
  exercises the 5-check end-to-end CLI contract per partner-integration
  devlead spec: version, health, scan tracked archive, --staged
  scan in temp git fixture, branch+commit-msg stdin surfaces. Catches
  build-passes-tests-but-binary-doesnt-actually-work scenarios.
- **Exit-code contract implementation** — scan subcommand now emits
  exit codes 0/1/2/3 matching the documented contract instead of
  collapsing all errors to 1. Sentinel errors `ErrConfigInvalid` and
  `ErrRuntime` in `internal/cmd/scan.go`; `main.go` dispatches via
  `errors.Is`. Locked by `TestScanExitCodeContract` (7 subtests).
- **Output stream contract** — verified and locked: stdout is always
  pure scan-result JSON; stderr is diagnostics only (empty in
  non-verbose happy-path). Locked by `TestScanOutputStreamContract`
  (3 subtests). CI wrappers can `tee` stdout JSON without filtering.
- **Handoff doc slate** — `HANDOFF.md` (architecture tour, design
  decisions with rationale, beta-tester relationships, backlog),
  `CONTRIBUTING.md` (build/test/lint, scan contract DO-NOT-BREAK,
  commit attribution standard, release process), `docs/roadmap.md`
  (v0.0.3 → v0.0.4 firm → v0.1.0 directional), `MAINTAINERS.md`
  refit (Dave as primary maintainer, the maintainer team listed with
  supervised-agent acknowledgement). `CHANGELOG.md` seeded.
- **Productbook entry** — formalized in
  `the internal productbook/content/projmgmt/limensafe/`
  mirroring datawidget/idpbolt shape.

### Fixed

- **Identity-shadow bug** (partner-integration devlead live-validation
  2026-05-08) — the limensafe binary mis-identified itself as the
  foreign repo's app when run from inside another workhorse's tree.
  Root cause: gofulmen `appidentity.discoverIdentity` put CWD
  ancestor search above the registered embedded identity. Fixed
  upstream in **gofulmen v0.3.5** (2026-05-12) via precedence reorder
  per the coordination memo (`~/dev/gofulmen/internal coordination notes/limensafe/2026-05-08-appidentity-precedence-bug.md`).
  limensafe v0.0.3 pins gofulmen v0.3.5; the brief local workaround
  carried during the v0.0.3 cycle was removed before tag.
  `internal/appid/appid.go` is now a ~10-line wrapper as originally
  designed. Regression test
  `TestGet_EmbeddedIdentityWinsOverForeignCWD` reproduces the
  original symptom and locks the systemic fix.

### Changed

- **CI runner image** — bumped `goneat-tools-runner` from v0.2.1 to
  v0.3.3 (parity with refbolt); `CGO_ENABLED=0` env added (limensafe
  is pure Go by design pillar); GOPATH preparation step added.
- **Format check** — CI no longer treats prettier diffs as
  non-blocking (removed `|| echo` fallback). Format drift fails the
  gate.
- **Signing reference** — `RELEASE_CHECKLIST.md` now points at
  `~/dev/goneat/RELEASE_CHECKLIST.md` as the canonical
  fulmenhq manual-signing flow; uses concrete
  `$HOME/.minisign/fulmenhq-release.{key,pub}` paths and
  `security@fulmenhq.dev` GPG identity.

### Deferred to v0.0.4 (the maintainer team)

- `dogfood-scan` Make target + CI job (depends on `.limensafeignore`)
- `--git-archive HEAD` convenience flag (saves the temp-dir mktemp + trap dance)
- `policy.block_threshold` from repo config (currently hardcoded high|critical → block)
- Workhorse-template HTTP server cleanup (`internal/server/*` not exposed by the CLI)
- CI-side release signing (currently manual per goneat pattern)

## [v0.0.2] — 2026-05-06

**Theme**: First private-remote release. Closed the v0 spike; published
to `fulmenhq/limensafe` (private) as a 18-commit reference workhorse.

### Added

- `kind: builtin` catalog source + vendored `limensafe-public-baseline-v0`
  (sentinel markers + hygiene patterns; opt-in only)
- `--staged` flag + StagedExtractor for sound pre-commit gating (reads
  git index, not working tree)
- Bounded worker pool + `make perf-smoke` (222ms on Hugo's full tree)
- Repo-config loader (`--config-file .limensafe/config.yaml`) with `file`,
  `env`, and `builtin` source kinds plus layered resolution
- T5 stdin surfaces — `--branch-name -` and `--commit-msg -` for
  branch-name and commit-message scanning
- `make install` — drops binary at `~/.local/bin/limensafe` (sfetch-style
  INSTALL_BINDIR with macOS/Linux/Windows handling)
- `make verify-version-alignment` + `scripts/sync-version.sh` —
  VERSION/.fulmen/app.yaml/embedded copy stay aligned atomically;
  drift fails precommit/prepush

### Fixed

- Exit 1 when findings have `decision=block` (per v0-spike-plan §2;
  prior behavior was exit 0 with blocking findings emitted but no gate
  signal)
- Manual `.fulmen/app.yaml` edits during version bump now caught by
  `make verify-version-alignment` gate

### Validated

- All 9 v0 acceptance tests (T1–T9) passing
- Zero-leak invariant verified end-to-end (no protected substring in
  any output stream — ADR-0003)
- ~75 unit tests passing, build clean, CGO=0
- Pre-publication sanitization sweep across docs, fixtures, ADRs

## [v0.0.1] — 2026-04-29

**Theme**: Seed. CDRL'd from `forge-workhorse-groningen` to establish
the limensafe project on `main` (local only at this point).

### Added

- Initial commit from groningen template — workhorse CLI scaffolding,
  Cobra commands (serve, version, health, envinfo, doctor), HTTP
  server with /health, /version, /metrics endpoints, gofulmen logging
  (SIMPLE/STRUCTURED profiles), Prometheus metrics
- `.fulmen/app.yaml` refit for limensafe identity
- Initial design document set (problem-statement, existing-tools-gap,
  catalog-schema, architecture)
- ADR-0003 (redaction-safe-output / zero-leak invariant)
- ADR-0004 (schema-validation multi-draft support)
- First detector pipeline scaffolding (Redactor, JSONFormatter,
  catalog YAML loader, filesystem walker)

### Notes

- Working name was `contextsafe`; renamed to `limensafe` (Latin
  _limen_ = threshold) 2026-05-01 via namelens after discovering
  collisions with a GSAP plugin and a `contextsafe.es` OSS project.

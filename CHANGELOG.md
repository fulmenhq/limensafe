# Changelog

All notable changes to limensafe are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and limensafe adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
For the forward-looking plan see [`docs/roadmap.md`](docs/roadmap.md).

## [Unreleased]

### Added

- Catalog entities now support `variants.whole_word` to require
  literal matches to be bounded by start/end of input or non-word
  characters. Aliases shorter than four characters default to
  whole-word matching unless `whole_word: false` is explicitly set.
- Catalog loading now records non-fatal warnings and surfaces them
  through `scan` stderr after redaction; the first warning flags
  `whole_word: true` paired with `case_insensitive: true`.
- `scan` now honors root-level `.gitignore` and `.limensafeignore`
  files for filesystem and `--staged` scans. `--include-ignored`
  disables the matcher for deliberate local hygiene scans.
- Scan metadata now includes per-reason file skip counts and ignored
  directory prune counts; skip diagnostics are emitted on stderr after
  redaction.

### Fixed

- Short acronym aliases no longer match inside unrelated words or
  dependency-lockfile hashes by default. This suppresses the internal-brief
  false-positive class where aliases such as `ILT` matched `built`,
  `split`, `rebuilt`, or random checksum substrings.

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

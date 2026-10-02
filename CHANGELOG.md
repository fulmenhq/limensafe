# Changelog

All notable changes to limensafe are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and limensafe adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
For the forward-looking plan see [`docs/roadmap.md`](docs/roadmap.md).

## [v0.2.1] — 2026-10-02

### Added

- Release-mode coverage gates for `scan` and `audit-publish`, with
  reason-specific skip ceilings and explicit coverage summaries.
- Signed annotated release-tag tooling, committed public verification
  pins, isolated signature verification, and a signature gate before
  draft release creation.

### Changed

- Scan output schema is **1.2.0**; publish-surface output schema is
  **1.1.0**. Historical schemas remain available. See the
  [v0.2.1 migration notes](docs/releases/v0.2.1.md#migration).
- Release-mode incomplete coverage exits **1**, even without findings.
  Configuration errors remain **2**; fatal runtime/I/O errors retain
  **3** precedence. Normal stdout JSON/stderr diagnostics separation
  is unchanged.
- Updated gofulmen to v0.3.6 and chi to v5.3.2. Removed unconditional
  forwarded-client-IP rewriting from the HTTP middleware stack.
- Go 1.25 remains the build line; release validation uses patched Go
  1.25.13 or newer. Native Linux smoke retains the musl runner v0.5.7.

## [v0.2.0] — 2026-07-09

**Theme**: First public release. v0.2.0 promotes the post-MVP feature
set into the public-bound line: versioned schemas, runtime catalog
validation, catalog authoring helpers, allowlists, publish-surface
audit, rewrite hygiene guidance, and the reliability fixes needed for
repeatable release gates.

### Added

- **Structured corpus input for `catalog build`.** The existing
  `PROTECTED==>replacement` term-list grammar remains byte-compatible, and
  `catalog build` now also accepts explicit `regex:<pattern>`,
  `allowlist:literal:<pattern>`, and `allowlist:regex:<pattern>` lines.
  Regex detector lines emit `regex_patterns` entities with stable opaque
  `e-rx-<hex>` ids; allowlist lines emit top-level allowlist entries
  with stable opaque `al-tl-<hex>` ids. Regexes are validated before output is
  written, malformed structured input exits `2`, and diagnostics stay
  line-numbered/value-free — never echoing the regex, allowlist pattern, or raw
  line.

- **Catalog allowlist primitive + live-visibility resolver.** Catalogs
  gain an optional top-level `allowlist` (literal or regex entries, with the
  same `case_insensitive`/`whole_word` flags aliases get) — catalog schema
  bumped to **1.1.0** (additive; 1.0.x catalogs validate unchanged). An allowlist
  match **suppresses any finding whose span it fully covers**, regardless of the
  producing entity: the engine _subtracts the allowlist, then matches_, so a
  suppressed term also drops out of co-occurrence evaluation. This expresses the
  cases a frozen denylist cannot — a codename that is also a public OSS tool, or
  the **reference-vs-disclosure** split where a filename (`AGENTS.local.md`) may
  be _mentioned_ even though its _contents_ must not leak. Suppressions are
  **counted, never silent**: the scan output schema bumped to **1.1.0** adds
  `scan_metadata.allowlist_suppressions` and `allowlist_suppressions_by_entry`
  (present-with-zero/empty; the by-entry map sums to the total), so a clean run
  with a non-zero count says "the allowlist subtracted N matches" rather than
  hiding them. `scan --explain` lists each suppression on stderr by alias-safe
  id — never the catalog-private pattern or matched text (zero-leak, ADR-0003).
  A new `limensafe catalog visibility-allowlist --catalog <in> --map
<CODENAME==>owner/repo> --out <out>` resolves each codename's backing
  repository visibility (`gh repo view --json visibility` semantics) and
  allowlists the ones that are **currently public** — composing with `catalog
build`. It **fails safe**: a 404, auth failure, rate limit, or
  missing `gh` never reads as public, generated ids are opaque, the summary is
  value-free (counts only), and re-runs are idempotent.

- **Rewrite operating patterns & post-action hygiene contract.**
  New `docs/usage/rewrite-operating-patterns.md` documents the git-history
  rewrite **mode taxonomy** (full / content-only / message-only / identity-only
  / tip-only and what each leaves untouched) and the **post-action hygiene
  contract**: a rewrite for confidentiality remediation is complete only when
  every artifact it created — backup ref, archive tag, snapshot branch,
  committed term-list/callback — is off the publishable remote and a
  `limensafe audit-publish` from a **fresh clone** exits `0`. The contract is
  captured as [`ADR-0008`](docs/decisions/ADR-0008-rewrite-completion-contract.md),
  cross-referenced from `RELEASE_CHECKLIST.md` (pre-public Final Validation) and
  CONTRIBUTING (backup-pattern refs are tier-elevated — `publish_safe` is
  `false` regardless of scanned content). Pairs with the `audit-publish`
  detector and warning copy. Detection and advice only — limensafe never
  deletes or rewrites refs.

- **`audit-publish` — pre-public publish-surface inventory.**
  A new `limensafe audit-publish [repo]` command audits **every ref a repository
  would expose when made public** — all branches and tags on the publish remote
  (or `--local-refs`) — not just the one ref you point a scan at. It flags
  **leak-vector refs**: those carrying protected entities the primary ref does
  not (`diverges_from_primary` — the detector that catches a `backup/*` branch a
  history rewrite left behind) and those whose names match danger patterns
  (`backup/*`, `*pre-rewrite*`, `*-snapshot-*`, `archive/*`, `*-bak`, `wip/*` —
  `name_pattern`, configurable via `--danger-pattern`). It composes the
  history blob model lifted to refs (content deduped across the union ref set,
  scanned once; content and path-segment findings attributed to each ref that
  reaches the blob at each path) and emits a single go/no-go report:
  `summary.publish_safe` plus `leak_vector_refs` and a `suggested_action` per
  ref. `publish_safe` is `true` only when no ref is a leak vector and no ref
  carries a block-tier finding. Exit codes mirror the locked scan contract
  (`0`/`1`/`2`/`3`); the primary baseline is scanned even when outside the
  emitted surface (e.g. `--tags-only`) so divergence is never measured against
  an empty set. **Detection and advice only — it never deletes or rewrites
  refs**; leak-vector refs also get a redaction-safe rewrite-hygiene warning on
  stderr. Output is pinned by a dedicated
  `schemas/limensafe/v1.0.0/publish-surface-output.schema.json`, and every
  string field — ref names and `suggested_action` included — is redacted per
  ADR-0003. See README "Audit before going public" and CONTRIBUTING §Scan CLI
  contract.
- **`catalog build --from-termlist` — generate a catalog from a flat term-list.**
  A new `limensafe catalog build` subcommand turns a flat
  `PROTECTED==>replacement` term-list into a schema-conformant catalog YAML.
  Terms sharing a replacement collapse into one entity (replacement →
  `replacement_suggestion`); a trailing ` # class=… severity=…` directive sets
  per-group metadata and is inherited by undirected sibling lines. Generated
  entities use the validated catalog-B posture (`case_insensitive` + `slug` +
  `whole_word`, with whole-word **on by default** and a `--no-whole-word`
  opt-out), and carry stable, opaque, hash-derived ids (`e-tl-<hex>`) that are
  never positional and never derived from protected text — so output is
  deterministic and byte-identical across rebuilds. `--out` is required (no
  stdout default; the output is protected vocabulary by construction), and the
  generated bytes are round-tripped through the loader + JSON Schema before
  being written. Diagnostics are **redaction-safe**: a malformed term-list is
  reported by line number and structural reason only — never echoing the term,
  replacement, directive value, or raw line (ADR-0003). Exit codes match the
  scan contract (`0` ok, `2` invalid term-list/options, `3` runtime I/O). See
  [Building a catalog from a term-list](docs/catalog/build-from-termlist.md)
  and the CI recipe in
  [`docs/usage/ci-integration.md`](docs/usage/ci-integration.md#building-a-catalog-from-a-term-list-in-ci).
- **Runtime catalog schema enforcement.**
  `limensafe scan` now validates every operator-supplied catalog against the
  catalog JSON Schema at load time, using a copy of the schema embedded in the
  binary (`santhosh-tekuri/jsonschema/v5`) so enforcement is identical in-repo
  and in an installed binary. Structural violations fail the load as a config
  error (exit 2) with **redaction-safe diagnostics** — JSON-pointer location
  plus failing keyword only, never the offending catalog value. A `schema_version`
  major mismatch is a hard error; a higher minor/patch within major 1 and an
  omitted `$schema` surface as compatibility warnings. The
  embedded schema is drift-checked against the canonical `schemas/` copy by
  `make verify-embedded-schemas` (wired into `check-all`/`pr-final`). See
  [`ADR-0006`](docs/decisions/ADR-0006-catalog-two-layer-validation.md).
- **Catalog JSON Schema contract.**
  `schemas/limensafe/v1/catalog.schema.json` now pins the structural shape
  of vocabulary catalogs: top-level identity/version fields, entities,
  variants including `whole_word`, regex-backed entities, visibility/severity
  enums, and co-occurrence rule windows. The schema is draft 2020-12, uses
  the hosted URI `https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json`,
  and is covered by meta-validation plus fixture conformance tests against the
  built-in and synthetic catalogs. It deliberately validates structure only;
  alias-safety, regex compilation, referential integrity, and semantic hygiene
  remain loader/linter responsibilities.
- **Published, versioned scan output JSON Schema.**
  `schemas/limensafe/v1.0.0/scan-output.schema.json` now pins the full
  stdout document (`version`, `scan_metadata`, `summary`, `findings[]`) —
  the surface downstream CI consumers parse. Fixed objects are
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

- **Centralized shared CLI flags (ADR-0007).**
  The catalog/scan-posture flags shared by `scan` and `audit-publish`
  (`--catalog`, `--config-file`, `--visibility`, `--mode`, `--workers`,
  `--max-file-size`, `--private-catalog-missing`) are now registered from a
  single helper (`internal/cmd/flags.go`) instead of being hand-declared per
  command, with a flag-parity test preventing drift. `scan` is the canonical
  definition; `audit-publish`'s `--workers`/`--max-file-size` help text now
  reads with scan's canonical wording (cosmetic, no behavior change). `attest`
  keeps its own registration by design (its `--mode`/`--config-file` semantics
  differ). See [`ADR-0007`](docs/decisions/ADR-0007-centralize-shared-cli-flags.md).
  Also tidied the `audit-publish` leak-vector warning helper to satisfy the
  static-analysis advisory (explicitly-ignored best-effort stderr writes).
- **Stable, reconcilable `.limensafeignore` skip accounting.**
  `scan_metadata.files_skipped` is now the stable total of file units not
  scanned **regardless of tree shape** — a wholesale directory prune folds
  the files it represents into `files_skipped` and
  `files_skipped_by_reason["ignored"]` instead of hiding them behind a bare
  `directories_skipped` count. `directories_skipped` remains an additional
  structural roll-up (never a substitute), `files_skipped_by_reason` always
  sums to `files_skipped`, and the matching stderr directory skip event
  carries a `files=<n>` count for reconciliation (counts only — pruned
  descendant paths are never enumerated, per the zero-leak invariant).
- **Core scan counters emit present-with-zero.** `worker_count`,
  `files_scanned`, `bytes_scanned`, `files_skipped`, `directories_skipped`,
  and `files_skipped_by_reason` are now always present (`0` / `{}`) rather
  than omitted via `omitempty`, so `jq`/CI consumers read a stable integer
  or object instead of `null` on a clean scan. Mode-specific fields stay
  omitted by design.

### Fixed

- **`.limensafeignore` skip counters no longer flip shape by scope.**
  Previously the same ignore rule reported skips two
  incompatible ways depending on surrounding tree structure (per-file
  `files_skipped` vs per-directory `directories_skipped` with the file
  count hidden), so a consumer could not read a single reliable
  "files not scanned" total. The directory-prune path now reports the
  files it represents, making both shapes reconcile.

- **CLI/catalog UX papercuts.** `scan <repo> --git-archive <ref>`
  now accepts the shell-natural space-separated ref form for explicit
  repository scans, matching the existing `--git-archive=<ref>` form and the
  cwd shorthand. Invalid two-argument archive usage now reports the accepted
  shape instead of the generic positional-argument error. Catalog diagnostics
  now name the real `regex_patterns` field (plural), and the common
  `match: {regex: ...}` authoring mistake fails with a value-free
  `did you mean regex_patterns?` hint rather than a low-signal schema error.

- **Scan reliability under repeated/piped invocation.** `scan` no
  longer terminates from `SIGPIPE` (exit 141, empty/truncated stdout) when a
  downstream consumer of stdout or stderr closes its read end early — common in
  CI pipelines (`limensafe scan … | head`, `| jq`, `| grep`, or a log collector
  that restarts). `SIGPIPE` is now neutralized: a broken **stderr** (advisory
  diagnostics — skip events, catalog warnings, `--explain`, progress) is
  swallowed best-effort so the scan still completes and emits its JSON document,
  while a broken **stdout** (the JSON contract consumer) surfaces as a classified
  runtime error (exit `3`). Scan-cancellation paths that previously escaped as an
  unclassified exit `1` are now classified exit `3` as well. The locked exit-code
  contract (0/1/2/3) and well-formed stdout-JSON now hold on every invocation,
  sequential or parallel.

- `scan <repo> --git-archive=<ref>` now supports bare mirror repositories as
  well as worktrees, and `attest <repo>` can write an attestation for a bare
  mirror by scanning the tracked tree at `HEAD`.

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
  exercises the 5-check end-to-end CLI contract: version, health,
  scan tracked archive, --staged
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
- **Productbook entry** — formalized in the internal product book with
  the standard project-entry shape.

### Fixed

- **Identity-shadow bug** — the limensafe binary mis-identified itself as the
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
  v0.3.3; `CGO_ENABLED=0` env added (limensafe
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

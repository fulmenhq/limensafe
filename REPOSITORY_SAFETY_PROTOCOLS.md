# limensafe — Repository Safety Protocols

Guardrails for high-risk operations against the limensafe repository.
Companion to [`AGENTS.md`](AGENTS.md) (day-to-day agent guide) and
[`MAINTAINERS.md`](MAINTAINERS.md) (ownership and escalation paths).

## Quick Reference

- **Human oversight required** for all merges to `main`, tag creation,
  signing, push to public, and any change that touches the locked scan
  CLI contract
- **The scan CLI contract is locked** (exit codes 0/1/2/3 + stdout=JSON,
  stderr=diagnostics); see [`CONTRIBUTING.md` §Scan CLI contract](CONTRIBUTING.md#scan-cli-contract-do-not-break)
- **Zero-leak invariant** ([ADR-0003](docs/decisions/ADR-0003-redaction-safe-output.md))
  is enforced by the Redactor; every emit path must route through it
- **Use Make targets** for build, test, lint, signing, release — they
  encode the gates
- **Use worktrees** for branch work (see [`AGENTS.md` §Worktree Discipline](AGENTS.md#worktree-discipline))
- **Real organizational vocabulary stays out of the repo** per the
  two-layer catalog rule

## limensafe-Specific Safety Guidelines

### Scan CLI contract preservation

The `scan` subcommand is an integration contract. CI wrappers
(Make recipes, pre-commit hooks, GitHub Actions, DataWidget partner-integration
integration) depend on:

- **Exit codes**: `0` = no findings at or above threshold; `1` = blocked
  finding(s); `2` = config / catalog error (`ErrConfigInvalid`); `3` =
  runtime / I/O error (`ErrRuntime`)
- **Stream separation**: scan-result JSON on stdout (valid JSON even on
  exit `1`); diagnostics on stderr only
- **Sentinel-error wrapping**: new error paths inside the scan flow wrap
  with the correct sentinel (`ErrConfigInvalid` or `ErrRuntime`); plain
  `fmt.Errorf` without a sentinel falls through to the generic-failure
  exit code

Contract is locked by
[`test/integration/scan_exit_codes_test.go`](test/integration/scan_exit_codes_test.go)
(`TestScanExitCodeContract` and `TestScanOutputStreamContract`).
Changes that touch the contract require:

1. devrev review
2. Updates to both contract tests
3. Updates to [`CONTRIBUTING.md` §Scan CLI contract](CONTRIBUTING.md#scan-cli-contract-do-not-break)
4. Updates to `scan --help` text
5. Escalation to @3leapsdave if the change is observable from a CI wrapper

### Zero-leak invariant

No byte emitted by limensafe — across stdout, stderr, JSON payload,
finding IDs, fingerprint inputs, log lines, debug output, error
messages — may contain a protected substring from any loaded catalog.

The Redactor (v0: regexp-alternation matcher built from the merged alias
set; Aho-Corasick is the slated v0.x swap path per `pkg/output/redactor.go`
implementation note) sits at the JSONFormatter boundary; every emit path
routes through it.

Boundary coverage is layered: `TestRedactor_*` unit tests in
`pkg/output/redactor_test.go` confirm the matcher handles representative
strings against the synthetic-acme alias set; `TestJSONFormatter_Emit_Redacts*`
in `pkg/output/json_formatter_test.go` confirm the formatter routes
findings through the Redactor; `TestScanOutputStreamContract` in
`test/integration/scan_exit_codes_test.go` confirms stream separation on
real scan runs.

**Forbidden patterns:**

```go
// FORBIDDEN — emits user content directly
fmt.Println(filePath)
log.Printf("scanning %s", userInput)
return fmt.Errorf("invalid catalog %s", catalogContents)
```

**Required pattern:**

```go
// Route through Redactor
redacted := redactor.Redact(filePath)
fmt.Println(redacted)

// Or: emit only opaque IDs and counts
log.Printf("scanning %d files", fileCount)
return fmt.Errorf("%w: catalog %q failed validation", ErrConfigInvalid, catalogID) // catalogID is opaque per ID-safety rule
```

### Catalog content discipline

- Real organizational vocabulary (client names, codenames, internal
  paths, etc.) **NEVER** lives in this repo. Two-layer catalog rule:
  the repo holds only opaque catalog IDs in `.limensafe/config.yaml`;
  raw vocabulary lives outside (local files, env-injected paths, or
  vendored builtins for the public baseline).
- Only synthetic placeholders (`acme`, `horizon`, `tilden`) and the
  public-baseline live inside the repo.
- Catalog content authorship is **devlead with secrev review**. uxdev
  shapes how operators interact with catalogs (loader error messages,
  documentation) but does not author content.
- Before any commit that touches `testdata/`, `pkg/catalog/builtin/`,
  or any other catalog-adjacent path: dogfood-scan the change with
  `limensafe scan <path> --visibility public_oss` to confirm no private
  vocabulary leaked in.

### Signing-key handling

Release signing keys are provisioned at the **fulmenhq org level** and
shared across fulmenhq workhorses (goneat, a sibling repo, limensafe). Blast
radius is limited to one org by design.

- **Private keys**: `$HOME/.minisign/fulmenhq-release.key` (minisign),
  `$HOME/.gnupg/` keyring for `security@fulmenhq.dev` (PGP)
- **NEVER commit** any file from `$HOME/.minisign/` or any `.gnupg/`
  contents
- **NEVER set** signing key paths to anything inside the repo tree
- Public keys (`fulmenhq-release-minisign.pub` and the exported PGP
  public block) ARE shipped with releases under `dist/release/` —
  uploaded as provenance assets, never committed to the repo
- Verify keys are public-only before any release upload:
  `make release-verify-keys`
- Canonical reference for the full signing flow:
  [`~/dev/goneat/RELEASE_CHECKLIST.md`](../goneat/RELEASE_CHECKLIST.md)

### App identity discipline

`.fulmen/app.yaml` is the canonical app identity. The embedded copy at
`internal/assets/appidentity/app.yaml` MUST agree. The `VERSION` file
MUST agree with both YAMLs.

- Use `make version-set VERSION=X.Y.Z` or `make version-bump-{patch,minor,major}`
  for version bumps. These call `scripts/sync-version.sh` which
  propagates atomically.
- Never edit the YAMLs directly; `make verify-version-alignment` blocks
  the precommit/prepush gate if they drift.
- `make verify-embedded-identity` separately confirms the embedded copy
  matches the source YAML.

## High-Risk Operations

### Version bumps

- Use `make version-set VERSION=X.Y.Z` (atomic) — never edit YAMLs manually
- Run `make check-all` after every version bump to catch any regression
- Major version bumps (X.0.0): require @3leapsdave approval
- Patch bumps (0.0.x): self-merge by devlead after `make check-all` green

### Tagging and releases

- Only `main` is taggable
- Tags are annotated and follow the `v<MAJOR.MINOR.PATCH>` convention
- Tag message includes a 1-line summary; CI consumes from the tag
- Push tags via `git push origin v<version>` — this triggers
  `.github/workflows/release.yml`
- Full release ritual: [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md)
- Per-release coordination: the `the release channel` channel
  (created per release by dispatch)

### Pushing to `main`

- All changes land via PR; `main` is protected
- @3leapsdave is Committer-of-Record on every commit in supervised mode
  (v0.x); see [`MAINTAINERS.md`](MAINTAINERS.md#operating-modes)
- Before `git push origin main`: `make check-all` green from a fresh
  shell; all attribution trailers present on all commits being pushed

### Structural changes

- Package reorganization, public API renames, or removal of any package
  exported via `pkg/`: requires a brief in `the internal productbook`
  (`internal-brief`) plus an ADR in `docs/decisions/` for the rationale
- Breaking CLI changes (rename a flag, change exit code semantics,
  rename a subcommand): require a deprecation cycle (one minor version)
  and escalation to @3leapsdave
- New config keys: must have safe defaults and not break existing
  `.limensafe/config.yaml` files in adopter repos

## Incident Response

### Scan output suspected to contain a leak

This is the worst-case failure for limensafe. If a finding report
appears to contain raw protected vocabulary:

1. **STOP**: do not commit the report anywhere; do not paste it into
   shared channels
2. Capture a minimal reproducer (the catalog + input that produced the
   leak) — store in a temp directory outside the repo
3. Notify `the internal coordination channel` with `secrev` +
   `@3leapsdave` — keep the alert message itself sanitized; reference
   the temp-dir reproducer by path
4. Coordinate with secrev on a regression test against the synthetic
   corpus before any fix lands
5. Fix in a hotfix branch (`hotfix/redactor-<short-name>`); patch release

### Build / test failures

1. Run `make test` and `make lint` to confirm
2. Isolate failing tests: `go test -v ./<package>/... -run <TestName>`
3. Fix root cause (do not skip tests)
4. Verify with `make check-all` from a fresh shell
5. Document root cause in commit message
6. Add regression test if applicable

### Bootstrap-smoke failures

`make bootstrap-smoke` is the end-to-end CLI proof (5 checks per the
partner-integration devlead spec). If it fails:

1. Run the affected check by hand to see actual output
2. Common causes: a flag was renamed; an exit code was changed; an
   error path stopped wrapping a sentinel; help text drifted from
   documented contract
3. Decide: revert the offending change, or update the smoke + docs
   together (the contract changed; the smoke is correct to fail)

### Dependency vulnerability

1. Confirm severity via `go list -m all` plus a vuln scanner
2. Review whether the vulnerability affects limensafe's runtime path
   or only build-time tooling
3. Update the dependency in a `fix/dep-bump-<name>` branch
4. Run `make check-all` plus `make bootstrap-smoke`
5. Patch release if high severity affects runtime; otherwise bundle
   into the next minor release

## Emergency Procedures

### Critical security issue

1. **Do not commit or push the analysis publicly**; the issue itself may
   be the leak
2. Contact @3leapsdave via direct channel (DM, not a public channel)
3. Create a private hotfix branch; coordinate fix without exposure
4. Release patch following the standard signing flow
5. Disclose post-release per the disclosure policy

### Production-impacting bug

Example: scanner emits no findings on a known-positive corpus.

1. Assess severity and adopter impact (partner-integration integration is the
   highest-impact case in v0.x)
2. Create hotfix branch: `hotfix/v<version>`
3. Implement minimal fix with regression test against the synthetic
   corpus
4. Fast-track review with @3leapsdave
5. Release hotfix following [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md)
6. Notify `the brief channel` and `the internal coordination channel`

## Safety Checklist for Common Operations

### Before every commit

- [ ] Tests pass on touched surface: `go test ./<package>/...`
- [ ] Code formatted: `make fmt` (or `make check-all`)
- [ ] Lint clean: `make lint`
- [ ] Manual smoke if CLI surface changed: `make bootstrap-smoke`
- [ ] No raw user content in any new emit (`fmt.Println`, `log.Printf`, etc.)
- [ ] Attribution trailers present (`Co-Authored-By`, `Role:`,
      `Committer-of-Record:`)

### Before every PR

- [ ] `make check-all` green from a fresh shell
- [ ] No uncommitted changes: `git status` clean
- [ ] Operator docs updated if CLI behavior changed (`scan --help`,
      README, CONTRIBUTING)
- [ ] New error paths have sentinel-wrap + a subtest in
      `TestScanExitCodeContract`
- [ ] If touching catalog-adjacent paths: dogfood-scan ran clean

### Before every release

- [ ] [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md) walked end-to-end
- [ ] Pre-tag refit sweep complete (no template residue in
      SCREAMING_CASE root-level files; spot-check at each release)
- [ ] `make verify-version-alignment` passes
- [ ] `make bootstrap-smoke` passes 5/5
- [ ] Release-build produces all platform binaries + manifests
- [ ] Signing keys verified public-only after export
- [ ] No secrets in code or config

## Guardrails

### Automated protections

- `.plans/` is gitignored (planning files never committed)
- `.env` is gitignored (secrets never committed)
- `.limensafe/config.yaml` is committed (opaque IDs only); raw catalog
  YAMLs in `testdata/` are synthetic-only and dogfood-scanned
- `make verify-version-alignment` blocks precommit/prepush if version
  state drifts
- `make verify-embedded-identity` blocks precommit/prepush if the
  embedded app-identity copy drifts from `.fulmen/app.yaml`
- `make check-all` is the canonical quality gate (fmt + verify-embedded-
  identity + verify-version-alignment + lint + test)
- CI re-runs `make check-all` on every push; release tag triggers
  `.github/workflows/release.yml`

### Manual protections

- All releases require @3leapsdave approval (Committer-of-Record)
- Breaking CLI changes require deprecation period
- Security-sensitive changes get secrev review before merge
- Scan CLI contract changes get devrev review and CONTRIBUTING update

### Process protections

- Briefs in `the internal productbook` (`internal-brief`) for any feature
  or non-trivial chore
- ADRs in `docs/decisions/` for architectural or contract changes
- Worktree discipline for parallel agent work (see [`AGENTS.md`](AGENTS.md#worktree-discipline))

## Escalation Paths

### Development questions

1. Check [`HANDOFF.md`](HANDOFF.md) (architecture tour, open questions,
   backlog priorities)
2. Check [`docs/`](docs/) (design, decisions, architecture)
3. Check `the internal coordination channel` history for prior context
4. Ask in `the internal coordination channel` with `devlead`

### Technical blockers

1. Document the blocker in `.plans/blockers/` (gitignored)
2. Attempt safe workaround if available
3. Escalate to `@3leapsdave` in `the internal coordination channel` with full context
4. Pause work if the blocker is critical

### Process uncertainty

1. When in doubt, ask `@3leapsdave`
2. Better to pause than proceed incorrectly
3. Document the decision rationale in the relevant file (ADR,
   CONTRIBUTING, MAINTAINERS) so the next session has the context

### Architecture / boundary changes

1. Post in `the architecture review channel` (org-wide architecture review) or
   `the internal coordination channel`
2. Loop in `entarch` for cross-repo coordination
3. Land the decision as an ADR in `docs/decisions/` before
   implementation

## References

- [`AGENTS.md`](AGENTS.md) — Day-to-day agent guide and operational rules
- [`MAINTAINERS.md`](MAINTAINERS.md) — Ownership, agent handles, channels
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — Build/test/lint, scan CLI contract
- [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md) — Release process
- [`HANDOFF.md`](HANDOFF.md) — Architecture, design decisions, open questions
- [`docs/decisions/ADR-0003-redaction-safe-output.md`](docs/decisions/ADR-0003-redaction-safe-output.md) — Zero-leak invariant
- [3 Leaps Commit Attribution Baseline](docs/catalog/agentic/attribution/git-commit.md)

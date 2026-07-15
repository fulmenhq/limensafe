# limensafe — AI Agent Guide

A focused agent guide for working on **limensafe** — a Confidential Context
Leakage (CCL) detector for Go projects, AI agent artifacts, and developer
workflows. See [`README.md`](README.md) for the user-facing overview and
[`HANDOFF.md`](HANDOFF.md) for the architecture tour.

## Read First

- **Trust the contract**: the `scan` CLI exit codes (0/1/2/3) and output
  stream separation (stdout = JSON; stderr = diagnostics) are locked. CI
  wrappers and pre-commit hooks depend on them. See
  [`CONTRIBUTING.md` §Scan CLI contract](CONTRIBUTING.md#scan-cli-contract-do-not-break).
- **Zero-leak boundary is load-bearing**: no protected substring may appear
  in any byte limensafe emits. The Redactor (regexp-alternation matcher
  over the merged alias set in v0; Aho-Corasick is the slated v0.x swap)
  sits at every emit path. See
  [`ADR-0003`](docs/decisions/ADR-0003-redaction-safe-output.md).
- **Conform to 3 Leaps OSS sensitive-data policy**: sensitive or
  proprietary local data lives outside this repository tree, not behind
  `.gitignore`. See [§Confidentiality Posture (OSS Surface)](#confidentiality-posture-oss-surface)
  below for the limensafe-specific statement; the canonical policy is
  <https://github.com/3leaps/oss-policies/blob/main/SENSITIVE-LOCAL-DATA.md>.
- **Confirm your agentic interface**. If the session does not name an
  interface adapter (e.g., Claude Code, Codex CLI), pause and request
  guidance from @3leapsdave before taking action.
- **Review [`REPOSITORY_SAFETY_PROTOCOLS.md`](REPOSITORY_SAFETY_PROTOCOLS.md)**
  before tagging, signing, or pushing.

**Project**: limensafe — Confidential Context Leakage detector
**Repository**: [fulmenhq/limensafe](https://github.com/fulmenhq/limensafe) (private; intended public after first signed releases)
**Governance**: 3 Leaps Initiative; the maintainer team owns the repo from v0.0.4
onward. See [`MAINTAINERS.md`](MAINTAINERS.md).

### Known Interface Adapters

| Agentic Interface | Definitive Prompt / Rules File          |
| ----------------- | --------------------------------------- |
| Claude Code       | `CLAUDE.md` (if present) or `AGENTS.md` |
| Codex CLI         | `CODEX.md` (if present) or `AGENTS.md`  |
| Cursor            | `AGENTS.md`                             |
| Cline             | `.cline/rules/PROJECT.md`               |
| KiloCode          | `AGENTS.md`                             |
| OpenCode          | `AGENTS.md`                             |

## Confidentiality Posture (OSS Surface)

limensafe is a Confidential Context Leakage detector. The OSS surface
of this repo — code, docs, schemas, examples, fixtures, commit
messages, PR descriptions, issue tracker — must hold itself to the
standard it enforces for adopters: no proprietary identifiers, no
internal operator artifacts, no client or engagement context.

**This repo conforms with the 3 Leaps OSS Sensitive Local Data
policy:** <https://github.com/3leaps/oss-policies/blob/main/SENSITIVE-LOCAL-DATA.md>
(canonical; this repo links rather than copying so the statement
stays consistent across the 3 Leaps stack).

### The principle

Sensitive or proprietary local data lives **outside the repository
working tree**, never in-tree behind `.gitignore`. `.gitignore` is a
convenience filter, not a security boundary; a mistaken edit to it
must never be able to leak a contributor's, operator's, or user's
information. If the data does not live in the tree in the first
place, it cannot.

### How this applies to limensafe specifically

limensafe scans for organization vocabulary catalogs — and catalogs
themselves are the kind of artifact this policy concerns. Real-world
catalogs live **outside this repo**:

- **Organization vocabulary catalogs** stay in operator-controlled
  storage; a scan references them by path via
  `--catalog <path-to-file>` (no convention requires the file to live
  anywhere in or near this repo)
- **Engagement-specific catalogs** stay in operator-private
  engagement storage per the 3 Leaps handbook convention; same
  reference-by-path pattern
- **The only catalogs vendored in this repo are synthetic
  placeholders** (`testdata/synthetic-acme/`, using the canonical
  `acme` / `horizon` / `tilden` vocabulary). These are a **contract
  reference for adopters and acceptance tests**, not real data.
  Adopters never use these in production scans; operators never
  add real entries to them.

The same out-of-tree principle applies to any operator-private
material an agent or contributor needs while working in this repo
(field-test journals, incident records, machine-local environment
notes). See `../AGENTS.limensafe.local.md` (operator-private; lives
**outside** this repo's tree by design, not gitignored within it)
for machine-local detail when present. **Ask the maintainer** (see
[`MAINTAINERS.md`](MAINTAINERS.md)) if you cannot find that file or
are unsure where local operational detail should live — do not
reconstruct missing case detail from inference or commit
speculative claims.

### What this section deliberately does not contain

Per the policy's "concrete mechanics → out-of-tree" rule, this
section states the principle without enumerating concrete denylists,
operator paths, or internal tooling specifics — those would
themselves republish the structure they are meant to keep private.
The principle is the public contract; the implementation lives in
operator-private notes.

## Roles

This repository uses the FulmenHQ Crucible role catalog. Role prompts live
under [`config/agentic/roles/`](config/agentic/roles/) and are referenced
from [`MAINTAINERS.md`](MAINTAINERS.md#roles).

| Role       | Prompt                                              | Use When                                                         |
| ---------- | --------------------------------------------------- | ---------------------------------------------------------------- |
| `devlead`  | [devlead.yaml](config/agentic/roles/devlead.yaml)   | Implementation, architecture, feature work                       |
| `devrev`   | [devrev.yaml](config/agentic/roles/devrev.yaml)     | Code review, bug finding, four-eyes audit                        |
| `uxdev`    | [uxdev.yaml](config/agentic/roles/uxdev.yaml)       | CLI UX, scan output, error-message quality, operator docs polish |
| `infoarch` | [infoarch.yaml](config/agentic/roles/infoarch.yaml) | Documentation, schemas, standards                                |
| `prodmktg` | [prodmktg.yaml](config/agentic/roles/prodmktg.yaml) | Release notes, README updates, public messaging (pre-public)     |
| `cicd`     | [cicd.yaml](config/agentic/roles/cicd.yaml)         | Pipelines, builds, release automation                            |

Additional roles (`secrev`, `releng`, `cxotech`, `entarch`) are referenced
from [`MAINTAINERS.md`](MAINTAINERS.md#roles); their prompts live in the
upstream Crucible repos and are pulled in as needed.

### Role Selection

- **Default to `devlead`** for most implementation work
- **Use `devrev`** for reviewing code written by others (enables four-eyes model)
- **Use `uxdev`** for changes to CLI flags, error messages, output formats,
  `--help` text, or operator-facing docs
- **Use `infoarch`** for documentation-focused work
- **Use `prodmktg`** for release notes and README updates (pre-public)
- **Use `cicd`** for pipeline and automation work

**Environment variables are authoritative.** If `LANYTE_AGENT_ROLE` says
`uxdev`, you are uxdev — regardless of the role table's defaults.

## Worktree Discipline

Multiple agents and contributors may work this repo concurrently.
Sharing a single checkout for branch work guarantees clobbering. Use git
worktrees for any branch work.

**Convention**: sibling-directory pattern.

```bash
# From ~/dev/limensafe/
git worktree add ../limensafe-<branch-slug> -b <branch> origin/main

# Examples:
git worktree add ../limensafe-fix-scan-color -b fix/scan-color origin/main
git worktree add ../limensafe-feat-limensafeignore -b feat/limensafeignore origin/main
```

**Rules:**

- One worktree per active branch. Remove when the branch lands:
  `git worktree remove ../limensafe-<branch-slug>`.
- Sibling layout (`../limensafe-<slug>/`) — not nested under the main
  checkout. Keeps the worktree out of the main repo's working tree.
- Never run `git checkout <other-branch>` in the main `~/dev/limensafe/`
  checkout while another agent's session is active there.
- Verify your CWD before any git operation: `pwd` should match the worktree
  for branch work; `~/dev/limensafe/` only for direct `main`
  operations (status checks, fetching, pulling).

When you finish a branch, clean up:

```bash
# After the PR merges
cd ~/dev/limensafe/
git worktree remove ../limensafe-<branch-slug>
git branch -d <branch>          # delete the local branch
git fetch --prune               # prune the now-deleted remote-tracking ref
```

## Commit Attribution

Follow the [3 Leaps commit attribution standard documented in
`CONTRIBUTING.md`](CONTRIBUTING.md#commit-attribution-3-leaps-standard).
Required trailers (supervised mode, v0.x):

```
Co-Authored-By: <Model display name> <noreply@3leaps.net>
Role: <role>
Committer-of-Record: Dave Thompson <dave.thompson@3leaps.net> [@3leapsdave]
```

**Never** use vendor defaults like `noreply@anthropic.com`. The `Role:`
trailer names the public role catalog slug, such as `devlead`, `devrev`, or
`uxdev`. See
[`MAINTAINERS.md`](MAINTAINERS.md#attribution-guidelines) for context on the
supervised vs. autonomous mode distinction.

### Example Commit

```
feat(scan): add --max-file-size flag with config-error exit on overflow

Adds a per-file byte cap (default 5MB). Files exceeding the cap emit a
skip-event on stderr; the scan continues. Invalid sizes return exit code
2 (ErrConfigInvalid) with an actionable message.

Changes:
- Add --max-file-size flag to scan command
- Wire size parsing through internal/cmd/scan.go validateFlags()
- Wrap parse errors with ErrConfigInvalid sentinel
- Update scan --help; add CONTRIBUTING entry
- Add TestScanExitCodeContract subtest for the parse-error path

Generated by Claude Opus 4.7 via Claude Code under supervision of @3leapsdave

Co-Authored-By: Claude Opus 4.7 <noreply@3leaps.net>
Role: uxdev
Committer-of-Record: Dave Thompson <dave.thompson@3leaps.net> [@3leapsdave]
```

## Session Startup Protocol

1. **Context review**
   - Confirm your role (see [Role Selection](#role-selection); if set,
     `LANYTE_AGENT_ROLE` is authoritative)
   - **REQUIRED**: Read [`Makefile`](Makefile) to understand build targets
   - Read [`MAINTAINERS.md`](MAINTAINERS.md),
     [`REPOSITORY_SAFETY_PROTOCOLS.md`](REPOSITORY_SAFETY_PROTOCOLS.md),
     [`README.md`](README.md), and [`.fulmen/app.yaml`](.fulmen/app.yaml)
   - Read [`HANDOFF.md`](HANDOFF.md) for architecture tour, design
     decisions, and open questions
   - Read your role prompt under [`config/agentic/roles/`](config/agentic/roles/)
   - Read [`CONTRIBUTING.md`](CONTRIBUTING.md) §Scan CLI contract (the
     contract you must not break)

2. **Environment check**
   - Confirm `go >= 1.21`, `goneat`, `make` are available
   - Run `make bootstrap` if tools are missing

3. **Plan**

   For non-trivial work: outline the change in `.plans/` (gitignored) or
   within the session before modifying files. Keep planning artifacts out
   of this repo — `.plans/` is permanently gitignored.

4. **Branch work uses a worktree**

   See [Worktree Discipline](#worktree-discipline) above. Never push a
   branch from inside the main `~/dev/limensafe/` checkout when
   another session may be using it.

5. **Quality assurance**
   - Run `make test` and `make lint` before commit
   - Run `make check-all` before commit (fast verify-mode quality gate)
   - Run `make prepush` before push (CI-aligned gate)
   - Run `make pr-final` before requesting final PR review
   - For CLI-surface changes, also run `make bootstrap-smoke` (the
     end-to-end CLI smoke spec)
   - Verify scan-contract integration tests:
     `go test ./test/integration/... -run TestScan`

6. **Attribution**

   Every commit gets the trailers in
   [Commit Attribution](#commit-attribution) above.

7. **Review**

   Open a PR for human review; supervised mode requires human review
   before merge.

## Operational Guidelines

### DO

- **Quality first**: run `make check-all` before commits to `main`-bound
  branches, `make prepush` before push, and `make pr-final` before final
  PR review; run `make lint` and targeted tests as you iterate
- **Use Make targets**: prefer `make test`, `make build`, `make scan`,
  `make check-all`, `make prepush`, and `make pr-final` over raw `go`
  invocations
- **Separate formatting fix and verify modes**: use `make fmt` to mutate
  files in the dev loop; use `make format-check` or `make prepush` when
  green must mean CI will see the same formatted tree
- **Trust the scan CLI contract**: when adding scan error paths, wrap with
  `ErrConfigInvalid` or `ErrRuntime`; add subtests to
  `TestScanExitCodeContract`
- **Route every emit through the Redactor**: any new `fmt.Println`,
  `log.Printf`, or output template that touches user content goes through
  the Redactor. Plain emit of user content is forbidden.
- **Maintain test coverage**: every behavior change ships with tests
- **Keep operator docs honest**: when you change CLI behavior, update
  `scan --help`, README, and CONTRIBUTING in the same PR
- **Use App Identity**: never hardcode app name, env var prefix, or
  config paths — call `appidentity.Get(ctx)` via `internal/appid`
- **Use worktrees for branch work**: see [Worktree Discipline](#worktree-discipline)
- **Self-scan attestation before push**: after the final source/docs
  commit, run `make limensafe-attest`, commit only
  `.limensafe/scan-attestation.json`, and verify with
  `make limensafe-verify`. Convention details in `CONTRIBUTING.md`
  §Limensafe self-scan attestation.

### DO NOT

- **Break the scan CLI contract**: never silently change exit codes,
  stream separation, or sentinel-error wrapping. Anything that touches
  the locked behavior in `CONTRIBUTING.md` §Scan CLI contract escalates
  to human maintainers
- **Emit raw user content**: `fmt.Println(userInput)`, `log.Printf("%s",
userPath)`, etc. — every emit path that touches user content routes
  through the Redactor
- **Edit `.fulmen/app.yaml` directly** without also running `make
sync-embedded-identity` to update `internal/assets/appidentity/app.yaml`.
  Better: use `make version-set` / `make version-bump-*` to bump versions
  atomically
- **Commit planning files**: `.plans/` is permanently gitignored
- **Skip tests**: never commit code with failing tests on the touched
  surface
- **Ignore linting**: all code must pass `make lint`
- **Commit without formatting**: `make fmt` to fix, then `make format-check`
  or `make check-all` to verify before commit
- **Push from the main checkout while another agent's session is active there**
- **Introduce new output formats without an ADR**: see uxdev role
  responsibilities; output formats are integration contracts

## limensafe-Specific Guidelines

### The scan CLI contract is locked

CI wrappers, pre-commit hooks, and downstream integrations depend on the
exit codes (0/1/2/3) and output stream separation. Tests in
[`test/integration/scan_exit_codes_test.go`](test/integration/scan_exit_codes_test.go)
lock the contract. Any change that touches these surfaces requires:

1. Coordination with devrev and (for any external-contract impact)
   human maintainers
2. Updates to both `TestScanExitCodeContract` and `TestScanOutputStreamContract`
3. Updates to [`CONTRIBUTING.md` §Scan CLI contract](CONTRIBUTING.md#scan-cli-contract-do-not-break)
4. Updates to `scan --help` text

### Zero-leak invariant (ADR-0003)

No byte emitted by limensafe — across stdout, stderr, log lines, error
messages, finding IDs, fingerprint inputs, debug output — may contain a
protected substring from any loaded catalog. The Redactor (v0:
regexp-alternation matcher built from the merged alias set; Aho-Corasick
is the v0.x swap path) sits at the JSONFormatter boundary; every new
emit path routes through it.

Boundary coverage is layered: `TestRedactor_*` unit tests in
`pkg/output/redactor_test.go` confirm the matcher handles representative
strings against the synthetic-acme alias set; `TestJSONFormatter_Emit_Redacts*`
in `pkg/output/json_formatter_test.go` confirm the formatter routes
findings through the Redactor; `TestScanOutputStreamContract` in
`test/integration/scan_exit_codes_test.go` confirms stream separation on
real scan runs.

### Catalog discipline

Catalog content (entities, aliases, classes, regex patterns,
co-occurrence rules) is **devlead with secrev review** territory. uxdev
shapes how operators interact with catalogs (loader error messages,
catalog-source documentation) but does not author catalog content.
Real organizational vocabulary lives **outside the repo** per the
two-layer catalog rule; only synthetic placeholders (acme/horizon/tilden)
and the public-baseline are inside the repo.

## Reference Documents

| Reference                                                                                                             | What you'll find                                                              |
| --------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| [`README.md`](README.md)                                                                                              | User-facing overview, CI integration patterns, scan contract                  |
| [`HANDOFF.md`](HANDOFF.md)                                                                                            | Architecture tour, design decisions, open questions                           |
| [`CONTRIBUTING.md`](CONTRIBUTING.md)                                                                                  | Build/test/lint, scan-contract DO-NOT-BREAK, commit standard                  |
| [`MAINTAINERS.md`](MAINTAINERS.md)                                                                                    | Ownership, maintainer roster, roles, escalation                               |
| [`REPOSITORY_SAFETY_PROTOCOLS.md`](REPOSITORY_SAFETY_PROTOCOLS.md)                                                    | Guardrails for high-risk operations (signing, tagging, pushing)               |
| [3leaps/oss-policies §Sensitive Local Data](https://github.com/3leaps/oss-policies/blob/main/SENSITIVE-LOCAL-DATA.md) | Canonical sensitive-data policy this repo conforms with                       |
| `../AGENTS.limensafe.local.md` (out-of-tree)                                                                          | Operator-private machine-local notes (when present; ask maintainer if unsure) |
| [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md)                                                                        | Release process; goneat is the canonical signing reference                    |
| [`TESTING.md`](TESTING.md)                                                                                            | Test suite overview, running tests, fixtures                                  |
| [`docs/decisions/ADR-0003-redaction-safe-output.md`](docs/decisions/ADR-0003-redaction-safe-output.md)                | Zero-leak invariant rationale and contract                                    |
| [`docs/design/`](docs/design/)                                                                                        | Problem statement, architecture, catalog schema, tools gap                    |
| [`config/agentic/roles/`](config/agentic/roles/)                                                                      | Role prompts (limensafe-tailored subset of the Crucible catalog)              |

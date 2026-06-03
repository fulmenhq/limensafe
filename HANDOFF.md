# HANDOFF — limensafe v0.0.3 → the maintainer team

> A technical onboarding guide for the the maintainer team taking over limensafe
> stewardship from v0.0.4 onward. Written by cxotech
> (with entarch's signing slice contribution) at the
> conclusion of the v0.0.3 cycle. Read in order; each section builds on
> the previous.

## Welcome

`limensafe` is a Go CLI + library that detects **Confidential Context
Leakage** (CCL) — ordinary-looking names, codenames, paths, branches,
fixtures, docs, logs, and agent artifacts that reveal private
organizational context when they cross into public or shared surfaces.

It sits in the gap between **secret scanners** (gitleaks, TruffleHog —
which target credentials) and **PII / DLP tools** (Presidio — which
target regulated personal data). The detection key is **organization-
defined vocabulary catalogs**: limensafe doesn't know what
`acme-platform-internal` means; the consuming organization's catalog
does, and limensafe enforces the boundary.

You're inheriting a working v0 deterministic detector across six input
surfaces (filesystem, staged-tree, branch-name, commit-msg, stdin
extractors), Aho-Corasick redaction-safe output, repo-config with
three catalog source kinds (file/env/builtin), end-to-end CICD with a
5-platform release matrix, and a beta-tested integration partner
(DataWidget / partner-integration).

This guide is structured for a first-week onboarding read. Each
section ends with **pointers to deeper docs** when you want them.

## Table of contents

1. [Repo orientation](#repo-orientation)
2. [Architecture tour](#architecture-tour)
3. [Design decisions and rationale](#design-decisions-and-rationale)
4. [v0 timeline (what happened and why)](#v0-timeline-what-happened-and-why)
5. [Beta-tester relationships](#beta-tester-relationships)
6. [Open questions you're inheriting](#open-questions-youre-inheriting)
7. [Backlog priorities](#backlog-priorities)
8. [First-week checklist](#first-week-checklist)
9. [Canonical references](#canonical-references)

---

## Repo orientation

```
.
├── cmd/limensafe/          # main package — version vars + Execute()
├── internal/
│   ├── appid/              # gofulmen appidentity wrapper (10-line shim)
│   ├── assets/appidentity/ # embedded copy of .fulmen/app.yaml
│   ├── cmd/                # cobra subcommands (scan, version, health, doctor, envinfo, serve)
│   ├── config/             # config loader (gofulmen integration)
│   ├── errors/             # HTTP-flavored error envelopes (server use)
│   ├── metrics/            # Prometheus metrics
│   ├── observability/      # logger setup
│   └── server/             # HTTP server (groningen-template inheritance; exposed via `serve` subcommand; Q1 below gates keep/strip/refactor)
├── pkg/                    # public Go packages — importable as a library
│   ├── catalog/            # catalog loaders + builtin baseline
│   ├── engine/             # detection engine (literal/slug/path-segment/regex/co-occurrence)
│   ├── extractor/          # filesystem + staged-tree + stdin extractors
│   └── output/             # JSONFormatter + Redactor (zero-leak invariant)
├── scripts/                # release-signing, bootstrap-smoke, sync-version, etc.
├── test/integration/       # exit-code contract, stream separation, standalone-binary tests
├── testdata/               # acceptance corpora (synthetic-acme, builtin-baseline)
├── docs/
│   ├── design/             # problem-statement, architecture, catalog-schema, existing-tools-gap
│   ├── decisions/          # ADR-0003, ADR-0004 + ADR template
│   └── architecture/       # ADR-0001 (repository-root-detection, from gofulmen)
├── config/agentic/roles/   # Crucible role catalog (synced from SSOT)
├── .fulmen/app.yaml        # canonical app identity
├── .github/workflows/      # ci.yml + release.yml
├── Makefile                # build, test, lint, release-build, sign, etc.
├── VERSION                 # source of truth for the version string
├── README.md               # user-facing
├── CONTRIBUTING.md         # developer-facing (you live here)
├── MAINTAINERS.md          # contacts + ownership
├── RELEASE_CHECKLIST.md    # release process (manual signing per goneat-canonical flow)
├── CHANGELOG.md            # version history
└── docs/roadmap.md         # forward plan
```

The bits that matter most for week 1: `pkg/`, `internal/cmd/scan.go`,
`Makefile`, and the `docs/design/*` set.

The bits you can safely ignore for week 1: `internal/server/*` (HTTP
groningen-inheritance exposed via the inherited `serve` subcommand
but not exercised in v0; decision-gated on Q1 in
[open questions](#open-questions-youre-inheriting) — see also
[backlog](#backlog-priorities)),
`internal/errors/*` (HTTP error envelopes only, not used by scan).

## Architecture tour

### High level

```
        ┌─────────────────────────────────────────────────────┐
        │  limensafe scan <path|-> [--config-file ...]        │
        │                  [--catalog ...] [--staged]         │
        │                  [--branch-name|--commit-msg]       │
        └─────────────────────────────────────────────────────┘
                            │
                            ▼
        ┌─────────────────────────────────────────────────────┐
        │  Catalog loader (pkg/catalog)                        │
        │  - file source                                       │
        │  - env source (LIMENSAFE_CATALOG_PATH)               │
        │  - builtin source (vendored public-baseline-v0)      │
        │  Validates schema, resolves layered catalogs.        │
        └─────────────────────────────────────────────────────┘
                            │
                            ▼
        ┌─────────────────────────────────────────────────────┐
        │  Engine (pkg/engine)                                 │
        │  - Literal / slug / path-segment / regex detectors   │
        │  - Co-occurrence rules (window-based)                │
        │  - Severity composition (scope × catalog × override) │
        │  - Fingerprinting (HMAC-salted, alias-free)          │
        └─────────────────────────────────────────────────────┘
                ▲                                ▲
                │                                │
       ┌────────┴────────┐              ┌────────┴────────┐
       │ Extractor       │              │ Extractor       │
       │ (filesystem,    │              │ (stdin: branch- │
       │  staged)        │              │  name, commit-  │
       │  pkg/extractor  │              │  msg)           │
       └─────────────────┘              └─────────────────┘
                            │
                            ▼
        ┌─────────────────────────────────────────────────────┐
        │  Output (pkg/output)                                 │
        │  - JSONFormatter                                     │
        │  - Redactor (Aho-Corasick over alias set)           │
        │  - Zero-leak invariant: NO protected substring in    │
        │    any emitted byte (stdout/stderr/JSON/IDs)         │
        └─────────────────────────────────────────────────────┘
```

### Per-component tour

#### `pkg/catalog` — Vocabulary

A **catalog** is a YAML bundle of `Entity` records (each with
`aliases`, `class`, optional `regex_patterns`, optional `allowed_in`
scope list) plus `co_occurrence_rules` and a `fingerprint_salt`. The
schema is in `schemas/` and the canonical reference is
[`docs/design/catalog-schema.md`](docs/design/catalog-schema.md).

Three source kinds in v0:

- `file` — local path (always available)
- `env` — env var holding a path (always available)
- `builtin` — vendored baseline catalog at
  `pkg/catalog/builtin/public-baseline.yaml` (opt-in only via repo
  config; never auto-loaded)

`profile` (v0.x) and `url` (v1+) are recognized as future source kinds
but not implemented.

`LoadConfigFile()` reads `.limensafe/config.yaml` (repo-safe — opaque
catalog IDs only, never raw vocabulary) and resolves catalogs in
layered order. The chicken-and-egg problem ("how do you reference
protected vocabulary without storing it") is solved by the
**two-layer catalog** model: catalogs live OUTSIDE the repo and are
referenced by stable ID. See ADR text in
[`docs/design/architecture.md`](docs/design/architecture.md).

#### `pkg/engine` — Detection

The engine is intentionally **deterministic** in v0. No NER, no
statistical models. Detectors:

- `literal` — exact alias match (case-sensitive by default; opt-in
  case-insensitive variant generated at catalog load)
- `slug` — generated slug-form match (`acme-foo` → matches
  `acme_foo`, `acme-foo`, `acmeFoo` depending on variant flags)
- `path-segment` — alias matches a discrete path segment, not a
  substring
- `regex` — operational identifiers with regex patterns (e.g.,
  internal account-number formats)
- `co-occurrence` — two below-threshold tokens in the same window
  emit a critical finding (the triangulation case — `<client> +
<codename>` together is much worse than either alone)

Severity composition combines (a) catalog default, (b) entity-level
override, (c) visibility-scope adjustment. See
[`docs/design/architecture.md`](docs/design/architecture.md) §Severity.

Fingerprints are HMAC-SHA256 over (normalized canonical form +
fingerprint_salt + entity_id). They're stable across scans (same
content → same fingerprint), alias-free (you can publish them safely),
and let consumers de-duplicate or trace recurrence.

#### `pkg/extractor` — Surfaces

The extractor abstracts how content reaches the engine. Each surface
emits `InputUnit{Content, Path, SourceKind, SourceID, Metadata}`
records to a channel.

- `FilesystemExtractor` — walks a directory, bounded worker pool,
  size-cap per file, binary skip, skip-events for diagnostic
- `StagedExtractor` — reads `git show :path` for each entry in the
  git index. This is what makes pre-commit gates **sound**: it sees
  what's about to be committed even if the working tree was edited
  after `git add`. Detail in
  [`docs/design/architecture.md`](docs/design/architecture.md) §Extractor.
- Stdin extractor for `--branch-name -` and `--commit-msg -` (small;
  inline in `scan.go`)

#### `pkg/output` — Zero-leak boundary

The **invariant**: no protected substring appears in any byte limensafe
emits — stdout, stderr, JSON payload, finding IDs, fingerprint
inputs, log lines, debug output. **All emitted strings pass through
`Redactor.Redact()` at the formatter boundary.** This is the core
guarantee that makes limensafe trustable for AI-agent contexts where
the agent might log its own observations.

The Redactor is an Aho-Corasick state machine built from the merged
alias set across all loaded catalogs. The JSONFormatter is the single
output writer; every other code path that wants to emit something
routes through it.

The zero-leak invariant is locked in [ADR-0003](docs/decisions/ADR-0003-redaction-safe-output.md)
and verified by `TestRedactor_ZeroLeak_SyntheticAcmeAliases` plus the
T1–T9 acceptance corpus tests.

#### `internal/appid` — Self-identification

Thin wrapper over `gofulmen/appidentity` for the binary's
self-identification (`limensafe version`, log service name, envinfo).
~10 LoC plus an `init()` that registers the embedded identity blob
from `internal/assets/appidentity/`.

Earlier in the v0.0.3 cycle this file briefly carried a local
workaround for a gofulmen precedence bug (partner-integration devlead,
2026-05-08) where CWD ancestor search shadowed the embedded identity
when limensafe ran inside a foreign workhorse's tree. **Fixed at the
gofulmen layer in v0.3.5** (2026-05-12); the workaround was removed
before v0.0.3 tagged. Regression test
`TestGet_EmbeddedIdentityWinsOverForeignCWD` exercises the now-fixed
code path so the systemic fix can't silently regress.

## Design decisions and rationale

Six locked decisions you'll inherit. Two have full ADRs in
`docs/decisions/`; the other four are documented across design docs
and embedded in the code. All six are summarized below.

### 1. Zero-leak output invariant ([ADR-0003](docs/decisions/ADR-0003-redaction-safe-output.md))

**Decision**: The scanner's own output (every byte across every
stream and intermediate ID) must not contain any protected substring.

**Why**: A leak scanner that leaks defeats its purpose. The most
hostile failure mode is "scanner finds the codename, then echoes it
into the scan report which gets PRed into a public repo." This
invariant prevents that by construction. The Redactor sits at the
JSONFormatter boundary; all emitted strings flow through it.

**Implication for you**: any new emit path (a new subcommand, a
debug print, a metric label, an error message that quotes user
content) MUST route through the formatter or call `Redactor.Redact()`
on its content. Plain `fmt.Println(userContent)` is forbidden.

### 2. Two-layer catalog (repo config + out-of-band vocabulary)

**Decision**: The repo's `.limensafe/config.yaml` contains only
visibility, policy, and **opaque catalog references by ID**. Raw
protected vocabulary lives **outside** the repo — local files,
env-injected paths, or vendored builtin baselines.

**Why**: Solves the chicken-and-egg "how do I reference protected
vocab without storing it in the repo." Reads like a real production
setup: vendor catalogs distributed via secret managers, env-injected
paths in CI, baseline builtin for hygiene.

**Implication for you**: when designing new features that touch
catalog identity (UI, dashboard, log lines), use the opaque ID
(`limensafe-public-baseline-v0`), never the raw aliases inside it.

### 3. ID-safety rule

**Decision**: Catalog IDs, entity IDs, rule IDs, replacement IDs must
**never contain any alias substring** of the entity they identify.

**Why**: Output IDs need to be safe to emit. If an entity for
`acme-platform` had ID `e-acme-platform-1`, emitting that ID is a
leak. So the convention is opaque sequential / hash-derived IDs:
`e-client-1`, `e-codename-1`, etc. Catalog `e-` prefix is the
convention.

**Implication for you**: catalog authors are guided in the YAML
schema docs; the validator enforces this rule at load time. Don't
weaken it.

### 4. Scope-adaptive severity

**Decision**: Repo visibility (`public_oss` / `unlisted_oss` /
`internal` / `engagement_private` / `local_only`) drives the severity
floor for findings. An alias that's `medium` severity in
`engagement_private` becomes `critical` in `public_oss`.

**Why**: A codename that's fine inside the engagement repo is
disastrous in the public OSS release. The scanner should apply
different bars to different distribution surfaces. `allowed_in` on
each catalog entity lets sanctioned codenames live safely in
designated scopes.

**Implication for you**: severity is composed, not stored. The
`visibility` flag is always required for a scan (defaults to
`public_oss` for safety). Don't bypass.

### 5. Co-occurrence rules

**Decision**: Two below-threshold tokens in the same window emit a
`critical` finding even if each individual token would be a
`medium` or `warn`-only finding.

**Why**: Triangulation. `acme` alone in a public test fixture is
probably fine (the term is generic). `horizon` alone might be fine
(it's a common english word). `acme` AND `horizon` in the same file
within a 100-line window is a confirmed engagement-identity leak.
The triangulation case dominates real-world leaks.

**Implication for you**: when implementing `--git-archive` or similar,
preserve the window-based co-occurrence path. Don't optimize away
the slower second-pass.

### 6. CGO=0 (design pillar)

**Decision**: limensafe is pure Go. No cgo dependencies.

**Why**: Cross-compile to 6 platforms from a single Linux/x86_64
runner. No host glibc/musl drama. No build-time surprises in the
release pipeline. Statically-linked binaries that drop into any
container or hook environment.

**Implication for you**: NER (v0.1.0 directional) needs an out-of-
process sidecar (Presidio) or pure-Go ONNX runtime, not a cgo binding
into libtorch / spaCy / etc.

## v0 timeline (what happened and why)

A compressed account so you have the context for why things are the
way they are. Full detail in [`CHANGELOG.md`](CHANGELOG.md), in
`#solution-planning-context-leakage` Mattermost history, and in the
the internal productbook entry.

| Date           | Event                                                                                                  |
| -------------- | ------------------------------------------------------------------------------------------------------ |
| 2026-04-29     | CDRL from forge-workhorse-groningen — `v0.0.1` seed                                                    |
| 2026-04-29 …   | v0 spike: engine + extractor + redactor + catalog + zero-leak invariant + acceptance corpus            |
| 2026-05-01     | Renamed `contextsafe` → `limensafe` (namelens after collision with GSAP plugin + `contextsafe.es`)     |
| 2026-05-04     | `kind: builtin` source + vendored public-baseline catalog; `--staged` extractor for sound pre-commit   |
| 2026-05-04     | DataWidget (partner-integration) picks limensafe as integration target; devlead recommends Option A config  |
| 2026-05-06     | v0.0.2 published privately at `fulmenhq/limensafe`; 18 commits, ~75 tests, all 9 acceptance tests pass |
| 2026-05-08     | partner-integration live-validation by devlead → identity-shadow bug + concrete CI-contract feedback          |
| 2026-05-08 +   | v0.0.3 cycle: identity-shadow fix (workaround), CICD, signing, 5-platform, exit-code contract, docs    |
| 2026-05-12     | gofulmen v0.3.5 ships precedence reorder (a sibling team) — bundled with separate datawidget fix          |
| 2026-05-16     | limensafe repins gofulmen v0.3.5; local workaround removed; v0.0.3 ready to tag                        |
| (post-handoff) | the maintainer team owns from v0.0.4                                                                             |

Two recurring themes you'll see in commit history:

- **"Sanitize the project itself before it goes anywhere"** —
  pre-publication sanitization sweeps were a real activity. The
  scanner deliberately catches its own catalog YAML if pointed at it
  without exclusions. This is why `dogfood-scan` is v0.0.4 work, not
  v0.0.3.
- **"Output is the boundary"** — the zero-leak invariant has caused
  several rewrites. Every emit path was re-examined. The Redactor
  exists because earlier output paths had been quoting user content
  directly.

## Beta-tester relationships

### partner-integration / DataWidget / devlead

**The primary integration partner.** DataWidget is a fulmenhq
data-products tool (india team / data products cluster). partner-integration is
their integration brief for adopting limensafe as the pre-commit /
CI gate.

- **Live validation 2026-05-08**: devlead ran v0.0.2 against
  DataWidget's synthetic-acme corpus + their own private
  engagement catalog. Core scanner behavior assented; all 10
  expected blocking findings caught; sentinel surfaces (branch-name,
  commit-msg, staged-index) all gated correctly.
- **Caveats that drove v0.0.3 work**: the identity-shadow bug + the
  layered-config-defaults warning when running limensafe from inside
  the datawidget tree. Identity-shadow fixed at the gofulmen layer
  in v0.3.5 (limensafe v0.0.3 ships against the fixed version);
  layered-config-defaults is a separate gofulmen bug still on the
  v0.0.4 watch list — see roadmap.
- **Config-shape recommendation**: devlead landed on Option A
  (builtin baseline + optional env-injected private catalog). This
  is the recommended pattern in the README's CI Integration section.

**Standing arrangement**:

- partner-integration PR (in datawidget) will land against v0.0.3+ — react to
  india's PR when it opens via `the brief channel`
- devlead is your concrete reference for "is this UX
  improvement worth shipping?" — they've used the tool in anger
- Their feedback inputs to v0.0.4 (mode-aware missing-private-config,
  `profile doctor` UX, layered private-org/engagement/repo catalogs)
  are documented in [`docs/roadmap.md`](docs/roadmap.md)

### Synthetic acceptance corpus (synthetic-acme)

`testdata/synthetic-acme/` is the v0 acceptance corpus — synthetic
client (`acme` placeholder), codename (`horizon` placeholder), and
sanctioned codename (`tilden` placeholder, `allowed_in:
[engagement_private, internal]`). 9 acceptance tests (T1–T9) cover
the design's invariants. Treat this as the regression bar: if you
touch the engine, run the synthetic-acme corpus and confirm all 9
still pass with expected severities.

These placeholders are NOT real organizational vocabulary — they're
synthetic, and they live in the public-tier repo. The real catalogs
(`engagement-alpha-2026-q2.yaml` etc.) live outside this repo, per
the two-layer catalog rule.

## Open questions you're inheriting

These are decisions that were deliberately NOT made in v0 because they
could wait for adoption signal. Now your call.

### Q1. Keep, strip, or refactor the inherited HTTP server?

The groningen template ships an HTTP server (`internal/server/*`) with
`/health`, `/version`, `/metrics` endpoints. limensafe inherits the
code AND exposes it via an inherited `serve` subcommand (registered
in `internal/cmd/serve.go` via `rootCmd.AddCommand(serveCmd)`). The v0
cycle deliberately did not invest in the surface: no UX pass, no
documentation, no release-gating around it, no examples in the README.

**Question**: do you keep the server and invest in real UX around it
(docs, release-gating, "limensafe-as-a-service" / dashboard use cases),
strip both the package and the subcommand, or refactor into a separate
companion (`limensafe-server`)?

**Context**: stripping it cleans up ~600 LoC of unexercised code, the
flaky integration test, and the inherited `serve` subcommand surface.
Keeping it leaves the door open for a control-plane or dashboard
surface in v1+ but requires real maintenance investment. Refbolt made
the same call and kept the server stubs; goneat removed them.

### Q2. Output format: human-readable mode?

Today `--format human` is documented but emits JSON regardless. Should
v0.0.x ship a real human-readable formatter (tables, color, summary
counts)?

**Context**: devlead's CI contract is JSON-on-stdout, so the
default has to stay JSON. But a `--format human` mode would make
local-developer use friendlier. The JSONFormatter abstraction is
already in place; adding a HumanFormatter is ~150 LoC and would slot
in cleanly.

### Q3. Catalog distribution / control plane (v1.0 directional)

The L4 control plane sketched in early design docs is deliberately
deferred. Two trigger conditions for picking it up:

- Multi-org / multi-tenant adoption pull (e.g., a consultancy with
  20+ engagements all needing per-engagement catalog distribution)
- Need for catalog signing + integrity verification at distribution
  time (today it's "trust the file you copied")

Neither is urgent for v0.x. Track signals in `the internal coordination channel`.

### Q4. `--strict` / lockdown mode

Should `limensafe scan --strict` reject ANY non-zero finding count
regardless of decision/severity? Today the block threshold drives
exit-1; lower-severity findings still emit but exit 0. Some CI
contexts (e.g., release-pipeline pre-publish gates) want "literally
zero findings or fail."

**Context**: trivial to implement (flag → adjust block_threshold to
`info` or whatever). The design question is whether `--strict` is
clearer than asking users to override `policy.block_threshold` in
their config.

### Q5. Catalog testing helpers

Catalog authors today have no `make` target for "validate my catalog
YAML + run synthetic test inputs through it." `pkg/catalog` has a
loader test suite but it's testing the loader, not consumers'
catalogs.

**Question**: do you ship a `limensafe catalog validate` subcommand
and/or a `make catalog-test` target?

**Context**: devlead asked for this implicitly via the
"profile doctor" UX request. A first-class `catalog validate` is
arguably the simplest version of profile-doctor.

## Backlog priorities

The firm v0.0.4 scope is in [`docs/roadmap.md`](docs/roadmap.md). What
follows is the **prioritization rationale** so you can re-sequence
without losing context.

### Tier 1 (do these in v0.0.4)

1. **`.limensafeignore`** — unlocks `dogfood-scan` (which gates v0.0.4
   release CI on the tool eating its own dog food). Also fixes the
   "scanning a project that contains its own catalog YAMLs" papercut
   that bites adopters in week 1.
2. **`policy.block_threshold` from config** — landed in the v0.0.5
   wave. Users can now set medium as the gate, and release mode uses
   that tighter posture.
3. **`--git-archive HEAD`** — landed in the v0.0.5 wave; turns a
   5-line Makefile recipe into a 1-line invocation.
4. **Mode-aware missing-private-config** — landed in the v0.0.5 wave.
   India-devlead's partner-integration input is now covered by explicit local/CI/
   release modes plus `--private-catalog-missing` override.

### Tier 2 (do these in v0.0.4 if capacity, else v0.0.5)

5. **Workhorse HTTP server cleanup** — decision pending Q1 above.
   Either way, don't ship v0.1.0 with the flaky test still in CI.
6. **`limensafe profile doctor`** — UX win, depends on Q5 decision.

### Tier 3 (v0.0.5+ unless adoption signal accelerates)

7. **Performance baseline / profiling** — current 222ms-on-Hugo is
   fine, but as catalogs grow, the Aho-Corasick automaton grows
   too. Worth baselining before adopters complain.
8. **Multi-arch container image** — only matters if limensafe ships
   in CI runner images. Wait for adoption signal.
9. **De-dup mode** — only matters when hooks become noisy in practice.

### Tier 4 (v0.1.0 directional)

10. NER plug-in (Presidio sidecar or ONNX runtime)
11. Additional extractors (notebook outputs, office docs, AI artifacts)
12. Public release prep + prodmktg pass

## First-week checklist

A suggested onboarding tour. Treat as advisory.

- [ ] Read this file (you're doing it). Time-box: 60 minutes.
- [ ] `git clone` and run `make bootstrap`, `make build`, `make test`,
      `make check-all`. Confirm everything green on your machine.
- [ ] Run `make bootstrap-smoke` and `make perf-smoke` (the latter
      will warn-skip if `PERF_SMOKE_ROOT` doesn't exist; set it to
      any large local repo to see real timings).
- [ ] Read [`README.md`](README.md) front-to-back (you're the next
      maintainer; you need to know what users see).
- [ ] Read [`docs/design/problem-statement.md`](docs/design/problem-statement.md)
      and [`docs/design/architecture.md`](docs/design/architecture.md).
      Time-box: 60–90 minutes combined.
- [ ] Read [`ADR-0003`](docs/decisions/ADR-0003-redaction-safe-output.md).
      This is the load-bearing decision; everything else flows from it.
- [ ] Walk through `internal/cmd/scan.go` start-to-end. It's the
      central nervous system. ~500 LoC. Trace one happy-path call
      (e.g., `scan testdata/builtin-baseline --config-file ...`) all
      the way through extractor → engine → output.
- [ ] Read [`CONTRIBUTING.md`](CONTRIBUTING.md) §Scan CLI contract.
      Internalize the sentinel-error pattern; you'll add to it.
- [ ] Skim `the internal coordination channel` Mattermost history. Most decisions
      from the v0.0.3 cycle are captured there.
- [ ] Read the partner-integration thread in `the brief channel` for devlead's
      live-validation feedback (the source-of-truth for v0.0.4 UX
      asks).
- [ ] (Historical — closed 2026-05-16.) The v0.0.3 cycle carried a
      gofulmen-v0.3.5 pre-tag gate that has since cleared. Your first
      cycle-1 PR is whatever you pick up from the internal-brief..internal-brief
      backlog, not infrastructure cleanup.

## Canonical references

Most useful when you're a few weeks in and need to find something:

| Reference                                                                                                              | What you'll find                                                   |
| ---------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| [`README.md`](README.md)                                                                                               | User-facing overview, CI integration patterns, scan contract       |
| [`CONTRIBUTING.md`](CONTRIBUTING.md)                                                                                   | Build/test/lint, scan-contract DO-NOT-BREAK, commit standard       |
| [`MAINTAINERS.md`](MAINTAINERS.md)                                                                                     | Ownership, agent handles, channels, escalation                     |
| [`CHANGELOG.md`](CHANGELOG.md)                                                                                         | Version history with rationale per release                         |
| [`docs/roadmap.md`](docs/roadmap.md)                                                                                   | v0.0.4 firm → v0.1.0 directional                                   |
| [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md)                                                                         | Release process; goneat is canonical signing reference             |
| [`docs/design/problem-statement.md`](docs/design/problem-statement.md)                                                 | Why this tool exists; what gap it fills                            |
| [`docs/design/architecture.md`](docs/design/architecture.md)                                                           | Component design; severity composition; extractor model            |
| [`docs/design/catalog-schema.md`](docs/design/catalog-schema.md)                                                       | Catalog YAML reference                                             |
| [`docs/design/existing-tools-gap.md`](docs/design/existing-tools-gap.md)                                               | Positioning vs gitleaks, Presidio, etc.                            |
| [`docs/decisions/ADR-0003-redaction-safe-output.md`](docs/decisions/ADR-0003-redaction-safe-output.md)                 | Zero-leak invariant rationale + implementation contract            |
| [`docs/decisions/ADR-0004-schema-validation-multi-draft.md`](docs/decisions/ADR-0004-schema-validation-multi-draft.md) | Schema validation across JSON Schema draft versions                |
| [`scripts/bootstrap-smoke.sh`](scripts/bootstrap-smoke.sh)                                                             | The 5-check end-to-end smoke (india's spec)                        |
| [`test/integration/scan_exit_codes_test.go`](test/integration/scan_exit_codes_test.go)                                 | Locks the 4-way exit code + stream-separation contract             |
| `~/dev/goneat/RELEASE_CHECKLIST.md`                                                                           | Canonical fulmenhq signing flow; org-level key conventions         |
| `~/dev/the internal productbook/content/projmgmt/limensafe/`                                             | Productbook entry; team-assignment matrix; brief tracking          |
| `the internal coordination channel` (Mattermost)                                                                                     | Persistent ops channel; status broadcasts; cross-cutting decisions |
| `the brief channel` (Mattermost)                                                                                           | DataWidget integration beta-test channel; india's feedback       |
| `the team channel` (Mattermost)                                                                                              | Your home channel; broader the maintainer team cluster context                    |

---

## Closing

Welcome to limensafe. The v0 surface is small enough that you can hold
it in your head after a focused week, the design is locked enough that
your changes will compose cleanly, and the beta-tester relationship is
warm enough that you'll get fast feedback on v0.0.4 work.

cxotech and entarch remain on standby in
`the internal coordination channel` for v0 context questions during your first cycle.
Don't hesitate to ping for "why did you decide X?" — the decisions
shipped, but the rationale lives in our heads (and now, in this file).

Build something users can rely on. The point of this tool is to make
the safe path mechanical instead of memory-based. Hold that line.

— cxotech, on behalf of the v0 build crew
2026-05-14

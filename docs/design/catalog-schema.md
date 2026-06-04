# Catalog and Config Schema

Status: review-ready draft
Owner: cxotech
Last updated: 2026-04-29

## Scope

This document specifies the **two-layer schema model** for `limensafe`:

1. **Vocabulary bundles** — private files containing raw protected entity
   data. Never live in public repositories.
2. **Repo config** — public/repo-safe files that reference catalogs by ID
   and declare repo-local policy. Live in the repository.

The schemas are **JSON Schema 2020-12**. This document describes the model
in tables and YAML examples; the formal schemas will be authored at
`schemas/v1/catalog.schema.json` and `schemas/v1/config.schema.json` once
the council approves the model. Schemas are intended for hosting alongside
crucible (the fulmenhq SSOT for schemas/standards).

All examples in this document use **synthetic placeholder names** per the
corpus rule. No real protected vocabulary appears here.

## Two-Layer Conceptual Model

```text
                    ┌──────────────────────────────────┐
                    │     Vocabulary Bundle (PRIVATE)   │
                    │                                   │
                    │  - entities (raw protected data)  │
                    │  - aliases, codenames, variants   │
                    │  - co-occurrence rules            │
                    │  - replacement suggestions        │
                    │                                   │
                    │  Lives outside public repos:      │
                    │  - encrypted provisioning bundle  │
                    │  - secret store / vault           │
                    │  - control plane (v2+)            │
                    └────────────────┬─────────────────┘
                                     │  referenced by ID
                                     ▼
                    ┌──────────────────────────────────┐
                    │     Repo Config (PUBLIC-SAFE)     │
                    │                                   │
                    │  - schema version                 │
                    │  - repo identity + visibility     │
                    │  - catalog references (by ID)     │
                    │  - severity overrides             │
                    │  - extractor settings             │
                    │  - .limensafeignore patterns    │
                    │                                   │
                    │  Lives in repo:                   │
                    │  - .limensafe/config.yaml       │
                    │  - .limensafeignore             │
                    └──────────────────────────────────┘
```

**Invariant:** Repo config never contains raw protected vocabulary. The
tool refuses to load a config containing strings that resolve as catalog
entries when the catalog is loaded.

## Vocabulary Bundle Schema

### Top-level structure

| Field                 | Type   | Required | Description                                          |
| --------------------- | ------ | -------- | ---------------------------------------------------- |
| `$schema`             | string | yes      | JSON Schema URL for vocabulary bundle v1             |
| `catalog_id`          | string | yes      | Stable identifier (e.g., `engagement-alpha-2026-q2`) |
| `schema_version`      | string | yes      | Bundle schema semver (e.g., `1.0.0`)                 |
| `description`         | string | no       | Human-readable note (catalog purpose, owner, scope)  |
| `default_severity`    | enum   | no       | Bundle-default severity (overridden per entity)      |
| `entities`            | array  | yes      | List of `Entity` records                             |
| `co_occurrence_rules` | array  | no       | List of `CoOccurrenceRule` records                   |
| `fingerprint_salt`    | string | no       | Salt for HMAC fingerprinting (see `architecture.md`) |

### Entity record

| Field                    | Type   | Required | Description                                                                                                                                                                                   |
| ------------------------ | ------ | -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                     | string | yes      | Unique within catalog. Stable. Used by config for ID-based reference.                                                                                                                         |
| `class`                  | enum   | yes      | `client_identity` \| `codename` \| `person` \| `project` \| `system` \| `hostname` \| `account_label` \| `operational_pattern`                                                                |
| `aliases`                | array  | yes      | Raw protected strings to match. Min length 1.                                                                                                                                                 |
| `variants`               | object | no       | Auto-generation and matching flags: `case_insensitive`, `slug`, `pluralize`, `path_segments`, `whole_word`                                                                                    |
| `tokens`                 | array  | no       | Additional discrete tokens to match exactly                                                                                                                                                   |
| `regex_patterns`         | array  | no       | Regex rules for operational identifiers (e.g., internal account number formats)                                                                                                               |
| `replacement_for`        | string | no       | Catalog ID of the entity this entry substitutes for (used by sanctioned codenames)                                                                                                            |
| `replacement_suggestion` | string | no       | Neutral suggestion shown in findings (e.g., `tenant-1`, `profile-a`)                                                                                                                          |
| `allowed_in`             | array  | no       | Visibility scopes where this entry may appear without finding. Default: empty.                                                                                                                |
| `blocked_in`             | array  | no       | Visibility scopes where this entry must not appear. Default: all scopes if absent.                                                                                                            |
| `visibility_scope`       | enum   | no       | Sharing boundary of the protected entity itself                                                                                                                                               |
| `severity_override`      | enum   | no       | Overrides bundle/scope-derived severity                                                                                                                                                       |
| `disclosure_safe`        | bool   | no       | Default `false`. Set to `true` only as a deliberate decision that this entity may live in a public-tier catalog (e.g., already-disclosed historical leak terms after explicit policy review). |
| `notes`                  | string | no       | Catalog-author note. Not exposed in findings.                                                                                                                                                 |

### Visibility scope enum (proposed)

| Scope                | Meaning                                                        |
| -------------------- | -------------------------------------------------------------- |
| `public_oss`         | Published OSS, indexed, training-scrape exposed                |
| `unlisted_oss`       | Public host but unpromoted (one config flip from `public_oss`) |
| `internal`           | Within the firm; not externally visible                        |
| `engagement_private` | Within a specific client engagement (tighter than internal)    |
| `local_only`         | Never committed (planning, scratch, `.gitignored`)             |

**Open question for council:** retain all five, or collapse to three (`public`/`internal`/`private`)? The richer enum gives more policy granularity at the cost of more configuration to author.

### Variant flags

| Field              | Type | Default                                                         | Description                                                                                                                                          |
| ------------------ | ---- | --------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `case_insensitive` | bool | `false`                                                         | Match aliases regardless of case.                                                                                                                    |
| `slug`             | bool | `false`                                                         | Generate slug-form variants for aliases.                                                                                                             |
| `pluralize`        | bool | `false`                                                         | Generate simple plural forms for aliases.                                                                                                            |
| `path_segments`    | bool | `false`                                                         | Indicates aliases are meaningful as path segments.                                                                                                   |
| `whole_word`       | bool | `true` for aliases shorter than 4 characters; otherwise `false` | Require matches to be bounded by start/end of input or non-word characters. Explicit `false` opts short aliases back into legacy substring matching. |

`whole_word` exists for short-acronym catalogs where aliases such as
`ILT` or `CBT` would otherwise match inside unrelated words or lockfile
hashes. The default remains substring matching for aliases with four or
more characters to preserve existing catalog behavior. Catalog authors
can still set `whole_word: true` on longer aliases when discrete-token
matching is desired.

Catalog load emits a non-fatal warning when `whole_word: true` is paired
with `case_insensitive: true`: the combination is supported, but authors
should verify they intended boundaries to apply to the lowercased token.

### Severity enum

| Severity   | Default behavior                                 |
| ---------- | ------------------------------------------------ |
| `critical` | CI-block; alert; require redaction before merge  |
| `high`     | CI-block by default; can be downgraded by policy |
| `medium`   | Warning; visible in CI output; non-blocking      |
| `low`      | Informational; visible in scan summary           |
| `info`     | Hygiene; not surfaced unless verbose             |

### Co-occurrence rule record

| Field               | Type   | Required    | Description                                                           |
| ------------------- | ------ | ----------- | --------------------------------------------------------------------- |
| `rule_id`           | string | yes         | Unique within catalog                                                 |
| `description`       | string | no          | Human-readable rationale (catalog-private, not in findings)           |
| `terms`             | array  | yes         | Two or more entity IDs whose co-occurrence triggers the rule          |
| `window_kind`       | enum   | yes         | `file` \| `hunk` \| `identifier` \| `near_n_chars` \| `near_n_tokens` |
| `window_size`       | int    | conditional | Required when window*kind is `near_n*\*`                              |
| `severity_override` | enum   | yes         | Severity to apply when the rule fires                                 |
| `apply_in`          | array  | no          | Visibility scopes where the rule applies. Default: all.               |

### Vocabulary bundle YAML example (synthetic)

```yaml
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"
catalog_id: engagement-alpha-2026-q2
schema_version: "1.0.0"
description: |
  Synthetic example. Replace before use.
  Owner: <engagement-lead>. Distribution: private vault only.
default_severity: high
fingerprint_salt: "${env:LIMENSAFE_CATALOG_SALT}"

entities:
  - id: client-alpha
    class: client_identity
    aliases:
      - "Alpha Industries"
      - "AlphaCorp"
      - "alpha-industries"
    variants:
      case_insensitive: true
      slug: true
      path_segments: true
    blocked_in: [public_oss, unlisted_oss, internal]
    severity_override: critical
    replacement_suggestion: "tenant-1"

  - id: project-willow
    class: codename
    aliases: ["willow", "project-willow", "WillowDB"]
    variants:
      case_insensitive: true
      slug: true
    blocked_in: [public_oss, unlisted_oss]
    severity_override: high
    replacement_suggestion: "internal-project-a"

  - id: codename-tilden
    class: codename
    aliases: ["tilden", "tilden-svc", "tilden_db"]
    replacement_for: client-alpha
    allowed_in: [engagement_private, internal]
    blocked_in: [public_oss, unlisted_oss]
    replacement_suggestion: "tenant-1"
    notes: |
      Sanctioned opaque substitute for client-alpha. Acceptable in private
      engagement repos but a public breadcrumb in OSS.

  - id: person-voss
    class: person
    aliases: ["Lara Voss", "L. Voss", "lara.voss"]
    blocked_in: [public_oss, unlisted_oss, internal]
    severity_override: critical

  - id: host-staging-eu1
    class: hostname
    regex_patterns:
      - '^staging-eu\d+\.alphacorp\.example$'
    blocked_in: [public_oss, unlisted_oss]
    severity_override: high

  - id: format-token-1
    class: operational_pattern
    aliases: ["ILT", "CBT"]
    variants:
      whole_word: true
    blocked_in: [public_oss, unlisted_oss]
    severity_override: high

co_occurrence_rules:
  - rule_id: alpha-willow-triangulation
    description: |
      client-alpha + project-willow together attests engagement existence
      and project codename in the same artifact.
    terms: [client-alpha, project-willow]
    window_kind: file
    severity_override: critical
    apply_in: [public_oss, unlisted_oss, internal]
```

## Repo Config Schema

### Top-level structure

| Field                | Type   | Required | Description                                          |
| -------------------- | ------ | -------- | ---------------------------------------------------- |
| `$schema`            | string | yes      | JSON Schema URL for repo config v1                   |
| `schema_version`     | string | yes      | Config schema semver                                 |
| `repo`               | object | yes      | `RepoIdentity` record                                |
| `policy`             | object | no       | `Policy` record (defaults if omitted)                |
| `catalogs`           | array  | yes      | `CatalogReference` records (min length 1)            |
| `severity_overrides` | array  | no       | `SeverityOverride` records (per-pattern adjustments) |
| `extractors`         | object | no       | Per-extractor configuration                          |

### Repo identity record

| Field           | Type   | Required | Description                                       |
| --------------- | ------ | -------- | ------------------------------------------------- |
| `id`            | string | yes      | Stable repo identifier (e.g., `org/example-repo`) |
| `visibility`    | enum   | yes      | One of the visibility scopes                      |
| `engagement_id` | string | no       | If repo is engagement-scoped, the engagement ID   |
| `description`   | string | no       | Human-readable note                               |

### Catalog reference record

A catalog reference must not embed catalog content. It points to a
resolvable source.

| Field        | Type   | Required | Description                                              |
| ------------ | ------ | -------- | -------------------------------------------------------- |
| `catalog_id` | string | yes      | Must match a `catalog_id` in the resolved bundle         |
| `source`     | object | yes      | `CatalogSource` record (one of: file, env, url, profile) |
| `optional`   | bool   | no       | If true, missing source produces warning, not error      |

`CatalogSource` is a tagged union:

```yaml
# File source
source:
  kind: file
  path: ~/.config/limensafe/catalogs/engagement-alpha-2026-q2.yaml

# Env source (path lives in env var)
source:
  kind: env
  var: LIMENSAFE_CATALOG_PATH

# URL source (control plane / signed bundle, v2+)
source:
  kind: url
  url: https://catalog.limensafe.example/v1/engagement-alpha-2026-q2
  auth: { kind: bearer, env: LIMENSAFE_CATALOG_TOKEN }

# Profile source (named profile in user-level config)
source:
  kind: profile
  name: alpha-2026-q2
```

### Policy record

| Field                            | Type  | Required | Description                                                  |
| -------------------------------- | ----- | -------- | ------------------------------------------------------------ |
| `default_severity`               | enum  | no       | Default if catalog and overrides are silent                  |
| `block_threshold`                | enum  | no       | Severity at or above which scanner exits non-zero (CI block) |
| `private_catalog_missing`        | enum  | no       | `silent`, `warn`, or `error` for absent optional catalogs    |
| `require_replacement_suggestion` | bool  | no       | If true, refuse to autofix without a catalog suggestion      |
| `redaction_safe_output`          | bool  | no       | Default: true. Verbose mode requires explicit flag.          |
| `co_occurrence_enabled`          | bool  | no       | Default: true                                                |
| `extractor_allow_list`           | array | no       | If set, only these extractors run                            |

### Severity override record

| Field      | Type   | Required | Description                                                       |
| ---------- | ------ | -------- | ----------------------------------------------------------------- |
| `pattern`  | string | yes      | Doublestar glob matching path or surface                          |
| `surface`  | enum   | no       | `content` \| `path` \| `metadata` \| `branch` \| `commit_message` |
| `severity` | enum   | yes      | Severity to apply                                                 |
| `reason`   | string | no       | Why (catalog-private; not exposed in findings)                    |

### Repo config YAML example (synthetic)

```yaml
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/config.schema.json"
schema_version: "1.0.0"

repo:
  id: org/example-repo
  visibility: public_oss
  description: "OSS Go tool — applies to client analytics surfaces"

policy:
  default_severity: medium
  block_threshold: high
  private_catalog_missing: warn
  redaction_safe_output: true
  co_occurrence_enabled: true

catalogs:
  - catalog_id: engagement-alpha-2026-q2
    source:
      kind: env
      var: LIMENSAFE_CATALOG_PATH
    optional: false

severity_overrides:
  - pattern: "internal/doctor/redact_test.go"
    surface: content
    severity: critical
    reason: "Known historical leak site; treat all hits as critical."

  - pattern: "**/*_test.go"
    severity: high
    reason: "Test fixtures are the most common leak surface."

extractors:
  filesystem:
    follow_symlinks: false
  git:
    scan_branch: true
    scan_commit_messages: true
    scan_staged_diff: true
  structured:
    json: true
    yaml: true
    markdown: true
```

`optional` controls whether a missing catalog may be skipped at all.
`policy.private_catalog_missing` controls how loud that skip is:

| `optional` | `private_catalog_missing` | Behavior                                 |
| ---------- | ------------------------- | ---------------------------------------- |
| `false`    | any                       | Scan fails with a config error           |
| `true`     | `silent`                  | Scan succeeds and skips quietly          |
| `true`     | `warn`                    | Scan succeeds with a JSON config warning |
| `true`     | `error`                   | Scan fails with a config error           |

Config-warning records are emitted in `findings[]` with
`kind: "config-warning"` and do not affect `summary.findings_total` or
exit-1 detection gating. Missing-catalog status emits only catalog ID,
source kind, status, and sanitized reason; local paths and env values are
not part of the public output contract.

## Layered Catalog Resolution

`limensafe` resolves catalogs in this order, with **nearest-wins** semantics:

```text
1. Org-level defaults    (~/.config/limensafe/org.yaml)
2. Engagement profile    (~/.config/limensafe/profiles/<name>.yaml)
3. Repo-local config     (.limensafe/config.yaml)
4. Local override        (.limensafe/config.local.yaml — gitignored)
```

**Catalog reference resolution is explicit at every layer.** A repo never
auto-picks up catalogs by directory walk; this prevents raw vocabulary from
landing in repo files. Catalog source kinds (`file`, `env`, `url`, `profile`)
are explicit pointers.

**Merge semantics:**

- Scalar fields (e.g., `default_severity`): nearest layer wins
- Arrays of records keyed by ID (e.g., `catalogs`, `severity_overrides`):
  records in nearer layers replace records with the same key in further
  layers; new records are appended
- Boolean flags: nearest layer wins; explicitly set takes precedence over default

## `.limensafeignore` Semantics

`.limensafeignore` is an operator-convenience filter, not a confidentiality
boundary. The primary discipline is still to keep private vocabulary catalogs
and operator-local artifacts outside the scanned repository tree. For v0.0.4,
limensafe reads only the root-level `.gitignore` and `.limensafeignore` files
at the scan root; nested ignore inheritance is deferred.

Behaves like `.gitignore`, with these v0.0.4 clarifications:

- **Doublestar globs.** `**/*` matches any depth. Helper libraries:
  `gofulmen` (Go), `tsfulmen` (TS), `rsfulmen` (Rust), or upstream
  `gobwas/glob` / `bmatcuk/doublestar`.
- **Root scope.** Only the scan-root `.limensafeignore` is loaded. A future
  release may compose nested ignore files down the tree.
- **Shell-bang re-includes.** A leading `!` re-includes a path previously
  excluded by an earlier root-level pattern.
- **`.gitignore` awareness by default.** `limensafe` honors `.gitignore`
  unless `--include-ignored` is set. This matters for local hygiene scans
  of `.plans/`, scratch logs, and dogfooding artifacts where leaks may
  exist but should not block CI.
- **Skip visibility.** Ignored files emit redacted `scan skip:` diagnostics
  on stderr and increment `scan_metadata.files_skipped_by_reason.ignored`.
  Ignored directory prunes emit stderr diagnostics and increment
  `scan_metadata.directories_skipped`, but they do not inflate file skip
  counts.

### Ignore example

```text
# .limensafeignore

# Convenience filter only. Keep private catalogs outside this repo tree.

# Skip vendored/generated trees
vendor/**
**/dist/**
**/node_modules/**

# Skip large fixture binaries we don't scan
test-fixtures/**/*.bin

# But re-include the synthetic-acme corpus root we *do* scan
!test-fixtures/synthetic-acme/**

# Skip scratch
.plans/**
*.scratch.md
```

## ID Safety Rule

Locked 2026-04-29 per entarch's amendment.

All output-visible identifiers — `catalog_id`, entity `id`, rule
`rule_id`, and `replacement_id` references — **must not contain
protected vocabulary** (i.e., must not match any catalog alias as a
substring under the same case/slug/path-segment normalization rules
the engine applies during detection).

Rationale: IDs flow into JSON output, CI logs, error messages, and
agent contexts. An ID like `client-realname` would re-leak the
protected term every time the scanner reports a finding, defeating
the redaction-safe invariant.

Convention for IDs: use opaque, enumerated, prefix-classified strings
(`e-client-1`, `e-codename-1`, `r-cooccur-1`, `cs-spike-private-v0`).
Human-readable mnemonics belong in the catalog `description` and
entity `notes` fields, which are local-only and never emitted.

Enforcement:

- **v0**: load-time check; reject entity IDs that contain a substring
  matching a literal alias in the same catalog. At scan startup, after
  layered catalogs are loaded, reject output-visible IDs that contain
  any literal alias from the merged loaded alias set. Validation errors
  are sanitized and do not echo unsafe IDs or aliases.
- **v1.x**: extend enforcement across all output-visible IDs and all
  loaded catalogs; require `--allow-unsafe-ids` only for explicit
  operator migration flows if such a bypass is approved.

## CI Integration Patterns

A repo using `limensafe` typically faces a tension: local pre-commit /
pre-push hooks can load a workspace-private catalog (env or profile
source), but **CI cannot** without secret-injection complexity. The
recommended pattern is a **two-tier catalog stack** with the
workspace-private catalog declared `optional`.

### Tier 1 — public catalog (in repo)

`.limensafe/public-catalog.yaml` is committed to the repo. Contents
are safe to disclose (no marginal-disclosure risk):

- Already-disclosed terms — e.g., historical leak strings already
  documented in incident reports. The disclosure has occurred; including
  them in a denylist prevents re-introduction.
- Deprecated path patterns (`/legacy/`, abandoned bucket prefixes).
- Generic format regexes (account number shapes, internal URL prefixes
  in DNS-discoverable space). The regex discloses the _shape_, not any
  specific value.
- Internal Slack-channel / repo-name patterns where the _space_ is
  public knowledge but specific terms aren't.

### Tier 2 — workspace-private catalog (out of repo)

The engagement-private vocabulary lives outside the repo and is loaded
locally by env or profile. CI does not see it.

### Stack them in repo config

```yaml
# .limensafe/config.yaml
repo:
  id: org/repo
  visibility: public_oss

catalogs:
  # Tier 1 — public; loads in CI and locally
  - catalog_id: org-repo-public
    source: { kind: file, path: .limensafe/public-catalog.yaml }
    optional: false

  # Tier 2 — workspace-private; loads locally only
  - catalog_id: engagement-active
    source: { kind: env, var: LIMENSAFE_CATALOG_PATH }
    optional: true
```

Behavior:

- **Local pre-commit / pre-push**: both catalogs load; full coverage
- **CI**: only tier 1 loads; `limensafe` logs `catalog
'engagement-active' not loaded; coverage reduced to public tier only`
  and continues. Public-tier leaks (e.g., re-introduction of historical
  terms) still block CI.
- **Branch protection + reviewer awareness** remain the safety net for
  tier-2-only terms.

### Why not single-catalog with secret injection?

Two structural problems:

- **Rotation/sharing scale poorly.** Each contributor and each CI runner
  needs the catalog. Multi-engagement orgs multiply this. Catalog
  becomes the bottleneck.
- **CI log leakage risk.** A catalog injected into CI may be echoed by
  misconfigured verbose modes, build telemetry, or downstream tooling.
  Keeping high-confidentiality terms out of CI by design eliminates
  this class of failure.

### Why not skip CI?

Local hooks can be bypassed (`--no-verify`). CI is the last automated
gate. Skipping it weakens the defense significantly. The two-tier
pattern preserves CI as a real gate while keeping the most sensitive
vocabulary local-only.

### Adapter pattern for repo `Makefile`

```makefile
sanitize-check:
	limensafe scan --staged --visibility public_oss
```

This is the direct-wiring pattern for repo integration today. A future
goneat assess category (`goneat assess --categories confidentiality`)
will internally call the same scan command; repos already wired
directly continue to work.

## Versioning and Extension

### Schema versioning

- **`schema_version`** is semver
- **Patch bumps** (1.0.0 → 1.0.1): doc/example fixes only
- **Minor bumps** (1.0.0 → 1.1.0): additive optional fields. Consumers
  must ignore unknown fields gracefully.
- **Major bumps** (1.0.0 → 2.0.0): breaking changes. Migration tooling
  required.

### Forward compatibility

- Consumers MUST tolerate unknown fields without erroring
- Unknown enum values produce a warning and fall back to the schema-default
  behavior
- Unknown `class` values produce findings as `class: unknown` rather than
  silently dropping the entity

### Schema locations

```text
schemas/v1/catalog.schema.json   # vocabulary bundle
schemas/v1/config.schema.json    # repo config
schemas/v1/finding.schema.json   # finding output (defined in architecture.md)
```

These will be authored after council approval and submitted to crucible
for cross-language code generation.

## Open Questions

- **Visibility scope enum** — five levels (this doc) or three? Council to decide.
- **Entity class enum** — current proposal includes `client_identity`,
  `codename`, `person`, `project`, `system`, `hostname`, `account_label`,
  `operational_pattern`. Are there missing classes? Should we open-end this
  with a `class: custom; subclass: <string>` escape hatch?
- **Catalog signing** — when v2 control plane lands, do we sign catalogs
  with HMAC, sigstore, or both? V1 trusts the file source.
- **Multi-engagement profiles** — when a developer works on multiple
  engagements simultaneously, the active profile selects the catalog set.
  Is profile selection by env var (`LIMENSAFE_PROFILE=alpha`) sufficient,
  or do we need per-repo binding?
- **`.limensafeinclude` companion file** — needed, or `!` in
  `.limensafeignore` is enough?
- **Catalog conflict** (locked 2026-04-29 per entarch): canonical
  fully-qualified IDs `<catalog_id>:<entity_id>` are the internal
  reference form. Unqualified IDs that collide across loaded catalogs
  produce a load error; the user must qualify or rename. This is
  preferred over silent namespace-prefixing or last-wins.
- **Pre-load validation** — should the tool refuse to run if repo config
  contains a string that resolves as catalog data when the catalog loads?
  This enforces the "no raw vocabulary in repo config" invariant.

## Cross-References

- `problem-statement.md` — class definition, threat model, success criteria
- `architecture.md` — engine, extractor interface, finding model, severity composition
- `existing-tools-gap.md` — why this schema isn't satisfied by existing tools
- [`testdata/synthetic-acme/`](../../testdata/synthetic-acme/) — concrete catalog and config fixtures exercised by the v0 acceptance suite

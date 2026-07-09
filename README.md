# limensafe

> Find the disclosure boundary before private context crosses it.

**Status**: **v0.2.0** — first public release. Working name (Latin _limen_ =
threshold). Release notes:
[`docs/releases/v0.2.0.md`](docs/releases/v0.2.0.md).

`limensafe` is a fast local CLI and Go library for preventing **Confidential
Context Leakage (CCL)**: ordinary-looking names, codenames, paths, branches,
fixtures, docs, logs, and agent artifacts that reveal private organizational
context when they cross into public or shared surfaces. It sits between secret
scanning and DLP — not looking for credentials or regulated PII, but for the
relationship and context clues that organizations define as confidential.

Schema-backed catalogs, repo-safe configuration, redaction-safe output, and
pre-commit / CI workflows make the safe path mechanical instead of memory-based.

**v0.2.0 in brief:** published scan-output and catalog JSON Schemas (runtime-
validated), `catalog build` from a term-list, top-level allowlists with
fail-safe live visibility resolution, `audit-publish` for whole-repo go/no-go
before a visibility flip, and scan reliability for bare mirrors and piped CI
consumers.

## Why

Consulting firms, enterprise R&D teams protecting unannounced product codenames,
and AI-builder organizations all face the same failure mode: in the heat of
getting work done, real client / project / person tokens end up in test fixtures,
sample data, branch names, commit messages, and AI artifacts that ship publicly.
No existing tool combines:

1. Org-specific entity catalogs as a first-class model
2. Sanctioned-codename allowlist with scope policy
3. Co-occurrence rules (`<client> + <codename>` together = critical)
4. Repo visibility severity (`public_oss`, `internal`, `engagement_private`, …)
5. Replacement suggestions
6. Multi-surface coverage (code, paths, branches, commits, docs)
7. **Redaction-safe output — the scanner's own report cannot leak**

`limensafe` does.

## Quick Start

### Prerequisites

- Go 1.25+
- `git` (used for staged-tree and tracked-archive surfaces)

### Build

```bash
git clone https://github.com/fulmenhq/limensafe.git
cd limensafe
make build           # produces ./bin/limensafe
```

### First scan — against the synthetic acceptance corpus

```bash
./bin/limensafe scan ./testdata/synthetic-acme/repo \
  --catalog ./testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml \
  --visibility public_oss
```

Expect: 10 findings (8 critical + 2 high), all paths and metadata
redaction-safe (`<r:e-client-1>` markers replace catalog matches), exit code 1
(decisions include `block`).

### Try the staged-tree surface (pre-commit shape)

```bash
cd $(mktemp -d)
git init -q -b main && git config user.email t@t && git config user.name t
git commit --allow-empty -q -m init
echo 'const Profile = "acme-dev"' > leak.go && git add leak.go

~/path/to/limensafe scan --staged \
  --catalog ~/path/to/limensafe/testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml \
  --visibility public_oss
```

Expect: 1 critical finding for the staged content, exit 1. Editing the working
copy after `git add` does NOT change the result — `--staged` reads the index, not
the working tree.

### Try the tracked-archive surface (CI / pre-push shape)

```bash
limensafe scan --git-archive HEAD \
  --config-file .limensafe/config.yaml \
  --visibility public_oss
```

`--git-archive HEAD` internally extracts Git's tracked tree to a temporary
directory, scans it, and removes the temporary directory. The output metadata
reports `scan_root: "HEAD"` plus `scan_root_kind: "git-archive"` and `git_ref`,
so consumers do not see machine-local temp paths. This excludes ignored local
artifacts (`.git/`, `.gocache/`, `.claude/`) so CI sees only tracked content.
You may also pass an explicit repository path, including a bare mirror:
`limensafe scan /path/to/repo.git --git-archive=HEAD --config-file ...`.

### Try the full-history surface (pre-rewrite audit shape)

```bash
limensafe scan . --git-history-all \
  --config-file .limensafe/config.yaml \
  --visibility public_oss
```

`--git-history-all` scans both historical blobs and historical commit
messages reachable from all refs. Blob content is deduplicated by blob SHA,
then findings are expanded to the commit/path attributions that reference that
blob. Findings report `location.git_ref` as the commit SHA and
`location.surface_kind` as `blob` or `commit_message`. Use this before a
history rewrite; limensafe detects, while remediation tools such as
`git filter-repo` perform the rewrite.

### Audit before going public (the whole publish surface)

`scan` audits **one surface you name**. Before flipping a repo to public you
need the opposite: an audit of **every ref the repository would expose** — all
branches and tags — because the dangerous content is usually on a ref you
aren't looking at (a `backup/*` branch a history rewrite left behind preserves
exactly what was scrubbed).

```bash
# Go/no-go audit of every ref on the remote.
limensafe audit-publish --remote origin \
  --catalog /secure/out-of-tree/my.catalog.yaml --visibility public_oss \
  > publish-audit.json

jq '.summary.publish_safe' publish-audit.json
# false → for each ref in .summary.leak_vector_refs, follow its suggested_action,
# then VERIFY FROM A FRESH CLONE (a working repo can mask refs you pruned
# locally but never pushed the deletion of):
git clone <remote> /tmp/verify && cd /tmp/verify && \
  limensafe audit-publish --remote origin --catalog /secure/.../my.catalog.yaml
```

`audit-publish` flags a ref as a **leak vector** when it carries protected
entities the primary ref does not (`diverges_from_primary`) **or** its name
matches a danger pattern (`backup/*`, `*pre-rewrite*`, `*-snapshot-*`,
`archive/*`, `*-bak`, `wip/*` — `name_pattern`). `summary.publish_safe` is
`true` only when no ref is a leak vector and no ref carries a block-tier
finding. Each leak-vector ref gets a `suggested_action` (e.g.
`git push origin --delete <ref>`). **Detection and advice only — limensafe
never deletes or rewrites refs.** Refs that exist on the remote but are not
fetched locally are still name-pattern checked; run `git fetch --all` first for
full content coverage. Exit codes mirror `scan` (`0` safe, `1` not safe, `2`
config, `3` runtime). It is the top rung of the scope ladder: a PR diff
(`--diff`) ⊂ one ref's history (`--git-history`) ⊂ **all refs**
(`audit-publish`).

### Performance

Scanning Hugo's full tree (2229 files, ~11.8 MB) on a default workstation:

```
duration_ms:    222
worker_count:   12
files_scanned:  2229
files_skipped:  211
findings_total: 18
zero-leak grep: PASS
```

`make perf-smoke` runs this measurement (default `PERF_SMOKE_ROOT=~/dev/playground/hugo`).

## CI Integration Patterns

### Two-tier catalog setup

Repo config (`.limensafe/config.yaml`) declares catalogs by ID. Raw protected
vocabulary lives **outside** the repo. The recommended pattern is builtin
public-safe catalogs plus private env-injected catalogs whose files are not
inside the scanned working tree. `.limensafeignore` is a secondary escape
hatch for legitimate in-tree exclusions; it is not a confidentiality boundary.

```yaml
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/config.schema.json"
schema_version: "1.0.0"

repo:
  id: org/example-repo # opaque; alias-safe per ID-safety rule
  visibility: public_oss

catalogs:
  # Tier 1 — public, builtin. Safe-to-disclose sentinel markers only.
  - catalog_id: limensafe-public-baseline-v0
    source:
      kind: builtin
      name: public-baseline
    optional: false

  # Tier 2 — private, env-injected. Real organization vocabulary.
  # The referenced file lives outside the scanned repo tree.
  - catalog_id: example-private
    source:
      kind: env
      var: LIMENSAFE_CATALOG_PATH
    optional: true

policy:
  default_severity: medium
  block_threshold: high
  private_catalog_missing: warn
  redaction_safe_output: true
  co_occurrence_enabled: true
```

### Makefile recipes

Use the diff surface as the quiet developer hot-path gate, then run tracked-tree
or history sweeps separately when you want a broader audit.

```makefile
# PR / pre-push gate: scans only lines introduced by this branch relative
# to the base ref. Quiet enough to block because pre-existing hits in
# touched files do not fire.
sanitize-check-diff:
	limensafe scan . --diff --diff-base origin/main \
	  --config-file .limensafe/config.yaml \
	  --mode ci \
	  --visibility public_oss

# Pre-push / pr-final / CI: scans tracked content (excludes .gitignored
# files, scratch, build cache). Useful as a sweep over "what ships".
sanitize-check:
	limensafe scan --git-archive HEAD \
	  --config-file .limensafe/config.yaml \
	  --mode ci \
	  --visibility public_oss

# Release/remediation audit: scans historical blobs and commit messages
# reachable from all refs. Use before any git filter-repo rewrite.
sanitize-check-history:
	limensafe scan . --git-history-all \
	  --config-file .limensafe/config.yaml \
	  --mode release \
	  --visibility public_oss

# Pre-commit: scans the staging index (added + modified files).
# Sound for "what's about to be committed" — sees staged content
# even if working tree was re-edited after git add.
sanitize-check-staged:
	limensafe scan --staged \
	  --config-file .limensafe/config.yaml \
	  --mode local \
	  --visibility public_oss
```

Wire `sanitize-check-staged` into `.git/hooks/pre-commit` (or your hook manager
of choice), use `sanitize-check-diff` for PR/pre-push blocking gates, run
`sanitize-check` as a broader tracked-tree sweep, and reserve
`sanitize-check-history` for release/remediation audits where you need every
historical blob and commit-message attribution.

### Local scan attestation

For limensafe development itself, the local proof is
`.limensafe/scan-attestation.json`. It is generated by:

```bash
make limensafe-attest
git commit -m "chore(attest): refresh limensafe scan attestation"
```

`limensafe verify-attestation --mode push` reads the attestation from the
committed `HEAD:.limensafe/scan-attestation.json` blob, not the working tree.
Push mode accepts an attestation bound to `HEAD`, or to `HEAD~1` only when
`HEAD` changes only the attestation file. Tag mode uses the same binding rule
with a shorter freshness window and a known catalog hash check.
When the target is a bare repository, `attest` writes the attestation after a
tracked-tree scan of `HEAD` and does not stage the file.

### Missing private catalog posture

Use `policy.private_catalog_missing` for optional private catalogs declared
in repo config:

| Value    | Behavior                                                         |
| -------- | ---------------------------------------------------------------- |
| `silent` | Current optional behavior: skip absent optional catalogs quietly |
| `warn`   | Succeed and emit a `kind: "config-warning"` JSON finding         |
| `error`  | Fail closed with scan exit code 2                                |

`optional: false` still dominates and fails when the catalog is absent.
`--mode local` defaults to `warn`; `--mode ci` and `--mode release` default
to `error`. The explicit `--private-catalog-missing` flag wins over both
config and `--mode`.

Detection findings include `entity_id` so downstream consumers can group
findings for the same protected entity without seeing matched text. Entity IDs
are opaque catalog identifiers such as `e-client-1`; catalog loading rejects
entity IDs that contain protected alias substrings, and layered scans reject
output-visible IDs that collide with the merged loaded alias set.

For educational fallback or debugging, the equivalent manual shape is:

```bash
TMPDIR=$(mktemp -d) && trap "rm -rf $TMPDIR" EXIT
git archive HEAD | tar -x -C "$TMPDIR"
limensafe scan "$TMPDIR" \
  --config-file .limensafe/config.yaml \
  --mode ci \
  --visibility public_oss
```

For limensafe development itself, use `make fmt` as the mutating formatter and
`make format-check` as the verify-only formatter that matches CI's
`goneat format --check`. Before pushing a PR branch, run `make prepush`; before
requesting final review, run `make pr-final`.

For working-tree dev-loop scans, `limensafe scan .` honors root-level
`.gitignore` and `.limensafeignore` files by default. Use this only for
generated mirrors, build outputs, fixtures, and docs examples that are valid
to keep in-tree but should not be scanned every time. Use `--include-ignored`
when you deliberately want a local hygiene scan over those skipped paths.

### Scan CLI contract

The `scan` subcommand exposes a stable contract designed for CI wrappers:

**Exit codes**

| Code | Meaning                                                                                                         |
| ---- | --------------------------------------------------------------------------------------------------------------- |
| `0`  | scan succeeded; no findings at or above block threshold                                                         |
| `1`  | scan succeeded; one or more findings have `decision=block`                                                      |
| `2`  | config / catalog validation error (bad flag combo, missing/malformed config or catalog YAML, no catalog loaded) |
| `3`  | runtime error (filesystem I/O, extractor init failure, stdin read failure, stdout write failure)                |

**Output streams**

- `stdout` — scan-result JSON (the formal output payload). Always parseable
  even on exit `1` (blocking findings). Never mixed with log lines.
- `stderr` — diagnostics and progress. Empty in non-verbose happy-path runs.
  Skip events, catalog warnings, verbose (`-v`) DEBUG/INFO lines, and fatal
  config/runtime error prefixes are written here. Skip event paths/details are
  redacted before emission.

This separation lets CI wrappers `tee`/archive the stdout JSON without
filtering log lines:

```bash
limensafe scan . \
  --config-file .limensafe/config.yaml \
  --visibility public_oss \
  > scan-result.json 2> scan-diagnostics.log
case $? in
  0) echo "clean" ;;
  1) echo "blocking findings — see scan-result.json"; cat scan-result.json | jq '.findings' ;;
  2) echo "config error — see scan-diagnostics.log"; cat scan-diagnostics.log ;;
  3) echo "runtime error — see scan-diagnostics.log"; cat scan-diagnostics.log ;;
esac
```

Locked by integration tests in `test/integration/scan_exit_codes_test.go`
(`TestScanExitCodeContract` and `TestScanOutputStreamContract`).

**Output JSON Schema**

The stdout document (`version`, `scan_metadata`, `summary`, `findings[]`) is
pinned by a published, versioned JSON Schema you can build against and
validate in your own CI:
[`schemas/limensafe/v1.1.0/scan-output.schema.json`](schemas/limensafe/v1.1.0/scan-output.schema.json)
(current emission). The prior
[`v1.0.0`](schemas/limensafe/v1.0.0/scan-output.schema.json) contract remains
available for comparison; **1.1.0** adds allowlist suppression accounting
(`allowlist_suppressions`, `allowlist_suppressions_by_entry`).

- Branch on **`scan_metadata.output_schema_version`** (`1.1.0`) to detect
  the output shape — not on the coarse top-level `version` or the
  independently-moving `tool_version`.
- Counters are stable: `files_scanned`, `bytes_scanned`, `files_skipped`,
  `directories_skipped`, and `files_skipped_by_reason` are always present
  (`0` / `{}`), so `jq` reads never have to distinguish zero from missing.
  `files_skipped` is the single reliable total of files not scanned —
  including files behind ignore-pruned directories.
- Allowlist suppressions, when a catalog declares them, are counted the same
  way — present totals, never silent subtraction without a metadata trail.

See [`CONTRIBUTING.md` §"Scan output contract"](CONTRIBUTING.md#scan-output-contract-json-schema)
for the full field reference, nullability rules, and versioning discipline.

### Without git: scan a directory tree

```bash
limensafe scan ./path/to/scan \
  --catalog ./catalogs/private.yaml \
  --visibility public_oss \
  --workers 8 \
  --max-file-size 5242880        # 5 MB cap
```

## Configuration

Two layers — the **two-layer separation** that solves the chicken-and-egg
problem: scan rules need protected vocabulary, but the rules can't ship in
the public repo.

### Repo config (`.limensafe/config.yaml`)

Public-safe. Lives in the repo. Contains references by ID — never raw
vocabulary. See [`docs/design/catalog-schema.md`](docs/design/catalog-schema.md)
for the full schema.

### Catalogs (vocabulary bundles)

Private. Authored locally or distributed out-of-band. Sources supported in v0:

Catalog structure is pinned by
[`schemas/limensafe/v1/catalog.schema.json`](schemas/limensafe/v1/catalog.schema.json)
and enforced at load time against the schema copy embedded in the binary.
Use `$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"`
and a `schema_version` in major 1 (`1.0.0` for the base shape; `1.1.0` when
you use top-level `allowlist` entries). Catalogs without `$schema` still load
with a warning for compatibility; new authored catalogs should include it.

To generate a catalog from a flat term-list (instead of hand-authoring YAML),
see [`docs/catalog/build-from-termlist.md`](docs/catalog/build-from-termlist.md)
and `limensafe catalog build --help`.

| Kind      | When   | Example                                                   |
| --------- | ------ | --------------------------------------------------------- |
| `file`    | always | `path: .limensafe/catalogs/public.yaml`                   |
| `env`     | always | `var: LIMENSAFE_CATALOG_PATH`                             |
| `builtin` | always | `name: public-baseline` (vendored sentinel-only baseline) |
| `profile` | v0.x   | named profile in user-level config                        |
| `url`     | v1+    | control-plane / signed bundle distribution                |

The vendored `public-baseline` catalog (catalog_id: `limensafe-public-baseline-v0`)
ships with the binary and contains generic sentinel/hygiene patterns
(`DO_NOT_COMMIT`, `TODO_REMOVE_BEFORE_PUSH`, deprecated path shapes). Opt
in via `source: { kind: builtin, name: public-baseline }`. No auto-load —
catalogs are always explicit in repo config.

### Visibility scopes

Severity adapts to where the leak would land:

| Scope                | Meaning                                                  |
| -------------------- | -------------------------------------------------------- |
| `public_oss`         | Published OSS, indexed by search / training scrapers     |
| `unlisted_oss`       | Public host but unpromoted (one config flip from public) |
| `internal`           | Within the firm, not externally visible                  |
| `engagement_private` | Within a specific client engagement (tighter)            |
| `local_only`         | Never committed (planning, scratch, dogfooding)          |

A catalog entry with `allowed_in: [engagement_private, internal]` won't fire
in those scopes but will in `public_oss`. This lets sanctioned codenames live
safely in private repos while blocking them from public ones.

## Architecture (v0)

Layered model — see [`docs/design/architecture.md`](docs/design/architecture.md):

```
L0 — Catalog        vocabulary bundles (private) + repo config (public)
L1 — Engine         deterministic detectors over span graph
L2 — CLI            scan / audit-publish / catalog / attest
L3 — Hooks / CI     pre-commit, pre-push, GH Actions, goneat adapter
```

v0 ships L0–L3. Forward direction is tracked in
[`docs/roadmap.md`](docs/roadmap.md).

V0 detectors: literal, slug, path-segment, regex, co-occurrence over a span
graph. All deterministic. NER / ML detection deferred to v1+ as a phase-2
plugin (likely Presidio or ONNX-runtime).

## V0 Acceptance Corpus

`testdata/synthetic-acme/` is a hand-crafted leak corpus using `acme` as the
canonical synthetic placeholder for a protected client identity, `horizon` as
a protected internal codename, and `tilden` as a sanctioned substitute
codename. The 9 acceptance properties (T1–T9) are documented in
[`testdata/synthetic-acme/README.md`](testdata/synthetic-acme/README.md).

The corpus exercises:

- Literal client-name fixtures (`acme-dev`, `acme-prod`)
- The triangulation case (`acme-horizon-dev`)
- Path-only leak (`internal/clients/acme/data.go` with clean content)
- Branch-name and commit-message stdin surfaces
- Two-tier catalog separation (public + private with `optional: true`)
- Co-occurrence rules
- Scope-adaptive severity (`tilden` allowed in engagement_private, blocked in public_oss)

Run the full acceptance suite:

```bash
go test ./...
```

## Design Documents

- [`docs/design/architecture.md`](docs/design/architecture.md) — layered L0–L6
  model, extractor and finding contracts, severity composition
- [`docs/design/catalog-schema.md`](docs/design/catalog-schema.md) — vocabulary
  bundle + repo config schema, layered resolution, ID-safety rule, CI
  integration patterns
- [`docs/design/existing-tools-gap.md`](docs/design/existing-tools-gap.md) —
  why secret scanners, DLP/PII tools, and policy engines don't cover this
  class
- [`docs/design/problem-statement.md`](docs/design/problem-statement.md) — CCL
  class definition, threat model, success criteria, non-goals, stakeholder map
- [`docs/decisions/ADR-0003-redaction-safe-output.md`](docs/decisions/ADR-0003-redaction-safe-output.md)
  — the implementation contract for the zero-leak invariant
- [`docs/releases/v0.2.0.md`](docs/releases/v0.2.0.md) — first public release
  notes (audience framing, migration, where to start)
- [`docs/guides/authoring-a-catalog.md`](docs/guides/authoring-a-catalog.md) —
  operator guide for private catalog design
- [`docs/catalog/build-from-termlist.md`](docs/catalog/build-from-termlist.md)
  — generate a catalog from a term-list
- [`docs/usage/rewrite-operating-patterns.md`](docs/usage/rewrite-operating-patterns.md)
  — rewrite modes and post-action publish-surface hygiene

## License

Apache-2.0. See [LICENSE](LICENSE).

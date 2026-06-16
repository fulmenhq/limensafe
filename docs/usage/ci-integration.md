# Integrating limensafe into CI/CD

This guide shows how to run `limensafe scan` as an automated check in a
CI/CD pipeline: where to put the **blocking gate**, where to put the
**advisory sweep**, and — the part that trips people up — how to give CI
a catalog **without ever committing your protected vocabulary to the
repository**.

It assumes you already know _which scan answers which question_. If you
don't, read [Scan modes & scopes](scan-modes.md) first — this guide
builds on the **gate vs sweep** model from that page and won't re-explain
the scopes.

> **The one rule that shapes everything below:** your real catalog —
> the vocabulary that names clients, codenames, people, or engagements —
> lives **outside the repository**, and CI loads it **by reference** at
> run time. The repo carries only synthetic placeholders and the public
> baseline. A leak you're scanning _for_ must never arrive _via_ the
> scanner's own configuration. See
> [Sourcing the catalog in CI](#sourcing-the-catalog-in-ci).

---

## The two-check pattern

Wire **two** checks with different scopes and different jobs. This is the
single most important decision; everything else is mechanics.

|              | Gate (PR check)                                | Sweep (scheduled audit)                            |
| ------------ | ---------------------------------------------- | -------------------------------------------------- |
| Scope        | `--diff` (lines this change introduces)        | `--git-archive HEAD` (the whole tracked tree)      |
| Trigger      | every pull request                             | a cron schedule (nightly / weekly) + release prep  |
| Blocking?    | **yes** — exit `1` fails the PR                | **no** — publishes a report, never fails the build |
| Noise budget | near-zero — it must stay quiet to stay trusted | tolerated — a human triages it                     |
| Answers      | "did _this change_ leak?"                      | "is there anything _else_, anywhere?"              |

Why the split: the noise that's acceptable in a sweep (a generic domain
word, a short acronym matching inside a dependency hash) is _fatal_ in a
gate. Developers who hit a false block learn to bypass the check, and
then the gate protects nothing. Keep the gate quiet and blockable; let
the sweep be thorough and advisory.

---

## Sourcing the catalog in CI

`limensafe scan` needs a **catalog** (the vocabulary to look for). You
have two layers, and they're sourced differently:

1. **Public baseline + synthetic catalogs — committed.** The vendored
   `pkg/catalog/builtin/public-baseline.yaml` and any synthetic
   placeholder catalogs are safe to commit and reference by path
   directly. They carry no organization-specific vocabulary.
2. **Your real catalog — never committed.** It is delivered to CI at run
   time from a secret store, written to a scratch path, used, and
   destroyed.

The supported way to reference a not-in-repo catalog is the **env
source** in a `.limensafe/config.yaml`. This fixture (from
`testdata/synthetic-acme/`) is the canonical shape:

```yaml
catalogs:
  - catalog_id: my-public-baseline
    source:
      kind: file
      path: ../catalog/synthetic-acme-public.catalog.yaml # committed, safe
    optional: false
  - catalog_id: my-private-catalog
    source:
      kind: env
      var: LIMENSAFE_CATALOG_PATH # resolved at run time
    optional: true
policy:
  block_threshold: high
```

CI then materializes the private catalog from a secret into the path that
env var points at:

```bash
# Write the secret to a scratch file, scan, then destroy it.
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
printf '%s' "$LIMENSAFE_PRIVATE_CATALOG_YAML" > "$work/private.catalog.yaml"
export LIMENSAFE_CATALOG_PATH="$work/private.catalog.yaml"

limensafe scan . --diff --diff-base "origin/${{ github.base_ref }}" \
  --config-file .limensafe/config.yaml --mode ci
# trap deletes $work — the protected vocabulary never lands on disk persistently
```

Equivalently, skip the config file and pass `--catalog "$work/private.catalog.yaml"`
directly (repeatable for layered catalogs).

**Three properties make this safe to run in a shared CI system:**

- **Reference, not copy.** The catalog is never in the repo, never in the
  image, never in the cache — only in a secret and a scratch file that is
  deleted when the step ends.
- **`optional: true` + `--mode ci`.** A private catalog declared
  `optional: true` is allowed to be absent — but `--mode ci` (and
  `--mode release`) **fail closed**: if the secret didn't materialize, the
  run errors (exit `2`) instead of silently scanning with a weaker
  catalog. `--mode local` only warns. Choose `ci`/`release` for anything
  that gates a merge or a publish. The `--private-catalog-missing
silent|warn|error` flag overrides this explicitly when you need to.
- **Redaction-safe output.** Even when a finding fires, limensafe never
  prints the matched protected substring — across stdout, stderr, finding
  IDs, or fingerprints (see
  [ADR-0003](../decisions/ADR-0003-redaction-safe-output.md)). Your CI
  logs and artifacts stay clean by construction, not by your remembering
  to scrub them.

> **Maintaining the catalog over time** is an operations concern, not a
> repo concern: the catalog is owned and versioned in your private store,
> CI reads the current version from the secret, and the repo never knows
> the difference. Treat the secret as the single source of truth and
> rotate it the way you rotate any other.

**A malformed catalog fails closed, safely.** At scan start, limensafe
validates each catalog against its embedded JSON Schema. A structural error
(a missing field, a bad enum, a wrong major `schema_version`) exits `2` before
any scanning — so a corrupted secret or a bad hand-edit stops the run instead
of scanning with a broken catalog. The diagnostic is redaction-safe: it names
the JSON-pointer location and failing rule, never your vocabulary. See
[ADR-0006](../decisions/ADR-0006-catalog-two-layer-validation.md).

---

## GitHub Actions

### PR gate (blocking)

```yaml
name: ccl-gate
on: pull_request
permissions:
  contents: read
jobs:
  diff-scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          persist-credentials: false
          fetch-depth: 0 # --diff needs the base ref's history

      - name: Install limensafe
        run: |
          set -euo pipefail
          ver="vX.Y.Z"            # pin to a released tag
          base="https://github.com/fulmenhq/limensafe/releases/download/${ver}"
          curl -fsSL -o limensafe "${base}/limensafe-linux-amd64"
          curl -fsSL -o SHA256SUMS "${base}/SHA256SUMS"
          grep " limensafe-linux-amd64$" SHA256SUMS | sed 's# .*# limensafe#' | sha256sum -c -
          chmod +x limensafe && sudo mv limensafe /usr/local/bin/

      - name: CCL diff gate
        env:
          LIMENSAFE_PRIVATE_CATALOG_YAML: ${{ secrets.LIMENSAFE_PRIVATE_CATALOG }}
        run: |
          set -euo pipefail
          work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
          printf '%s' "$LIMENSAFE_PRIVATE_CATALOG_YAML" > "$work/private.catalog.yaml"
          export LIMENSAFE_CATALOG_PATH="$work/private.catalog.yaml"
          limensafe scan . \
            --diff --diff-base "origin/${{ github.base_ref }}" \
            --config-file .limensafe/config.yaml \
            --visibility public_oss \
            --mode ci
          # exit 1 (a blocking finding) fails the step and the PR — no extra wiring needed
```

`fetch-depth: 0` is required: `--diff` resolves `origin/<base>...HEAD`,
which needs the base branch's history present in the checkout. A shallow
clone makes the diff base unresolvable — that's a **config error
(exit `2`)**, not a scan failure: the run never starts, so treat it as a
setup problem (fix the checkout depth), not a finding.

### Scheduled sweep (advisory)

```yaml
name: ccl-sweep
on:
  schedule:
    - cron: "0 7 * * 1" # Mondays 07:00 UTC
  workflow_dispatch:
permissions:
  contents: read
jobs:
  tree-sweep:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { persist-credentials: false, fetch-depth: 0 }
      - name: Install limensafe
        run: | # ...same install step as the gate...
          true
      - name: CCL tracked-tree sweep
        env:
          LIMENSAFE_PRIVATE_CATALOG_YAML: ${{ secrets.LIMENSAFE_PRIVATE_CATALOG }}
        run: |
          set -euo pipefail
          work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
          printf '%s' "$LIMENSAFE_PRIVATE_CATALOG_YAML" > "$work/private.catalog.yaml"
          export LIMENSAFE_CATALOG_PATH="$work/private.catalog.yaml"
          # || true: the sweep reports, it does not fail the build
          limensafe scan --git-archive HEAD \
            --config-file .limensafe/config.yaml \
            --visibility public_oss --mode ci \
            > sweep.json || true
          jq '{
            schema:       .scan_metadata.output_schema_version,
            files_scanned: .scan_metadata.files_scanned,
            files_skipped: .scan_metadata.files_skipped,
            findings:     .summary.findings_total,
            by_severity:  .summary.by_severity
          }' sweep.json
      - uses: actions/upload-artifact@v4
        with: { name: ccl-sweep, path: sweep.json }
```

The sweep deliberately does **not** propagate the exit code (`|| true`)
and uploads the JSON as an artifact for a human to triage. Promote a
finding from the sweep into the catalog's tuning (or fix the content)
rather than blocking the build on a scope you've chosen to keep advisory.

---

## Reading the result in any CI system

The contract is the same everywhere — you don't need GitHub Actions
specifically. Branch on the **exit code**, parse the **JSON on stdout**:

| Exit | Meaning                                                                                            | CI should…                                           |
| ---- | -------------------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| `0`  | clean — no finding at/above the block threshold                                                    | pass                                                 |
| `1`  | one or more findings have `decision=block`                                                         | **fail the gate** (or, for a sweep, report)          |
| `2`  | config / catalog validation error — **incl. an unresolvable `--diff-base`** (e.g. a shallow clone) | fail the build — your _setup_ is wrong, not the code |
| `3`  | runtime error (I/O, malformed input)                                                               | fail the build — investigate                         |

A bad `--diff-base` is **config-shaped (`2`)**, not runtime (`3`): the
scan never runs, so it belongs in the "fix your pipeline" bucket with
catalog errors. (This matches `CONTRIBUTING.md` and is locked by
`TestScanGitDiffInvalidBaseIsConfigError`.)

stdout carries the JSON **result document** for any scan that _completed_
— exits `0` and `1`. On a config or runtime failure (`2`/`3`) the scan
didn't complete and **stdout may be empty**; the diagnostic is on stderr.
So: branch on the exit code first, and only parse stdout JSON for `0`/`1`.
Never parse stderr for results. A GitLab CI / Jenkins / generic-shell
gate is the same shape: materialize the catalog into a temp path, run the
scan, let a non-zero exit fail the job.

**Parsing the JSON.** The stdout document (`version`, `scan_metadata`,
`summary`, `findings[]`) is pinned by a published, versioned JSON Schema:
[`schemas/limensafe/v1.0.0/scan-output.schema.json`](../../schemas/limensafe/v1.0.0/scan-output.schema.json).
If you parse the output, branch on **`scan_metadata.output_schema_version`**
(`1.0.0`) — that is the authoritative "which shape am I reading?" signal
(top-level `version` is a coarse generation marker; `tool_version` moves
with the build). `files_skipped` is the single reliable total of files not
scanned, so a sweep can report "what wasn't looked at" alongside what was.
See [CONTRIBUTING — scan output contract](../../CONTRIBUTING.md) for the
full field-stability rules.

For the precise block-vs-warn rule and how `--mode` changes the
threshold, see
[Scan modes — the decision model](scan-modes.md#decision-model-severity--block-vs-warn).

---

## Try it locally first (no secrets needed)

Everything above runs against the in-repo synthetic corpus with no
private data, so you can validate your wiring before adding the secret:

```bash
# A "diff gate" dry run against the public baseline:
limensafe scan . --diff --diff-base origin/main \
  --catalog pkg/catalog/builtin/public-baseline.yaml \
  --visibility public_oss --mode ci

# A "tracked-tree sweep" against the synthetic-acme catalog fixture:
limensafe scan --git-archive HEAD \
  --catalog testdata/synthetic-acme/catalog/synthetic-acme-public.catalog.yaml \
  --visibility public_oss
```

Once those behave, swap the `--catalog` for the
secret-materialized-into-`mktemp` pattern above and you have the real
gate.

---

## Hooks, not just CI

The same scopes drive local Git hooks — a `pre-commit` running `--staged`
and a `pre-push` running `--diff` catch leaks before they ever reach the
remote (and before CI spends a minute finding what a hook could have
caught in a second). See
[Scan modes — pre-commit gate](scan-modes.md#pre-commit-gate-staged-index)
and [git-metadata surfaces](scan-modes.md#git-metadata-surfaces-commit-message--branch-name)
(branch names and commit subjects leak too, and they're easy to forget).

---

## See also

- [Scan modes & scopes](scan-modes.md) — which scan answers which question (read first)
- [Authoring a catalog](../guides/authoring-a-catalog.md) — how the vocabulary file is built
- [ADR-0003 — redaction-safe output](../decisions/ADR-0003-redaction-safe-output.md) — why CI logs stay clean
- [ADR-0006 — catalog two-layer validation](../decisions/ADR-0006-catalog-two-layer-validation.md) — how catalogs are validated at scan start, and why the diagnostics never leak
- [Catalog & config schema](../design/catalog-schema.md) — the `config.yaml` and catalog reference shapes

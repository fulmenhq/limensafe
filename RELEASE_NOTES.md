# limensafe — release notes (current window)

Per the v0.1.0 docs package convention, this file carries release notes
for the most recent **three releases**. Earlier releases live
permanently under [`docs/releases/`](docs/releases/) as evergreen
mirrors; once a release falls out of this window, its narrative is
reachable from [`CHANGELOG.md`](CHANGELOG.md)'s "Theme" line back to
its archived mirror.

For the complete capability-arc audit trail with PR references, see
[`CHANGELOG.md`](CHANGELOG.md).

---

## v0.1.0 — YYYY-MM-DD

> **Date**: YYYY-MM-DD (set at tag)
> **Theme**: First MVP cut
> **Evergreen mirror**: [`docs/releases/v0.1.0.md`](docs/releases/v0.1.0.md)

### In one paragraph

limensafe v0.1.0 is the first MVP-level release of the Confidential
Context Leakage (CCL) detector — and the first cut intended for an
external audience. v0.0.x cuts were internal scaffolding by explicit
principle. v0.1.0 closes the gap between "the tool works on synthetic
fixtures" and "the tool runs the full pre-rewrite-remediation workflow
against a real repository," with native git-history audit surfaces
(`--git-history`, `--git-commit-messages`, `--git-history-all`), the
PR-diff introduced-lines gate (`--diff` / `--diff-base`), the
scan-attestation file that turns each local check into a verifiable
artifact (`.limensafe/scan-attestation.json` + `Limensafe-Scan:`
trailer + dual push/tag gate), and a mode-aware private-catalog posture
(`--mode local|ci|release`) that matches how operators actually run
gates in different contexts.

### What this protects against

> Secret scanners protect what grants access. PII / DLP protects
> regulated identity. **limensafe protects what reveals a
> relationship** — the client names, codenames, fixtures, paths, and
> branch slugs that look ordinary but disclose who you work with and
> what you're building.

**Confidential Context Leakage (CCL)** is the class of disclosure where
the leaked tokens look like ordinary words and require organizational
knowledge to recognize. A client name in a test fixture. A codename in
a commit message. A hostname implying topology. A branch slug implying
engagement existence. A path implying internal directory structure.
None of these are credentials, none are regulated PII, and none can be
caught by entropy or canonical pattern — but each is a relationship
disclosure that ordinary observation (competitor, journalist, search-
engine indexing, AI training scrape) can correlate against a real
engagement the organization chose to keep confidential.

|                    | Secret scanners         | PII / DLP                 | **limensafe**                                                                                          |
| ------------------ | ----------------------- | ------------------------- | ------------------------------------------------------------------------------------------------------ |
| **Detects**        | Credentials             | Regulated personal data   | Org-defined confidential vocabulary                                                                    |
| **Lives in repo**  | Rules can               | Recognizers can           | **Rules live in repo; vocabulary does not**                                                            |
| **Severity model** | Credential class        | PII class                 | **Visibility-scope adaptive** (`public_oss` ≠ `engagement_private`)                                    |
| **Triangulation**  | No                      | No                        | **Co-occurrence rules** (token A + token B = critical)                                                 |
| **Output safety**  | Echoes match by default | Anonymization mode opt-in | **Zero-leak invariant by construction** ([ADR-0003](docs/decisions/ADR-0003-redaction-safe-output.md)) |

**limensafe coexists with credential scanners and PII / DLP. It does
not replace them.** Recommended setup: gitleaks (or your secret
scanner of choice) for credentials + limensafe for context, both in
the same pre-commit and CI gate. The two classes don't overlap; the
cost of running both is small.

### What's new in v0.1.0

#### Native git-history audit

`scan --git-history` walks unique historical blobs reachable from all
refs, dedupes by blob SHA, and expands findings to every eligible
commit/path attribution with the commit SHA in `location.git_ref`.
`scan --git-commit-messages` adds the commit-message surface — the
audit lens that catches branch slugs, codenames, and operator notes
that never landed in any blob. `scan --git-history-all` runs both in
a single pass. This is the use case that drove the limensafe pitch:
the pre-rewrite-remediation audit needs to see _every byte that ever
existed_, not just the working tree.

History scans report `history_blobs_scanned`,
`history_commits_scanned`, and `history_unique_blobs` in scan metadata.
History findings carry `source_kind: "git_history_blob"` /
`surface_kind: "blob"` and `source_kind: "git_commit_message"` /
`surface_kind: "commit_message"` so downstream consumers can
distinguish history-derived findings from working-tree findings.

#### The PR-diff gate

`scan --diff` with `--diff-base <ref>` (default `origin/main`) scans
only lines introduced by `HEAD` relative to the base ref. PR and pre-
push gates get a quiet introduced-content scope that does not re-flag
pre-existing matches in files touched by the change. Diff findings
carry `surface_kind: "diff"`. Invalid `--diff-base` refs return exit 2
with the protected ref text redacted at the scan boundary before
stderr emission — the zero-leak invariant covers diagnostic streams
the same way it covers JSON output.

#### Scan-attestation — commit the proof, not the corpus

`limensafe attest` produces a committed
`.limensafe/scan-attestation.json` proof file plus the
`Limensafe-Scan:` commit-trailer convention; `limensafe
verify-attestation` checks it with push-mode and tag-mode semantics.
The verifier reads the attestation and the known-catalog-hash
allowlist from committed `HEAD:` blobs, not from the working tree.
Parent-bound attestations are accepted only when `HEAD` changes
exactly `.limensafe/scan-attestation.json`. Tag-mode known-catalog-
hash approval comes only from committed
`.limensafe/known-catalog-hashes.txt` or the documented explicit
override `LIMENSAFE_RELEASE_CATALOG_OK=1`. Timestamps more than ~5
minutes in the future are rejected as malformed.

`make limensafe-attest`, `make limensafe-verify`, and
`make limensafe-verify-tag` wrap the workflow.

This gate matters because of what it makes provable. The
[`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md) §Final Validation
guardrail commits limensafe to running a public-baseline scan
(Tier 1, reproducible by any contributor) and an operator-private
deep scan (Tier 2, by role and policy only — never by catalog path,
vocabulary, or findings) before every tag. The
attestation gate **mechanically proves the committed Tier 1 (public-
baseline) scan ran** and is the release-stop enforcement point;
**Tier 2 remains a maintainer release-checklist signoff obligation by
role and policy** in v0.1.0, because `make limensafe-attest` attests
only the catalog set explicitly named in the make target. Layered-
catalog attestation that would mechanically prove both tiers in one
signed artifact without disclosing private catalog path / vocabulary /
findings is a v0.1.x refinement target. See
[ADR-0005](docs/decisions/ADR-0005-scan-attestation-gate.md) for the
proof-model rationale.

#### Tracked-tree scan surface

`scan --git-archive <ref>` (default `HEAD` when the flag is bare)
extracts Git's tracked tree to a temporary directory, scans it,
removes the temp, and reports `scan_root: "HEAD"`,
`scan_root_kind: "git-archive"`, and `git_ref` in metadata. Replaces
the manual `git archive | tar -x` CI recipe while keeping machine-
local temp paths out of stdout and stderr.

#### Mode-aware private-catalog posture

`scan --mode {local|ci|release}` selects the default missing-optional-
private-catalog posture and (in `release` mode) the medium block
threshold. `--private-catalog-missing {silent|warn|error}` overrides
per-invocation when the mode default isn't right. Warn mode emits a
`kind: "config-warning"` JSON finding without affecting detection
counts or exit-1 gating; CI and release modes fail closed with
exit 2.

`policy.block_threshold` from repo config is now honored by the
scanner, including the release-mode macro's medium threshold; the
prior hardcoded `high|critical → block` threshold is replaced.

#### `.limensafeignore` and skip-visibility

`scan` honors root-level `.gitignore` and `.limensafeignore` files
for filesystem and `--staged` scans. `--include-ignored` disables the
matcher for deliberate local hygiene scans. Skip diagnostics emit on
stderr after redaction. Scan metadata reports per-reason file-skip
counts and ignored directory-prune counts.

**`.limensafeignore` is a hygiene escape hatch, not a confidentiality
boundary.** Protected vocabulary in a `.limensafeignore`-skipped file
is still in the public repo if anyone removes the ignore entry or
scans with `--include-ignored`. The confidentiality boundary is the
two-layer catalog model; see
[`docs/guides/authoring-a-catalog.md`](docs/guides/authoring-a-catalog.md).

#### Whole-word matching

Catalog entities support `variants.whole_word` to require literal
matches to be bounded by start/end of input or non-word characters.
Aliases shorter than four characters default to `whole_word: true`
unless `whole_word: false` is explicitly set — closes the short-
acronym false-positive class where aliases such as `ILT` matched
`built`, `split`, `rebuilt`, or random checksum substrings. Catalog
loading records non-fatal warnings (e.g. `whole_word: true` paired
with `case_insensitive: true`) and surfaces them through `scan`
stderr after redaction. Operators who tuned catalogs against v0.0.x
should review their entries for the new default and explicitly set
`whole_word: false` where the prior substring behavior was
deliberate.

#### `entity_id` in findings + tightened ID-safety

Detection findings include the redaction-safe `entity_id` in the JSON
output contract, enabling downstream `jq` aggregation by protected
entity without disclosing matched text. Catalog loading rejects
entity IDs that contain protected alias substrings. Scan startup
checks output-visible IDs (catalog IDs, entity IDs, rule IDs,
replacement IDs) against the merged loaded alias set and refuses to
start if any collide. The ID-safety rule is enforced by construction;
operators who renamed entities along permitted lines do not need to
re-fingerprint anything.

#### Output contract — `location.surface_kind` and `git_ref`

Finding `location` now includes `surface_kind` (one of
`"working_tree"`, `"staged_index"`, `"diff"`, `"blob"`,
`"commit_message"`, `"branch_name"`) and reserves `git_ref` for
git-derived surfaces. Finding fingerprints now include
`location.surface_kind` as a normalized input — see
[Migration notes](#migration-notes) below.

#### Developer tooling — verify-only format gates

`make format-check` (verify-only), `make prepush` (CI-aligned pre-
push), and `make pr-final` (full pre-review quality gate) close the
recurring CI red where `make check-all`'s auto-fix masked the format
drift that `goneat format --check` catches in CI. `.goneat/assess.yaml`
adds explicit format scoping for Markdown, JSON, and YAML.

For the complete capability arc with brief IDs and PR references,
see [`CHANGELOG.md`](CHANGELOG.md).

### The corpus paradox — sovereign, not shippable

A leak-prevention tool that ships with the corpus it detects defeats
its own purpose. limensafe is built so the protected vocabulary lives
_outside_ the repo it protects — by design. The two-layer catalog
model is structural, not editorial:

- The **repo** holds policy, visibility scope, and references-by-ID — never raw vocabulary. `.limensafe/config.yaml` is safe to publish.
- The **catalogs** hold words — distributed out-of-band (local files, env-injected paths, secret-manager-distributed bundles, or the vendored sentinel-only `public-baseline`).

The schema enforces the rule (the ID-safety rule rejects catalog IDs
that contain protected alias substrings at load time). The Redactor
([ADR-0003](docs/decisions/ADR-0003-redaction-safe-output.md)) enforces
it at the emission boundary: no protected substring may appear in any
byte limensafe emits across stdout, stderr, JSON, log lines, finding
IDs, or fingerprint inputs.

The honest follow-up matters more than the architecture: **limensafe
cannot prove negatives via in-repo examples.** We cannot ship a "see,
the repo is free of these references" demonstration without putting
real references in the repo first.

What we can demonstrate is the _discipline_. The scan-attestation gate
is a committed proof file — `.limensafe/scan-attestation.json` — that
says: _"this commit was scanned against catalog hash X with N
findings"_ without disclosing what the catalog contained. The
[`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md) §Final Validation
guardrail commits limensafe to running a public-baseline scan and an
operator-private deep scan before every tag. The
attestation gate **mechanically proves the committed Tier 1 (public-
baseline) scan ran** and is the release-stop enforcement point; Tier 2
remains a maintainer release-checklist signoff obligation by role and
policy in v0.1.0, with layered-catalog attestation that would
mechanically prove both tiers as a v0.1.x refinement target. The
operator deep scan is referenced by role and policy only — never by
catalog path, vocabulary, or findings. **That non-disclosure is itself
the worked example of the principle limensafe teaches.**

**Commit the proof, not the corpus.**

### Where to start

Two starter-corpus questions and their answers:

**"Can I see it work end-to-end before I write a catalog?"** — Yes.
`testdata/synthetic-acme/` is a hand-crafted leak corpus using `acme`
as a synthetic client identity, `horizon` as a synthetic internal
codename, and `tilden` as a sanctioned substitute codename (allowed in
`engagement_private` and `internal`, blocked in `public_oss`). Nine
acceptance tests (T1–T9) exercise the corpus; the
[`README.md`](README.md) walks the first scan in ~5 minutes.

**"Where do I start authoring my own catalog?"** — Take the vendored
`public-baseline` catalog as your immediate gate (it ships with the
binary, contains only generic sentinel patterns, and is safe to
publish). For the private catalog that holds your real vocabulary,
work from `testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml`
as a template — copy it, rename the entity IDs (the ID-safety rule
will reject any ID containing your alias substrings; the loader will
tell you if you slip), and keep the file outside the scanned repo.
The catalog-authoring guide at
[`docs/guides/authoring-a-catalog.md`](docs/guides/authoring-a-catalog.md)
walks the framing decisions.

### Migration notes

#### Output contract — fingerprint inputs include `surface_kind`

Finding fingerprints now include `location.surface_kind` as a
normalized input. **All v0.1.0 finding fingerprints differ from
v0.0.x** — not only for the git-derived surfaces introduced in this
release (`"diff"`, `"blob"`, `"commit_message"`). Pre-existing
working-tree, staged-index, branch-name, and stdin-commit-message
findings will all compute new fingerprint values because
`surface_kind` is now part of the hash for every finding. Dedupe
consumers keying on legacy fingerprints should expect cross-surface
churn and re-baseline accordingly. The v0.1.0 output-contract lock —
including the formal `surface_kind` enum and the fingerprint-input
contract — lands under the planned v0.1.x output JSON Schema work.
Plan dedupe-store migrations accordingly.

#### Tag-mode attestation — `LIMENSAFE_KNOWN_CATALOG_HASHES` env merge removed

Tag-mode catalog-hash approval now reads only the committed
`.limensafe/known-catalog-hashes.txt` file or the documented explicit
override `LIMENSAFE_RELEASE_CATALOG_OK=1`. If your release flow
relied on the `LIMENSAFE_KNOWN_CATALOG_HASHES` env-merge bypass into
the tag-mode allowlist, switch to the committed-file path before
upgrading.

#### Whole-word default for short aliases

Catalog aliases shorter than four characters now default to
`whole_word: true`. Operators who tuned catalogs against v0.0.x
should review entries shorter than four characters and explicitly
set `whole_word: false` where the prior substring behavior was
deliberate (typically: never; the substring behavior produced the
false-positive class this fix closes). The catalog-loading warning
for `whole_word: true` paired with `case_insensitive: true` will
flag any combinations worth re-reading.

### Known mechanism notes (v0.1.0 → v0.1.x refinement targets)

#### Self-scan guardrail Tier 1 — currently diff-scoped

`make limensafe-attest` (the release-checklist Tier 1 mechanism)
scopes via `--diff-base origin/main`. Diff-scope is acceptable for
v0.1.0; a release self-scan guardrail is more defensibly **full-
tree** (and git-history via `--git-history-all` now that the
git-history surfaces ship). Tracked under the release-checklist
guardrail for v0.1.x refinement; will not silently widen without
devlead + cicd sign-off.

#### Self-scan guardrail Tier 1 — catalog manifest is `public-baseline` only

Tier 1 currently uses the vendored `public-baseline` catalog alone.
The `synthetic-acme` reference catalog at `testdata/synthetic-acme/`
serves adopter walkthroughs and the T1–T9 acceptance suite, not the
release self-scan. Additional structural-pattern catalogs (planned
synthetic structural entities — ACME-/HRZN- prefixes — plus a
generic internal-brief-ID class and agent-identifier regex classes)
will join the Tier 1 manifest as they are vendored.

#### Catalog `class` field is free-form

`class` on catalog entities is a free-form string in v0.1.0; the
closed enum lands with the v0.1.x catalog JSON schema work. Use one
of the three conventional classes the in-repo catalogs already use
(`client_identity`, `codename`, `operational_pattern`); design-doc
classes (`person_name`, `regulated_pii`, `internal_identifier`) are
forward-compatible names, not v0.1.0-enforced.

#### Output-contract formal lock pending

The v0.1.0 output-contract changes (`location.surface_kind` enum,
fingerprint-input contract including `surface_kind`) are documented
here and in the CHANGELOG; the formal JSON-schema lock lands under
the v0.1.x output JSON Schema work. Consumers building against
v0.1.0 today should treat the documented shape as authoritative and
re-validate against the schema once it ships.

### What's not in v0.1.0 (by design)

- **Catalog signing + distribution control plane.** Deferred to v1+
  per `HANDOFF.md §Q3` on two trigger conditions (multi-org adoption
  pull; signing/integrity demand). Neither has fired.
- **NER / ML-driven detection.** limensafe v0.1.x is deterministic by
  design. NER is a phase-2 plugin (likely Presidio sidecar or pure-Go
  ONNX runtime); see [`docs/design/problem-statement.md`](docs/design/problem-statement.md)
  §Non-Goals.
- **Public-locked-down cloud bucket of synthetic catalog packs.**
  Three reasons: signed-bundle distribution is a catalog-poisoning
  surface (handled by the deferred control plane); adopters don't
  need 50 example engagements, they need a template (synthetic-acme
  already covers it); and a public bucket of "synthetic confidential
  vocabulary" is exactly the kind of corpus an LLM training scraper
  would ingest. A lighter-weight `make starter-corpus DEST=...`
  generator is **reserved as post-v0.1.0 polish** to ship when
  adopter signal materializes.
- **Public release flip.** v0.1.0 ships with the repo still private.
  Planned public-cutover work (an external-OSS readability
  rebalance; a publish-surface inventory) iterates v0.1.x patches
  against this v0.1.0 baseline.

### References

| Area                            | File                                                                                                   |
| ------------------------------- | ------------------------------------------------------------------------------------------------------ |
| Full capability arc + briefs    | [`CHANGELOG.md`](CHANGELOG.md)                                                                         |
| CI patterns + scan CLI contract | [`README.md`](README.md)                                                                               |
| Operator best practices         | [`docs/guides/authoring-a-catalog.md`](docs/guides/authoring-a-catalog.md)                             |
| Scan modes quick reference      | [`docs/usage/scan-modes.md`](docs/usage/scan-modes.md)                                                 |
| Self-scan guardrail             | [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md) §Final Validation                                       |
| Zero-leak invariant             | [`docs/decisions/ADR-0003-redaction-safe-output.md`](docs/decisions/ADR-0003-redaction-safe-output.md) |
| Attestation gate                | [`docs/decisions/ADR-0005-scan-attestation-gate.md`](docs/decisions/ADR-0005-scan-attestation-gate.md) |
| Problem statement (CCL class)   | [`docs/design/problem-statement.md`](docs/design/problem-statement.md)                                 |
| Architecture                    | [`docs/design/architecture.md`](docs/design/architecture.md)                                           |
| Catalog schema                  | [`docs/design/catalog-schema.md`](docs/design/catalog-schema.md)                                       |
| Why not gitleaks / Presidio     | [`docs/design/existing-tools-gap.md`](docs/design/existing-tools-gap.md)                               |

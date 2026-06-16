# Authoring a limensafe catalog

A short, opinionated guide for operators writing their first private
catalog (and revisiting it as the surface grows). Covers the framing
decisions that matter most before you write any YAML, then walks an
entity end-to-end against the `synthetic-acme` reference corpus. New
to limensafe? Start with [`README.md`](../../README.md) for a tour of
what the scanner does and the scan CLI contract; then come here when
you need to author or review catalog content.

This guide is operator-facing. The schema reference is
[`docs/design/catalog-schema.md`](../design/catalog-schema.md); the
zero-leak invariant is [`ADR-0003`](../decisions/ADR-0003-redaction-safe-output.md);
the scan-mode patterns are [`docs/usage/scan-modes.md`](../usage/scan-modes.md).
This guide is the prose that joins them.

## What lives in the repo and what does not

limensafe enforces a **two-layer catalog** model. It is structural, not
editorial.

- The **repo** holds policy: visibility scope, block thresholds,
  catalog references-by-ID, scan-mode defaults. Safe to publish. Lives
  at `.limensafe/config.yaml` and `.limensafeignore`.
- The **catalogs** hold vocabulary: the actual words you are protecting
  — client identities, codenames, internal hostnames, agreement labels,
  account prefixes, deal cryptonyms. **Distributed out-of-band.** Loaded
  via `file`, `env`, or `builtin` sources; never committed alongside
  the code they protect.

The repo config can be reviewed in a public pull request without
disclosing anything. The catalog is the artifact that must travel via
the secret-management path your organization already uses for
credentials and customer data.

**This rule is the answer to "the corpus paradox."** A leak-prevention
tool that ships with the corpus it detects defeats its own purpose.
limensafe addresses it by construction: your protected vocabulary
literally cannot be in the repo limensafe is protecting. The
[scan-attestation gate](../decisions/ADR-0005-scan-attestation-gate.md)
is how you prove the scan ran without disclosing what it found. The
working principle, kept in front of every authoring decision: **commit
the proof, not the corpus.**

## The ID-safety rule

Catalog IDs, entity IDs, rule IDs, and replacement IDs **must not
contain any alias substring** of the entity they identify. The schema
validator rejects entity IDs that violate this at load time. The scan
boundary rejects output-visible IDs that collide with the merged loaded
alias set at startup.

This is not a style preference. Every output-visible ID gets emitted
into JSON findings, log lines, fingerprints, and downstream
aggregations. If an entity for `acme-platform` had ID
`e-acme-platform-1`, every finding for that entity would leak the
codename via its ID. So the convention is **opaque sequential or
hash-derived IDs**:

```yaml
- id: e-client-1
  class: client_identity
- id: e-codename-1
  class: codename
- id: e-sentinel-1
  class: operational_pattern
```

Prefix with `e-` for entity, `r-` for replacement, `co-` for
co-occurrence. Numbers can be sequential within a catalog or
hash-derived from the canonical alias — your choice; either is safe.

The reverse-search check is fast: paste your catalog and grep your
proposed IDs against every alias in the file. If any ID contains any
alias, rename before you commit anywhere.

## Authoring an entity

The `synthetic-acme` reference catalog at
`testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml` is the
shape every new catalog should start from. The first entity:

```yaml
- id: e-client-1
  class: client_identity
  aliases:
    - "Acme"
    - "Acme Corp"
    - "AcmeCorp"
    - "acme"
    - "acme-corp"
  variants:
    case_insensitive: true
    slug: true
    path_segments: true
  blocked_in: [public_oss, unlisted_oss, internal]
```

Five fields earn their keep:

- **`id`** — opaque, ID-safe, stable across catalog revisions. Once
  fingerprints have been emitted against an ID, do not rename it
  without a plan to invalidate the downstream artifacts that reference
  it.
- **`class`** — drives default severity and downstream filtering.
  In v0.1.0 the schema treats `class` as a free-form string; the
  closed enum is planned as part of the v0.1.x catalog JSON schema
  work. Until then, use one of the **conventional classes** the
  in-repo catalogs already use — `client_identity`, `codename`,
  `operational_pattern` — and prefer the most specific match. The
  design docs reference a broader set (`person_name`,
  `regulated_pii`, `internal_identifier`) for future use; treat those
  as forward-compatible names rather than v0.1.0-enforced classes.
  Do not default to `operational_pattern` when a more specific class
  fits.
- **`aliases`** — every shape the protected term takes. Capitalization
  variants, hyphenated and unhyphenated forms, slug variants, prefixes
  used in account labels. The matcher's job is to canonicalize these
  through the `variants` flags; your job is to enumerate the variants
  _limensafe cannot generate_.
- **`variants`** — `case_insensitive`, `slug`, `path_segments`,
  `whole_word`. See the next section for `whole_word` defaults.
- **`blocked_in`** (or `allowed_in`) — the visibility-scope policy.
  See the visibility-scopes section below.

Defer `severity_override`, `replacement_suggestion`,
`fingerprint_salt`, and the more advanced fields until you have a
working catalog and a baseline of real findings to tune against. The
defaults exist for a reason.

## Aliases and whole-word matching

Short aliases match too eagerly by default. `ILT` as a literal alias
would otherwise match `built`, `split`, `rebuilt`, and arbitrary SHA
substrings — the kind of false-positive flood that destroys adopter
trust on day one. So:

- Aliases **shorter than four characters default to `whole_word: true`**
  — the alias must be bounded by start/end of input or non-word
  characters to match. Set `whole_word: false` only with a written
  reason in `notes:`.
- Aliases four characters and longer default to `whole_word: false`
  (substring match) and can be tightened on a per-entity basis with
  `variants.whole_word: true`.
- `case_insensitive: true` combined with `whole_word: true` triggers an
  alias-hygiene warning at load — case-insensitive whole-word matching
  is nearly always a sign that the alias is too short for the policy
  the operator intends. Check the warning; if you really mean it, keep
  the entity and document why.

Recommended discipline: scan the **synthetic-acme** corpus
(`./testdata/synthetic-acme/repo`) with your private catalog loaded
alongside, look at the false-positive count, and tighten the aliases
that misfire most. Five iterations usually closes the gap.

Use `tokens` for protected strings that must be matched as exact,
case-sensitive whole tokens without alias variant expansion. Use
`aliases` when slug, plural, path-segment, case-insensitive, or
substring-compatible behavior is intended.

## Co-occurrence rules — the triangulation case

A token alone may be generic. Two below-threshold tokens together
inside a window are almost always a confirmed leak. limensafe expresses
this as a co-occurrence rule:

```yaml
co_occurrence_rules:
  - id: co-client-codename-1
    entities: [e-client-1, e-codename-1]
    window_lines: 100
    severity: critical
    notes: |
      Client identity + internal codename within 100 lines of the same
      file = engagement-identity disclosure regardless of either token's
      individual severity.
```

Co-occurrence dominates real-world leak reports. `acme` alone is the
generic word for a placeholder; `horizon` alone is a common english
word; `acme` and `horizon` in the same file within a 100-line window
is a confirmed engagement-identity leak. The synthetic-acme corpus
exercises this case in T6.

Author co-occurrence rules for **identity + activity** pairs — for
example, `client + codename`, `codename + region`, or
`account-prefix + hostname`. Do not author rules for pairs that fire
spuriously in routine code; the goal is high-confidence escalation,
not noise.

## Visibility scopes — `allowed_in` vs `blocked_in`

The scope ladder, from most to least exposed:

| Scope                | What it means                                             |
| -------------------- | --------------------------------------------------------- |
| `public_oss`         | Published OSS; indexed by search engines and AI training. |
| `unlisted_oss`       | Public host but not promoted; one config flip from above. |
| `internal`           | Within the firm; not externally visible.                  |
| `engagement_private` | Within one client engagement; tighter than `internal`.    |
| `local_only`         | Never committed; planning, scratch, dogfooding.           |

The scan's `--visibility` flag (required; defaults to `public_oss` for
safety) drives the floor severity. An entity with `blocked_in:
[public_oss, unlisted_oss]` does not fire in `internal` or below; an
entity with `allowed_in: [engagement_private, internal]` fires
everywhere except those two scopes.

**Use `allowed_in` for sanctioned substitutes** — `tilden` in the
synthetic-acme corpus is `allowed_in: [engagement_private, internal]`,
meaning the team uses it freely in the engagement repo but limensafe
blocks it in any `public_oss` artifact. **Use `blocked_in` for
protected vocabulary** — the client identity, real codenames, internal
hostnames. The list explicit on each entity; no implicit defaults.

This is what makes scope-adaptive severity work without bypass flags:
the same catalog ships across your engagement repos and your public
repos; the visibility scope of the surface determines the gate.

## `.limensafeignore` is a secondary escape hatch

`.limensafeignore` exists for legitimate in-tree exclusions: generated
mirrors, build outputs, fixture trees, docs-examples directories. It
honors `.gitignore` syntax plus a `# skip-visibility: <reason>`
trailing-comment convention so operators can review what was excluded
and why.

**It is not a confidentiality boundary.** A protected term in a
`.limensafeignore`-skipped file is still in the public repo if anyone
removes the ignore entry or scans with `--include-ignored` later.
The confidentiality boundary is the two-layer catalog model: real
vocabulary stays out of tree. `.limensafeignore` is the hygiene tool
for "don't scan generated mirror N every commit."

When `--include-ignored` is set, limensafe scans the ignored paths
anyway — useful for periodic local hygiene sweeps. CI runs should
_not_ set `--include-ignored` for routine gates; reserve it for
maintainer audits.

## Choosing a scan mode

limensafe ships four scan-mode patterns, each with a default policy
posture. See [`docs/usage/scan-modes.md`](../usage/scan-modes.md) for
the full ladder; the operator quick-reference:

- **Local dev loop** — `limensafe scan .` honors `.gitignore` and
  `.limensafeignore` by default; warn-on-missing-private-catalog;
  for fast iteration while you tune aliases.
- **Pre-commit (staged)** — `limensafe scan --staged` reads the git
  index, not the working tree. Sound for "what's about to be
  committed" even if the working tree was edited after `git add`.
- **Pre-push / PR / CI (tracked archive)** — `limensafe scan
--git-archive HEAD` extracts the tracked tree to a temp directory,
  scans it, and removes the temp. Excludes ignored local artifacts;
  CI sees only tracked content.
- **PR-diff gate** — `limensafe scan --diff --diff-base origin/main`
  scans only lines introduced by `HEAD` relative to the base ref. Use
  on PR pre-push gates where pre-existing matches in touched files
  should not re-flag.

`--mode local|ci|release` selects the missing-private-catalog posture
(`warn` / `error` / `error`); `--private-catalog-missing` overrides it
explicitly when needed. The `release` mode applies the medium block
threshold; the gate fails closed.

## The corpus paradox, in operator terms

You will eventually want to scan limensafe-the-tool (or any internal
tool) against your private catalog. The repo will contain catalog
references by ID and the synthetic-acme reference vocabulary; the
private catalog you load alongside contains your real terms. The scan
will work as designed.

What you cannot do — ever — is _publish_ a "look, the repo is clean of
these terms" demonstration that names the terms. Publishing such a
demonstration would be the leak you were preventing. The
[scan-attestation gate](../decisions/ADR-0005-scan-attestation-gate.md)
is the answer: commit `.limensafe/scan-attestation.json` (the proof
file) at every tag; reference your private catalog tier by **role and
policy only** in your release checklist, never by catalog path,
vocabulary, or findings. The limensafe project itself follows this
pattern in [`RELEASE_CHECKLIST.md`](../../RELEASE_CHECKLIST.md) — see
the two-tier guardrail under Final Validation.

When colleagues, auditors, or adopters ask to "see the catalog," the
answer is: the public-baseline catalog is in the repo and reproducible
by anyone; the synthetic-acme reference catalog is in the repo for
adopter walkthroughs and the acceptance suite (T1–T9); the operator
catalog is held under the same access policy as the protected
vocabulary it contains. The
[`RELEASE_CHECKLIST.md`](../../RELEASE_CHECKLIST.md) §Final Validation
guardrail commits the maintainer to running both a Tier 1
public-baseline scan and a Tier 2 operator deep scan before every tag.
The attestation gate **mechanically proves the committed Tier 1
(public-baseline) scan ran** and is the release-stop enforcement
point; **Tier 2 remains a maintainer release-checklist signoff
obligation by role and policy** in v0.1.0, because
`make limensafe-attest` attests only the catalog set explicitly named
in the make target. Layered-catalog attestation that would
mechanically prove both tiers in one signed artifact without
disclosing private catalog path / vocabulary / findings is a v0.1.x
refinement target. **The corpus is sovereign, not shippable.** The
non-disclosure is itself the worked example of the principle
limensafe teaches.

## Validation and schema versioning

Every catalog is validated against the
[catalog JSON Schema](../../schemas/limensafe/v1/catalog.schema.json) **at
load time**, on each `limensafe scan` — not just at build time in this repo.
The schema is compiled from a copy embedded in the binary, so the same
structural contract is enforced whether you run from a checkout or an
installed release.

What this means when you author or build a catalog:

- **Declare both `$schema` and `schema_version`.** Set
  `$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"`
  and a semver `schema_version` (e.g. `"1.0.0"`). During the v0.1.x window a
  catalog that omits `$schema` still loads with a warning; from v0.2.0 it
  becomes required.
- **A structural mistake stops the scan.** An invalid catalog fails the load
  as a configuration error (**exit 2**) before any scanning happens. The
  diagnostic names the offending location as a JSON pointer plus the failing
  rule — e.g. `/entities/0: required` — and, by the zero-leak invariant,
  **never echoes your protected vocabulary** (no alias, token, or replacement
  string appears in the message). Read the pointer against
  [`catalog-schema.md`](../design/catalog-schema.md) to find the field.
- **Version compatibility is explicit.** A `schema_version` with a major other
  than `1` is rejected (exit 2); a higher minor/patch within major 1 loads with
  an advisory warning so a newer catalog still runs on an older limensafe.

Structure is all the schema checks. Alias-safety of ids, regex compilation,
co-occurrence referential integrity, and the substantive hygiene this guide
describes remain loader/linter responsibilities — the
[structural vs. semantic split](../decisions/ADR-0006-catalog-two-layer-validation.md)
is deliberate.

## Where to go from here

- [`docs/design/catalog-schema.md`](../design/catalog-schema.md) —
  full catalog YAML schema reference, including the fields this guide
  defers.
- [`docs/decisions/ADR-0006-catalog-two-layer-validation.md`](../decisions/ADR-0006-catalog-two-layer-validation.md) —
  how the catalog schema is enforced at runtime and build time, and why
  validation diagnostics stay redaction-safe.
- [`docs/design/architecture.md`](../design/architecture.md) — detector
  pipeline, severity composition, fingerprinting.
- [`docs/usage/scan-modes.md`](../usage/scan-modes.md) — the full
  scan-mode ladder + recipe set.
- [`docs/decisions/ADR-0003-redaction-safe-output.md`](../decisions/ADR-0003-redaction-safe-output.md) —
  the zero-leak invariant; why the Redactor sits where it does.
- [`docs/decisions/ADR-0005-scan-attestation-gate.md`](../decisions/ADR-0005-scan-attestation-gate.md) —
  the scan-attestation gate that lets you commit the proof without
  disclosing the corpus.
- [`testdata/synthetic-acme/README.md`](../../testdata/synthetic-acme/README.md) —
  the v0 acceptance corpus and the T1–T9 invariants every catalog
  change is regression-tested against.

When in doubt, run the scan against `testdata/synthetic-acme/` with
your catalog loaded alongside and look at what changes. The corpus is
small enough to read end-to-end; your catalog evolves against a stable
baseline.

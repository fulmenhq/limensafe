# Problem Statement: Confidential Context Leakage (CCL)

Status: design — public
Last updated: 2026-05-01

## Class Definition

**Confidential Context Leakage (CCL)** is the appearance, in artifacts that
are or could become public, of *organizationally-private context* — identities,
relationships, codenames, projects, people, systems, or operational facts —
that the controlling organization has chosen to keep confidential.

CCL is distinct from two adjacent classes:

- **Credential leakage** — secrets that are credentials (API keys, tokens,
  certificates). Detected by gitleaks, TruffleHog, GitGuardian, etc. Failure
  mode: a string that *grants access*.
- **PII leakage** — regulated personal data. Detected by Presidio, Comprehend,
  Macie, etc. Failure mode: a string that *identifies a regulated person*.

CCL is the failure mode of a string that *discloses an organizational
relationship or internal taxonomy*: a client name in a test fixture, an
unannounced project codename in a CI log, a person's name in a commit message
implying a confidential engagement, a hostname implying topology, a path
showing internal directory structure, an account label implying scale.

The unifying property: **CCL tokens look like ordinary words.** They cannot
be detected by entropy or by canonical pattern. They require organizational
knowledge — a vocabulary the org defines for itself.

## Canonical Triggering Pattern

The motivating case for limensafe is the **consulting firm or agency that
develops in OSS first and applies the work to client engagements**. The
failure mode is a Go/Python/TypeScript test fixture written against a
real client's data while the developer is heads-down on the feature, then
landed in a public repo without sanitization. Variations:

- `internal/cmd/profile_test.go` containing literal `<client>-dev` /
  `<client>-prod` profile fixtures
- `internal/config/loader_test.go` declaring a profile name keyed by
  the client's brand
- `internal/doctor/redact_test.go` containing the worst-case
  triangulation: `<client>-<internal-codename>-dev` — neither token alone
  is a credential, but the combination publicly attests both engagement
  existence and project codename
- branch names like `feat/<client>-redash-fix` and commit messages
  describing "the `<client>` team" by name

The synthetic acceptance corpus at
[`testdata/synthetic-acme/`](../../testdata/synthetic-acme/) reproduces
this exact shape using `acme` as the protected client placeholder,
`horizon` as a protected internal codename, and `acme-horizon-dev` as
the worst-case triangulation token. T1–T9 in the project's acceptance
suite verify limensafe catches each variation while preserving the
zero-leak invariant in scanner output.

## Threat Model

### Actors who introduce leaks

- **Developer (human)** — reflexively names test fixtures after the
  data source they're working against; copies sample data verbatim
- **AI agent** — same failure mode, often amplified by speed and breadth
  of edits across files
- **Contributor** — outside collaborator unaware of org-private vocabulary
- **Past self** — author of a fixture written before a confidentiality
  policy existed, surfaced when the repo is later opened
- **Tooling** — generators, scaffolders, log capture, screenshot tools that
  embed contextual paths or environment

### Surfaces where leaks land

Cross-reference [`architecture.md`](architecture.md) for the formal
extractor list. The threat surfaces, ranked by frequency-of-occurrence
in the consulting-firm case:

1. Source code (test fixtures, sample data, profile names, comments,
   error strings, env-var defaults, JSON/YAML schemas)
2. Filenames and directory paths
3. Branch names
4. Commit messages and PR descriptions
5. Logs and CI output (uploaded as public artifacts)
6. Documentation (Markdown, README examples, screenshots)
7. Notebooks (especially cell outputs with real data dumps)
8. Compiled binaries (embedded build paths, debug symbols, container layers)
9. Office documents (.docx, .pptx, .xlsx — visible content + metadata +
   comments + tracked changes + speaker notes)
10. PDFs (text + metadata + form fields + invisible OCR layer)
11. AI-era artifacts (prompts, eval/golden fixtures, RAG corpora,
    fine-tuning datasets, agent memory exports, MCP tool descriptions)

### Channels where leaks become public

- Public OSS repository on a public host (GitHub, GitLab, etc.)
- Public CI artifact (workflow logs, coverage reports, build outputs)
- Public package registry (npm, PyPI, crates.io, Docker Hub)
- Public doc site
- Social media screenshot
- Conference talk slides or video
- AI training data scrape (the artifact itself becomes corpus)
- Agent context shared between unrelated tasks
- Sidecar communication (Slack/Discord pasted snippets)

### Adversaries (informed observation)

We do not assume malicious targeting. The realistic adversary model is
**informed observation**:

- **Competitors** browsing public repos for engagement signals
- **Journalists** correlating codenames against PR pipelines
- **Automated correlators** — search engines, code search, AI training
  pipelines
- **Casual observers** — investors, partners, prospects who recognize
  a name they shouldn't have learned about
- **Future adversaries** — the public artifact persists; the adversary
  arrives later

CCL therefore requires *anticipatory* defense. A leak that does not appear
to expose anything today may correlate against future disclosures.

## Severity by Visibility Scope

The same protected token has different severity in different scopes.
A first-class severity input is the *visibility scope of the surface where
the token appears*. See [`catalog-schema.md`](catalog-schema.md) for the
formal enum:

- `public_oss` — published OSS, indexed by search engines / training scrapers
- `unlisted_oss` — public host but not actively promoted; one config flip
  away from `public_oss`
- `internal` — within the firm, not externally visible
- `engagement_private` — within a specific client engagement; tighter
  boundary than `internal`
- `local_only` — never committed (planning workspaces, `.gitignored`
  scratch, dogfooding captures)

A leak in `public_oss` is critical. The same token in `local_only` is a
hygiene matter, not a CI block.

## Non-Goals

`limensafe` deliberately does not aim to be:

- A **credential scanner**. It coexists with gitleaks/TruffleHog;
  it does not replace them.
- A **regulated-PII DLP**. Presidio and cloud DLP services serve that
  purpose better. CCL may overlap with PII when client personnel names
  are involved, but the primary detection model is org-defined vocabulary.
- A **runtime data clean room**. Tonic/Skyflow/Privera-style substitution
  pipelines for production data are out of scope. (A future L5
  proxy/databridge layer may approach that space; v1 is not it.)
- A **code review tool**. Semgrep/Opengrep enforce coding patterns.
  `limensafe` enforces vocabulary policy.
- A **prose autofix tool**. v1 may suggest replacements for literals,
  filenames, and identifiers, but free-form prose autofix is advisory
  only. Semantic preservation is too costly to guarantee.
- A **content-safety filter for LLM outputs at inference time**. That
  is a runtime guard problem; v1 is a build-time/commit-time scanner.

## Success Criteria

A successful release holds these properties:

1. **Zero-leak scanner output (full surface).** No catalog-protected
   substring appears in any output stream — `stdout`, `stderr`, JSON
   output, log files, or persisted reports — under default settings.
   The invariant covers **all emitted strings**, not only matched
   evidence text:
   - Evidence text and snippets — never include raw protected text
   - Path segments — paths are scan-root-relative; segments matching
     aliases are redacted
   - Branch names and commit messages — surfaces identified by enum,
     never echoed as raw text
   - Catalog source paths — emit catalog IDs and load status only
   - All output-visible IDs — catalog IDs, entity IDs, rule IDs, and
     replacement IDs **must not contain protected vocabulary**
     (schema validation; warn at v0, error at v1.x)

   Verifiable via post-scan grep over all output streams returning
   zero. See ADR-0003 for the implementation contract.

2. **Catalog separation.** The repo configuration
   (`.limensafe/config.yaml` and `.limensafeignore`) contains no raw
   protected vocabulary. All entity references are by ID or class.
3. **Catalog survives turnover.** A contributor who has never read
   the private catalog cannot accidentally introduce a leak that the
   scanner misses, *given the catalog is current*.
4. **Severity adapts to scope.** A token in `public_oss` blocks; the same
   token in `internal` warns; in `local_only` it is hygiene-only.
5. **Honest substitutions are not penalized in their permitted scope.**
   A sanctioned codename in a private engagement repo where it is
   `allowed_in` does not produce a finding. The same codename in a
   `public_oss` repo, where it is `blocked_in`, does.
6. **Co-occurrence catches triangulation.** When two tokens individually
   below threshold appear together within a defined window, severity
   escalates per rule.
7. **Replacement suggestions are conservative.** Auto-fix is allowed for
   simple literals and filenames; advisory only elsewhere. Public OSS
   prefers neutral generics over codenames.
8. **Performance is CI-acceptable.** Default scan of a typical Go/JS/Python
   monorepo completes within seconds, not minutes.

## Stakeholder Map

### Primary wedge — consulting firms / agencies

Firms that develop tools in OSS while applying them to private client
engagements. Confidentiality of the client *relationship* (not only
data) is a hallmark property. Multiple concurrent engagements with
overlapping vocabularies. Bias toward private catalogs that travel
with the engagement, not the repo.

### Natural expansion — enterprise R&D

Large product orgs protecting unannounced product codenames. Internal
documentation that may surface in support, vendor exchanges, or
public docs. Catalogs are org-wide, with project sub-scopes.

### Natural expansion — AI builders

Organizations training models, building agents, or shipping LLM-powered
features that fear prompt/eval leakage. Need to scan: prompt templates,
eval golden fixtures, RAG corpora, agent memory exports, MCP tool
descriptions. This is the OWASP LLM07 (System Prompt Leakage) class.

### Adjacent — OSS maintainers

Maintainers of OSS projects who occasionally apply tools to private
collaborations. Lighter-weight use case: per-project catalog, no
central distribution.

### Adjacent — regulated teams

Teams with non-PII confidentiality controls (financial deal codenames,
HR investigation labels, M&A project names) — same shape, different
industry vertical.

## What V1 Defers

These are real concerns we acknowledge but explicitly defer:

- **Catalog distribution beyond files.** v1 catalogs are file-based
  bundles referenced by ID. Control plane and signed distribution come
  in v2.
- **NER and ML-driven detection.** v1 is deterministic. NER is a
  phase-2 plugin (likely via Presidio or ONNX-runtime) for catching
  entity classes the catalog couldn't enumerate (novel person names,
  addresses).
- **Multi-format extractors.** v1 covers source files, paths, git
  surfaces, JSON, YAML, Markdown. Office docs, PDFs, notebooks,
  binaries, containers, and AI artifacts are phase-2 extractors over
  the same detector core.
- **Runtime substitution / proxy.** The L5 proxy/databridge layer
  ships when agent-workflow runtime guards earn the platform shape.
- **MCP server.** The L6 agent-facing wrapper lands once the v1
  contract is proven.

## Open Questions

- **Confidence scoring.** How do we represent confidence on top of
  severity? (Severity = "if this is real, how bad?"; confidence = "how
  sure are we it's a real match, not a false positive?")
- **Replacement authority.** Are replacement suggestions catalog-authored
  only, or can the engine generate them? If generated, how do we ensure
  the generated replacement isn't itself a breadcrumb?
- **Cross-engagement isolation.** When a developer works on multiple
  engagements simultaneously, how do we ensure catalogs don't bleed
  across? (Likely a profile/context layer in the config.)
- **Audit trail.** Should the scanner emit a fingerprinted audit record
  of findings (with no raw values) for compliance evidence?

## Cross-References

- [`existing-tools-gap.md`](existing-tools-gap.md) — why no current
  tool covers this class
- [`architecture.md`](architecture.md) — layered model, extractor
  interface, finding model
- [`catalog-schema.md`](catalog-schema.md) — vocabulary and config
  formalism
- [ADR-0003](../decisions/ADR-0003-redaction-safe-output.md) — the
  redaction-safe output contract that operationalizes Success
  Criterion 1

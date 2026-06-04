# limensafe Architecture

Status: review-ready draft
Owner: entarch
Last updated: 2026-04-29

## Summary

`limensafe` is a proposed standalone tool and library for detecting
Confidential Context Leakage (CCL): organizationally private context appearing
in artifacts that are public, shareable, or outside the intended boundary.

The tool should start as a deterministic, local-first scanner with a CLI,
redaction-safe JSON output, and integration points for git hooks, CI, and
databridge modes.

## Core Principles

- Protect context, not only credentials.
- Keep private vocabulary out of public repositories.
- Never create a leak in scanner output.
- Make the detector core source-agnostic.
- Make extractors additive, not special cases in the engine.
- Prefer deterministic behavior for v1.
- Treat repo visibility and sharing boundary as first-class policy inputs.
- Design for agents and humans using the same structured result model.

## Layered Model

### L0: Catalog

Vocabulary and policy bundles describing protected entities, aliases, codenames,
co-occurrence rules, replacement suggestions, and scope restrictions.

V1 catalog storage is file-based. Later versions can distribute catalogs through
a control plane.

### L1: Engine Library

Pure detector core:

```text
(normalized spans, resolved catalog, scan policy) -> findings
```

The engine does not walk files, parse git, decode notebooks, or inspect PDFs.
It consumes normalized spans emitted by extractors.

### L2: CLI

Developer-facing command line interface for local scans:

```bash
limensafe scan .
limensafe scan --staged
limensafe scan --diff origin/main...HEAD
limensafe scan --include-ignored .
limensafe check --format json
```

### L3: Hooks, CI, and goneat Adapter

Workflow integration:

- pre-commit and pre-push hooks
- GitHub Actions or generic CI
- goneat adapter, likely as a security/confidentiality assessment category
- SARIF or similar export if useful for code-host annotations

### L4: Control Plane

Optional future layer for central catalog distribution, policy assignment,
audit trails, team/org mapping, and fleet visibility.

This should not be required for v1.

### L5: Proxy or Databridge

Optional future runtime guard for agent I/O, logs, uploaded documents, API
bodies, or prompt/eval flows.

This is where substitution-on-the-fly and policy mediation can live if the
product grows toward a a sibling repo-like shape.

### L6: MCP Server

Optional future agent-facing wrapper over the CLI/API:

- `scan_path`
- `scan_text`
- `scan_diff`
- `suggest_fix`
- `explain_finding`

MCP should wrap a stable core contract rather than define the core behavior.

## V1 Cut-Line

V1 should include L0 through L3 only:

- deterministic catalog engine
- repo-safe config and private catalog references
- filesystem and git extractors
- source/text/JSON/YAML/Markdown scanning
- branch name, file path, staged diff, and commit message scanning
- redaction-safe JSON output
- basic replacement suggestions
- hook/CI/goneat integration shape

V1 should not require:

- central control plane
- MCP server
- Presidio or other Python runtime dependency
- NER/ML detection
- PDF/DOCX/notebook/binary parsing, except as spike fixtures or later extractors
- automatic rewriting of complex prose/code

## Configuration Model

Separate public policy from private vocabulary.

Repo-safe config may live in public repositories:

```text
.limensafe/config.yaml
.limensafeignore
```

It can declare:

- schema version
- repository visibility/scope, such as `public_oss`, `internal`, or `client_private`
- catalog IDs or catalog source profiles
- severity overrides
- extractor settings
- path include/exclude patterns

It must not contain raw client names, people names, private domains, or other
protected vocabulary.

Private catalog bundles should live outside public repositories, for example in
developer secret storage, encrypted provisioning bundles, or a later control
plane. Repo config references them by ID, profile, or environment variable.

## Ignore and Include Semantics

`limensafe` should support gitignore/goneatignore-style traversal:

- doublestar patterns
- inherited ignore files throughout a tree
- shell-bang patterns to re-include content that an earlier pattern excluded
- `.gitignore` awareness by default
- explicit `--include-ignored` for local hygiene scans of planning folders,
  scratch logs, and dogfooding artifacts

The exact merge semantics should be schema-documented and testable.

## Source-Agnostic Input Contract

All extractors should produce normalized input units. Pseudocode:

```go
type InputUnit struct {
    SourceID     string            // stable ID for the scanned source
    SourceKind   SourceKind        // file, git_diff, branch, commit_msg, stdin, api_body, etc.
    LocationHint string            // human-readable source location
    Content      []byte            // original or decoded bytes for this unit
    Encoding     string            // utf-8, binary, extracted-text, etc.
    Metadata     map[string]string // repo scope, file mode, parser info, etc.
}
```

Structured extractors then emit spans:

```go
type InputSpan struct {
    UnitID      string
    Surface     SurfaceKind       // content, path, metadata, branch, commit_message, etc.
    Text        string
    ByteStart   int
    ByteEnd     int
    LineStart   int
    LineEnd     int
    ColumnStart int
    ColumnEnd   int
    Path        string            // JSONPath, YAML path, XML path, notebook cell path, etc.
    Metadata    map[string]string
}
```

The engine only sees spans and metadata. It does not care whether a span came
from source code, an XML attribute, a notebook output, a PDF text layer, or an
API body.

## Extractors

V1 extractors:

- filesystem walker
- path/filename scanner
- git branch scanner
- git staged diff scanner
- git diff range scanner
- commit message scanner
- plain text scanner
- Markdown scanner
- JSON/YAML scanner using decoded values plus structured paths

Planned extractors:

- XML/HTML: decode first, raw fallback
- notebooks: metadata, source cells, output cells
- DOCX/PPTX/XLSX: visible text, properties, comments, tracked changes, notes
- PDF: text layer, metadata, forms, OCR layer
- binary artifacts: strings, debug paths, build metadata
- containers/SBOM/log archives
- API body or request/response streams
- AI artifacts: prompts, evals, RAG corpora, memory exports

## Detector Core

V1 should be deterministic:

- exact dictionary matching
- token/slug/path-segment matching
- generated variants, such as case, separators, pluralization, and common slugs
- regex rules for operational identifiers
- co-occurrence rules over a span graph
- scoped codename policy

Aho-Corasick or equivalent multi-pattern matching should be considered for
dictionary performance.

NER/ML should be a plugin later, not a v1 dependency.

## Finding Model

Findings must be redaction-safe by default.

```go
type Finding struct {
    ID             string
    Fingerprint    string
    Severity       Severity
    Confidence     Confidence
    Decision       PolicyDecision
    EntityID       string        // opaque catalog entity id, redaction-safe
    EntityClass    string        // client_identity, codename, person, project, system, etc.
    DetectorID     string
    RuleID         string
    SourceKind     SourceKind
    Surface        SurfaceKind
    Location       Location
    ReplacementID  string        // optional suggestion reference, not raw value
    Message        string        // no matched protected text by default
    EvidenceShape  string        // e.g. literal, slug, cooccurrence, regex
}
```

Default output must not include matched text. It may include `entity_id`
because catalog validation rejects entity IDs containing protected alias
substrings before scan output is emitted. A verbose mode can reveal values only
with an explicit flag and warning. CI defaults should remain redaction-safe.

## Fingerprinting

Findings need stable correlation without leaking protected values.

Recommended v1 approach:

- HMAC over detector ID, normalized protected value, source path, and span shape
- key/salt provided by local profile or private catalog context
- fallback salted digest for local-only scans
- no raw protected text in the fingerprint payload or output

Open question: whether location should be included in the fingerprint. Including
location makes findings stable for a given file but changes on moves. Excluding
location improves term-level correlation but can collapse repeated findings.

## Replacement Suggestions

V1 suggestions should be conservative:

- simple literal replacement suggestions are OK
- filename/path replacement suggestions are OK if reversible by user review
- prose/code autofix should be advisory unless semantics are clearly safe
- public OSS should prefer generic substitutes over engagement codenames
- private engagement repositories can allow scoped codenames if policy permits

## Severity Model

Severity should combine:

- repo visibility/scope
- entity class
- surface type
- co-occurrence strength
- whether the item is committed, staged, ignored, or local scratch
- whether a replacement suggestion exists

Example policy direction:

- public OSS + client identity in committed content: high/critical
- public OSS + sanctioned codename in branch/path: medium/high breadcrumb risk
- internal repo + scoped codename: allow or info
- private engagement repo + real client identity: warning unless policy blocks
- ignored local scratch + protected value: warning for hygiene, not CI block

## Output Contracts

Formats:

- human summary
- JSON for agents/CI
- optional SARIF later

Default JSON should include:

- scan metadata
- policy version
- catalog bundle IDs, not raw catalog content
- counts by severity and surface
- redaction-safe findings
- nonzero exit code when policy decisions require blocking

Default JSON should not include:

- matched protected strings
- raw snippets containing protected strings
- private catalog entries
- unredacted replacement values if they disclose a protected mapping

## Open Questions

- Should planning workspace be initialized as git immediately?
- What minimum public schema belongs in v0?
- How much of JSON/YAML structured location support is v1 versus spike?
- Should `limensafe` produce SARIF in v1 or defer until CI usage is proven?
- What is the best default HMAC key source for local-only use?
- How should catalog bundle integrity and versioning work before a control plane?

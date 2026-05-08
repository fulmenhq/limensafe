# Existing Tools Gap Analysis

Status: review-ready draft
Owner: entarch
Last updated: 2026-04-29

## Summary

No surveyed tool appears to cover Confidential Context Leakage (CCL) as a
first-class developer workflow problem.

The adjacent ecosystems are strong but incomplete:

- secret scanners find credentials
- DLP/PII tools find regulated personal data
- document scrubbers redact PDFs, Office documents, and images
- policy scanners enforce generic rules

CCL needs a combined model: organization-specific vocabulary, repository
visibility policy, multi-surface extraction, co-occurrence rules, replacement
guidance, and redaction-safe reporting.

## What CCL Is Not

CCL is not only secret leakage. Many CCL tokens are ordinary words or valid
business names. They are sensitive because of context, audience, repository
visibility, engagement boundaries, or co-occurrence.

CCL is not only PII. It includes companies, clients, internal projects,
codenames, cloud resource names, product roadmap clues, hostnames, account
labels, branch names, test fixture names, and generated artifact metadata.

CCL is not only document redaction. It starts in developer workflows: tests,
fixtures, examples, docs, diffs, commit messages, PR text, logs, and agent
artifacts.

## Tool Categories Surveyed

### Secret Scanners

Representative tools:

- gitleaks
- TruffleHog
- detect-secrets
- ggshield / GitGuardian
- Semgrep Secrets
- Talisman
- git-secrets
- Whispers

Strengths:

- mature pre-commit and CI workflows
- fast git/diff/history scanning
- known credential patterns
- entropy checks
- credential verification in some tools
- SARIF/JSON outputs in several tools
- custom regex support in several tools

Gaps for CCL:

- treat organization-specific identities as ad hoc regex rules
- no first-class entity catalog
- no scoped codename/replacement model
- no repo visibility severity model
- no co-occurrence semantics for triangulation
- limited or no safe replacement guidance
- typically code/git focused, not broad artifact/source extraction
- output can echo matched values unless carefully configured

Best stopgap:

- gitleaks with private custom rules can catch some known terms in code and
  diffs, but it remains credential-scanner-shaped.

### DLP and PII Frameworks

Representative tools:

- Microsoft Presidio
- scrubadub
- Phileas
- Google Cloud DLP
- AWS Comprehend PII or Macie-adjacent services
- commercial DLP platforms

Strengths:

- strong PII taxonomies
- custom recognizers or deny lists
- anonymization/redaction operators
- NER support in some systems
- text and structured-data support in some systems
- mature compliance framing

Gaps for CCL:

- oriented toward regulated person data, not confidential relationships
- not repo-native by default
- do not understand git surfaces such as branch names, commit messages, staged
  diffs, PR bodies, or file paths as developer risk surfaces
- no natural repo visibility model
- no catalog separation pattern for OSS repository config versus private client
  vocabulary
- no replacement workflow aimed at tests/fixtures/docs
- cloud DLP may be unsuitable for sensitive local client vocabulary

Best substrate:

- Presidio is a strong conceptual reference for recognizers, deny lists, and
  anonymizers. For v1, reimplementing a narrow deterministic subset in Go is
  likely lower friction than embedding Python or operating a sidecar.

### Document Redaction Tools

Representative tools:

- scrubfile
- RedactDesk
- redacta
- redacter-rs
- mat2 and exiftool for metadata
- Adobe/Apryse/enterprise document redaction products

Strengths:

- PDF, DOCX, image, and OCR workflows
- metadata scrubbing
- permanent redaction in some tools
- local-only operation in newer tools
- JSON/API/MCP support in some emerging tools

Gaps for CCL:

- usually document-first, not repo-first
- not designed for pre-commit, staged diff, branch name, commit message, or CI
  policy
- usually PII-centered rather than org-entity centered
- custom vocabulary support varies
- not designed to produce code-review replacement suggestions
- may not support private catalog layering or repo classification

Important lesson:

- Document extractors should become limensafe plugins rather than v1 blockers.
  The core engine should be able to consume text/spans from tools like these.

### Policy and Static Analysis Tools

Representative tools:

- Semgrep / Opengrep
- OPA / Conftest
- custom linters
- CI policy engines

Strengths:

- configurable rules
- CI integration
- policy-as-code patterns
- structured output
- rich code matching in some tools

Gaps for CCL:

- users still need to build the confidential-entity model
- generic policy engines do not solve extraction, catalog secrecy, or safe
  reporting by themselves
- code-oriented matchers do not cover all artifact surfaces
- custom rules can become noisy and difficult to maintain
- no built-in replacement/codename/co-occurrence model

Best use:

- possible integration targets or output consumers, not the core product shape.

### Notebook and Data Artifact Tools

Representative tools:

- nbstripout
- jupyter cleaners
- ad hoc data sanitizers

Strengths:

- can remove notebook outputs wholesale
- simple to wire into git hooks

Gaps for CCL:

- no selective org-entity detection
- no replacement suggestions
- output stripping can be too blunt for documentation notebooks
- no cross-surface catalog model

Important lesson:

- Notebook outputs are high-risk and should be an early extractor plugin.

### Supply Chain and Artifact Scanners

Representative tools:

- Syft/Grype
- Trivy
- container scanners
- binary string scans
- SBOM validators

Strengths:

- artifact/package visibility
- CI-friendly
- mature vulnerability workflows

Gaps for CCL:

- not built for confidential names or relationship disclosure
- may reveal absolute paths, usernames, or build metadata but not classify them
  as CCL
- no organization-specific vocabulary policy

Important lesson:

- Compiled artifacts and SBOM metadata are legitimate later surfaces.

## Gap Matrix

| Capability                      | Secret Scanners | DLP/PII            | Doc Scrubbers  | Policy Scanners      | limensafe Need       |
| ------------------------------- | --------------- | ------------------ | -------------- | -------------------- | -------------------- |
| Credential detection            | strong          | mixed              | mixed          | mixed                | integrate or coexist |
| Client/company entity catalog   | weak            | custom only        | custom only    | custom only          | first-class          |
| Codename policy                 | weak            | weak               | weak           | weak                 | first-class          |
| Co-occurrence severity          | weak            | weak               | weak           | possible but manual  | first-class          |
| Repo visibility model           | weak            | weak               | weak           | possible but manual  | first-class          |
| Git staged/diff workflow        | strong          | weak               | weak           | mixed                | first-class          |
| Branch/commit message scanning  | mixed           | weak               | weak           | weak                 | first-class          |
| File path scanning              | mixed           | weak               | weak           | weak                 | first-class          |
| Office/PDF extraction           | weak            | mixed              | strong         | weak                 | plugin               |
| Notebook output scanning        | weak            | weak               | weak           | weak                 | plugin               |
| Redaction-safe findings         | mixed           | mixed              | mixed          | mixed                | default invariant    |
| Replacement suggestions         | weak            | anonymization only | redaction only | autofix in code only | first-class          |
| Private vocabulary outside repo | possible        | possible           | possible       | manual               | first-class          |

## Why Not Just Use Gitleaks Custom Rules?

Gitleaks is a good immediate comparison because it is fast, portable, and already
fits pre-commit/CI. But CCL needs a richer model than rule = regex:

- A company name, codename, bucket prefix, email domain, and branch slug may be
  different aliases for the same protected entity.
- A sanctioned codename can be allowed in one visibility scope and blocked in
  another.
- Two individually low-risk terms can become high-risk when found together.
- Public repository config should not contain the raw terms being protected.
- Findings must be safe to post in CI logs without echoing the match.
- The scanner should suggest neutral replacements, not only fail a build.

Custom rules can approximate the first detection pass. They do not provide the
product model.

## Why Not Just Use Presidio?

Presidio is the best conceptual reference among PII tools. It already has
recognizers, deny lists, context enhancers, and anonymizers.

Reasons not to make it the v1 core:

- Python runtime dependency complicates a small developer CLI.
- NER is not required for the first consulting-firm use case.
- The hard product gap is catalog/policy/workflow, not NLP availability.
- Repo-native git surfaces still need to be built around it.

Presidio should remain a later plugin option for NER-enhanced detection.

## What Is Genuinely New

`limensafe` would be differentiated by combining:

1. Org/entity catalogs as a first-class model.
2. Private vocabulary bundles separate from public repo config.
3. Scoped codename and replacement policies.
4. Co-occurrence rules over matched spans.
5. Repo visibility and surface-aware severity.
6. Redaction-safe findings by default.
7. Multi-surface extractors over a common detector core.
8. Developer and agent workflows from the same JSON contract.
9. Replacement suggestions aimed at fixtures, examples, docs, and paths.

## Positioning

Potential positioning line:

```text
limensafe prevents confidential context from leaking into public or shared
artifacts, filling the gap between secret scanners and DLP tools.
```

Target wedge:

- consulting firms and agencies that build OSS tools while applying them to
  private client work

Natural expansion:

- enterprise R&D protecting product codenames
- AI-builder organizations protecting prompts, evals, memory exports, and RAG
  corpora
- regulated teams that need custom non-PII confidentiality controls

## Open Questions

- Should limensafe integrate with existing secret scanners or stay strictly
  focused on CCL?
- Which document extractors should be first after v1?
- Should SARIF be part of v1, or is JSON enough for the spike?
- Should replacement suggestions be catalog-authored only, generated, or both?
- How should organizations distribute private catalogs before a control plane?

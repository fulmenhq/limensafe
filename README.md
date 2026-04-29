# limensafe

> Find the disclosure boundary before private context crosses it.

**Status**: pre-v0 spike. CDRL'd from `forge-workhorse-groningen` on 2026-04-29.
Working name; subject to namelens review. Repo is local-only — not yet pushed.

`limensafe` is a fast local CLI and Go library for preventing Confidential
Context Leakage (CCL): ordinary-looking names, codenames, paths, branches,
fixtures, docs, logs, and agent artifacts that reveal private organizational
context when they cross into public or shared surfaces. It sits between secret
scanning and DLP: not looking for credentials or regulated PII alone, but for
the relationship and context clues that organizations define as confidential.
Schema-backed catalogs, repo-safe configuration, redaction-safe output, and
pre-commit/CI workflows make the safe path mechanical instead of memory-based.

## What it is

`limensafe` detects organizationally-private context — client identities,
internal codenames, project relationships, person names tied to engagements,
hostnames implying topology — appearing in artifacts that are or could become
public. It fills the gap between credential scanners (gitleaks, TruffleHog) and
PII redaction (Presidio, Comprehend): a class of leakage where the protected
tokens look like ordinary words and require organization-defined vocabulary
to detect.

## Why

Consulting firms, enterprise R&D teams protecting unannounced product
codenames, and AI-builder organizations all face the same failure mode: in the
heat of getting work done, real client / project / person tokens end up in
test fixtures, sample data, branch names, commit messages, and AI artifacts
that ship publicly. No existing tool combines:

1. Org-specific entity catalogs as a first-class model
2. Sanctioned-codename allowlist (with scope policy)
3. Co-occurrence rules (e.g., `<client> + <codename>` together = critical)
4. Repo visibility severity model (`public_oss`, `internal`, `engagement_private`)
5. Replacement suggestions
6. Multi-surface coverage (code, paths, branches, commits, docs, AI artifacts)
7. Redaction-safe output (the scanner's own report cannot leak)

`limensafe` does.

## Architecture (v0)

Layered model — see [`docs/design/architecture.md`](docs/design/architecture.md):

```
L0 — Catalog        vocabulary bundles (private) + repo config (public)
L1 — Engine         deterministic detectors over span graph
L2 — CLI            scan / check / hooks
L3 — Hooks / CI     pre-commit, pre-push, GH Actions, goneat adapter
                    --- v0 ships L0–L3 ---
L4 — Control plane  catalog distribution, fleet policy (later)
L5 — Proxy          runtime guard for agent I/O / logs / docs (later)
L6 — MCP            agent-facing wrapper (later)
```

## V0 Spike Status

- 2-day first pass scoped: minimum scanner that proves the zero-leak
  invariant against the synthetic corpus + perf-smoke against a large repo
- Phase 1 work split (cxotech: catalog/extractor/output/policy/CLI;
  entarch: schemas/engine)
- Acceptance corpus: [`testdata/synthetic-acme/`](testdata/synthetic-acme/)
  — uses canonical placeholder vocabulary (`acme`, `horizon`, `tilden`)
- Public-tier vs private-tier catalog separation (chicken-and-egg solved
  by repo config referencing catalogs by ID; raw vocabulary lives outside
  the repo)

## Design Documents

- [`docs/design/architecture.md`](docs/design/architecture.md) —
  layered L0–L6 model, extractor and finding contracts
- [`docs/design/catalog-schema.md`](docs/design/catalog-schema.md) —
  vocabulary bundle + repo config schema, layered resolution, ID-safety rule
- [`docs/design/existing-tools-gap.md`](docs/design/existing-tools-gap.md) —
  why secret scanners, DLP/PII tools, and policy engines don't cover this
- [`docs/decisions/ADR-0003-redaction-safe-output.md`](docs/decisions/ADR-0003-redaction-safe-output.md) —
  the implementation contract for the zero-leak invariant

## License

Apache-2.0. See [LICENSE](LICENSE).

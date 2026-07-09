# ADR-0006: Catalog Two-Layer Schema Validation (runtime + build-time)

**Status**: Accepted
**Date**: 2026-06-15
**Deciders**: @3leapsdave
**Role**: devlead

## Context

internal-brief published the catalog JSON Schema
(`schemas/limensafe/v1/catalog.schema.json`, draft 2020-12) as the structural
contract for operator vocabulary catalogs. Publishing the contract is not the
same as enforcing it: until the loader validates operator catalogs against the
schema at scan time, an adopter can author a structurally-invalid catalog and
get silent, surprising behavior. Two enforcement points are needed, and they
have different constraints:

1. **Build-time** — our own schema files must never ship malformed. This is
   already handled by goneat meta-validation (`make meta-validate-schemas`,
   wired into `lint` → `check-all`), per ADR-0004's multi-draft stance.
2. **Runtime** — every `limensafe scan` must validate the operator-supplied
   catalog against the schema, in the shipped binary, with diagnostics that
   never leak protected vocabulary (ADR-0003).

The runtime layer cannot reuse the config loader's mechanism. `internal/config`
validates via `gofulmen/schema.NewCatalog(<projectRoot>/schemas)`, which reads
schema files from disk and only works when a project root is discoverable
(in-repo / dev). gofulmen v0.3.5 exposes no embed/`fs.FS` constructor, so in an
installed binary outside the repo there is no `schemas/` tree and that
validation silently no-ops. A catalog contract that only validates in-repo is
not a contract.

## Decision

Enforce the catalog schema in **two layers, by two mechanisms**:

- **Build-time (unchanged):** goneat meta-validates every `schemas/**` file
  against its declared JSON Schema draft via `make meta-validate-schemas`.
- **Runtime (new):** embed the canonical catalog schema into the binary and
  validate operator catalogs against the embedded copy with
  `santhosh-tekuri/jsonschema/v5` (already a direct dependency), inside
  `pkg/catalog.LoadBytes`.

Supporting decisions:

- **Embedded mirror with a drift gate.** The canonical schema lives at
  `schemas/limensafe/v1/catalog.schema.json`; a byte-identical mirror is kept
  at `internal/assets/schemas/limensafe/v1/catalog.schema.json` and embedded
  via `//go:embed`. `make sync-embedded-schemas` refreshes it;
  `make verify-embedded-schemas` (wired into `check-all` and `pr-final`) fails
  the build on drift, so the runtime can never validate against a stale
  contract. This mirrors the existing `sync/verify-embedded-identity` pattern.
- **Redaction-safe diagnostics (ADR-0003).** Schema validation errors are
  rendered from `(instance JSON-pointer, failing keyword)` only. The validator
  library's free-text message — which can in principle carry instance
  fragments — is deliberately discarded. The same value-free rule applies to
  every diagnostic emitted before the schema renderer runs: the pre-schema
  `schema_version` policy and the YAML→JSON decode path are operator-controlled
  inputs, so their error text names neither the offending value nor wrapped
  library text. No protected alias, token, replacement, or raw source line can
  appear on any stream; the guarantee holds by construction, not by trusting
  the upstream library. A redactor backstop (scrubbing diagnostics through the
  catalog's own alias set) was considered and **deliberately not added** for
  v1: pointer+keyword is sufficient and a backstop would be only partial while
  no trusted catalog/redactor exists at that point in the load (secrev).
  - **Caveat for future schema evolution (secrev):** the pointer-only guarantee
    relies on object members being fixed schema field names and arrays being
    index-addressed, so a JSON pointer never carries operator content. If the
    v-next schema introduces user-keyed maps, `patternProperties`, or otherwise
    dynamic property names, JSON pointers can carry those names — revisit this
    decision (and whether a redactor backstop is then warranted) before
    shipping that schema.
- **Version policy.** A non-`1` (or unparseable) `schema_version` major is a
  hard config error (`ErrConfigInvalid` → exit 2). A higher minor/patch within
  major 1 loads and surfaces an advisory `LoadWarning`.
- **Compatibility window for `$schema`.** Omitting `$schema` is accepted with a
  `LoadWarning` for compatibility. The schema already models `$schema` as an
  optional const; the loader adds the advisory. Making it required remains a
  future hardening change that should update docs and tests in the same PR.
- **Layering.** The JSON Schema is the structural front gate; the Go-level
  `Catalog.Validate` remains the semantic-safety layer (duplicate ids,
  output-visible ID alias-safety) that the schema does not express. Structural
  error messages therefore move from Go text to schema pointer+keyword form.

Implementation references:

- Validator + policy: `pkg/catalog/schema.go`
- Loader wiring: `pkg/catalog/catalog.go` (`LoadBytes`)
- Embedded asset: `internal/assets/schemas/embedded.go`
- Sync/verify: `scripts/{sync,verify}-embedded-schemas.sh`, `make verify-embedded-schemas`
- Tests: `pkg/catalog/schema_runtime_test.go`, `pkg/catalog/catalog_schema_test.go`

## Consequences

- The catalog contract is enforced identically in-repo and in an installed
  binary; internal-brief (term-list → catalog) can rely on built output validating at
  scan time.
- A second copy of the schema exists (canonical + embedded mirror); the drift
  gate makes divergence a build failure rather than a silent risk.
- Structural-failure diagnostics are terser (pointer + keyword) than the prior
  Go messages, in exchange for an airtight zero-leak guarantee. Existing tests
  that asserted the old message text were updated to the schema form.
- Co-occurrence `window_kind` / `window_size` runtime semantics remain a
  separate follow-up (out of scope here), as flagged in the internal-brief review.

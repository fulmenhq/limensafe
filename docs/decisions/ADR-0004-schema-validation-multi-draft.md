# ADR-0004: JSON Schema Validation Multi-Draft Support

**Status**: Accepted
**Date**: 2026-02-05
**Deciders**: @3leapsdave

## Context

CDRL consumers frequently integrate with external systems that publish JSON Schemas
in different drafts (legacy and modern). Requiring a single draft forces unnecessary
translation work and breaks compatibility with upstream schemas.

## Decision

Support validation across all major JSON Schema drafts by auto-detecting the draft
from the schema's `$schema` field.

- Supported drafts: Draft-04, Draft-06, Draft-07, Draft 2019-09, Draft 2020-12.
- Validate schemas themselves (meta-validation) as part of tests.

Implementation references:

- Guide: `docs/schema-validation.md`
- Fixtures: `testdata/schemas/`
- Tests: `internal/config/schema_flavors_test.go`

## Consequences

- Better compatibility for consumers using externally-defined schemas.
- Fewer manual steps during CDRL refits.
- Slightly broader test surface; mitigated via fixtures and meta-validation.

## Alternatives Considered

- Support only the latest draft.
  - Rejected: breaks integration with common legacy drafts.
- Require explicit draft selection in configuration.
  - Rejected: adds friction and is redundant with `$schema`.

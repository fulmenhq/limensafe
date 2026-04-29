# Architecture Decision Records (ADRs)

This directory records the key architectural decisions for this workhorse template.
The goal is to help CDRL consumers understand the "why" behind default patterns
and safely refit the template for their own service.

## Index

- ADR-0001: Control Plane and Data Plane Split (`ADR-0001-control-plane-data-plane-split.md`)
- ADR-0002: Control Plane Auth and Loopback Defaults (`ADR-0002-control-plane-auth-localhost.md`)
- ADR-0003: Data Plane Auth Route Policy (`ADR-0003-data-plane-auth-route-policy.md`)
- ADR-0004: JSON Schema Validation Multi-Draft Support (`ADR-0004-schema-validation-multi-draft.md`)

## Creating a New ADR

1. Copy `ADR-template.md`
2. Assign the next ADR number (zero-padded)
3. Keep it short (1-2 pages)
4. Link to relevant code and docs

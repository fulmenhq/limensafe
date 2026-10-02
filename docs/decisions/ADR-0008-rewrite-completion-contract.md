# ADR-0008: Rewrite-completion contract

**Status**: Accepted
**Date**: 2026-06-22
**Deciders**: @3leapsdave
**Role**: devlead

## Context

limensafe positions itself as the structured, redaction-safe tool for
confidentiality work — the principled alternative to `git grep`-based
remediation. A core use case is scrubbing protected vocabulary from git history
before a repository goes public, using `git filter-repo` to perform the rewrite
and limensafe to verify it.

There is a structural gap between "the rewrite tool exited cleanly" and "the
repository is safe to publish." A history rewrite is destructive, so operators
reasonably create safety-net artifacts before running it — most commonly a
backup branch or tag preserving the pre-rewrite state. When that safety net is
left **on the remote the repository publishes from**, it re-exposes exactly the
vocabulary the rewrite removed: the backup ref carries the un-scrubbed history
verbatim, one `git clone --mirror` away. The rewrite's intent (remove the terms
from the publish surface) is defeated by the rewrite's own implementation
(moving them onto another ref on the same surface).

This is amplified by team handoff: the operator who runs the rewrite is rarely
the one who clears the repo for publication, and a convention carried only in
operator memory loses on every handoff. A scan of `main` — even full-history —
is structurally blind to other refs, so post-rewrite verification aimed at the
primary line does not catch it.

internal-brief (`audit-publish`) made the publish surface — every ref the remote would
expose — the unit of audit, and flags leak-vector refs (content divergence +
danger-name patterns). What was missing was a **named contract** for when a
rewrite is complete, durable against document drift and independent of any
single operator's memory.

## Decision

**A history rewrite performed for confidentiality remediation is complete only
when the publishable surface satisfies all of the following:**

1. The surfaces the chosen rewrite mode targets carry no residual catalog
   matches on any publishable ref.
2. Every artifact the rewrite **created** has been removed from the publishable
   remote: backup branches and tags (the `audit-publish` default danger
   patterns `backup/*`, `*pre-rewrite*`, `*-snapshot-*`, `archive/*`, `*-bak`,
   `wip/*`), any committed term-list / replacement
   file, any committed callback scripts, and any accidentally-tracked operator
   memos.
3. `limensafe audit-publish --mode release` run from a **fresh clone** of the
   publishable remote exits `0` (`summary.publish_safe: true`). Inspect
   `summary.coverage`: `incomplete` blocks even with zero findings;
   `acknowledged` and `complete` remain passing coverage statuses.
4. The completion is recorded in an operator-private incident note citing the
   chosen rewrite mode and the verification artifact.

**The contract is scoped to the publishable surface, not to absolute history
destruction.** Disaster-recovery copies of pre-rewrite state are legitimate and
encouraged — they must simply live off the publishable remote (operator-private
storage, a different remote, or a local mirror).

**Enforcement (tool-side):**

- `audit-publish` flags a danger-name ref as a leak vector **by name alone**,
  independent of scanned content and even when its objects are not fetched.
  `summary.publish_safe` **cannot be `true`** while any backup-pattern ref
  exists on the audited surface — this is a contract guarantee, not a function
  of content findings, so a danger-name ref is effectively tier-elevated above
  ordinary block-tier content findings.
- Each leak-vector ref emits a redaction-safe, human-readable rewrite-hygiene
  warning on stderr (the loud backup-defeat copy), naming the defeat scenario
  and the removal action. Detection and advice only — limensafe never deletes
  or rewrites refs.
- The danger-name pattern set is configurable (`--danger-pattern`) for
  organizations using different safety-net conventions.

## Consequences

- **Positive**: the contract is stated once, enforced mechanically by
  `audit-publish` at publish time from a fresh clone, and survives team handoff
  because the tool speaks it rather than relying on operator memory. It composes
  with the scan-attestation gate ([ADR-0005](ADR-0005-scan-attestation-gate.md)):
  a release that goes public should require `publish_safe: true`.
- **Neutral**: the contract is methodology + enforcement, not automation —
  limensafe deliberately does not perform ref deletion (consistent with the
  internal-brief/internal-brief detection-only boundary). The operator runs
  `git push --delete` themselves.
- **Negative / accepted**: danger-name flagging is intentionally conservative —
  a legitimately-named `archive/*` ref that an operator wants to publish will be
  flagged and must be renamed or explicitly excluded via `--danger-pattern`.
  Erring toward false positives on the publish surface is the correct bias for a
  confidentiality tool.

## References

- `docs/usage/rewrite-operating-patterns.md` — the operator-facing methodology
  (mode taxonomy + this contract in prose).
- internal-brief (`audit-publish`) — the detector this contract is built on.
- internal-brief (this change) — the methodology + contract + release-checklist
  integration.
- [ADR-0003](ADR-0003-redaction-safe-output.md) — the redaction invariant a
  rewrite extends across history; this contract makes that extension durable.
- [ADR-0005](ADR-0005-scan-attestation-gate.md) — the attestation gate this
  contract composes with for pre-public releases.
- `RELEASE_CHECKLIST.md` §Final Validation — the pre-public cross-reference.

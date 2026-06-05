# ADR-0005: Scan Attestation Gate

**Status**: Proposed
**Date**: 2026-06-04
**Deciders**: @3leapsdave, devlead, devrev

## Context

limensafe's pre-public development workflow relies on local scans with
operator-controlled catalogs. CI can run public-baseline checks, but it
must not receive private vocabulary catalogs. The project therefore needs
a reviewable artifact that proves a local scan ran and passed before a
branch is pushed or a release tag is prepared.

A committed attestation file is self-referential if it claims to bind to
the exact commit that contains it: changing the file changes the commit
SHA. The verifier must account for this without accepting stale proofs
for commits that changed source code or documentation after the scan.

## Decision

Add `.limensafe/scan-attestation.json` as the committed proof file. It
contains:

- `schema_version`
- `commit_sha` for the commit that was scanned
- `scan_timestamp`
- `tool_version`
- `catalog_id_hash`
- `visibility`
- `exit_code`
- `operator`
- small scan metadata counters

`limensafe attest` writes and stages the file after a successful
introduced-lines scan (`scan --diff --diff-base <ref>`). For this slice,
attestation requires explicit `--catalog` paths so `catalog_id_hash`
can hash actual catalog file content. `--config-file` attestation is
rejected until resolver-backed hashing of loaded private catalog sources
exists.

`limensafe verify-attestation` reads the attestation from the committed
blob at `HEAD:.limensafe/scan-attestation.json`, not from the working
tree. This prevents staged or uncommitted attestations from satisfying
the proof requirement.

Push and tag verification both accept either:

- `commit_sha == HEAD`, or
- `commit_sha == HEAD~1` only when `HEAD` changes only
  `.limensafe/scan-attestation.json`

The second case is the final attestation-commit model: commit the work,
run `limensafe attest`, then commit only the attestation file. Any commit
that mixes source/doc changes with a parent-bound attestation fails.

Push mode uses a 24-hour freshness window by default. Tag mode uses a
60-minute window and requires the attestation catalog hash to appear in
`.limensafe/known-catalog-hashes.txt`, unless the operator explicitly
sets `LIMENSAFE_RELEASE_CATALOG_OK=1`.

## Consequences

Reviewers can verify that the pushed tree carries a local scan proof, and
release preparation can fail closed when the proof is stale, missing, or
bound to the wrong commit.

The attestation commit model adds one small final commit to PR branches.
That is deliberate: it avoids the impossible exact self-reference while
preserving a strict proof that no non-attestation files changed after the
scan.

Catalog-hash support is conservative in this slice. Direct catalog paths
work now; config-file driven private catalog hashing waits for profile or
resolver support that can hash loaded catalog source content without
emitting private source details.

## Alternatives Considered

Exact `commit_sha == HEAD` for all modes was rejected because a committed
file cannot contain the SHA of the commit that includes that file.

Reading `.limensafe/scan-attestation.json` from the working tree was
rejected because an uncommitted or staged-only file would let a pre-push
hook pass while the pushed commit lacks the proof.

Cryptographic signing of attestations was deferred. It would strengthen
authenticity, but adds key-management surface before the profile/control
plane work exists.

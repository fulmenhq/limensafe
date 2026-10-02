# Public release verification

Release tags are checked against public provenance in their own tagged
commit, not a current checkout's replacement key or an ambient keyring.

The public GPG export is `docs/security/release-signing-keys.asc`. The
primary fingerprint anchors identity; `keys/authorized-signer.txt` names
the only permitted signing subkey. One sign-only subkey is required. An
optional encryption subkey is not a signer. Fingerprints are derived from
the export. `keys/expected-fingerprints.txt` and `.ndjson` contain the GPG
primary and SHA256 of the decoded 42-byte minisign public blob, excluding
the untrusted comment. Maintenance and new signing require a currently
unexpired, unrevoked primary and signer; maintenance rejects revoked
encryption packets too. Historical verification requires signature time
before primary/signer expiry and before any superseded or retired
revocation. Compromise or missing/unknown revocation reasons fail even
when dated after the signature. Later wall-clock expiry does not fail
historical verification. Encryption-subkey expiry or revocation does not
invalidate the historical tag.

The minisign `key_id` is advisory metadata when no public file is supplied;
it cannot be derived from the blob hash. When `LIMENSAFE_MINISIGN_PUB` is
supplied, both tag verification and `release-verify-minisign-pin` derive
the hash and key ID from the same public blob and require both to match.
Manifest verification requires this public-file check. GPG-only tag
verification does not authenticate minisign metadata by itself.

Maintainer maintenance uses these environment variables:

- `LIMENSAFE_GPG_PRIMARY_FINGERPRINT`: approved full primary fingerprint
- `LIMENSAFE_PGP_KEY_ID`: approved full signing-subkey fingerprint plus `!`
- `LIMENSAFE_GPG_HOMEDIR`: absolute isolated keyring, not the default home
- `LIMENSAFE_MINISIGN_PUB`: approved public minisign file

After separate authorization, `make release-export-pin` exports the
**whole primary**, without `!`, and validates its public packets and
live keys. `make release-validate-pin` is read-only. Then
`make release-insert-anchors` derives both anchor files and the authorized
signer. These commands refuse symlinks, private material and overwrites.
Rotation replaces the export and all anchors together in a reviewed
change, never by editing fingerprints by hand.

Public verification needs only Git, GPG, Python 3 and the tagged history:

```sh
git checkout --detach vX.Y.Z
LIMENSAFE_RELEASE_TAG=vX.Y.Z make release-verify-tag
LIMENSAFE_RELEASE_TAG=vX.Y.Z make release-verify-remote-tag
```

The tag must directly target HEAD, match VERSION, have the exact
`Release vX.Y.Z` annotation and be signed by the committed authorized
subkey. Primary signatures and additional signing keys are rejected.
The remote check additionally compares tag object and peeled commit,
and requires GitHub's verification result via the GitHub CLI.
Hosted GitHub verification remains an additional publication check.
The release workflow must pass committed-pin verification before it can
create its draft. Follow `RELEASE_CHECKLIST.md` for separately authorized
manual publication and manifest signature verification.

Historical v0.2.0 has no committed pin. Published public assets are
provenance candidates, not a retroactive pin; do not re-sign that release.

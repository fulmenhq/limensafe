#!/usr/bin/env python3
"""Public release-pin maintenance and isolated, committed-pin tag verification."""

import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

PIN = "docs/security/release-signing-keys.asc"
TEXT = "keys/expected-fingerprints.txt"
JSON = "keys/expected-fingerprints.ndjson"
SIGNER = "keys/authorized-signer.txt"
FPR = r"[0-9A-F]{40}"
TAG = r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(rc|beta|alpha)\.(0|[1-9][0-9]*))?"


def require(ok, message):
    if not ok:
        raise ValueError(message)


def run(args, data=None, env=None):
    result = subprocess.run(args, input=data, capture_output=True, env=env)
    require(result.returncode == 0, f"{args[0]} operation failed")
    return result.stdout


def git(*args):
    return run(["git", *args])


def variable(name):
    value = os.environ.get(name, "")
    require(bool(value), f"{name} is required")
    return value


def regular(path):
    path = Path(path)
    require(
        path.is_file() and not path.is_symlink(),
        "non-symlink regular public file required",
    )
    require(not any(p.is_symlink() for p in path.parents), "symlink parent forbidden")
    data = path.read_bytes()
    require(bool(data), "empty public file")
    return data


def gpg(home, *args, data=None):
    return run(
        ["gpg", "--no-options", "--homedir", str(home), "--batch", "--no-tty", *args],
        data,
    )


def inspect(
    home, data, primary=None, signer=None, signature_time=None, structural=False
):
    require(
        b"-----BEGIN PGP PUBLIC KEY BLOCK-----" in data, "armored public key required"
    )
    require(
        b"PRIVATE KEY" not in data and b"SECRET KEY" not in data,
        "private material forbidden",
    )
    packets = gpg(home, "--list-packets", data=data)
    require(
        b":secret key packet:" not in packets
        and b":secret sub key packet:" not in packets,
        "private packets forbidden",
    )
    gpg(home, "--import", data=data)
    require(
        not gpg(home, "--with-colons", "--list-secret-keys").strip(),
        "secret key forbidden",
    )
    listing = gpg(home, "--with-colons", "--fixed-list-mode", "--list-keys").decode()
    records = []
    pending = None
    for line in listing.splitlines():
        fields = line.split(":")
        if fields[0] in ("pub", "sub"):
            pending = fields
        elif fields[0] == "fpr" and pending:
            require(re.fullmatch(FPR, fields[9]), "invalid fingerprint")
            require(pending[1] not in ("i", "d", "n"), "invalid key")
            require("D" not in pending[11], "disabled key")
            records.append(
                (pending[0], fields[9], pending[11], pending[5], pending[6], pending[1])
            )
            pending = None
    primaries = [r for r in records if r[0] == "pub"]
    signing = [r for r in records if r[0] == "sub" and "s" in r[2]]
    require(len(primaries) == 1, "exactly one primary required")
    require(
        len(signing) == 1 and signing[0][2].lower() == "s",
        "exactly one sign-only subkey required",
    )
    others = [r for r in records if r[0] == "sub" and r not in signing]
    require(
        len(others) <= 1 and all(r[2].lower() == "e" for r in others),
        "only an optional encryption subkey is permitted",
    )
    derived_primary, derived_signer = primaries[0][1], signing[0][1]
    require(primary is None or derived_primary == primary, "primary anchor mismatch")
    require(signer is None or derived_signer == signer, "authorized signer mismatch")
    if not structural:
        moment = time.time() if signature_time is None else signature_time
        require(moment <= time.time(), "future signature time")
        for record in primaries + signing:
            require(int(record[3]) <= moment, "signature predates key")
            require(
                not record[4] or moment < int(record[4]),
                "key expired at authorization or signature time",
            )
        if signature_time is None:
            require(all(r[5] != "r" for r in records), "revoked key in public export")
        else:
            # GPG's machine interface associates each checked revocation and
            # hashed reason subpacket with its current primary/subkey record.
            signatures = gpg(
                home,
                "--with-colons",
                "--list-options",
                "show-sig-subpackets=29",
                "--check-sigs",
            ).decode()
            subject = None
            revocations = []
            rev = None
            for line in signatures.splitlines():
                f = line.split(":")
                if f[0] in ("pub", "sub", "uid", "sig", "rev"):
                    if rev is not None:
                        revocations.append(rev)
                        rev = None
                if f[0] in ("pub", "sub"):
                    subject = {"key": None, "kind": f[0], "uid": False}
                elif f[0] == "uid":
                    if subject is not None:
                        subject["uid"] = True
                elif f[0] == "fpr":
                    require(
                        subject is not None and subject["key"] is None,
                        "ambiguous revocation key block",
                    )
                    subject["key"] = f[9]
                elif f[0] == "rev":
                    rev_class = f[10].split(",", 1)[0][:2].lower()
                    if rev_class == "30":
                        require(
                            subject is not None and subject["uid"],
                            "certification revocation outside UID block",
                        )
                        continue
                    require(f[1] == "!", "unverified key revocation")
                    require(subject is not None, "revocation target block missing")
                    require(
                        not subject["uid"]
                        and (
                            (rev_class == "20" and subject["kind"] == "pub")
                            or (rev_class == "28" and subject["kind"] == "sub")
                        ),
                        "key revocation class/target mismatch",
                    )
                    # Store the pub/sub block itself: primary revocations may
                    # precede its fpr row. The rev row's fingerprint is the
                    # issuer, not necessarily the revoked subkey's identity.
                    rev = {"subject": subject, "time": int(f[5]), "reason": None}
                elif f[0] == "spk" and rev is not None and f[1] == "29":
                    require(
                        f[2] == "1" and rev["reason"] is None,
                        "unhashed or duplicate revocation reason",
                    )
                    require(
                        re.match(r"^%[0-9A-Fa-f]{2}", f[4]),
                        "malformed revocation reason",
                    )
                    rev["reason"] = int(f[4][1:3], 16)
            if rev is not None:
                revocations.append(rev)
            for r in revocations:
                r["key"] = r["subject"]["key"]
                require(
                    r["key"] in [record[1] for record in records],
                    "revocation target fingerprint missing",
                )
            relevant = [
                r for r in revocations if r["key"] in (derived_primary, derived_signer)
            ]
            for r in relevant:
                require(
                    r["reason"] in (1, 3), "compromise or unknown revocation reason"
                )
                require(moment < r["time"], "key revoked at signature time")
            for record in primaries + signing:
                # GPG propagates a primary's revoked validity to its subkeys.
                # That inherited status has no separate subkey revocation.
                inherited = (
                    record[0] == "sub"
                    and primaries[0][5] == "r"
                    and any(r["key"] == derived_primary for r in relevant)
                )
                require(
                    record[5] != "r"
                    or inherited
                    or any(r["key"] == record[1] for r in relevant),
                    "revocation metadata missing",
                )
    return derived_primary, derived_signer


def strict_json(data):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "duplicate machine-anchor field")
            result[key] = value
        return result

    return json.loads(data, object_pairs_hook=unique)


def minisign(data):
    lines = data.decode("ascii").splitlines()
    require(
        len(lines) == 2 and lines[0].startswith("untrusted comment:"),
        "minisign public file required",
    )
    blob = base64.b64decode(lines[1], validate=True)
    require(len(blob) == 42 and blob[:2] == b"Ed", "invalid minisign public blob")
    return hashlib.sha256(blob).hexdigest(), blob[2:10].hex().upper()


def check_minisign_material(blob_hash, key_id):
    derived_hash, derived_id = minisign(regular(variable("LIMENSAFE_MINISIGN_PUB")))
    require(derived_hash == blob_hash, "minisign public blob anchor mismatch")
    require(derived_id == key_id, "minisign public key id mismatch")


def approved():
    primary = variable("LIMENSAFE_GPG_PRIMARY_FINGERPRINT")
    selector = variable("LIMENSAFE_PGP_KEY_ID")
    require(
        re.fullmatch(FPR, primary) and re.fullmatch(FPR + "!", selector),
        "full primary fingerprint and forced signing-subkey selector required",
    )
    return primary, selector[:-1]


def anchors(primary, blob_hash, key_id):
    text = f"gpg {primary}\nminisign {blob_hash}\n".encode()
    common = {"schema_version": "v0", "class": "public", "confidence": "high"}
    rows = [
        dict(
            common,
            kind="gpg",
            algorithm="openpgp-fingerprint",
            fingerprint=primary,
            fingerprint_scheme="openpgp-fingerprint-v1",
            key_id=primary[-16:],
            key_role="primary",
        ),
        dict(
            common,
            kind="minisign",
            algorithm="sha256",
            fingerprint=blob_hash,
            fingerprint_scheme="minisign-public-blob-sha256-v1",
            key_id=key_id,
        ),
    ]
    ndjson = "".join(json.dumps(r, separators=(",", ":")) + "\n" for r in rows).encode()
    return text, ndjson


def install(path, data):
    target = Path(path)
    require(
        not target.exists() and not target.is_symlink(),
        "refusing to overwrite pin or anchors",
    )
    require(not any(p.is_symlink() for p in target.parents), "symlink parent forbidden")
    target.parent.mkdir(parents=True, exist_ok=True)
    with target.open("xb") as stream:
        stream.write(data)


def maintain(action):
    primary, signer = approved()
    public = regular(variable("LIMENSAFE_MINISIGN_PUB"))
    blob_hash, key_id = minisign(public)
    if action == "export":
        require(os.environ.get("CI") != "true", "export disabled in CI")
        home = Path(variable("LIMENSAFE_GPG_HOMEDIR"))
        require(
            home.is_absolute() and home.is_dir() and not home.is_symlink(),
            "absolute isolated GPG home required",
        )
        default = Path.home() / ".gnupg"
        require(home.resolve() != default.resolve(), "default GPG home forbidden")
        # Export the whole primary, never the forced selector which drops subkeys.
        data = gpg(home, "--armor", "--export", primary)
    else:
        data = regular(PIN)
    with tempfile.TemporaryDirectory(prefix="limensafe-pin-") as home:
        inspect(home, data, primary, signer)
    if action == "export":
        install(PIN, data)
    if action == "insert":
        text, ndjson = anchors(primary, blob_hash, key_id)
        # Re-derive before writing any anchor, and preflight every destination.
        with tempfile.TemporaryDirectory(prefix="limensafe-pin-") as home:
            require(
                inspect(home, regular(PIN)) == (primary, signer),
                "pin changed during derivation",
            )
        require(
            minisign(regular(variable("LIMENSAFE_MINISIGN_PUB")))
            == (blob_hash, key_id),
            "minisign changed",
        )
        for path in (TEXT, JSON, SIGNER):
            require(
                not Path(path).exists() and not Path(path).is_symlink(),
                "anchors already exist",
            )
        install(TEXT, text)
        install(JSON, ndjson)
        install(SIGNER, (signer + "\n").encode())
    print("validated public primary and authorized signing subkey")


def committed(commit, path):
    entry = git("ls-tree", commit, "--", path).decode()
    require(
        entry.startswith("100644 blob ") or entry.startswith("100755 blob "),
        "committed regular pin file missing",
    )
    return git("show", f"{commit}:{path}")


def github_repository(url):
    https = re.fullmatch(
        r"https://github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?", url
    )
    if https:
        return https[1]
    ssh = re.fullmatch(
        r"git@([A-Za-z0-9][A-Za-z0-9_.-]*):([A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*?)(?:\.git)?",
        url,
    )
    require(ssh is not None, "unsupported GitHub origin")
    config = run(["ssh", "-G", ssh[1]]).decode().splitlines()
    hosts = [line.split()[1] for line in config if line.startswith("hostname ")]
    require(hosts == ["github.com"], "SSH origin does not resolve to GitHub")
    return ssh[2]


def verify(remote=False):
    tag = variable("LIMENSAFE_RELEASE_TAG")
    require(re.fullmatch(TAG, tag), "canonical release tag required")
    ref = "refs/tags/" + tag
    obj = git("rev-parse", "--verify", ref).decode().strip()
    require(git("cat-file", "-t", obj).strip() == b"tag", "annotated tag required")
    commit = git("rev-parse", ref + "^{commit}").decode().strip()
    require(
        commit == git("rev-parse", "HEAD").decode().strip(), "tag target must be HEAD"
    )
    require(
        committed(commit, "VERSION") == (tag[1:] + "\n").encode(),
        "tag version mismatch",
    )
    raw = git("cat-file", "tag", obj)
    header, body = raw.split(b"\n\n", 1)
    header_lines = header.splitlines()
    require(
        len(header_lines) == 4
        and header_lines[0] == b"object " + commit.encode()
        and header_lines[1] == b"type commit"
        and header_lines[2] == b"tag " + tag.encode()
        and re.fullmatch(
            rb"tagger .+ <[^<>\r\n]+> [0-9]+ [-+][0-9]{4}", header_lines[3]
        ),
        "tag must directly name its release commit",
    )
    signature = git("for-each-ref", "--format=%(contents:signature)", ref).rstrip(b"\n")
    require(
        signature and body.endswith(signature + b"\n"),
        "parsed signature suffix required",
    )
    require(
        body[: -len(signature + b"\n")] == f"Release {tag}\n".encode(),
        "unexpected tag annotation",
    )
    text = committed(commit, TEXT).decode()
    require(
        re.fullmatch(r"gpg " + FPR + r"\nminisign [0-9a-f]{64}\n", text),
        "malformed anchors",
    )
    primary = text.splitlines()[0][4:]
    signer_bytes = committed(commit, SIGNER)
    require(
        re.fullmatch((FPR + "\n").encode(), signer_bytes), "malformed authorized signer"
    )
    signer = signer_bytes.decode().strip()
    require(signer != primary, "primary cannot be authorized tag signer")
    rows = [strict_json(line) for line in committed(commit, JSON).splitlines()]
    require(
        all(isinstance(row, dict) for row in rows), "machine anchor object required"
    )
    require(
        len(rows) == 2
        and rows[0].get("kind") == "gpg"
        and rows[0].get("fingerprint") == primary
        and rows[1].get("kind") == "minisign"
        and rows[1].get("fingerprint") == text.splitlines()[1][9:],
        "machine anchors mismatch",
    )
    mini_id = rows[1].get("key_id", "")
    require(re.fullmatch(r"[0-9A-F]{16}", mini_id), "invalid minisign key id")
    _, expected_json = anchors(primary, text.splitlines()[1][9:], mini_id)
    require(
        rows == [json.loads(line) for line in expected_json.splitlines()],
        "machine anchor fields mismatch",
    )
    # A key ID is advisory without the public blob; SHA256 cannot reveal it.
    if os.environ.get("LIMENSAFE_MINISIGN_PUB"):
        check_minisign_material(text.splitlines()[1][9:], mini_id)
    with tempfile.TemporaryDirectory(prefix="limensafe-pin-") as home:
        public_pin = committed(commit, PIN)
        inspect(home, public_pin, primary, signer, structural=True)
        env = dict(os.environ, GNUPGHOME=home)
        # Ignore ambient GPG settings and prohibit automatic key retrieval.
        wrapper = Path(home) / "verify-gpg"
        wrapper.write_text(
            '#!/bin/sh\nexec gpg --no-options --batch --no-tty --no-auto-key-retrieve "$@"\n'
        )
        wrapper.chmod(0o700)
        result = subprocess.run(
            [
                "git",
                "-c",
                "gpg.format=openpgp",
                "-c",
                f"gpg.program={wrapper}",
                "verify-tag",
                "--raw",
                obj,
            ],
            capture_output=True,
            env=env,
        )
        statuses = [
            line.split()
            for line in result.stderr.decode().splitlines()
            if line.startswith("[GNUPG:] ")
        ]
        valid = [s for s in statuses if s[1] == "VALIDSIG"]
        good = [s for s in statuses if s[1] in ("GOODSIG", "EXPKEYSIG", "REVKEYSIG")]
        require(
            len(valid) == len(good) == 1,
            "exactly one valid and good signature required",
        )
        require(
            not any(
                s[1]
                in (
                    "BADSIG",
                    "ERRSIG",
                    "EXPSIG",
                )
                for s in statuses
            ),
            "bad signature status",
        )
        require(
            valid[0][2] == signer and valid[0][-1] == primary, "unauthorized signature"
        )
        require(result.returncode in (0, 1), "tag signature verification failed")
        inspect(home, public_pin, primary, signer, signature_time=int(valid[0][4]))
    if remote:
        refs = (
            git("ls-remote", "--tags", "origin", ref, ref + "^{}").decode().splitlines()
        )
        require(
            sorted(refs) == sorted([f"{obj}\t{ref}", f"{commit}\t{ref}^{{}}"]),
            "remote tag object mismatch",
        )
        url = git("remote", "get-url", "origin").decode().strip()
        repository = github_repository(url)
        hosted = run(["gh", "api", f"repos/{repository}/git/tags/{obj}"])
        require(
            json.loads(hosted).get("verification", {}).get("verified") is True,
            "hosted tag verification failed",
        )
    print(f"verified committed public pin: {tag} -> {commit}")


def verify_export(path):
    primary, signer = approved()
    commit = git("rev-parse", "HEAD").decode().strip()
    data = regular(path)
    require(data == committed(commit, PIN), "public export differs from committed pin")
    require(
        committed(commit, SIGNER) == (signer + "\n").encode(),
        "committed signer mismatch",
    )
    with tempfile.TemporaryDirectory(prefix="limensafe-export-") as home:
        inspect(home, data, primary, signer)
    print("verified whole public export against committed pin")


def sign_manifests(directory):
    require(os.environ.get("CI") != "true", "signing disabled in CI")
    primary, signer = approved()
    tag = variable("LIMENSAFE_RELEASE_TAG")
    require(re.fullmatch(TAG, tag), "canonical release tag required")
    require(regular("VERSION") == (tag[1:] + "\n").encode(), "tag version mismatch")
    root = Path(directory)
    require(
        root.is_dir() and not root.is_symlink(), "regular staging directory required"
    )
    snapshots = {name: regular(root / name) for name in ("SHA256SUMS", "SHA512SUMS")}
    outputs = ["SHA256SUMS.asc", "SHA512SUMS.asc", "fulmenhq-release-signing-key.asc"]
    for name in outputs:
        require(
            not (root / name).exists() and not (root / name).is_symlink(),
            "refusing to overwrite PGP provenance",
        )
    home = Path(variable("LIMENSAFE_GPG_HOMEDIR"))
    require(
        home.is_absolute() and home.is_dir() and not home.is_symlink(),
        "absolute isolated GPG home required",
    )
    require(
        home.resolve() != (Path.home() / ".gnupg").resolve(),
        "default GPG home forbidden",
    )
    with tempfile.TemporaryDirectory(prefix="limensafe-pgp-") as temporary:
        temporary = Path(temporary).resolve()
        public = gpg(home, "--armor", "--export", primary)
        export = temporary / outputs[2]
        export.write_bytes(public)
        verify_export(export)
        with tempfile.TemporaryDirectory(prefix="limensafe-pgp-check-") as verification:
            inspect(verification, public, primary, signer)
            for name, content in snapshots.items():
                manifest = temporary / name
                manifest.write_bytes(content)
                signature = temporary / (name + ".asc")
                gpg(
                    home,
                    "--armor",
                    "--local-user",
                    signer + "!",
                    "--detach-sign",
                    "--output",
                    str(signature),
                    str(manifest),
                )
                status = gpg(
                    verification,
                    "--status-fd",
                    "1",
                    "--verify",
                    str(signature),
                    str(manifest),
                ).decode()
                valid = [
                    line.split()
                    for line in status.splitlines()
                    if line.startswith("[GNUPG:] VALIDSIG ")
                ]
                require(
                    len(valid) == 1
                    and valid[0][2] == signer
                    and valid[0][-1] == primary,
                    "unauthorized manifest signature",
                )
        for name, content in snapshots.items():
            require(
                regular(root / name) == content,
                "checksum manifest changed during signing",
            )
        installed = []
        try:
            for name in outputs:
                target = root / name
                with target.open("xb") as stream:
                    installed.append(target)
                    stream.write((temporary / name).read_bytes())
        except OSError:
            for target in installed:
                target.unlink()
            raise
    print("created both PGP manifest signatures and verified whole public export")


def verify_minisign():
    commit = git("rev-parse", "HEAD").decode().strip()
    text = committed(commit, TEXT).decode()
    require(
        re.fullmatch(r"gpg " + FPR + r"\nminisign [0-9a-f]{64}\n", text),
        "malformed anchors",
    )
    rows = [strict_json(line) for line in committed(commit, JSON).splitlines()]
    require(
        len(rows) == 2 and all(isinstance(row, dict) for row in rows),
        "machine anchor objects required",
    )
    mini_id = rows[1].get("key_id", "")
    require(re.fullmatch(r"[0-9A-F]{16}", mini_id), "invalid minisign key id")
    _, expected = anchors(text.splitlines()[0][4:], text.splitlines()[1][9:], mini_id)
    require(
        rows == [strict_json(line) for line in expected.splitlines()],
        "machine anchor fields mismatch",
    )
    check_minisign_material(text.splitlines()[1][9:], mini_id)
    print("verified minisign public blob against committed anchor")


if __name__ == "__main__":
    try:
        action = sys.argv[1]
        if action in ("export", "validate", "insert"):
            maintain(action)
        elif action in ("verify", "verify-remote"):
            verify(action == "verify-remote")
        elif action == "verify-minisign":
            verify_minisign()
        elif action == "verify-export":
            verify_export(sys.argv[2])
        elif action == "sign-manifests":
            sign_manifests(sys.argv[2])
        else:
            raise ValueError("unknown release-pin action")
    except (ValueError, OSError, IndexError, KeyError, UnicodeError) as error:
        print(f"error: {error}", file=sys.stderr)
        sys.exit(1)

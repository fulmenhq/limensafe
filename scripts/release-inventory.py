#!/usr/bin/env python3
"""Fail-closed release asset inventory; never uploads or signs anything."""

import hashlib
from pathlib import Path
import re
import sys


def require(condition, message):
    if not condition:
        raise ValueError(message)


def inventory(directory, app, tag, mode):
    require(re.fullmatch(r"[a-z][a-z0-9-]*", app), "application name required")
    require(
        re.fullmatch(
            r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:alpha|beta|rc)\.(0|[1-9][0-9]*))?",
            tag,
        ),
        "release tag required",
    )
    require(mode in ("build", "all", "provenance"), "unknown inventory mode")
    root = Path(directory)
    require(
        root.is_dir() and not root.is_symlink(), "regular staging directory required"
    )
    binaries = [
        f"{app}-{platform}-{arch}{'.exe' if platform == 'windows' else ''}"
        for platform in ("linux", "darwin", "windows")
        for arch in ("amd64", "arm64")
    ]
    manifests = ["SHA256SUMS", "SHA512SUMS"]
    provenance = (
        manifests
        + [f"{name}.minisig" for name in manifests]
        + [f"{app}-minisign.pub", f"release-notes-{tag}.md"]
    )
    pgp = [f"{name}.asc" for name in manifests] + ["fulmenhq-release-signing-key.asc"]
    present = {entry.name for entry in root.iterdir()}
    if mode == "build":
        expected = binaries + manifests
    else:
        if present.intersection(pgp):
            provenance += pgp
        expected = binaries + provenance
    require(present == set(expected), "missing or unexpected release assets")
    for name in expected:
        path = root / name
        require(
            path.is_file() and not path.is_symlink() and path.stat().st_size > 0,
            "nonempty regular release assets required",
        )
    for manifest, algorithm in zip(manifests, ("sha256", "sha512")):
        rows = (root / manifest).read_text(encoding="ascii").splitlines()
        require(
            len(rows) == len(binaries), "checksum manifest must contain six binaries"
        )
        seen = set()
        for row in rows:
            match = re.fullmatch(r"([0-9a-f]+) [ *]([^/\\]+)", row)
            require(match is not None, "malformed checksum manifest")
            digest, name = match.groups()
            require(
                name in binaries and name not in seen,
                "unexpected or duplicate manifest entry",
            )
            seen.add(name)
            actual = hashlib.new(algorithm, (root / name).read_bytes()).hexdigest()
            require(digest == actual, "release checksum mismatch")
    return [
        str(root / name) for name in (provenance if mode == "provenance" else expected)
    ]


if __name__ == "__main__":
    try:
        require(
            len(sys.argv) == 5,
            "usage: release-inventory.py <dir> <app> <tag> <build|all|provenance>",
        )
        print("\n".join(inventory(*sys.argv[1:])))
    except (ValueError, OSError, UnicodeError) as error:
        print(f"error: {error}", file=sys.stderr)
        sys.exit(1)

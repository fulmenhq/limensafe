#!/usr/bin/env bash

set -euo pipefail

DIR=${1:-dist/release}
BINARY_NAME=${2:-}

if [ -z "${BINARY_NAME}" ]; then
    echo "usage: $0 [dir] <binary_name>" >&2
    exit 1
fi

if [ ! -d "${DIR}" ]; then
    echo "error: directory ${DIR} not found" >&2
    exit 1
fi

cd "${DIR}"

rm -f SHA256SUMS.txt SHA256SUMS.txt.*

artifacts=()
for platform in linux darwin windows; do
    for arch in amd64 arm64; do
        suffix=""
        if [ "$platform" = windows ]; then suffix=".exe"; fi
        name="${BINARY_NAME}-${platform}-${arch}${suffix}"
        if [ ! -s "$name" ] || [ -L "$name" ] || [ ! -f "$name" ]; then
            echo "error: missing or invalid release binary" >&2
            exit 1
        fi
        artifacts+=("$name")
    done
done
if [ "${#artifacts[@]}" -eq 6 ]; then
    if command -v sha256sum > /dev/null 2>&1; then
        sha256sum "${artifacts[@]}" > SHA256SUMS
    else
        shasum -a 256 "${artifacts[@]}" > SHA256SUMS
    fi

    if command -v sha512sum > /dev/null 2>&1; then
        sha512sum "${artifacts[@]}" > SHA512SUMS
    else
        shasum -a 512 "${artifacts[@]}" > SHA512SUMS
    fi
else
    echo "error: no artifacts found matching ${BINARY_NAME}-* in ${DIR}" >&2
    exit 1
fi

echo "✅ Wrote ${DIR}/SHA256SUMS and ${DIR}/SHA512SUMS"

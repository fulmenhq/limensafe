#!/usr/bin/env bash

set -euo pipefail

TAG="${1:-}"
SOURCE_DIR="${2:-dist/release}"
APP="${SIGNING_APP_NAME:?application identity required}"

if [[ -z "${TAG}" ]]; then
    echo "usage: $0 vX.Y.Z [source_dir]" >&2
    exit 1
fi

if ! command -v gh > /dev/null 2>&1; then
    echo "❌ gh (GitHub CLI) not found in PATH" >&2
    echo "Install: https://cli.github.com/" >&2
    exit 1
fi

if [[ ! -d "${SOURCE_DIR}" ]]; then
    echo "❌ Source dir not found: ${SOURCE_DIR}" >&2
    exit 1
fi

# Upload only provenance outputs (never binaries) to avoid clobbering CI-built assets.
# Expected inputs:
# - SHA256SUMS, SHA512SUMS
# - SHA256SUMS.minisig/.asc, SHA512SUMS.minisig/.asc
# - *.pub and *release-signing-key.asc
# - release-notes-*.md
inventory="$(python3 "$(dirname "$0")/release-inventory.py" "$SOURCE_DIR" "$APP" "$TAG" provenance)"
final_assets=()
while IFS= read -r f; do final_assets+=("$f"); done <<< "$inventory"

if [[ ${#final_assets[@]} -eq 0 ]]; then
    echo "❌ No provenance assets found to upload from ${SOURCE_DIR}" >&2
    exit 1
fi

echo "→ Uploading ${#final_assets[@]} provenance asset(s) to ${TAG} (clobber)"
gh release upload "${TAG}" "${final_assets[@]}" --clobber

echo "✅ Upload complete"

#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR="$(mktemp -d)"

cleanup() {
    rm -rf "${TMPDIR}"
}
trap cleanup EXIT

FIXTURE="${TMPDIR}/limensafe-format-check-fixture"

mkdir -p \
    "${FIXTURE}/.fulmen" \
    "${FIXTURE}/.goneat" \
    "${FIXTURE}/docs" \
    "${FIXTURE}/internal/assets/appidentity" \
    "${FIXTURE}/scripts"

cp "${ROOT}/Makefile" "${FIXTURE}/Makefile"
cp "${ROOT}/VERSION" "${FIXTURE}/VERSION"
cp "${ROOT}/.goneat/assess.yaml" "${FIXTURE}/.goneat/assess.yaml"
cp "${ROOT}/.fulmen/app.yaml" "${FIXTURE}/.fulmen/app.yaml"
cp "${ROOT}/.fulmen/app.yaml" "${FIXTURE}/internal/assets/appidentity/app.yaml"
cp "${ROOT}/scripts/sync-embedded-identity.sh" "${FIXTURE}/scripts/sync-embedded-identity.sh"
chmod +x "${FIXTURE}/scripts/sync-embedded-identity.sh"

cat > "${FIXTURE}/docs/unformatted.md" << 'MARKDOWN'
|left|right|
|-|-|
|one|two|
MARKDOWN

if make -s -C "${FIXTURE}" format-check > /dev/null 2>&1; then
    echo "format-check unexpectedly passed on unformatted markdown" >&2
    exit 1
fi

make -s -C "${FIXTURE}" fmt > /dev/null
make -s -C "${FIXTURE}" format-check > /dev/null

echo "✅ format-check negative fixture passed"

#!/usr/bin/env bash

set -euo pipefail

# bootstrap-smoke.sh — end-to-end smoke for fresh CI runners and post-build
# validation. Implements the 5-check spec from partner-integration devlead
# (live-validation 2026-05-08): version, health, scan tracked archive with
# builtin config, --staged scan in temp git fixture, branch+commit-msg
# stdin sentinel checks.
#
# All checks pass on clean inputs (exit 0). The smoke catches "happy-path
# break" — if any of the 5 fail, the binary is not safe to ship. Existing
# unit + integration tests cover negative-detect cases exhaustively; this
# target is end-to-end positive validation.
#
# Fixtures are built fresh per run inside a temp directory so the smoke
# does not depend on tracked testdata corpora staying "clean" — the
# repo's testdata/ trees are deliberately seeded with leak markers for
# negative-detect tests.
#
# Usage: bootstrap-smoke.sh [path-to-binary]
#
# When no argument is given, defaults to bin/limensafe relative to the
# repo root (assumes CWD is the repo root, as `make bootstrap-smoke` invokes).

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${1:-${REPO_ROOT}/bin/limensafe}"

if [ ! -x "$BIN" ]; then
    echo "error: binary not found or not executable: $BIN" >&2
    echo "  hint: run 'make build' first" >&2
    exit 1
fi

if ! command -v git > /dev/null 2>&1; then
    echo "error: git not found in PATH (required for --staged check)" >&2
    exit 1
fi

BUILTIN_CATALOG="${REPO_ROOT}/pkg/catalog/builtin/public-baseline.yaml"
if [ ! -f "$BUILTIN_CATALOG" ]; then
    echo "error: builtin baseline catalog missing: $BUILTIN_CATALOG" >&2
    exit 1
fi

# Build a clean temp fixture (no leak markers) for checks 3 and 4. Used as
# both a tracked-archive scan target and a git working tree for --staged.
SMOKE_TMP="$(mktemp -d)"
trap 'rm -rf "$SMOKE_TMP"' EXIT

# .limensafe/config.yaml using the builtin baseline catalog
mkdir -p "${SMOKE_TMP}/.limensafe"
cat > "${SMOKE_TMP}/.limensafe/config.yaml" << 'EOF'
$schema: "https://schemas.fulmenhq.dev/limensafe/v1/config.schema.json"
schema_version: "1.0.0"
repo:
  id: bootstrap-smoke-fixture
  visibility: public_oss
catalogs:
  - catalog_id: limensafe-public-baseline-v0
    source:
      kind: builtin
      name: public-baseline
    optional: false
policy:
  default_severity: medium
  block_threshold: high
  redaction_safe_output: true
  co_occurrence_enabled: true
EOF

# Clean Go source — no sentinel markers, no protected vocabulary
mkdir -p "${SMOKE_TMP}/cmd"
cat > "${SMOKE_TMP}/cmd/main.go" << 'EOF'
package main

import "fmt"

func main() {
    fmt.Println("hello from bootstrap smoke fixture")
}
EOF

cat > "${SMOKE_TMP}/README.md" << 'EOF'
# Bootstrap Smoke Fixture

Synthetic clean fixture generated per-run by scripts/bootstrap-smoke.sh.
EOF

echo "→ Bootstrap smoke: 5 checks per partner-integration devlead spec"
echo "  binary:  $BIN"
echo "  fixture: $SMOKE_TMP"

# Check 1: version subcommand prints to stdout, exits 0
echo "  [1/5] limensafe version"
"$BIN" version > /dev/null

# Check 2: health subcommand exits 0
echo "  [2/5] limensafe health"
"$BIN" health > /dev/null

(
    cd "$SMOKE_TMP"
    git init --quiet
    git config user.email "smoke@example.invalid"
    git config user.name "Bootstrap Smoke"
    git add .
    git commit --quiet -m "bootstrap smoke fixture"
)

# Check 3: scan a clean tracked archive with builtin-baseline config (expect clean)
echo "  [3/5] scan clean tracked archive (builtin config, expect clean)"
(
    cd "$SMOKE_TMP"
    "$BIN" scan --git-archive HEAD \
        --config-file "${SMOKE_TMP}/.limensafe/config.yaml" \
        --visibility public_oss > /dev/null
)

# Check 4: --staged scan in clean temp git fixture (expect clean)
echo "  [4/5] --staged scan in clean temp git fixture (expect clean)"
(
    cd "$SMOKE_TMP"
    echo "clean staged content" > staged-clean.txt
    git add staged-clean.txt
    "$BIN" scan . --staged \
        --config-file "${SMOKE_TMP}/.limensafe/config.yaml" \
        --visibility public_oss > /dev/null
)

# Check 5: branch-name + commit-msg stdin surfaces accept clean inputs
echo "  [5/5] branch-name + commit-msg stdin surfaces (clean inputs)"
echo "feature/clean-branch-name" | "$BIN" scan - --branch-name \
    --catalog "$BUILTIN_CATALOG" \
    --visibility public_oss > /dev/null

echo "feat: clean commit message" | "$BIN" scan - --commit-msg \
    --catalog "$BUILTIN_CATALOG" \
    --visibility public_oss > /dev/null

echo "✅ Bootstrap smoke passed (5/5)"

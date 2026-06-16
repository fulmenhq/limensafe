#!/usr/bin/env bash

set -euo pipefail

# Fail the build if any embedded schema mirror has drifted from its canonical
# source, so the runtime can never validate operator catalogs against a stale
# contract. Keep this list in lockstep with sync-embedded-schemas.sh.

SCHEMAS=(
    "schemas/limensafe/v1/catalog.schema.json:internal/assets/schemas/limensafe/v1/catalog.schema.json"
)

for pair in "${SCHEMAS[@]}"; do
    SRC="${pair%%:*}"
    DST="${pair##*:}"
    if [ ! -f "${SRC}" ]; then
        echo "❌ Missing canonical schema: ${SRC}" >&2
        exit 1
    fi
    if [ ! -f "${DST}" ]; then
        echo "❌ Missing embedded schema mirror: ${DST}" >&2
        echo "Run: make sync-embedded-schemas" >&2
        exit 1
    fi
    if ! cmp -s "${SRC}" "${DST}"; then
        echo "❌ Embedded schema mirror is out of sync" >&2
        echo "  ${SRC} != ${DST}" >&2
        echo "Run: make sync-embedded-schemas" >&2
        exit 1
    fi
done

echo "✅ Embedded schema mirrors are in sync"

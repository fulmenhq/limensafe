#!/usr/bin/env bash

set -euo pipefail

# Mirror canonical JSON Schemas into the Go-embeddable assets tree so the
# runtime validator works in a standalone binary. Keep this list in lockstep
# with the //go:embed directives in internal/assets/schemas/embedded.go.

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
    mkdir -p "$(dirname "${DST}")"
    cp "${SRC}" "${DST}"
    echo "✅ Synced ${SRC} → ${DST}"
done

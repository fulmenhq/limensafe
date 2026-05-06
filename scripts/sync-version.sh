#!/usr/bin/env bash
# Propagate the canonical VERSION file into .fulmen/app.yaml's version: field
# and the embedded copy at internal/assets/appidentity/app.yaml. Run from the
# repo root. Designed to be the single source of truth for version updates;
# `make version-bump` and `make version-set` invoke this.

set -euo pipefail

VERSION_FILE=${VERSION_FILE:-VERSION}
APP_YAML=${APP_YAML:-.fulmen/app.yaml}
EMBEDDED_YAML=${EMBEDDED_YAML:-internal/assets/appidentity/app.yaml}

if [ ! -f "$VERSION_FILE" ]; then
    echo "❌ Missing $VERSION_FILE" >&2
    exit 1
fi
if [ ! -f "$APP_YAML" ]; then
    echo "❌ Missing $APP_YAML" >&2
    exit 1
fi

V=$(tr -d '[:space:]' < "$VERSION_FILE")
if [ -z "$V" ]; then
    echo "❌ $VERSION_FILE is empty" >&2
    exit 1
fi

# Update version: line in $APP_YAML using awk (preserves indentation).
TMP=$(mktemp)
awk -v ver="$V" '
    /^[[:space:]]*version:[[:space:]]*/ {
        match($0, /^[[:space:]]*/)
        prefix = substr($0, 1, RLENGTH)
        print prefix "version: " ver
        next
    }
    { print }
' "$APP_YAML" > "$TMP"

# Sanity: TMP must not be empty
if [ ! -s "$TMP" ]; then
    rm -f "$TMP"
    echo "❌ awk produced empty output; refusing to overwrite $APP_YAML" >&2
    exit 1
fi

# Sanity: TMP must contain a non-empty version line
if ! grep -q "^[[:space:]]*version:[[:space:]]*$V[[:space:]]*$" "$TMP"; then
    rm -f "$TMP"
    echo "❌ awk did not produce expected version: $V line; refusing to overwrite $APP_YAML" >&2
    exit 1
fi

mv "$TMP" "$APP_YAML"

# Sync embedded mirror.
mkdir -p "$(dirname "$EMBEDDED_YAML")"
cp "$APP_YAML" "$EMBEDDED_YAML"

echo "✅ Version $V propagated: VERSION → $APP_YAML → $EMBEDDED_YAML"

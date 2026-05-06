#!/usr/bin/env bash
# Verify that the canonical VERSION file, .fulmen/app.yaml's version: field,
# and the embedded internal/assets/appidentity/app.yaml's version: field
# all agree. Exits non-zero on drift; suitable as a precommit / prepush /
# pr-final dependency.

set -euo pipefail

VERSION_FILE=${VERSION_FILE:-VERSION}
APP_YAML=${APP_YAML:-.fulmen/app.yaml}
EMBEDDED_YAML=${EMBEDDED_YAML:-internal/assets/appidentity/app.yaml}

extract_version() {
    local file=$1
    if [ ! -f "$file" ]; then
        echo "❌ Missing file: $file" >&2
        exit 1
    fi
    awk '/^[[:space:]]*version:[[:space:]]*/ {
        sub(/^[[:space:]]*version:[[:space:]]*/, "", $0)
        sub(/[[:space:]]*$/, "", $0)
        gsub(/"/, "", $0)
        print $0
        exit
    }' "$file"
}

if [ ! -f "$VERSION_FILE" ]; then
    echo "❌ Missing $VERSION_FILE" >&2
    exit 1
fi

V_FILE=$(tr -d '[:space:]' < "$VERSION_FILE")
V_APP=$(extract_version "$APP_YAML")
V_EMB=$(extract_version "$EMBEDDED_YAML")

if [ -z "$V_FILE" ] || [ -z "$V_APP" ] || [ -z "$V_EMB" ]; then
    echo "❌ Empty version in one of: $VERSION_FILE / $APP_YAML / $EMBEDDED_YAML" >&2
    echo "   $VERSION_FILE: '$V_FILE'" >&2
    echo "   $APP_YAML: '$V_APP'" >&2
    echo "   $EMBEDDED_YAML: '$V_EMB'" >&2
    exit 1
fi

if [ "$V_FILE" != "$V_APP" ] || [ "$V_FILE" != "$V_EMB" ]; then
    echo "❌ Version drift detected — files disagree:" >&2
    echo "   $VERSION_FILE: $V_FILE" >&2
    echo "   $APP_YAML: $V_APP" >&2
    echo "   $EMBEDDED_YAML: $V_EMB" >&2
    echo "" >&2
    echo "Fix: run 'make version-set VERSION=$V_FILE' (or similar) to" >&2
    echo "propagate from VERSION into the YAML files, then commit." >&2
    exit 1
fi

echo "✅ Version aligned at $V_FILE (VERSION + $APP_YAML + $EMBEDDED_YAML)"

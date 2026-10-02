#!/usr/bin/env bash

set -euo pipefail

# Export minisign public material. Optional PGP provenance is produced
# together with its manifest signatures by release-sign-pgp.
# Usage: export-release-keys.sh [dir]
#
# Env:
#   SIGNING_ENV_PREFIX - prefix for "<APP>_" env var lookups (ex: LIMENSAFE)
#   SIGNING_APP_NAME   - used for output file naming (ex: limensafe)
#   MINISIGN_KEY       - path to minisign secret key (used to locate .pub)
#   MINISIGN_PUB       - optional explicit path to minisign public key

DIR=${1:-dist/release}
mkdir -p "$DIR"

SIGNING_ENV_PREFIX=${SIGNING_ENV_PREFIX:-}
SIGNING_APP_NAME=${SIGNING_APP_NAME:-workhorse}

get_var() {
    local name="$1"
    # Prefer app-prefixed variables when SIGNING_ENV_PREFIX is set.
    if [ -n "${SIGNING_ENV_PREFIX}" ]; then
        local prefixed_name="${SIGNING_ENV_PREFIX}_${name}"
        local prefixed_val="${!prefixed_name:-}"
        if [ -n "$prefixed_val" ]; then
            echo "$prefixed_val"
            return 0
        fi
    fi

    local val="${!name:-}"
    if [ -n "$val" ]; then
        echo "$val"
        return 0
    fi

    echo ""
}

MINISIGN_KEY="$(get_var MINISIGN_KEY)"
MINISIGN_PUB="$(get_var MINISIGN_PUB)"

exported_any=false

if [ -n "${MINISIGN_KEY}" ] || [ -n "${MINISIGN_PUB}" ]; then
    pub_path="${MINISIGN_PUB}"
    if [ -z "${pub_path}" ]; then
        pub_path="${MINISIGN_KEY%.key}.pub"
    fi

    if [ ! -f "${pub_path}" ]; then
        echo "error: minisign public key not found (expected at ${pub_path}); set MINISIGN_PUB to override" >&2
        exit 1
    fi

    out="${DIR}/${SIGNING_APP_NAME}-minisign.pub"
    cp "${pub_path}" "${out}"
    echo "✅ Exported minisign public key to ${out}"
    exported_any=true
else
    echo "ℹ️  Skipping minisign public key export (set MINISIGN_KEY or MINISIGN_PUB to enable)"
fi

if [ "${exported_any}" = false ]; then
    echo "warning: no keys exported (set MINISIGN_KEY/MINISIGN_PUB)" >&2
fi

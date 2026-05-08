#!/usr/bin/env bash

set -euo pipefail

# Export public signing keys into the release artifact directory.
# Usage: export-release-keys.sh [dir]
#
# Env:
#   SIGNING_ENV_PREFIX - prefix for "<APP>_" env var lookups (ex: LIMENSAFE)
#   SIGNING_APP_NAME   - used for output file naming (ex: limensafe)
#   MINISIGN_KEY       - path to minisign secret key (used to locate .pub)
#   MINISIGN_PUB       - optional explicit path to minisign public key
#   PGP_KEY_ID         - gpg key/email/fingerprint to export (optional)
#   GPG_HOMEDIR           - isolated gpg homedir containing the signing key (required if PGP_KEY_ID is set)

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
PGP_KEY_ID="$(get_var PGP_KEY_ID)"
GPG_HOMEDIR="$(get_var GPG_HOMEDIR)"

# Back-compat with earlier naming.
if [ -z "$GPG_HOMEDIR" ]; then
    GPG_HOMEDIR="$(get_var GPG_HOME)"
fi

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

if [ -n "${PGP_KEY_ID}" ]; then
    if ! command -v gpg > /dev/null 2>&1; then
        echo "error: gpg not found in PATH (required to export PGP key)" >&2
        exit 1
    fi
    if [ -z "${GPG_HOMEDIR}" ]; then
        echo "error: GPG_HOMEDIR (or ${SIGNING_ENV_PREFIX}_GPG_HOMEDIR) must be set for PGP export" >&2
        exit 1
    fi
    if ! gpg --homedir "${GPG_HOMEDIR}" --list-keys "${PGP_KEY_ID}" > /dev/null 2>&1; then
        echo "error: public key ${PGP_KEY_ID} not found in GPG_HOMEDIR=${GPG_HOMEDIR}" >&2
        exit 1
    fi
    out="${DIR}/fulmenhq-release-signing-key.asc"
    gpg --homedir "${GPG_HOMEDIR}" --armor --output "${out}" --export "${PGP_KEY_ID}"
    echo "✅ Exported PGP public key to ${out} (homedir: ${GPG_HOMEDIR})"
    exported_any=true
else
    echo "ℹ️  Skipping PGP public key export (set PGP_KEY_ID to enable)"
fi

if [ "${exported_any}" = false ]; then
    echo "warning: no keys exported (set MINISIGN_KEY/MINISIGN_PUB and/or PGP_KEY_ID)" >&2
fi

#!/usr/bin/env bash

set -euo pipefail

# Verify release signatures (minisign and optional PGP).
# Verifies signatures on checksum manifests (SHA256SUMS, SHA512SUMS).
#
# Usage: verify-signatures.sh [dir]
#
# Env:
#   SIGNING_ENV_PREFIX - prefix for "<APP>_" env var lookups (ex: LIMENSAFE)
#   SIGNING_APP_NAME   - app name used for exported public-key fallback (ex: limensafe)
#   MINISIGN_PUB       - path to minisign public key (required when .minisig files exist)
#   GPG_HOMEDIR        - isolated gpg homedir for PGP verification (optional)

DIR=${1:-dist/release}

if [ ! -d "$DIR" ]; then
    echo "error: directory $DIR not found" >&2
    exit 1
fi

SIGNING_ENV_PREFIX=${SIGNING_ENV_PREFIX:-}
SIGNING_APP_NAME=${SIGNING_APP_NAME:-limensafe}

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

MINISIGN_PUB="$(get_var MINISIGN_PUB)"
GPG_HOMEDIR="$(get_var GPG_HOMEDIR)"

# Back-compat with earlier naming.
if [ -z "$GPG_HOMEDIR" ]; then
    GPG_HOMEDIR="$(get_var GPG_HOME)"
fi

if [ -z "${MINISIGN_PUB}" ]; then
    if [ -f "${DIR}/${SIGNING_APP_NAME}-minisign.pub" ]; then
        MINISIGN_PUB="${DIR}/${SIGNING_APP_NAME}-minisign.pub"
    elif [ -f "${DIR}/fulmenhq-release-minisign.pub" ]; then
        MINISIGN_PUB="${DIR}/fulmenhq-release-minisign.pub"
    fi
fi

verified=0
failed=0

verify_minisign() {
    local manifest="$1"
    local base="${DIR}/${manifest}"
    local sig="${base}.minisig"

    if [ ! -f "${sig}" ]; then
        echo "ℹ️  No minisign signature for ${manifest} (skipping)"
        return 0
    fi

    if [ -z "${MINISIGN_PUB}" ]; then
        echo "⚠️  MINISIGN_PUB (or ${SIGNING_ENV_PREFIX}_MINISIGN_PUB) not set, cannot verify ${manifest}.minisig"
        failed=$((failed + 1))
        return 1
    fi

    if [ ! -f "${MINISIGN_PUB}" ]; then
        echo "error: MINISIGN_PUB=${MINISIGN_PUB} not found" >&2
        failed=$((failed + 1))
        return 1
    fi

    if ! command -v minisign > /dev/null 2>&1; then
        echo "error: minisign not found in PATH" >&2
        failed=$((failed + 1))
        return 1
    fi

    echo "🔍 [minisign] Verifying ${manifest}"
    if minisign -V -p "${MINISIGN_PUB}" -m "${base}"; then
        echo "✅ ${manifest}.minisig verified"
        verified=$((verified + 1))
    else
        echo "❌ ${manifest}.minisig verification FAILED"
        failed=$((failed + 1))
    fi
}

verify_pgp() {
    local manifest="$1"
    local base="${DIR}/${manifest}"
    local sig="${base}.asc"

    if [ ! -f "${sig}" ]; then
        echo "ℹ️  No PGP signature for ${manifest} (skipping)"
        return 0
    fi

    if ! command -v gpg > /dev/null 2>&1; then
        echo "⚠️  gpg not found, cannot verify ${manifest}.asc"
        failed=$((failed + 1))
        return 1
    fi

    local gpg_opts=()
    if [ -n "${GPG_HOMEDIR}" ] && [ -d "${GPG_HOMEDIR}" ]; then
        gpg_opts=(--homedir "${GPG_HOMEDIR}")
    fi

    echo "🔍 [PGP] Verifying ${manifest}"
    if gpg "${gpg_opts[@]}" --verify "${sig}" "${base}" 2>&1; then
        echo "✅ ${manifest}.asc verified"
        verified=$((verified + 1))
    else
        echo "❌ ${manifest}.asc verification FAILED"
        failed=$((failed + 1))
    fi
}

echo "Verifying release signatures in ${DIR}..."
echo ""

verify_minisign "SHA256SUMS"
verify_minisign "SHA512SUMS"

echo ""

verify_pgp "SHA256SUMS"
verify_pgp "SHA512SUMS"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
if [ $failed -gt 0 ]; then
    echo "❌ Signature verification: ${verified} passed, ${failed} FAILED"
    exit 1
elif [ $verified -eq 0 ]; then
    echo "⚠️  No signatures found to verify"
    exit 1
else
    echo "✅ Signature verification: ${verified} passed"
fi

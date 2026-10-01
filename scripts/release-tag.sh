#!/usr/bin/env bash
set -euo pipefail

# Local signed-tag ceremony; create, inspect/verify and push are separate gates.
# Only app-prefixed identity variables and an isolated keyring are accepted.
# Adapted from the Goneat signed release-tag procedure.
die() { echo "error: $*" >&2; exit 1; }
require_var() { [ -n "${!1:-}" ] || die "$1 is required"; }
gpg_isolated() { gpg --homedir "$LIMENSAFE_GPG_HOMEDIR" --batch --no-tty "$@"; }

guard_version() {
    require_var LIMENSAFE_RELEASE_TAG
    local version number canonical
    version=$(cat VERSION)
    number='(0|[1-9][0-9]*)'
    canonical="^$number\\.$number\\.$number(-(rc|beta|alpha)\\.$number)?$"
    [[ "$version" =~ $canonical ]] || die "VERSION is not canonical X.Y.Z[-(rc|beta|alpha).N]"
    [ "$LIMENSAFE_RELEASE_TAG" = "v$version" ] || die "LIMENSAFE_RELEASE_TAG does not match VERSION"
}

guard_repo() {
    local fetch_url push_urls
    fetch_url=$(git remote get-url origin) || die "origin is missing"
    push_urls=$(git remote get-url --push --all origin) || die "cannot resolve origin push destination"
    [ "$push_urls" = "$fetch_url" ] || die "origin must have one matching fetch/push destination"
    [ -z "$(git status --porcelain)" ] || die "working tree is not clean"
    [ "$(git symbolic-ref --quiet --short HEAD || true)" = main ] || die "release tags require main"
    git fetch --quiet --no-tags origin '+refs/heads/main:refs/remotes/origin/main' || die "cannot fetch origin/main"
    [ "$(git rev-parse HEAD)" = "$(git rev-parse refs/remotes/origin/main)" ] || die "HEAD does not match origin/main"
}

remote_absent() {
    local refs
    refs=$(git ls-remote --tags origin "refs/tags/$LIMENSAFE_RELEASE_TAG") || die "cannot query origin tags"
    [ -z "$refs" ] || die "tag already exists on origin; never replace published tags"
}

identity() {
    require_var LIMENSAFE_PGP_KEY_ID
    require_var LIMENSAFE_GPG_HOMEDIR
    require_var LIMENSAFE_TAGGER_NAME
    require_var LIMENSAFE_TAGGER_EMAIL
    case "$LIMENSAFE_TAGGER_NAME$LIMENSAFE_TAGGER_EMAIL" in
        *'<'*|*'>'*|*$'\n'*|*$'\r'*) die "tagger identity contains invalid delimiters" ;;
    esac
    [ -d "$LIMENSAFE_GPG_HOMEDIR" ] || die "isolated GPG homedir is not a directory"
    local selector listing records matches count primary_flag validity caps
    selector=$(printf '%s' "$LIMENSAFE_PGP_KEY_ID" | tr '[:lower:]' '[:upper:]')
    forced=false
    if [ "${selector%!}" != "$selector" ]; then forced=true; selector=${selector%!}; fi
    [[ "$selector" =~ ^[0-9A-F]{40}$ || "$selector" =~ ^[0-9A-F]{16}$ ]] || die "key selector must be a full fingerprint or long key ID, optionally forced with !"
    listing=$(gpg_isolated --with-colons --fixed-list-mode --list-keys) || die "cannot list isolated keys"
    records=$(printf '%s\n' "$listing" | awk -F: '
        $1 == "pub" { primary=""; kind="pub"; v=$2; c=$12; pending=1; next }
        $1 == "sub" { kind="sub"; v=$2; c=$12; pending=1; next }
        $1 == "fpr" && pending {
            pending=0; if (kind=="pub") primary=$10;
            printf "key %s %s %s %s %s\n", primary,$10,(kind=="pub" ? "1" : "0"),(v=="" ? "-" : v),(c=="" ? "-" : c); next
        }
        $1 == "uid" { printf "uid %s %s %s\n", primary,($2=="" ? "-" : $2),$10 }
    ')
    matches=$(printf '%s\n' "$records" | awk -v s="$selector" '$1=="key" && ($3==s || (length(s)==16 && substr($3,25)==s))')
    count=$(printf '%s\n' "$matches" | grep -c '^key ' || true)
    [ "$count" -eq 1 ] || die "key selector is missing or ambiguous in isolated keyring"
    read -r _ primary selected primary_flag validity caps <<<"$matches"
    # A revoked/expired/disabled primary invalidates its signing subkeys too.
    local primary_validity
    primary_validity=$(printf '%s\n' "$records" | awk -v p="$primary" '$1=="key" && $3==p {print $5}')
    [[ ! "$primary_validity" =~ [reidn] ]] || die "primary key is not live"
    if [ "$forced" = true ]; then
        [[ ! "$validity" =~ [reidn] && "$caps" == *s* ]] || die "selected key is not live and signing-capable"
        allowed=$selected
    else
        [ "$primary_flag" = 1 ] || die "subkey selector requires !"
        allowed=$(printf '%s\n' "$records" | awk -v p="$primary" '$1=="key" && $2==p && $5 !~ /[reidn]/ && $6 ~ /s/ {printf "%s ",$3}')
        [ -n "$allowed" ] || die "primary has no live signing key"
    fi
    local uids email matched=false
    uids=$(printf '%s\n' "$records" | awk -v p="$primary" '$1=="uid" && $2==p && $3 !~ /[reidn]/ {sub(/^uid [^ ]+ [^ ]+ /,""); print}')
    while IFS= read -r uid; do
        email=$(printf '%s' "$uid" | sed -n 's/.*<\([^>]*\)>.*/\1/p')
        if [ "$email" = "$LIMENSAFE_TAGGER_EMAIL" ]; then matched=true; fi
    done <<<"$uids"
    [ "$matched" = true ] || die "tagger email is not a live UID email on selected key"
}

verify_tag() {
    local ref="refs/tags/$LIMENSAFE_RELEASE_TAG" target object header tagger status signer signature_primary body expected signature annotation
    [ "$(git cat-file -t "$ref" 2>/dev/null || true)" = tag ] || die "not an annotated tag"
    object=$(git cat-file -p "$ref")
    header=$(printf '%s\n' "$object" | sed '/^$/q')
    [ "$(printf '%s\n' "$header" | sed -n 's/^tag //p')" = "$LIMENSAFE_RELEASE_TAG" ] || die "tag object records a different name"
    [ "$(printf '%s\n' "$header" | sed -n 's/^type //p')" = commit ] || die "tag must target a commit directly"
    target=$(printf '%s\n' "$header" | sed -n 's/^object //p')
    [ "$target" = "$(git rev-parse HEAD)" ] || die "tag target is not HEAD"
    tagger=$(printf '%s\n' "$header" | sed -n 's/^tagger //p')
    [[ "$tagger" =~ ^(.+)\ \<([^\>]+)\>\ [0-9]+\ [-+][0-9]{4}$ ]] || die "invalid tagger record"
    [ "${BASH_REMATCH[1]}" = "$LIMENSAFE_TAGGER_NAME" ] && [ "${BASH_REMATCH[2]}" = "$LIMENSAFE_TAGGER_EMAIL" ] || die "unexpected tagger identity"
    status=$(GNUPGHOME="$LIMENSAFE_GPG_HOMEDIR" git -c gpg.format=openpgp -c gpg.program=gpg verify-tag --raw "$LIMENSAFE_RELEASE_TAG" 2>&1) || die "tag signature verification failed"
    [ "$(printf '%s\n' "$status" | grep -c '^\[GNUPG:\] VALIDSIG ' || true)" -eq 1 ] || die "expected exactly one valid signature"
    [ "$(printf '%s\n' "$status" | grep -c '^\[GNUPG:\] GOODSIG ' || true)" -eq 1 ] || die "expected exactly one good signature"
    if printf '%s\n' "$status" | grep -qE '^\[GNUPG:\] (BADSIG|ERRSIG|EXPSIG|EXPKEYSIG|REVKEYSIG) '; then die "bad signature status"; fi
    signer=$(printf '%s\n' "$status" | awk '$2=="VALIDSIG" {print $3}')
    signature_primary=$(printf '%s\n' "$status" | awk '$2=="VALIDSIG" {print $NF}')
    [ "$signature_primary" = "$primary" ] || die "unexpected signing primary"
    case " $allowed " in *" $signer "*) ;; *) die "unexpected signing key" ;; esac
    # Git identifies the signature boundary; annotation text can itself contain
    # an armor marker. Remove only that parsed signature suffix, then compare
    # every annotation byte, including the one required trailing newline.
    body=${object#*$'\n\n'}
    signature=$(git for-each-ref --format='%(contents:signature)' "$ref") || die "cannot parse tag signature boundary"
    [ -n "$signature" ] && [[ "$body" == *"$signature" ]] || die "parsed signature is not the tag body suffix"
    annotation=${body%"$signature"}
    expected="Release $LIMENSAFE_RELEASE_TAG"$'\n'
    [ "$annotation" = "$expected" ] || die "unexpected tag message; expected exactly Release $LIMENSAFE_RELEASE_TAG"
    echo "verified: $LIMENSAFE_RELEASE_TAG -> $target"
    echo "signer: $signer; primary: $primary"
}

create() {
    [ "${CI:-}" != true ] || die "live tag signing is disabled in CI"
    guard_version
    guard_repo
    if git show-ref --verify --quiet "refs/tags/$LIMENSAFE_RELEASE_TAG"; then die "tag already exists locally; never replace tags"; fi
    remote_absent
    identity
    local secret_listing sign_key
    secret_listing=$(gpg_isolated --with-colons --fixed-list-mode --list-secret-keys) || die "cannot list secret keys"
    # Unforced primary selectors permit any of their live signing keys. A
    # primary secret stub is sufficient when an allowed secret subkey exists.
    # Force the resolved available key rather than delegating selection to GPG.
    sign_key=$(printf '%s\n' "$secret_listing" | awk -F: -v allowed=" $allowed " '
        $1=="sec" || $1=="ssb" {token=$15; pending=1; next}
        $1=="fpr" && pending {
            pending=0
            if (index(allowed," " $10 " ") && token!="#") {print $10; exit}
        }')
    [ -n "$sign_key" ] || die "allowed live signing secret key is unavailable"
    sign_key="$sign_key!"
    GNUPGHOME="$LIMENSAFE_GPG_HOMEDIR" GIT_COMMITTER_NAME="$LIMENSAFE_TAGGER_NAME" GIT_COMMITTER_EMAIL="$LIMENSAFE_TAGGER_EMAIL" \
        git -c gpg.format=openpgp -c gpg.program=gpg tag -s -u "$sign_key" --cleanup=verbatim "$LIMENSAFE_RELEASE_TAG" -m "Release $LIMENSAFE_RELEASE_TAG"$'\n'
    # Fresh process preserves errexit even when verification is conditional.
    if ! bash "${BASH_SOURCE[0]}" verify; then
        git tag -d "$LIMENSAFE_RELEASE_TAG" >/dev/null
        die "deleted newly created local tag after failed verification"
    fi
    echo "created signed local tag; not pushed"
}

verify() { guard_version; identity; verify_tag; }

push() {
    guard_version
    guard_repo
    identity
    remote_absent
    verify_tag
    local object target refs remote_object remote_target
    object=$(git rev-parse "refs/tags/$LIMENSAFE_RELEASE_TAG")
    target=$(git rev-parse HEAD)
    git -c push.followTags=false -c remote.origin.mirror=false push origin "refs/tags/$LIMENSAFE_RELEASE_TAG:refs/tags/$LIMENSAFE_RELEASE_TAG"
    refs=$(git ls-remote --tags origin "refs/tags/$LIMENSAFE_RELEASE_TAG" "refs/tags/$LIMENSAFE_RELEASE_TAG^{}") || die "cannot independently read pushed tag"
    remote_object=$(printf '%s\n' "$refs" | awk -v r="refs/tags/$LIMENSAFE_RELEASE_TAG" '$2==r {print $1}')
    remote_target=$(printf '%s\n' "$refs" | awk -v r="refs/tags/$LIMENSAFE_RELEASE_TAG^{}" '$2==r {print $1}')
    [ "$remote_object" = "$object" ] && [ "$remote_target" = "$target" ] || die "remote tag does not match inspected local object and target"
    echo "pushed $LIMENSAFE_RELEASE_TAG only; remote object and target verified"
}

case "${1:-}" in
    create) create ;;
    verify) verify ;;
    push) push ;;
    *) die "usage: release-tag.sh create|verify|push" ;;
esac

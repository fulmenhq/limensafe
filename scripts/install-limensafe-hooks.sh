#!/usr/bin/env sh
set -eu

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  exit 0
fi

hook_path="$(git rev-parse --git-path hooks/pre-push)"
hook_dir="$(dirname "$hook_path")"
mkdir -p "$hook_dir"

if [ ! -f "$hook_path" ]; then
  {
    printf '%s\n' '#!/usr/bin/env sh'
    printf '%s\n' 'set -eu'
    printf '\n'
  } >"$hook_path"
fi

if grep -q 'limensafe verify-attestation hook' "$hook_path"; then
  chmod +x "$hook_path"
  exit 0
fi

cat >>"$hook_path" <<'HOOK'

# limensafe verify-attestation hook
if command -v limensafe >/dev/null 2>&1; then
  limensafe verify-attestation --mode push
elif [ -x ./bin/limensafe ]; then
  ./bin/limensafe verify-attestation --mode push
else
  echo "pre-push: limensafe binary not found; run make build or make install" >&2
  exit 1
fi
HOOK

chmod +x "$hook_path"

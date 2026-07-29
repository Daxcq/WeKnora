#!/usr/bin/env bash
# Fill per-deployment secrets in .env with random values.
#
# Every bootstrap path (`make docker-run`, scripts/start_all.sh, ...) copies
# .env.example to .env, so any literal secret shipped in the template would be
# identical — and publicly known — on every WeKnora install. A known JWT_SECRET
# lets anyone mint a valid access token for any user; a known SYSTEM_AES_KEY
# lets anyone decrypt the API keys and provider credentials stored in the DB.
#
# Only placeholder/empty values are replaced, so the script is idempotent and
# never overwrites a secret an operator has already set.
set -euo pipefail

ENV_FILE="${1:-.env}"

[ -f "${ENV_FILE}" ] || exit 0

rand() { # rand <bytes> -> url-safe random string
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 "$1" | tr -d '\n=+/' 
  else
    head -c "$((3 * $1))" /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

# Values that must never survive into a real deployment: the templates'
# placeholders and anything empty.
is_placeholder() {
  case "$1" in
    "" | weknora-jwt-secret | weknora-system-aes-key-32bytes'!!' | CHANGE-ME-* | CHANGE_ME* | changeme*) return 0 ;;
    *) return 1 ;;
  esac
}

current() {
  sed -n "s|^$1=||p" "${ENV_FILE}" | tail -n 1
}

set_value() {
  local key="$1" val="$2"
  if grep -qE "^${key}=" "${ENV_FILE}"; then
    # `|` as the sed delimiter: base64 output can contain `/`.
    sed -i "s|^${key}=.*|${key}=${val}|" "${ENV_FILE}"
  else
    printf '%s=%s\n' "${key}" "${val}" >>"${ENV_FILE}"
  fi
  echo "[gen-secrets] generated ${key}"
}

if is_placeholder "$(current JWT_SECRET)"; then
  set_value JWT_SECRET "$(rand 48 | cut -c1-48)"
fi

# AES-256 requires exactly 32 bytes; a wrong length silently disables
# encryption (see internal/runtime/startup.go).
if is_placeholder "$(current SYSTEM_AES_KEY)"; then
  set_value SYSTEM_AES_KEY "$(rand 48 | cut -c1-32)"
fi

#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ -f .env ]]; then
  # Avoid `source` — COMPOSE_FILE contains "|" which bash treats as a pipe.
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    [[ "$line" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]] || continue
    key="${BASH_REMATCH[1]}"
    val="${BASH_REMATCH[2]}"
    if [[ "$val" =~ ^\"(.*)\"$ ]]; then
      val="${BASH_REMATCH[1]}"
    elif [[ "$val" =~ ^\'(.*)\'$ ]]; then
      val="${BASH_REMATCH[1]}"
    fi
    export "$key=$val"
  done < .env
fi

WEB_PORT="${WEB_HOST_PORT:-18080}"
GW_PORT="${GATEWAY_HOST_PORT:-18088}"
PORTAL_PORT="${PORTAL_HTTP_HOST_PORT:-18000}"

fail=0
check() {
  local name="$1" url="$2"
  if curl -fsS "$url" >/dev/null; then
    echo "OK $name $url"
  else
    echo "FAIL $name $url" >&2
    fail=1
  fi
}

check web "http://127.0.0.1:${WEB_PORT}/healthz"
check gateway "http://127.0.0.1:${GW_PORT}/healthz"
check portal "http://127.0.0.1:${PORTAL_PORT}/readyz"

if [[ $fail -ne 0 ]]; then
  exit 1
fi

cat > deploy/last-smoke.json <<EOF
{"ok":true,"web":"http://127.0.0.1:${WEB_PORT}/healthz","gateway":"http://127.0.0.1:${GW_PORT}/healthz","portal":"http://127.0.0.1:${PORTAL_PORT}/readyz"}
EOF
echo "smoke ok"

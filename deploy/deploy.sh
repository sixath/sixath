#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

WITH_NEO4J=0
WITH_TLS=0
DO_BUILD=0
DO_DOWN=0
SMOKE_ONLY=0

usage() {
  cat <<'EOF'
Usage: ./deploy/deploy.sh [--with-neo4j] [--with-tls] [--build] [--down] [--smoke-only]
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --with-neo4j) WITH_NEO4J=1 ;;
    --with-tls) WITH_TLS=1 ;;
    --build) DO_BUILD=1 ;;
    --down) DO_DOWN=1 ;;
    --smoke-only) SMOKE_ONLY=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 1 ;;
  esac
  shift
done

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing command: $1" >&2; exit 1; }
}

need_cmd docker
docker compose version >/dev/null

ver="$(docker compose version --short 2>/dev/null || docker compose version | head -n1)"
# Require Compose >= 2.20 when we can parse X.Y
if [[ "$ver" =~ ([0-9]+)\.([0-9]+) ]]; then
  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"
  if (( major < 2 || (major == 2 && minor < 20) )); then
    echo "Docker Compose >= 2.20 required (got $ver)" >&2
    exit 1
  fi
fi

if [[ ! -f .env ]]; then
  cp .env.example .env
  echo "WARNING: created .env from .env.example"
fi

mkdir -p secrets
for ex in secrets/*.txt.example; do
  [[ -e "$ex" ]] || continue
  dest="${ex%.example}"
  if [[ ! -f "$dest" ]]; then
    # Strip CR/LF: MySQL MYSQL_*_PASSWORD_FILE treats trailing \r as part of the password.
    tr -d '\r\n' < "$ex" > "$dest"
    echo "WARNING: created $dest from example — replace before production use"
  fi
done

# Normalize existing secrets (Windows CRLF would break MySQL vs Portal password match).
for dest in secrets/*.txt; do
  [[ -f "$dest" ]] || continue
  [[ "$dest" == *.example ]] && continue
  tmp="$(mktemp)"
  tr -d '\r\n' < "$dest" > "$tmp"
  cat "$tmp" > "$dest"
  rm -f "$tmp"
done

# Load KEY=VAL from .env without evaluating shell metacharacters (e.g. | in COMPOSE_FILE).
load_dotenv() {
  local f="${1:-.env}"
  [[ -f "$f" ]] || return 0
  local line key val
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
  done < "$f"
}

load_dotenv .env

if [[ "$WITH_TLS" -eq 1 ]]; then
  if [[ -z "${DOMAIN:-}" || "$DOMAIN" == "localhost" ]]; then
    echo "ERROR: --with-tls requires DOMAIN in .env (not empty/localhost)" >&2
    exit 1
  fi
fi

PROFILES=()
[[ "$WITH_NEO4J" -eq 1 ]] && PROFILES+=(--profile neo4j)
[[ "$WITH_TLS" -eq 1 ]] && PROFILES+=(--profile tls)

if [[ "$DO_DOWN" -eq 1 ]]; then
  docker compose --profile neo4j --profile tls down
  exit 0
fi

if [[ "$SMOKE_ONLY" -eq 0 ]]; then
  UP_ARGS=(up -d)
  [[ "$DO_BUILD" -eq 1 ]] && UP_ARGS+=(--build)
  docker compose "${PROFILES[@]}" "${UP_ARGS[@]}"

  echo "waiting for healthy services..."
  deadline=$((SECONDS + 180))
  while (( SECONDS < deadline )); do
    if ./deploy/smoke-check.sh >/dev/null 2>&1; then
      break
    fi
    sleep 3
  done
fi

./deploy/smoke-check.sh

WEB_PORT="${WEB_HOST_PORT:-18080}"
echo "Web UI: http://127.0.0.1:${WEB_PORT}"
echo "Bootstrap email: ${BOOTSTRAP_ADMIN_EMAIL:-admin@example.com}"
if [[ "$WITH_TLS" -eq 1 ]]; then
  echo "TLS URL: https://${DOMAIN}"
fi

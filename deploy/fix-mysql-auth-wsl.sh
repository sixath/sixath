#!/usr/bin/env bash
# One-shot fix: strip CR from secrets, wipe mysql volume, redeploy.
# Usage: bash deploy/fix-mysql-auth-wsl.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "==> normalize secrets/*.txt (strip CR/LF; lengths only)"
shopt -s nullglob
for f in secrets/*.txt; do
  case "$f" in
    *.example) continue ;;
  esac
  before="$(wc -c < "$f" | tr -d ' ')"
  tmp="$(mktemp)"
  tr -d '\r\n' < "$f" > "$tmp"
  after="$(wc -c < "$tmp" | tr -d ' ')"
  cat "$tmp" > "$f"
  rm -f "$tmp"
  echo "  $f ${before} -> ${after} bytes"
done

echo "==> docker compose down"
docker compose --profile neo4j --profile tls down || docker compose down

echo "==> remove mysql data volume (re-init with clean password)"
if docker volume inspect sixath_mysql_data >/dev/null 2>&1; then
  docker volume rm sixath_mysql_data
  echo "  removed sixath_mysql_data"
else
  echo "  sixath_mysql_data not found"
fi

echo "==> redeploy"
exec bash "$ROOT/deploy/deploy-wsl.sh" --build

#!/bin/sh
set -eu
read_secret() {
  f="/run/secrets/$1"
  if [ -f "$f" ]; then tr -d '\r\n' < "$f"; fi
}
if [ -z "${SATH_RUNTIME_TOKEN:-}" ]; then
  v=$(read_secret runtime_token)
  [ -n "$v" ] && export SATH_RUNTIME_TOKEN="$v"
fi
exec ./gateway -config /app/configs/config.docker.yaml

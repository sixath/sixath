#!/bin/sh
set -eu
if [ -d /data/conf-ro ]; then
  mkdir -p /data/conf
  cp -a /data/conf-ro/. /data/conf/
fi
read_secret() {
  f="/run/secrets/$1"
  if [ -f "$f" ]; then tr -d '\r\n' < "$f"; fi
}
[ -z "${SATH_RUNTIME_TOKEN:-}" ] && v=$(read_secret runtime_token) && [ -n "$v" ] && export SATH_RUNTIME_TOKEN="$v"
[ -z "${SATH_BOOTSTRAP_PASSWORD:-}" ] && v=$(read_secret bootstrap_password) && [ -n "$v" ] && export SATH_BOOTSTRAP_PASSWORD="$v"
[ -z "${SATH_BOOTSTRAP_TOKEN:-}" ] && v=$(read_secret bootstrap_token) && [ -n "$v" ] && export SATH_BOOTSTRAP_TOKEN="$v"
[ -z "${SATH_MYSQL_PASSWORD:-}" ] && v=$(read_secret mysql_root_password) && [ -n "$v" ] && export SATH_MYSQL_PASSWORD="$v"
if [ -z "${SATH_BOOTSTRAP_EMAIL:-}" ] && [ -n "${BOOTSTRAP_ADMIN_EMAIL:-}" ]; then
  export SATH_BOOTSTRAP_EMAIL="$BOOTSTRAP_ADMIN_EMAIL"
fi
NEO4J_PW=$(read_secret neo4j_password || true)
if [ -n "${NEO4J_PW:-}" ] && [ -f /data/conf/agent_extra.yaml ]; then
  awk -v pw="$NEO4J_PW" '{gsub(/REPLACE_ME/, pw); print}' /data/conf/agent_extra.yaml > /data/conf/agent_extra.yaml.tmp
  mv /data/conf/agent_extra.yaml.tmp /data/conf/agent_extra.yaml
fi
exec ./backend -conf /data/conf

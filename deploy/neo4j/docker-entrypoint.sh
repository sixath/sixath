#!/bin/bash
set -euo pipefail
PW="$(tr -d '\r\n' < /run/secrets/neo4j_password)"
export NEO4J_AUTH="neo4j/${PW}"
exec /startup/docker-entrypoint.sh neo4j

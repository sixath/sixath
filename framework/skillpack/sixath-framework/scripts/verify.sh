#!/usr/bin/env bash
# Run from execute_skill_script; finds framework root via go.mod.
set -euo pipefail
ROOT="${SKILL_ROOT:-}"
if [[ -z "$ROOT" ]]; then
  HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  ROOT="$(cd "$HERE/../../.." && pwd)"
fi
if [[ ! -f "$ROOT/go.mod" ]]; then
  echo "error: framework go.mod not found at $ROOT" >&2
  exit 1
fi
cd "$ROOT"
echo "==> go test ./... (framework root: $ROOT)"
go test ./...

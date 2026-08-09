#!/usr/bin/env bash
# Hermes P0 automated E2E (Task 16). Run from repo root or portal/.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
echo "== framework unit tests =="
(cd "$ROOT/../framework" 2>/dev/null || cd "$ROOT/framework") && go test ./... -count=1
echo "== framework memory integration (optional tag) =="
(cd "$ROOT/../framework" 2>/dev/null || cd "$ROOT/framework") && go test -tags=integration ./tool/... -run MemoryIntegration -count=1 || true
echo "== portal Hermes P0 E2E =="
cd "$ROOT"
go test ./internal/chat/... -run TestHermesP0E2E_Checklist -count=1 -v
echo "== portal chat + cron packages =="
go test ./internal/chat/... ./internal/cron/... -count=1
echo "OK: Hermes P0 E2E passed"

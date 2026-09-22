#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
f=deploy/deploy-wsl.sh
python3 - <<'PY'
from pathlib import Path
p = Path("deploy/deploy-wsl.sh")
t = p.read_bytes().replace(b"\r\n", b"\n").replace(b"\r", b"\n")
t = t.replace(b"ensure_linux_data_di\n", b"ensure_linux_data_dir\n")
p.write_bytes(t)
print("ok", t.count(b"ensure_linux_data_dir()"), "defs;", t.count(b"\nensure_linux_data_dir\n"), "calls")
PY
sed -i 's/\r$//' deploy/deploy.sh deploy/smoke-check.sh deploy/deploy-wsl.sh || true
exec bash deploy/deploy-wsl.sh --build

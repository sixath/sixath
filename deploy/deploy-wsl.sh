#!/usr/bin/env bash
# WSL one-click deploy wrapper around deploy/deploy.sh.
# Target: Docker Engine inside WSL2 (no Docker Desktop required).
# Also handles: CRLF scripts, data_root on Linux FS when repo is under /mnt/*.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

usage() {
  cat <<'EOF'
Usage: ./deploy/deploy-wsl.sh [--with-neo4j] [--with-tls] [--build] [--down] [--smoke-only]

WSL-oriented wrapper for deploy/deploy.sh (Docker Engine in WSL, not Docker Desktop).
First-time Docker install:  bash deploy/install-docker-wsl.sh
From Windows PowerShell:    .\deploy\deploy-wsl.ps1 -Build
EOF
}

WITH_NEO4J=0
WITH_TLS=0
DO_BUILD=0
DO_DOWN=0
SMOKE_ONLY=0

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

is_wsl() {
  grep -qiE 'microsoft|wsl' /proc/version 2>/dev/null || [[ -n "${WSL_DISTRO_NAME:-}" ]]
}

if ! is_wsl; then
  echo "NOTE: not detected as WSL; continuing (same as ./deploy/deploy.sh)." >&2
fi

if ! command -v docker >/dev/null 2>&1; then
  cat >&2 <<'EOF'
ERROR: docker not found in this WSL distro.

Company environments often block Docker Desktop — install Docker Engine inside WSL instead:
  bash deploy/install-docker-wsl.sh
Then re-open the WSL terminal and retry.
EOF
  exit 1
fi

# If dockerd is installed but not running (common when systemd is off), try to start it.
if ! docker info >/dev/null 2>&1; then
  if command -v systemctl >/dev/null 2>&1 && systemctl list-units >/dev/null 2>&1; then
    sudo systemctl start docker 2>/dev/null || true
  elif command -v service >/dev/null 2>&1; then
    sudo service docker start 2>/dev/null || true
  fi
fi

# Last resort: start dockerd in background (WSL without systemd).
if ! docker info >/dev/null 2>&1; then
  if ! pgrep -x dockerd >/dev/null 2>&1; then
    echo "NOTE: starting dockerd inside WSL..."
    if command -v update-alternatives >/dev/null; then
      sudo update-alternatives --set iptables /usr/sbin/iptables-legacy 2>/dev/null || true
      sudo update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy 2>/dev/null || true
    fi
    sudo mkdir -p /var/run
    sudo dockerd >/tmp/dockerd.log 2>&1 &
    disown 2>/dev/null || true
    for _ in $(seq 1 30); do
      docker info >/dev/null 2>&1 && break
      sleep 1
    done
  fi
fi

if ! docker info >/dev/null 2>&1; then
  cat >&2 <<'EOF'
ERROR: docker CLI found but daemon unreachable.

This path expects Docker Engine inside WSL (not Docker Desktop).
Try:
  1) bash deploy/install-docker-wsl.sh
  2) sudo service docker start
  3) Or enable systemd in /etc/wsl.conf:
       [boot]
       systemd=true
     then from Windows: wsl --shutdown
  4) Check /tmp/dockerd.log
EOF
  exit 1
fi

# Strip CRLF from shell scripts when repo lives on a Windows mount (common with git on NTFS).
fix_crlf_scripts() {
  local f
  for f in deploy/deploy.sh deploy/smoke-check.sh deploy/deploy-wsl.sh \
           deploy/portal/docker-entrypoint.sh deploy/gateway/docker-entrypoint.sh \
           deploy/neo4j/docker-entrypoint.sh; do
    [[ -f "$f" ]] || continue
    if grep -q $'\r' "$f" 2>/dev/null; then
      # Prefer in-place without touching inode metadata awkwardly on /mnt/*
      local tmp
      tmp="$(mktemp)"
      tr -d '\r' < "$f" > "$tmp"
      cat "$tmp" > "$f"
      rm -f "$tmp"
      echo "NOTE: stripped CRLF from $f"
    fi
  done
}
fix_crlf_scripts

# When the repo is under /mnt/<drive>/..., bind-mount I/O is slow and flaky.
# Default Portal data_root to a path inside the WSL Linux filesystem.
ensure_linux_data_dir() {
  case "$ROOT" in
    /mnt/*)
      ;;
    *)
      return 0
      ;;
  esac

  local default_data="${HOME}/sixath-data/portal"
  mkdir -p "$default_data"

  if [[ ! -f .env ]]; then
    cp .env.example .env
    echo "WARNING: created .env from .env.example"
  fi

  if grep -qE '^[[:space:]]*PORTAL_DATA_DIR=' .env 2>/dev/null; then
    local cur
    cur="$(grep -E '^[[:space:]]*PORTAL_DATA_DIR=' .env | tail -n1 | cut -d= -f2- | tr -d '\r' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    case "$cur" in
      ./data/portal|data/portal|"")
        # Replace relative default with Linux-home path
        local tmp
        tmp="$(mktemp)"
        grep -vE '^[[:space:]]*PORTAL_DATA_DIR=' .env | tr -d '\r' > "$tmp"
        printf 'PORTAL_DATA_DIR=%s\n' "$default_data" >> "$tmp"
        cat "$tmp" > .env
        rm -f "$tmp"
        echo "NOTE: repo is on Windows mount ($ROOT); set PORTAL_DATA_DIR=$default_data"
        ;;
      /mnt/*)
        echo "WARNING: PORTAL_DATA_DIR=$cur is still on /mnt/* (slow). Prefer $default_data" >&2
        ;;
      *)
        echo "NOTE: using PORTAL_DATA_DIR=$cur"
        ;;
    esac
  else
    printf '\nPORTAL_DATA_DIR=%s\n' "$default_data" >> .env
    echo "NOTE: repo is on Windows mount ($ROOT); set PORTAL_DATA_DIR=$default_data"
  fi
}
ensure_linux_data_dir

ARGS=()
[[ "$WITH_NEO4J" -eq 1 ]] && ARGS+=(--with-neo4j)
[[ "$WITH_TLS" -eq 1 ]] && ARGS+=(--with-tls)
[[ "$DO_BUILD" -eq 1 ]] && ARGS+=(--build)
[[ "$DO_DOWN" -eq 1 ]] && ARGS+=(--down)
[[ "$SMOKE_ONLY" -eq 1 ]] && ARGS+=(--smoke-only)

chmod +x deploy/deploy.sh deploy/smoke-check.sh 2>/dev/null || true

echo "WSL distro: ${WSL_DISTRO_NAME:-unknown}"
echo "Repo root:  $ROOT"
exec bash "$ROOT/deploy/deploy.sh" "${ARGS[@]}"

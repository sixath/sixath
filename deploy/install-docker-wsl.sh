#!/usr/bin/env bash
# Install Docker Engine + Compose plugin inside WSL Ubuntu (no Docker Desktop).
# Usage (in Ubuntu):
#   bash deploy/install-docker-wsl.sh
set -euo pipefail

if ! grep -qiE 'microsoft|wsl' /proc/version 2>/dev/null && [[ -z "${WSL_DISTRO_NAME:-}" ]]; then
  echo "WARNING: not detected as WSL; continuing anyway." >&2
fi

if [[ "$(id -u)" -eq 0 ]]; then
  echo "ERROR: run as a normal user (script will sudo when needed)." >&2
  exit 1
fi

if ! command -v sudo >/dev/null; then
  echo "ERROR: sudo is required." >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive

echo "==> Installing prerequisites"
sudo apt-get update -y
sudo apt-get install -y ca-certificates curl gnupg

echo "==> Adding Docker apt repository"
sudo install -m 0755 -d /etc/apt/keyrings
if [[ ! -f /etc/apt/keyrings/docker.gpg ]]; then
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
    | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  sudo chmod a+r /etc/apt/keyrings/docker.gpg
fi

. /etc/os-release
ARCH="$(dpkg --print-architecture)"
echo "deb [arch=${ARCH} signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null

echo "==> Installing Docker Engine + Compose plugin"
sudo apt-get update -y
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

echo "==> Allow current user to run docker without sudo"
sudo usermod -aG docker "$USER" || true

# WSL often has no real systemd; start dockerd manually and persist a helper.
# Prefer systemd service when available (Ubuntu WSL with [boot] systemd=true).
ensure_dockerd() {
  # Group membership from usermod may not apply until re-login; fall back to sudo.
  docker_ready() {
    docker info >/dev/null 2>&1 || sudo docker info >/dev/null 2>&1
  }

  if docker_ready; then
    return 0
  fi

  if command -v systemctl >/dev/null 2>&1 && [[ "$(ps -p 1 -o comm= 2>/dev/null)" == "systemd" ]]; then
    echo "==> Starting docker via systemd"
    sudo systemctl enable --now docker 2>/dev/null || sudo systemctl start docker 2>/dev/null || true
    for _ in $(seq 1 30); do
      docker_ready && return 0
      sleep 1
    done
  fi

  if command -v service >/dev/null 2>&1; then
    sudo service docker start 2>/dev/null || true
    for _ in $(seq 1 20); do
      docker_ready && return 0
      sleep 1
    done
  fi

  if ! pgrep -x dockerd >/dev/null 2>&1; then
    echo "==> Starting dockerd (no systemd)"
    sudo mkdir -p /var/run
    # iptables-legacy avoids common WSL nftables issues
    if command -v update-alternatives >/dev/null; then
      sudo update-alternatives --set iptables /usr/sbin/iptables-legacy 2>/dev/null || true
      sudo update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy 2>/dev/null || true
    fi
    sudo dockerd >/tmp/dockerd.log 2>&1 &
    disown || true
    for _ in $(seq 1 30); do
      docker_ready && return 0
      sleep 1
    done
  fi

  docker_ready
}

if ! ensure_dockerd; then
  cat >&2 <<'EOF'
ERROR: dockerd did not become ready. Check /tmp/dockerd.log
If your distro uses systemd, try: sudo systemctl start docker
Also try a new WSL window (docker group) or: newgrp docker
EOF
  exit 1
fi

# Persist auto-start for new WSL shells (idempotent)
BASHRC="${HOME}/.bashrc"
MARKER="# sixath-docker-wsl"
if [[ -f "$BASHRC" ]] && ! grep -qF "$MARKER" "$BASHRC"; then
  cat >> "$BASHRC" <<'EOF'

# sixath-docker-wsl
sixath_ensure_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    return 0
  fi
  if docker info >/dev/null 2>&1; then
    return 0
  fi
  if pgrep -x dockerd >/dev/null 2>&1; then
    return 0
  fi
  sudo mkdir -p /var/run
  sudo dockerd >/tmp/dockerd.log 2>&1 &
  disown 2>/dev/null || true
}
sixath_ensure_docker
EOF
  echo "==> Appended dockerd auto-start helper to ~/.bashrc"
fi

ver="$(docker compose version --short 2>/dev/null || docker compose version | head -n1)"
echo
echo "Docker Engine ready (Compose: $ver)"
echo "IMPORTANT: close this WSL window and open a new one so group 'docker' applies,"
echo "           or run: newgrp docker"
echo
echo "Next:"
echo "  cd /path/to/sixath"
echo "  ./deploy/deploy-wsl.sh --build"

#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILL_SRC="${SCRIPT_DIR}/sixath-framework"
SKILL_NAME="sixath-framework"

if [[ ! -f "${SKILL_SRC}/SKILL.md" ]]; then
  echo "Error: ${SKILL_SRC}/SKILL.md not found" >&2
  exit 1
fi

install_one() {
  local dest="$1"
  mkdir -p "$(dirname "${dest}")"
  rm -rf "${dest}"
  cp -R "${SKILL_SRC}" "${dest}"
  echo "Installed -> ${dest}"
}

install_one "${HOME}/.cursor/skills/${SKILL_NAME}"
install_one "${HOME}/.claude/skills/${SKILL_NAME}"
install_one "${HOME}/.codex/skills/${SKILL_NAME}"
install_one "${HOME}/.agents/skills/${SKILL_NAME}"

echo ""
echo "Done. Use @sixath-framework in your agent."
echo "For framework runtime: add 'skillpack' to skills.skills_dirs in config.yaml"

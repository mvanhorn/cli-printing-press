#!/usr/bin/env bash
# Install internal printing-press development skills to ~/.claude/skills/
# so they're available globally without needing the repo checked out.
#
# Usage: .claude/scripts/install-internal-skills.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_SKILLS="$SCRIPT_DIR/../../skills"
USER_SKILLS="$HOME/.claude/skills"

if [ ! -d "$REPO_SKILLS" ]; then
  echo "error: no skills found at $REPO_SKILLS"
  exit 1
fi

mkdir -p "$USER_SKILLS"

copied=0
for skill_dir in "$REPO_SKILLS"/*/; do
  [ -d "$skill_dir" ] || continue
  skill_name="$(basename "$skill_dir")"
  target="$USER_SKILLS/$skill_name"

  if [ -d "$target" ]; then
    echo "updating: $skill_name"
    rm -rf "$target"
  else
    echo "installing: $skill_name"
  fi

  cp -R "$skill_dir" "$target"
  copied=$((copied + 1))
done

if [ "$copied" -eq 0 ]; then
  echo "no skills found to install"
else
  echo "installed $copied skill(s) to $USER_SKILLS"
fi

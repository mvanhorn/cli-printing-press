#!/usr/bin/env bash
set -euo pipefail

shopt -s nullglob

status=0
for skill in skills/*/SKILL.md; do
  if [[ "$(sed -n '1p' "$skill")" != "---" ]]; then
    echo "::error file=$skill::SKILL.md must start with YAML frontmatter"
    status=1
    continue
  fi

  end_line="$(awk 'NR > 1 && $0 == "---" { print NR; exit }' "$skill")"
  if [[ -z "$end_line" ]]; then
    echo "::error file=$skill::SKILL.md frontmatter must close with ---"
    status=1
    continue
  fi

  frontmatter="$(sed -n "2,$((end_line - 1))p" "$skill")"
  if ! grep -Eq '^name:[[:space:]]+[^[:space:]]' <<<"$frontmatter"; then
    echo "::error file=$skill::SKILL.md frontmatter must include name"
    status=1
  fi
  if ! grep -Eq '^description:[[:space:]]+.' <<<"$frontmatter"; then
    echo "::error file=$skill::SKILL.md frontmatter must include description"
    status=1
  fi
done

exit "$status"

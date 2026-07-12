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

  if ! sed -n "2,$((end_line - 1))p" "$skill" | ruby -ryaml -e '
    begin
      frontmatter = YAML.safe_load($stdin.read, permitted_classes: [Symbol], aliases: false)
    rescue Psych::Exception => e
      warn "invalid YAML frontmatter: #{e.message}"
      exit 2
    end

    unless frontmatter.is_a?(Hash)
      warn "frontmatter must be a YAML mapping"
      exit 2
    end

    %w[name description].each do |field|
      value = frontmatter[field]
      if value.nil? || value.to_s.strip.empty?
        warn "frontmatter must include #{field}"
        exit 2
      end
    end
  '; then
    echo "::error file=$skill::SKILL.md frontmatter must be valid YAML with name and description"
    status=1
  fi
done

node .github/scripts/validate-printing-press-skill-list.mjs || status=1

exit "$status"

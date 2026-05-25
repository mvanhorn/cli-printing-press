#!/usr/bin/env bash
#
# verify-learn-purity.sh — fail if the learn-loop templates reference
# domain-specific identifiers that leaked in from the prediction-goat plan
# lineage.
#
# The self-learning recall/teach loop (plan 2026-05-23-002) generalizes a
# pattern first shipped in prediction-goat-pp-cli. The generator templates
# under internal/generator/templates/learn*.go.tmpl must stay domain-neutral
# so every printed CLI gets the loop without inheriting prediction-market
# vocabulary. This script greps the stripped-comment bodies of those templates
# for the watchlist identifiers and exits non-zero on any hit.
#
# Comments are stripped so legitimate lineage notes in doc.go.tmpl files can
# reference prediction-goat without tripping the gate. The doc.go allowlist is
# explicit: prediction-goat is the one identifier permitted inside doc.go.tmpl
# comments anywhere in the learn template tree.
#
# Pass when no learn templates exist yet (the templates are emitted by U3-U5
# of the plan; this script ships in U11 as a CI belt for future contributors).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
templates_root="$repo_root/internal/generator/templates"

# Domain identifiers prohibited in stripped learn template source. Each is the
# canonical spelling that leaked into early drafts; the prediction-goat lineage
# is intentional in doc.go.tmpl comments and called out via the allowlist below.
watch_terms=(
  "kalshi"
  "polymarket"
  "country_iso2"
  "prediction_goat"
)

# Gather every learn template path. Four shapes:
#   - internal/generator/templates/learn*.go.tmpl       (flat helpers)
#   - internal/generator/templates/learn/*.go.tmpl      (learn package)
#   - internal/generator/templates/learn_*/*.go.tmpl    (package subdirs)
#   - internal/generator/templates/teach*.go.tmpl       (learn CLI commands)
# `find` returns empty when no matches; the empty-check below handles that.
# Avoid `mapfile` for portability with macOS bash 3.2.
templates_list="$(
  {
    find "$templates_root" -maxdepth 1 -type f -name 'learn*.go.tmpl' 2>/dev/null || true
    find "$templates_root" -mindepth 2 -type f -path '*/learn/*.go.tmpl' 2>/dev/null || true
    find "$templates_root" -maxdepth 1 -type f -name 'teach*.go.tmpl' 2>/dev/null || true
    find "$templates_root" -mindepth 2 -type f -path '*/learn_*/*.go.tmpl' 2>/dev/null || true
  } | sort -u
)"

if [[ -z "$templates_list" ]]; then
  echo "verify-learn-purity: no learn templates found under $templates_root — skipping"
  exit 0
fi

template_count=0
fail=0
while IFS= read -r tmpl; do
  [[ -z "$tmpl" ]] && continue
  template_count=$((template_count + 1))
  rel="${tmpl#$repo_root/}"
  base="$(basename "$tmpl")"

  # Strip full-line // comments so legitimate lineage notes don't trip the
  # gate. Block-comment stripping is deliberately omitted — the templates use
  # //-style comments only, and bash-regex block stripping would risk false
  # negatives on intra-line text.
  stripped="$(grep -v '^[[:space:]]*//' "$tmpl" || true)"

  for term in "${watch_terms[@]}"; do
    if grep -iqE "\b${term}\b" <<<"$stripped"; then
      # Allowlist: prediction-goat lineage notes are permitted in doc.go.tmpl
      # files (in comments — but those are already stripped, so a hit here is
      # in real code and remains a failure). The check below would only fire
      # if a non-comment line referenced the term in doc.go.tmpl, which is
      # still wrong.
      # No allowlist needed: full-line // comments are already stripped above,
      # so prediction_goat in doc.go.tmpl comments never reaches this branch.
      # Any hit here is in real code and must fail.
      echo "::error file=${rel}::learn template contains prohibited domain identifier '${term}'"
      echo "  See docs/plans/2026-05-23-002-feat-generator-wide-self-learning-cli-plan.md (Domain words prohibition)."
      fail=1
    fi
  done
done <<< "$templates_list"

if [[ "$fail" -ne 0 ]]; then
  echo
  echo "verify-learn-purity: FAILED — domain identifiers must not appear in learn-loop template code."
  echo "  Lineage notes in doc.go.tmpl comments are allowed (this script strips // comments before grepping)."
  exit 1
fi

echo "verify-learn-purity: ${template_count} learn template(s) passed."

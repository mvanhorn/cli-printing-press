// Package canonicalargs provides shared canonical mock values for positional
// placeholder names found in printed-CLI Usage strings. Both the verify
// pipeline (mock-mode dispatch) and the generator's SKILL template consult
// this registry so generated SKILL examples and verify invocations stay
// in sync.
//
// The registry is intentionally tiny and biased toward generic, cross-domain
// names. Domain-specific placeholder names (e.g., recipe `servings`,
// brokerage `ticker`, geo `airport_code`) belong in the spec author's
// `Param.Default` field, not here. AGENTS.md anti-pattern: "Never change
// the machine for one CLI's edge case."
package canonicalargs

import "strings"

// canonical maps lowercase placeholder names to their canonical mock value.
// Keep entries small in number and clearly cross-domain. If a name only
// makes sense for one product or industry, it does not belong here — the
// spec author should set Param.Default instead.
var canonical = map[string]string{
	// Time windows used by sync, list, and search endpoints across many
	// domains (changelog, audit log, listings, articles, tickets).
	"since": "2026-01-01",
	"from":  "2026-01-01",
	"until": "2026-12-31",
	"to":    "2026-12-31",

	// Filter dimensions common to taxonomy- or section-driven products
	// (tags on articles/recipes/issues; vertical on classifieds, news,
	// jobs, sports content).
	"tag":      "mock-tag",
	"vertical": "mock-vertical",
}

// Lookup returns the canonical mock value for a positional placeholder
// name, or ("", false) if the registry has no entry. Names are matched
// case-insensitively. Callers should always treat absence as a signal to
// fall through to the next step in the lookup chain (spec.Param.Default
// → canonicalargs → caller-specific switch → "mock-value").
func Lookup(name string) (string, bool) {
	v, ok := canonical[strings.ToLower(strings.TrimSpace(name))]
	return v, ok
}

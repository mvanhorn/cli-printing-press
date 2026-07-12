package paramnames

import (
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// Ident returns the name a Param should use when deriving Go identifiers
// or public CLI/MCP flag names. Wire-side serialization must still use the
// Param's WireName, BodyWireName, or Name fields as appropriate.
func Ident(p spec.Param) string {
	if p.IdentName != "" {
		return p.IdentName
	}
	return p.Name
}

// PublicFlagName returns the CLI/MCP flag name for a parameter, honoring an
// explicit flag_name before deriving one from Ident.
func PublicFlagName(p spec.Param) string {
	if p.FlagName != "" {
		return p.FlagName
	}
	return naming.FlagName(Ident(p))
}

// FlattenCollidingBodyFields returns body with Fields cleared on object params
// whose nested expansion would claim a Go identifier already used by another
// body leaf. Those parents then fall back to JSON-string flags in generated
// CLIs and manifest surfaces instead of producing divergent names.
func FlattenCollidingBodyFields(body []spec.Param) []spec.Param {
	counts := countBodyLeaves(body, "")
	for _, n := range counts {
		if n > 1 {
			return clearCollidingParents(body, "", counts)
		}
	}
	return body
}

func countBodyLeaves(params []spec.Param, prefix string) map[string]int {
	counts := map[string]int{}
	var walk func([]spec.Param, string)
	walk = func(ps []spec.Param, pfx string) {
		for _, p := range ps {
			ident := pfx + naming.CamelIdentifier(Ident(p))
			if p.Type == "object" && len(p.Fields) > 0 {
				walk(p.Fields, ident)
				continue
			}
			counts[ident]++
		}
	}
	walk(params, prefix)
	return counts
}

func clearCollidingParents(params []spec.Param, prefix string, counts map[string]int) []spec.Param {
	out := make([]spec.Param, len(params))
	copy(out, params)
	for i := range out {
		p := &out[i]
		if p.Type != "object" || len(p.Fields) == 0 {
			continue
		}
		ident := prefix + naming.CamelIdentifier(Ident(*p))
		if subtreeHasCollidingLeaf(p.Fields, ident, counts) {
			p.Fields = nil
			continue
		}
		p.Fields = clearCollidingParents(p.Fields, ident, counts)
	}
	return out
}

func subtreeHasCollidingLeaf(params []spec.Param, prefix string, counts map[string]int) bool {
	for _, p := range params {
		ident := prefix + naming.CamelIdentifier(Ident(p))
		if p.Type == "object" && len(p.Fields) > 0 {
			if subtreeHasCollidingLeaf(p.Fields, ident, counts) {
				return true
			}
			continue
		}
		if counts[ident] > 1 {
			return true
		}
	}
	return false
}

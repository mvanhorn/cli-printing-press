package generator

import (
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// flattenCollidingBodyFields returns body with Fields cleared on any
// object param whose nested expansion would produce a Go identifier
// already claimed by another leaf in the same body tree. The colliding
// parent then falls through to the JSON-string fallback in
// renderBodyMap, so the user can still reach the field via the parent
// flag as a JSON blob.
//
// Without this pass, two body properties whose camelCased prefix-paths
// converge on the same identifier — e.g. top-level `leadAccountId`
// alongside nested `lead.accountId` (Atlassian's ProjectComponent
// exposes exactly this pair) — emit two `var bodyLeadAccountId ...`
// declarations and the generated CLI fails to compile with
// "redeclared in this block".
//
// The check uses the same identifier-prediction rule as renderBodyMap
// and renderBodyVarDecls (`toCamel(paramIdent(p))` joined to the
// parent prefix) so detection and emission cannot drift.
func flattenCollidingBodyFields(body []spec.Param) []spec.Param {
	counts := countBodyLeaves(body, "")
	collision := false
	for _, n := range counts {
		if n > 1 {
			collision = true
			break
		}
	}
	if !collision {
		return body
	}
	return clearCollidingParents(body, "", counts)
}

func countBodyLeaves(params []spec.Param, prefix string) map[string]int {
	counts := map[string]int{}
	var walk func([]spec.Param, string)
	walk = func(ps []spec.Param, pfx string) {
		for _, p := range ps {
			ident := pfx + toCamel(paramIdent(p))
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
		ident := prefix + toCamel(paramIdent(*p))
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
		ident := prefix + toCamel(paramIdent(p))
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

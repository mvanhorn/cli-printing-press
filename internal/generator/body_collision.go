package generator

import (
	"github.com/mvanhorn/cli-printing-press/v4/internal/paramnames"
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
	return paramnames.FlattenCollidingBodyFields(body)
}

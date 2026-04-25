package generator

import (
	"sort"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
)

// firstCommandExample returns a runnable "resource [endpoint]" path for docs
// that need a concrete example. Read-only verbs (list, get, search, query)
// are preferred to keep examples non-destructive. Returns empty when the
// spec has no endpoints, so callers can skip the block rather than render
// nonsense.
//
// For single-endpoint resources that the generator promotes to top-level
// commands, the returned path is just the resource name (the actual cobra
// command path), not "resource endpoint" (the pre-promotion path). The
// SKILL.md verifier in printing-press-library walks command references and
// rejects pre-promotion paths because they don't exist in the shipped
// internal/cli/*.go.
func firstCommandExample(resources map[string]spec.Resource) string {
	var resNames []string
	for name := range resources {
		resNames = append(resNames, name)
	}
	sort.Strings(resNames)
	preferredVerbs := []string{"list", "get", "search", "query"}

	pathFor := func(rName string, r spec.Resource, eName string) string {
		if isPromotableSingleEndpoint(rName, r) {
			return rName
		}
		return rName + " " + eName
	}

	for _, rName := range resNames {
		r := resources[rName]
		for _, verb := range preferredVerbs {
			if _, ok := r.Endpoints[verb]; ok {
				return pathFor(rName, r, verb)
			}
		}
	}
	for _, rName := range resNames {
		r := resources[rName]
		eNames := sortedEndpointNames(r.Endpoints)
		if len(eNames) > 0 {
			return pathFor(rName, r, eNames[0])
		}
	}
	return ""
}

// isPromotableSingleEndpoint mirrors buildPromotedCommands's promotion
// criterion: a resource with exactly one endpoint whose derived command
// name does not collide with a CLI builtin (version, help, doctor, ...)
// gets promoted to a top-level command. The dedup-against-already-promoted
// step in buildPromotedCommands is multi-resource bookkeeping, not a
// per-resource property, so it is intentionally omitted here; this helper
// answers "would this resource standalone-promote?" not "does this
// resource end up promoted in this exact spec?".
func isPromotableSingleEndpoint(resName string, r spec.Resource) bool {
	if len(r.Endpoints) != 1 {
		return false
	}
	return !builtinCommands[toKebab(resName)]
}

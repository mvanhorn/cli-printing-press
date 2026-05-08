package generator

import (
	"fmt"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// dedupeTypeFieldIdentifiers ensures every TypeField in g.Spec.Types
// produces a Go identifier unique within its struct under toCamel. Without
// this, JSON keys differing only in leading non-alphanumeric punctuation
// (e.g. +1 and -1) emit two struct fields with the same Go name.
func (g *Generator) dedupeTypeFieldIdentifiers() {
	if g.Spec == nil || len(g.Spec.Types) == 0 {
		return
	}
	for typeName, td := range g.Spec.Types {
		td.Fields = uniquifyTypeFieldIdentifiers(td.Fields)
		g.Spec.Types[typeName] = td
	}
}

// uniquifyTypeFieldIdentifiers suffixes IdentName on later-occurring
// fields whose toCamel collides with an earlier one, leaving Name and
// thus the JSON tag unchanged. Caller must pass fields in deterministic
// order; suffixes are assigned in encounter order.
func uniquifyTypeFieldIdentifiers(fields []spec.TypeField) []spec.TypeField {
	if len(fields) == 0 {
		return fields
	}
	used := make(map[string]struct{}, len(fields))
	out := make([]spec.TypeField, len(fields))
	for i, f := range fields {
		f.IdentName = "" // ensure idempotence when callers reuse the same APISpec
		ident := toCamel(f.Name)
		if _, taken := used[ident]; !taken {
			used[ident] = struct{}{}
			out[i] = f
			continue
		}
		for n := 2; ; n++ {
			candidate := fmt.Sprintf("%s_%d", f.Name, n)
			candidateIdent := toCamel(candidate)
			if _, taken := used[candidateIdent]; !taken {
				f.IdentName = candidate
				used[candidateIdent] = struct{}{}
				out[i] = f
				break
			}
		}
	}
	return out
}

// typeFieldIdent mirrors paramIdent: prefer IdentName when the dedup
// pass populated it, otherwise fall back to Name. The result is for Go
// identifier derivation only; wire-side serialization (json tags, GraphQL
// selections) always reads Name directly.
func typeFieldIdent(f spec.TypeField) string {
	if f.IdentName != "" {
		return f.IdentName
	}
	return f.Name
}

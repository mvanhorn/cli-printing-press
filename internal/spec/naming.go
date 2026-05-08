package spec

import (
	"strings"
	"unicode"
)

// ToSnakeCase converts camelCase, PascalCase, or kebab-case to snake_case.
// Expects ASCII input — callers are SQL identifier paths whose inputs come
// from parsed APISpec types/fields that already passed through the openapi
// parser's ASCIIFold chokepoints.
func ToSnakeCase(s string) string {
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.ReplaceAll(s, "-", "_")

	var result strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			prev := rune(s[i-1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				result.WriteRune('_')
			} else if unicode.IsUpper(prev) && i+1 < len(s) && unicode.IsLower(rune(s[i+1])) {
				result.WriteRune('_')
			}
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}

// SubResourceShards is a lookup that flags sub-resource leaves needing
// parent-prefixed table names. Callers ask `IsSharded(leaf)` without
// knowing the underlying key shape (snake_case), which keeps the schema
// builder and the profiler from reimplementing the normalization.
type SubResourceShards struct {
	keys map[string]bool
}

// IsSharded reports whether the given leaf name requires parent-prefixed
// table naming. Snake-cases internally so camelCase, kebab-case, and
// already-snake-cased inputs all resolve to the same canonical key.
func (s SubResourceShards) IsSharded(leaf string) bool {
	return s.keys[ToSnakeCase(leaf)]
}

// SubResourceShardedNames returns the lookup for sub-resource leaves that
// require parent-prefixed table naming. A leaf shards when it appears
// under multiple parents OR when it collides with a top-level resource of
// the same name (e.g. a top-level "loginEvents" collides with a
// sub-resource "login_events").
func SubResourceShardedNames(s *APISpec) SubResourceShards {
	if s == nil {
		return SubResourceShards{}
	}
	parents := make(map[string]map[string]bool)
	topLevelKeys := make(map[string]bool, len(s.Resources))
	for parentName, parent := range s.Resources {
		topLevelKeys[ToSnakeCase(parentName)] = true
		for subName := range parent.SubResources {
			key := ToSnakeCase(subName)
			if parents[key] == nil {
				parents[key] = make(map[string]bool)
			}
			parents[key][ToSnakeCase(parentName)] = true
		}
	}
	shards := make(map[string]bool, len(parents))
	for subKey, parentSet := range parents {
		if len(parentSet) > 1 || topLevelKeys[subKey] {
			shards[subKey] = true
		}
	}
	return SubResourceShards{keys: shards}
}

// ShardedSubResourceTableName returns the snake-cased per-parent table name
// for a sub-resource that requires sharding. Both the profiler (when emitting
// DependentResource.Name) and the schema builder (when emitting the table)
// must call this so the names line up exactly.
func ShardedSubResourceTableName(parent, leaf string) string {
	return ToSnakeCase(parent + "_" + leaf)
}

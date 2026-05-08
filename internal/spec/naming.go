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

// SubResourceShardedNames returns the set of sub-resource leaf names that
// require parent-prefixed table naming. A leaf shards when it appears under
// multiple parents OR when it collides with a top-level resource of the same
// name. Keys are snake-cased so callers that have already normalized their
// candidate (the profiler lowercases, the schema builder snake-cases) all
// see the same shape. Top-level collisions match by snake-cased identity
// too — a top-level resource named "loginEvents" collides with a sub-resource
// named "login_events".
func SubResourceShardedNames(s *APISpec) map[string]bool {
	if s == nil {
		return nil
	}
	parents := make(map[string]map[string]bool)
	for parentName, parent := range s.Resources {
		for subName := range parent.SubResources {
			key := ToSnakeCase(subName)
			if parents[key] == nil {
				parents[key] = make(map[string]bool)
			}
			parents[key][ToSnakeCase(parentName)] = true
		}
	}
	topLevelKeys := make(map[string]bool, len(s.Resources))
	for name := range s.Resources {
		topLevelKeys[ToSnakeCase(name)] = true
	}
	shards := make(map[string]bool, len(parents))
	for subKey, parentSet := range parents {
		if len(parentSet) > 1 {
			shards[subKey] = true
			continue
		}
		if topLevelKeys[subKey] {
			shards[subKey] = true
		}
	}
	return shards
}

// ShardedSubResourceTableName returns the snake-cased per-parent table name
// for a sub-resource that requires sharding. Both the profiler (when emitting
// DependentResource.Name) and the schema builder (when emitting the table)
// must call this so the names line up exactly.
func ShardedSubResourceTableName(parent, leaf string) string {
	return ToSnakeCase(parent + "_" + leaf)
}

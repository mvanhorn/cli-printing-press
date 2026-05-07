package generator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v3/internal/canonicalargs"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
)

// firstCommandExample returns a runnable "resource [endpoint] <pos1> <pos2>..."
// invocation for docs that need a concrete example. Required public flags are
// included so generated docs do not advertise commands that fail immediately.
// Read-only verbs (list, get, search, query) are preferred to keep examples
// non-destructive.
// Returns empty when the spec has no endpoints, so callers can skip the
// block rather than render nonsense.
//
// For single-endpoint resources that the generator promotes to top-level
// commands, the returned path starts with just the resource name (the
// actual cobra command path), not "resource endpoint" (the pre-promotion
// path). The SKILL.md verifier in printing-press-library walks command
// references and rejects pre-promotion paths because they don't exist in
// the shipped internal/cli/*.go.
//
// Positional values use the same lookup chain as verify mock-mode in
// runtime_commands.go: spec.Param.Default → canonicalargs.Lookup →
// "mock-value". Spec authors who set realistic defaults on positional
// params get them surfaced in the SKILL example automatically; specs
// without defaults fall through to the cross-domain registry, then to
// the mock-value catch-all. This keeps SKILL examples honest enough that
// verify-skill exits 0 on first generation.
func firstCommandExample(resources map[string]spec.Resource) string {
	var resNames []string
	for name := range resources {
		resNames = append(resNames, name)
	}
	sort.Strings(resNames)
	preferredVerbs := []string{"list", "get", "search", "query"}

	pathFor := func(rName string, r spec.Resource, eName string, ep spec.Endpoint) string {
		parts := []string{rName}
		if !isPromotableSingleEndpoint(rName, r) {
			parts = append(parts, eName)
		}
		parts = append(parts, readmeExampleArgs(ep)...)
		return strings.Join(parts, " ")
	}

	for _, rName := range resNames {
		r := resources[rName]
		for _, verb := range preferredVerbs {
			if ep, ok := r.Endpoints[verb]; ok {
				return pathFor(rName, r, verb, ep)
			}
		}
	}
	for _, rName := range resNames {
		r := resources[rName]
		eNames := sortedEndpointNames(r.Endpoints)
		if len(eNames) > 0 {
			return pathFor(rName, r, eNames[0], r.Endpoints[eNames[0]])
		}
	}
	return ""
}

func commandExampleArgs(ep spec.Endpoint) string {
	return strings.Join(commandExampleArgParts(ep), " ")
}

func commandExampleArgParts(ep spec.Endpoint) []string {
	var parts []string
	for _, p := range ep.Params {
		if !p.Positional {
			continue
		}
		val := exampleValue(p)
		if val == "" {
			val = "<" + p.Name + ">"
		}
		parts = append(parts, val)
	}
	return append(parts, requiredFlagExampleParts(ep)...)
}

func readmeExampleArgs(ep spec.Endpoint) []string {
	var parts []string
	for _, p := range ep.Params {
		if p.Positional {
			parts = append(parts, skillExamplePositionalValue(p))
		}
	}
	return append(parts, requiredFlagExampleParts(ep)...)
}

func requiredFlagExampleParts(ep spec.Endpoint) []string {
	var parts []string
	for _, p := range ep.Params {
		if p.Positional || !p.Required {
			continue
		}
		val := exampleValue(p)
		if val == "" {
			val = "value"
		}
		parts = append(parts, "--"+publicFlagName(p), val)
	}

	switch strings.ToUpper(ep.Method) {
	case "POST", "PUT", "PATCH":
		for _, p := range ep.Body {
			if p.Required && p.Type == "string" {
				val := exampleValue(p)
				if val == "" {
					val = "value"
				}
				parts = append(parts, "--"+publicFlagName(p), val)
				break
			}
		}
	}
	return parts
}

// skillExamplePositionalValue resolves one positional param to the value
// the SKILL/README example should display. Mirrors the verify mock-mode
// lookup chain in internal/pipeline/runtime_commands.go so a spec's
// Param.Default flows through to both verify dispatch and the docs the
// generator emits.
func skillExamplePositionalValue(p spec.Param) string {
	if p.Default != nil {
		if s := stringifyDefault(p.Default); s != "" {
			return s
		}
	}
	name := strings.ToLower(strings.TrimSpace(p.Name))
	if v, ok := canonicalargs.Lookup(name); ok {
		return v
	}
	return "mock-value"
}

func stringifyDefault(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
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

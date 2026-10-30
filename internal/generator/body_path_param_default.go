package generator

import (
	"fmt"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// bodyPathParamDefaults fills an unset body leaf from the positional path
// argument of the same name. Callers emit it immediately before
// bodyRequiredChecks, which still runs before replacePathParam. The match key
// is naming.FlagName, so parentRef and parent_ref match while a nested flag
// prefix such as child-parent-ref does not. Ambiguous keys and non-string
// leaves are skipped; an explicit flag is left unchanged.
func bodyPathParamDefaults(endpoint spec.Endpoint, indent string) string {
	if endpoint.BodyJSONFallback || len(endpoint.Body) == 0 {
		return ""
	}
	sources := positionalPathArgSources(endpoint)
	if len(sources) == 0 {
		return ""
	}
	body := endpoint.Body
	expand := true
	if bodyUsesFlatEmission(endpoint) {
		expand = false
	} else {
		body = flattenCollidingBodyFields(body)
	}
	var b strings.Builder
	writeBodyPathParamDefaults(&b, body, sources, 0, expand, indent, "", "")
	return b.String()
}

// positionalPathArgSources maps a normalized path-parameter name to the
// args index orderedPositionalParams assigns it. Non-path positionals keep
// their slots but are not sources. A repeated normalized key is ambiguous
// and is dropped.
func positionalPathArgSources(endpoint spec.Endpoint) map[string]int {
	sources := make(map[string]int)
	ambiguous := make(map[string]struct{})
	for i, p := range orderedPositionalParams(endpoint) {
		if !strings.Contains(endpoint.Path, "{"+p.Name+"}") {
			continue
		}
		key := pathBodyMatchKey(p.Name)
		if key == "" {
			continue
		}
		if _, seen := sources[key]; seen {
			ambiguous[key] = struct{}{}
			continue
		}
		sources[key] = i
	}
	for key := range ambiguous {
		delete(sources, key)
	}
	return sources
}

func pathBodyMatchKey(name string) string {
	return naming.FlagName(strings.TrimSpace(name))
}

func writeBodyPathParamDefaults(b *strings.Builder, body []spec.Param, sources map[string]int, depth int, expand bool, indent, flagPrefix, identPrefix string) {
	for _, p := range body {
		if expand && p.Type == "object" && len(p.Fields) > 0 && depth+1 < maxBodyFlagDepth {
			childIdent := identPrefix + toCamel(paramIdent(p))
			childFlag := joinFlag(flagPrefix, publicFlagName(p))
			writeBodyPathParamDefaults(b, p.Fields, sources, depth+1, expand, indent, childFlag, childIdent)
			continue
		}
		if !bodyLeafAcceptsPathDefault(p) {
			continue
		}
		key := pathBodyMatchKey(p.BodyWireName())
		idx, ok := sources[key]
		if !ok {
			continue
		}
		ident := identPrefix + toCamel(paramIdent(p))
		flag := joinFlag(flagPrefix, publicFlagName(p))
		changed := bodyFlagChangedExpr(p, flag)
		fmt.Fprintf(b, "\n%sif len(args) > %d && args[%d] != \"\" && !%s {", indent, idx, idx, changed)
		fmt.Fprintf(b, "\n%s\tbody%s = args[%d]", indent, ident, idx)
		fmt.Fprintf(b, "\n%s}", indent)
	}
}

// bodyLeafAcceptsPathDefault reports whether a leaf variable is a plain
// string the positional text can be copied into. JSON-object flags, file
// parts, and CSV lists stay untouched: copying a path segment into them
// would fail their own parsers or send a string the API rejects.
func bodyLeafAcceptsPathDefault(p spec.Param) bool {
	if isComplexBodyField(p) || isJSONStringParam(p) || isBinaryParam(p) || isStringCSVArrayParam(p) || isStringBackedBoolParam(p) {
		return false
	}
	return primitiveKind(p.Type) == "string" && goTypeForBodyParam(p) == "string"
}

package spec

import (
	"regexp"
)

var (
	topLevelYAMLKeyRE = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_-]*)\s*:`)
	// GraphQL fields may be written unindented (`name: String`), so a bare
	// key scan is not enough. An internal spec names the API with a scalar
	// and nests resources as a YAML mapping (block key or nonempty flow
	// `{payments:`). An indented `}` after `resources: [Type]!` is SDL.
	yamlNameScalarRE   = regexp.MustCompile(`(?m)^name:\s+(?:"[^"]+"|'[^']+'|[A-Za-z][A-Za-z0-9._-]*)\s*$`)
	yamlResourcesMapRE = regexp.MustCompile(`(?m)^resources:\s*(?:\{[ \t]*[A-Za-z_]|(?:#.*)?\n[ \t]+[A-Za-z_][A-Za-z0-9_-]*\s*:)`)
)

// LooksLikeInternalYAML reports whether data is an authored internal YAML spec
// rather than OpenAPI or GraphQL SDL. Description prose is ignored because
// only top-level YAML structure is scanned.
func LooksLikeInternalYAML(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	keys := topLevelYAMLKeys(data)
	if keys["openapi"] || keys["swagger"] {
		return false
	}
	if !keys["name"] || !keys["resources"] {
		return false
	}
	return yamlNameScalarRE.Match(data) && yamlResourcesMapRE.Match(data)
}

func topLevelYAMLKeys(data []byte) map[string]bool {
	found := make(map[string]bool)
	for _, m := range topLevelYAMLKeyRE.FindAllSubmatch(data, -1) {
		found[string(m[1])] = true
	}
	return found
}

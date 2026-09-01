package spec

import (
	"regexp"
)

var topLevelYAMLKeyRE = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_-]*)\s*:`)

// LooksLikeInternalYAML reports whether data is an authored internal YAML spec
// (unindented name: and resources:) rather than OpenAPI or GraphQL SDL.
// Description prose is ignored because only top-level keys are scanned.
func LooksLikeInternalYAML(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	keys := topLevelYAMLKeys(data)
	if keys["openapi"] || keys["swagger"] {
		return false
	}
	return keys["name"] && keys["resources"]
}

func topLevelYAMLKeys(data []byte) map[string]bool {
	found := make(map[string]bool)
	for _, m := range topLevelYAMLKeyRE.FindAllSubmatch(data, -1) {
		found[string(m[1])] = true
	}
	return found
}

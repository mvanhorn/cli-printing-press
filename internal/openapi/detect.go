package openapi

import (
	"bytes"

	"github.com/mvanhorn/cli-printing-press/v3/internal/graphql"
)

func IsOpenAPI(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// Check for JSON-style keys (case-sensitive, covers 99% of specs)
	if bytes.Contains(data, []byte(`"openapi"`)) ||
		bytes.Contains(data, []byte(`"swagger"`)) {
		return true
	}

	// Check for YAML-style keys
	if bytes.Contains(data, []byte("openapi:")) ||
		bytes.Contains(data, []byte("swagger:")) {
		return true
	}

	return false
}

func IsGraphQLSDL(data []byte) bool {
	return graphql.IsGraphQLSDL(data)
}

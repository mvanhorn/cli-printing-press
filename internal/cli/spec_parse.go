package cli

import (
	"github.com/mvanhorn/cli-printing-press/v4/internal/googlediscovery"
	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// parseSpecBytes routes spec bytes to the parser for the format the author
// supplied. Internal YAML and OpenAPI win over GraphQL SDL detection so a
// failed structured-spec parse is reported as that failure, not as a
// GraphQL root-type miss triggered by description prose.
func parseSpecBytes(specFile string, data []byte, opts openapi.ParseOptions) (*spec.APISpec, error) {
	if openapi.IsOpenAPI(data) {
		return parseOpenAPISpec(specFile, data, opts)
	}
	if spec.LooksLikeInternalYAML(data) {
		return spec.ParseBytes(data)
	}
	if graphql.IsGraphQLSDL(data) {
		return graphql.ParseSDLBytes(specFile, data)
	}
	if googlediscovery.IsDiscovery(data) {
		return googlediscovery.Parse(specFile, data)
	}
	return spec.ParseBytes(data)
}

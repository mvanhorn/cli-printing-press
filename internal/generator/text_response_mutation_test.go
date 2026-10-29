package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Non-GET endpoint commands bind statusCode; the text-response branch prints
// the body and must still compile (it previously failed with "declared and
// not used" for any POST/PUT/DELETE endpoint returning text).
func TestTextResponseMutationCommandsCompile(t *testing.T) {
	t.Parallel()

	textAccept := []spec.RequiredHeader{{Name: "Accept", Value: "text/plain"}}
	apiSpec := minimalSpec("textmut")
	apiSpec.Resources = map[string]spec.Resource{
		"docs": {
			Description: "Search and ask the docs",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/docs",
					Description: "List docs",
				},
				"ask": {
					Method:             "POST",
					Path:               "/docs/ask",
					Description:        "Ask a question and get a text answer",
					RequestContentType: "application/json",
					BodyRequired:       true,
					Body: []spec.Param{{
						Name:        "question",
						Type:        "string",
						Required:    true,
						Description: "The question",
					}},
					ResponseFormat:  spec.ResponseFormatText,
					HeaderOverrides: textAccept,
				},
				"replace": {
					Method:             "PUT",
					Path:               "/docs/current",
					Description:        "Replace the current doc and echo it as text",
					RequestContentType: "application/json",
					BodyRequired:       true,
					Body: []spec.Param{{
						Name:        "content",
						Type:        "string",
						Required:    true,
						Description: "Doc content",
					}},
					ResponseFormat:  spec.ResponseFormatText,
					HeaderOverrides: textAccept,
				},
				"purge": {
					Method:          "DELETE",
					Path:            "/docs/cache",
					Description:     "Purge the docs cache and return a text report",
					ResponseFormat:  spec.ResponseFormatText,
					HeaderOverrides: textAccept,
				},
			},
		},
	}
	apiSpec.Learn.Disabled = true

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	ask := readGeneratedFile(t, outputDir, "internal", "cli", "docs_ask.go")
	assert.Contains(t, ask, "data, statusCode, err :=")
	assert.Contains(t, ask, "_ = statusCode")
	assert.Contains(t, ask, "textEnvelope.Body")

	requireGeneratedCompiles(t, outputDir)
}

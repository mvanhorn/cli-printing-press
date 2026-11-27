package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileFromHARProducesHostLockedCartridge(t *testing.T) {
	tmp := t.TempDir()
	harPath := filepath.Join(tmp, "sample.har")

	harJSON := `{
  "log": {
    "entries": [
      {
        "request": {
          "method": "GET",
          "url": "https://api.flights.example/v1/deals?origin=SEA",
          "headers": [{"name": "Accept", "value": "application/json"}]
        },
        "response": {
          "status": 200,
          "headers": [{"name": "Content-Type", "value": "application/json"}],
          "content": {
            "mimeType": "application/json",
            "size": 92,
            "text": "{\"deals\":[{\"id\":\"d1\",\"dest\":\"NRT\",\"price\":640}]}"
          }
        }
      },
      {
        "request": {
          "method": "GET",
          "url": "https://cdn.analytics.example/pixel.gif",
          "headers": []
        },
        "response": {
          "status": 200,
          "headers": [{"name": "Content-Type", "value": "image/gif"}],
          "content": {"mimeType": "image/gif", "size": 10, "text": ""}
        }
      }
    ]
  }
}`
	require.NoError(t, os.WriteFile(harPath, []byte(harJSON), 0o600))

	res, err := CompileFromHAR(harPath, "flight-deals")
	require.NoError(t, err)
	require.NotNil(t, res.Cartridge)
	assert.Equal(t, "flight-deals", res.Cartridge.Name)
	assert.GreaterOrEqual(t, res.CommandCount, 1)
	assert.Contains(t, res.AllowedHosts, "api.flights.example")
	assert.NotContains(t, res.AllowedHosts, "cdn.analytics.example")
	assert.Contains(t, res.SkillMD, "pp run flight-deals")
	assert.NoError(t, res.Cartridge.Validate())

	// Verify HAR query parameter (`origin`) is wired into Query map.
	foundOriginQuery := false
	for _, cmd := range res.Cartridge.Commands {
		if cmd.Query["origin"] == "{origin}" {
			foundOriginQuery = true
		}
	}
	assert.True(t, foundOriginQuery, "HAR query param origin must be mapped into CommandSpec.Query")
}

func TestCompileSpecPreservesBodyAndParamDefaults(t *testing.T) {
	tmp := t.TempDir()
	specPath := filepath.Join(tmp, "api.yaml")
	specYAML := `name: widget-api
base_url: https://api.widget.example
resources:
  widgets:
    endpoints:
      create:
        method: POST
        path: /v1/widgets
        params:
          - name: region
            in: query
            type: string
            required: true
            default: us-west
        body:
          - name: title
            type: string
            required: true
          - name: count
            type: integer
            default: 3
        response:
          type: object
`
	require.NoError(t, os.WriteFile(specPath, []byte(specYAML), 0o600))
	res, err := CompileFromSpecFile(specPath, "")
	require.NoError(t, err)
	cmd, ok := res.Cartridge.Commands["create"]
	require.True(t, ok)
	assert.Equal(t, "{region}", cmd.Query["region"])
	assert.Equal(t, "us-west", cmd.Params["region"].Default)
	assert.False(t, cmd.Params["region"].Required, "param with spec default should not force required input")
	assert.Equal(t, "3", cmd.Params["count"].Default)
	assert.Contains(t, cmd.Body, `"title":"{title}"`)
	assert.Contains(t, cmd.Body, `"count":"{count}"`)
}

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
}

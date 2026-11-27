package compiler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertBrowseNetworkToHARPostBodyAndEmptyRejection(t *testing.T) {
	raw := []byte(`{
  "exchanges": [
    {
      "url": "https://api.shop.example/v1/search",
      "method": "POST",
      "status": 200,
      "requestHeaders": {
        "Content-Type": "application/json",
        "Authorization": "[redacted]"
      },
      "requestBody": "{\"q\":\"milk\"}",
      "responseHeaders": {
        "Content-Type": "application/json"
      },
      "responseBody": {
        "mimeType": "application/json",
        "text": "{\"items\":[{\"id\":\"1\",\"name\":\"Organic Milk\"}]}"
      }
    }
  ]
}`)
	harBytes, err := convertBrowseNetworkToHAR(raw)
	require.NoError(t, err)

	var doc struct {
		Log struct {
			Version string `json:"version"`
			Entries []struct {
				Request struct {
					Method   string `json:"method"`
					URL      string `json:"url"`
					PostData struct {
						MimeType string `json:"mimeType"`
						Text     string `json:"text"`
					} `json:"postData"`
				} `json:"request"`
				Response struct {
					Status  int `json:"status"`
					Content struct {
						MimeType string `json:"mimeType"`
						Text     string `json:"text"`
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}
	require.NoError(t, json.Unmarshal(harBytes, &doc))
	assert.Equal(t, "1.2", doc.Log.Version)
	require.Len(t, doc.Log.Entries, 1)
	assert.Equal(t, "POST", doc.Log.Entries[0].Request.Method)
	assert.Equal(t, `{"q":"milk"}`, doc.Log.Entries[0].Request.PostData.Text)
	assert.Equal(t, 200, doc.Log.Entries[0].Response.Status)
	assert.Contains(t, doc.Log.Entries[0].Response.Content.Text, "Organic Milk")

	// Empty exchanges list must return an explicit error.
	_, err = convertBrowseNetworkToHAR([]byte(`{"exchanges":[]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zero exchanges")
}

func TestRecordWithOpenCodeBrowseHappyPathAndCancelCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell mock")
	}
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	mockBrowse := filepath.Join(tmp, "browse")

	script := `#!/bin/sh
echo "$*" >> "` + logPath + `"
if [ "$1" = "network" ] && echo "$*" | grep -q '"action":"stop"'; then
  printf '{"exchanges":[{"id":"request-1","request":{"method":"GET","url":"https://api.chrome-demo.test/v1/items","headers":[{"name":"accept","value":"application/json"}]},"response":{"status":200,"headers":[{"name":"content-type","value":"application/json"}],"body":{"mimeType":"application/json","size":42,"text":"{\\"items\\":[{\\"id\\":\\"a1\\",\\"title\\":\\"Alpha\\"}]}"}}}]}\n'
  exit 0
fi
exit 0
`
	require.NoError(t, os.WriteFile(mockBrowse, []byte(script), 0o755))

	// 1. Happy path compiles a valid host-locked Cartridge from nested browse v0.1.5 output.
	res, err := RecordWithOpenCodeBrowse(context.Background(), BrowseRecordOptions{
		TargetURL:    "https://chrome-demo.test",
		NameOverride: "chrome-demo",
		WaitSeconds:  1,
		BrowseBin:    mockBrowse,
	})
	require.NoError(t, err)
	assert.Equal(t, "chrome-demo", res.Cartridge.Name)
	assert.Equal(t, "browse:https://chrome-demo.test", res.Source)
	assert.GreaterOrEqual(t, res.CommandCount, 1)

	// 2. Cancelled context during wait must STILL run `network cancel`
	// and `tabs close` via the detached cleanup context.
	require.NoError(t, os.WriteFile(logPath, nil, 0o600))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	_, err = RecordWithOpenCodeBrowse(ctx, BrowseRecordOptions{
		TargetURL:    "https://chrome-demo.test",
		NameOverride: "chrome-cancel",
		WaitSeconds:  10,
		BrowseBin:    mockBrowse,
	})
	require.Error(t, err)

	loggedBytes, readErr := os.ReadFile(logPath)
	require.NoError(t, readErr)
	logged := string(loggedBytes)
	assert.True(t, strings.Contains(logged, `"action":"cancel"`), "expected detached network cancel on ctx cancel, got:\n%s", logged)
	assert.True(t, strings.Contains(logged, `"action":"close"`), "expected detached tabs close on ctx cancel, got:\n%s", logged)
}

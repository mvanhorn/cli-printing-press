package browsersniff

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHAR(t *testing.T) {
	t.Parallel()

	har, err := ParseHAR(filepath.Join("..", "..", "testdata", "sniff", "sample.har"))
	require.NoError(t, err)

	assert.Len(t, har.Log.Entries, 5)
	assert.Equal(t, "GET", har.Log.Entries[1].Request.Method)
	assert.Equal(t, "https://httpbin.org/get", har.Log.Entries[1].Request.URL)
}

func TestParseEnriched(t *testing.T) {
	t.Parallel()

	capture, err := ParseEnriched(filepath.Join("..", "..", "testdata", "sniff", "sample-enriched.json"))
	require.NoError(t, err)

	assert.Len(t, capture.Entries, 3)
	assert.NotEmpty(t, capture.Entries[0].ResponseBody)
	assert.Equal(t, "https://hn.algolia.com", capture.TargetURL)
}

func TestParseCapture(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		path           string
		wantEntries    int
		wantTargetURL  string
		wantFirstURL   string
		wantFirstBody  string
		wantStatusCode int
	}{
		{
			name:           "auto-detects har format",
			path:           filepath.Join("..", "..", "testdata", "sniff", "sample.har"),
			wantEntries:    5,
			wantTargetURL:  "data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjAwcHgiICBoZWlnaHQ9IjIwMHB4IiAgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIiB2aWV3Qm94PSIwIDAgMTAwIDEwMCIgcHJlc2VydmVBc3BlY3RSYXRpbz0ieE1pZFlNaWQiIGNsYXNzPSJsZHMtcm9sbGluZyIgc3R5bGU9ImJhY2tncm91bmQtaW1hZ2U6IG5vbmU7IGJhY2tncm91bmQtcG9zaXRpb246IGluaXRpYWwgaW5pdGlhbDsgYmFja2dyb3VuZC1yZXBlYXQ6IGluaXRpYWwgaW5pdGlhbDsiPjxjaXJjbGUgY3g9IjUwIiBjeT0iNTAiIGZpbGw9Im5vbmUiIG5nLWF0dHItc3Ryb2tlPSJ7e2NvbmZpZy5jb2xvcn19IiBuZy1hdHRyLXN0cm9rZS13aWR0aD0ie3tjb25maWcud2lkdGh9fSIgbmctYXR0ci1yPSJ7e2NvbmZpZy5yYWRpdXN9fSIgbmctYXR0ci1zdHJva2UtZGFzaGFycmF5PSJ7e2NvbmZpZy5kYXNoYXJyYXl9fSIgc3Ryb2tlPSIjNTU1NTU1IiBzdHJva2Utd2lkdGg9IjEwIiByPSIzNSIgc3Ryb2tlLWRhc2hhcnJheT0iMTY0LjkzMzYxNDMxMzQ2NDE1IDU2Ljk3Nzg3MTQzNzgyMTM4Ij48YW5pbWF0ZVRyYW5zZm9ybSBhdHRyaWJ1dGVOYW1lPSJ0cmFuc2Zvcm0iIHR5cGU9InJvdGF0ZSIgY2FsY01vZGU9ImxpbmVhciIgdmFsdWVzPSIwIDUwIDUwOzM2MCA1MCA1MCIga2V5VGltZXM9IjA7MSIgZHVyPSIxcyIgYmVnaW49IjBzIiByZXBlYXRDb3VudD0iaW5kZWZpbml0ZSI+PC9hbmltYXRlVHJhbnNmb3JtPjwvY2lyY2xlPjwvc3ZnPgo=",
			wantFirstURL:   "data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjAwcHgiICBoZWlnaHQ9IjIwMHB4IiAgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIiB2aWV3Qm94PSIwIDAgMTAwIDEwMCIgcHJlc2VydmVBc3BlY3RSYXRpbz0ieE1pZFlNaWQiIGNsYXNzPSJsZHMtcm9sbGluZyIgc3R5bGU9ImJhY2tncm91bmQtaW1hZ2U6IG5vbmU7IGJhY2tncm91bmQtcG9zaXRpb246IGluaXRpYWwgaW5pdGlhbDsgYmFja2dyb3VuZC1yZXBlYXQ6IGluaXRpYWwgaW5pdGlhbDsiPjxjaXJjbGUgY3g9IjUwIiBjeT0iNTAiIGZpbGw9Im5vbmUiIG5nLWF0dHItc3Ryb2tlPSJ7e2NvbmZpZy5jb2xvcn19IiBuZy1hdHRyLXN0cm9rZS13aWR0aD0ie3tjb25maWcud2lkdGh9fSIgbmctYXR0ci1yPSJ7e2NvbmZpZy5yYWRpdXN9fSIgbmctYXR0ci1zdHJva2UtZGFzaGFycmF5PSJ7e2NvbmZpZy5kYXNoYXJyYXl9fSIgc3Ryb2tlPSIjNTU1NTU1IiBzdHJva2Utd2lkdGg9IjEwIiByPSIzNSIgc3Ryb2tlLWRhc2hhcnJheT0iMTY0LjkzMzYxNDMxMzQ2NDE1IDU2Ljk3Nzg3MTQzNzgyMTM4Ij48YW5pbWF0ZVRyYW5zZm9ybSBhdHRyaWJ1dGVOYW1lPSJ0cmFuc2Zvcm0iIHR5cGU9InJvdGF0ZSIgY2FsY01vZGU9ImxpbmVhciIgdmFsdWVzPSIwIDUwIDUwOzM2MCA1MCA1MCIga2V5VGltZXM9IjA7MSIgZHVyPSIxcyIgYmVnaW49IjBzIiByZXBlYXRDb3VudD0iaW5kZWZpbml0ZSI+PC9hbmltYXRlVHJhbnNmb3JtPjwvY2lyY2xlPjwvc3ZnPgo=",
			wantStatusCode: 200,
		},
		{
			name:           "auto-detects enriched format",
			path:           filepath.Join("..", "..", "testdata", "sniff", "sample-enriched.json"),
			wantEntries:    3,
			wantTargetURL:  "https://hn.algolia.com",
			wantFirstURL:   "https://uj5wyc0l7x-dsn.algolia.net/1/indexes/Item_dev/query?x-algolia-api-key=28f0e1ec37a5e792e6845e67da5f20dd&x-algolia-application-id=UJ5WYC0L7X",
			wantFirstBody:  "{\"hits\": [{\"objectID\": \"21711748\", \"title\": \"Troubleshooting K8s\", \"url\": \"https://example.com\", \"author\": \"test\", \"points\": 240, \"num_comments\": 53, \"created_at\": \"2019-12-05T12:22:28Z\", \"created_at_i\": 1575548548}, {\"objectID\": \"21711749\", \"title\": \"K8s Best Practices\", \"url\": \"https://example2.com\", \"author\": \"test2\", \"points\": 180, \"num_comments\": 30, \"created_at\": \"2020-01-10T08:00:00Z\", \"created_at_i\": 1578643200}], \"nbHits\": 4804, \"page\": 0, \"nbPages\": 334, \"hitsPerPage\": 3, \"processingTimeMS\": 16, \"query\": \"kubernetes\"}",
			wantStatusCode: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, targetURL, err := ParseCapture(tt.path)
			require.NoError(t, err)

			assert.Len(t, entries, tt.wantEntries)
			assert.Equal(t, tt.wantTargetURL, targetURL)
			assert.Equal(t, tt.wantFirstURL, entries[0].URL)
			assert.Equal(t, tt.wantFirstBody, entries[0].ResponseBody)
			assert.Equal(t, tt.wantStatusCode, entries[0].ResponseStatus)
		})
	}
}

func TestParseCaptureHARUsesRequestPostDataText(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "capture.har")
	err := os.WriteFile(path, []byte(`{
		"log": {
			"entries": [
				{
					"request": {
						"method": "POST",
						"url": "https://api.example.com/generate",
						"headers": [{"name": "content-type", "value": "application/json"}],
						"postData": {
							"mimeType": "application/json",
							"text": "{\"prompt\":\"donkey love\",\"gpt_description_prompt\":\"make a song\"}"
						}
					},
					"response": {
						"status": 200,
						"headers": [{"name": "content-type", "value": "application/json"}],
						"content": {
							"mimeType": "application/json",
							"text": "{\"metadata\":{\"prompt\":\"response-only shape\"}}"
						}
					}
				}
			]
		}
	}`), 0o600)
	require.NoError(t, err)

	entries, targetURL, err := ParseCapture(path)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, "https://api.example.com/generate", targetURL)
	assert.Equal(t, "POST", entries[0].Method)
	assert.JSONEq(t, `{"prompt":"donkey love","gpt_description_prompt":"make a song"}`, entries[0].RequestBody)
	assert.JSONEq(t, `{"metadata":{"prompt":"response-only shape"}}`, entries[0].ResponseBody)
}

func TestParseHAR_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		path        string
		parse       func(string) (*HAR, error)
		assertError func(*testing.T, *HAR, error)
	}{
		{
			name:    "empty har",
			content: `{"log":{"entries":[]}}`,
			parse:   ParseHAR,
			assertError: func(t *testing.T, har *HAR, err error) {
				require.NoError(t, err)
				assert.NotNil(t, har)
				assert.Len(t, har.Log.Entries, 0)
			},
		},
		{
			name:    "invalid json",
			content: `{`,
			parse:   ParseHAR,
			assertError: func(t *testing.T, har *HAR, err error) {
				require.Error(t, err)
				assert.Nil(t, har)
			},
		},
		{
			name:  "missing file",
			path:  filepath.Join(t.TempDir(), "missing.har"),
			parse: ParseHAR,
			assertError: func(t *testing.T, har *HAR, err error) {
				require.Error(t, err)
				assert.Nil(t, har)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if tt.content != "" {
				path = filepath.Join(t.TempDir(), "capture.har")
				err := os.WriteFile(path, []byte(tt.content), 0o600)
				require.NoError(t, err)
			}

			har, err := tt.parse(path)
			tt.assertError(t, har, err)
		})
	}
}

func TestParseCapture_InvalidJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "capture.json")
	err := os.WriteFile(path, []byte(`{`), 0o600)
	require.NoError(t, err)

	entries, targetURL, err := ParseCapture(path)
	require.Error(t, err)
	assert.Nil(t, entries)
	assert.Empty(t, targetURL)
}

func TestParseCapture_MissingFile(t *testing.T) {
	t.Parallel()

	entries, targetURL, err := ParseCapture(filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)
	assert.Nil(t, entries)
	assert.Empty(t, targetURL)
}

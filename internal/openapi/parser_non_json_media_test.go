package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePreservesRawRequestAndTextResponseMediaTypes(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Media API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /uploads:
    post:
      operationId: uploadAudio
      parameters:
        - name: upload_id
          in: query
          required: true
          schema: { type: string }
      requestBody:
        required: true
        content:
          application/octet-stream:
            schema:
              type: string
              format: binary
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: { type: object }
  /transcripts/{id}/srt:
    get:
      operationId: getTranscriptSRT
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200":
          description: captions
          content:
            text/plain:
              schema: { type: string }
  /widgets:
    post:
      operationId: createWidget
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              properties:
                name: { type: string }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: { type: object }
  /blobs:
    put:
      operationId: replaceBlob
      requestBody:
        required: true
        content:
          application/octet-stream: {}
      responses:
        "204":
          description: replaced
`))
	require.NoError(t, err)

	rawUpload := findParsedEndpointByPath(t, parsed, "POST", "/uploads")
	assert.Equal(t, "application/octet-stream", rawUpload.RequestContentType)
	assert.True(t, rawUpload.BodyRequired)
	assert.Empty(t, rawUpload.Body)

	textResponse := findParsedEndpointByPath(t, parsed, "GET", "/transcripts/{id}/srt")
	assert.Equal(t, "text", textResponse.ResponseFormat)
	accept, ok := acceptOverride(textResponse)
	require.True(t, ok)
	assert.Equal(t, "text/plain", accept)

	jsonEndpoint := findParsedEndpointByPath(t, parsed, "POST", "/widgets")
	assert.Equal(t, "application/json", jsonEndpoint.RequestContentType)
	assert.NotEmpty(t, jsonEndpoint.Body)
	assert.Empty(t, jsonEndpoint.ResponseFormat)

	schemaLessRaw := findParsedEndpointByPath(t, parsed, "PUT", "/blobs")
	assert.Equal(t, "application/octet-stream", schemaLessRaw.RequestContentType)
	assert.True(t, schemaLessRaw.BodyRequired)
	assert.Empty(t, schemaLessRaw.Body)
}

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

func TestParsePreservesRawMetadataForNonFlattenableRequestSchemas(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`
openapi: 3.0.3
info:
  title: Non-Flattenable Media API
  version: 1.0.0
paths:
  /raw-array:
    post:
      operationId: uploadRawArray
      requestBody:
        required: true
        content:
          application/octet-stream:
            schema:
              type: array
              items: { type: string }
      responses:
        "204":
          description: accepted
  /raw-one-of:
    post:
      operationId: uploadRawUnion
      requestBody:
        required: true
        content:
          application/protobuf:
            schema:
              oneOf:
                - type: string
                - type: object
                  properties:
                    value: { type: string }
      responses:
        "204":
          description: accepted
  /raw-any-of:
    post:
      operationId: uploadRawAnyOf
      requestBody:
        required: true
        content:
          text/plain:
            schema:
              anyOf:
                - type: string
                - type: number
      responses:
        "204":
          description: accepted
  /json-array:
    post:
      operationId: uploadJSONArray
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: array
              items: { type: string }
      responses:
        "204":
          description: accepted
  /multipart-union:
    post:
      operationId: uploadMultipartUnion
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              oneOf:
                - type: object
                  properties:
                    file: { type: string, format: binary }
                - type: object
                  properties:
                    url: { type: string }
      responses:
        "204":
          description: accepted
`))
	require.NoError(t, err)

	for _, path := range []string{"/raw-array", "/raw-one-of", "/raw-any-of"} {
		endpoint := findParsedEndpointByPath(t, parsed, "POST", path)
		assert.True(t, endpoint.BodyRequired, path)
		assert.False(t, endpoint.BodyJSONFallback, path)
		assert.Empty(t, endpoint.Body, path)
	}
	assert.Equal(t, "application/octet-stream", findParsedEndpointByPath(t, parsed, "POST", "/raw-array").RequestContentType)
	assert.Equal(t, "application/protobuf", findParsedEndpointByPath(t, parsed, "POST", "/raw-one-of").RequestContentType)
	assert.Equal(t, "text/plain", findParsedEndpointByPath(t, parsed, "POST", "/raw-any-of").RequestContentType)

	jsonArray := findParsedEndpointByPath(t, parsed, "POST", "/json-array")
	assert.Equal(t, "application/json", jsonArray.RequestContentType)
	assert.True(t, jsonArray.BodyRequired)
	assert.True(t, jsonArray.BodyJSONFallback)
	assert.True(t, jsonArray.BodyIsArray)

	multipartUnion := findParsedEndpointByPath(t, parsed, "POST", "/multipart-union")
	assert.Empty(t, multipartUnion.RequestContentType)
	assert.False(t, multipartUnion.BodyJSONFallback)
	assert.Empty(t, multipartUnion.Body)
}

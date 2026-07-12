package openapi

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// binaryResponseSpec exercises the four shapes that matter for binary-only
// response detection: JSON-only (no override), octet-stream-only (override),
// pdf-only (override to the concrete type), text/XML-only (no override because
// the client does not binary-wrap them), and a mixed octet-stream+JSON response
// (no override — JSON is reachable with the default Accept).
const binaryResponseSpec = `
openapi: 3.0.0
info:
  title: binresp
  version: "1"
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id: { type: string }
  /widgets/{id}/content:
    get:
      operationId: downloadWidgetContent
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200":
          description: ok
          content:
            application/octet-stream:
              schema:
                type: string
                format: byte
  /widgets/{id}/report:
    get:
      operationId: widgetReport
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200":
          description: ok
          content:
            application/pdf:
              schema:
                type: string
                format: byte
  /widgets/{id}/export:
    get:
      operationId: exportWidget
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200":
          description: ok
          content:
            application/octet-stream:
              schema: { type: string, format: byte }
            application/json:
              schema: { type: object }
  /widgets/{id}/csv:
    get:
      operationId: exportWidgetCSV
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200":
          description: ok
          content:
            text/csv:
              schema: { type: string }
  /widgets/{id}/xml:
    get:
      operationId: exportWidgetXML
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses:
        "200":
          description: ok
          content:
            application/xml:
              schema: { type: string }
`

func acceptOverride(e spec.Endpoint) (string, bool) {
	for _, h := range e.HeaderOverrides {
		if h.Name == "Accept" {
			return h.Value, true
		}
	}
	return "", false
}

func endpointByPath(s *spec.APISpec, path string) (spec.Endpoint, bool) {
	for _, r := range s.Resources {
		for _, e := range r.Endpoints {
			if e.Path == path {
				return e, true
			}
		}
		for _, sub := range r.SubResources {
			for _, e := range sub.Endpoints {
				if e.Path == path {
					return e, true
				}
			}
		}
	}
	return spec.Endpoint{}, false
}

func TestParseBinaryOnlyResponseEmitsAcceptOverride(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(binaryResponseSpec))
	require.NoError(t, err)

	t.Run("octet-stream-only gets Accept override", func(t *testing.T) {
		e, ok := endpointByPath(parsed, "/widgets/{id}/content")
		require.True(t, ok, "expected /widgets/{id}/content endpoint")
		v, has := acceptOverride(e)
		require.True(t, has, "binary-only endpoint must carry an Accept header override")
		assert.Equal(t, "application/octet-stream", v)
	})

	t.Run("non-octet binary type is pinned to the concrete type", func(t *testing.T) {
		e, ok := endpointByPath(parsed, "/widgets/{id}/report")
		require.True(t, ok)
		v, has := acceptOverride(e)
		require.True(t, has)
		assert.Equal(t, "application/pdf", v)
	})

	t.Run("JSON-only endpoint gets no Accept override", func(t *testing.T) {
		e, ok := endpointByPath(parsed, "/widgets")
		require.True(t, ok)
		_, has := acceptOverride(e)
		assert.False(t, has, "JSON endpoints must keep the default application/json Accept")
	})

	t.Run("mixed octet-stream+JSON keeps the JSON default", func(t *testing.T) {
		e, ok := endpointByPath(parsed, "/widgets/{id}/export")
		require.True(t, ok)
		_, has := acceptOverride(e)
		assert.False(t, has, "a JSON-reachable response must not be forced to octet-stream")
	})

	t.Run("text response gets no Accept override", func(t *testing.T) {
		e, ok := endpointByPath(parsed, "/widgets/{id}/csv")
		require.True(t, ok)
		_, has := acceptOverride(e)
		assert.False(t, has, "text responses must not be forced into the binary response path")
	})

	t.Run("XML response gets an application/xml Accept override and xml format", func(t *testing.T) {
		e, ok := endpointByPath(parsed, "/widgets/{id}/xml")
		require.True(t, ok)
		v, has := acceptOverride(e)
		require.True(t, has, "XML-only endpoints pin Accept so the server returns XML, not 406")
		assert.Equal(t, "application/xml", v)
		assert.Equal(t, spec.ResponseFormatXML, e.ResponseFormat)
	})
}

func TestUpsertHeaderOverride(t *testing.T) {
	t.Parallel()

	base := []spec.RequiredHeader{{Name: "X-Api-Version", Value: "2"}}

	appended := upsertHeaderOverride(base, "Accept", "application/octet-stream")
	assert.Len(t, appended, 2)
	v, ok := acceptOverride(spec.Endpoint{HeaderOverrides: appended})
	assert.True(t, ok)
	assert.Equal(t, "application/octet-stream", v)

	replaced := upsertHeaderOverride(appended, "accept", "application/pdf")
	assert.Len(t, replaced, 2, "case-insensitive name match must replace, not append")
	v, _ = acceptOverride(spec.Endpoint{HeaderOverrides: replaced})
	assert.Equal(t, "application/pdf", v)
}

package openapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddedPagedSubresources_Spotify exercises the Spotify-shape:
// GET /playlists/{id} returns a struct whose `tracks` property is a
// PagingObject (items + next). The detector must surface tracks as a
// companion-helper candidate.
func TestEmbeddedPagedSubresources_Spotify(t *testing.T) {
	t.Parallel()

	doc := []byte(`
openapi: "3.0.0"
info:
  title: Spotify Mock
  version: "1.0"
servers:
  - url: https://api.spotify.example/v1
paths:
  /playlists/{id}:
    get:
      operationId: getPlaylist
      parameters:
        - { name: id, in: path, required: true, schema: { type: string } }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: string }
                  name: { type: string }
                  tracks:
                    type: object
                    properties:
                      items:
                        type: array
                        items: { type: object }
                      next: { type: string }
                      total: { type: integer }
`)
	parsed, err := Parse(doc)
	require.NoError(t, err)

	var ep, ok = findGetEndpoint(parsed, "/playlists/{id}")
	require.True(t, ok, "expected GET /playlists/{id} endpoint")
	require.Len(t, ep.EmbeddedPagedSubresources, 1, "expected one detected sub-resource")
	got := ep.EmbeddedPagedSubresources[0]
	assert.Equal(t, "tracks", got.Property)
	assert.Equal(t, "/playlists/{id}/tracks", got.ChildPath)
	assert.Equal(t, "items", got.ItemsField)
	assert.Equal(t, "next", got.NextField)
	assert.True(t, got.NextIsURL, "`next` is the canonical URL-bearing next-page field")
	assert.False(t, got.NextIsBoolean)
}

// TestEmbeddedPagedSubresources_HasMore covers the boolean has_more shape
// used by Stripe-style envelopes nested under a parent GET.
func TestEmbeddedPagedSubresources_HasMore(t *testing.T) {
	t.Parallel()

	doc := []byte(`
openapi: "3.0.0"
info: { title: Stripe Mock, version: "1.0" }
servers: [{ url: https://api.stripe.example/v1 }]
paths:
  /customers/{id}:
    get:
      operationId: getCustomer
      parameters:
        - { name: id, in: path, required: true, schema: { type: string } }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: string }
                  subscriptions:
                    type: object
                    properties:
                      data:
                        type: array
                        items: { type: object }
                      has_more: { type: boolean }
`)
	parsed, err := Parse(doc)
	require.NoError(t, err)
	ep, ok := findGetEndpoint(parsed, "/customers/{id}")
	require.True(t, ok)
	require.Len(t, ep.EmbeddedPagedSubresources, 1)
	got := ep.EmbeddedPagedSubresources[0]
	assert.Equal(t, "subscriptions", got.Property)
	assert.Equal(t, "data", got.ItemsField)
	assert.Equal(t, "has_more", got.NextField)
	assert.True(t, got.NextIsBoolean)
	assert.False(t, got.NextIsURL, "has_more envelopes carry no URL — runtime must single-fetch and warn")
}

// TestEmbeddedPagedSubresources_NoEnvelope ensures plain non-paged nested
// objects don't trigger detection — a top-level scalar field, an array
// without a next-page sibling, and an object missing the next signal must
// all be skipped.
func TestEmbeddedPagedSubresources_NoEnvelope(t *testing.T) {
	t.Parallel()

	doc := []byte(`
openapi: "3.0.0"
info: { title: Plain, version: "1.0" }
servers: [{ url: https://api.plain.example/v1 }]
paths:
  /things/{id}:
    get:
      operationId: getThing
      parameters:
        - { name: id, in: path, required: true, schema: { type: string } }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: string }
                  tags:
                    type: array
                    items: { type: string }
                  metadata:
                    type: object
                    properties:
                      created_at: { type: string }
                  partial_envelope:
                    type: object
                    properties:
                      items:
                        type: array
                        items: { type: object }
`)
	parsed, err := Parse(doc)
	require.NoError(t, err)
	ep, ok := findGetEndpoint(parsed, "/things/{id}")
	require.True(t, ok)
	assert.Empty(t, ep.EmbeddedPagedSubresources,
		"plain scalars, top-level arrays, plain nested objects, and item-only envelopes without a next signal must not match")
}

// TestEmbeddedPagedSubresources_ListEndpointSkipped confirms list-style
// endpoints (no path placeholder) are exempt from detection — they ARE
// the paged endpoint and don't need a companion helper.
func TestEmbeddedPagedSubresources_ListEndpointSkipped(t *testing.T) {
	t.Parallel()

	doc := []byte(`
openapi: "3.0.0"
info: { title: List Only, version: "1.0" }
servers: [{ url: https://api.list.example/v1 }]
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
                type: object
                properties:
                  data:
                    type: array
                    items: { type: object }
                  has_more: { type: boolean }
`)
	parsed, err := Parse(doc)
	require.NoError(t, err)
	ep, ok := findGetEndpoint(parsed, "/widgets")
	require.True(t, ok)
	assert.Empty(t, ep.EmbeddedPagedSubresources,
		"list endpoints (no path placeholder) must not surface embedded sub-resources")
}

// TestEmbeddedPagedSubresources_CursorVsURL is the runtime-correctness
// guard: detection must distinguish full-URL next fields (next, next_url)
// from opaque cursor tokens (next_cursor, next_page_token, cursor),
// because the runtime helper can follow the former with a direct GET
// but cannot construct the next request from the latter without
// API-specific cursor-to-query-param arithmetic. Misclassifying a
// cursor as a URL would make the runtime issue GETs against the
// cursor string itself, producing 404s — the opposite of the silent-
// truncation bug this fix targets.
func TestEmbeddedPagedSubresources_CursorVsURL(t *testing.T) {
	t.Parallel()

	doc := []byte(`
openapi: "3.0.0"
info: { title: KindCheck, version: "1.0" }
servers: [{ url: https://api.kind.example/v1 }]
paths:
  /url-shape/{id}:
    get:
      operationId: getURLShape
      parameters:
        - { name: id, in: path, required: true, schema: { type: string } }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  tracks:
                    type: object
                    properties:
                      items: { type: array, items: { type: object } }
                      next: { type: string }
  /cursor-shape/{id}:
    get:
      operationId: getCursorShape
      parameters:
        - { name: id, in: path, required: true, schema: { type: string } }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  children:
                    type: object
                    properties:
                      results: { type: array, items: { type: object } }
                      next_cursor: { type: string }
  /token-shape/{id}:
    get:
      operationId: getTokenShape
      parameters:
        - { name: id, in: path, required: true, schema: { type: string } }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  events:
                    type: object
                    properties:
                      data: { type: array, items: { type: object } }
                      next_page_token: { type: string }
`)
	parsed, err := Parse(doc)
	require.NoError(t, err)

	urlEP, ok := findGetEndpoint(parsed, "/url-shape/{id}")
	require.True(t, ok)
	require.Len(t, urlEP.EmbeddedPagedSubresources, 1)
	assert.True(t, urlEP.EmbeddedPagedSubresources[0].NextIsURL,
		"`next` is a known URL-bearing field; the runtime can follow it directly")
	assert.False(t, urlEP.EmbeddedPagedSubresources[0].NextIsBoolean)

	cursorEP, ok := findGetEndpoint(parsed, "/cursor-shape/{id}")
	require.True(t, ok)
	require.Len(t, cursorEP.EmbeddedPagedSubresources, 1)
	assert.False(t, cursorEP.EmbeddedPagedSubresources[0].NextIsURL,
		"`next_cursor` is opaque; the runtime cannot GET it as a path and must emit a truncation event")
	assert.False(t, cursorEP.EmbeddedPagedSubresources[0].NextIsBoolean)

	tokenEP, ok := findGetEndpoint(parsed, "/token-shape/{id}")
	require.True(t, ok)
	require.Len(t, tokenEP.EmbeddedPagedSubresources, 1)
	assert.False(t, tokenEP.EmbeddedPagedSubresources[0].NextIsURL,
		"`next_page_token` is opaque; same constraint as next_cursor")
	assert.False(t, tokenEP.EmbeddedPagedSubresources[0].NextIsBoolean)
}

// TestEmbeddedPagedSubresources_CaseInsensitive guards the detector
// against schemas that declare envelope fields in non-canonical casing
// (PascalCase `Items` / `NextPageToken`, screaming-snake `HAS_MORE`).
// Detection must fire on those AND preserve the wire-side casing
// rather than rewriting it to the canonical lowercase candidate —
// the runtime helper now compensates for casing drift on its own,
// but the parser shouldn't paper over real schema spellings.
func TestEmbeddedPagedSubresources_CaseInsensitive(t *testing.T) {
	t.Parallel()

	doc := []byte(`
openapi: "3.0.0"
info: { title: PascalCase, version: "1.0" }
servers: [{ url: https://api.pascal.example/v1 }]
paths:
  /pages/{id}:
    get:
      operationId: getPage
      parameters:
        - { name: id, in: path, required: true, schema: { type: string } }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: string }
                  Children:
                    type: object
                    properties:
                      Items:
                        type: array
                        items: { type: object }
                      NextPageToken: { type: string }
`)
	parsed, err := Parse(doc)
	require.NoError(t, err)
	ep, ok := findGetEndpoint(parsed, "/pages/{id}")
	require.True(t, ok)
	require.Len(t, ep.EmbeddedPagedSubresources, 1)
	got := ep.EmbeddedPagedSubresources[0]
	assert.Equal(t, "Children", got.Property)
	assert.Equal(t, "Items", got.ItemsField,
		"detector must preserve the wire-side casing of the items field")
	assert.Equal(t, "NextPageToken", got.NextField,
		"detector must preserve the wire-side casing of the next-page field")
}

// TestEmbeddedPagedSubresources_MultipleProperties verifies the detector
// reports every paged sub-resource on the same parent — e.g. GitHub-style
// where a single GET response embeds both `commits` and `review_comments`
// envelopes.
func TestEmbeddedPagedSubresources_MultipleProperties(t *testing.T) {
	t.Parallel()

	doc := []byte(`
openapi: "3.0.0"
info: { title: Multi, version: "1.0" }
servers: [{ url: https://api.multi.example/v1 }]
paths:
  /repos/{owner}/{repo}/pulls/{n}:
    get:
      operationId: getPull
      parameters:
        - { name: owner, in: path, required: true, schema: { type: string } }
        - { name: repo, in: path, required: true, schema: { type: string } }
        - { name: n, in: path, required: true, schema: { type: integer } }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  commits:
                    type: object
                    properties:
                      items:
                        type: array
                        items: { type: object }
                      next_page_token: { type: string }
                  review_comments:
                    type: object
                    properties:
                      data:
                        type: array
                        items: { type: object }
                      has_more: { type: boolean }
`)
	parsed, err := Parse(doc)
	require.NoError(t, err)
	ep, ok := findGetEndpoint(parsed, "/repos/{owner}/{repo}/pulls/{n}")
	require.True(t, ok)
	require.Len(t, ep.EmbeddedPagedSubresources, 2)
	byProp := map[string]spec.EmbeddedPagedSubresource{}
	for _, sub := range ep.EmbeddedPagedSubresources {
		byProp[sub.Property] = sub
	}
	assert.Equal(t, "/repos/{owner}/{repo}/pulls/{n}/commits", byProp["commits"].ChildPath,
		"multi-path-param parents must propagate every placeholder into ChildPath so the runtime helper substitutes them all")
	assert.Equal(t, "/repos/{owner}/{repo}/pulls/{n}/review_comments", byProp["review_comments"].ChildPath)
	assert.Equal(t, "next_page_token", byProp["commits"].NextField)
	assert.False(t, byProp["commits"].NextIsURL,
		"next_page_token is an opaque cursor, not a URL — runtime must NOT GET it as a path")
	assert.True(t, byProp["review_comments"].NextIsBoolean)
}

// TestItemsFieldNamesMatchRuntimeExtractor guards against silent drift
// between detect-time (itemsFieldNames in this package) and runtime
// (the extractPaginatedItems helper emitted into every generated CLI).
// If one list grows a new envelope key and the other doesn't, the
// detector either fires on shapes the helper can't handle or skips
// shapes it could.
func TestItemsFieldNamesMatchRuntimeExtractor(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "generator", "templates", "helpers.go.tmpl"))
	require.NoError(t, err)
	for _, name := range itemsFieldNames {
		assert.Contains(t, string(data), `"`+name+`"`,
			"runtime extractPaginatedItems must list %q so detection and runtime walks stay in sync", name)
	}
}

func findGetEndpoint(parsed *spec.APISpec, path string) (spec.Endpoint, bool) {
	if parsed == nil {
		return spec.Endpoint{}, false
	}
	for _, r := range parsed.Resources {
		for _, e := range r.Endpoints {
			if e.Method == "GET" && e.Path == path {
				return e, true
			}
		}
		for _, sub := range r.SubResources {
			for _, e := range sub.Endpoints {
				if e.Method == "GET" && e.Path == path {
					return e, true
				}
			}
		}
	}
	return spec.Endpoint{}, false
}

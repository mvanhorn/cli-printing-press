package openapi

import (
	"fmt"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseOptionalOpenAPIDefaultsAreServerAssumptions(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`openapi: 3.0.3
info:
  title: Omit Defaults
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /items/search:
    get:
      operationId: searchItems
      tags: [items]
      parameters:
        - name: resource_subtype
          in: query
          description: Subtype filter
          schema:
            type: string
            enum: [milestone, task, approval]
            default: milestone
        - name: include_completed
          in: query
          schema:
            type: boolean
            default: true
        - name: opt_count
          in: query
          schema:
            type: integer
            default: 5
        - name: active
          in: query
          schema:
            type: boolean
            default: false
        - name: view
          in: query
          required: true
          schema:
            type: string
            default: summary
        - name: X-Mode
          in: header
          schema:
            type: string
            default: full
        - name: X-Api-Version
          in: header
          required: true
          schema:
            type: string
            default: "2026-04-01"
      responses:
        "200":
          description: OK
  /items:
    get:
      operationId: listItems
      tags: [items]
      parameters:
        - name: resource_subtype
          in: query
          schema:
            type: string
            default: milestone
        - name: limit
          in: query
          schema:
            type: integer
            default: 25
        - name: cursor
          in: query
          schema:
            type: string
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      type: object
                  next_cursor:
                    type: string
    post:
      operationId: createItem
      tags: [items]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              properties:
                name:
                  type: string
                  default: untitled
                resource_subtype:
                  type: string
                  default: milestone
                count:
                  type: integer
                  default: 5
                settings:
                  type: object
                  properties:
                    mode:
                      type: string
                      default: compact
                    note:
                      type: string
                      default: 'say "hi"'
                    kind:
                      type: string
                      default: box
                  required: [kind]
      responses:
        "201":
          description: Created
  /orders/{page}:
    get:
      operationId: getOrderPage
      tags: [orders]
      parameters:
        - name: page
          in: path
          required: true
          schema:
            type: integer
            default: 1
      responses:
        "200":
          description: OK
`))
	require.NoError(t, err)

	search := findParsedEndpoint(t, parsed, "/items/search")
	subtype := findParam(t, search.Params, "resource_subtype")
	assert.True(t, subtype.ServerDefault)
	assert.Equal(t, "milestone", fmt.Sprint(subtype.Default))
	assert.False(t, subtype.Required)

	include := findParam(t, search.Params, "include_completed")
	assert.True(t, include.ServerDefault)
	assert.Equal(t, true, include.Default)

	count := findParam(t, search.Params, "opt_count")
	assert.True(t, count.ServerDefault)
	assert.Equal(t, "5", fmt.Sprint(count.Default))

	active := findParam(t, search.Params, "active")
	assert.True(t, active.ServerDefault)
	assert.Equal(t, false, active.Default)

	view := findParam(t, search.Params, "view")
	assert.False(t, view.ServerDefault, "required query defaults are still client-sent")
	assert.True(t, view.Required)
	assert.Equal(t, "summary", fmt.Sprint(view.Default))

	mode := findParam(t, search.Params, "X-Mode")
	assert.True(t, mode.ServerDefault)
	version := findParam(t, search.Params, "X-Api-Version")
	assert.False(t, version.ServerDefault)
	assert.True(t, version.Required)

	list := findParsedEndpoint(t, parsed, "/items", "GET")
	limit := findParam(t, list.Params, "limit")
	assert.True(t, limit.ServerDefault)
	assert.Equal(t, "25", fmt.Sprint(limit.Default))

	create := findParsedEndpoint(t, parsed, "/items", "POST")
	name := findParam(t, create.Body, "name")
	assert.True(t, name.Required)
	assert.False(t, name.ServerDefault)
	assert.Equal(t, "untitled", fmt.Sprint(name.Default))
	bodySubtype := findParam(t, create.Body, "resource_subtype")
	assert.True(t, bodySubtype.ServerDefault)
	bodyCount := findParam(t, create.Body, "count")
	assert.True(t, bodyCount.ServerDefault)
	settings := findParam(t, create.Body, "settings")
	nestedMode := findParam(t, settings.Fields, "mode")
	assert.True(t, nestedMode.ServerDefault)
	assert.Equal(t, "compact", fmt.Sprint(nestedMode.Default))
	nestedNote := findParam(t, settings.Fields, "note")
	assert.True(t, nestedNote.ServerDefault)
	assert.Equal(t, `say "hi"`, fmt.Sprint(nestedNote.Default))
	nestedKind := findParam(t, settings.Fields, "kind")
	assert.True(t, nestedKind.Required)
	assert.False(t, nestedKind.ServerDefault, "required nested defaults stay client-sent")
	assert.Equal(t, "box", fmt.Sprint(nestedKind.Default))

	page := findParsedEndpoint(t, parsed, "/orders/{page}")
	pageParam := findParam(t, page.Params, "page")
	assert.True(t, pageParam.PathParam)
	assert.False(t, pageParam.ServerDefault, "path-segment defaults stay client-sent")
	assert.Equal(t, "1", fmt.Sprint(pageParam.Default))

	blob, err := yaml.Marshal(subtype)
	require.NoError(t, err)
	var roundTripped spec.Param
	require.NoError(t, yaml.Unmarshal(blob, &roundTripped))
	assert.True(t, roundTripped.ServerDefault)
	assert.Equal(t, "milestone", fmt.Sprint(roundTripped.Default))
}

func TestParseGlobalScopeClearsServerDefault(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(`openapi: 3.0.3
info:
  title: Scope Defaults
  version: 1.0.0
paths:
  /a:
    get:
      parameters:
        - name: tenant
          in: query
          schema: {type: string, default: acme}
      responses: {"200": {description: ok}}
  /b:
    get:
      parameters:
        - name: tenant
          in: query
          schema: {type: string, default: acme}
      responses: {"200": {description: ok}}
  /c:
    get:
      parameters:
        - name: tenant
          in: query
          schema: {type: string, default: acme}
      responses: {"200": {description: ok}}
`))
	require.NoError(t, err)

	var saw bool
	walkParsedEndpoints(parsed, func(ep spec.Endpoint) {
		for _, param := range ep.Params {
			if param.Name != "tenant" {
				continue
			}
			saw = true
			assert.True(t, param.GlobalScope)
			assert.True(t, param.Required)
			assert.False(t, param.ServerDefault)
			assert.Equal(t, "acme", fmt.Sprint(param.Default))
		}
	})
	assert.True(t, saw, "tenant param missing after global-scope promotion")
}

func findParsedEndpoint(t *testing.T, api *spec.APISpec, path string, method ...string) spec.Endpoint {
	t.Helper()
	wantMethod := ""
	if len(method) > 0 {
		wantMethod = method[0]
	}
	var found []spec.Endpoint
	walkParsedEndpoints(api, func(ep spec.Endpoint) {
		if ep.Path != path {
			return
		}
		if wantMethod != "" && ep.Method != wantMethod {
			return
		}
		found = append(found, ep)
	})
	require.Len(t, found, 1, "path %s method %q", path, wantMethod)
	return found[0]
}

func walkParsedEndpoints(api *spec.APISpec, fn func(spec.Endpoint)) {
	var walk func(map[string]spec.Resource)
	walk = func(resources map[string]spec.Resource) {
		for _, resource := range resources {
			for _, ep := range resource.Endpoints {
				fn(ep)
			}
			walk(resource.SubResources)
		}
	}
	walk(api.Resources)
}

func findParam(t *testing.T, params []spec.Param, name string) spec.Param {
	t.Helper()
	for _, param := range params {
		if param.Name == name {
			return param
		}
	}
	names := make([]string, 0, len(params))
	for _, param := range params {
		names = append(names, param.Name)
	}
	t.Fatalf("param %q not found in %v", name, names)
	return spec.Param{}
}

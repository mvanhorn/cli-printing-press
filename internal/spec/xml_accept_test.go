package spec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureXMLAcceptHeadersPinsMissingAcceptOnly(t *testing.T) {
	t.Parallel()

	api := &APISpec{
		Resources: map[string]Resource{
			"feeds": {
				Endpoints: map[string]Endpoint{
					"list": {Method: "GET", Path: "/rss", ResponseFormat: ResponseFormatXML},
					"show": {
						Method:         "GET",
						Path:           "/atom",
						ResponseFormat: ResponseFormatXML,
						HeaderOverrides: []RequiredHeader{{
							Name:  "accept",
							Value: "application/atom+xml",
						}},
					},
					"raw": {
						Method:         "GET",
						Path:           "/raw",
						ResponseFormat: ResponseFormatXML,
						HeaderOverrides: []RequiredHeader{
							{Name: "X-Trace", Value: "1"},
							{Name: " Accept ", Value: "application/json"},
						},
					},
					"items": {Method: "GET", Path: "/items"},
				},
			},
			"wrap": {
				SubResources: map[string]Resource{
					"child": {
						Endpoints: map[string]Endpoint{
							"feed": {Method: "GET", Path: "/wrap/child", ResponseFormat: ResponseFormatXML},
						},
					},
				},
			},
		},
	}

	api.EnsureXMLAcceptHeaders()
	api.EnsureXMLAcceptHeaders()

	list := api.Resources["feeds"].Endpoints["list"]
	value, count := acceptOverride(list.HeaderOverrides)
	require.Equal(t, xmlAcceptHeader, value)
	require.Equal(t, 1, count)

	show := api.Resources["feeds"].Endpoints["show"]
	value, count = acceptOverride(show.HeaderOverrides)
	require.Equal(t, "application/atom+xml", value)
	require.Equal(t, 1, count)

	raw := api.Resources["feeds"].Endpoints["raw"]
	value, count = acceptOverride(raw.HeaderOverrides)
	require.Equal(t, "application/json", value)
	require.Equal(t, 1, count)
	require.Len(t, raw.HeaderOverrides, 2)

	_, count = acceptOverride(api.Resources["feeds"].Endpoints["items"].HeaderOverrides)
	require.Equal(t, 0, count)

	child := api.Resources["wrap"].SubResources["child"].Endpoints["feed"]
	value, count = acceptOverride(child.HeaderOverrides)
	require.Equal(t, xmlAcceptHeader, value)
	require.Equal(t, 1, count)

	var empty *APISpec
	empty.EnsureXMLAcceptHeaders()
}

func TestParseBytesPinsXMLAcceptWithoutClobberingOverride(t *testing.T) {
	t.Parallel()

	parsed, err := ParseBytes([]byte(`
name: feed-spec
base_url: https://api.example.com
auth:
  type: none
resources:
  rss:
    description: RSS
    endpoints:
      serie:
        method: GET
        path: /rss/SG
        description: Feed
        response_format: xml
  atom:
    description: Atom
    endpoints:
      list:
        method: GET
        path: /atom
        description: Atom feed
        response_format: xml
        header_overrides:
          - name: Accept
            value: application/atom+xml
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        description: Items
  wrap:
    description: Wrap
    sub_resources:
      child:
        description: Child
        endpoints:
          feed:
            method: GET
            path: /wrap/child
            description: Child feed
            response_format: xml
`))
	require.NoError(t, err)

	value, count := acceptOverride(parsed.Resources["rss"].Endpoints["serie"].HeaderOverrides)
	require.Equal(t, xmlAcceptHeader, value)
	require.Equal(t, 1, count)

	value, count = acceptOverride(parsed.Resources["atom"].Endpoints["list"].HeaderOverrides)
	require.Equal(t, "application/atom+xml", value)
	require.Equal(t, 1, count)

	_, count = acceptOverride(parsed.Resources["items"].Endpoints["list"].HeaderOverrides)
	require.Equal(t, 0, count)

	value, count = acceptOverride(parsed.Resources["wrap"].SubResources["child"].Endpoints["feed"].HeaderOverrides)
	require.Equal(t, xmlAcceptHeader, value)
	require.Equal(t, 1, count)
}

func acceptOverride(headers []RequiredHeader) (string, int) {
	value := ""
	count := 0
	for _, header := range headers {
		if hasHeaderOverride([]RequiredHeader{header}, "Accept") {
			count++
			value = header.Value
		}
	}
	return value, count
}

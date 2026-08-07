package googlediscovery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePreservesBasePathWithoutBaseURL(t *testing.T) {
	data := []byte(`{
		"kind":"discovery#restDescription",
		"name":"gmail",
		"rootUrl":"https://www.googleapis.com/",
		"basePath":"gmail/v1/",
		"resources":{"users":{"methods":{"getProfile":{
			"path":"users/{userId}/profile",
			"httpMethod":"GET",
			"parameters":{"userId":{"type":"string","location":"path","required":true}}
		}}}}
	}`)

	got, err := Parse("gmail.json", data)
	require.NoError(t, err)
	require.Equal(t, "https://www.googleapis.com/gmail/v1", got.BaseURL)
	require.Equal(t, "/users/{userId}/profile", got.Resources["users"].Endpoints["getProfile"].Path)
}

func TestParseUsesDiscoveryParameterOrderForPathArguments(t *testing.T) {
	data := []byte(`{
		"kind":"discovery#restDescription",
		"name":"example",
		"baseUrl":"https://example.googleapis.com/v1/",
		"resources":{"widgets":{"methods":{"get":{
			"path":"projects/{project}/widgets/{widget}",
			"httpMethod":"GET",
			"parameterOrder":["widget","project"],
			"parameters":{
				"project":{"type":"string","location":"path","required":true},
				"widget":{"type":"string","location":"path","required":true},
				"view":{"type":"string","location":"query"}
			}
		}}}}
	}`)

	got, err := Parse("example.json", data)
	require.NoError(t, err)
	params := got.Resources["widgets"].Endpoints["get"].Params
	require.Len(t, params, 3)
	require.Equal(t, []string{"widget", "project", "view"}, []string{params[0].Name, params[1].Name, params[2].Name})
}

// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package openapi

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

// arrayRootMultipartBody builds the request body shape Atlassian ships for
// file-upload endpoints: content type multipart/form-data, schema a top-level
// array. The item schema is an object because their generator dumped the Java
// MultipartFile type instead of declaring `type: string, format: binary`.
func arrayRootMultipartBody() *openapi3.RequestBodyRef {
	item := openapi3.NewObjectSchema()
	item.Properties = openapi3.Schemas{
		"name":             openapi3.NewStringSchema().NewRef(),
		"originalFilename": openapi3.NewStringSchema().NewRef(),
	}
	arr := openapi3.NewArraySchema()
	arr.Items = item.NewRef()

	body := openapi3.NewRequestBody()
	body.Required = true
	body.Content = openapi3.Content{
		"multipart/form-data": openapi3.NewMediaType().WithSchema(arr),
	}
	return &openapi3.RequestBodyRef{Value: body}
}

// TestMapRequestBody_ArrayRootMultipartKeepsContentType pins that a multipart
// upload endpoint is still recognised as multipart.
//
// mapRequestBody dropped the content type entirely for any array-root body
// whose media type was not JSON. endpointUsesMultipart then saw "" and the
// generator emitted the generic JSON path: the command took --stdin and POSTed
// a JSON body with the file *path* as a string. Against an endpoint that
// requires multipart/form-data that can never succeed, and the command's own
// help still claimed multipart.
//
// The transport was never the gap — the client template already ships
// PostMultipart / multipartRequestBody{Fields, FileFields}. Only the
// classification was lost.
func TestMapRequestBody_ArrayRootMultipartKeepsContentType(t *testing.T) {
	params, contentType, bodyJSONFallback, required, arrayBody :=
		mapRequestBody(arrayRootMultipartBody(), "post", "/servicedesk/{id}/attachTemporaryFile")

	require.Equal(t, "multipart/form-data", contentType,
		"content type must survive so endpointUsesMultipart classifies this endpoint")
	require.True(t, required)
	require.False(t, arrayBody,
		"a multipart upload is not a JSON array body; the --body-json array marker must stay off")
	require.False(t, bodyJSONFallback,
		"a multipart upload must not fall back to --body-json")

	require.Len(t, params, 1, "a file param must be synthesized")
	require.Equal(t, "binary", params[0].Format,
		"the param must be binary so multipartBodyMaps routes it to fileFields, not fields")
	require.True(t, params[0].Required)
	require.NotContains(t, params[0].Description, "Repeatable",
		"fileFields is map[string]string, so only one file per field is possible; do not promise otherwise")
}

// TestMapRequestBody_ArrayRootJSONStillUsesBodyJSON guards the neighbouring
// case: a genuine JSON array body keeps the --body-json fallback and the
// array-body marker. This is the branch the multipart change sits beside.
func TestMapRequestBody_ArrayRootJSONStillUsesBodyJSON(t *testing.T) {
	arr := openapi3.NewArraySchema()
	arr.Items = openapi3.NewObjectSchema().NewRef()
	body := openapi3.NewRequestBody()
	body.Required = true
	body.Content = openapi3.Content{
		"application/json": openapi3.NewMediaType().WithSchema(arr),
	}

	params, contentType, bodyJSONFallback, required, arrayBody :=
		mapRequestBody(&openapi3.RequestBodyRef{Value: body}, "put", "/postings")

	require.Equal(t, "application/json", contentType)
	require.True(t, bodyJSONFallback)
	require.True(t, arrayBody)
	require.True(t, required)
	require.Empty(t, params)
}

package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEffectiveEndpointPathPrecedence(t *testing.T) {
	t.Parallel()

	resource := Resource{BaseURL: "https://resource.example.com/"}
	endpoint := Endpoint{Path: "/items", BaseURL: "https://endpoint.example.com/v1/"}
	assert.Equal(t, "https://endpoint.example.com/v1/items", EffectiveEndpointPath(resource, endpoint))

	endpoint.BaseURL = ""
	assert.Equal(t, "https://resource.example.com/items", EffectiveEndpointPath(resource, endpoint))

	resource.BaseURL = ""
	assert.Equal(t, "/items", EffectiveEndpointPath(resource, endpoint))
}

func TestEffectiveSubEndpointPathInheritsParentBase(t *testing.T) {
	t.Parallel()

	parent := Resource{BaseURL: "https://parent.example.com"}
	sub := Resource{}
	endpoint := Endpoint{Path: "/parent/{id}/children"}
	assert.Equal(t, "https://parent.example.com/parent/{id}/children", EffectiveSubEndpointPath(parent, sub, endpoint))

	sub.BaseURL = "https://child.example.com"
	assert.Equal(t, "https://child.example.com/parent/{id}/children", EffectiveSubEndpointPath(parent, sub, endpoint))

	endpoint.BaseURL = "https://endpoint.example.com"
	assert.Equal(t, "https://endpoint.example.com/parent/{id}/children", EffectiveSubEndpointPath(parent, sub, endpoint))
}

func TestEndpointPathWithBaseLeavesAbsolutePathAlone(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "https://clob.example.com/book",
		EndpointPathWithBase("https://gamma.example.com", "https://clob.example.com/book"))
	assert.Equal(t, "http://clob.example.com/book",
		EndpointPathWithBase("https://gamma.example.com", "http://clob.example.com/book"))
	assert.Equal(t, "https://gamma.example.com/v1/book",
		EndpointPathWithBase("https://gamma.example.com/v1/", "/book"))
	assert.Equal(t, "https://labs.google/fx/api/trpc/foo",
		EndpointPathWithBase("https://labs.google/fx/api/trpc", "/foo"))
	// Protocol-relative URLs stay relative. Only explicit http:// and
	// https:// paths select another host.
	assert.Equal(t, "https://gamma.example.com//cdn.example.com/book",
		EndpointPathWithBase("https://gamma.example.com", "//cdn.example.com/book"))
}

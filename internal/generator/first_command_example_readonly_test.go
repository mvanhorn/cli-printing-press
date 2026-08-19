// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestFirstCommandExample_PrefersReadOnlyWhenVerbsArePrefixed(t *testing.T) {
	resources := map[string]spec.Resource{
		"servicedeskapi": {
			Endpoints: map[string]spec.Endpoint{
				"add-customers": {
					Method: "POST",
					Path:   "/rest/servicedeskapi/servicedesk/{serviceDeskId}/customer",
					Params: []spec.Param{
						{Name: "serviceDeskId", Positional: true, Required: true},
					},
				},
				"get-service-desks": {
					Method: "GET",
					Path:   "/rest/servicedeskapi/servicedesk",
				},
			},
		},
	}

	require.Equal(t, "servicedeskapi get-service-desks", firstCommandExample(resources))
}

func TestFirstCommandExample_PrefersGETAcrossResources(t *testing.T) {
	resources := map[string]spec.Resource{
		"aaa-writes": {
			Endpoints: map[string]spec.Endpoint{
				"create-thing": {Method: "POST", Path: "/things"},
			},
		},
		"zzz-reads": {
			Endpoints: map[string]spec.Endpoint{
				"fetch-thing": {Method: "GET", Path: "/things"},
			},
		},
	}

	got := firstCommandExample(resources)
	require.Contains(t, got, "zzz-reads",
		"a GET in a later resource beats a POST in an earlier one; got %q", got)
}

func TestFirstCommandExample_MutationOnlySpecStillReturnsSomething(t *testing.T) {
	resources := map[string]spec.Resource{
		"writes": {
			Endpoints: map[string]spec.Endpoint{
				"create-thing": {Method: "POST", Path: "/things"},
			},
		},
	}
	require.NotEmpty(t, firstCommandExample(resources))
}

func TestFirstCommandExampleUsesEndpointReadClassification(t *testing.T) {
	resources := map[string]spec.Resource{
		"res": {
			Endpoints: map[string]spec.Endpoint{
				"get-dangerous":  {Method: "GET", Path: "/dangerous", Mutation: spec.OptionalBool(true)},
				"run-rebuild":    {Method: "GET", Path: "/rebuild"},
				"search-records": {Method: "POST", Path: "/search"},
			},
		},
	}
	require.Equal(t, "res search-records", firstCommandExample(resources))
}

// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestFirstCommandExample_PrefersReadOnlyWhenVerbsArePrefixed pins that the
// docs example is never a mutating call.
//
// The read-only preference matched endpoint names *exactly* — "list", "get",
// "search", "query". Real specs name endpoints with the verb as a prefix
// ("get-service-desks", "add-customers"), so the preference silently did not
// apply and the fallback picked the alphabetically-first endpoint. For a Jira
// Service Management CLI that is `add-customers`, a POST that adds customers to
// a service desk — and it landed in the README/SKILL blocks that illustrate
// --select, --json, --dry-run, --agent and --profile.
//
// These artifacts are read by agents, so a mutating command in the "here is how
// you use the flags" slot is materially likely to be executed.
func TestFirstCommandExample_PrefersReadOnlyWhenVerbsArePrefixed(t *testing.T) {
	resources := map[string]spec.Resource{
		"servicedeskapi": {
			Endpoints: map[string]spec.Endpoint{
				// Alphabetically first, and a mutation.
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

	got := firstCommandExample(resources)
	require.NotEmpty(t, got)
	require.False(t, strings.Contains(got, "add-customers"),
		"docs example must not be a mutating endpoint; got %q", got)
	require.Contains(t, got, "get-service-desks",
		"a GET endpoint was available and should have been chosen; got %q", got)
}

// TestFirstCommandExample_PrefersGETAcrossResources covers the cross-resource
// case: a mutation in an alphabetically earlier resource must not beat a GET in
// a later one.
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

// TestFirstCommandExample_MutationOnlySpecStillReturnsSomething: when a spec has
// no read-only endpoint at all there is nothing safe to show, and returning
// empty would delete the docs block. Falling back is correct — but it must be
// the only case where a mutation appears.
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

// TestFirstCommandExample_RespectsExplicitMutationFlag: a GET marked
// Mutation:true (a GET action with side effects) must not be treated as safe.
func TestFirstCommandExample_RespectsExplicitMutationFlag(t *testing.T) {
	resources := map[string]spec.Resource{
		"res": {
			Endpoints: map[string]spec.Endpoint{
				"aaa-trigger-rebuild": {Method: "GET", Path: "/rebuild", Mutation: true},
				"bbb-fetch-status":    {Method: "GET", Path: "/status"},
			},
		},
	}
	got := firstCommandExample(resources)
	require.Contains(t, got, "bbb-fetch-status",
		"an endpoint flagged Mutation must not be chosen as the safe example; got %q", got)
}

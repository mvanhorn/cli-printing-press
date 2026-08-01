package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func bracketSpec(name string, params []spec.Param, pageable bool) *spec.APISpec {
	apiSpec := minimalSpec(name)
	ep := spec.Endpoint{
		Method: "GET", Path: "/items", Description: "List items",
		Params:   params,
		Response: spec.ResponseDef{Type: "array", Item: "Item"},
	}
	if pageable {
		ep.Pagination = &spec.Pagination{Type: "cursor", CursorParam: "cursor", NextCursorPath: "next_cursor"}
	}
	apiSpec.Resources = map[string]spec.Resource{
		"items": {Description: "Items", Endpoints: map[string]spec.Endpoint{"get": ep}},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Item": {Fields: []spec.TypeField{{Name: "id", Type: "integer"}}},
	}
	return apiSpec
}

// Collisions the dedup pass already resolves must keep generating (CTO round-1
// regression guard): foo[] + foo collide in the flag namespace and dedup
// suffixes an IdentName, yielding distinct public names.
func TestDedupResolvableCollisionStillGenerates(t *testing.T) {
	t.Parallel()
	apiSpec := bracketSpec("dedup-ok", []spec.Param{
		{Name: "foo[]", Type: "array", ItemType: "string"},
		{Name: "foo", Type: "string"},
	}, false)
	outputDir := filepath.Join(t.TempDir(), "dedup-ok-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
}

// Two >64-char names identical in their first 64 chars clamp to the same
// public key — the flag-space dedup never sees it, the assertion must.
func TestClampCollisionFailsGeneration(t *testing.T) {
	t.Parallel()
	head := strings.Repeat("a", 64)
	apiSpec := bracketSpec("clamp-collide", []spec.Param{
		{Name: head + "[one]", Type: "string"},
		{Name: head + "[two]", Type: "string"},
	}, false)
	err := New(apiSpec, filepath.Join(t.TempDir(), "clamp-collide-pp-cli")).Generate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "MCP input name")
	require.Contains(t, err.Error(), "collide")
}

// A sanitized name landing on a generator-reserved input key must fail:
// cursor[] -> cursor on a pageable endpoint would shadow the injected
// pagination cursor input.
func TestSanitizedNameHittingReservedCursorFails(t *testing.T) {
	t.Parallel()
	apiSpec := bracketSpec("reserved-hit", []spec.Param{
		{Name: "cursor[]", Type: "array", ItemType: "string"},
	}, true)
	err := New(apiSpec, filepath.Join(t.TempDir(), "reserved-hit-pp-cli")).Generate()
	require.Error(t, err)
	require.Contains(t, err.Error(), `"cursor"`)
}

// A final name >64 chars must fail even when it arrives via a path the
// sanitizer never sees (authored flag_name is unclamped today).
func TestOverlongAuthoredFlagNameFailsGeneration(t *testing.T) {
	t.Parallel()
	apiSpec := bracketSpec("overlong", []spec.Param{
		{Name: "q", Type: "string", FlagName: strings.Repeat("a", 65)},
	}, false)
	err := New(apiSpec, filepath.Join(t.TempDir(), "overlong-pp-cli")).Generate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "64")
}

// The Architect's round-1 hole, pinned exactly (design §4 test 3): a
// dedup-SUFFIXED final name >64 chars must fail. Two 63-char names that
// collide in the cobra flag namespace (the second differs only by a
// flag-illegal ">" suffix, Twilio-style) force uniquifyIdentifiers to assign
// IdentName "<name>_2", whose kebab public form is 65 chars — charset-legal,
// length-illegal, and invisible to any pre-dedup check.
func TestOverlongDedupSuffixedNameFailsGeneration(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 63)
	apiSpec := bracketSpec("dedup-overlong", []spec.Param{
		{Name: long, Type: "string"},
		{Name: long + ">", Type: "string"},
	}, false)
	err := New(apiSpec, filepath.Join(t.TempDir(), "dedup-overlong-pp-cli")).Generate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "64")
}

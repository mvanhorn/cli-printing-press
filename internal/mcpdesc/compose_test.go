package mcpdesc

import (
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v2/internal/spec"
	"github.com/stretchr/testify/assert"
)

func TestCompose_CreatePOST(t *testing.T) {
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "POST",
			Path:        "/projects",
			Description: "Create project",
			Body: []spec.Param{
				{Name: "name", Required: true},
				{Name: "visibility", Required: true},
				{Name: "owner_email", Required: false},
			},
			Response: spec.ResponseDef{Type: "object", Item: "Project"},
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Equal(t, "Create project. Required: name, visibility. Optional: owner_email. Returns the new Project.", got)
}

func TestCompose_ListGETArrayResponse(t *testing.T) {
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "GET",
			Path:        "/projects",
			Description: "List projects",
			Params: []spec.Param{
				{Name: "status", Required: false},
				{Name: "limit", Required: false},
				{Name: "cursor", Required: false},
			},
			Response: spec.ResponseDef{Type: "array", Item: "Project"},
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Equal(t, "List projects. Optional: status, limit, cursor. Returns array of Project.", got)
}

func TestCompose_PatchUPDATEWithPathParams(t *testing.T) {
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "PATCH",
			Path:        "/projects/{projectId}/tasks/{taskId}",
			Description: "Update project task",
			Params: []spec.Param{
				{Name: "projectId", Required: true, Positional: true},
				{Name: "taskId", Required: true, Positional: true},
			},
			Body: []spec.Param{
				{Name: "title", Required: false},
				{Name: "priority", Required: false},
				{Name: "completed", Required: false},
			},
			// No response shape declared
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Equal(t, "Update project task. Required: projectId, taskId. Optional: title, priority, completed. Partial update.", got)
}

func TestCompose_DeleteAddsDestructive(t *testing.T) {
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "DELETE",
			Path:        "/items/{itemId}",
			Description: "Delete item",
			Params: []spec.Param{
				{Name: "itemId", Required: true, Positional: true},
			},
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Equal(t, "Delete item. Required: itemId. Destructive.", got)
}

func TestCompose_PathParamWithDefaultStaysRequired(t *testing.T) {
	// API-contract view: enum-typed path param with default still
	// shows as Required (it's structurally in the URL). Default
	// annotation tells the agent "you may skip it; runtime fills in".
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "POST",
			Path:        "/calendars/{calendar}/disconnect",
			Description: "Disconnect a calendar",
			Params: []spec.Param{
				{
					Name:       "calendar",
					Default:    "apple",
					Positional: false,
					PathParam:  true, // reclassified by parser
					Required:   false,
				},
			},
			Body: []spec.Param{
				{Name: "id", Required: true},
			},
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Contains(t, got, "Required: calendar (default: apple), id.", "path param must be Required regardless of default; default value annotated")
	assert.NotContains(t, got, "Optional: calendar", "path param must not appear as Optional")
}

func TestCompose_OptionalTruncationHonored(t *testing.T) {
	body := []spec.Param{
		{Name: "a", Required: false},
		{Name: "b", Required: false},
		{Name: "c", Required: false},
		{Name: "d", Required: false},
		{Name: "e", Required: false},
	}
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "POST",
			Path:        "/things",
			Description: "Create thing",
			Body:        body,
			Response:    spec.ResponseDef{Type: "object", Item: "Thing"},
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Contains(t, got, "Optional: a, b, c (plus 2 more).")
}

func TestCompose_PassesThroughHandTunedOverride(t *testing.T) {
	// mcpoverrides.Apply writes the override into endpoint.Description
	// before Compose runs. If Compose blindly added Required/Optional/
	// Returns on top, the override "Required: name" + composer
	// "Required: name, X" would double-stamp. Pre-composed descriptions
	// (any of "Required:" / "Optional:" / "Returns ") get passed
	// through with only auth-suffix and period normalization applied.
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "POST",
			Path:        "/tags",
			Description: "Create a new tag in the workspace. Required: name. Returns the tag's id and slug. Tags must exist before they can be assigned to links.",
			Body: []spec.Param{
				{Name: "name", Required: true},
				{Name: "color", Required: false},
			},
			Response: spec.ResponseDef{Type: "object", Item: "Tag"},
		},
		AuthType: "none",
	}
	got := Compose(in)
	expected := "Create a new tag in the workspace. Required: name. Returns the tag's id and slug. Tags must exist before they can be assigned to links."
	assert.Equal(t, expected, got)
}

func TestCompose_PreComposedSpecDescriptionPassesThrough(t *testing.T) {
	// Auto-generated specs sometimes describe what an endpoint
	// returns in the description itself. Treat that as authoritative
	// to avoid "Returns pet inventories by status. Returns array of
	// integers." — keep what the spec author chose.
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "GET",
			Path:        "/store/inventory",
			Description: "Returns pet inventories by status",
			Response:    spec.ResponseDef{Type: "object", Item: "Inventory"},
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Equal(t, "Returns pet inventories by status.", got)
	assert.Equal(t, 1, strings.Count(strings.ToLower(got), "returns"), "must not double-up Returns clause")
}

func TestCompose_DeleteAlwaysGetsDestructiveEvenWithReturnsInAction(t *testing.T) {
	// appendMethodMarker fires after composition so the Destructive
	// marker is added to pre-composed (override) descriptions too,
	// not just fresh-composed ones. Agents need to know the call
	// removes data regardless of whether the override mentioned it.
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "DELETE",
			Path:        "/things/{id}",
			Description: "Returns the deleted resource",
			Params:      []spec.Param{{Name: "id", Required: true, Positional: true}},
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Contains(t, got, "Destructive", "DELETE method must always carry the destructive marker")
}

func TestCompose_DeleteSkipsDestructiveWhenAlreadyPresent(t *testing.T) {
	// If the override or spec description already mentions destructive,
	// don't double up. Case-insensitive match.
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "DELETE",
			Path:        "/things/{id}",
			Description: "Delete the resource. This is destructive.",
			Params:      []spec.Param{{Name: "id", Required: true, Positional: true}},
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Equal(t, 1, strings.Count(strings.ToLower(got), "destructive"), "destructive marker must not double up")
}

func TestCompose_SpecDescriptionWithReturnsStillGetsRequiredOptional(t *testing.T) {
	// A spec description that mentions "returns" in narrative prose
	// (e.g., "Permanently deletes a card. Returns the deleted card.")
	// is NOT a structural override. Required/Optional composition must
	// still run; only the explicit Returns clause is suppressed to
	// avoid doubling. Without this, every endpoint whose description
	// happens to use the word "returns" loses parameter context — a
	// large fraction of real-world specs.
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "DELETE",
			Path:        "/cards/{cardId}",
			Description: "Permanently deletes a card. Returns the deleted card.",
			Params: []spec.Param{
				{Name: "cardId", Required: true, Positional: true, Description: "Card ID"},
				{Name: "force", Required: false, Description: "Skip confirmation"},
			},
			Response: spec.ResponseDef{Type: "object", Item: "Card"},
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Contains(t, got, "Required: cardId", "Required clause must be added even when description mentions returns")
	assert.Contains(t, got, "Optional: force", "Optional clause must be added")
	assert.Equal(t, 1, strings.Count(strings.ToLower(got), "returns"), "explicit Returns clause must be suppressed when description already mentions returns")
	assert.Contains(t, got, "Destructive", "DELETE method must carry the destructive marker")
}

func TestCompose_NoParamsNoResponse(t *testing.T) {
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "GET",
			Path:        "/health",
			Description: "Health check",
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Equal(t, "Health check.", got)
}

func TestCompose_AppendsAuthAnnotationViaMCPDescription(t *testing.T) {
	// Compose delegates the (public)/(requires auth) suffix to
	// naming.MCPDescription so the auth-annotation logic stays
	// single-sourced. Mixed-auth: 1 public out of 5 → public side
	// is the minority, gets "(public)" suffix.
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "GET",
			Path:        "/public/status",
			Description: "Get status",
			Response:    spec.ResponseDef{Type: "object", Item: "Status"},
		},
		NoAuth:      true,
		AuthType:    "bearer_token",
		PublicCount: 1,
		TotalCount:  5,
	}
	got := Compose(in)
	assert.Contains(t, got, "(public)", "mixed-auth APIs annotate the minority side")
}

func TestCompose_DefaultAnnotationBoundedToScalars(t *testing.T) {
	// Defaults that exceed defaultValueMaxLen or contain newlines
	// should be silently dropped from the inline annotation; the
	// param still appears, just without the (default: ...) suffix.
	tests := []struct {
		name      string
		dflt      any
		wantHas   string
		wantNoHas string
	}{
		{"scalar string", "apple", "(default: apple)", ""},
		{"int default", 25, "(default: 25)", ""},
		{"bool default", true, "(default: true)", ""},
		{"oversize default skipped", strings.Repeat("x", 50), "name", "(default:"},
		{"newline default skipped", "a\nb", "name", "(default:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ep := spec.Endpoint{
				Method:      "GET",
				Path:        "/x",
				Description: "Get",
				Params: []spec.Param{
					{Name: "name", Required: false, Default: tc.dflt},
				},
			}
			got := Compose(Input{Endpoint: ep, AuthType: "none"})
			assert.Contains(t, got, tc.wantHas)
			if tc.wantNoHas != "" {
				assert.NotContains(t, got, tc.wantNoHas)
			}
		})
	}
}

func TestCompose_EmptyDescriptionStillBuildsParts(t *testing.T) {
	// Defensive: if Endpoint.Description is somehow empty, the
	// composed string still contains Required/Optional/Returns so
	// downstream consumers get something useful instead of "".
	in := Input{
		Endpoint: spec.Endpoint{
			Method:      "POST",
			Path:        "/x",
			Description: "",
			Body:        []spec.Param{{Name: "name", Required: true}},
			Response:    spec.ResponseDef{Type: "object", Item: "X"},
		},
		AuthType: "none",
	}
	got := Compose(in)
	assert.Equal(t, "Required: name. Returns the new X.", got)
}

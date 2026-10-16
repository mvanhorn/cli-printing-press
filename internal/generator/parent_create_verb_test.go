package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
)

func TestParentCommandShortOmitsCreateForActionPOST(t *testing.T) {
	t.Parallel()

	distributors := parentCommandShort("distributors", "accounts", spec.Resource{
		Description: "Manage distributors",
		Endpoints: map[string]spec.Endpoint{
			"list":   {Method: "GET", Path: "/distributors", Description: "List distributors"},
			"get":    {Method: "GET", Path: "/distributors/{id}", Description: "Get a distributor"},
			"cancel": {Method: "POST", Path: "/distributors/{id}/cancel", Description: "Cancel a distributor"},
		},
	}, "")
	assert.Equal(t, "List and get distributors for accounts", distributors)
	assert.NotContains(t, distributors, "create")

	executables := parentCommandShort("executables", "accounts", spec.Resource{
		Description: "Manage executables",
		Endpoints: map[string]spec.Endpoint{
			"list":             {Method: "GET", Path: "/executables", Description: "List executables"},
			"block-executable": {Method: "POST", Path: "/executables/{uuid}:block", Description: "Block an executable"},
		},
	}, "")
	assert.Equal(t, "List executables for accounts", executables)
	assert.NotContains(t, executables, "create")

	widgets := parentCommandShort("widgets", "accounts", spec.Resource{
		Description: "Manage widgets",
		Endpoints: map[string]spec.Endpoint{
			"list":     {Method: "GET", Path: "/widgets", Description: "List widgets"},
			"register": {Method: "POST", Path: "/widgets", Description: "Register a widget"},
			"update":   {Method: "PATCH", Path: "/widgets/{id}", Description: "Update a widget"},
		},
	}, "")
	assert.Equal(t, "List, create, and update widgets for accounts", widgets)

	renders := parentCommandShort("renders", "accounts", spec.Resource{
		Description: "Manage renders",
		Endpoints: map[string]spec.Endpoint{
			"submit": {Method: "POST", Path: "/renders", Description: "Submit a render"},
		},
	}, "")
	assert.Equal(t, "Submit renders for accounts", renders)
	assert.NotContains(t, renders, "create")
}

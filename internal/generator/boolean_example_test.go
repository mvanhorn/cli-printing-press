package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestBooleanExamplesMatchRegisteredFlags(t *testing.T) {
	t.Parallel()
	api := minimalSpec("boolean-examples")
	endpoint := spec.Endpoint{
		Method: "POST", Path: "/settings", Description: "Create settings",
		Body: []spec.Param{
			{Name: "enabled", Type: "boolean"},
			{Name: "archived", Type: "bool"},
			{Name: "confirmed", Type: "boolean", Required: true},
			{Name: "label", Type: "string"},
		},
		RequestBodyExample: map[string]any{"enabled": true, "archived": false, "confirmed": true, "label": "false"},
	}
	api.Resources = map[string]spec.Resource{"settings": {Endpoints: map[string]spec.Endpoint{
		"create": endpoint,
		"list":   {Method: "GET", Path: "/settings", Description: "List settings"},
	}}}
	output := filepath.Join(t.TempDir(), naming.CLI(api.Name))
	gen := New(api, output)
	args, err := shellargs.ArgsAfterBinary(gen.exampleLine("settings", "create", endpoint))
	require.NoError(t, err)
	require.Equal(t, []string{"settings", "create", "--enabled", "--archived=false", "--confirmed=true", "--label", "false"}, args)
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, output)
	binaryName := platform.ExecutablePath("boolean-examples-bin")
	runGoCommandRequired(t, output, "build", "-o", "./"+binaryName, "./cmd/"+naming.CLI(api.Name))
	stdout := requireLearnDryRunOutput(t, filepath.Join(output, binaryName), append(args, "--dry-run", "--json"))
	require.Contains(t, stdout, `"success": true`)
}

package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// End-to-end guard for #982: a tRPC-style spec whose GET endpoints route per-
// call input through a single optional `input` query param (present on every
// endpoint) must still expose a `--input` flag. filterGlobalParams previously
// stripped it as global noise, so the generated command emitted
// `params := map[string]string{}` with no flag and calls 400'd. The parser-
// layer test guards param retention; this guards the emitted flag + wiring.
func TestGenerateKeepsSoleGlobalQueryParamAsFlag(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`openapi: "3.0.3"
info:
  title: Trpc Input API
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /users:
    get:
      operationId: listUsers
      tags: [users]
      parameters:
        - {name: input, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
  /projects:
    get:
      operationId: listProjects
      tags: [projects]
      parameters:
        - {name: input, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
  /tasks:
    get:
      operationId: listTasks
      tags: [tasks]
      parameters:
        - {name: input, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
`))
	require.NoError(t, err)
	apiSpec.Name = "trpcinput"
	apiSpec.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/trpcinput-pp-cli/config.toml"}

	outputDir := filepath.Join(t.TempDir(), "trpcinput-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	// Each single-endpoint resource is promoted to a top-level command; all
	// three must expose the flag, not just the first.
	for _, file := range []string{"promoted_users.go", "promoted_projects.go", "promoted_tasks.go"} {
		cmdBytes, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", file))
		require.NoError(t, err, "%s should be generated", file)
		cmdSrc := string(cmdBytes)

		assert.Contains(t, cmdSrc, `cmd.Flags().StringVar(&flagInput, "input", "", "Input")`,
			"%s: the sole global query param must be registered as a --input flag", file)
		assert.Contains(t, cmdSrc, `params["input"] = fmt.Sprintf("%v", flagInput)`,
			"%s: the --input flag value must propagate into the request params map", file)
	}

	runGoCommand(t, outputDir, "mod", "tidy")
	runGoCommand(t, outputDir, "build", "./...")
}

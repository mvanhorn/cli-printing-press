package generator

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/stretchr/testify/require"
)

func TestGeneratedNestedNonASCIIBodyKey(t *testing.T) {
	apiSpec, err := openapi.Parse([]byte(nestedNonASCIIBodySpec))
	require.NoError(t, err)
	require.Equal(t, "accent", apiSpec.Name)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.ModulePath = "example.com/accent"
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	command := generatedSourceContaining(t, outputDir, `["café"]`)
	require.Contains(t, command, `bodyMap["café"] = bodyDetailsCafe`)
	require.Contains(t, command, `body = map[string]any{"details": bodyMap}`)
	require.NotContains(t, command, `["cafe"]`)
	require.Contains(t, command, `"details-cafe"`)
	require.Contains(t, command, `"required flag \"%s\" not set", "details-cafe"`)
	require.Contains(t, command, `bodyMap["note"] = bodyDetailsNote`)

	tools := generatedSourceContaining(t, outputDir, `WireName: "café"`)
	require.Contains(t, tools, `PublicName: "details-cafe", WireName: "café", Location: "body", BodyPath: []string{"details", "café"}`)
	require.Contains(t, tools, `mcplib.WithString("details-cafe", mcplib.Required()`)
	require.NotContains(t, tools, `WireName: "cafe"`)

	requireGeneratedCompiles(t, outputDir)

	bin := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", bin, "./cmd/"+naming.CLI(apiSpec.Name))

	missing, err := runAccentCLI(t, bin, "places", "create", "--details-note", "quiet")
	require.Error(t, err, missing)
	require.Contains(t, missing, `required flag "details-cafe" not set`)

	dry, err := runAccentCLI(t, bin, "places", "create", "--details-cafe", "latte", "--details-note", "quiet", "--dry-run")
	require.NoError(t, err, dry)
	body := unmarshalDryRunObject(t, dry)
	details, ok := body["details"].(map[string]any)
	require.True(t, ok, "details = %#v", body["details"])
	require.Equal(t, "latte", details["café"])
	require.Equal(t, "quiet", details["note"])
	_, folded := details["cafe"]
	require.False(t, folded, "folded key present in %#v", details)
}

func generatedSourceContaining(t *testing.T, root, needle string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return walkErr
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(body), needle) {
			found = string(body)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, found, "no generated file contains %q", needle)
	return found
}

func runAccentCLI(t *testing.T, bin string, args ...string) (string, error) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"ACCENT_BASE_URL=https://api.example.com",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func unmarshalDryRunObject(t *testing.T, output string) map[string]any {
	t.Helper()
	const bodyMarker = "  Body:\n"
	start := strings.Index(output, bodyMarker)
	require.NotEqual(t, -1, start, output)
	start += len(bodyMarker)
	end := strings.Index(output[start:], "\n\n(dry run")
	require.NotEqual(t, -1, end, output)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(output[start:start+end]), &body))
	return body
}

const nestedNonASCIIBodySpec = `
openapi: 3.0.3
info:
  title: Accent API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /places:
    get:
      operationId: listPlaces
      responses:
        '200': {description: ok}
    post:
      operationId: createPlace
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [details]
              properties:
                details:
                  type: object
                  required: [café]
                  properties:
                    café:
                      type: string
                      description: Venue name
                    note:
                      type: string
      responses:
        '200': {description: ok}
`

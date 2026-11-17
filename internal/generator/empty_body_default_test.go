package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultValForBodyParamEmitsEmptyContainers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `"[]"`, defaultValForBodyParam(spec.Param{Type: "array", Default: []any{}}))
	assert.Equal(t, `"{}"`, defaultValForBodyParam(spec.Param{Type: "object", Default: map[string]any{}}))
	assert.Equal(t, `""`, defaultVal(spec.Param{Type: "array", Default: []any{}}))
	assert.Equal(t, `""`, defaultVal(spec.Param{Type: "object", Default: map[string]any{}}))
	assert.Equal(t, `""`, defaultValForBodyParam(spec.Param{Type: "array", Default: []any{}, ServerDefault: true}))
	assert.Equal(t, `""`, defaultValForBodyParam(spec.Param{Type: "object", Default: map[string]any{}, ServerDefault: true}))
	assert.Equal(t, `""`, defaultValForBodyParam(spec.Param{Type: "array", ItemType: "string", Default: []any{}}))
	assert.Equal(t, `""`, defaultValForBodyParam(spec.Param{Type: "array", Default: map[string]any{}}))
	assert.Equal(t, `""`, defaultValForBodyParam(spec.Param{Type: "object", Default: []any{}}))
	assert.Equal(t, `""`, defaultValForBodyParam(spec.Param{Type: "array"}))
	text, ok := emptyContainerBodyDefault(spec.Param{Type: "array", Default: make(chan int)})
	assert.False(t, ok)
	assert.Empty(t, text)

	nonEmpty := spec.Param{Type: "array", Default: []any{"a"}}
	assert.Equal(t, defaultVal(nonEmpty), defaultValForBodyParam(nonEmpty))
	assert.NotEqual(t, `""`, defaultValForBodyParam(nonEmpty))
	assert.NotEqual(t, `"[]"`, defaultValForBodyParam(nonEmpty))
}

func TestSniffedEmptyContainerBodyDryRun(t *testing.T) {
	t.Parallel()

	const body = `{"anticipatedAdvance":[],"meta":{},"filters":{"tags":[]}}`
	apiSpec, err := browsersniff.AnalyzeCapture(&browsersniff.EnrichedCapture{
		TargetURL: "https://incentnow.example",
		Entries: []browsersniff.EnrichedEntry{
			filteredIncentivesEntry(body),
			filteredIncentivesEntry(body),
			{
				Method:              "GET",
				URL:                 "https://incentnow.example/incentivemanager/api/public/incentives",
				ResponseStatus:      200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"ok":true}`,
				Classification:      "api",
			},
		},
	})
	require.NoError(t, err)

	endpoint := apiSpec.Resources["incentivemanager"].Endpoints["create_filtered_incentives"]
	require.NotEmpty(t, endpoint.Body)
	advance := findGeneratedBodyParam(endpoint.Body, "anticipatedAdvance")
	require.NotNil(t, advance)
	assert.False(t, advance.Required)
	assertContainerJSON(t, advance.Default, "[]")
	meta := findGeneratedBodyParam(endpoint.Body, "meta")
	require.NotNil(t, meta)
	assert.False(t, meta.Required)
	assertContainerJSON(t, meta.Default, "{}")
	filters := findGeneratedBodyParam(endpoint.Body, "filters")
	require.NotNil(t, filters)
	assert.True(t, filters.Required)
	tags := findGeneratedBodyParam(filters.Fields, "tags")
	require.NotNil(t, tags)
	assert.False(t, tags.Required)
	assertContainerJSON(t, tags.Default, "[]")

	specPath := filepath.Join(t.TempDir(), "incentnow-spec.yaml")
	require.NoError(t, browsersniff.WriteSpec(apiSpec, specPath))
	loaded, err := spec.Parse(specPath)
	require.NoError(t, err)
	loadedEndpoint := loaded.Resources["incentivemanager"].Endpoints["create_filtered_incentives"]
	loadedAdvance := findGeneratedBodyParam(loadedEndpoint.Body, "anticipatedAdvance")
	require.NotNil(t, loadedAdvance)
	assert.False(t, loadedAdvance.Required)
	assertContainerJSON(t, loadedAdvance.Default, "[]")
	loadedMeta := findGeneratedBodyParam(loadedEndpoint.Body, "meta")
	require.NotNil(t, loadedMeta)
	assertContainerJSON(t, loadedMeta.Default, "{}")
	loadedTags := findGeneratedBodyParam(findGeneratedBodyParam(loadedEndpoint.Body, "filters").Fields, "tags")
	require.NotNil(t, loadedTags)
	assertContainerJSON(t, loadedTags.Default, "[]")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(loaded.Name))
	require.NoError(t, New(loaded, outputDir).Generate())

	commandSrc := readGeneratedCLIFileContaining(t, outputDir, `"anticipated-advance"`)
	assert.Contains(t, commandSrc, `StringVar(&bodyAnticipatedAdvance, "anticipated-advance", "[]"`)
	assert.Contains(t, commandSrc, `StringVar(&bodyMeta, "meta", "{}"`)
	assert.Contains(t, commandSrc, `StringVar(&bodyFiltersTags, "filters-tags", "[]"`)
	assert.Contains(t, commandSrc, `bodyMap["anticipatedAdvance"] = asArray`)
	assert.Contains(t, commandSrc, `bodyMap["meta"] = asMap`)
	assert.Contains(t, commandSrc, `nestedFilters["tags"] = asArray`)
	assert.NotContains(t, commandSrc, `required flag \"anticipated-advance\" not set`)
	assert.NotContains(t, commandSrc, `required flag \"meta\" not set`)
	assert.NotContains(t, commandSrc, `required flag \"filters-tags\" not set`)

	requireGeneratedCompiles(t, outputDir)
	binaryPath := platform.ExecutablePath(filepath.Join(outputDir, naming.CLI(loaded.Name)))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(loaded.Name))

	env := decoyCLIEnv(t)

	omitted := dryRunRequestBody(t, binaryPath, env, "incentivemanager", "create-filtered-incentives", "--dry-run")
	assert.Equal(t, []any{}, omitted["anticipatedAdvance"])
	assert.Equal(t, map[string]any{}, omitted["meta"])
	filtersBody, ok := omitted["filters"].(map[string]any)
	require.True(t, ok, "filters = %#v", omitted["filters"])
	assert.Equal(t, []any{}, filtersBody["tags"])

	overridden := dryRunRequestBody(t, binaryPath, env, "incentivemanager", "create-filtered-incentives", "--dry-run", "--anticipated-advance", `["kept"]`)
	assert.Equal(t, []any{"kept"}, overridden["anticipatedAdvance"])
	assert.Equal(t, map[string]any{}, overridden["meta"])
	overriddenFilters, ok := overridden["filters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{}, overriddenFilters["tags"])
}

func TestOptionalObjectEmptyContainerDryRun(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("optional-settings")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/items", Description: "List items"},
			"create": {
				Method:      "POST",
				Path:        "/items",
				Description: "Create an item",
				Body: []spec.Param{{
					Name: "settings",
					Type: "object",
					Fields: []spec.Param{
						{Name: "name", Type: "string", Required: true},
						{Name: "tags", Type: "array", Default: []any{}},
					},
				}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	commandSrc := readGeneratedCLIFileContaining(t, outputDir, `"settings-tags"`)
	assert.Contains(t, commandSrc, `cmd.Flags().Changed("settings-tags")`)
	assert.NotContains(t, commandSrc, `bodySettingsTags != ""`)
	assert.Contains(t, commandSrc, `StringVar(&bodySettingsTags, "settings-tags", "[]"`)
	assert.Contains(t, commandSrc, `nestedSettings["tags"] = asArray`)

	requireGeneratedCompiles(t, outputDir)
	binaryPath := platform.ExecutablePath(filepath.Join(outputDir, naming.CLI(apiSpec.Name)))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	env := decoyCLIEnv(t)

	omitted := dryRunRequestBody(t, binaryPath, env, "items", "create", "--dry-run")
	_, present := omitted["settings"]
	assert.False(t, present, "settings = %#v", omitted["settings"])

	supplied := dryRunRequestBody(t, binaryPath, env, "items", "create", "--dry-run", "--settings-name", "ada")
	settings, ok := supplied["settings"].(map[string]any)
	require.True(t, ok, "settings = %#v", supplied["settings"])
	assert.Equal(t, "ada", settings["name"])
	assert.Equal(t, []any{}, settings["tags"])
}

func TestOptionalObjectTwoEmptyContainersCompile(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("paired-containers")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/items", Description: "List items"},
			"create": {
				Method:      "POST",
				Path:        "/items",
				Description: "Create an item",
				Body: []spec.Param{{
					Name: "settings",
					Type: "object",
					Fields: []spec.Param{
						{Name: "name", Type: "string", Required: true},
						{Name: "tags", Type: "array", Default: []any{}},
						{Name: "labels", Type: "array", Default: []any{}},
						{Name: "meta", Type: "object", Default: map[string]any{}},
						{Name: "extra", Type: "object", Default: map[string]any{}},
					},
				}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)
	binaryPath := platform.ExecutablePath(filepath.Join(outputDir, naming.CLI(apiSpec.Name)))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	env := decoyCLIEnv(t)

	omitted := dryRunRequestBody(t, binaryPath, env, "items", "create", "--dry-run")
	_, present := omitted["settings"]
	assert.False(t, present, "settings = %#v", omitted["settings"])

	supplied := dryRunRequestBody(t, binaryPath, env, "items", "create", "--dry-run", "--settings-name", "ada")
	settings, ok := supplied["settings"].(map[string]any)
	require.True(t, ok, "settings = %#v", supplied["settings"])
	assert.Equal(t, "ada", settings["name"])
	assert.Equal(t, []any{}, settings["tags"])
	assert.Equal(t, []any{}, settings["labels"])
	assert.Equal(t, map[string]any{}, settings["meta"])
	assert.Equal(t, map[string]any{}, settings["extra"])
}

func TestOmittedObjectKeepsChildRequirements(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("omitted-object")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/items", Description: "List items"},
			"create": {
				Method:      "POST",
				Path:        "/items",
				Description: "Create an item",
				Body: []spec.Param{{
					Name:     "filter",
					Type:     "object",
					Required: true,
					Fields: []spec.Param{
						{Name: "label", Type: "string", Required: true},
						{
							Name: "config",
							Type: "object",
							Fields: []spec.Param{
								{Name: "name", Type: "string", Required: true},
								{Name: "mode", Type: "string", Required: true},
							},
						},
					},
				}},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)
	binaryPath := platform.ExecutablePath(filepath.Join(outputDir, naming.CLI(apiSpec.Name)))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	env := decoyCLIEnv(t)

	omitted := dryRunRequestBody(t, binaryPath, env, "items", "create", "--dry-run", "--filter-label", "b")
	filter, ok := omitted["filter"].(map[string]any)
	require.True(t, ok, "filter = %#v", omitted["filter"])
	assert.Equal(t, "b", filter["label"])
	_, present := filter["config"]
	assert.False(t, present, "config = %#v", filter["config"])

	code, out := runPrintedCLI(t, binaryPath, env, "items", "create", "--dry-run", "--filter-label", "b", "--filter-config-mode", "y")
	require.NotEqual(t, 0, code, out)
	assert.Contains(t, out, `required flag "filter-config-name" not set`)

	supplied := dryRunRequestBody(t, binaryPath, env, "items", "create", "--dry-run", "--filter-label", "b", "--filter-config-name", "a", "--filter-config-mode", "y")
	filter, ok = supplied["filter"].(map[string]any)
	require.True(t, ok, "filter = %#v", supplied["filter"])
	config, ok := filter["config"].(map[string]any)
	require.True(t, ok, "config = %#v", filter["config"])
	assert.Equal(t, "a", config["name"])
	assert.Equal(t, "y", config["mode"])
}

func decoyCLIEnv(t *testing.T) []string {
	t.Helper()
	decoy := t.TempDir()
	for _, dir := range []string{
		filepath.Join(decoy, ".config"),
		filepath.Join(decoy, ".local", "share"),
		filepath.Join(decoy, ".local", "state"),
		filepath.Join(decoy, ".cache"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	return harnessFreeEnv(
		"HOME="+decoy,
		"USERPROFILE="+decoy,
		"XDG_CONFIG_HOME="+filepath.Join(decoy, ".config"),
		"XDG_DATA_HOME="+filepath.Join(decoy, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(decoy, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(decoy, ".cache"),
	)
}

func filteredIncentivesEntry(body string) browsersniff.EnrichedEntry {
	return browsersniff.EnrichedEntry{
		Method:              "POST",
		URL:                 "https://incentnow.example/incentivemanager/api/public/filtered-incentives",
		RequestHeaders:      map[string]string{"Content-Type": "application/json"},
		RequestBody:         body,
		ResponseStatus:      200,
		ResponseContentType: "application/json",
		ResponseBody:        `{"ok":true}`,
		Classification:      "api",
	}
}

func findGeneratedBodyParam(params []spec.Param, name string) *spec.Param {
	for i := range params {
		if params[i].Name == name {
			return &params[i]
		}
	}
	return nil
}

func assertContainerJSON(t *testing.T, value any, want string) {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	assert.Equal(t, want, string(raw))
}

func dryRunRequestBody(t *testing.T, binary string, env []string, args ...string) map[string]any {
	t.Helper()
	code, out := runPrintedCLI(t, binary, env, args...)
	require.Equal(t, 0, code, out)
	const marker = "Body:\n"
	idx := strings.Index(out, marker)
	require.NotEqual(t, -1, idx, "dry-run output missing body:\n%s", out)
	var body map[string]any
	require.NoError(t, json.NewDecoder(strings.NewReader(out[idx+len(marker):])).Decode(&body), out)
	return body
}

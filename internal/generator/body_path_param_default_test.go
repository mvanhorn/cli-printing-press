package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBodyPathParamDefaults_NestedStringLeaf(t *testing.T) {
	t.Parallel()
	got := bodyPathParamDefaults(spec.Endpoint{
		Path: "/parents/{parentRef}/children",
		Params: []spec.Param{
			{Name: "parentRef", Type: "string", Required: true, Positional: true},
		},
		Body: []spec.Param{{
			Name: "child", Type: "object", Required: true,
			Fields: []spec.Param{
				{Name: "parentRef", Type: "string", Required: true},
				{Name: "title", Type: "string", Required: true},
				{Name: "childParentRef", Type: "string"},
			},
		}},
	}, "\t\t\t")
	require.Contains(t, got, `if len(args) > 0 && args[0] != "" && !cmd.Flags().Changed("child-parent-ref") {`)
	require.Contains(t, got, "\tbodyChildParentRef = args[0]")
	assert.NotContains(t, got, "bodyChildTitle")
	assert.NotContains(t, got, "bodyChildChildParentRef")
	assert.NotContains(t, got, "child-child-parent-ref")
}

func TestBodyPathParamDefaults_NormalizesCamelAndSnake(t *testing.T) {
	t.Parallel()
	got := bodyPathParamDefaults(spec.Endpoint{
		Path: "/widgets/{widget_id}",
		Params: []spec.Param{
			{Name: "widget_id", Type: "string", Required: true, Positional: true},
		},
		Body: []spec.Param{{
			Name: "widget", Type: "object", Required: true,
			Fields: []spec.Param{{Name: "widgetId", Type: "string", Required: true}},
		}},
	}, "\t")
	require.Contains(t, got, `!cmd.Flags().Changed("widget-widget-id")`)
	require.Contains(t, got, "bodyWidgetWidgetId = args[0]")
}

func TestBodyPathParamDefaults_UsesSecondPositionalSlot(t *testing.T) {
	t.Parallel()
	got := bodyPathParamDefaults(spec.Endpoint{
		Path: "/orgs/{orgRef}/parents/{parentRef}",
		Params: []spec.Param{
			{Name: "format", Type: "string", Required: true, Positional: true},
			{Name: "parentRef", Type: "string", Required: true, Positional: true},
			{Name: "orgRef", Type: "string", Required: true, Positional: true},
		},
		Body: []spec.Param{
			{Name: "parentRef", Type: "string", Required: true},
			{Name: "orgRef", Type: "string"},
		},
	}, "\t")
	require.Contains(t, got, "bodyParentRef = args[2]")
	require.Contains(t, got, "bodyOrgRef = args[1]")
	assert.NotContains(t, got, "args[0]")
}

func TestBodyPathParamDefaults_AmbiguousNormalizedNamesFillNothing(t *testing.T) {
	t.Parallel()
	got := bodyPathParamDefaults(spec.Endpoint{
		Path: "/a/{parentRef}/b/{parent_ref}/c/{id}",
		Params: []spec.Param{
			{Name: "parentRef", Type: "string", Required: true, Positional: true},
			{Name: "parent_ref", Type: "string", Required: true, Positional: true},
			{Name: "id", Type: "string", Required: true, Positional: true},
		},
		Body: []spec.Param{
			{Name: "parentRef", Type: "string", Required: true},
			{Name: "id", Type: "string", Required: true},
		},
	}, "\t")
	assert.NotContains(t, got, "bodyParentRef")
	require.Contains(t, got, "bodyId = args[2]")
}

func TestBodyPathParamDefaults_SkipsNonStringAndOpaqueLeaves(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		field spec.Param
	}{
		{name: "integer", field: spec.Param{Name: "parentRef", Type: "integer", Required: true}},
		{name: "required bool", field: spec.Param{Name: "parentRef", Type: "boolean", Required: true}},
		{name: "optional bool", field: spec.Param{Name: "parentRef", Type: "boolean"}},
		{name: "json string", field: spec.Param{Name: "parentRef", Type: "string", Format: "json", Required: true}},
		{name: "binary", field: spec.Param{Name: "parentRef", Type: "string", Format: "binary", Required: true}},
		{name: "csv", field: spec.Param{Name: "parentRef", Type: "string_csv_array", Required: true}},
		{name: "object leaf", field: spec.Param{Name: "parentRef", Type: "object", Required: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bodyPathParamDefaults(spec.Endpoint{
				Path:   "/parents/{parentRef}",
				Params: []spec.Param{{Name: "parentRef", Type: "string", Required: true, Positional: true}},
				Body:   []spec.Param{tc.field},
			}, "\t")
			assert.Empty(t, got)
		})
	}
}

func TestBodyPathParamDefaults_DepthCappedObjectIsNotAStringLeaf(t *testing.T) {
	t.Parallel()
	got := bodyPathParamDefaults(spec.Endpoint{
		Path:   "/parents/{parentRef}",
		Params: []spec.Param{{Name: "parentRef", Type: "string", Required: true, Positional: true}},
		Body: []spec.Param{{
			Name: "a", Type: "object",
			Fields: []spec.Param{{
				Name: "b", Type: "object",
				Fields: []spec.Param{{
					Name: "parentRef", Type: "object",
					Fields: []spec.Param{{Name: "x", Type: "string", Required: true}},
				}},
			}},
		}},
	}, "\t")
	assert.Empty(t, got)
}

func TestBodyPathParamDefaults_DeepStringLeafStillMatches(t *testing.T) {
	t.Parallel()
	got := bodyPathParamDefaults(spec.Endpoint{
		Path:   "/parents/{parentRef}",
		Params: []spec.Param{{Name: "parentRef", Type: "string", Required: true, Positional: true}},
		Body: []spec.Param{{
			Name: "a", Type: "object",
			Fields: []spec.Param{{
				Name: "b", Type: "object",
				Fields: []spec.Param{{Name: "parentRef", Type: "string", Required: true}},
			}},
		}},
	}, "\t")
	require.Contains(t, got, "bodyABParentRef = args[0]")
	require.Contains(t, got, `Changed("a-b-parent-ref")`)
}

func TestBodyPathParamDefaults_MatchesWireNameNotFlagName(t *testing.T) {
	t.Parallel()
	path := spec.Endpoint{
		Path:   "/parents/{parentRef}",
		Params: []spec.Param{{Name: "parentRef", Type: "string", Required: true, Positional: true}},
	}
	renamed := path
	renamed.Body = []spec.Param{{Name: "label", BodyName: "parentRef", Type: "string", Required: true}}
	got := bodyPathParamDefaults(renamed, "\t")
	require.Contains(t, got, "bodyLabel = args[0]")

	unrelatedWire := path
	unrelatedWire.Body = []spec.Param{{Name: "parentRef", BodyName: "label", Type: "string", Required: true}}
	assert.Empty(t, bodyPathParamDefaults(unrelatedWire, "\t"))
}

func TestBodyPathParamDefaults_FlagPathParamIsNotASource(t *testing.T) {
	t.Parallel()
	got := bodyPathParamDefaults(spec.Endpoint{
		Path: "/parents/{parentRef}",
		Params: []spec.Param{
			{Name: "parentRef", Type: "string", Required: true, PathParam: true},
		},
		Body: []spec.Param{{Name: "parentRef", Type: "string", Required: true}},
	}, "\t")
	assert.Empty(t, got)
}

func TestBodyPathParamDefaults_EmptyMatchKeySkipped(t *testing.T) {
	t.Parallel()
	got := bodyPathParamDefaults(spec.Endpoint{
		Path:   "/x/{---}",
		Params: []spec.Param{{Name: "---", Type: "string", Required: true, Positional: true}},
		Body:   []spec.Param{{Name: "---", Type: "string", Required: true}},
	}, "\t")
	assert.Empty(t, got)
}

func TestBodyPathParamDefaults_JSONFallbackAndFlatObjectDoNotRecurse(t *testing.T) {
	t.Parallel()
	base := spec.Endpoint{
		Path:   "/parents/{parentRef}",
		Params: []spec.Param{{Name: "parentRef", Type: "string", Required: true, Positional: true}},
		Body: []spec.Param{{
			Name: "child", Type: "object",
			Fields: []spec.Param{{Name: "parentRef", Type: "string", Required: true}},
		}},
	}
	fallback := base
	fallback.BodyJSONFallback = true
	assert.Empty(t, bodyPathParamDefaults(fallback, "\t"))

	for _, contentType := range []string{"multipart/form-data", "application/x-www-form-urlencoded"} {
		flat := base
		flat.RequestContentType = contentType
		got := bodyPathParamDefaults(flat, "\t")
		assert.Empty(t, got, contentType)
	}
}

func TestBodyPathParamDefaults_DedupedTopLevelFlagKeepsWireMatch(t *testing.T) {
	t.Parallel()
	ep, err := dedupeEndpointIdentifiers("uploads", "create", spec.Endpoint{
		Method: "POST",
		Path:   "/buckets/{bucketId}/uploads",
		Params: []spec.Param{
			{Name: "bucketId", Type: "string", Required: true, Positional: true},
		},
		Body: []spec.Param{
			{Name: "bucketId", Type: "string", Required: true, Aliases: []string{"pref"}},
			{Name: "caption", Type: "string", Required: true},
		},
		RequestContentType: "multipart/form-data",
	}, nil, false)
	require.NoError(t, err)
	got := bodyPathParamDefaults(ep, "\t")
	require.Contains(t, got, `!(cmd.Flags().Changed("bucket-id-2") || cmd.Flags().Changed("pref"))`)
	require.Contains(t, got, "bodyBucketId2 = args[0]")
	assert.NotContains(t, got, "bodyCaption")
}

func TestGeneratedBodyDefaultsFromPositionalPathParam(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:        "pathbody",
		Version:     "0.1.0",
		BaseURL:     "https://api.example.com",
		Description: "Path body default fixture",
		Owner:       "test-owner",
		OwnerName:   "Test Author",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			EnvVars: []string{"PATHBODY_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/pathbody-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"children": {
				Description: "Children",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/children", Description: "List children"},
					"create": {
						Method:      "POST",
						Path:        "/orgs/{orgRef}/parents/{parentRef}/children",
						Description: "Create a child",
						Body: []spec.Param{
							{
								Name: "child", Type: "object", Required: true, Description: "Child",
								Fields: []spec.Param{
									{Name: "parentRef", Type: "string", Required: true, Description: "Parent"},
									{Name: "title", Type: "string", Required: true, Description: "Title"},
									{Name: "note", Type: "string", Description: "Note"},
									{Name: "childParentRef", Type: "string", Description: "Not the path param"},
								},
							},
							{
								Name: "meta", Type: "object", Description: "Optional echo",
								Fields: []spec.Param{
									{Name: "parentRef", Type: "string", Description: "Optional parent copy"},
									{Name: "orgRef", Type: "string", Description: "Optional org copy"},
								},
							},
							{
								Name: "stats", Type: "object", Description: "Numeric lookalike",
								Fields: []spec.Param{
									{Name: "parentRef", Type: "integer", Description: "Not a string leaf"},
								},
							},
						},
					},
				},
			},
			"uploads": {
				Description: "Uploads",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/uploads", Description: "List uploads"},
					"create": {
						Method:             "POST",
						Path:               "/buckets/{bucketId}/uploads",
						Description:        "Upload",
						RequestContentType: "multipart/form-data",
						Params: []spec.Param{
							{Name: "bucketId", Type: "string", Required: true, Positional: true, Description: "Bucket"},
						},
						Body: []spec.Param{
							{Name: "bucketId", Type: "string", Required: true, Description: "Bucket copy"},
							{Name: "caption", Type: "string", Required: true, Description: "Caption"},
							{Name: "echo", Type: "string", Description: "Unrelated"},
						},
					},
				},
			},
			"widget-link": {
				Description: "Widget link",
				Endpoints: map[string]spec.Endpoint{
					"create": {
						Method:      "POST",
						Path:        "/widgets/{widget_id}",
						Description: "Link a widget",
						Body: []spec.Param{{
							Name: "widget", Type: "object", Required: true, Description: "Widget",
							Fields: []spec.Param{
								{Name: "widgetId", Type: "string", Required: true, Description: "Widget id"},
								{Name: "name", Type: "string", Required: true, Description: "Name"},
							},
						}},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	childrenSrc := readPathBodyFile(t, filepath.Join(outputDir, "internal", "cli", "children_create.go"))
	assertDefaultBeforeCheck(t, childrenSrc, "bodyChildParentRef = args[1]", "child-parent-ref", "parentRef")
	assertDefaultBeforeCheck(t, childrenSrc, "bodyMetaParentRef = args[1]", "", "parentRef")
	require.Contains(t, childrenSrc, "bodyMetaOrgRef = args[0]")
	assert.NotContains(t, childrenSrc, "bodyChildTitle = args")
	assert.NotContains(t, childrenSrc, "bodyChildNote = args")
	assert.NotContains(t, childrenSrc, "bodyChildChildParentRef = args")
	assert.NotContains(t, childrenSrc, "bodyStatsParentRef = args")

	uploadsSrc := readPathBodyFile(t, filepath.Join(outputDir, "internal", "cli", "uploads_create.go"))
	assertDefaultBeforeCheck(t, uploadsSrc, "bodyBucketId2 = args[0]", "bucket-id-2", "bucketId")
	assert.NotContains(t, uploadsSrc, "bodyCaption = args")
	assert.NotContains(t, uploadsSrc, "bodyEcho = args")

	promotedSrc := readPathBodyFile(t, filepath.Join(outputDir, "internal", "cli", "promoted_widget-link.go"))
	assertDefaultBeforeCheck(t, promotedSrc, "bodyWidgetWidgetId = args[0]", "widget-widget-id", "widget_id")
	assert.NotContains(t, promotedSrc, "bodyWidgetName = args")

	requireGeneratedCompiles(t, outputDir)
	binaryPath := filepath.Join(outputDir, "pathbody-pp-cli")
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/pathbody-pp-cli")

	home := t.TempDir()
	for _, dir := range []string{
		filepath.Join(home, ".config"),
		filepath.Join(home, ".local", "share"),
		filepath.Join(home, ".local", "state"),
		filepath.Join(home, ".cache"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}

	stdout, stderr, err := runPathBodyCLI(t, binaryPath, home, "children", "create", "org-1", "parents/9", "--child-title", "Hello", "--dry-run")
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	body := dryRunBody(t, stderr)
	child := nestedObject(t, body, "child")
	assert.Equal(t, "parents/9", child["parentRef"])
	assert.Equal(t, "Hello", child["title"])
	assert.NotContains(t, child, "note")
	assert.NotContains(t, child, "childParentRef")
	meta := nestedObject(t, body, "meta")
	assert.Equal(t, "parents/9", meta["parentRef"])
	assert.Equal(t, "org-1", meta["orgRef"])
	assert.NotContains(t, body, "stats")
	assert.Contains(t, stderr, "POST https://api.example.com/orgs/org-1/parents/parents%2F9/children")

	stdout, stderr, err = runPathBodyCLI(t, binaryPath, home, "children", "create", "org-1", "parents/9", "--child-title", "Hello", "--child-parent-ref", "other/id", "--dry-run")
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	body = dryRunBody(t, stderr)
	child = nestedObject(t, body, "child")
	assert.Equal(t, "other/id", child["parentRef"])
	meta = nestedObject(t, body, "meta")
	assert.Equal(t, "parents/9", meta["parentRef"])
	assert.Contains(t, stderr, "/parents/parents%2F9/children")
	assert.NotContains(t, stderr, "other%2Fid")

	assertPathBodyCLIError(t, binaryPath, home, `required flag "child-title" not set`, "children", "create", "org-1", "parents/9", "--dry-run")
	assertPathBodyCLIError(t, binaryPath, home, `required flag "child-parent-ref" not set`, "children", "create", "org-1", "", "--child-title", "Hello", "--dry-run")
	assertPathBodyCLIError(t, binaryPath, home, `required flag "child-parent-ref" not set`, "children", "create", "org-1", "--child-title", "Hello", "--dry-run")

	stdout, stderr, err = runPathBodyCLI(t, binaryPath, home, "widget-link", "widget-9", "--widget-name", "Ada", "--dry-run")
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	body = dryRunBody(t, stderr)
	widget := nestedObject(t, body, "widget")
	assert.Equal(t, "widget-9", widget["widgetId"])
	assert.Equal(t, "Ada", widget["name"])
	assert.Contains(t, stderr, "POST https://api.example.com/widgets/widget-9")

	stdout, stderr, err = runPathBodyCLI(t, binaryPath, home, "widget-link", "widget-9", "--widget-name", "Ada", "--widget-widget-id", "other/id", "--dry-run")
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	body = dryRunBody(t, stderr)
	widget = nestedObject(t, body, "widget")
	assert.Equal(t, "other/id", widget["widgetId"])
	assert.Contains(t, stderr, "/widgets/widget-9")

	assertPathBodyCLIError(t, binaryPath, home, `required flag "widget-name" not set`, "widget-link", "widget-9", "--dry-run")

	stdout, stderr, err = runPathBodyCLI(t, binaryPath, home, "uploads", "create", "bucket-1", "--caption", "hi", "--dry-run")
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	body = dryRunBody(t, stderr)
	assert.Equal(t, "bucket-1", body["bucketId"])
	assert.Equal(t, "hi", body["caption"])
	assert.NotContains(t, body, "echo")
	assert.Contains(t, stderr, "/buckets/bucket-1/uploads")

	stdout, stderr, err = runPathBodyCLI(t, binaryPath, home, "uploads", "create", "bucket-1", "--caption", "hi", "--bucket-id-2", "other", "--dry-run")
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	body = dryRunBody(t, stderr)
	assert.Equal(t, "other", body["bucketId"])
	assert.Contains(t, stderr, "/buckets/bucket-1/uploads")

	assertPathBodyCLIError(t, binaryPath, home, `required flag "caption" not set`, "uploads", "create", "bucket-1", "--dry-run")
}

func assertPathBodyCLIError(t *testing.T, binary, home, want string, args ...string) {
	t.Helper()
	stdout, stderr, err := runPathBodyCLI(t, binary, home, args...)
	require.Error(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stdout+stderr, want, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
}

func assertDefaultBeforeCheck(t *testing.T, src, assignment, flag, pathParam string) {
	t.Helper()
	def := strings.Index(src, assignment)
	require.NotEqual(t, -1, def, "missing %s", assignment)
	if flag != "" {
		check := strings.Index(src, `required flag \"%s\" not set", "`+flag+`"`)
		require.NotEqual(t, -1, check, "missing required check for %s", flag)
		assert.Less(t, def, check, "%s must run before the required check for %s", assignment, flag)
	}
	path := strings.Index(src, fmt.Sprintf("replacePathParam(path, %q", pathParam))
	require.NotEqual(t, -1, path, "missing replacePathParam for %s", pathParam)
	assert.Less(t, def, path, "%s must run before replacePathParam(%s)", assignment, pathParam)
}

func readPathBodyFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func runPathBodyCLI(t *testing.T, binary, home string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = decoyHomeEnv(home)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func decoyHomeEnv(home string) []string {
	drop := map[string]bool{
		"HOME": true, "USERPROFILE": true,
		"XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true,
		"XDG_STATE_HOME": true, "XDG_CACHE_HOME": true,
	}
	out := make([]string, 0, len(os.Environ())+6)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && drop[name] {
			continue
		}
		out = append(out, entry)
	}
	return append(out,
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"PATHBODY_TOKEN=test-token",
	)
}

func dryRunBody(t *testing.T, stderr string) map[string]any {
	t.Helper()
	const marker = "Body:\n"
	start := strings.Index(stderr, marker)
	require.NotEqual(t, -1, start, stderr)
	rest := stderr[start+len(marker):]
	var body map[string]any
	require.NoError(t, json.NewDecoder(strings.NewReader(rest)).Decode(&body), rest)
	return body
}

func nestedObject(t *testing.T, body map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := body[key].(map[string]any)
	require.True(t, ok, "body[%s] = %#v", key, body[key])
	return value
}

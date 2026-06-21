package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSyncHelpSearchHintTracksEmittedSearchCommand(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		gql     bool
		search  bool
		wantTip bool
	}{
		{name: "rest without search", search: false, wantTip: false},
		{name: "rest with search", search: true, wantTip: true},
		{name: "graphql without search", gql: true, search: false, wantTip: false},
		{name: "graphql with search", gql: true, search: true, wantTip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := batch3SyncSpec("batch3-"+tc.name, tc.gql)
			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			gen := New(apiSpec, outputDir)
			gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: tc.search}
			require.NoError(t, gen.Generate())

			syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
			require.NoError(t, err)
			assert.Equal(t, tc.wantTip, strings.Contains(string(syncGo), "Once synced, use the 'search' command"),
				"sync help search hint must match VisionSet.Search")

			_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "search.go"))
			if tc.search {
				require.NoError(t, err, "search.go should be emitted when VisionSet.Search is true")
			} else {
				require.ErrorIs(t, err, os.ErrNotExist, "search.go should not be emitted when VisionSet.Search is false")
			}
		})
	}
}

func TestGeneratedTailShortCircuitsFollowUnderDogfood(t *testing.T) {
	t.Parallel()

	apiSpec := batch3SyncSpec("batch3-tail", false)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Tail: true}
	require.NoError(t, gen.Generate())

	tailGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "tail.go"))
	require.NoError(t, err)
	tailSrc := string(tailGo)
	assert.Contains(t, tailSrc, naming.CLI(apiSpec.Name)+`/internal/cliutil"`)
	assert.Contains(t, tailSrc, "if cliutil.IsDogfoodEnv() {\n\t\t\t\tfollow = false\n\t\t\t}")
	assert.Contains(t, tailSrc, "if !follow {\n\t\t\t\treturn nil\n\t\t\t}")
	assert.Less(t, strings.Index(tailSrc, "if !follow {"), strings.Index(tailSrc, "ticker := time.NewTicker(interval)"),
		"tail must return after one poll before creating the follow ticker")

	// An unknown resource must fail fast with a non-zero exit (it would only
	// 404 forever) — and the validation must run before the poll loop binds.
	assert.Contains(t, tailSrc, "for _, r := range tailKnownResources() {")
	assert.Contains(t, tailSrc, `return fmt.Errorf("unknown resource %q; known resources: %v", resource, tailKnownResources())`)
	assert.Less(t, strings.Index(tailSrc, "known resources: %v"), strings.Index(tailSrc, "c, err := flags.newClient()"),
		"tail must reject an unknown resource before constructing the client")
}

func TestGeneratedSyncTreatsArgumentMissing400AsWarning(t *testing.T) {
	t.Parallel()

	apiSpec := batch3SyncSpec("batch3-argument-missing", false)
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		EnvVars: []string{"BATCH3_TOKEN"},
	}
	apiSpec.Config = spec.ConfigSpec{
		Format: "toml",
		Path:   "~/.config/batch3-argument-missing-pp-cli/config.toml",
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	helpersGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	helpersSrc := string(helpersGo)
	assert.Contains(t, helpersSrc, "func looksLikeArgumentMissing(body string) bool")
	assert.Contains(t, helpersSrc, `Reason: "argument_missing"`)

	behaviorTest := fmt.Sprintf(`package cli

import (
	"errors"
	"fmt"
	"testing"

	"%s/internal/client"
)

func TestIsSyncAccessWarningArgumentMissing(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"argument missing", "argument missing: project_id", true},
		{"required field missing", "required parameter project_id is missing", true},
		{"missing required", "missing required filter: task_id", true},
		{"no id provided", "No id to filter notes provided", true},
		{"unrelated bad request", "invalid cursor", false},
		{"required field format invalid", "required field 'email' format is invalid and the avatar is missing", false},
		{"no results provided", "no results were provided by the upstream cache", false},
		{"payment required receipt missing", "Payment required but the receipt is missing", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, ok := isSyncAccessWarning(fmt.Errorf("sync comments: %%w", &client.APIError{Method: "GET", Path: "/comments", StatusCode: 400, Body: tc.body}))
			if ok != tc.want {
				t.Fatalf("ok = %%v, want %%v", ok, tc.want)
			}
			if !tc.want {
				return
			}
			if w.Status != 400 {
				t.Fatalf("status = %%d, want 400", w.Status)
			}
			if w.Reason != "argument_missing" {
				t.Fatalf("reason = %%q, want argument_missing", w.Reason)
			}
		})
	}
	if _, ok := isSyncAccessWarning(errors.New("plain error")); ok {
		t.Fatal("plain error classified as sync warning")
	}
}
`, naming.CLI(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_argument_missing_test.go"), []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestIsSyncAccessWarningArgumentMissing")
}

func batch3SyncSpec(name string, gql bool) *spec.APISpec {
	path := "/items"
	responsePath := ""
	if gql {
		path = "/graphql"
		responsePath = "data.items"
	}
	return &spec.APISpec{
		Name:        name,
		Version:     "0.1.0",
		Description: "Batch 3 template fixture",
		BaseURL:     "https://api.example.com",
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:       "GET",
						Path:         path,
						Description:  "List items",
						ResponsePath: responsePath,
						Response:     spec.ResponseDef{Type: "array"},
						Pagination:   &spec.Pagination{CursorParam: "cursor", LimitParam: "limit"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Item": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "title", Type: "string"},
				},
			},
		},
	}
}

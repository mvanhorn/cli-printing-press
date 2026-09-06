// Copyright 2026 Anthropic, PBC. Licensed under Apache-2.0.

package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSyncResponsePathDedup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		syncableName    string
		nonSyncableName string
	}{
		{name: "syncable sorts first", syncableName: "list", nonSyncableName: "privileges"},
		{name: "non-syncable sorts first", syncableName: "zzz_list", nonSyncableName: "alpha_privileges"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testGenerateSyncResponsePathDedup(t, tt.syncableName, tt.nonSyncableName)
		})
	}
}

func testGenerateSyncResponsePathDedup(t *testing.T, syncableName, nonSyncableName string) {
	t.Helper()

	apiSpec := &spec.APISpec{
		Name:    "dup-path-sample",
		Version: "0.1.0",
		BaseURL: "https://api.example.test/v1",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"DUP_PATH_SAMPLE_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/dup-path-sample-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"roles": {
				Description: "Directory roles",
				Endpoints: map[string]spec.Endpoint{
					syncableName: {
						Method:       "GET",
						Path:         "/customer/{customer}/roles",
						Description:  "List roles",
						Syncable:     true,
						ResponsePath: "items",
						Response:     spec.ResponseDef{Type: "array"},
					},
					nonSyncableName: {
						Method:       "GET",
						Path:         "/customer/{customer}/roles",
						Description:  "List role privileges",
						ResponsePath: "rolePrivileges",
						Response:     spec.ResponseDef{Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)

	caseLabel := `case "roles":`
	got := strings.Count(syncContent, caseLabel)
	assert.Equal(t, 1, got,
		"responsePathForResource must emit the shared resource identity key exactly once; got %d", got)
	assert.NotContains(t, syncContent, `resource + "\x00" + path`,
		"unwrap must not key on the live request path")
	assert.NotContains(t, syncContent, `case "roles\x00/customer/{customer}/roles":`,
		"unwrap cases must not bake the spec request path")

	assert.Contains(t, syncContent, `return []string{"items"}`,
		"syncable endpoint should own the deduped case")
	assert.NotContains(t, syncContent, `return []string{"rolePrivileges"}`,
		"non-syncable endpoint must not own the deduped case")

	requireGeneratedCompiles(t, outputDir)
}

func TestEndpointMatchingSyncRequestPrefersSyncableOnSharedPath(t *testing.T) {
	t.Parallel()

	resource := spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"alpha_privileges": {
				Method:       "GET",
				Path:         "/customer/{customer}/roles",
				ResponsePath: "rolePrivileges",
				Response:     spec.ResponseDef{Type: "array"},
			},
			"zzz_list": {
				Method:       "GET",
				Path:         "/customer/{customer}/roles",
				Syncable:     true,
				ResponsePath: "items",
				Response:     spec.ResponseDef{Type: "array"},
			},
		},
	}

	got, ok := endpointMatchingSyncRequest(resource, "/customer/{customer}/roles", "GET")
	require.True(t, ok)
	assert.Equal(t, "items", strings.TrimSpace(got.ResponsePath),
		"shared Path+Method must pick the Syncable envelope, not the alphabetically first name")
}

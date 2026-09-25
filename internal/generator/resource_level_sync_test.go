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

func TestGeneratedResourceLevelIDFieldAndSyncable(t *testing.T) {
	t.Parallel()

	input := []byte(`name: homeauto
base_url: https://api.example.com
auth:
  type: api_key
  header: Authorization
  format: "Bearer {token}"
  env_vars:
    - HOMEAUTO_API_KEY
cache:
  enabled: true
resources:
  states:
    description: Entity states
    id_field: entity_id
    endpoints:
      list:
        method: GET
        path: /states
        description: List states
        response:
          type: array
          item: State
  overridden:
    description: Overridden identity
    id_field: entity_id
    syncable: false
    endpoints:
      list:
        method: GET
        path: /overridden
        description: List overridden
        id_field: canonical_id
        syncable: true
        response:
          type: array
          item: State
  live:
    description: Live host state
    syncable: false
    endpoints:
      list:
        method: GET
        path: /live
        description: List live
        response:
          type: array
types:
  State:
    fields:
      - name: entity_id
        type: string
      - name: state
        type: string
`)
	apiSpec, err := spec.ParseBytes(input)
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	syncGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	syncContent := string(syncGo)
	storeGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	storeContent := string(storeGo)
	refreshGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auto_refresh.go"))
	require.NoError(t, err)
	refreshContent := string(refreshGo)

	overrideStart := strings.Index(syncContent, `var resourceIDFieldOverrides = map[string]string{`)
	require.GreaterOrEqual(t, overrideStart, 0)
	overrideEnd := strings.Index(syncContent[overrideStart:], "\n}")
	require.Greater(t, overrideEnd, 0)
	overrideBlock := syncContent[overrideStart : overrideStart+overrideEnd]
	assert.Regexp(t, `"states":\s+"entity_id"`, overrideBlock)
	assert.Regexp(t, `"overridden":\s+"canonical_id"`, overrideBlock)
	assert.NotContains(t, overrideBlock, `"live"`)
	assert.Regexp(t, `"states":\s+"entity_id"`, storeContent)
	assert.Regexp(t, `"overridden":\s+"canonical_id"`, storeContent)
	paramStart := strings.Index(storeContent, "var parameterKeyedResources = map[string]bool{")
	require.GreaterOrEqual(t, paramStart, 0)
	paramEnd := strings.Index(storeContent[paramStart:], "\n}")
	require.Greater(t, paramEnd, 0)
	paramBlock := storeContent[paramStart : paramStart+paramEnd]
	assert.NotContains(t, paramBlock, `"states"`)
	assert.NotContains(t, paramBlock, `"overridden"`)

	defaultStart := strings.Index(syncContent, "func defaultSyncResources() []string {")
	require.GreaterOrEqual(t, defaultStart, 0)
	defaultEnd := strings.Index(syncContent[defaultStart:], "\n}")
	require.Greater(t, defaultEnd, 0)
	defaultBlock := syncContent[defaultStart : defaultStart+defaultEnd]
	assert.Contains(t, defaultBlock, `"states"`)
	assert.Contains(t, defaultBlock, `"overridden"`)
	assert.NotContains(t, defaultBlock, `"live"`)

	knownStart := strings.Index(syncContent, "func knownSyncResourceNames() []string {")
	require.GreaterOrEqual(t, knownStart, 0)
	knownEnd := strings.Index(syncContent[knownStart:], "\n}")
	require.Greater(t, knownEnd, 0)
	knownBlock := syncContent[knownStart : knownStart+knownEnd]
	assert.Contains(t, knownBlock, `"states"`)
	assert.Contains(t, knownBlock, `"live"`)

	assert.Contains(t, refreshContent, `"homeauto-pp-cli states"`)
	assert.Contains(t, refreshContent, `"homeauto-pp-cli overridden"`)
	assert.NotContains(t, refreshContent, `"homeauto-pp-cli live"`)
}

package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestParseDestructiveHint(t *testing.T) {
	// Not parallel: captureWarnings swaps a package-global writer.
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "device-safety-destructive.yaml"))
	require.NoError(t, err)
	var parsed *spec.APISpec
	warnings := captureWarnings(t, func() {
		var err error
		parsed, err = Parse(body)
		require.NoError(t, err)
	})

	retire := findParsedEndpointByPath(t, parsed, "POST", "/devices/{id}/retire")
	require.Nil(t, retire.Destructive)
	require.Nil(t, retire.Mutation)

	tag := findParsedEndpointByPath(t, parsed, "POST", "/widgets/{id}/tag")
	require.NotNil(t, tag.Destructive)
	require.False(t, *tag.Destructive)

	cancel := findParsedEndpointByPath(t, parsed, "POST", "/widgets/{id}/cancel")
	require.NotNil(t, cancel.Destructive)
	require.True(t, *cancel.Destructive)

	label := findParsedEndpointByPath(t, parsed, "POST", "/widgets/{id}/label")
	require.Nil(t, label.Destructive, "a string false must not opt into destructiveHint false")

	nudge := findParsedEndpointByPath(t, parsed, "POST", "/widgets/{id}/nudge")
	require.Nil(t, nudge.Destructive, "a non-boolean must not set the hint")

	query := findParsedEndpointByPath(t, parsed, "POST", "/reports/query")
	require.NotNil(t, query.Mutation)
	require.False(t, *query.Mutation)
	require.NotNil(t, query.Destructive)
	require.True(t, *query.Destructive, "the parser keeps an explicit true even when the operation is a read")

	revoke := findParsedEndpointByPath(t, parsed, "POST", "/devices/{id}/sessions/{sessionId}/revoke")
	require.Nil(t, revoke.Destructive)

	require.Contains(t, warnings, "x-pp-destructive must be a boolean")
	require.Contains(t, warnings, "/widgets/{id}/label")
	require.Contains(t, warnings, "/widgets/{id}/nudge")
	lower := strings.ToLower(warnings)
	require.NotContains(t, lower, "/widgets/{id}/tag")
	require.NotContains(t, lower, "/devices/{id}/retire")
}

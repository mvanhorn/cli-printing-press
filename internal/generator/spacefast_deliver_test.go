package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every printed CLI can route output to a live Spacefast page with
// --deliver spacefast:<name>; the sink ships with its own runtime tests.
func TestPrintedCLIEmitsSpacefastDeliverSink(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("sf-sink")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	outputDir := filepath.Join(t.TempDir(), "sf-sink-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	deliver := readGeneratedFile(t, outputDir, "internal", "cli", "deliver.go")
	assert.Contains(t, deliver, `case "spacefast":`)
	assert.Contains(t, deliver, "deliverSpacefast(sink.Target, body)")

	sink := readGeneratedFile(t, outputDir, "internal", "cli", "deliver_spacefast.go")
	assert.Contains(t, sink, "func deliverSpacefast(")
	assert.Contains(t, sink, `"/v1/publish"`)
	assert.Contains(t, sink, "sf-sink-pp-cli/deliver")

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, root, "spacefast:<name> (live web page)")

	readGeneratedFile(t, outputDir, "internal", "cli", "deliver_spacefast_test.go")
	requireGeneratedCompiles(t, outputDir)
}

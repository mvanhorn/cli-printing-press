package generator

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratorMakefileDerivesWindowsBinarySuffix(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("windowsbuild")
	outputDir := filepath.Join(t.TempDir(), "windowsbuild-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}

	require.NoError(t, gen.Generate())

	makefile := readGeneratedFile(t, outputDir, "Makefile")
	require.Contains(t, makefile, `BIN_EXT := $(if $(filter windows,$(shell go env GOOS)),.exe,)`)
	require.Contains(t, makefile, `go build -o bin/windowsbuild-pp-cli$(BIN_EXT) ./cmd/windowsbuild-pp-cli`)
	require.Contains(t, makefile, `go build -o bin/windowsbuild-pp-mcp$(BIN_EXT) ./cmd/windowsbuild-pp-mcp`)
	require.Contains(t, makefile, `go install ./cmd/windowsbuild-pp-cli`)
}

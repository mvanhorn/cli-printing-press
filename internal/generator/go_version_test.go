package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoDirectiveVersionFromRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "patch", version: "go1.26.4", want: "1.26.4"},
		{name: "minor", version: "go1.26", want: "1.26.0"},
		{name: "devel", version: "devel go1.27-abc123", want: "1.27.0"},
		{name: "suffix", version: "go1.26.4 X:cacheprog", want: "1.26.4"},
		{name: "unknown", version: "unknown", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, goDirectiveVersionFromRuntime(tt.version))
		})
	}
}

func TestGeneratedGoModUsesPatchGoDirective(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("patch-directive")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	goMod := readGeneratedFile(t, outputDir, "go.mod")
	assert.Contains(t, goMod, "\ngo "+currentGoDirectiveVersion()+"\n")
	assert.Contains(t, goMod, "\ntoolchain "+currentGoToolchainVersion()+"\n")
	assert.NotContains(t, goMod, "\ngo 1.26\n")
}

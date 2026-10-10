package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestLegacyStoreAdoptFilesFollowHelperGate(t *testing.T) {
	t.Parallel()

	t.Run("store", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("adopt-with-store")
		apiSpec.Learn.Disabled = true
		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
		require.NoError(t, gen.Generate())
		assertStoreAdoptEmission(t, gen, outputDir, true)
		requireGeneratedCompiles(t, outputDir)
	})

	t.Run("no-store", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("adopt-no-store")
		apiSpec.Learn.Disabled = true
		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		gen.VisionSet = VisionTemplateSet{MCP: true}
		require.NoError(t, gen.Generate())
		require.False(t, gen.VisionSet.Store)
		assertStoreAdoptEmission(t, gen, outputDir, false)
		requireGeneratedCompiles(t, outputDir)
	})

	t.Run("store-without-auth", func(t *testing.T) {
		t.Parallel()
		apiSpec := minimalSpec("adopt-no-auth")
		apiSpec.Learn.Disabled = true
		apiSpec.Auth.Type = "none"
		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
		require.NoError(t, gen.Generate())
		assertStoreAdoptEmission(t, gen, outputDir, false)
		requireGeneratedCompiles(t, outputDir)
	})

	t.Run("learn-only-store", func(t *testing.T) {
		t.Parallel()
		apiSpec := postOnlyOutputSpec("adopt-learn-only")
		apiSpec.Learn.Enabled = true
		outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
		gen := New(apiSpec, outputDir)
		require.NoError(t, gen.Generate())
		require.True(t, gen.VisionSet.Store)
		require.False(t, gen.hasDataLayer(), "learn-only promotion must keep the helper on HasStorePath")
		assertStoreAdoptEmission(t, gen, outputDir, true)
		requireGeneratedCompiles(t, outputDir)
	})
}

func assertStoreAdoptEmission(t *testing.T, gen *Generator, outputDir string, want bool) {
	t.Helper()
	require.Equal(t, want, gen.emitsLegacyStoreAdoption())

	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Equal(t, want, strings.Contains(helpers, "func legacyStoreAdoptPaths("))
	require.Equal(t, want, strings.Contains(helpers, "claimClosedStore("))

	for _, name := range []string{
		"store_adopt_linux.go",
		"store_adopt_darwin.go",
		"store_adopt_windows.go",
		"store_adopt_other.go",
	} {
		path := filepath.Join(outputDir, "internal", "cli", name)
		if want {
			require.FileExists(t, path)
			continue
		}
		require.NoFileExists(t, path)
	}
	if want {
		linux := readGeneratedFile(t, outputDir, "internal", "cli", "store_adopt_linux.go")
		require.Contains(t, linux, "legacyStoreAdoptPaths(legacy)")
	}
}

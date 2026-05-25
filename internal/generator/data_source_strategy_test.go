package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedReadCommandUsesDataSourceStrategy(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("strategy")
	items := apiSpec.Resources["items"]
	endpoint := items.Endpoints["list"]
	endpoint.DataSourceStrategy = spec.DataSourceStrategyLocal
	items.Endpoints["list"] = endpoint
	apiSpec.Resources["items"] = items

	outputDir := filepath.Join(t.TempDir(), "strategy-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	dataSourceSrc := readGeneratedFile(t, outputDir, "internal", "cli", "data_source.go")
	require.Contains(t, dataSourceSrc, "func resolveReadWithStrategy(")
	require.Contains(t, dataSourceSrc, "no live equivalent for this command")

	commandSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	require.Contains(t, commandSrc, `resolveReadWithStrategy(cmd.Context(), c, flags, "local", "items"`)
}

func TestGeneratedLiveReadUsesStrategyAwareResolver(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("live-strategy")
	items := apiSpec.Resources["items"]
	endpoint := items.Endpoints["list"]
	endpoint.DataSourceStrategy = spec.DataSourceStrategyLive
	items.Endpoints["list"] = endpoint
	apiSpec.Resources["items"] = items

	outputDir := filepath.Join(t.TempDir(), "live-strategy-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	dataSourceSrc := readGeneratedFile(t, outputDir, "internal", "cli", "data_source.go")
	require.Contains(t, dataSourceSrc, "no local data source for this command")

	commandSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	require.Contains(t, commandSrc, `resolveReadWithStrategy(cmd.Context(), c, flags, "live", "items"`)
}

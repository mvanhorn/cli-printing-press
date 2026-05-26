package generator

import (
	"os"
	"path/filepath"
	"strings"
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
	require.Contains(t, dataSourceSrc, `no live equivalent for this command (requested %q)`)

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
	require.Contains(t, dataSourceSrc, `no local data source for this command (requested %q)`)

	commandSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	require.Contains(t, commandSrc, `resolveReadWithStrategy(cmd.Context(), c, flags, "live", "items"`)
}

func TestGeneratedGraphQLListValidatesLocalStrategyBeforeLocalDispatch(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("graphql-local-strategy")
	apiSpec.BaseURL = "https://api.example.com/graphql"
	items := apiSpec.Resources["items"]
	endpoint := items.Endpoints["list"]
	endpoint.Method = "GET"
	endpoint.Path = "/graphql"
	endpoint.DataSourceStrategy = spec.DataSourceStrategyLocal
	items.Endpoints["list"] = endpoint
	items.Endpoints["get"] = spec.Endpoint{
		Method:      "GET",
		Path:        "/graphql",
		Description: "Get item",
		Params: []spec.Param{{
			Name:        "id",
			Type:        "string",
			Required:    true,
			Positional:  true,
			Description: "Item ID",
		}},
	}
	apiSpec.Resources["items"] = items

	outputDir := filepath.Join(t.TempDir(), "graphql-local-strategy-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	cliFiles, err := os.ReadDir(filepath.Join(outputDir, "internal", "cli"))
	require.NoError(t, err)
	var commandSrc string
	for _, file := range cliFiles {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") {
			continue
		}
		src := readGeneratedFile(t, outputDir, "internal", "cli", file.Name())
		if strings.Contains(src, `flags.dataSource == "local" || "local" == "local"`) {
			commandSrc = src
			break
		}
	}
	require.NotEmpty(t, commandSrc)
	validateIdx := strings.Index(commandSrc, `validateDataSourceStrategy(flags, "local")`)
	localDispatchIdx := strings.Index(commandSrc, `flags.dataSource == "local" || "local" == "local"`)
	require.NotEqual(t, -1, validateIdx)
	require.NotEqual(t, -1, localDispatchIdx)
	require.Less(t, validateIdx, localDispatchIdx)
}

func TestGeneratedGraphQLGetHonorsLocalStrategy(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("graphql-get-local-strategy")
	apiSpec.BaseURL = "https://api.example.com/graphql"
	items := apiSpec.Resources["items"]
	listEndpoint := items.Endpoints["list"]
	listEndpoint.Method = "GET"
	listEndpoint.Path = "/graphql"
	items.Endpoints["list"] = listEndpoint
	items.Endpoints["get"] = spec.Endpoint{
		Method:             "GET",
		Path:               "/graphql",
		Description:        "Get item",
		DataSourceStrategy: spec.DataSourceStrategyLocal,
		Params: []spec.Param{{
			Name:        "id",
			Type:        "string",
			Required:    true,
			Positional:  true,
			Description: "Item ID",
		}},
	}
	apiSpec.Resources["items"] = items

	storeOutputDir := filepath.Join(t.TempDir(), "graphql-get-local-strategy-store-pp-cli")
	storeGen := New(apiSpec, storeOutputDir)
	storeGen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, storeGen.Generate())

	storeGetSrc := readGeneratedFile(t, storeOutputDir, "internal", "cli", "promoted_items.go")
	require.Contains(t, storeGetSrc, `resolveReadWithStrategy(cmd.Context(), c, flags, "local", "items", false`)

	noStoreOutputDir := filepath.Join(t.TempDir(), "graphql-get-local-strategy-nostore-pp-cli")
	noStoreGen := New(apiSpec, noStoreOutputDir)
	noStoreGen.VisionSet = VisionTemplateSet{Export: true}
	require.NoError(t, noStoreGen.Generate())

	noStoreGetSrc := readGeneratedFile(t, noStoreOutputDir, "internal", "cli", "promoted_items.go")
	require.Contains(t, noStoreGetSrc, `data_source_strategy local requires the local store data layer`)
}

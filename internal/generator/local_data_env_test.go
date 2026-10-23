package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratedLocalDataEnvSwitches runs emitted-code tests for the
// NO_LOCAL_DATA and DATA_SOURCE env switches, including MCP child inheritance.
func TestGeneratedLocalDataEnvSwitches(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "local-env-pp-cli")
	gen := New(minimalSpec("local-env"), outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	const cliTest = `package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runRoot(t *testing.T, flags *rootFlags, args ...string) error {
	t.Helper()
	root := newRootCmd(flags)
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)
	return root.Execute()
}

func TestNoLocalDataForcesLiveReadsAndBlocksStoreWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv(noLocalDataEnvVar, "1")
	t.Setenv(dataSourceEnvVar, "local")

	flags := &rootFlags{}
	if err := runRoot(t, flags, "version", "--home", home); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !flags.noCache || flags.dataSource != "live" {
		t.Fatalf("noCache=%v dataSource=%q, want true/live", flags.noCache, flags.dataSource)
	}
	writeMutationResponseToStore(context.Background(), "items", []byte(` + "`" + `{"id":"1"}` + "`" + `), "")
	if matches, _ := filepath.Glob(filepath.Join(home, "data", "data*.db")); len(matches) != 0 {
		t.Fatalf("mutation write-through created %v under the switch", matches)
	}
	if err := runRoot(t, &rootFlags{}, "version", "--data-source", "auto"); err == nil || !strings.Contains(err.Error(), noLocalDataEnvVar) {
		t.Fatalf("explicit --data-source auto under the switch: err=%v", err)
	}
	flags = &rootFlags{}
	if err := runRoot(t, flags, "version", "--data-source", "local"); err != nil || flags.dataSource != "local" {
		t.Fatalf("explicit local read under the switch: dataSource=%q err=%v", flags.dataSource, err)
	}
	for _, args := range [][]string{{"sync"}, {"workflow", "archive"}} {
		if err := runRoot(t, &rootFlags{}, args...); err == nil || !strings.Contains(err.Error(), "is disabled while") {
			t.Fatalf("%v under the switch: err=%v", args, err)
		}
	}
	root := &cobra.Command{Use: "local-env-pp-cli"}
	localOnly := &cobra.Command{Use: "digest", Annotations: map[string]string{"pp:data-source": "local"}}
	root.AddCommand(localOnly)
	flags = &rootFlags{dataSource: "auto"}
	if err := applyLocalDataEnv(localOnly, flags); err != nil || flags.dataSource != "local" {
		t.Fatalf("local-only command under the switch: dataSource=%q err=%v", flags.dataSource, err)
	}
}

func TestDataSourceEnvSetsDefault(t *testing.T) {
	t.Setenv(dataSourceEnvVar, "local")
	flags := &rootFlags{}
	if err := runRoot(t, flags, "version"); err != nil || flags.dataSource != "local" {
		t.Fatalf("dataSource=%q err=%v, want local", flags.dataSource, err)
	}
	flags = &rootFlags{}
	if err := runRoot(t, flags, "version", "--data-source", "live"); err != nil || flags.dataSource != "live" {
		t.Fatalf("explicit flag lost to env: dataSource=%q err=%v", flags.dataSource, err)
	}
	t.Setenv(dataSourceEnvVar, "remote")
	if err := runRoot(t, &rootFlags{}, "version"); err == nil || !strings.Contains(err.Error(), dataSourceEnvVar) {
		t.Fatalf("invalid env value accepted: err=%v", err)
	}
}
`
	const mcpTest = `package cobratree

import (
	"slices"
	"testing"
)

func TestMCPChildInheritsNoLocalData(t *testing.T) {
	t.Setenv("LOCAL_ENV_NO_LOCAL_DATA", "1")
	if !slices.Contains(mcpChildEnviron(), "LOCAL_ENV_NO_LOCAL_DATA=1") {
		t.Fatal("MCP-spawned CLI children must inherit the local-data switch")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "local_data_env_runtime_test.go"), []byte(cliTest), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "cobratree", "local_data_env_runtime_test.go"), []byte(mcpTest), 0o600))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "./internal/mcp/cobratree", "-run", "^Test(NoLocalData|DataSourceEnv|MCPChildInheritsNoLocalData)", "-count=1")

	// Learn-disabled, store-less prints must still define the switch.
	minimalSpecNoLearn := minimalSpec("local-env-min")
	minimalSpecNoLearn.Learn.Disabled = true
	minimal := filepath.Join(t.TempDir(), "local-env-min-pp-cli")
	require.NoError(t, New(minimalSpecNoLearn, minimal).Generate())
	requireGeneratedCompiles(t, minimal)
}

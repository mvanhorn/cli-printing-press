package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateFromPlan_BasicScaffold(t *testing.T) {
	t.Parallel()

	planSpec := &PlanSpec{
		CLIName:     "screencap",
		Description: "Screen capture CLI tool",
		Commands: []PlanCommand{
			{Name: "record", Description: "Record screen capture"},
			{Name: "screenshot", Description: "Take a screenshot"},
			{Name: "gif", Description: "Convert recording to GIF"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(planSpec.CLIName))
	require.NoError(t, os.MkdirAll(outputDir, 0o755))

	err := GenerateFromPlan(planSpec, outputDir)
	require.NoError(t, err)

	// Verify expected files exist
	expectedFiles := []string{
		filepath.Join("cmd", naming.CLI("screencap"), "main.go"),
		filepath.Join("internal", "cli", "root.go"),
		filepath.Join("internal", "cli", "helpers.go"),
		filepath.Join("internal", "cli", "doctor.go"),
		filepath.Join("internal", "cli", "record.go"),
		filepath.Join("internal", "cli", "screenshot.go"),
		filepath.Join("internal", "cli", "gif.go"),
		"go.mod",
		"go.sum",
	}

	for _, f := range expectedFiles {
		fullPath := filepath.Join(outputDir, f)
		_, err := os.Stat(fullPath)
		assert.NoError(t, err, "expected file %s to exist", f)
	}

	// Verify go.mod contains the correct module path
	goMod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(goMod), naming.CLI("screencap"))

	// Verify root.go contains command registrations
	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	rootContent := string(rootGo)
	assert.Contains(t, rootContent, "newRecordCmd()")
	assert.Contains(t, rootContent, "newScreenshotCmd()")
	assert.Contains(t, rootContent, "newGifCmd()")
	assert.Contains(t, rootContent, "newDoctorCmd()")

	// Verify a stub command has the "not implemented" error
	recordGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "record.go"))
	require.NoError(t, err)
	assert.Contains(t, string(recordGo), "not implemented")

	// Verify it compiles
	buildCmd := exec.Command("go", "build", "./...")
	buildCmd.Dir = outputDir
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", string(buildOut))
}

func TestGenerateFromPlan_WithSubcommands(t *testing.T) {
	t.Parallel()

	planSpec := &PlanSpec{
		CLIName:     "devtool",
		Description: "Developer tooling CLI",
		Commands: []PlanCommand{
			{Name: "auth login", Description: "Log in to your account"},
			{Name: "auth logout", Description: "Log out of your account"},
			{Name: "deploy", Description: "Deploy application"},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(planSpec.CLIName))
	require.NoError(t, os.MkdirAll(outputDir, 0o755))

	err := GenerateFromPlan(planSpec, outputDir)
	require.NoError(t, err)

	// Verify parent command file exists
	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	assert.NoError(t, err, "expected auth.go parent command file")

	// Verify subcommand files exist
	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "auth_login.go"))
	assert.NoError(t, err, "expected auth_login.go")
	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "auth_logout.go"))
	assert.NoError(t, err, "expected auth_logout.go")

	// Verify top-level command exists
	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "deploy.go"))
	assert.NoError(t, err, "expected deploy.go")

	// Verify root.go references the parent command, not individual subcommands
	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	rootContent := string(rootGo)
	assert.Contains(t, rootContent, "newAuthCmd()")
	assert.Contains(t, rootContent, "newDeployCmd()")

	// Verify it compiles
	buildCmd := exec.Command("go", "build", "./...")
	buildCmd.Dir = outputDir
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", string(buildOut))
}

func TestGenerateFromPlan_EmptyName(t *testing.T) {
	t.Parallel()

	planSpec := &PlanSpec{
		CLIName:  "",
		Commands: []PlanCommand{{Name: "run", Description: "Run it"}},
	}

	outputDir := t.TempDir()
	err := GenerateFromPlan(planSpec, outputDir)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "no CLI name"))
}

func TestPartitionCommands(t *testing.T) {
	t.Parallel()

	commands := []PlanCommand{
		{Name: "auth login", Description: "Log in"},
		{Name: "auth logout", Description: "Log out"},
		{Name: "deploy", Description: "Deploy"},
		{Name: "config set", Description: "Set config value"},
		{Name: "config get", Description: "Get config value"},
	}

	topLevel, parents := partitionCommands(commands)

	assert.Len(t, topLevel, 1)
	assert.Equal(t, "deploy", topLevel[0].Name)

	assert.Len(t, parents, 2)
	// Parents should be sorted
	assert.Equal(t, "auth", parents[0].Name)
	assert.Len(t, parents[0].SubCommands, 2)
	assert.Equal(t, "config", parents[1].Name)
	assert.Len(t, parents[1].SubCommands, 2)
}

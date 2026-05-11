package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanClientEnvReads(t *testing.T) {
	t.Run("returns nil when internal/client dir missing", func(t *testing.T) {
		got, err := scanClientEnvReads(t.TempDir())
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("returns sorted dedup set of os.Getenv args", func(t *testing.T) {
		dir := t.TempDir()
		writeClientFile(t, dir, "client.go", `package client

import "os"

func mintToken() string {
	id := os.Getenv("FEDEX_API_KEY")
	if id == "" {
		id = os.Getenv("FEDEX_API_KEY")
	}
	_ = os.Getenv("FEDEX_SECRET_KEY")
	return id
}
`)
		writeClientFile(t, dir, "auth_refresh.go", `package client

import "os"

func tryRefresh() (string, string) {
	return os.Getenv("RENTALWORKS_HOME_USERNAME"), os.Getenv("RENTALWORKS_HOME_PASSWORD")
}
`)

		got, err := scanClientEnvReads(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{
			"FEDEX_API_KEY",
			"FEDEX_SECRET_KEY",
			"RENTALWORKS_HOME_PASSWORD",
			"RENTALWORKS_HOME_USERNAME",
		}, got)
	})

	t.Run("skips non-string-literal Getenv args", func(t *testing.T) {
		dir := t.TempDir()
		writeClientFile(t, dir, "client.go", `package client

import "os"

const tokenVar = "X_TOKEN"

func read(name string) string {
	_ = os.Getenv(name)      // variable arg — skip
	_ = os.Getenv(tokenVar)  // identifier arg — skip
	return os.Getenv("X_API_KEY")
}
`)
		got, err := scanClientEnvReads(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"X_API_KEY"}, got)
	})

	t.Run("ignores non-os Getenv calls and unrelated calls", func(t *testing.T) {
		dir := t.TempDir()
		writeClientFile(t, dir, "client.go", `package client

type fake struct{}

func (f fake) Getenv(s string) string { return s }

func read() string {
	f := fake{}
	_ = f.Getenv("LOOKS_LIKE_GETENV")
	return ""
}
`)
		got, err := scanClientEnvReads(dir)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("skips files that fail to parse without aborting", func(t *testing.T) {
		dir := t.TempDir()
		writeClientFile(t, dir, "broken.go", `package client
this is not go`)
		writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("GOOD_VAR") }
`)
		got, err := scanClientEnvReads(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"GOOD_VAR"}, got)
	})

	t.Run("ignores non-go files and subdirs", func(t *testing.T) {
		dir := t.TempDir()
		clientDir := filepath.Join(dir, "internal", "client")
		require.NoError(t, os.MkdirAll(filepath.Join(clientDir, "sub"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(clientDir, "notes.txt"), []byte("os.Getenv(\"IGNORE_ME\")"), 0o644))
		writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("PICK_ME") }
`)
		got, err := scanClientEnvReads(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"PICK_ME"}, got)
	})
}

func TestReconcileMCPBManifestFromClient(t *testing.T) {
	t.Run("no manifest file is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, reconcileMCPBManifestFromClient(dir))
	})

	t.Run("no client dir leaves manifest unchanged", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{APIName: "noop", MCPBinary: "noop-pp-mcp", AuthType: "api_key"})
		writeMCPBManifest(t, dir, MCPBManifest{
			Name: "noop-pp-mcp",
			Server: MCPBServer{
				MCPConfig: MCPBLaunchSpec{Env: map[string]string{"NOOP_API_KEY": "${user_config.noop_api_key}"}},
			},
			UserConfig: map[string]MCPBVar{
				"noop_api_key": {Type: "string", Title: "NOOP_API_KEY", Required: true, Sensitive: true},
			},
		})

		require.NoError(t, reconcileMCPBManifestFromClient(dir))

		got := readMCPBManifest(t, dir)
		assert.Equal(t, map[string]string{"NOOP_API_KEY": "${user_config.noop_api_key}"}, got.Server.MCPConfig.Env)
		assert.Len(t, got.UserConfig, 1)
	})

	t.Run("declared env vars are skipped", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:     "stripe",
			DisplayName: "Stripe",
			MCPBinary:   "stripe-pp-mcp",
			AuthType:    "api_key",
		})
		writeMCPBManifest(t, dir, MCPBManifest{
			Name: "stripe-pp-mcp",
			Server: MCPBServer{
				MCPConfig: MCPBLaunchSpec{Env: map[string]string{"STRIPE_API_KEY": "${user_config.stripe_api_key}"}},
			},
			UserConfig: map[string]MCPBVar{
				"stripe_api_key": {Type: "string", Title: "STRIPE_API_KEY", Required: true, Sensitive: true},
			},
		})
		writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("STRIPE_API_KEY") }
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir))

		got := readMCPBManifest(t, dir)
		assert.Len(t, got.Server.MCPConfig.Env, 1)
		assert.Len(t, got.UserConfig, 1)
	})

	t.Run("adds sensitive user_config for undeclared env reads on required-credential auth", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:     "rentalworks-home",
			DisplayName: "RentalWorks Home",
			MCPBinary:   "rentalworks-home-pp-mcp",
			AuthType:    "bearer_token",
			AuthEnvVars: []string{"RENTALWORKS_HOME_TOKEN"},
		})
		writeMCPBManifest(t, dir, MCPBManifest{
			Name: "rentalworks-home-pp-mcp",
			Server: MCPBServer{
				MCPConfig: MCPBLaunchSpec{Env: map[string]string{"RENTALWORKS_HOME_TOKEN": "${user_config.rentalworks_home_token}"}},
			},
			UserConfig: map[string]MCPBVar{
				"rentalworks_home_token": {Type: "string", Title: "RENTALWORKS_HOME_TOKEN", Required: true, Sensitive: true},
			},
		})
		writeClientFile(t, dir, "auth_refresh.go", `package client

import "os"

func refresh() (string, string) {
	return os.Getenv("RENTALWORKS_HOME_USERNAME"), os.Getenv("RENTALWORKS_HOME_PASSWORD")
}
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir))

		got := readMCPBManifest(t, dir)
		assert.Equal(t, "${user_config.rentalworks_home_token}", got.Server.MCPConfig.Env["RENTALWORKS_HOME_TOKEN"])
		assert.Equal(t, "${user_config.rentalworks_home_username}", got.Server.MCPConfig.Env["RENTALWORKS_HOME_USERNAME"])
		assert.Equal(t, "${user_config.rentalworks_home_password}", got.Server.MCPConfig.Env["RENTALWORKS_HOME_PASSWORD"])

		username, ok := got.UserConfig["rentalworks_home_username"]
		require.True(t, ok)
		assert.Equal(t, "RENTALWORKS_HOME_USERNAME", username.Title)
		assert.Equal(t, "string", username.Type)
		assert.True(t, username.Sensitive)
		assert.True(t, username.Required, "credential-required bearer_token auth must propagate Required to discovered fields")
		assert.Contains(t, username.Description, "RentalWorks Home")
		assert.Contains(t, username.Description, "credential refresh")

		password, ok := got.UserConfig["rentalworks_home_password"]
		require.True(t, ok)
		assert.True(t, password.Sensitive)
		assert.True(t, password.Required)
	})

	t.Run("optional auth keeps discovered fields optional", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:      "recipe-goat",
			DisplayName:  "Recipe Goat",
			MCPBinary:    "recipe-goat-pp-mcp",
			AuthType:     "api_key",
			AuthOptional: true,
		})
		writeMCPBManifest(t, dir, MCPBManifest{
			Name:   "recipe-goat-pp-mcp",
			Server: MCPBServer{MCPConfig: MCPBLaunchSpec{Env: map[string]string{}}},
		})
		writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("RECIPE_EXTRA_SECRET") }
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir))

		got := readMCPBManifest(t, dir)
		entry, ok := got.UserConfig["recipe_extra_secret"]
		require.True(t, ok)
		assert.False(t, entry.Required, "AuthOptional=true must mark discovered fields optional")
		assert.True(t, entry.Sensitive)
		assert.Contains(t, entry.Description, "Optional.")
	})

	t.Run("composed auth marks discovered fields optional", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{
			APIName:   "pizza",
			MCPBinary: "pizza-pp-mcp",
			AuthType:  "composed",
		})
		writeMCPBManifest(t, dir, MCPBManifest{
			Name:   "pizza-pp-mcp",
			Server: MCPBServer{MCPConfig: MCPBLaunchSpec{Env: map[string]string{}}},
		})
		writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("PIZZA_HIDDEN_TOKEN") }
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir))

		got := readMCPBManifest(t, dir)
		entry, ok := got.UserConfig["pizza_hidden_token"]
		require.True(t, ok)
		assert.False(t, entry.Required, "composed auth keeps user_config optional")
	})

	t.Run("manifest with nil env/userconfig maps gets populated", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, CLIManifest{APIName: "x", MCPBinary: "x-pp-mcp", AuthType: "api_key"})
		writeMCPBManifest(t, dir, MCPBManifest{
			Name:   "x-pp-mcp",
			Server: MCPBServer{},
		})
		writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("X_HIDDEN") }
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir))

		got := readMCPBManifest(t, dir)
		assert.Equal(t, "${user_config.x_hidden}", got.Server.MCPConfig.Env["X_HIDDEN"])
		_, ok := got.UserConfig["x_hidden"]
		assert.True(t, ok)
	})
}

func writeClientFile(t *testing.T, dir, name, content string) {
	t.Helper()
	clientDir := filepath.Join(dir, "internal", "client")
	require.NoError(t, os.MkdirAll(clientDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(clientDir, name), []byte(content), 0o644))
}

// Sanity check that MCPBVar json round-trips the new Sensitive+Required flags.
func TestMCPBVarRoundtripFlags(t *testing.T) {
	in := MCPBVar{Type: "string", Title: "X", Sensitive: true, Required: true}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	var out MCPBVar
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out)
}

package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanClientEnvReads(t *testing.T) {
	t.Run("returns empty when client and config dirs missing", func(t *testing.T) {
		got, err := scanClientEnvReads(t.TempDir())
		require.NoError(t, err)
		assert.Empty(t, got)
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

	t.Run("logs but continues past files that fail to parse", func(t *testing.T) {
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

	t.Run("includes os.Getenv reads from internal/config", func(t *testing.T) {
		dir := t.TempDir()
		writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() {
	if v := os.Getenv("HUDU_BASE_URL"); v != "" {
		_ = v
	}
	_ = os.Getenv("HUDU_API_KEY")
	_ = os.Getenv("HUDU_CONFIG")
	_ = os.Getenv("PRINTING_PRESS_VERIFY")
}
`)
		got, err := scanClientEnvReads(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"HUDU_API_KEY", "HUDU_BASE_URL"}, got,
			"config-package BASE_URL must be declared; config-file path and harness flags must not")
	})

	t.Run("skips Getenv calls in _test.go files", func(t *testing.T) {
		dir := t.TempDir()
		writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() { _ = os.Getenv("HUDU_API_KEY") }
`)
		writeConfigFile(t, dir, "config_perms_test.go", `package config

import "os"

func helper() { _ = os.Getenv("USERNAME") }
`)
		got, err := scanClientEnvReads(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"HUDU_API_KEY"}, got)
	})

	t.Run("unions client and config env reads", func(t *testing.T) {
		dir := t.TempDir()
		writeClientFile(t, dir, "client.go", `package client

import "os"

func refresh() string { return os.Getenv("HUDU_REFRESH_SECRET") }
`)
		writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() string { return os.Getenv("HUDU_BASE_URL") }
`)
		got, err := scanClientEnvReads(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"HUDU_BASE_URL", "HUDU_REFRESH_SECRET"}, got)
	})
}

func TestReconcileMCPBManifestFromClient(t *testing.T) {
	t.Run("no manifest file is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, reconcileMCPBManifestFromClient(dir, CLIManifest{}))
	})

	t.Run("no client dir leaves manifest unchanged", func(t *testing.T) {
		dir := t.TempDir()
		cli := CLIManifest{APIName: "noop", MCPBinary: "noop-pp-mcp", AuthType: "api_key"}
		writeMCPBManifest(t, dir, MCPBManifest{
			Name: "noop-pp-mcp",
			Server: MCPBServer{
				MCPConfig: MCPBLaunchSpec{Env: map[string]string{"NOOP_API_KEY": "${user_config.noop_api_key}"}},
			},
			UserConfig: map[string]MCPBVar{
				"noop_api_key": {Type: "string", Title: "NOOP_API_KEY", Required: true, Sensitive: true},
			},
		})

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		assert.Equal(t, map[string]string{"NOOP_API_KEY": "${user_config.noop_api_key}"}, got.Server.MCPConfig.Env)
		assert.Len(t, got.UserConfig, 1)
	})

	t.Run("declared env vars are skipped", func(t *testing.T) {
		dir := t.TempDir()
		cli := CLIManifest{
			APIName:     "stripe",
			DisplayName: "Stripe",
			MCPBinary:   "stripe-pp-mcp",
			AuthType:    "api_key",
		}
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

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		assert.Len(t, got.Server.MCPConfig.Env, 1)
		assert.Len(t, got.UserConfig, 1)
	})

	t.Run("adds sensitive user_config for undeclared env reads on required-credential auth", func(t *testing.T) {
		dir := t.TempDir()
		cli := CLIManifest{
			APIName:     "rentalworks-home",
			DisplayName: "RentalWorks Home",
			MCPBinary:   "rentalworks-home-pp-mcp",
			AuthType:    "bearer_token",
			AuthEnvVars: []string{"RENTALWORKS_HOME_TOKEN"},
		}
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

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

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
		assert.NotContains(t, username.Description, "Optional.", "required-auth descriptions must not carry the Optional prefix")

		password, ok := got.UserConfig["rentalworks_home_password"]
		require.True(t, ok)
		assert.True(t, password.Sensitive)
		assert.True(t, password.Required)
	})

	t.Run("optional auth keeps discovered fields optional", func(t *testing.T) {
		dir := t.TempDir()
		cli := CLIManifest{
			APIName:      "recipe-goat",
			DisplayName:  "Recipe Goat",
			MCPBinary:    "recipe-goat-pp-mcp",
			AuthType:     "api_key",
			AuthOptional: true,
		}
		writeMCPBManifest(t, dir, MCPBManifest{
			Name:   "recipe-goat-pp-mcp",
			Server: MCPBServer{MCPConfig: MCPBLaunchSpec{Env: map[string]string{}}},
		})
		writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("RECIPE_EXTRA_SECRET") }
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		entry, ok := got.UserConfig["recipe_extra_secret"]
		require.True(t, ok)
		assert.False(t, entry.Required, "AuthOptional=true must mark discovered fields optional")
		assert.True(t, entry.Sensitive)
		assert.Contains(t, entry.Description, "Optional.")
	})

	t.Run("composed auth marks discovered fields optional", func(t *testing.T) {
		dir := t.TempDir()
		cli := CLIManifest{
			APIName:   "pizza",
			MCPBinary: "pizza-pp-mcp",
			AuthType:  "composed",
		}
		writeMCPBManifest(t, dir, MCPBManifest{
			Name:   "pizza-pp-mcp",
			Server: MCPBServer{MCPConfig: MCPBLaunchSpec{Env: map[string]string{}}},
		})
		writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("PIZZA_HIDDEN_TOKEN") }
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		entry, ok := got.UserConfig["pizza_hidden_token"]
		require.True(t, ok)
		assert.False(t, entry.Required, "composed auth keeps user_config optional")
	})

	t.Run("discovered User-Agent override is optional and non-sensitive regardless of auth", func(t *testing.T) {
		dir := t.TempDir()
		cli := CLIManifest{
			APIName:     "espn",
			DisplayName: "ESPN",
			MCPBinary:   "espn-pp-mcp",
			AuthType:    "bearer_token",
			AuthEnvVars: []string{"ESPN_TOKEN"},
		}
		writeMCPBManifest(t, dir, MCPBManifest{
			Name: "espn-pp-mcp",
			Server: MCPBServer{
				MCPConfig: MCPBLaunchSpec{Env: map[string]string{"ESPN_TOKEN": "${user_config.espn_token}"}},
			},
			UserConfig: map[string]MCPBVar{
				"espn_token": {Type: "string", Title: "ESPN_TOKEN", Required: true, Sensitive: true},
			},
		})
		writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("ESPN_USER_AGENT") }
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		entry, ok := got.UserConfig["espn_user_agent"]
		require.True(t, ok)
		assert.False(t, entry.Required, "User-Agent override must stay optional even when base auth requires a credential")
		assert.False(t, entry.Sensitive, "User-Agent override is not a secret")
		assert.Contains(t, entry.Description, "not a credential")
		assert.NotContains(t, entry.Description, "credential refresh")
	})

	t.Run("adds optional non-sensitive user_config for config-package BASE_URL", func(t *testing.T) {
		dir := t.TempDir()
		cli := CLIManifest{
			APIName:     "hudu",
			DisplayName: "Hudu",
			MCPBinary:   "hudu-pp-mcp",
			AuthType:    "api_key",
			AuthEnvVars: []string{"HUDU_API_KEY"},
		}
		writeMCPBManifest(t, dir, MCPBManifest{
			Name: "hudu-pp-mcp",
			Server: MCPBServer{
				MCPConfig: MCPBLaunchSpec{Env: map[string]string{"HUDU_API_KEY": "${user_config.hudu_api_key}"}},
			},
			UserConfig: map[string]MCPBVar{
				"hudu_api_key": {Type: "string", Title: "HUDU_API_KEY", Required: true, Sensitive: true},
			},
		})
		writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() {
	_ = os.Getenv("HUDU_API_KEY")
	if v := os.Getenv("HUDU_BASE_URL"); v != "" {
		_ = v
	}
	_ = os.Getenv("HUDU_CONFIG")
}
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		assert.Equal(t, "${user_config.hudu_api_key}", got.Server.MCPConfig.Env["HUDU_API_KEY"])
		assert.Equal(t, "${user_config.hudu_base_url}", got.Server.MCPConfig.Env["HUDU_BASE_URL"])
		_, hasConfig := got.Server.MCPConfig.Env["HUDU_CONFIG"]
		assert.False(t, hasConfig, "config-file path must not become MCPB user_config")

		entry, ok := got.UserConfig["hudu_base_url"]
		require.True(t, ok)
		assert.Equal(t, "HUDU_BASE_URL", entry.Title)
		assert.Equal(t, "string", entry.Type)
		assert.False(t, entry.Required)
		assert.False(t, entry.Sensitive)
		assert.Contains(t, entry.Description, "HUDU_BASE_URL")
		assert.Contains(t, entry.Description, "not a credential")
		assert.NotContains(t, entry.Description, "credential refresh")
	})

	t.Run("platform profile still promotes credentials the binary reads", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "platform"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "platform", "profile.go"), []byte("package platform\n"), 0o644))
		cli := CLIManifest{
			APIName:     "hudu",
			DisplayName: "Hudu",
			MCPBinary:   "hudu-pp-mcp",
			AuthType:    "api_key",
			AuthEnvVars: []string{"HUDU_API_KEY"},
		}
		writeMCPBManifest(t, dir, MCPBManifest{
			Name: "hudu-pp-mcp",
			Server: MCPBServer{
				MCPConfig: MCPBLaunchSpec{Env: map[string]string{}},
			},
		})
		writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() {
	_ = os.Getenv("HUDU_API_KEY")
	_ = os.Getenv("HUDU_BASE_URL")
}
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		assert.Equal(t, "${user_config.hudu_api_key}", got.Server.MCPConfig.Env["HUDU_API_KEY"])
		assert.Equal(t, "${user_config.hudu_base_url}", got.Server.MCPConfig.Env["HUDU_BASE_URL"])
		_, hasProfile := got.Server.MCPConfig.Env["PRINTING_PRESS_CLIENT_PROFILE"]
		assert.False(t, hasProfile)
		_, ok := got.UserConfig["hudu_base_url"]
		assert.True(t, ok)
	})

	t.Run("platform profile still declares required endpoint template vars", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "platform"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "platform", "profile.go"), []byte("package platform\n"), 0o644))
		cli := CLIManifest{
			APIName:              "shopify",
			DisplayName:          "Shopify",
			MCPBinary:            "shopify-pp-mcp",
			AuthType:             "api_key",
			AuthEnvVars:          []string{"SHOPIFY_ACCESS_TOKEN"},
			EndpointTemplateVars: []string{"shop"},
		}
		writeMCPBManifest(t, dir, MCPBManifest{
			Name: "shopify-pp-mcp",
			Server: MCPBServer{
				MCPConfig: MCPBLaunchSpec{Env: map[string]string{}},
			},
		})
		writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() {
	_ = os.Getenv("SHOPIFY_ACCESS_TOKEN")
	_ = os.Getenv("SHOPIFY_SHOP")
}
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		assert.Equal(t, "${user_config.shopify_access_token}", got.Server.MCPConfig.Env["SHOPIFY_ACCESS_TOKEN"])
		assert.Equal(t, "${user_config.shopify_shop}", got.Server.MCPConfig.Env["SHOPIFY_SHOP"])
		_, hasProfile := got.Server.MCPConfig.Env["PRINTING_PRESS_CLIENT_PROFILE"]
		assert.False(t, hasProfile)
		shop, ok := got.UserConfig["shopify_shop"]
		require.True(t, ok, "required {shop} must be promoted when discovered next to the profile selector")
		assert.Equal(t, "SHOPIFY_SHOP", shop.Title)
		assert.True(t, shop.Required, "{shop} has no spec-level default")
		assert.False(t, shop.Sensitive)
		assert.Contains(t, shop.Description, "{shop}")
		assert.NotContains(t, shop.Description, "credential refresh")
	})

	t.Run("platform profile rejects auth-named endpoint override", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "platform"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "platform", "profile.go"), []byte("package platform\n"), 0o644))
		cli := CLIManifest{
			APIName:                      "shopify",
			DisplayName:                  "Shopify",
			MCPBinary:                    "shopify-pp-mcp",
			AuthType:                     "api_key",
			AuthEnvVars:                  []string{"SHOPIFY_ACCESS_TOKEN"},
			EndpointTemplateVars:         []string{"shop"},
			EndpointTemplateEnvOverrides: map[string]string{"shop": "SHOPIFY_ACCESS_TOKEN"},
		}
		writeMCPBManifest(t, dir, MCPBManifest{
			Name: "shopify-pp-mcp",
			Server: MCPBServer{
				MCPConfig: MCPBLaunchSpec{Env: map[string]string{}},
			},
		})
		writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() {
	_ = os.Getenv("SHOPIFY_ACCESS_TOKEN")
	_ = os.Getenv("SHOPIFY_SHOP")
}
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		assert.Equal(t, "${user_config.shopify_access_token}", got.Server.MCPConfig.Env["SHOPIFY_ACCESS_TOKEN"])
		tokenField, hasTokenField := got.UserConfig["shopify_access_token"]
		require.True(t, hasTokenField)
		assert.True(t, tokenField.Sensitive)
		assert.Equal(t, "${user_config.shopify_shop}", got.Server.MCPConfig.Env["SHOPIFY_SHOP"])
		shop, ok := got.UserConfig["shopify_shop"]
		require.True(t, ok)
		assert.False(t, shop.Sensitive)
	})

	t.Run("platform profile promotes Getenv names the binary reads", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "platform"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "platform", "profile.go"), []byte("package platform\n"), 0o644))
		cli := CLIManifest{
			APIName:     "shopify",
			DisplayName: "Shopify",
			MCPBinary:   "shopify-pp-mcp",
			AuthType:    "api_key",
			AuthEnvVars: []string{"SHOPIFY_ACCESS_TOKEN"},
		}
		writeMCPBManifest(t, dir, MCPBManifest{
			Name: "shopify-pp-mcp",
			Server: MCPBServer{
				MCPConfig: MCPBLaunchSpec{Env: map[string]string{}},
			},
		})
		writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() {
	_ = os.Getenv("SHOPIFY_ACCESS_TOKEN")
	_ = os.Getenv("SHOPIFY_SHOP")
}
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		assert.Equal(t, "${user_config.shopify_access_token}", got.Server.MCPConfig.Env["SHOPIFY_ACCESS_TOKEN"])
		assert.Equal(t, "${user_config.shopify_shop}", got.Server.MCPConfig.Env["SHOPIFY_SHOP"])
		_, hasProfile := got.Server.MCPConfig.Env["PRINTING_PRESS_CLIENT_PROFILE"]
		assert.False(t, hasProfile)
	})

	t.Run("manifest with nil env/userconfig maps gets populated", func(t *testing.T) {
		dir := t.TempDir()
		cli := CLIManifest{APIName: "x", MCPBinary: "x-pp-mcp", AuthType: "api_key"}
		writeMCPBManifest(t, dir, MCPBManifest{
			Name:   "x-pp-mcp",
			Server: MCPBServer{},
		})
		writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("X_HIDDEN") }
`)

		require.NoError(t, reconcileMCPBManifestFromClient(dir, cli))

		got := readMCPBManifest(t, dir)
		assert.Equal(t, "${user_config.x_hidden}", got.Server.MCPConfig.Env["X_HIDDEN"])
		_, ok := got.UserConfig["x_hidden"]
		assert.True(t, ok)
	})
}

// TestWriteMCPBManifestFromStruct_ReconcilesClientEnvReads is the
// integration guard: every WriteMCPBManifest* call site must invoke the
// reconciler automatically. A regression that detaches reconcile from the
// writer would let the lock+promote and bundle paths ship un-reconciled
// manifests, reintroducing #859.
func TestWriteMCPBManifestFromStruct_ReconcilesClientEnvReads(t *testing.T) {
	dir := t.TempDir()
	writeClientFile(t, dir, "auth_refresh.go", `package client

import "os"

func refresh() (string, string) {
	return os.Getenv("RENTALWORKS_HOME_USERNAME"), os.Getenv("RENTALWORKS_HOME_PASSWORD")
}
`)

	m := CLIManifest{
		APIName:     "rentalworks-home",
		DisplayName: "RentalWorks Home",
		MCPBinary:   "rentalworks-home-pp-mcp",
		MCPReady:    "full",
		AuthType:    "bearer_token",
		AuthEnvVars: []string{"RENTALWORKS_HOME_TOKEN"},
	}

	require.NoError(t, WriteMCPBManifestFromStruct(dir, m))

	got := readMCPBManifest(t, dir)
	assert.Equal(t, "${user_config.rentalworks_home_token}", got.Server.MCPConfig.Env["RENTALWORKS_HOME_TOKEN"])
	assert.Equal(t, "${user_config.rentalworks_home_username}", got.Server.MCPConfig.Env["RENTALWORKS_HOME_USERNAME"])
	assert.Equal(t, "${user_config.rentalworks_home_password}", got.Server.MCPConfig.Env["RENTALWORKS_HOME_PASSWORD"])

	for _, key := range []string{"rentalworks_home_username", "rentalworks_home_password"} {
		entry, ok := got.UserConfig[key]
		require.True(t, ok, "%s must be present in user_config", key)
		assert.True(t, entry.Sensitive, "%s must be sensitive", key)
		assert.True(t, entry.Required, "%s must be required when base auth requires credential", key)
		assert.NotContains(t, entry.Description, "Optional.", "%s must not carry Optional prefix on required auth", key)
	}
}

func TestWriteMCPBManifestFromStruct_ReconcilesConfigBaseURLWithPlatformProfile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "platform"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "platform", "profile.go"), []byte("package platform\n"), 0o644))
	writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() {
	_ = os.Getenv("HUDU_API_KEY")
	_ = os.Getenv("HUDU_BASE_URL")
	_ = os.Getenv("HUDU_CONFIG")
}
`)

	m := CLIManifest{
		APIName:     "hudu",
		DisplayName: "Hudu",
		MCPBinary:   "hudu-pp-mcp",
		MCPReady:    "full",
		AuthType:    "api_key",
		AuthEnvVars: []string{"HUDU_API_KEY"},
	}

	require.NoError(t, WriteMCPBManifestFromStruct(dir, m))

	got := readMCPBManifest(t, dir)
	assert.Equal(t, "${user_config.hudu_api_key}", got.Server.MCPConfig.Env["HUDU_API_KEY"])
	assert.Equal(t, "${user_config.hudu_base_url}", got.Server.MCPConfig.Env["HUDU_BASE_URL"])
	_, hasProfile := got.Server.MCPConfig.Env["PRINTING_PRESS_CLIENT_PROFILE"]
	assert.False(t, hasProfile)
	_, hasConfig := got.Server.MCPConfig.Env["HUDU_CONFIG"]
	assert.False(t, hasConfig)
	entry, ok := got.UserConfig["hudu_base_url"]
	require.True(t, ok)
	assert.False(t, entry.Sensitive)
	assert.False(t, entry.Required)
}

// TestWriteMCPBManifest_DiskReadVariantReconciles guards the
// disk-read entry point used by lock.go, publish.go, and mcpsync. A
// regression that broke `WriteMCPBManifest`'s delegation to
// `WriteMCPBManifestFromStruct` would skip reconcile silently for the
// most common call shape.
func TestWriteMCPBManifest_DiskReadVariantReconciles(t *testing.T) {
	dir := t.TempDir()
	writeClientFile(t, dir, "auth_refresh.go", `package client

import "os"

func u() (string, string) {
	return os.Getenv("X_USERNAME"), os.Getenv("X_PASSWORD")
}
`)
	writeManifest(t, dir, CLIManifest{
		APIName:     "x",
		MCPBinary:   "x-pp-mcp",
		MCPReady:    "full",
		AuthType:    "bearer_token",
		AuthEnvVars: []string{"X_TOKEN"},
	})

	require.NoError(t, WriteMCPBManifest(dir))

	got := readMCPBManifest(t, dir)
	assert.Equal(t, "${user_config.x_username}", got.Server.MCPConfig.Env["X_USERNAME"])
	assert.Equal(t, "${user_config.x_password}", got.Server.MCPConfig.Env["X_PASSWORD"])
	for _, key := range []string{"x_username", "x_password"} {
		entry, ok := got.UserConfig[key]
		require.True(t, ok, "%s must be in user_config after disk-read writer path", key)
		assert.True(t, entry.Required, "%s must inherit required from bearer_token auth", key)
	}
}

// TestWriteMCPBManifestFromStruct_AuthOptionalPropagates guards the
// in-memory CLIManifest plumbing — a regression that dropped AuthOptional
// between the writer and the reconciler would mark discovered fields
// Required on optional-auth APIs.
func TestWriteMCPBManifestFromStruct_AuthOptionalPropagates(t *testing.T) {
	dir := t.TempDir()
	writeClientFile(t, dir, "client.go", `package client

import "os"

func read() string { return os.Getenv("OPTIONAL_HIDDEN") }
`)
	m := CLIManifest{
		APIName:      "optional-cli",
		MCPBinary:    "optional-cli-pp-mcp",
		MCPReady:     "full",
		AuthType:     "api_key",
		AuthOptional: true,
	}

	require.NoError(t, WriteMCPBManifestFromStruct(dir, m))

	got := readMCPBManifest(t, dir)
	entry, ok := got.UserConfig["optional_hidden"]
	require.True(t, ok)
	assert.False(t, entry.Required, "AuthOptional=true must propagate through the writer entry point")
	assert.Contains(t, entry.Description, "Optional.")
}

// TestWriteMCPBManifestFromStruct_IdempotentReconcile ensures running the
// writer twice on the same dir produces identical output bytes — the
// reconciler must not double-append or churn fields.
func TestWriteMCPBManifestFromStruct_IdempotentReconcile(t *testing.T) {
	dir := t.TempDir()
	writeClientFile(t, dir, "auth_refresh.go", `package client

import "os"

func u() string { return os.Getenv("X_USERNAME") }
`)

	m := CLIManifest{
		APIName:     "x",
		MCPBinary:   "x-pp-mcp",
		MCPReady:    "full",
		AuthType:    "bearer_token",
		AuthEnvVars: []string{"X_TOKEN"},
	}

	require.NoError(t, WriteMCPBManifestFromStruct(dir, m))
	first, err := os.ReadFile(filepath.Join(dir, MCPBManifestFilename))
	require.NoError(t, err)

	require.NoError(t, WriteMCPBManifestFromStruct(dir, m))
	second, err := os.ReadFile(filepath.Join(dir, MCPBManifestFilename))
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second), "reconcile must be idempotent across consecutive writer runs")
}

// TestScanClientEnvReadsExcludesSystemEnvVarsFromUserConfig guards the
// platform-convention denylist: XDG_CACHE_HOME, XDG_CONFIG_HOME, etc. are
// read by the generated client (e.g. cache root selection) but must never
// be promoted to user_config — they are user-global settings, not
// per-CLI credentials, and surfacing them as Required+Sensitive would
// break install prompts on every printed CLI.
func TestScanClientEnvReadsExcludesSystemEnvVarsFromUserConfig(t *testing.T) {
	dir := t.TempDir()
	writeClientFile(t, dir, "client.go", `package client

import "os"

func cachePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	_ = os.Getenv("XDG_CONFIG_HOME")
	_ = os.Getenv("XDG_DATA_HOME")
	_ = os.Getenv("XDG_STATE_HOME")
	_ = os.Getenv("HOME")
	_ = os.Getenv("USERPROFILE")
	return base + os.Getenv("REAL_API_KEY")
}
`)
	got, err := scanClientEnvReads(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"REAL_API_KEY"}, got,
		"system env vars (XDG_*, HOME, USERPROFILE) must never reach user_config; only the credential REAL_API_KEY should survive")
}

// TestScanClientEnvReadsBacktickLiteral guards against a regression that
// would drop backtick-quoted env var names. strconv.Unquote handles both
// forms, but the integration is worth a fixture in case the parser path
// changes.
func TestScanClientEnvReadsBacktickLiteral(t *testing.T) {
	dir := t.TempDir()
	writeClientFile(t, dir, "client.go", "package client\n\nimport \"os\"\n\nfunc read() string { return os.Getenv(`BACKTICK_VAR`) }\n")
	got, err := scanClientEnvReads(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"BACKTICK_VAR"}, got)
}

func writeClientFile(t *testing.T, dir, name, content string) {
	t.Helper()
	clientDir := filepath.Join(dir, "internal", "client")
	require.NoError(t, os.MkdirAll(clientDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(clientDir, name), []byte(content), 0o644))
}

func writeConfigFile(t *testing.T, dir, name, content string) {
	t.Helper()
	configDir := filepath.Join(dir, "internal", "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, name), []byte(content), 0o644))
}

// A scanner fixture can miss a split where config.go.tmpl emits
// *_BASE_URL but the generate-time writer never reads that file. The
// installer ships the printed manifest.json, so that file is the
// contract this proof checks.
func TestWriteManifestForGenerate_IncludesGeneratedConfigBaseURL(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:      "hudu",
		Version:   "0.1.0",
		BaseURL:   "https://example.huducloud.com/api/v1",
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "x-api-key",
			In:      "header",
			EnvVars: []string{"HUDU_API_KEY"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/hudu-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"articles": {
				Description: "Knowledge base articles",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/articles", Description: "List articles"},
				},
			},
		},
	}

	dir := filepath.Join(t.TempDir(), "hudu-pp-cli")
	require.NoError(t, generator.New(apiSpec, dir).Generate())

	configSrc, err := os.ReadFile(filepath.Join(dir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.Contains(t, string(configSrc), `os.Getenv("HUDU_BASE_URL")`,
		"generated config.go must remain the BASE_URL Getenv source this test is proving")
	require.Contains(t, string(configSrc), `os.Getenv("HUDU_CONFIG")`)

	require.NoError(t, WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "hudu",
		OutputDir: dir,
		Spec:      apiSpec,
	}))

	cli, err := ReadCLIManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"HUDU_API_KEY"}, cli.AuthEnvVars)
	assert.NotContains(t, cli.AuthEnvVars, "PRINTING_PRESS_CLIENT_PROFILE")

	got := readMCPBManifest(t, dir)
	assert.Equal(t, "${user_config.hudu_base_url}", got.Server.MCPConfig.Env["HUDU_BASE_URL"])
	assert.Equal(t, "${user_config.hudu_api_key}", got.Server.MCPConfig.Env["HUDU_API_KEY"])
	_, hasProfile := got.Server.MCPConfig.Env["PRINTING_PRESS_CLIENT_PROFILE"]
	assert.False(t, hasProfile, "tenant profile selector is not a credential the binary reads")
	_, hasConfig := got.Server.MCPConfig.Env["HUDU_CONFIG"]
	assert.False(t, hasConfig, "HUDU_CONFIG is a local file path, not an MCPB install prompt")
	_, hasVerify := got.Server.MCPConfig.Env["PRINTING_PRESS_VERIFY"]
	assert.False(t, hasVerify)

	entry, ok := got.UserConfig["hudu_base_url"]
	require.True(t, ok, "generated MCPB manifest must prompt for HUDU_BASE_URL")
	assert.Equal(t, "HUDU_BASE_URL", entry.Title)
	assert.False(t, entry.Required)
	assert.False(t, entry.Sensitive)
	assert.Contains(t, entry.Description, "not a credential")
}

// A suffix allowlist cannot name {shop}: SHOPIFY_SHOP has no
// _BASE_URL-style ending, but the printed CLI still reads it before
// the first request. The installer prompt has to collect it alongside
// the credential the binary reads.
func TestWriteManifestForGenerate_IncludesRequiredEndpointTemplateVar(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:                 "shopify",
		Version:              "2026-04",
		BaseURL:              "https://{shop}.myshopify.com/admin/api/2026-04",
		EndpointTemplateVars: []string{"shop"},
		Owner:                "test-owner",
		OwnerName:            "Test Author",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-Shopify-Access-Token",
			EnvVars: []string{"SHOPIFY_ACCESS_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/shopify-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"orders": {
				Description: "Orders",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/orders", Description: "List orders"},
				},
			},
		},
	}

	dir := filepath.Join(t.TempDir(), "shopify-pp-cli")
	require.NoError(t, generator.New(apiSpec, dir).Generate())

	configSrc, err := os.ReadFile(filepath.Join(dir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.Contains(t, string(configSrc), `os.Getenv("SHOPIFY_SHOP")`,
		"generated config.go must remain the {shop} Getenv source this test is proving")

	require.NoError(t, WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "shopify",
		OutputDir: dir,
		Spec:      apiSpec,
	}))

	got := readMCPBManifest(t, dir)
	assert.Equal(t, "${user_config.shopify_shop}", got.Server.MCPConfig.Env["SHOPIFY_SHOP"])
	assert.Equal(t, "${user_config.shopify_access_token}", got.Server.MCPConfig.Env["SHOPIFY_ACCESS_TOKEN"])
	_, hasProfile := got.Server.MCPConfig.Env["PRINTING_PRESS_CLIENT_PROFILE"]
	assert.False(t, hasProfile, "tenant profile selector is not a credential the binary reads")

	entry, ok := got.UserConfig["shopify_shop"]
	require.True(t, ok, "generated MCPB manifest must prompt for SHOPIFY_SHOP")
	assert.Equal(t, "SHOPIFY_SHOP", entry.Title)
	assert.True(t, entry.Required)
	assert.False(t, entry.Sensitive)
	assert.Contains(t, entry.Description, "{shop}")
}

func TestWriteManifestForGenerate_CollidingEndpointOverrideAgreesWithGetenv(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:                 "shopify",
		Version:              "2026-04",
		BaseURL:              "https://{shop}.myshopify.com/admin/api/2026-04",
		EndpointTemplateVars: []string{"shop"},
		EndpointTemplateEnvOverrides: map[string]string{
			"shop": "SHOPIFY_ACCESS_TOKEN",
		},
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-Shopify-Access-Token",
			EnvVars: []string{"SHOPIFY_ACCESS_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/shopify-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"orders": {
				Description: "Orders",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/orders", Description: "List orders"},
				},
			},
		},
	}

	dir := filepath.Join(t.TempDir(), "shopify-pp-cli")
	require.NoError(t, generator.New(apiSpec, dir).Generate())

	configSrc, err := os.ReadFile(filepath.Join(dir, "internal", "config", "config.go"))
	require.NoError(t, err)
	shopGetenv := extractEndpointGetenv(t, string(configSrc), "_SHOP")
	require.Equal(t, "SHOPIFY_SHOP", shopGetenv,
		"rejected {shop} → ACCESS_TOKEN override must generate the default shop Getenv, not the credential name")

	require.NoError(t, WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "shopify",
		OutputDir: dir,
		Spec:      apiSpec,
	}))

	cli, err := ReadCLIManifest(dir)
	require.NoError(t, err)
	_, hasOverride := cli.EndpointTemplateEnvOverrides["shop"]
	assert.False(t, hasOverride, "stored override must be dropped so a later manifest write cannot re-bind the credential name")

	got := readMCPBManifest(t, dir)
	require.Contains(t, got.Server.MCPConfig.Env, shopGetenv,
		"MCPB env key must be the generated {shop} Getenv")
	assert.Equal(t, "${user_config."+userConfigKey(shopGetenv)+"}", got.Server.MCPConfig.Env[shopGetenv])
	assert.Equal(t, "${user_config.shopify_access_token}", got.Server.MCPConfig.Env["SHOPIFY_ACCESS_TOKEN"])
	tokenField, hasTokenField := got.UserConfig["shopify_access_token"]
	require.True(t, hasTokenField, "installer must prompt for the credential the binary reads")
	assert.True(t, tokenField.Sensitive)
	_, hasProfile := got.Server.MCPConfig.Env["PRINTING_PRESS_CLIENT_PROFILE"]
	assert.False(t, hasProfile)
	shop, ok := got.UserConfig[userConfigKey(shopGetenv)]
	require.True(t, ok)
	assert.False(t, shop.Sensitive)
}

// A later MCPB refresh does not regenerate client source. If generate already
// dropped a colliding {shop} → ACCESS_TOKEN override, the printed Getenv is
// SHOPIFY_SHOP and the installer must follow it. If the existing tree still
// Getenvs the credential name, rebinding to SHOPIFY_SHOP leaves the first
// request unset. Keep the installer key paired with the scan-confirmed
// Getenv and mask it so it does not sit unmasked beside the profile selector.
func TestWriteMCPBManifest_LegacyCollidingGetenvKeepsInstallerAligned(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "platform"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "platform", "profile.go"), []byte("package platform\n"), 0o644))
	writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() {
	_ = os.Getenv("SHOPIFY_ACCESS_TOKEN")
}
`)
	writeManifest(t, dir, CLIManifest{
		APIName:     "shopify",
		DisplayName: "Shopify",
		CLIName:     "shopify-pp-cli",
		MCPBinary:   "shopify-pp-mcp",
		MCPReady:    "full",
		AuthType:    "api_key",
		AuthEnvVars: []string{"SHOPIFY_ACCESS_TOKEN"},
		AuthEnvVarSpecs: []spec.AuthEnvVar{{
			Name: "SHOPIFY_ACCESS_TOKEN", Kind: spec.AuthEnvVarKindPerCall,
			Required: true, Sensitive: true,
		}},
		EndpointTemplateVars:         []string{"shop"},
		EndpointTemplateEnvOverrides: map[string]string{"shop": "SHOPIFY_ACCESS_TOKEN"},
	})

	require.NoError(t, WriteMCPBManifest(dir))
	got := readMCPBManifest(t, dir)

	require.Contains(t, got.Server.MCPConfig.Env, "SHOPIFY_ACCESS_TOKEN",
		"manifest-only refresh must collect the Getenv the existing client still reads")
	assert.Equal(t, "${user_config.shopify_access_token}", got.Server.MCPConfig.Env["SHOPIFY_ACCESS_TOKEN"])
	_, hasShop := got.Server.MCPConfig.Env["SHOPIFY_SHOP"]
	assert.False(t, hasShop, "must not silently rebind the installer to the default shop name")

	_, hasProfile := got.Server.MCPConfig.Env["PRINTING_PRESS_CLIENT_PROFILE"]
	assert.False(t, hasProfile)

	token, ok := got.UserConfig["shopify_access_token"]
	require.True(t, ok, "legacy colliding Getenv must stay on the installer")
	assert.Equal(t, "SHOPIFY_ACCESS_TOKEN", token.Title)
	assert.True(t, token.Sensitive, "credential-named shop binding must stay masked beside the profile selector")
	assert.True(t, token.Required)

	require.NoError(t, WriteCLIManifest(dir, CLIManifest{
		SchemaVersion:                CurrentCLIManifestSchemaVersion,
		APIName:                      "shopify",
		CLIName:                      "shopify-pp-cli",
		MCPBinary:                    "shopify-pp-mcp",
		MCPReady:                     "full",
		AuthType:                     "api_key",
		AuthEnvVars:                  []string{"SHOPIFY_ACCESS_TOKEN"},
		EndpointTemplateVars:         []string{"shop"},
		EndpointTemplateEnvOverrides: map[string]string{"shop": "SHOPIFY_ACCESS_TOKEN"},
	}))
	cli, err := ReadCLIManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "SHOPIFY_ACCESS_TOKEN", cli.EndpointTemplateEnvOverrides["shop"],
		"CLI manifest write must keep the scan-confirmed colliding override")
}

func TestAlignPrefixedEnvName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		envName string
		apiName string
		want    string
	}{
		{name: "stale default after hyphenated rename", envName: "SHOPIFY_SHOP", apiName: "shopify-alt", want: "SHOPIFY_ALT_SHOP"},
		{name: "already current prefix", envName: "SHOPIFY_SHOP", apiName: "shopify", want: "SHOPIFY_SHOP"},
		{name: "custom suffix under old prefix", envName: "SHOPIFY_STORE", apiName: "shopify-alt", want: "SHOPIFY_ALT_STORE"},
		{name: "foreign override stays", envName: "ST_TENANT_ID", apiName: "servicetitan-alt", want: "ST_TENANT_ID"},
		{name: "bare prefix", envName: "SHOPIFY", apiName: "shopify-alt", want: "SHOPIFY_ALT"},
		{name: "empty name", envName: "", apiName: "shopify-alt", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, alignPrefixedEnvName(tt.envName, tt.apiName))
		})
	}
}

// Rename rewrites generated os.Getenv names that begin with the old CLI
// prefix, but used to leave EndpointTemplateEnvOverrides on the old
// name. The installer then collected SHOPIFY_SHOP while the client read
// SHOPIFY_ALT_SHOP. This print-then-rename proof is the contract: the
// MCPB env key must be the rewritten Getenv.
func TestRenameCLI_PlatformProfileEndpointEnvMatchesRewrittenGetenv(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:                 "shopify",
		Version:              "2026-04",
		BaseURL:              "https://{shop}.myshopify.com/admin/api/2026-04",
		EndpointTemplateVars: []string{"shop"},
		EndpointTemplateEnvOverrides: map[string]string{
			"shop": "SHOPIFY_SHOP",
		},
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "X-Shopify-Access-Token",
			EnvVars: []string{"SHOPIFY_ACCESS_TOKEN"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/shopify-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"orders": {
				Description: "Orders",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/orders", Description: "List orders"},
				},
			},
		},
	}

	root := t.TempDir()
	cliDir := filepath.Join(root, "shopify-pp-cli")
	require.NoError(t, generator.New(apiSpec, cliDir).Generate())
	require.NoError(t, WriteManifestForGenerate(GenerateManifestParams{
		APIName:   "shopify",
		OutputDir: cliDir,
		Spec:      apiSpec,
	}))

	_, err := os.Stat(filepath.Join(cliDir, "internal", "platform", "profile.go"))
	require.NoError(t, err, "fixture must be a platform-profile CLI so the rename hits the profile+endpoint bind path")

	preConfig, err := os.ReadFile(filepath.Join(cliDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.Contains(t, string(preConfig), `os.Getenv("SHOPIFY_SHOP")`)

	preManifest := readMCPBManifest(t, cliDir)
	require.Contains(t, preManifest.Server.MCPConfig.Env, "SHOPIFY_SHOP")

	_, err = RenameCLI(cliDir, "shopify-pp-cli", "shopify-alt-pp-cli", "shopify")
	require.NoError(t, err)

	newDir := filepath.Join(root, naming.LibraryDirName("shopify-alt-pp-cli"))
	configSrc, err := os.ReadFile(filepath.Join(newDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	rewritten := extractEndpointGetenv(t, string(configSrc), "_SHOP")
	require.NotEqual(t, "SHOPIFY_SHOP", rewritten, "rename must rewrite the generated {shop} Getenv")
	require.Contains(t, string(configSrc), `os.Getenv("`+rewritten+`")`)
	require.NotContains(t, string(configSrc), `os.Getenv("SHOPIFY_SHOP")`)

	got := readMCPBManifest(t, newDir)
	assert.Equal(t, "${user_config."+userConfigKey(rewritten)+"}", got.Server.MCPConfig.Env[rewritten],
		"MCPB manifest must collect the rewritten Getenv, not the pre-rename override")
	_, stale := got.Server.MCPConfig.Env["SHOPIFY_SHOP"]
	assert.False(t, stale, "stale SHOPIFY_SHOP must not remain on the installer prompt")
	assert.Equal(t, "${user_config.shopify_access_token}", got.Server.MCPConfig.Env["SHOPIFY_ACCESS_TOKEN"],
		"rename must keep the credential the binary reads")

	cliData, err := os.ReadFile(filepath.Join(newDir, CLIManifestFilename))
	require.NoError(t, err)
	var cli CLIManifest
	require.NoError(t, json.Unmarshal(cliData, &cli))
	assert.Equal(t, rewritten, cli.EndpointTemplateEnvOverrides["shop"],
		"stored override must move with the generated Getenv so a later manifest write stays paired")
}

func TestAlignEndpointTemplateEnvNamesScanConfirmed(t *testing.T) {
	t.Parallel()

	t.Run("rewrites stale override when generated client already moved", func(t *testing.T) {
		dir := t.TempDir()
		writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() { _ = os.Getenv("SHOPIFY_ALT_SHOP") }
`)
		got := alignEndpointTemplateEnvNames(dir, CLIManifest{
			APIName:                      "shopify-alt",
			EndpointTemplateVars:         []string{"shop"},
			EndpointTemplateEnvOverrides: map[string]string{"shop": "SHOPIFY_SHOP"},
		})
		assert.Equal(t, "SHOPIFY_ALT_SHOP", got.EndpointTemplateEnvOverrides["shop"])
	})

	t.Run("keeps vendor-short override the generated client still reads", func(t *testing.T) {
		dir := t.TempDir()
		writeConfigFile(t, dir, "config.go", `package config

import "os"

func Load() { _ = os.Getenv("SHOPIFY_SHOP") }
`)
		original := CLIManifest{
			APIName:                      "shopify-plus",
			EndpointTemplateVars:         []string{"shop"},
			EndpointTemplateEnvOverrides: map[string]string{"shop": "SHOPIFY_SHOP"},
		}
		got := alignEndpointTemplateEnvNames(dir, original)
		assert.Equal(t, "SHOPIFY_SHOP", got.EndpointTemplateEnvOverrides["shop"])
	})
}

func extractEndpointGetenv(t *testing.T, src, suffix string) string {
	t.Helper()
	matches := regexp.MustCompile(`os\.Getenv\("([^"]+`+regexp.QuoteMeta(suffix)+`)"\)`).FindAllStringSubmatch(src, -1)
	require.NotEmpty(t, matches, "expected a Getenv ending in %s", suffix)
	require.Len(t, matches[0], 2)
	return matches[0][1]
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

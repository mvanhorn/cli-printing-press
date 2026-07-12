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

// Sanity check that MCPBVar json round-trips the new Sensitive+Required flags.
func TestMCPBVarRoundtripFlags(t *testing.T) {
	in := MCPBVar{Type: "string", Title: "X", Sensitive: true, Required: true}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	var out MCPBVar
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out)
}

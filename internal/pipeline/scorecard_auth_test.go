package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// scoreAuthTestDir builds a minimal fake CLI output dir with the given file
// contents and returns the dir path.
func scoreAuthTestDir(t *testing.T, configGo, authGo, clientGo string) string {
	t.Helper()
	dir := t.TempDir()
	if configGo != "" {
		writeFile(t, filepath.Join(dir, "internal", "config", "config.go"), configGo)
	}
	if authGo != "" {
		writeFile(t, filepath.Join(dir, "internal", "cli", "auth.go"), authGo)
	}
	if clientGo != "" {
		writeFile(t, filepath.Join(dir, "internal", "client", "client.go"), clientGo)
	}
	return dir
}

// perfectSimpleAuthConfig is a config.go snippet that satisfies all
// non-OAuth2 auth scoring criteria.
const perfectSimpleAuthConfig = `package config

import "os"

func Load(path string) (*Config, error) {
	cfg := &Config{}
	cfg.Token = os.Getenv("MYAPI_TOKEN")
	return cfg, nil
}

func (c *Config) Save(path string) error {
	return os.WriteFile(path, data, 0o600)
}
`

// perfectSimpleAuthClient is a client.go snippet with credential masking.
const perfectSimpleAuthClient = `package client

import "fmt"

func maskToken(t string) string {
	if len(t) <= 4 {
		return "***"
	}
	return "...last 4: " + t[len(t)-4:]
}
`

// perfectSimpleAuthFile is an auth.go that has auth subcommands but no OAuth2.
const perfectSimpleAuthFile = `package cli

func newAuthCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "auth"}
	cmd.AddCommand(newAuthSetupCmd(flags))
	cmd.AddCommand(newAuthStatusCmd(flags))
	cmd.AddCommand(newAuthLogoutCmd(flags))
	return cmd
}
`

func TestScoreAuth_SimpleAPIKeyMaxIs8(t *testing.T) {
	t.Parallel()
	dir := scoreAuthTestDir(t, perfectSimpleAuthConfig, perfectSimpleAuthFile, perfectSimpleAuthClient)
	// A perfect simple-auth CLI scores 2 (Getenv) + 1 (auth file) + 2 (0o600) +
	// 2 (masking via "last 4") + 1 (multi-source: Getenv + Load) = 8.
	// No OAuth2 signals → no +2 grant → max 8/10.
	assert.Equal(t, 8, scoreAuth(dir))
}

func TestScoreAuth_OAuth2BrowserFlowScores10(t *testing.T) {
	t.Parallel()
	// auth.go contains runOAuthLogin → browser-based OAuth2 flow.
	oauthAuthFile := perfectSimpleAuthFile + `
func runOAuthLogin(cmd *cobra.Command, flags *rootFlags, clientID, clientSecret string, port int) error {
	openBrowser("https://example.com/oauth/authorize")
	return nil
}
`
	dir := scoreAuthTestDir(t, perfectSimpleAuthConfig, oauthAuthFile, perfectSimpleAuthClient)
	assert.Equal(t, 10, scoreAuth(dir))
}

func TestScoreAuth_DeviceCodeFlowScores10(t *testing.T) {
	t.Parallel()
	// auth.go contains DeviceCode → device-code OAuth2 flow.
	deviceAuthFile := perfectSimpleAuthFile + `
type pendingState struct {
	DeviceCode string ` + "`json:\"device_code\"`" + `
}
`
	dir := scoreAuthTestDir(t, perfectSimpleAuthConfig, deviceAuthFile, perfectSimpleAuthClient)
	assert.Equal(t, 10, scoreAuth(dir))
}

func TestScoreAuth_NoAuthExemptionStillReturns10(t *testing.T) {
	t.Parallel()
	// When auth.go does not exist the no-auth exemption fires and returns 10.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 10, scoreAuth(dir))
}

func TestScoreAuth_MinimalAuthScoresLow(t *testing.T) {
	t.Parallel()
	// Only the auth file exists; no env var, no perms, no masking, no OAuth2.
	dir := scoreAuthTestDir(t, "", `package cli

func newAuthCmd() {}
`, "")
	// 0 (no Getenv) + 1 (auth file) + 0 + 0 + 0 + 0 (no OAuth2) = 1
	assert.Equal(t, 1, scoreAuth(dir))
}

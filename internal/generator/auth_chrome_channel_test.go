// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chromeChannelSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.BaseURL = "https://www.example.com"
	apiSpec.Auth = spec.AuthConfig{
		Type:         "cookie",
		Header:       "Cookie",
		In:           "cookie",
		CookieDomain: ".example.com",
		Cookies:      []string{"session_id"},
		EnvVars:      []string{name + "_SESSION"},
	}
	return apiSpec
}

// The generated cookie-auth flow must enumerate every installed Chrome channel
// rather than hardcode the stable data dir, so a Beta-only user authenticates
// without a flag. Assert the emitted helper shape and drop the old single-dir one.
func TestCookieAuthEmitsChannelAwareDiscovery(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "chromechan-pp-cli")
	require.NoError(t, New(chromeChannelSpec("chromechan"), outputDir).Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	assert.Contains(t, authGo, "func chromeChannelDirs()")
	// chromeChannelStable is the max-width const, so gofmt leaves exactly one
	// space before "=" — a stable anchor for the typed channel labels.
	assert.Contains(t, authGo, `chromeChannelStable = "Chrome"`)
	assert.Contains(t, authGo, "chromeChannelBeta")
	assert.Contains(t, authGo, `filepath.Join(base, "Chrome Beta")`)
	assert.Contains(t, authGo, `filepath.Join(base, "google-chrome-beta")`)
	assert.Contains(t, authGo, "func (p chromeProfile) profileLocation()")
	// The single-channel helper must be gone so no caller silently reads stable.
	assert.NotContains(t, authGo, "func chromeDataDir()")

	requireGeneratedCompiles(t, outputDir)
}

// A browser-session cookie CLI must persist the channel/profile chosen at login
// and have `auth refresh`'s cookie-DB fallback re-read it, otherwise refresh
// clobbers a Beta session with stable cookies (or none, for a Beta-only user).
func TestCookieAuthRefreshResolvesChannel(t *testing.T) {
	t.Parallel()

	apiSpec := chromeChannelSpec("chromechanrefresh")
	apiSpec.Auth.RequiresBrowserSession = true
	apiSpec.Auth.BrowserSessionValidationPath = "/api/me"
	apiSpec.Auth.BrowserSessionValidationMethod = "GET"

	outputDir := filepath.Join(t.TempDir(), "chromechanrefresh-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	// Login persists the resolved profile location.
	assert.Contains(t, authGo, "loginProfileLocation = profile.profileLocation()")
	assert.Contains(t, authGo, "cfg.ChromeProfile = loginProfileLocation")
	// Refresh re-reads it (no more blind empty-profile extraction).
	_, refresh, found := strings.Cut(authGo, "func refreshStoredBrowserCookies")
	require.True(t, found, "expected refreshStoredBrowserCookies in generated auth.go")
	assert.Contains(t, refresh, "savedProfile = cfg.ChromeProfile")
	assert.NotContains(t, refresh, "extractCookies(tool, domain, chromeProfile{})")
	// A saved-but-unresolvable profile fails loudly instead of silently
	// switching channels.
	assert.Contains(t, refresh, "could not be resolved; re-run auth login --chrome")

	// The persisted field exists in config with an omitempty tag.
	configGo := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	assert.Contains(t, configGo, "ChromeProfile string")
	assert.Contains(t, configGo, "chrome_profile,omitempty")

	requireGeneratedCompiles(t, outputDir)
}

// A cookie CLI without a browser-session refresh must NOT carry the
// chrome_profile field — it is scoped to the refresh feature that uses it.
func TestCookieAuthWithoutRefreshOmitsChromeProfileField(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "chromechannoref-pp-cli")
	require.NoError(t, New(chromeChannelSpec("chromechannoref"), outputDir).Generate())

	configGo := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	assert.NotContains(t, configGo, "ChromeProfile")
}

// Compile a runtime test into the generated CLI proving chromeChannelDirs picks
// up a Beta-only install and orders stable ahead of Beta when both exist.
func TestGeneratedChromeChannelDirsFindsBetaOnly(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "chromechanrt-pp-cli")
	require.NoError(t, New(chromeChannelSpec("chromechanrt"), outputDir).Generate())

	const runtimeTest = `package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// chromeChannelBase mirrors chromeChannelDirs' per-OS root so the test can plant
// fake channel dirs under a temp HOME.
func chromeChannelBase(t *testing.T, home string) (string, map[string]string) {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support", "Google")
		return base, map[string]string{"Chrome": "Chrome", "Chrome Beta": "Chrome Beta"}
	case "linux":
		base := filepath.Join(home, ".config")
		return base, map[string]string{"Chrome": "google-chrome", "Chrome Beta": "google-chrome-beta"}
	default:
		t.Skipf("channel path layout not asserted on %s", runtime.GOOS)
		return "", nil
	}
}

func TestChromeChannelDirsBetaOnlyAndOrdering(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base, dirs := chromeChannelBase(t, home)

	// Only Beta installed: it must be discovered on its own.
	betaDir := filepath.Join(base, dirs["Chrome Beta"])
	if err := os.MkdirAll(betaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := chromeChannelDirs()
	if err != nil {
		t.Fatalf("chromeChannelDirs() error = %v", err)
	}
	if len(got) != 1 || got[0].Channel != "Chrome Beta" || got[0].DataDir != betaDir {
		t.Fatalf("beta-only: got %#v, want single Chrome Beta at %s", got, betaDir)
	}

	// With stable also present, stable must sort first.
	stableDir := filepath.Join(base, dirs["Chrome"])
	if err := os.MkdirAll(stableDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = chromeChannelDirs()
	if err != nil {
		t.Fatalf("chromeChannelDirs() error = %v", err)
	}
	if len(got) != 2 || got[0].Channel != "Chrome" || got[1].Channel != "Chrome Beta" {
		t.Fatalf("both: got %#v, want [Chrome, Chrome Beta]", got)
	}
}

func TestProfileLocationChannelQualified(t *testing.T) {
	p := chromeProfile{Channel: "Chrome Beta", Dir: "Default"}
	if got := p.profileLocation(); got != "Chrome Beta/Default" {
		t.Fatalf("profileLocation() = %q, want Chrome Beta/Default", got)
	}
	bare := chromeProfile{Dir: "Default"}
	if got := bare.profileLocation(); got != "Default" {
		t.Fatalf("profileLocation() bare = %q, want Default", got)
	}
}

// With a "Default" profile in both Stable and Beta, bare "Default" resolves to
// stable, and the channel-qualified form selects Beta's cookie DB.
func TestResolveProfileByNameDisambiguatesChannel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base, dirs := chromeChannelBase(t, home)
	for _, ch := range []string{"Chrome", "Chrome Beta"} {
		if err := os.MkdirAll(filepath.Join(base, dirs[ch], "Default"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	stable, err := resolveProfileByName("Default")
	if err != nil || stable.Channel != "Chrome" {
		t.Fatalf(` + "`" + `resolveProfileByName("Default") = %#v, err=%v; want stable channel` + "`" + `, stable, err)
	}
	beta, err := resolveProfileByName("Chrome Beta/Default")
	if err != nil || beta.Channel != "Chrome Beta" || beta.DataDir != filepath.Join(base, dirs["Chrome Beta"]) {
		t.Fatalf(` + "`" + `resolveProfileByName("Chrome Beta/Default") = %#v, err=%v; want Beta channel` + "`" + `, beta, err)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "chrome_channel_test.go"), []byte(runtimeTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestChromeChannelDirs|TestProfileLocation|TestResolveProfileByNameDisambiguatesChannel")
}

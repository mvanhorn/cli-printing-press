package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isolatePP(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, ".cache"))
	t.Setenv("PP_HOME", filepath.Join(tmp, ".pp"))
	return tmp
}

func TestRouteMatchesBuiltinCatalogAndFallback(t *testing.T) {
	isolatePP(t)

	cat := Catalog{
		SchemaVersion: 2,
		Entries: []Entry{
			{
				Name:        "instacart",
				Category:    "commerce",
				Description: "Instacart GraphQL CLI",
				Homepage:    "https://www.instacart.com",
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(cat)
	}))
	defer srv.Close()

	client := &Client{
		RegistryURL: srv.URL,
		CachePath:   filepath.Join(t.TempDir(), "registry.json"),
	}

	// 1. Embedded starter cartridge match.
	espnDec, err := client.Route(context.Background(), "https://www.espn.com/nba/scoreboard")
	require.NoError(t, err)
	assert.True(t, espnDec.Matched)
	assert.Equal(t, "espn", espnDec.App)
	assert.Equal(t, "cartridge", espnDec.Mode)
	assert.True(t, espnDec.Installed)
	assert.Contains(t, espnDec.Commands, "scoreboard")
	assert.Contains(t, espnDec.Commands, "schedule")
	assert.Contains(t, espnDec.Commands, "summary")
	require.NotNil(t, espnDec.MCPCall)
	assert.Equal(t, "pp_run", espnDec.MCPCall["tool"])

	// 2. Catalog homepage match.
	instaDec, err := client.Route(context.Background(), "https://www.instacart.com/store/costco")
	require.NoError(t, err)
	assert.True(t, instaDec.Matched)
	assert.Equal(t, "instacart", instaDec.App)
	assert.Equal(t, "prebuilt-binary", instaDec.Mode)
	assert.Equal(t, "pp use instacart", instaDec.InstallCmd)
	require.NotNil(t, instaDec.MCPCall)
	assert.Equal(t, "pp_run", instaDec.MCPCall["tool"])

	// 3. Unknown site falls back with compile command and WebMCP hint.
	unkDec, err := client.Route(context.Background(), "https://unknown-portal.example/dashboard")
	require.NoError(t, err)
	assert.False(t, unkDec.Matched)
	assert.Contains(t, unkDec.CompileCmd, "pp record --backend ")
	assert.NotEmpty(t, unkDec.WebMCPHint)

	// 4. Natural-language intents match built-in cartridges cleanly without URLs.
	nlSports, err := client.Route(context.Background(), "who won the arizona wildcats men's college basketball game")
	require.NoError(t, err)
	assert.True(t, nlSports.Matched)
	assert.Equal(t, "espn", nlSports.App)
	assert.Equal(t, "cartridge", nlSports.Mode)

	nlHN, err := client.Route(context.Background(), "top 5 stories on hacker news right now")
	require.NoError(t, err)
	assert.True(t, nlHN.Matched)
	assert.Equal(t, "hackernews", nlHN.App)
	assert.Equal(t, "cartridge", nlHN.Mode)

	// Explicit named apps (both built-in and catalog) must beat topic aliases.
	namedBeatsTopic, err := client.Route(context.Background(), "hackernews stories about basketball")
	require.NoError(t, err)
	assert.True(t, namedBeatsTopic.Matched)
	assert.Equal(t, "hackernews", namedBeatsTopic.App)

	catalogBeatsTopic, err := client.Route(context.Background(), "instacart basketball snacks")
	require.NoError(t, err)
	assert.True(t, catalogBeatsTopic.Matched)
	assert.Equal(t, "instacart", catalogBeatsTopic.App)
}

func TestZeroGoPrebuiltBinaryInstallVerifiesChecksum(t *testing.T) {
	isolatePP(t)

	fakeBin := []byte("#!/bin/sh\necho hello\n")
	sum := sha256.Sum256(fakeBin)
	goodHash := hex.EncodeToString(sum[:])
	asset := releaseAssetName("acme-pp-cli", runtime.GOOS, runtime.GOARCH)

	cat := Catalog{
		SchemaVersion: 2,
		Entries: []Entry{
			{
				Name:        "acme",
				Category:    "developer-tools",
				Description: "Acme CLI",
				Release:     &ReleaseInfo{CLIName: "acme-pp-cli", Version: "2026.11.1"},
			},
		},
	}

	tamperChecksum := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.json":
			_ = json.NewEncoder(w).Encode(cat)
		case "/releases/acme-current/checksums.txt":
			h := goodHash
			if tamperChecksum {
				h = "0000000000000000000000000000000000000000000000000000000000000000"
			}
			_, _ = fmt.Fprintf(w, "%s  %s\n", h, asset)
		case "/releases/acme-current/" + asset:
			_, _ = w.Write(fakeBin)
		case "/skills/pp-acme/SKILL.md":
			_, _ = w.Write([]byte("# Acme Skill\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := &Client{
		RegistryURL:    srv.URL + "/registry.json",
		ReleaseBaseURL: srv.URL + "/releases",
		SkillBaseURL:   srv.URL + "/skills",
		CachePath:      filepath.Join(t.TempDir(), "registry.json"),
	}

	res, err := client.Install(context.Background(), "acme")
	require.NoError(t, err)
	assert.Equal(t, "binary", res.Kind)
	assert.Equal(t, goodHash, res.SHA256)
	assert.True(t, res.ZeroGo)
	assert.NotEmpty(t, res.SkillPath)

	binPath, ok := InstalledBinaryPath("acme")
	require.True(t, ok)
	assert.Equal(t, res.Path, binPath)

	// Tampered checksum must fail closed.
	tamperChecksum = true
	_, err = client.Install(context.Background(), "acme")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestEnsureInstalledAutoHydratesCatalogAppOnce(t *testing.T) {
	isolatePP(t)

	fakeBin := []byte("#!/bin/sh\necho hydrated\n")
	sum := sha256.Sum256(fakeBin)
	goodHash := hex.EncodeToString(sum[:])
	asset := releaseAssetName("hyd-pp-cli", runtime.GOOS, runtime.GOARCH)

	cat := Catalog{
		SchemaVersion: 2,
		Entries: []Entry{
			{
				Name:        "hyd",
				Category:    "productivity",
				Description: "Hydrated app",
				Release:     &ReleaseInfo{CLIName: "hyd-pp-cli", Version: "2026.11.1"},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.json":
			_ = json.NewEncoder(w).Encode(cat)
		case "/releases/hyd-current/checksums.txt":
			_, _ = fmt.Fprintf(w, "%s  %s\n", goodHash, asset)
		case "/releases/hyd-current/" + asset:
			_, _ = w.Write(fakeBin)
		case "/skills/pp-hyd/SKILL.md":
			_, _ = w.Write([]byte("# Hyd Skill\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := &Client{
		RegistryURL:    srv.URL + "/registry.json",
		ReleaseBaseURL: srv.URL + "/releases",
		SkillBaseURL:   srv.URL + "/skills",
		CachePath:      filepath.Join(t.TempDir(), "registry.json"),
	}

	// First call auto-hydrates.
	p1, downloaded1, err := client.EnsureInstalled(context.Background(), "hyd")
	require.NoError(t, err)
	assert.True(t, downloaded1)
	assert.FileExists(t, p1)

	// Second call reuses verified pin without re-downloading.
	p2, downloaded2, err := client.EnsureInstalled(context.Background(), "hyd")
	require.NoError(t, err)
	assert.False(t, downloaded2)
	assert.Equal(t, p1, p2)

	// Unknown slug fails closed.
	_, _, err = client.EnsureInstalled(context.Background(), "not-in-catalog")
	require.Error(t, err)
}

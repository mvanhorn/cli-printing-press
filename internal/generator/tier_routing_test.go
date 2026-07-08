package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestTierRoutingEmitsTierAwareClientAndCommands(t *testing.T) {
	t.Parallel()
	apiSpec := minimalSpec("tiered")
	apiSpec.TierRouting = spec.TierRoutingConfig{
		DefaultTier: "free",
		Tiers: map[string]spec.TierConfig{
			"free": {
				Auth: spec.AuthConfig{Type: "none"},
			},
			"paid": {
				BaseURL: "https://paid.api.example.com",
				Auth: spec.AuthConfig{
					Type:    "api_key",
					In:      "query",
					Header:  "api_key",
					EnvVars: []string{"TIERED_PAID_KEY"},
				},
			},
			"enterprise": {
				Auth: spec.AuthConfig{
					Type:    "bearer_token",
					Header:  "Authorization",
					Format:  "Bearer {access_token}",
					EnvVars: []string{"TIERED_ENTERPRISE_TOKEN"},
				},
			},
		},
	}
	items := apiSpec.Resources["items"]
	items.Endpoints["premium"] = spec.Endpoint{
		Method:      "GET",
		Path:        "/items/premium",
		Description: "List premium items",
		Tier:        "paid",
	}
	items.Endpoints["enterprise"] = spec.Endpoint{
		Method:      "GET",
		Path:        "/items/enterprise",
		Description: "List enterprise items",
		Tier:        "enterprise",
	}
	items.SubResources = map[string]spec.Resource{
		"comments": {
			Tier: "paid",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/items/{item_id}/comments",
					Description: "List paid comments",
					Pagination:  &spec.Pagination{Type: "cursor", CursorParam: "cursor", LimitParam: "limit"},
				},
			},
		},
	}
	apiSpec.Resources["items"] = items
	apiSpec.MCP = spec.MCPConfig{
		Intents: []spec.Intent{
			{
				Name:        "premium_lookup",
				Description: "Look up premium items",
				Steps: []spec.IntentStep{
					{Endpoint: "items.premium", Capture: "premium"},
				},
				Returns: "premium",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "tiered-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Regexp(t, `\brequestTier\s+string\b`, clientSrc)
	require.Regexp(t, `\blimiters\s+map\[string\]\*cliutil\.AdaptiveLimiter\b`, clientSrc)
	require.Contains(t, clientSrc, "next.limiter = c.limiterForTier(tier)")
	require.Regexp(t, `"paid":\s+newRateLimiter\(rateLimit\)`, clientSrc)
	require.Contains(t, clientSrc, `case "free":`)
	require.Contains(t, clientSrc, `case "paid":`)
	require.Contains(t, clientSrc, `return strings.TrimRight("https://paid.api.example.com", "/")`)
	require.Contains(t, clientSrc, `os.Getenv("TIERED_PAID_KEY")`)
	require.Regexp(t, `"access_token":\s+tierValue0`, clientSrc)
	require.Contains(t, clientSrc, `q.Set(authInfo.Name, authHeader)`)
	require.Contains(t, clientSrc, `key += "|base_url=" + c.BaseURL`)
	require.Contains(t, clientSrc, `key += "|tier=" + c.requestTier + "|tier_base_url=" + c.baseURLForRequest()`)

	freeCmd := readGeneratedFile(t, outputDir, "internal", "cli", "items_list.go")
	require.Contains(t, freeCmd, `c = c.WithTier("free")`)
	paidCmd := readGeneratedFile(t, outputDir, "internal", "cli", "items_premium.go")
	require.Contains(t, paidCmd, `c = c.WithTier("paid")`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpSrc, `makeAPIHandler("GET", "/items", "free", true, false`)
	require.Contains(t, mcpSrc, `makeAPIHandler("GET", "/items/premium", "paid", true, false`)
	require.Contains(t, mcpSrc, `c = c.WithTier(tier)`)
	require.Contains(t, mcpSrc, `"tier_routing": map[string]any`)
	require.Regexp(t, `"items_premium":\s+"paid"`, mcpSrc)

	intentsSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "intents.go")
	require.Regexp(t, `\btier\s+string\b`, intentsSrc)
	require.Contains(t, intentsSrc, `"items.premium": {method: "GET", path: "/items/premium", tier: "paid"}`)
	require.Contains(t, intentsSrc, `c = c.WithTier(ep.tier)`)

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, `func syncClientForResource(c *client.Client, resource string) *client.Client`)
	require.Regexp(t, `"items":\s+"free"`, syncSrc)
	require.Regexp(t, `"comments":\s+"paid"`, syncSrc)

	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	require.Contains(t, doctorSrc, `report["tier_env_vars"] = tierEnvStatus`)
	require.Contains(t, doctorSrc, `os.Getenv("TIERED_PAID_KEY")`)

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.NotContains(t, configSrc, "TIERED_PAID_KEY",
		"tier credentials must stay env-only and not become serialized config fields")

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = outputDir
	tidyOutput, err := tidy.CombinedOutput()
	require.NoError(t, err, string(tidyOutput))

	cmd := exec.Command("go", "run", "./cmd/tiered-pp-cli", "items", "list", "--dry-run", "--json")
	cmd.Dir = outputDir
	cmd.Env = append(os.Environ(), "TIERED_PAID_KEY=")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.NotContains(t, string(output), "TIERED_PAID_KEY")

	cmd = exec.Command("go", "run", "./cmd/tiered-pp-cli", "items", "enterprise", "--dry-run", "--json")
	cmd.Dir = outputDir
	cmd.Env = append(os.Environ(), "TIERED_ENTERPRISE_TOKEN=enterprise-secret")
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "Authorization: ****cret")

	codeSpec := minimalSpec("tiered-code")
	codeSpec.TierRouting = apiSpec.TierRouting
	codeItems := codeSpec.Resources["items"]
	codeItems.Endpoints["premium"] = spec.Endpoint{
		Method:      "GET",
		Path:        "/items/premium",
		Description: "List premium items",
		Tier:        "paid",
	}
	codeSpec.Resources["items"] = codeItems
	codeSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	codeOutputDir := filepath.Join(t.TempDir(), "tiered-code-pp-cli")
	require.NoError(t, New(codeSpec, codeOutputDir).Generate())
	codeOrchSrc := readGeneratedFile(t, codeOutputDir, "internal", "mcp", "code_orch.go")
	require.Regexp(t, `\bTier\s+string\b`, codeOrchSrc)
	require.Regexp(t, `Tier:\s+"paid"`, codeOrchSrc)
	require.Regexp(t, `"tier":\s+r\.ep\.Tier`, codeOrchSrc)
	require.Contains(t, codeOrchSrc, `c = c.WithTier(ep.Tier)`)
}

func TestTierRoutingRedirectsStripCustomHeaderCrossHost(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("tier-redirect")
	apiSpec.TierRouting = spec.TierRoutingConfig{
		DefaultTier: "free",
		Tiers: map[string]spec.TierConfig{
			"free": {Auth: spec.AuthConfig{Type: "none"}},
			"paid": {
				Auth: spec.AuthConfig{
					Type:    "api_key",
					In:      "header",
					Header:  "X-Tier-Key",
					EnvVars: []string{"TIER_REDIRECT_PAID_KEY"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "tier-redirect-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const clientTest = `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tier-redirect-pp-cli/internal/config"
)

func TestTierRedirectCustomHeaderStripping(t *testing.T) {
	t.Setenv("TIER_REDIRECT_PAID_KEY", "paid-secret")

	sameHostFinalHeader := ""
	sameHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same-start":
			http.Redirect(w, r, "/same-final", http.StatusFound)
		case "/same-final":
			sameHostFinalHeader = r.Header.Get("X-Tier-Key")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(` + "`{}`" + `))
		default:
			http.NotFound(w, r)
		}
	}))
	defer sameHost.Close()

	cfg := &config.Config{BaseURL: sameHost.URL}
	c := New(cfg, time.Second, 0).WithTier("paid")
	c.NoCache = true
	if _, err := c.Get(context.Background(), "/same-start", nil); err != nil {
		t.Fatalf("same-host redirect request failed: %v", err)
	}
	if sameHostFinalHeader != "paid-secret" {
		t.Fatalf("same-host redirect X-Tier-Key = %q, want paid-secret", sameHostFinalHeader)
	}

	crossHostFinalHeader := "not-called"
	crossHostTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		crossHostFinalHeader = r.Header.Get("X-Tier-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`{}`" + `))
	}))
	defer crossHostTarget.Close()

	crossHostStart := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, crossHostTarget.URL+"/cross-final", http.StatusFound)
	}))
	defer crossHostStart.Close()

	cfg = &config.Config{BaseURL: crossHostStart.URL}
	c = New(cfg, time.Second, 0).WithTier("paid")
	c.NoCache = true
	if _, err := c.Get(context.Background(), "/cross-start", nil); err != nil {
		t.Fatalf("cross-host redirect request failed: %v", err)
	}
	if crossHostFinalHeader != "" {
		t.Fatalf("cross-host redirect leaked X-Tier-Key = %q", crossHostFinalHeader)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "tier_redirect_test.go"), []byte(clientTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "TestTierRedirectCustomHeaderStripping", "-count=1")
}

func readGeneratedFile(t *testing.T, root string, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	require.NoError(t, err)
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

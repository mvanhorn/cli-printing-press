package heal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnoseAndApplySurgicalPatch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("PP_HOME", filepath.Join(tmp, ".pp"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/old":
			http.NotFound(w, r)
		case "/v2/new":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"id":"1"}]}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	broken := &cartridge.Cartridge{
		Schema:      cartridge.SchemaVersionV1,
		Name:        "shop",
		Version:     "1.0",
		Description: "Shop API",
		Trust:       cartridge.TrustPolicy{AllowedHosts: []string{u.Hostname()}, ReadOnly: true},
		Commands: map[string]cartridge.CommandSpec{
			"list": {
				Description: "List products",
				Method:      "GET",
				URL:         srv.URL + "/v1/old",
				Extract:     "$.items[*]",
			},
		},
	}

	diag, err := Diagnose(context.Background(), broken, "list", nil, cartridge.RunOptions{})
	require.NoError(t, err)
	assert.Equal(t, "upstream_drift", diag.Class)
	assert.Equal(t, 404, diag.HTTPStatus)

	fresh := &cartridge.Cartridge{
		Schema:      cartridge.SchemaVersionV1,
		Name:        "shop",
		Version:     "2.0",
		Description: "Shop API",
		Trust:       cartridge.TrustPolicy{AllowedHosts: []string{u.Hostname()}, ReadOnly: true},
		Commands: map[string]cartridge.CommandSpec{
			"list": {
				Description: "List products",
				Method:      "GET",
				URL:         srv.URL + "/v2/new",
				Extract:     "$.items[*]",
			},
		},
	}

	prDir := filepath.Join(tmp, "pr-bundle")
	res, err := ApplySurgicalPatch(broken, fresh, "list", prDir)
	require.NoError(t, err)
	assert.NotEmpty(t, res.NewSHA256)
	assert.FileExists(t, filepath.Join(prDir, "cartridge.yaml"))
	assert.FileExists(t, filepath.Join(prDir, "PR_BODY.md"))

	// Verify healed cartridge now succeeds live!
	healed, err := cartridge.LoadFile(res.UpdatedPath)
	require.NoError(t, err)
	diagAfter, err := Diagnose(context.Background(), healed, "list", nil, cartridge.RunOptions{})
	require.NoError(t, err)
	assert.Equal(t, "healthy", diagAfter.Class)

	prBody, err := os.ReadFile(filepath.Join(prDir, "PR_BODY.md"))
	require.NoError(t, err)
	assert.Contains(t, string(prBody), "/v2/new")
}

func TestApplySurgicalPatchSafetyGuardsAndSecretScrubbing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("PP_HOME", filepath.Join(tmp, ".pp"))

	// 1. Refuse unrelated / ambiguous capture (no matching command, different host).
	target := &cartridge.Cartridge{
		Schema:      cartridge.SchemaVersionV1,
		Name:        "authapp",
		Version:     "1.0",
		Description: "Auth app",
		Trust:       cartridge.TrustPolicy{AllowedHosts: []string{"api.good.example"}, ReadOnly: true},
		Auth:        cartridge.AuthConfig{Strategy: cartridge.AuthStrategyEnvBearer, EnvVar: "AUTHAPP_TOKEN"},
		Commands: map[string]cartridge.CommandSpec{
			"orders": {
				Method:  "GET",
				URL:     "https://api.good.example/v1/orders?api_key=supersecret123",
				Headers: map[string]string{"Authorization": "Bearer live-secret", "Accept": "application/json"},
			},
		},
	}
	unrelated := &cartridge.Cartridge{
		Schema:      cartridge.SchemaVersionV1,
		Name:        "authapp",
		Version:     "2.0",
		Description: "Auth app",
		Trust:       cartridge.TrustPolicy{AllowedHosts: []string{"evil.example"}, ReadOnly: true},
		Commands: map[string]cartridge.CommandSpec{
			"profile": {Method: "GET", URL: "https://evil.example/steal"},
		},
	}
	_, err := ApplySurgicalPatch(target, unrelated, "orders", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no matching command")

	// 2. Refuse moving authenticated command to an unpinned host.
	movedHost := &cartridge.Cartridge{
		Schema:      cartridge.SchemaVersionV1,
		Name:        "authapp",
		Version:     "2.0",
		Description: "Auth app",
		Trust:       cartridge.TrustPolicy{AllowedHosts: []string{"evil.example"}, ReadOnly: true},
		Commands: map[string]cartridge.CommandSpec{
			"orders": {Method: "GET", URL: "https://evil.example/v2/orders"},
		},
	}
	_, err = ApplySurgicalPatch(target, movedHost, "orders", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unpinned host")

	// 3. Refuse healing read-only command into a mutating POST.
	mutated := &cartridge.Cartridge{
		Schema:      cartridge.SchemaVersionV1,
		Name:        "authapp",
		Version:     "2.0",
		Description: "Auth app",
		Trust:       cartridge.TrustPolicy{AllowedHosts: []string{"api.good.example"}},
		Commands: map[string]cartridge.CommandSpec{
			"orders": {Method: "POST", URL: "https://api.good.example/v2/orders", Mutates: true},
		},
	}
	_, err = ApplySurgicalPatch(target, mutated, "orders", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only command")

	// 4. PR bundle scrubs secret headers and credential query parameters from both YAML and PR_BODY.md.
	sameHost := &cartridge.Cartridge{
		Schema:      cartridge.SchemaVersionV1,
		Name:        "authapp",
		Version:     "2.0",
		Description: "Auth app",
		Trust:       cartridge.TrustPolicy{AllowedHosts: []string{"api.good.example"}, ReadOnly: true},
		Commands: map[string]cartridge.CommandSpec{
			"orders": {Method: "GET", URL: "https://api.good.example/v2/orders?api_key=newsecret456"},
		},
	}
	prDir := filepath.Join(tmp, "scrubbed-pr")
	_, err = ApplySurgicalPatch(target, sameHost, "orders", prDir)
	require.NoError(t, err)

	exportedYAML, err := os.ReadFile(filepath.Join(prDir, "cartridge.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(exportedYAML), "live-secret")
	assert.NotContains(t, string(exportedYAML), "newsecret456")
	assert.Contains(t, string(exportedYAML), "Accept")

	prBody, err := os.ReadFile(filepath.Join(prDir, "PR_BODY.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(prBody), "supersecret123")
	assert.NotContains(t, string(prBody), "newsecret456")
	assert.Contains(t, string(prBody), "REDACTED")
}

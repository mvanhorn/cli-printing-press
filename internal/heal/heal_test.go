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

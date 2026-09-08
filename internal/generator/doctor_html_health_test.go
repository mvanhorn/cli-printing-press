package generator

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedDoctorTreatsHTML200AsReachable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Home</title></head><body><h1>Welcome</h1></body></html>`))
	}))
	t.Cleanup(server.Close)

	apiSpec := htmlHealthSpec("html-health")
	apiSpec.BaseURL = server.URL
	_, binaryPath := buildGeneratedBinary(t, apiSpec)

	payload, err := runDoctorJSON(t, binaryPath, htmlHealthEnv(t, apiSpec, server.URL), "--fail-on", "error")
	require.NoError(t, err, payload)
	assert.Equal(t, "reachable (HTML body at /)", payload["api"])
	assert.NotContains(t, payload["api"], "unreachable")
	assert.NotContains(t, payload["api"], "expected JSON")
}

func TestGeneratedDoctorDetectsCloudflareInterstitialOnHTML200(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Just a moment...</title></head><body>challenges.cloudflare.com</body></html>`))
	}))
	t.Cleanup(server.Close)

	apiSpec := htmlHealthSpec("html-cf")
	apiSpec.BaseURL = server.URL
	_, binaryPath := buildGeneratedBinary(t, apiSpec)

	payload, err := runDoctorJSON(t, binaryPath, htmlHealthEnv(t, apiSpec, server.URL))
	require.NoError(t, err, payload)
	api, _ := payload["api"].(string)
	assert.Contains(t, api, "blocked by Cloudflare interstitial")
	assert.NotContains(t, api, "unreachable")
	assert.NotContains(t, api, "expected JSON")
}

func TestGeneratedDoctorJSONHealthStaysReachable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	apiSpec := htmlHealthSpec("json-health")
	apiSpec.BaseURL = server.URL
	_, binaryPath := buildGeneratedBinary(t, apiSpec)

	payload, err := runDoctorJSON(t, binaryPath, htmlHealthEnv(t, apiSpec, server.URL), "--fail-on", "error")
	require.NoError(t, err, payload)
	assert.Equal(t, "reachable", payload["api"])
}

func htmlHealthSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.HealthCheckPath = "/"
	return apiSpec
}

func htmlHealthEnv(t *testing.T, apiSpec *spec.APISpec, baseURL string) []string {
	t.Helper()
	prefix := naming.EnvPrefix(apiSpec.Name)
	return append(doctorEnv(t.TempDir(), prefix), prefix+"_BASE_URL="+baseURL)
}

package generator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	doctorHomeHTML = `<!doctype html><html><head><title>Home</title></head><body><h1>Welcome</h1></body></html>`
	doctorWallHTML = `<!doctype html><html><head><title>Just a moment...</title></head><body>challenges.cloudflare.com</body></html>`
	// Title is not a challenge phrase. The wall signal is only in the body,
	// which HTML summarization drops from APIError.Body.
	doctorTurnstileHTML = `<!doctype html><html><head><title>Please wait</title></head><body>challenges.cloudflare.com</body></html>`
)

func TestGeneratedDoctorHTMLHealth(t *testing.T) {
	t.Parallel()

	apiSpec := htmlHealthSpec("html-health")
	outputDir, binaryPath := buildGeneratedBinary(t, apiSpec)
	modulePath := generatedModulePath(t, outputDir)
	writeDoctorInterstitialUnitTests(t, outputDir, modulePath)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestDoctorInterstitial|TestDoctorCheckPresentation|TestDoctorReachabilityBody", "-count=1")
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestAPIErrorRawBodyKeepsChallengePage", "-count=1")

	envFor := func(baseURL string) []string {
		t.Helper()
		return htmlHealthEnv(t, apiSpec, baseURL)
	}

	t.Run("html 200 stays reachable", func(t *testing.T) {
		server := newDoctorProbeServer(t, http.StatusOK, "text/html; charset=utf-8", doctorHomeHTML)
		payload, err := runDoctorJSON(t, binaryPath, envFor(server.URL), "--fail-on", "error")
		require.NoError(t, err, payload)
		assert.Equal(t, "reachable (HTML body at /)", payload["api"])

		out, err := runDoctorText(t, binaryPath, envFor(server.URL), "--fail-on", "error")
		require.NoError(t, err, out)
		line := doctorAPILine(out)
		assert.Contains(t, line, "OK")
		assert.NotContains(t, line, "FAIL")
	})

	t.Run("json 200 stays reachable", func(t *testing.T) {
		server := newDoctorProbeServer(t, http.StatusOK, "application/json", `{"ok":true}`)
		payload, err := runDoctorJSON(t, binaryPath, envFor(server.URL), "--fail-on", "error")
		require.NoError(t, err, payload)
		assert.Equal(t, "reachable", payload["api"])
	})

	t.Run("non-interstitial 403 stays reachable", func(t *testing.T) {
		server := newDoctorProbeServer(t, http.StatusForbidden, "text/html; charset=utf-8", doctorHomeHTML)
		payload, err := runDoctorJSON(t, binaryPath, envFor(server.URL), "--fail-on", "error")
		require.NoError(t, err, payload)
		api, ok := payload["api"].(string)
		require.True(t, ok, "non-interstitial api report = %#v", payload["api"])
		assert.Contains(t, api, "reachable (HTTP 403")
		assert.NotContains(t, api, "blocked")

		out, err := runDoctorText(t, binaryPath, envFor(server.URL))
		require.NoError(t, err, out)
		line := doctorAPILine(out)
		assert.Contains(t, line, "OK")
		assert.NotContains(t, line, "FAIL")
	})

	cases := []struct {
		name       string
		status     int
		body       string
		wantHTTP   bool
		wantVendor string
	}{
		{name: "interstitial 200", status: http.StatusOK, body: doctorWallHTML, wantVendor: "Cloudflare"},
		{name: "interstitial 403", status: http.StatusForbidden, body: doctorWallHTML, wantHTTP: true, wantVendor: "Cloudflare"},
		{name: "interstitial 503", status: http.StatusServiceUnavailable, body: doctorWallHTML, wantHTTP: true, wantVendor: "Cloudflare"},
		{name: "turnstile body 403", status: http.StatusForbidden, body: doctorTurnstileHTML, wantHTTP: true, wantVendor: "Cloudflare"},
		{name: "mixed-case title 200", status: http.StatusOK, body: strings.Replace(doctorWallHTML, "Just a moment", "JuSt A mOmEnT", 1), wantVendor: "Cloudflare"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newDoctorProbeServer(t, tc.status, "text/html; charset=utf-8", tc.body)
			env := envFor(server.URL)

			payload, err := runDoctorJSON(t, binaryPath, env, "--fail-on", "error")
			require.Error(t, err, "blocked probe must fail --fail-on=error; payload=%v", payload)
			section, ok := payload["api"].(map[string]any)
			require.True(t, ok, "api report = %#v", payload["api"])
			assert.Equal(t, "error", section["status"])
			detail, _ := section["detail"].(string)
			assert.Contains(t, detail, "blocked by "+tc.wantVendor+" interstitial")
			assert.NotContains(t, detail, "unreachable")
			if tc.wantHTTP {
				assert.Contains(t, detail, fmt.Sprintf("HTTP %d", tc.status))
			} else {
				assert.NotContains(t, detail, "HTTP ")
			}

			out, textErr := runDoctorText(t, binaryPath, env, "--fail-on", "error")
			require.Error(t, textErr, out)
			line := doctorAPILine(out)
			assert.Contains(t, line, "FAIL")
			assert.NotContains(t, line, "OK")
			assert.Contains(t, line, "blocked by "+tc.wantVendor+" interstitial")
			assert.NotContains(t, line, "map[")

			out, textErr = runDoctorText(t, binaryPath, env)
			require.NoError(t, textErr, out)
			line = doctorAPILine(out)
			assert.Contains(t, line, "FAIL")
			assert.NotContains(t, line, "OK")
		})
	}
}

func newDoctorProbeServer(t *testing.T, status int, contentType, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func runDoctorText(t *testing.T, binaryPath string, env []string, extraArgs ...string) (string, error) {
	t.Helper()
	args := append([]string{"doctor"}, extraArgs...)
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func doctorAPILine(out string) string {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, " API:") {
			return strings.TrimSpace(line)
		}
	}
	return ""
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

func writeDoctorInterstitialUnitTests(t *testing.T, outputDir, modulePath string) {
	t.Helper()
	cliTest := strings.ReplaceAll(doctorInterstitialCLITest, "__MODULE__", modulePath)
	clientTest := strings.ReplaceAll(doctorInterstitialClientTest, "__MODULE__", modulePath)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "doctor_interstitial_test.go"), []byte(cliTest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "apierror_raw_body_test.go"), []byte(clientTest), 0o644))
}

const doctorInterstitialCLITest = `package cli

import (
	"strings"
	"testing"

	"__MODULE__/internal/client"
)

func TestDoctorReachabilityBody(t *testing.T) {
	raw := ` + "`" + `<!doctype html><html><head><title>Just a moment...</title></head><body>challenges.cloudflare.com</body></html>` + "`" + `
	home := ` + "`" + `<!doctype html><html><head><title>Home</title></head><body>Welcome</body></html>` + "`" + `
	turnstile := ` + "`" + `<!doctype html><html><head><title>Please wait</title></head><body>challenges.cloudflare.com</body></html>` + "`" + `
	summary := "HTML error page (90 bytes): Just a moment..."

	if got := looksLikeDoctorInterstitial([]byte(raw)); got != "Cloudflare" {
		t.Fatalf("raw wall = %q", got)
	}
	if got := looksLikeDoctorInterstitial([]byte(summary)); got != "" {
		t.Fatalf("collapsed summary classified as %q; detection must use the raw body", got)
	}
	if got := looksLikeDoctorInterstitial([]byte(home)); got != "" {
		t.Fatalf("home page classified as %q", got)
	}
	if got := looksLikeDoctorInterstitial([]byte(turnstile)); got != "Cloudflare" {
		t.Fatalf("turnstile body = %q", got)
	}
	mixed := strings.ReplaceAll(raw, "Just a moment", "JuSt A mOmEnT")
	if got := looksLikeDoctorInterstitial([]byte(mixed)); got != "Cloudflare" {
		t.Fatalf("mixed-case title = %q", got)
	}

	if doctorReachabilityBody(nil) != nil {
		t.Fatal("nil error returned a body")
	}
	got := doctorReachabilityBody(&client.APIError{Body: summary, RawBody: home})
	if vendor := looksLikeDoctorInterstitial(got); vendor != "" {
		t.Fatalf("raw home page lost to summary: %q", vendor)
	}
	got = doctorReachabilityBody(&client.APIError{Body: "HTML error page (40 bytes): Home", RawBody: turnstile})
	if vendor := looksLikeDoctorInterstitial(got); vendor != "Cloudflare" {
		t.Fatalf("raw turnstile body = %q", vendor)
	}
	got = doctorReachabilityBody(&client.APIError{Body: raw})
	if vendor := looksLikeDoctorInterstitial(got); vendor != "Cloudflare" {
		t.Fatalf("empty RawBody fallback = %q", vendor)
	}
	if len(doctorReachabilityBody(&client.APIError{})) != 0 {
		t.Fatal("empty error returned a body")
	}
}

func TestDoctorCheckPresentation(t *testing.T) {
	ind, text := doctorCheckPresentation(doctorSection("error", "blocked by Cloudflare interstitial — wall"))
	if !strings.Contains(ind, "FAIL") || strings.Contains(ind, "OK") || strings.Contains(text, "map[") {
		t.Fatalf("indicator=%q text=%q", ind, text)
	}
	if text != "blocked by Cloudflare interstitial — wall" {
		t.Fatalf("text = %q", text)
	}
	ind, text = doctorCheckPresentation(doctorSection(" Error ", "blocked by vendor interstitial"))
	if !strings.Contains(ind, "FAIL") || text != "blocked by vendor interstitial" {
		t.Fatalf("folded status indicator=%q text=%q", ind, text)
	}
	ind, text = doctorCheckPresentation(map[string]any{"status": "error"})
	if !strings.Contains(ind, "FAIL") || text != "error" {
		t.Fatalf("blank detail indicator=%q text=%q", ind, text)
	}
	ind, text = doctorCheckPresentation(map[string]any{"status": "error", "detail": "   "})
	if !strings.Contains(ind, "FAIL") || text != "error" {
		t.Fatalf("whitespace detail indicator=%q text=%q", ind, text)
	}
	ind, text = doctorCheckPresentation("reachable (HTML body at /)")
	if !strings.Contains(ind, "OK") || strings.Contains(ind, "FAIL") || text != "reachable (HTML body at /)" {
		t.Fatalf("reachable indicator=%q text=%q", ind, text)
	}
	ind, _ = doctorCheckPresentation("unreachable: dial tcp")
	if !strings.Contains(ind, "FAIL") {
		t.Fatalf("unreachable indicator = %q", ind)
	}
	ind, _ = doctorCheckPresentation("optional — not configured")
	if !strings.Contains(ind, "INFO") {
		t.Fatalf("optional indicator = %q", ind)
	}
	ind, _ = doctorCheckPresentation("WARN not verified (HTTP 404 from /)")
	if !strings.Contains(ind, "WARN") || strings.Contains(ind, "FAIL") {
		t.Fatalf("warn prefix indicator = %q", ind)
	}
	ind, _ = doctorCheckPresentation("not required")
	if !strings.Contains(ind, "OK") || strings.Contains(ind, "WARN") {
		t.Fatalf("not required indicator = %q", ind)
	}
	ind, text = doctorCheckPresentation(doctorSection("ok", "blocked by Cloudflare interstitial — wall"))
	if strings.Contains(ind, "FAIL") {
		t.Fatalf("non-error status rendered FAIL: %q %q", ind, text)
	}
}

func TestDoctorInterstitialFailOn(t *testing.T) {
	blocked := map[string]any{"api": doctorSection("error", "blocked by Cloudflare interstitial — wall")}
	if err := doctorExitForFailOn("error", blocked); err == nil {
		t.Fatal("status=error did not trip --fail-on=error")
	}
	if err := doctorExitForFailOn("WARN", blocked); err == nil {
		t.Fatal("status=error did not trip --fail-on=warn")
	}
	if err := doctorExitForFailOn("Stale", blocked); err == nil {
		t.Fatal("status=error did not trip --fail-on=stale")
	}
	if err := doctorExitForFailOn("", blocked); err != nil {
		t.Fatalf("empty gate tripped: %v", err)
	}
	if err := doctorExitForFailOn("error", map[string]any{"api": "reachable (HTML body at /)"}); err != nil {
		t.Fatalf("reachable HTML tripped --fail-on=error: %v", err)
	}
	if err := doctorExitForFailOn("error", map[string]any{"api": "reachable"}); err != nil {
		t.Fatalf("reachable JSON tripped --fail-on=error: %v", err)
	}
	if err := doctorExitForFailOn("error", map[string]any{"api": "reachable (HTTP 403 at /)"}); err != nil {
		t.Fatalf("non-interstitial 403 tripped --fail-on=error: %v", err)
	}
	if err := doctorExitForFailOn("error", map[string]any{"api": doctorSection("ok", "blocked by Cloudflare interstitial — wall")}); err != nil {
		t.Fatalf("status=ok tripped --fail-on=error: %v", err)
	}
	if err := doctorExitForFailOn("nope", blocked); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown gate = %v", err)
	}
}
`

const doctorInterstitialClientTest = `package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"__MODULE__/internal/config"
)

func TestAPIErrorRawBodyKeepsChallengePage(t *testing.T) {
	const token = "super/secret-token"
	page := "<!doctype html><html><head><title>Just a moment...</title></head><body>challenges.cloudflare.com " + token + " " + url.QueryEscape(token) + "</body></html>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(server.Close)

	c := New(&config.Config{BaseURL: server.URL, AuthHeaderVal: token}, time.Second, 0)
	c.NoCache = true

	_, err := c.GetWithHeaders(context.Background(), "/", nil, map[string]string{HTMLResponseHeader: "true"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want APIError", err)
	}
	if strings.Contains(apiErr.RawBody, token) || strings.Contains(apiErr.RawBody, url.QueryEscape(token)) {
		t.Fatalf("RawBody kept the credential: %q", apiErr.RawBody)
	}
	if strings.Contains(apiErr.Error(), token) || strings.Contains(apiErr.Error(), "<title") {
		t.Fatalf("Error() leaked raw HTML or the credential: %q", apiErr.Error())
	}
	if !strings.Contains(apiErr.RawBody, "<title>Just a moment") || !strings.Contains(apiErr.RawBody, "challenges.cloudflare.com") {
		t.Fatalf("RawBody = %q, want the challenge page", apiErr.RawBody)
	}
	if !strings.Contains(apiErr.Body, "HTML error page") || strings.Contains(apiErr.Body, "<title") {
		t.Fatalf("Body = %q, want the collapsed summary", apiErr.Body)
	}
	if looksLike := strings.Contains(strings.ToLower(apiErr.RawBody), "<title>just a moment"); !looksLike {
		t.Fatalf("RawBody lost the title anchor: %q", apiErr.RawBody)
	}

	_, err = c.GetWithHeaders(context.Background(), "/", nil, map[string]string{BinaryResponseHeader: "true"})
	var binaryErr *APIError
	if !errors.As(err, &binaryErr) {
		t.Fatalf("binary error = %v, want APIError", err)
	}
	if binaryErr.RawBody != "" {
		t.Fatalf("binary RawBody = %q, want empty", binaryErr.RawBody)
	}

	mixedToken := "SuPeR/SeCrEt-ToKeN"
	mixedPage := strings.ReplaceAll(page, token, mixedToken)
	mixedPage = strings.ReplaceAll(mixedPage, url.QueryEscape(token), url.QueryEscape(mixedToken))
	mixedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(mixedPage))
	}))
	t.Cleanup(mixedServer.Close)
	mixedClient := New(&config.Config{BaseURL: mixedServer.URL, AuthHeaderVal: token}, time.Second, 0)
	mixedClient.NoCache = true
	_, err = mixedClient.GetWithHeaders(context.Background(), "/", nil, map[string]string{HTMLResponseHeader: "true"})
	var mixedErr *APIError
	if !errors.As(err, &mixedErr) {
		t.Fatalf("mixed-case error = %v, want APIError", err)
	}
	foldedRaw := strings.ToLower(mixedErr.RawBody)
	foldedMsg := strings.ToLower(mixedErr.Error())
	if strings.Contains(foldedRaw, strings.ToLower(token)) || strings.Contains(foldedRaw, strings.ToLower(url.QueryEscape(token))) || strings.Contains(foldedMsg, strings.ToLower(token)) {
		t.Fatalf("mixed-case credential survived: raw=%q err=%q", mixedErr.RawBody, mixedErr.Error())
	}
}
`

package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestNoAuthForbiddenClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		auth   spec.AuthConfig
		authed bool
	}{
		{name: "none", auth: spec.AuthConfig{Type: "none"}},
		{name: "empty", auth: spec.AuthConfig{}},
		{
			name: "apikey",
			auth: spec.AuthConfig{
				Type:    "api_key",
				Header:  "Authorization",
				Format:  "Bearer {token}",
				EnvVars: []string{"MYAPI_TOKEN"},
			},
			authed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec("forbid403" + tt.name)
			apiSpec.Auth = tt.auth
			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			assertForbiddenExitSources(t, outputDir, tt.authed)
			emitted := assertHelperExitCodesDocumented(t, outputDir)
			if tt.authed {
				require.True(t, emitted[4], "authed helpers must be able to exit 4")
			} else {
				require.False(t, emitted[4], "no-auth helpers must not emit exit 4")
			}
			require.True(t, emitted[5])
			require.True(t, emitted[7])

			requireGeneratedCompiles(t, outputDir)
			writeForbiddenRuntimeTests(t, outputDir, naming.CLI(apiSpec.Name), naming.EnvPrefix(apiSpec.Name)+"_BASE_URL", tt.authed)
			runGoCommandRequired(t, outputDir, "test", "./internal/cli", "./internal/cliutil", "./internal/mcp", "-run", "^TestClassifyForbidden$|^TestClientForbiddenRoundTrip$|^TestTransportBlockMarkers$|^TestMCPForbiddenClassification$", "-count=1")
		})
	}
}

func assertForbiddenExitSources(t *testing.T, outputDir string, authed bool) {
	t.Helper()
	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	tools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	readme := readGeneratedFile(t, outputDir, "README.md")
	skill := readGeneratedFile(t, outputDir, "SKILL.md")
	exitLine := readmeExitLine(t, readme)

	if authed {
		require.Contains(t, helpers, "return authErr(")
		require.NotContains(t, helpers, "LooksLikeTransportBlock")
		require.NotContains(t, tools, "LooksLikeTransportBlock")
		require.NotContains(t, exitLine, "or blocked")
		require.Contains(t, skill, "| 7 | Rate limited (wait and retry) |")
		require.Contains(t, skill, "| 4 | Authentication required |")
		return
	}

	require.Contains(t, helpers, "cliutil.LooksLikeTransportBlock(msg)")
	require.Contains(t, helpers, "return rateLimitErr(classified)")
	require.Contains(t, helpers, "return apiErr(classified)")
	require.NotContains(t, helpers, "return authErr(")
	require.NotContains(t, helpers, "func authErr(")
	require.Contains(t, tools, `prefix := "api error: "`)
	require.Contains(t, tools, `prefix = "rate limited: "`)
	require.NotContains(t, tools, `mcpToolError("permission denied: "`)
	require.Contains(t, exitLine, "`7` rate limited or blocked")
	require.NotContains(t, exitLine, "`4`")
	require.Contains(t, skill, "| 7 | Rate limited or blocked by a firewall or bot challenge (back off; do not retry immediately) |")
	require.NotContains(t, skill, "| 4 | Authentication required |")
	require.Contains(t, helpers, "permission denied. This API is configured without credentials, so the service may be blocking the request by rate limit, geography, bot protection, or endpoint policy.")
}

func assertHelperExitCodesDocumented(t *testing.T, outputDir string) map[int]bool {
	t.Helper()
	emitted := emittedHelperExitCodes(t, outputDir)
	readmeCodes := readmeExitCodes(t, readGeneratedFile(t, outputDir, "README.md"))
	skillCodes := skillExitCodes(t, readGeneratedFile(t, outputDir, "SKILL.md"))
	for code := range emitted {
		require.Truef(t, readmeCodes[code], "README exit table missing emitted code %d", code)
		require.Truef(t, skillCodes[code], "SKILL exit table missing emitted code %d", code)
	}
	for _, code := range []int{2, 3, 5, 7, 10} {
		require.Truef(t, emitted[code], "helpers do not emit exit %d", code)
		require.Truef(t, readmeCodes[code], "README exit table missing %d", code)
		require.Truef(t, skillCodes[code], "SKILL exit table missing %d", code)
	}
	return emitted
}

func emittedHelperExitCodes(t *testing.T, outputDir string) map[int]bool {
	t.Helper()
	cliDir := filepath.Join(outputDir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	require.NoError(t, err)

	ctorRe := regexp.MustCompile(`(?s)func\s+[A-Za-z0-9_]+\s*\(\s*err\s+error\s*\)\s*error\s*\{\s*return\s+&cliError\{\s*code:\s*(\d+)\s*,`)
	literalRe := regexp.MustCompile(`cliError\{\s*code:\s*(\d+)\s*,`)
	jobConstRe := regexp.MustCompile(`const\s+ExitJobPending\s*=\s*(\d+)`)
	jobUseRe := regexp.MustCompile(`cliError\{\s*code:\s*ExitJobPending\b`)

	codes := map[int]bool{}
	var jobConst int
	var jobUsed bool
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body := readGeneratedFile(t, outputDir, "internal", "cli", entry.Name())
		for _, match := range ctorRe.FindAllStringSubmatch(body, -1) {
			codes[mustExitCode(t, match[1])] = true
		}
		for _, match := range literalRe.FindAllStringSubmatch(body, -1) {
			codes[mustExitCode(t, match[1])] = true
		}
		if match := jobConstRe.FindStringSubmatch(body); match != nil {
			jobConst = mustExitCode(t, match[1])
		}
		if jobUseRe.MatchString(body) {
			jobUsed = true
		}
	}
	if jobUsed {
		require.NotZero(t, jobConst)
		codes[jobConst] = true
	}
	require.NotEmpty(t, codes)
	return codes
}

func mustExitCode(t *testing.T, raw string) int {
	t.Helper()
	code, err := strconv.Atoi(raw)
	require.NoError(t, err)
	return code
}

func readmeExitLine(t *testing.T, readme string) string {
	t.Helper()
	for line := range strings.SplitSeq(readme, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Exit codes:") {
			return line
		}
	}
	t.Fatal("README has no Exit codes line")
	return ""
}

func readmeExitCodes(t *testing.T, readme string) map[int]bool {
	t.Helper()
	return exitCodesIn(t, readmeExitLine(t, readme), regexp.MustCompile("`(\\d+)`"))
}

func skillExitCodes(t *testing.T, skill string) map[int]bool {
	t.Helper()
	const start = "## Exit Codes"
	idx := strings.Index(skill, start)
	require.NotEqual(t, -1, idx, "SKILL.md has no Exit Codes section")
	rest := skill[idx+len(start):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	return exitCodesIn(t, rest, regexp.MustCompile(`(?m)^\|\s*(\d+)\s*\|`))
}

func exitCodesIn(t *testing.T, text string, re *regexp.Regexp) map[int]bool {
	t.Helper()
	codes := map[int]bool{}
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		codes[mustExitCode(t, match[1])] = true
	}
	require.NotEmpty(t, codes)
	return codes
}

func writeForbiddenRuntimeTests(t *testing.T, outputDir, modulePath, baseEnv string, authed bool) {
	t.Helper()
	authedLit := "false"
	if authed {
		authedLit = "true"
	}
	replacer := strings.NewReplacer(
		"__MODULE__", modulePath,
		"__AUTHED__", authedLit,
		"__BASE_ENV__", baseEnv,
	)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "forbidden_exit_test.go"), []byte(replacer.Replace(forbiddenCLITest)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cliutil", "transport_block_test.go"), []byte(forbiddenCLIUtilTest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "forbidden_exit_test.go"), []byte(replacer.Replace(forbiddenMCPTest)), 0o644))
}

const forbiddenCLITest = `package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	clientpkg "__MODULE__/internal/client"
	cfgpkg "__MODULE__/internal/config"
)

func TestClassifyForbidden(t *testing.T) {
	const hint = "permission denied. This API is configured without credentials, so the service may be blocking the request by rate limit, geography, bot protection, or endpoint policy."
	challenges := []string{
		"GET /items returned HTTP 403: Just a moment...",
		"GET /items returned HTTP 403: JUST A MOMENT",
		"GET /items returned HTTP 403: cf-mitigated: challenge",
		"GET /items returned HTTP 403: cf-mitigated: block",
		"GET /items returned HTTP 403: Attention Required! | Cloudflare",
		"GET /items returned HTTP 403: Sorry, you have been blocked",
	}
	plains := []string{
		"GET /items returned HTTP 403: {\"error\":\"forbidden\"}",
		"GET /items returned HTTP 403: permission denied for this resource",
		"GET /items returned HTTP 403: Attention required: this record needs approval.",
	}
	if __AUTHED__ {
		for _, msg := range append(append([]string{}, challenges...), plains...) {
			err := classifyAPIErrorOnly(fmt.Errorf("%s", msg))
			if ExitCode(err) != 4 {
				t.Fatalf("authed %q exit %d, want 4 (%v)", msg, ExitCode(err), err)
			}
			if strings.Contains(err.Error(), "without credentials") {
				t.Fatalf("authed 403 must not say credentials are absent: %v", err)
			}
			if !strings.Contains(err.Error(), "lack access") {
				t.Fatalf("authed 403 hint = %v", err)
			}
		}
		unauthorized := classifyAPIErrorOnly(fmt.Errorf("%s", "GET /items returned HTTP 401: unauthorized"))
		if ExitCode(unauthorized) != 4 {
			t.Fatalf("authed 401 exit %d, want 4 (%v)", ExitCode(unauthorized), unauthorized)
		}
		return
	}
	for _, msg := range challenges {
		err := classifyAPIErrorOnly(fmt.Errorf("%s", msg))
		if ExitCode(err) != 7 {
			t.Fatalf("challenge %q exit %d, want 7 (%v)", msg, ExitCode(err), err)
		}
		if !strings.Contains(err.Error(), hint) {
			t.Fatalf("challenge hint = %v", err)
		}
	}
	for _, msg := range plains {
		err := classifyAPIErrorOnly(fmt.Errorf("%s", msg))
		if ExitCode(err) != 5 {
			t.Fatalf("plain %q exit %d, want 5 (%v)", msg, ExitCode(err), err)
		}
		if !strings.Contains(err.Error(), hint) {
			t.Fatalf("plain hint = %v", err)
		}
	}
	unauthorized := classifyAPIErrorOnly(fmt.Errorf("%s", "GET /items returned HTTP 401: unauthorized"))
	if ExitCode(unauthorized) != 5 {
		t.Fatalf("no-auth 401 exit %d, want 5 (%v)", ExitCode(unauthorized), unauthorized)
	}
	if strings.Contains(unauthorized.Error(), "check your API credentials") {
		t.Fatalf("no-auth 401 must not ask for credentials: %v", unauthorized)
	}
	htmlErr := classifyAPIErrorOnly(fmt.Errorf("%s", "GET /x: expected JSON, API returned HTML instead of JSON: HTML document (40 bytes): 403 Forbidden"))
	if ExitCode(htmlErr) != 5 {
		t.Fatalf("no-auth HTML exit %d, want 5 (%v)", ExitCode(htmlErr), htmlErr)
	}
}

func TestClientForbiddenRoundTrip(t *testing.T) {
	challengeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><head><title>Error</title></head><body>you have been blocked</body></html>"))
	}))
	t.Cleanup(challengeSrv.Close)

	challengeErr := forbiddenGet(t, challengeSrv)
	challengeText := strings.ToLower(challengeErr.Error())
	if !strings.Contains(challengeErr.Error(), "HTTP 403") || !strings.Contains(challengeText, "cf-mitigated") || !strings.Contains(challengeText, "you have been blocked") {
		t.Fatalf("challenge response dropped block markers: %v", challengeErr)
	}
	challengeCode := 7
	if __AUTHED__ {
		challengeCode = 4
	}
	if got := ExitCode(classifyAPIErrorOnly(challengeErr)); got != challengeCode {
		t.Fatalf("challenge round trip exit %d, want %d (%v)", got, challengeCode, classifyAPIErrorOnly(challengeErr))
	}

	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("{\"error\":\"forbidden\"}"))
	}))
	t.Cleanup(plainSrv.Close)

	plainErr := forbiddenGet(t, plainSrv)
	plainText := strings.ToLower(plainErr.Error())
	if !strings.Contains(plainErr.Error(), "HTTP 403") || !strings.Contains(plainText, "forbidden") {
		t.Fatalf("plain 403 = %v", plainErr)
	}
	if strings.Contains(plainText, "cf-mitigated") || strings.Contains(plainText, "you have been blocked") || strings.Contains(plainText, "just a moment") {
		t.Fatalf("plain 403 gained block markers: %v", plainErr)
	}
	plainCode := 5
	if __AUTHED__ {
		plainCode = 4
	}
	if got := ExitCode(classifyAPIErrorOnly(plainErr)); got != plainCode {
		t.Fatalf("plain round trip exit %d, want %d (%v)", got, plainCode, classifyAPIErrorOnly(plainErr))
	}
}

func forbiddenGet(t *testing.T, srv *httptest.Server) error {
	t.Helper()
	c := clientpkg.New(&cfgpkg.Config{BaseURL: srv.URL}, time.Second, 0)
	c.HTTPClient = srv.Client()
	c.NoCache = true
	_, err := c.Get(context.Background(), "/items", nil)
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	return err
}
`

const forbiddenCLIUtilTest = `package cliutil

import (
	"net/http"
	"strings"
	"testing"
)

func TestTransportBlockMarkers(t *testing.T) {
	for _, text := range []string{
		"cf-mitigated: challenge",
		"Just a moment...",
		"Attention Required! | Cloudflare",
		"you have been blocked",
	} {
		if !LooksLikeTransportBlock(text) {
			t.Errorf("LooksLikeTransportBlock(%q) = false", text)
		}
	}
	for _, text := range []string{
		"{\"error\":\"forbidden\"}",
		"permission denied for this resource",
		"HTTP 403",
		"Attention required: this record needs approval.",
	} {
		if LooksLikeTransportBlock(text) {
			t.Errorf("LooksLikeTransportBlock(%q) = true", text)
		}
	}

	header := make(http.Header)
	header.Set("cf-mitigated", "challenge")
	raw := []byte("<!DOCTYPE html><html><head><title>Error</title></head><body>you have been blocked</body></html>")
	annotated := AnnotateTransportBlock("HTML error page (80 bytes): Error", header, raw)
	if !strings.Contains(annotated, "cf-mitigated: challenge") || !strings.Contains(annotated, "you have been blocked") {
		t.Fatalf("AnnotateTransportBlock = %q", annotated)
	}
	plain := "{\"error\":\"forbidden\"}"
	if got := AnnotateTransportBlock(plain, nil, []byte(plain)); got != plain {
		t.Fatalf("plain AnnotateTransportBlock = %q", got)
	}
}
`

const forbiddenMCPTest = `package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestMCPForbiddenClassification(t *testing.T) {
	challenge := mcpForbiddenText(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><head><title>Just a moment...</title></head><body>you have been blocked</body></html>"))
	})
	plain := mcpForbiddenText(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("{\"error\":\"forbidden\"}"))
	})
	if __AUTHED__ {
		for _, text := range []string{challenge, plain} {
			if !strings.HasPrefix(text, "permission denied: ") {
				t.Fatalf("authed MCP 403 = %q", text)
			}
			if strings.Contains(text, "without credentials") {
				t.Fatalf("authed MCP 403 must not say credentials are absent: %s", text)
			}
		}
		return
	}
	if !strings.HasPrefix(challenge, "rate limited: ") {
		t.Fatalf("challenge MCP 403 = %q", challenge)
	}
	if !strings.Contains(challenge, "this API is configured without credentials") || !strings.Contains(challenge, "rate limit, geography, bot protection, or endpoint policy") {
		t.Fatalf("challenge MCP hint = %q", challenge)
	}
	if strings.Contains(challenge, "check auth status") {
		t.Fatalf("no-auth challenge must not send the operator to auth status: %s", challenge)
	}
	if !strings.HasPrefix(plain, "api error: ") {
		t.Fatalf("plain MCP 403 = %q", plain)
	}
	if strings.HasPrefix(plain, "permission denied:") || strings.HasPrefix(plain, "rate limited:") {
		t.Fatalf("plain MCP 403 = %q", plain)
	}
	if !strings.Contains(plain, "this API is configured without credentials") {
		t.Fatalf("plain MCP hint = %q", plain)
	}
}

func mcpForbiddenText(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("__BASE_ENV__", srv.URL)

	call := makeAPIHandler("GET", "/items", true, false, nil, mcpPageConfig{}, nil, nil)
	result, err := call(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if result == nil || !result.IsError || len(result.Content) == 0 {
		t.Fatalf("result = %#v", result)
	}
	text, ok := result.Content[0].(mcplib.TextContent)
	if !ok {
		t.Fatalf("content type %T", result.Content[0])
	}
	return text.Text
}
`

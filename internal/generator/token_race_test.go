package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestEmitsConcurrentTokenRaceTest(t *testing.T) {
	t.Parallel()

	cc := ccRemintSpec()
	require.True(t, New(cc, t.TempDir()).emitsConcurrentTokenRaceTest())

	refresh := minimalSpec("oauth2-refresh-race")
	refresh.Auth = spec.AuthConfig{
		Type:     spec.AuthTypeOAuth2Refresh,
		Header:   "Authorization",
		TokenURL: "https://auth.example.com/oauth/token",
	}
	require.True(t, New(refresh, t.TempDir()).emitsConcurrentTokenRaceTest())

	apiKey := minimalSpec("api-key-race")
	require.False(t, New(apiKey, t.TempDir()).emitsConcurrentTokenRaceTest())

	google := minimalSpec("google-race")
	google.Auth = spec.AuthConfig{
		Type:     "bearer_token",
		Subtype:  spec.AuthSubtypeGoogleServiceAccount,
		Header:   "Authorization",
		TokenURL: "https://oauth2.googleapis.com/token",
	}
	require.False(t, New(google, t.TempDir()).emitsConcurrentTokenRaceTest())
}

func TestConcurrentClientCredentialsMintIsRaceFree(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("generated CLI race tests run in the full generated-test CI lane")
	}

	apiSpec := ccRemintSpec()
	apiSpec.Name = "ccrace"
	apiSpec.Auth.EnvVars = []string{"CCRACE_API_KEY", "CCRACE_SECRET_KEY"}
	apiSpec.Config.Path = "~/.config/ccrace-pp-cli/config.toml"
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	body := readGeneratedFile(t, outputDir, "internal", "client", "token_race_test.go")
	require.Contains(t, body, "func TestConcurrentClientCredentialsMint")

	out, err := runGoCommandOutput(t, outputDir, "test", "-race", "-count=1", "-v", "./internal/client", "-run", "^TestConcurrentClientCredentialsMint$")
	require.NoError(t, err, out)
	require.Contains(t, out, "--- PASS: TestConcurrentClientCredentialsMint")
}

func TestConcurrentRefreshTokenMintIsRaceFree(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("generated CLI race tests run in the full generated-test CI lane")
	}

	apiSpec := minimalSpec("refreshrace")
	apiSpec.Auth = spec.AuthConfig{
		Type:     spec.AuthTypeOAuth2Refresh,
		Header:   "Authorization",
		TokenURL: "https://auth.example.com/oauth/token",
		EnvVars: []string{
			"REFRESHRACE_CLIENT_ID",
			"REFRESHRACE_CLIENT_SECRET",
			"REFRESHRACE_REFRESH_TOKEN",
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	body := readGeneratedFile(t, outputDir, "internal", "client", "token_race_test.go")
	require.Contains(t, body, "func TestConcurrentRefreshTokenMint")

	out, err := runGoCommandOutput(t, outputDir, "test", "-race", "-count=1", "-v", "./internal/client", "-run", "^TestConcurrentRefreshTokenMint$")
	require.NoError(t, err, out)
	require.Contains(t, out, "--- PASS: TestConcurrentRefreshTokenMint")
}

func TestMintedAuthCarrier(t *testing.T) {
	t.Parallel()

	refresh := func(in, header string) *clientTemplateData {
		return &clientTemplateData{APISpec: &spec.APISpec{Auth: spec.AuthConfig{
			Type:   spec.AuthTypeOAuth2Refresh,
			In:     in,
			Header: header,
		}}}
	}

	tests := []struct {
		name string
		data *clientTemplateData
		want authCarrier
	}{
		{
			name: "nil",
			want: authCarrier{Kind: authCarrierHeader, Name: "Authorization"},
		},
		{
			name: "header",
			data: refresh("", "Authorization"),
			want: authCarrier{Kind: authCarrierHeader, Name: "Authorization"},
		},
		{
			name: "custom header",
			data: refresh("header", "X-Token"),
			want: authCarrier{Kind: authCarrierHeader, Name: "X-Token"},
		},
		{
			name: "query",
			data: refresh("query", "access_token"),
			want: authCarrier{Kind: authCarrierQuery, Name: "access_token"},
		},
		{
			name: "query default name",
			data: refresh("query", ""),
			want: authCarrier{Kind: authCarrierQuery, Name: "api_key"},
		},
		{
			name: "cookie",
			data: refresh("cookie", "session_token"),
			want: authCarrier{Kind: authCarrierCookie, Name: "session_token"},
		},
		{
			name: "cookie default name",
			data: refresh("cookie", ""),
			want: authCarrier{Kind: authCarrierCookie, Name: "api_key"},
		},
		{
			name: "cookie in is case sensitive",
			data: refresh("Cookie", "session_token"),
			want: authCarrier{Kind: authCarrierHeader, Name: "session_token"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, mintedAuthCarrier(tt.data))
		})
	}

	tiered := refresh("cookie", "session_token")
	tiered.TierRouting.DefaultTier = "free"
	tiered.TierRouting.Tiers = map[string]spec.TierConfig{
		"free": {Auth: spec.AuthConfig{Type: "none"}},
	}
	require.Equal(t, authCarrier{Kind: authCarrierHeader, Name: "session_token"}, mintedAuthCarrier(tiered))
}

func TestTokenRaceCarrierMatchesClient(t *testing.T) {
	t.Parallel()

	ccCookie := ccRemintSpec()
	ccCookie.Name = "cccookie"
	ccCookie.Auth.In = "cookie"
	ccCookie.Auth.Header = "session_token"
	ccCookie.Auth.EnvVars = []string{"CCCOOKIE_API_KEY", "CCCOOKIE_SECRET_KEY"}
	ccCookie.Config.Path = "~/.config/cccookie-pp-cli/config.toml"

	tiered := oauth2RefreshRaceSpec("refreshtier", spec.AuthConfig{In: "cookie", Header: "session_token"})
	tiered.TierRouting = spec.TierRoutingConfig{
		DefaultTier: "free",
		Tiers: map[string]spec.TierConfig{
			"free": {Auth: spec.AuthConfig{Type: "none"}},
		},
	}

	tests := []struct {
		name      string
		spec      *spec.APISpec
		clientHas []string
		raceHas   []string
		raceLacks []string
	}{
		{
			name:      "header",
			spec:      oauth2RefreshRaceSpec("refreshheader", spec.AuthConfig{Header: "Authorization"}),
			clientHas: []string{`req.Header.Set("Authorization", authHeader)`},
			raceHas:   []string{`r.Header.Get("Authorization")`},
			raceLacks: []string{`r.Cookie(`, `r.URL.Query().Get(`},
		},
		{
			name:      "query",
			spec:      oauth2RefreshRaceSpec("refreshquery", spec.AuthConfig{In: "query", Header: "access_token"}),
			clientHas: []string{`q.Set("access_token", authHeader)`},
			raceHas:   []string{`r.URL.Query().Get("access_token")`},
			raceLacks: []string{`r.Cookie(`, `r.Header.Get(`},
		},
		{
			name:      "query default name",
			spec:      oauth2RefreshRaceSpec("refreshquerykey", spec.AuthConfig{In: "query"}),
			clientHas: []string{`q.Set("api_key", authHeader)`},
			raceHas:   []string{`r.URL.Query().Get("api_key")`},
			raceLacks: []string{`r.Cookie(`, `r.Header.Get(`},
		},
		{
			name:      "cookie",
			spec:      oauth2RefreshRaceSpec("refreshcookieemit", spec.AuthConfig{In: "cookie", Header: "session_token"}),
			clientHas: []string{`req.AddCookie(&http.Cookie{Name: "session_token", Value: authHeader})`},
			raceHas:   []string{`r.Cookie("session_token")`},
			raceLacks: []string{`r.Header.Get(`, `r.URL.Query().Get(`},
		},
		{
			name:      "cookie default name",
			spec:      oauth2RefreshRaceSpec("refreshcookiekey", spec.AuthConfig{In: "cookie"}),
			clientHas: []string{`req.AddCookie(&http.Cookie{Name: "api_key", Value: authHeader})`},
			raceHas:   []string{`r.Cookie("api_key")`},
			raceLacks: []string{`r.Header.Get(`, `r.URL.Query().Get(`},
		},
		{
			name:      "tier routing keeps cookie credentials on a header",
			spec:      tiered,
			clientHas: []string{`In: "header", Name: "session_token"`, `req.Header.Set(authInfo.Name, authHeader)`},
			raceHas:   []string{`r.Header.Get("session_token")`},
			raceLacks: []string{`r.Cookie(`},
		},
		{
			name:      "client credentials cookie",
			spec:      ccCookie,
			clientHas: []string{`req.AddCookie(&http.Cookie{Name: "session_token", Value: authHeader})`},
			raceHas:   []string{"func TestConcurrentClientCredentialsMint", `r.Cookie("session_token")`},
			raceLacks: []string{`r.Header.Get(`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			outputDir := filepath.Join(t.TempDir(), naming.CLI(tt.spec.Name))
			require.NoError(t, New(tt.spec, outputDir).Generate())
			clientBody := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
			raceBody := readGeneratedFile(t, outputDir, "internal", "client", "token_race_test.go")
			for _, snippet := range tt.clientHas {
				require.Contains(t, clientBody, snippet)
			}
			for _, snippet := range tt.raceHas {
				require.Contains(t, raceBody, snippet)
			}
			for _, snippet := range tt.raceLacks {
				require.NotContains(t, raceBody, snippet)
			}
			requireGeneratedCompiles(t, outputDir)
		})
	}
}

func TestGeneratedConcurrentMintCarriers(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("generated CLI race tests run in the full generated-test CI lane")
	}

	ccCookie := ccRemintSpec()
	ccCookie.Name = "cccookierace"
	ccCookie.Auth.In = "cookie"
	ccCookie.Auth.Header = "session_token"
	ccCookie.Auth.EnvVars = []string{"CCCOOKIERACE_API_KEY", "CCCOOKIERACE_SECRET_KEY"}
	ccCookie.Config.Path = "~/.config/cccookierace-pp-cli/config.toml"

	tests := []struct {
		name     string
		spec     *spec.APISpec
		testName string
	}{
		{
			name:     "refresh cookie",
			spec:     oauth2RefreshRaceSpec("refreshcookie", spec.AuthConfig{In: "cookie", Header: "session_token"}),
			testName: "TestConcurrentRefreshTokenMint",
		},
		{
			name:     "refresh cookie default name",
			spec:     oauth2RefreshRaceSpec("refreshcookiekeyrace", spec.AuthConfig{In: "cookie"}),
			testName: "TestConcurrentRefreshTokenMint",
		},
		{
			name:     "refresh query",
			spec:     oauth2RefreshRaceSpec("refreshqueryrace", spec.AuthConfig{In: "query", Header: "access_token"}),
			testName: "TestConcurrentRefreshTokenMint",
		},
		{
			name:     "client credentials cookie",
			spec:     ccCookie,
			testName: "TestConcurrentClientCredentialsMint",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			outputDir := filepath.Join(t.TempDir(), naming.CLI(tt.spec.Name))
			require.NoError(t, New(tt.spec, outputDir).Generate())
			requireGeneratedCompiles(t, outputDir)
			out, err := runGoCommandOutput(t, outputDir, "test", "-race", "-count=1", "-v", "./internal/client", "-run", "^"+tt.testName+"$")
			require.NoError(t, err, out)
			require.Contains(t, out, "--- PASS: "+tt.testName)
		})
	}
}

func oauth2RefreshRaceSpec(name string, auth spec.AuthConfig) *spec.APISpec {
	apiSpec := minimalSpec(name)
	auth.Type = spec.AuthTypeOAuth2Refresh
	auth.TokenURL = "https://auth.example.com/oauth/token"
	prefix := naming.EnvPrefix(name)
	auth.EnvVars = []string{
		prefix + "_CLIENT_ID",
		prefix + "_CLIENT_SECRET",
		prefix + "_REFRESH_TOKEN",
	}
	apiSpec.Auth = auth
	return apiSpec
}

// TestGeneratedMintTestsLeaveRelocatedCredentialsAlone runs the printed OAuth
// mint tests with the CLI's own relocation variables pointed at a seeded
// store. generate's quality gate runs these tests with the caller's
// environment, so a store they can reach is that person's live credentials.
func TestGeneratedMintTestsLeaveRelocatedCredentialsAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI race tests run in the full generated-test CI lane")
	}
	t.Parallel()

	for _, fixture := range []string{"golden-api-oauth2-cc.yaml", "golden-api-oauth2-authcode.yaml"} {
		apiSpec, outputDir := generateGoldenFixture(t, fixture)
		for _, relocation := range []string{"_HOME", "_DATA_DIR"} {
			output, rewritten, err := runGeneratedMintTestsAgainstDecoy(t, apiSpec, outputDir, relocation)
			require.NoError(t, err, "%s %s: printed mint tests failed:\n%s", fixture, relocation, output)
			require.False(t, rewritten, "%s %s: printed mint tests rewrote the relocated credentials store:\n%s", fixture, relocation, output)
		}
	}
}

// TestGeneratedMintTestsDecoyCatchesHandRolledIsolation proves the decoy run
// above can fail: with the hand-rolled HOME redirect put back, the printed
// mint test rewrites the relocated store.
func TestGeneratedMintTestsDecoyCatchesHandRolledIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI race tests run in the full generated-test CI lane")
	}
	t.Parallel()

	apiSpec, outputDir := generateGoldenFixture(t, "golden-api-oauth2-cc.yaml")
	racePath := filepath.Join(outputDir, "internal", "client", "token_race_test.go")
	src := readGeneratedFile(t, outputDir, "internal", "client", "token_race_test.go")
	handRolled := strings.Replace(src,
		"\trestore, err := cliutil.SetHomeOverride(\"\")\n\tif err != nil {\n\t\tt.Fatalf(\"reset home override: %v\", err)\n\t}\n\tt.Cleanup(restore)\n\thome := testenv.Isolate(t, cliutil.ConfigDir, cliutil.DataDir, cliutil.StateDir, cliutil.CacheDir)\n",
		"\thome := t.TempDir()\n\tt.Setenv(\"HOME\", home)\n\tt.Setenv(\"USERPROFILE\", home)\n\tt.Setenv(\"XDG_CACHE_HOME\", filepath.Join(home, \"cache\"))\n\tt.Setenv(\"XDG_CONFIG_HOME\", filepath.Join(home, \"config\"))\n\tt.Setenv(\"XDG_DATA_HOME\", filepath.Join(home, \"data\"))\n\tt.Setenv(\"XDG_STATE_HOME\", filepath.Join(home, \"state\"))\n",
		1)
	require.NotEqual(t, src, handRolled, "expected to put the hand-rolled HOME redirect back")
	handRolled = dropGeneratedImportLines(handRolled, "/internal/cliutil")
	require.NoError(t, os.WriteFile(racePath, []byte(handRolled), 0o644))

	output, rewritten, err := runGeneratedMintTestsAgainstDecoy(t, apiSpec, outputDir, "_HOME")
	require.NoError(t, err, "the hand-rolled mint test should still pass:\n%s", output)
	require.True(t, rewritten, "the hand-rolled redirect must let the printed mint test rewrite the relocated store:\n%s", output)
}

func generateGoldenFixture(t *testing.T, fixture string) (*spec.APISpec, string) {
	t.Helper()

	apiSpec, err := openapi.ParseFile(filepath.Join("..", "..", "testdata", "golden", "fixtures", fixture))
	require.NoError(t, err)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	return apiSpec, outputDir
}

// runGeneratedMintTestsAgainstDecoy seeds <store>/data/credentials.toml, points
// <PREFIX><relocation> at it with every other relocation variable empty, runs
// the printed client tests, and reports whether the store changed.
func runGeneratedMintTestsAgainstDecoy(t *testing.T, apiSpec *spec.APISpec, outputDir, relocation string) ([]byte, bool, error) {
	t.Helper()

	envPrefix := naming.EnvPrefix(apiSpec.Name)
	store := t.TempDir()
	credentialsPath := filepath.Join(store, "data", "credentials.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(credentialsPath), 0o700))
	require.NoError(t, os.WriteFile(credentialsPath, []byte(generatedConfigPermsDecoy), 0o600))
	target := store
	if relocation == "_DATA_DIR" {
		target = filepath.Dir(credentialsPath)
	}
	operatorHome := t.TempDir()

	cmd := exec.Command("go", "test", "-mod=mod", "./internal/client", "-run", "^(TestConcurrent|TestLooseConfig)", "-count=1")
	cmd.Dir = outputDir
	var output []byte
	err := withGoBuildCache(outputDir, func(cacheDir string) error {
		cmd.Env = append(os.Environ(), "HOME="+operatorHome, "USERPROFILE="+operatorHome, "GOCACHE="+cacheDir)
		for _, suffix := range []string{"_HOME", "_CONFIG", "_CONFIG_DIR", "_DATA_DIR", "_STATE_DIR", "_CACHE_DIR"} {
			cmd.Env = append(cmd.Env, envPrefix+suffix+"=")
		}
		cmd.Env = append(cmd.Env, envPrefix+relocation+"="+target)
		for _, name := range []string{"GOPATH", "GOMODCACHE"} {
			if value := goEnvValue(t, name); value != "" {
				cmd.Env = append(cmd.Env, name+"="+value)
			}
		}
		var runErr error
		output, runErr = cmd.CombinedOutput()
		return runErr
	})
	after, readErr := os.ReadFile(credentialsPath)
	require.NoError(t, readErr)
	return output, string(after) != generatedConfigPermsDecoy, err
}

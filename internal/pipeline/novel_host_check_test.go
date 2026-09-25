package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnverifiedNovelHostAbsentFromResearchFails(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import "fmt"

func manifestURL() string {
	return "https://playback.indazn.com/v5/live.mpd"
}

func newPlayCmd() {
	_ = "Use: \"play\""
	fmt.Println(manifestURL())
}

// Use: "play"
`,
	}, []NovelFeature{{
		Name:        "Scheduled Headless Stream DVR",
		Command:     "play",
		Description: "Capture https://playback.api.indazn.com/manifest",
	}})
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "spec.yaml"), []byte(`
openapi: 3.0.0
info: {title: DAZN, version: "1"}
servers:
  - url: https://rail-router.discovery.indazn.com
paths: {}
`), 0o644))

	got := checkReimplementation(cliDir, researchDir)
	require.NotEmpty(t, got.UnverifiedHosts)
	hosts := map[string]ReimplementationFinding{}
	for _, finding := range got.UnverifiedHosts {
		hosts[finding.Host] = finding
	}
	play, ok := hosts["playback.indazn.com"]
	require.True(t, ok, "command literal host must fail: %#v", got.UnverifiedHosts)
	assert.Equal(t, "play.go", play.File)
	assert.NotZero(t, play.Line)
	assert.Contains(t, play.Reason, "not in research artifacts")

	payload, ok := hosts["playback.api.indazn.com"]
	require.True(t, ok, "payload host must fail: %#v", got.UnverifiedHosts)
	assert.Equal(t, "research.json", payload.File)
	assert.Equal(t, "play", payload.Command)
	assert.NotZero(t, payload.Line)

	report := &DogfoodReport{ReimplementationCheck: got}
	assert.Equal(t, DogfoodVerdictFail, deriveDogfoodVerdict(report, true))
}

func TestNovelHostInOpenAPIServersPasses(t *testing.T) {
	cliDir, researchDir := seedNovelHostFixture(t, "https://rail-router.discovery.indazn.com/rails")
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "spec.yaml"), []byte(`
openapi: 3.0.0
info: {title: DAZN, version: "1"}
servers:
  - url: https://rail-router.discovery.indazn.com
paths: {}
`), 0o644))

	got := checkReimplementation(cliDir, researchDir)
	assert.Empty(t, got.UnverifiedHosts)
}

func TestNovelHostInSniffSamplePasses(t *testing.T) {
	cliDir, researchDir := seedNovelHostFixture(t, "https://event.discovery.indazn.com/v1/events")
	sampleDir := filepath.Join(researchDir, "dazn-samples")
	require.NoError(t, os.MkdirAll(sampleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sampleDir, "get__events.json"), []byte(`{
  "raw_url": "https://event.discovery.indazn.com/v1/events"
}`), 0o644))

	got := checkReimplementation(cliDir, researchDir)
	assert.Empty(t, got.UnverifiedHosts)
}

func TestNovelHostInDocumentedURLListPasses(t *testing.T) {
	cliDir, researchDir := seedNovelHostFixture(t, "https://ott-authz-bff-prod.ar.indazn.com/auth")
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "observed-urls.txt"), []byte("https://ott-authz-bff-prod.ar.indazn.com/auth\n"), 0o644))

	got := checkReimplementation(cliDir, researchDir)
	assert.Empty(t, got.UnverifiedHosts)
}

func TestNovelHostDNSNXDOMAINFailsWhenObserved(t *testing.T) {
	findings := unverifiedNovelHosts(NovelHostInput{
		FeatureOverride: []NovelFeature{{
			Command:     "play",
			Description: "Play https://rail-router.discovery.indazn.com/live",
		}},
		SpecPaths: nil,
		Resolve: func(context.Context, string) error {
			return errNovelHostNXDOMAIN
		},
		ResearchDir: writeSpecRoot(t, "https://rail-router.discovery.indazn.com"),
	})
	require.Len(t, findings, 1)
	assert.Equal(t, "rail-router.discovery.indazn.com", findings[0].Host)
	assert.Contains(t, findings[0].Reason, "does not resolve")
}

func seedNovelHostFixture(t *testing.T, manifestURL string) (string, string) {
	t.Helper()
	return seedReimplementationFixture(t, map[string]string{
		"play.go": "package cli\n\nconst manifestURL = \"" + manifestURL + "\"\n\nfunc newPlayCmd() { _ = \"Use: \\\"play\\\"\" }\n\n// Use: \"play\"\n",
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})
}

func TestOpenAPIExampleURLIsNotObserved(t *testing.T) {
	cliDir, researchDir := seedNovelHostFixture(t, "https://cdn.example.test/asset")
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "spec.yaml"), []byte(`
openapi: 3.0.0
info:
  title: DAZN
  version: "1"
  description: See https://docs.example.test/guide
servers:
  - url: https://rail-router.discovery.indazn.com
paths:
  /rails:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              example:
                href: https://cdn.example.test/asset
`), 0o644))

	got := checkReimplementation(cliDir, researchDir)
	require.NotEmpty(t, got.UnverifiedHosts)
	assert.Equal(t, "cdn.example.test", got.UnverifiedHosts[0].Host)
}

func TestSampleResponseBodyURLIsNotObserved(t *testing.T) {
	cliDir, researchDir := seedNovelHostFixture(t, "https://cdn.example.test/asset")
	sampleDir := filepath.Join(researchDir, "dazn-samples")
	require.NoError(t, os.MkdirAll(sampleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sampleDir, "get__events.json"), []byte(`{
  "raw_url": "https://event.discovery.indazn.com/v1/events",
  "response_body": {"download": "https://cdn.example.test/asset"}
}`), 0o644))

	got := checkReimplementation(cliDir, researchDir)
	require.NotEmpty(t, got.UnverifiedHosts)
	assert.Equal(t, "cdn.example.test", got.UnverifiedHosts[0].Host)
}

func TestNovelHelperHostIsChecked(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

func newPlayCmd() {
	_ = manifestURL()
}

// Use: "play"
`,
		"manifest.go": `package cli

func manifestURL() string {
	return playbackHost
}

func docs() string {
	return "https://unrelated.example.test/help"
}
`,
		"hosts.go": `package cli

const playbackHost = "https://playback.indazn.com/v5/live.mpd"
`,
		"helpers.go": `package cli

const docs = "https://unrelated.example.test/help"
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	require.NotEmpty(t, got.UnverifiedHosts)
	var playback ReimplementationFinding
	for _, finding := range got.UnverifiedHosts {
		assert.NotEqual(t, "unrelated.example.test", finding.Host)
		if finding.Host == "playback.indazn.com" {
			playback = finding
		}
	}
	assert.Equal(t, "hosts.go", playback.File)
	assert.NotZero(t, playback.Line)
	assert.Equal(t, "play", playback.Command)
	assert.Contains(t, playback.Reason, "not in research artifacts")
}

func TestImportedNovelHelperHostIsChecked(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import "example.com/demo/internal/playback"

func newPlayCmd() {
	_ = playback.Manifest()
}

// Use: "play"
`,
		"helpers.go": `package cli

const docs = "https://unrelated.example.test/help"
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})
	playbackDir := filepath.Join(cliDir, "internal", "playback")
	require.NoError(t, os.MkdirAll(playbackDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(playbackDir, "manifest.go"), []byte(`package playback

func Manifest() string {
	return "https://edge.indazn.com/v5/live.mpd"
}

func Docs() string {
	return "https://unrelated.example.test/help"
}
`), 0o644))
	otherDir := filepath.Join(cliDir, "internal", "catalog")
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "urls.go"), []byte(`package catalog

const docs = "https://catalog.example.test/help"
`), 0o644))

	got := checkReimplementation(cliDir, researchDir)
	require.Len(t, got.UnverifiedHosts, 1)
	assert.Equal(t, "edge.indazn.com", got.UnverifiedHosts[0].Host)
	assert.Equal(t, "internal/playback/manifest.go", got.UnverifiedHosts[0].File)
	assert.NotZero(t, got.UnverifiedHosts[0].Line)
	assert.Equal(t, "play", got.UnverifiedHosts[0].Command)
}

func TestFeatureNamedHelperFileIsChecked(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

func newPlayCmd() {}

// Use: "play"
`,
		"play_urls.go": `package cli

const manifestURL = "https://playback.indazn.com/v5/live.mpd"
`,
		"helpers.go": `package cli

const docs = "https://unrelated.example.test/help"
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	require.Len(t, got.UnverifiedHosts, 1)
	assert.Equal(t, "playback.indazn.com", got.UnverifiedHosts[0].Host)
	assert.Equal(t, "play_urls.go", got.UnverifiedHosts[0].File)
	assert.Equal(t, "play", got.UnverifiedHosts[0].Command)
}

func TestNovelMethodHelperHostIsChecked(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

func newPlayCmd() {
	var client streamClient
	_ = client.Manifest()
}

// Use: "play"
`,
		"stream.go": `package cli

type streamClient struct{}

func (streamClient) Manifest() string {
	return "https://edge.indazn.com/v5/live.mpd"
}

func (streamClient) Docs() string {
	return "https://unrelated.example.test/help"
}
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	require.Len(t, got.UnverifiedHosts, 1)
	assert.Equal(t, "edge.indazn.com", got.UnverifiedHosts[0].Host)
	assert.Equal(t, "stream.go", got.UnverifiedHosts[0].File)
}

func TestUnrelatedGoFileDoesNotFailNovelHostGate(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go":    "package cli\n\nfunc newPlayCmd() {}\n\n// Use: \"play\"\n",
		"helpers.go": "package cli\n\nconst docs = \"https://unrelated.example.test/help\"\n",
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	assert.Empty(t, got.UnverifiedHosts)
}

func TestDottedProseIdentifierIsNotAHost(t *testing.T) {
	findings := unverifiedNovelHosts(NovelHostInput{
		FeatureOverride: []NovelFeature{{
			Command:     "play",
			Description: "Reads the settings.production flag and does not call a URL",
		}},
	})
	assert.Empty(t, findings)
}

func TestRepeatedUnverifiedHostUsesFeatureLine(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"root.go": "package cli\n",
	}, []NovelFeature{
		{Name: "One", Command: "one", Description: "Calls https://ghost.example.test/a"},
		{Name: "Two", Command: "two", Description: "Calls https://ghost.example.test/b"},
	})
	raw, err := os.ReadFile(filepath.Join(researchDir, "research.json"))
	require.NoError(t, err)

	got := checkReimplementation(cliDir, researchDir)
	require.Len(t, got.UnverifiedHosts, 2)
	byCommand := map[string]ReimplementationFinding{}
	for _, finding := range got.UnverifiedHosts {
		byCommand[finding.Command] = finding
	}
	one := byCommand["one"]
	two := byCommand["two"]
	assert.NotEqual(t, one.Line, two.Line)
	assert.Equal(t, "research.json", one.File)
	assert.Contains(t, lineText(raw, one.Line), "ghost.example.test/a")
	assert.Contains(t, lineText(raw, two.Line), "ghost.example.test/b")
}

func TestDogfoodHostGateUsesResolvedSpecOnce(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"), []byte("package cli\nfunc newRootCmd() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "play.go"), []byte("package cli\n\nconst manifestURL = \"https://example.com/v1\"\n\n// Use: \"play\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "client", "client.go"), []byte("package client\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "store", "store.go"), []byte("package store\n"), 0o644))

	researchDir := filepath.Join(dir, "pipeline")
	require.NoError(t, os.MkdirAll(researchDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "research.json"), []byte(`{"novel_features":[{"name":"Play","command":"play"}]}`), 0o644))
	specPath := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte("openapi: 3.0.0\ninfo: {title: Example, version: \"1\"}\nservers:\n  - url: https://example.com\npaths: {}\n"), 0o644))

	report, err := RunDogfood(dir, specPath, WithResearchDir(researchDir))
	require.NoError(t, err)
	assert.Empty(t, report.ReimplementationCheck.UnverifiedHosts)
}

func lineText(raw []byte, line int) string {
	if line <= 0 {
		return ""
	}
	lines := strings.Split(string(raw), "\n")
	if line > len(lines) {
		return ""
	}
	return lines[line-1]
}

func writeSpecRoot(t *testing.T, serverURL string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.yaml"), []byte("servers:\n  - url: "+serverURL+"\n"), 0o644))
	return dir
}

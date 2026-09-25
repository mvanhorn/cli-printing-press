package pipeline

import (
	"context"
	"os"
	"path/filepath"
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
		"play.go": "package cli\n\nconst manifestURL = \"" + manifestURL + "\"\n\nfunc newPlayCmd() { _ = \"Use: \\\"play\\\"\" }\n",
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})
}

func writeSpecRoot(t *testing.T, serverURL string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.yaml"), []byte("servers:\n  - url: "+serverURL+"\n"), 0o644))
	return dir
}

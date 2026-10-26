package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
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

func TestRFCReservedHostsInNovelHelpAndResearchPass(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import "github.com/spf13/cobra"

func newPlayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "play --webhook https://hooks.example.com/events",
		Short: "Send a test notification to hooks.example.net",
		Long:  "Use https://preview.example.org/events or https://service.example/test.",
		Example: ` + "`" + `  play --webhook https://hooks.example.com/events
  play --email reader@example.com
  play --webhook https://preview.test/events
  play --webhook https://preview.invalid/events
  play --webhook https://preview.localhost/events` + "`" + `,
	}
}
`,
	}, []NovelFeature{{
		Name:         "Play",
		Command:      "play",
		Description:  "Try https://hooks.example.com/events before configuring a real webhook.",
		Example:      "Send a receipt to reader@example.org through https://preview.test/events.",
		WhyItMatters: "The invalid URL https://preview.invalid/events is safe documentation.",
	}})

	got := checkReimplementation(cliDir, researchDir)
	assert.Empty(t, got.UnverifiedHosts)
}

func TestRFCReservedRuntimeHostStillFailsNovelHostGate(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import "github.com/spf13/cobra"

const webhookURL = "https://hooks.example.com/events"

func newPlayCmd() *cobra.Command {
	return &cobra.Command{Use: "play"}
}
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	require.Len(t, got.UnverifiedHosts, 1)
	assert.Equal(t, "hooks.example.com", got.UnverifiedHosts[0].Host)
	assert.Equal(t, "play.go", got.UnverifiedHosts[0].File)
}

func TestRFCReservedHostInAssignedNovelHelpPasses(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import "github.com/spf13/cobra"

func newPlayCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "play"}
	cmd.Example = "play --webhook https://hooks.example.com/events"
	return cmd
}
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	assert.Empty(t, got.UnverifiedHosts)
}

func TestRFCReservedHostInLiteralNovelHelpPasses(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import "github.com/spf13/cobra"

func newPlayCmd() *cobra.Command {
	return &cobra.Command{
		Use: "play",
		Long: "Send events to https://hooks.example.com/events.",
	}
}
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	assert.Empty(t, got.UnverifiedHosts)
}

func TestRFCReservedHostInNovelHelpCallFails(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import (
	"net/http"

	"github.com/spf13/cobra"
)

func newPlayCmd() *cobra.Command {
	return &cobra.Command{
		Use: "play",
		Long: func() string {
			_, _ = http.Get("https://hooks.example.com/events")
			return "Send events."
		}(),
	}
}
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	require.Len(t, got.UnverifiedHosts, 1)
	assert.Equal(t, "hooks.example.com", got.UnverifiedHosts[0].Host)
}

func TestRFCReservedHostInNovelFlagUsagePasses(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import "github.com/spf13/cobra"

func newPlayCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "play"}
	cmd.PersistentFlags().String("webhook", "", "URL, e.g. https://hooks.example.com/events")
	return cmd
}
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	assert.Empty(t, got.UnverifiedHosts)
}

func TestRFCReservedHostInNovelFlagDefaultStillFails(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import "github.com/spf13/cobra"

func newPlayCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "play"}
	cmd.Flags().String("webhook", "https://hooks.example.com/events", "Webhook URL")
	return cmd
}
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	require.Len(t, got.UnverifiedHosts, 1)
	assert.Equal(t, "hooks.example.com", got.UnverifiedHosts[0].Host)
}

func TestUnverifiedRealHostInAssignedNovelHelpFails(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import "github.com/spf13/cobra"

func newPlayCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "play"}
	cmd.Example = "play --webhook https://hooks.unverified.example-api.com/events"
	return cmd
}
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	require.Len(t, got.UnverifiedHosts, 1)
	assert.Equal(t, "hooks.unverified.example-api.com", got.UnverifiedHosts[0].Host)
}

func TestUnverifiedRealHostInNovelHelpFails(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import "github.com/spf13/cobra"

func newPlayCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "play",
		Example: "play --webhook https://hooks.unverified.example-api.com/events",
	}
}
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	require.Len(t, got.UnverifiedHosts, 1)
	assert.Equal(t, "hooks.unverified.example-api.com", got.UnverifiedHosts[0].Host)
	assert.Equal(t, "play.go", got.UnverifiedHosts[0].File)
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

func TestImportedClientMethodIgnoresUnrelatedManifest(t *testing.T) {
	const playbackSrc = `package playback

type Client struct{}

func New() *Client { return &Client{} }

func (Client) Manifest() string {
	return "https://edge.indazn.com/v5/live.mpd"
}

func (Client) Docs() string {
	return "https://docs.example.test/help"
}
`
	const unrelatedSrc = `package cli

type Client struct{}

func (Client) Manifest() string {
	return "https://unrelated.example.test/help"
}
`
	tests := []struct {
		name string
		play string
	}{
		{
			name: "receiver type",
			play: `package cli

import "example.com/demo/internal/playback"

func newPlayCmd() {
	var client playback.Client
	_ = client.Manifest()
}

// Use: "play"
`,
		},
		{
			name: "constructor",
			play: `package cli

import "example.com/demo/internal/playback"

func newPlayCmd() {
	client := playback.New()
	_ = client.Manifest()
}

// Use: "play"
`,
		},
		{
			name: "factory method",
			play: `package cli

import "example.com/demo/internal/playback"

type rootFlags struct{}

func (f *rootFlags) newClient() (*playback.Client, error) {
	return nil, nil
}

func newPlayCmd(flags *rootFlags) {
	client, err := flags.newClient()
	_ = err
	_ = client.Manifest()
}

// Use: "play"
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
				"play.go":    tt.play,
				"catalog.go": unrelatedSrc,
			}, []NovelFeature{{
				Name:    "Play",
				Command: "play",
			}})
			playbackDir := filepath.Join(cliDir, "internal", "playback")
			require.NoError(t, os.MkdirAll(playbackDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(playbackDir, "client.go"), []byte(playbackSrc), 0o644))

			got := checkReimplementation(cliDir, researchDir)
			require.Len(t, got.UnverifiedHosts, 1, "hosts: %#v", got.UnverifiedHosts)
			assert.Equal(t, "edge.indazn.com", got.UnverifiedHosts[0].Host)
			assert.Equal(t, "internal/playback/client.go", got.UnverifiedHosts[0].File)
			assert.NotZero(t, got.UnverifiedHosts[0].Line)
			assert.Equal(t, "play", got.UnverifiedHosts[0].Command)
		})
	}
}

func TestFieldReceiverHelperHostIsChecked(t *testing.T) {
	const streamSrc = `package cli

type streamClient struct{}

func (streamClient) Manifest() string {
	return "https://edge.indazn.com/v5/live.mpd"
}

func (streamClient) Docs() string {
	return "https://unrelated.example.test/help"
}
`
	const playbackSrc = `package playback

type Client struct{}

type Options struct {
	Client *Client
}

func New() *Client { return &Client{} }

func (Client) Manifest() string {
	return "https://edge.indazn.com/v5/live.mpd"
}

func (Client) Docs() string {
	return "https://docs.example.test/help"
}
`
	const unrelatedSrc = `package cli

type Client struct{}

func (Client) Manifest() string {
	return "https://unrelated.example.test/help"
}
`
	tests := []struct {
		name     string
		files    map[string]string
		playback bool
		wantFile string
	}{
		{
			name: "param field",
			files: map[string]string{
				"play.go": `package cli

type playFlags struct {
	client *streamClient
}

func newPlayCmd(flags *playFlags) {
	_ = flags.client.Manifest()
}

// Use: "play"
`,
				"stream.go":  streamSrc,
				"helpers.go": "package cli\n\nconst docs = \"https://unrelated.example.test/help\"\n",
			},
			wantFile: "stream.go",
		},
		{
			name: "nested field",
			files: map[string]string{
				"play.go": `package cli

type playFlags struct {
	opts playOpts
}

type playOpts struct {
	client *streamClient
}

func newPlayCmd(flags *playFlags) {
	_ = flags.opts.client.Manifest()
}

// Use: "play"
`,
				"stream.go": streamSrc,
			},
			wantFile: "stream.go",
		},
		{
			name: "anonymous struct field",
			files: map[string]string{
				"play.go": `package cli

func newPlayCmd() {
	var flags struct {
		client *streamClient
	}
	_ = flags.client.Manifest()
}

// Use: "play"
`,
				"stream.go": streamSrc,
			},
			wantFile: "stream.go",
		},
		{
			name: "imported client field",
			files: map[string]string{
				"play.go": `package cli

import "example.com/demo/internal/playback"

type playFlags struct {
	client *playback.Client
}

func newPlayCmd(flags *playFlags) {
	_ = flags.client.Manifest()
}

// Use: "play"
`,
				"catalog.go": unrelatedSrc,
			},
			playback: true,
			wantFile: "internal/playback/client.go",
		},
		{
			name: "imported struct field",
			files: map[string]string{
				"play.go": `package cli

import "example.com/demo/internal/playback"

func newPlayCmd() {
	var opts playback.Options
	_ = opts.Client.Manifest()
}

// Use: "play"
`,
				"catalog.go": unrelatedSrc,
			},
			playback: true,
			wantFile: "internal/playback/client.go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cliDir, researchDir := seedReimplementationFixture(t, tt.files, []NovelFeature{{
				Name:    "Play",
				Command: "play",
			}})
			if tt.playback {
				playbackDir := filepath.Join(cliDir, "internal", "playback")
				require.NoError(t, os.MkdirAll(playbackDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(playbackDir, "client.go"), []byte(playbackSrc), 0o644))
			}

			got := checkReimplementation(cliDir, researchDir)
			require.Len(t, got.UnverifiedHosts, 1, "hosts: %#v", got.UnverifiedHosts)
			assert.Equal(t, "edge.indazn.com", got.UnverifiedHosts[0].Host)
			assert.Equal(t, tt.wantFile, got.UnverifiedHosts[0].File)
			assert.NotZero(t, got.UnverifiedHosts[0].Line)
			assert.Equal(t, "play", got.UnverifiedHosts[0].Command)
		})
	}
}

func TestLaterShadowDoesNotRedirectEarlierCall(t *testing.T) {
	const playbackSrc = `package playback

type Client struct{}

func New() *Client { return &Client{} }

func (Client) Manifest() string {
	return "https://edge.indazn.com/v5/live.mpd"
}
`
	const localSrc = `package cli

type localClient struct{}

func (localClient) Manifest() string {
	return "https://unrelated.example.test/help"
}
`
	tests := []struct {
		name      string
		body      string
		wantHosts map[string]string
	}{
		{
			name: "earlier call",
			body: `
	client := playback.New()
	_ = client.Manifest()
	if true {
		var client localClient
		_ = client
	}
`,
			wantHosts: map[string]string{
				"edge.indazn.com": "internal/playback/client.go",
			},
		},
		{
			name: "call after inner scope",
			body: `
	client := playback.New()
	if true {
		client := localClient{}
		_ = client.Manifest()
	}
	_ = client.Manifest()
`,
			wantHosts: map[string]string{
				"edge.indazn.com":        "internal/playback/client.go",
				"unrelated.example.test": "catalog.go",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			play := "package cli\n\nimport \"example.com/demo/internal/playback\"\n\nfunc newPlayCmd() {" + tt.body + "}\n\n// Use: \"play\"\n"
			cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
				"play.go":    play,
				"catalog.go": localSrc,
			}, []NovelFeature{{
				Name:    "Play",
				Command: "play",
			}})
			playbackDir := filepath.Join(cliDir, "internal", "playback")
			require.NoError(t, os.MkdirAll(playbackDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(playbackDir, "client.go"), []byte(playbackSrc), 0o644))

			got := checkReimplementation(cliDir, researchDir)
			require.Len(t, got.UnverifiedHosts, len(tt.wantHosts), "hosts: %#v", got.UnverifiedHosts)
			gotHosts := map[string]string{}
			for _, finding := range got.UnverifiedHosts {
				gotHosts[finding.Host] = finding.File
				assert.Equal(t, "play", finding.Command)
				assert.NotZero(t, finding.Line)
			}
			assert.Equal(t, tt.wantHosts, gotHosts)
		})
	}
}

func TestShortDeclMultiResultKeepsClientType(t *testing.T) {
	const playbackSrc = `package playback

type Client struct{}

func New() *Client { return &Client{} }

func (Client) Manifest() string {
	return "https://edge.indazn.com/v5/live.mpd"
}
`
	const unrelatedSrc = `package cli

type localClient struct{}

func (localClient) Manifest() string {
	return "https://unrelated.example.test/help"
}
`
	tests := []struct {
		name      string
		body      string
		wantHosts map[string]string
	}{
		{
			name: "second result",
			body: `
func acquire() (error, *playback.Client) {
	return nil, nil
}

func newPlayCmd() {
	err, client := acquire()
	_ = err
	_ = client.Manifest()
}
`,
			wantHosts: map[string]string{
				"edge.indazn.com": "internal/playback/client.go",
			},
		},
		{
			name: "method second result",
			body: `
type rootFlags struct{}

func (f *rootFlags) acquire() (error, *playback.Client) {
	return nil, nil
}

func newPlayCmd(flags *rootFlags) {
	err, client := flags.acquire()
	_ = err
	_ = client.Manifest()
}
`,
			wantHosts: map[string]string{
				"edge.indazn.com": "internal/playback/client.go",
			},
		},
		{
			name: "redeclared second result",
			body: `
func newPlayCmd() {
	client := playback.New()
	err, client := missing()
	_ = err
	_ = client.Manifest()
}
`,
			wantHosts: map[string]string{
				"edge.indazn.com": "internal/playback/client.go",
			},
		},
		{
			name: "reassigned second result",
			body: `
func newPlayCmd() {
	client := playback.New()
	var err error
	err, client = missing()
	_ = err
	_ = client.Manifest()
}
`,
			wantHosts: map[string]string{
				"edge.indazn.com": "internal/playback/client.go",
			},
		},
		{
			name: "inner second result",
			body: `
func acquireLocal() (error, *localClient) {
	return nil, nil
}

func newPlayCmd() {
	client := playback.New()
	if true {
		err, client := acquireLocal()
		_ = err
		_ = client.Manifest()
	}
}
`,
			wantHosts: map[string]string{
				"unrelated.example.test": "catalog.go",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			play := "package cli\n\nimport \"example.com/demo/internal/playback\"\n" + tt.body + "\n// Use: \"play\"\n"
			cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
				"play.go":    play,
				"catalog.go": unrelatedSrc,
			}, []NovelFeature{{
				Name:    "Play",
				Command: "play",
			}})
			playbackDir := filepath.Join(cliDir, "internal", "playback")
			require.NoError(t, os.MkdirAll(playbackDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(playbackDir, "client.go"), []byte(playbackSrc), 0o644))

			got := checkReimplementation(cliDir, researchDir)
			require.Len(t, got.UnverifiedHosts, len(tt.wantHosts), "hosts: %#v", got.UnverifiedHosts)
			gotHosts := map[string]string{}
			for _, finding := range got.UnverifiedHosts {
				gotHosts[finding.Host] = finding.File
				assert.Equal(t, "play", finding.Command)
				assert.NotZero(t, finding.Line)
			}
			assert.Equal(t, tt.wantHosts, gotHosts)
		})
	}
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

func TestNovelHostLiteralClassification(t *testing.T) {
	tests := []struct {
		literal string
		want    []string
	}{
		{literal: "events.jsonl"},
		{literal: "exchange-snapshots.db"},
		{literal: "audit.log"},
		{literal: "notes.md"},
		{literal: "8.0.1.0"},
		{literal: "2.0"},
		{literal: "tables.create"},
		{literal: "VM.Audit"},
		{literal: "articles.get"},
		{literal: "api.vendor.example", want: []string{"api.vendor.example"}},
		{literal: "evil.example.com", want: []string{"evil.example.com"}},
		{literal: "www.googleapis.com", want: []string{"www.googleapis.com"}},
		{literal: "cdn.example.test", want: []string{"cdn.example.test"}},
		{literal: "https://evil.example.com/v1", want: []string{"evil.example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.literal, func(t *testing.T) {
			src := "package cli\n\nfunc newPlayCmd() {\n\t_ = " + strconv.Quote(tt.literal) + "\n}\n"
			var got []string
			for _, decl := range hostsInGoSource("play.go", src, "play") {
				got = append(got, decl.host)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRepeatedUnverifiedHostUsesFeatureLine(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"root.go": "package cli\n",
	}, []NovelFeature{
		{Name: "One", Command: "one", Description: "Calls https://ghost.unverified.example-api.com/a"},
		{Name: "Two", Command: "two", Description: "Calls https://ghost.unverified.example-api.com/b"},
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
	assert.Contains(t, lineText(raw, one.Line), "ghost.unverified.example-api.com/a")
	assert.Contains(t, lineText(raw, two.Line), "ghost.unverified.example-api.com/b")
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

func TestRFCReservedHostInFormattedNovelHelpPasses(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"play.go": `package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPlayCmd(path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "play",
		Long: fmt.Sprintf("Send events to https://hooks.example.com/%s.", path),
	}
	cmd.Flags().String("target", "", fmt.Sprintf("Target URL, e.g. %s", "https://hooks.example.com/events"))
	return cmd
}
`,
	}, []NovelFeature{{
		Name:    "Play",
		Command: "play",
	}})

	got := checkReimplementation(cliDir, researchDir)
	assert.Empty(t, got.UnverifiedHosts)
}

func TestNovelHostApexAndVocabularyNamespace(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"deals.go": `package cli

import "strings"

const schemaOrgPrefix = "https://schema.org/"

func newDealsCmd() {
	var host, itemCondition string
	if host == "subito.it" || strings.HasSuffix(host, ".subito.it") {
		_ = host
	}
	if host == "evilsubito.it" || host == "subito.it.evil.com" || host == "shop.subito.it" {
		_ = host
	}
	_ = strings.TrimPrefix(itemCondition, schemaOrgPrefix)
	_ = strings.TrimPrefix(itemCondition, "https://schema.org/")
	_ = strings.TrimPrefix(itemCondition, "https://www.schema.org/")
	_ = strings.TrimPrefix(itemCondition, "https://notschema.org/")
	_ = strings.TrimPrefix(itemCondition, "https://evil.schema.org/")
	_ = strings.TrimPrefix(itemCondition, "https://schema.org.evil.com/")
	_ = "https://ghost.unverified.example-api.com/v1/items"
	// Use: "deals"
}
`,
	}, []NovelFeature{{
		Name:    "Deals",
		Command: "deals",
	}})
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "observed-urls.txt"), []byte("https://www.subito.it/annunci\nhttps://hades.subito.it/v1/search\n"), 0o644))

	got := checkReimplementation(cliDir, researchDir)
	hosts := map[string]bool{}
	for _, finding := range got.UnverifiedHosts {
		hosts[finding.Host] = true
	}
	assert.False(t, hosts["subito.it"], "apex of an observed subdomain must pass: %#v", got.UnverifiedHosts)
	assert.False(t, hosts["schema.org"], "schema.org trim prefix must pass: %#v", got.UnverifiedHosts)
	assert.False(t, hosts["www.schema.org"], "www.schema.org trim prefix must pass: %#v", got.UnverifiedHosts)
	for _, host := range []string{
		"evilsubito.it",
		"subito.it.evil.com",
		"shop.subito.it",
		"notschema.org",
		"evil.schema.org",
		"schema.org.evil.com",
		"ghost.unverified.example-api.com",
	} {
		assert.True(t, hosts[host], "host %s must still fail: %#v", host, got.UnverifiedHosts)
	}
}

func TestNovelHostApexDoesNotMatchLookalikeObservedHosts(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"deals.go": `package cli

func newDealsCmd() {
	host := "subito.it"
	_ = host
	// Use: "deals"
}
`,
	}, []NovelFeature{{
		Name:    "Deals",
		Command: "deals",
	}})
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "observed-urls.txt"), []byte("https://www.evilsubito.it/\nhttps://subito.it.evil.com/path\n"), 0o644))

	got := checkReimplementation(cliDir, researchDir)
	require.NotEmpty(t, got.UnverifiedHosts)
	assert.Equal(t, "subito.it", got.UnverifiedHosts[0].Host)
}

func TestVocabularyNamespaceAffixExemptionScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		files   map[string]string
		present []string
		absent  []string
	}{
		{
			name: "local const trim",
			files: map[string]string{
				"deals.go": `package cli

import "strings"

func newDealsCmd() {
	const prefix = "https://schema.org/"
	var item string
	_ = strings.TrimPrefix(item, prefix)
	// Use: "deals"
}
`,
			},
			absent: []string{"schema.org"},
		},
		{
			name: "local var trim",
			files: map[string]string{
				"deals.go": `package cli

import "strings"

func newDealsCmd() {
	var prefix = "https://schema.org/"
	var item string
	_ = strings.TrimPrefix(item, prefix)
	// Use: "deals"
}
`,
			},
			absent: []string{"schema.org"},
		},
		{
			name: "local const fetch",
			files: map[string]string{
				"deals.go": `package cli

import "net/http"

func newDealsCmd() {
	const prefix = "https://schema.org/"
	_, _ = http.Get(prefix + "version/latest/schemaorg-current-https.jsonld")
	// Use: "deals"
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "local var fetch",
			files: map[string]string{
				"deals.go": `package cli

import "net/http"

func newDealsCmd() {
	var prefix = "https://schema.org/"
	_, _ = http.Get(prefix + "version/latest/schemaorg-current-https.jsonld")
	// Use: "deals"
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "cross file fetch",
			files: map[string]string{
				"deals.go": `package cli

import "strings"

const schemaOrgPrefix = "https://schema.org/"

func newDealsCmd() {
	var item string
	_ = strings.TrimPrefix(item, schemaOrgPrefix)
	fetchSchema()
	// Use: "deals"
}
`,
				"schema_fetch.go": `package cli

import "net/http"

func fetchSchema() {
	_, _ = http.Get(schemaOrgPrefix + "version/latest/schemaorg-current-https.jsonld")
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "const in other file fetched from command",
			files: map[string]string{
				"deals.go": `package cli

import "net/http"

func newDealsCmd() {
	_, _ = http.Get(schemaOrgPrefix + "version/latest/schemaorg-current-https.jsonld")
	// Use: "deals"
}
`,
				"schema_const.go": `package cli

import "strings"

const schemaOrgPrefix = "https://schema.org/"

func trimSchema(item string) string {
	return strings.TrimPrefix(item, schemaOrgPrefix)
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "cross file trim only",
			files: map[string]string{
				"deals.go": `package cli

const schemaOrgPrefix = "https://schema.org/"

func newDealsCmd() {
	trimSchema("")
	// Use: "deals"
}
`,
				"schema_trim.go": `package cli

import "strings"

func trimSchema(item string) string {
	return strings.TrimPrefix(item, schemaOrgPrefix)
}
`,
			},
			absent: []string{"schema.org"},
		},
		{
			name: "alias const then fetch",
			files: map[string]string{
				"deals.go": `package cli

const schemaOrgPrefix = "https://schema.org/"

func newDealsCmd() {
	trimSchema("")
	// Use: "deals"
}
`,
				"schema_alias.go": `package cli

import "net/http"

const alias = schemaOrgPrefix
const alias2 = alias

func trimSchema(item string) string { return item }

func fetchSchema() {
	_, _ = http.Get(alias2 + "version/latest/schemaorg-current-https.jsonld")
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "alias const then trim",
			files: map[string]string{
				"deals.go": `package cli

import "strings"

const schemaOrgPrefix = "https://schema.org/"
const alias = schemaOrgPrefix
const alias2 = alias

func newDealsCmd() {
	var item string
	_ = strings.TrimPrefix(item, alias2)
	// Use: "deals"
}
`,
			},
			absent: []string{"schema.org"},
		},
		{
			name: "grouped const copy fetch",
			files: map[string]string{
				"deals.go": `package cli

import "net/http"

const (
	schemaOrgPrefix = "https://schema.org/"
	alias
)

func newDealsCmd() {
	_, _ = http.Get(alias + "version/latest/schemaorg-current-https.jsonld")
	// Use: "deals"
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "grouped const copy trim",
			files: map[string]string{
				"deals.go": `package cli

import "strings"

const (
	schemaOrgPrefix = "https://schema.org/"
	alias
)

func newDealsCmd() {
	var item string
	_ = strings.TrimPrefix(item, alias)
	// Use: "deals"
}
`,
			},
			absent: []string{"schema.org"},
		},
		{
			name: "grouped const original fetch and copy trim",
			files: map[string]string{
				"deals.go": `package cli

import (
	"net/http"
	"strings"
)

const (
	schemaOrgPrefix = "https://schema.org/"
	alias
)

func newDealsCmd() {
	var item string
	_ = strings.TrimPrefix(item, alias)
	_, _ = http.Get(schemaOrgPrefix + "version/latest/schemaorg-current-https.jsonld")
	// Use: "deals"
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "grouped const inherited alias then fetch",
			files: map[string]string{
				"deals.go": `package cli

import (
	"net/http"
	"strings"
)

const (
	base = "https://schema.org/"
	trim = base
	fetch
)

func newDealsCmd() {
	var item string
	_ = strings.TrimPrefix(item, trim)
	_, _ = http.Get(fetch)
	// Use: "deals"
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "grouped const inherited alias trim only",
			files: map[string]string{
				"deals.go": `package cli

import "strings"

const (
	base = "https://schema.org/"
	trim = base
	also
)

func newDealsCmd() {
	var item string
	_ = strings.TrimPrefix(item, trim)
	_ = strings.TrimPrefix(item, also)
	// Use: "deals"
}
`,
			},
			absent: []string{"schema.org"},
		},
		{
			name: "local grouped const inherited alias then fetch",
			files: map[string]string{
				"deals.go": `package cli

import (
	"net/http"
	"strings"
)

func newDealsCmd() {
	const (
		base = "https://schema.org/"
		trim = base
		fetch
	)
	var item string
	_ = strings.TrimPrefix(item, trim)
	_, _ = http.Get(fetch)
	// Use: "deals"
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "local grouped const inherited alias trim only",
			files: map[string]string{
				"deals.go": `package cli

import "strings"

func newDealsCmd() {
	const (
		base = "https://schema.org/"
		trim = base
		also
	)
	var item string
	_ = strings.TrimPrefix(item, trim)
	_ = strings.TrimPrefix(item, also)
	// Use: "deals"
}
`,
			},
			absent: []string{"schema.org"},
		},
		{
			name: "shadow fetch before local declaration",
			files: map[string]string{
				"deals.go": `package cli

import (
	"net/http"
	"strings"
)

const schemaOrgPrefix = "https://schema.org/"

func trimSchema(item string) string {
	return strings.TrimPrefix(item, schemaOrgPrefix)
}

func newDealsCmd() {
	_, _ = http.Get(schemaOrgPrefix + "version/latest/schemaorg-current-https.jsonld")
	schemaOrgPrefix := "safe"
	_ = strings.TrimPrefix("", schemaOrgPrefix)
	// Use: "deals"
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "param shadows package const",
			files: map[string]string{
				"deals.go": `package cli

import (
	"net/http"
	"strings"
)

const schemaOrgPrefix = "https://schema.org/"

func newDealsCmd() {
	var item string
	_ = strings.TrimPrefix(item, schemaOrgPrefix)
	sneak("https://ghost.unverified.example-api.com/x")
	// Use: "deals"
}

func sneak(schemaOrgPrefix string) {
	_, _ = http.Get(schemaOrgPrefix)
}
`,
			},
			present: []string{"ghost.unverified.example-api.com"},
			absent:  []string{"schema.org"},
		},
		{
			name: "pass const into a fetch helper",
			files: map[string]string{
				"deals.go": `package cli

import "strings"

const schemaOrgPrefix = "https://schema.org/"

func newDealsCmd() {
	var item string
	_ = strings.TrimPrefix(item, schemaOrgPrefix)
	fetchSchema(schemaOrgPrefix + "version/latest/schemaorg-current-https.jsonld")
	// Use: "deals"
}
`,
				"schema_fetch.go": `package cli

import "net/http"

func fetchSchema(url string) {
	_, _ = http.Get(url)
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "local alias then fetch",
			files: map[string]string{
				"deals.go": `package cli

import (
	"net/http"
	"strings"
)

const schemaOrgPrefix = "https://schema.org/"

func newDealsCmd() {
	var item string
	_ = strings.TrimPrefix(item, schemaOrgPrefix)
	u := schemaOrgPrefix + "version/latest/schemaorg-current-https.jsonld"
	_, _ = http.Get(u)
	// Use: "deals"
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "local const copied to var then trimmed",
			files: map[string]string{
				"deals.go": `package cli

import "strings"

func newDealsCmd() {
	const prefix = "https://schema.org/"
	var alias = prefix
	var item string
	_ = strings.TrimPrefix(item, alias)
	// Use: "deals"
}
`,
			},
			absent: []string{"schema.org"},
		},
		{
			name: "local const copied to var then fetched",
			files: map[string]string{
				"deals.go": `package cli

import "net/http"

func newDealsCmd() {
	const prefix = "https://schema.org/"
	var alias = prefix
	_, _ = http.Get(alias + "version/latest/schemaorg-current-https.jsonld")
	// Use: "deals"
}
`,
			},
			present: []string{"schema.org"},
		},
		{
			name: "local shadow fetch does not hide package literal",
			files: map[string]string{
				"deals.go": `package cli

import "net/http"

const schemaOrgPrefix = "https://schema.org/"

func newDealsCmd() {
	schemaOrgPrefix := "https://schema.org/version/"
	_, _ = http.Get(schemaOrgPrefix + "latest.jsonld")
	// Use: "deals"
}
`,
			},
			present: []string{"schema.org"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cliDir, researchDir := seedReimplementationFixture(t, tt.files, []NovelFeature{{
				Name:    "Deals",
				Command: "deals",
			}})
			got := checkReimplementation(cliDir, researchDir)
			hosts := map[string]bool{}
			for _, finding := range got.UnverifiedHosts {
				hosts[finding.Host] = true
			}
			for _, host := range tt.present {
				assert.True(t, hosts[host], "host %s must fail: %#v", host, got.UnverifiedHosts)
			}
			for _, host := range tt.absent {
				assert.False(t, hosts[host], "host %s must pass: %#v", host, got.UnverifiedHosts)
			}
		})
	}
}

func TestVocabularyNamespaceFetchStillFailsNovelHostGate(t *testing.T) {
	cliDir, researchDir := seedReimplementationFixture(t, map[string]string{
		"deals.go": `package cli

import "net/http"

const schemaOrgPrefix = "https://schema.org/"

func newDealsCmd() {
	_, _ = http.Get(schemaOrgPrefix + "version/latest/schemaorg-current-https.jsonld")
	// Use: "deals"
}
`,
	}, []NovelFeature{{
		Name:    "Deals",
		Command: "deals",
	}})

	got := checkReimplementation(cliDir, researchDir)
	require.NotEmpty(t, got.UnverifiedHosts)
	assert.Equal(t, "schema.org", got.UnverifiedHosts[0].Host)
}

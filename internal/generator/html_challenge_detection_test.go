package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedHTMLChallengeDetection(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "html-challenge-detection",
		Version: "0.1.0",
		BaseURL: "https://example.test",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/html-challenge-detection-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"pages": {
				Description: "HTML pages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/pages",
						Description:    "Fetch page metadata",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
						Response:       spec.ResponseDef{Type: "object"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	inlineTest := `package cli

import (
	"strings"
	"testing"
)

func TestHTMLChallengeDetection(t *testing.T) {
	tests := []struct {
		name          string
		raw           string
		wantChallenge bool
	}{
		{
			name: "normal Cloudflare page with passive beacon",
			raw:  ` + "`" + `<!doctype html><html><head><title>Catalog</title><script src="/cdn-cgi/challenge-platform/h/g/orchestrate/jsch/v1"></script></head><body>Products</body></html>` + "`" + `,
		},
		{
			name:          "Cloudflare active challenge page script with alternate title",
			raw:           ` + "`" + `<!doctype html><html><head><title>Security check</title><script src="/cdn-cgi/challenge-platform/h/g/orchestrate/chl_page/v1"></script></head><body></body></html>` + "`" + `,
			wantChallenge: true,
		},
		{
			name: "normal page mentioning Cloudflare labels",
			raw:  ` + "`" + `<html><head><title>Cloudflare reference</title></head><body>Headers such as cf-challenge and cf-mitigated are documented here.</body></html>` + "`" + `,
		},
		{name: "access denied documentation", raw: ` + "`" + `<html><head><title>Access Denied errors and how to fix them</title></head><body>Troubleshooting</body></html>` + "`" + `},
		{name: "attention required article", raw: ` + "`" + `<html><head><title>Attention Required: Renew Your Subscription</title></head><body>Billing</body></html>` + "`" + `},
		{name: "just a moment title", raw: ` + "`" + `<html><head><title>Just a moment...</title></head><body></body></html>` + "`" + `, wantChallenge: true},
		{name: "verify human title", raw: ` + "`" + `<html><head><title>Verify You Are Human</title></head><body></body></html>` + "`" + `, wantChallenge: true},
		{name: "Cloudflare verification element", raw: ` + "`" + `<html><body><div id="cf-browser-verification"></div></body></html>` + "`" + `, wantChallenge: true},
		{name: "Cloudflare challenge token", raw: ` + "`" + `<html><body><script>window._cf_chl_ctx = {}</script></body></html>` + "`" + `, wantChallenge: true},
		{name: "checking browser interstitial", raw: ` + "`" + `<html><body>Checking your browser before accessing example.test.</body></html>` + "`" + `, wantChallenge: true},
		{name: "DDoS protection interstitial", raw: ` + "`" + `<html><body>DDoS protection by Cloudflare</body></html>` + "`" + `, wantChallenge: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractHTMLResponse([]byte(tt.raw), htmlExtractionOptions{
				Mode:        "page",
				ContentType: "text/html",
				BaseURL:     "https://example.test",
			})
			if tt.wantChallenge {
				if err == nil || !strings.Contains(err.Error(), "browser challenge") {
					t.Fatalf("extractHTMLResponse() error = %v, want browser challenge error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractHTMLResponse() error = %v, want success", err)
			}
			if len(got) == 0 {
				t.Fatal("extractHTMLResponse() returned an empty result")
			}
		})
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "html_challenge_detection_test.go"), []byte(inlineTest), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestHTMLChallengeDetection$", "-count=1")
}

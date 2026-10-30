package generator

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A parameter named url hides the net/url import inside deliverWebhook, so
// this runs the emitted tests instead of only reading the template text.
func TestGeneratedDeliverWebhookRedactsRedirectLocation(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("webhook-loc")
	outputDir := filepath.Join(t.TempDir(), "webhook-loc-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	deliverGo := readGeneratedFile(t, outputDir, "internal", "cli", "deliver.go")
	require.Contains(t, deliverGo, "func deliverWebhook(webhookURL string,")
	require.Contains(t, deliverGo, "http.DefaultTransport.RoundTrip")
	require.Contains(t, deliverGo, "req.SetBasicAuth(user.Username(), password)")
	require.Contains(t, deliverGo, "url.Parse(loc)")
	require.Contains(t, deliverGo, `u.Scheme != "" && u.Host != ""`)
	require.NotContains(t, deliverGo, "http.ErrUseLastResponse")
	require.NotContains(t, deliverGo, `Location: %q); redirects are not followed", resp.Status, resp.Header.Get("Location")`)

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "-count=1", "-run", "TestDeliverWebhook", "./internal/cli")
}

package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestBinaryResponseHonorsDeliverAndDryRun(t *testing.T) {
	payload := []byte("%PDF-1.7\nbinary fixture")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/reports/export" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(payload)
		case r.URL.Path == "/audio/speech" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write(payload)
		case r.URL.Path == "/certificate" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(payload)
		case r.URL.Path == "/items" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"1"}]`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("binary-deliver")
	apiSpec.BaseURL = server.URL
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"reports": {
			Description: "Reports",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      http.MethodGet,
					Path:        "/reports",
					Description: "List reports",
				},
			},
			SubResources: map[string]spec.Resource{
				"export": {
					Description: "Report export",
					Endpoints: map[string]spec.Endpoint{
						"report-year": {
							Method:         http.MethodGet,
							Path:           "/reports/export",
							Description:    "Download the annual report as a binary file",
							ResponseFormat: spec.ResponseFormatBinary,
						},
					},
				},
			},
		},
		"audio": {
			Description: "Audio",
			Endpoints: map[string]spec.Endpoint{
				"create-speech": {
					Method:         http.MethodPost,
					Path:           "/audio/speech",
					Description:    "Create speech audio",
					ResponseFormat: spec.ResponseFormatBinary,
					Body: []spec.Param{
						{Name: "input", Type: "string", Required: true, Description: "Text"},
					},
				},
				"voices": {
					Method:      http.MethodGet,
					Path:        "/audio/voices",
					Description: "List voices",
				},
			},
		},
		"certificate": {
			Description: "Download a certificate",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:         http.MethodGet,
					Path:           "/certificate",
					Description:    "Download a certificate",
					ResponseFormat: spec.ResponseFormatBinary,
				},
			},
		},
		"items": {
			Description: "JSON items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      http.MethodGet,
					Path:        "/items",
					Description: "List items",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "reports_export_report-year.go")
	require.Contains(t, endpointSrc, `handleBinaryResponseDelivery(cmd, flags, data)`)
	require.Contains(t, endpointSrc, `!deliverSinkIsNonStdout(flags)`)
	require.Contains(t, endpointSrc, `binary response cannot be rendered as structured output`)

	audioSrc := readGeneratedFile(t, outputDir, "internal", "cli", "audio_create-speech.go")
	require.Contains(t, audioSrc, `handleBinaryResponseDelivery(cmd, flags, data)`)
	require.Contains(t, audioSrc, `!deliverSinkIsNonStdout(flags)`)

	promotedSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_certificate.go")
	require.Contains(t, promotedSrc, `handleBinaryResponseDelivery(cmd, flags, data)`)

	deliverSrc := readGeneratedFile(t, outputDir, "internal", "cli", "deliver.go")
	require.Contains(t, deliverSrc, `unwrapBinaryDeliverBody(body)`)
	require.Contains(t, deliverSrc, `client.UnwrapBinaryResponse`)

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, `func UnwrapBinaryResponse(`)

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	baseEnv := append(os.Environ(),
		"HOME="+t.TempDir(),
		"MYAPI_TOKEN=test-token",
		strings.ToUpper(strings.ReplaceAll(apiSpec.Name, "-", "_"))+"_BASE_URL="+server.URL,
	)

	t.Run("endpoint-json-refuses-without-deliver", func(t *testing.T) {
		out, err := runGeneratedCLI(t, binaryPath, baseEnv, "reports", "export", "report-year", "--json")
		require.Error(t, err, out)
		require.Contains(t, out, "binary response cannot be rendered as structured output")
		require.Contains(t, out, "--deliver file:")
		requireExitCode(t, err, 2)
	})

	t.Run("endpoint-json-deliver-writes-raw-bytes", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "report.pdf")
		out, err := runGeneratedCLI(t, binaryPath, baseEnv, "reports", "export", "report-year", "--json", "--deliver", "file:"+dest)
		require.NoError(t, err, out)
		require.NotContains(t, out, "binary response cannot be rendered as structured output")
		got, readErr := os.ReadFile(dest)
		require.NoError(t, readErr)
		require.Equal(t, payload, got)
		require.NotContains(t, string(got), `_pp_binary`)
		var receipt map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &receipt), out)
		require.Equal(t, true, receipt["delivered"])
		require.Equal(t, "file", receipt["sink"])
		require.Equal(t, dest, receipt["target"])
		require.Equal(t, float64(len(payload)), receipt["bytes"])
	})

	t.Run("endpoint-deliver-without-json-writes-raw-bytes", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "report.pdf")
		out, err := runGeneratedCLI(t, binaryPath, baseEnv, "reports", "export", "report-year", "--deliver", "file:"+dest)
		require.NoError(t, err, out)
		got, readErr := os.ReadFile(dest)
		require.NoError(t, readErr)
		require.Equal(t, payload, got)
	})

	t.Run("endpoint-dry-run-json", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "should-not-exist.pdf")
		out, err := runGeneratedCLI(t, binaryPath, baseEnv, "reports", "export", "report-year", "--dry-run", "--json", "--deliver", "file:"+dest)
		require.NoError(t, err, out)
		require.NotContains(t, out, "binary response cannot be rendered as structured output")
		require.Contains(t, out, `"dry_run"`)
		_, statErr := os.Stat(dest)
		require.Error(t, statErr)
	})

	t.Run("post-json-deliver-writes-raw-bytes", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "speech.mp3")
		out, err := runGeneratedCLI(t, binaryPath, baseEnv, "audio", "create-speech", "--input", "hello", "--json", "--deliver", "file:"+dest)
		require.NoError(t, err, out)
		require.NotContains(t, out, "binary response cannot be rendered as structured output")
		got, readErr := os.ReadFile(dest)
		require.NoError(t, readErr)
		require.Equal(t, payload, got)
	})

	t.Run("post-dry-run-json", func(t *testing.T) {
		out, err := runGeneratedCLI(t, binaryPath, baseEnv, "audio", "create-speech", "--input", "hello", "--dry-run", "--json")
		require.NoError(t, err, out)
		require.NotContains(t, out, "binary response cannot be rendered as structured output")
		require.Contains(t, out, `"dry_run"`)
	})

	t.Run("promoted-json-deliver-writes-raw-bytes", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "cert.pdf")
		out, err := runGeneratedCLI(t, binaryPath, baseEnv, "certificate", "--json", "--deliver", "file:"+dest)
		require.NoError(t, err, out)
		got, readErr := os.ReadFile(dest)
		require.NoError(t, readErr)
		require.Equal(t, payload, got)
		require.NotContains(t, string(got), `_pp_binary`)
	})

	t.Run("json-items-deliver-stays-json", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "items.json")
		out, err := runGeneratedCLI(t, binaryPath, baseEnv, "items", "--json", "--deliver", "file:"+dest)
		require.NoError(t, err, out)
		got, readErr := os.ReadFile(dest)
		require.NoError(t, readErr)
		require.Contains(t, string(got), `"id"`)
		require.NotEqual(t, payload, got)
	})
}

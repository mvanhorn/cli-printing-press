package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestGeneratedNonJSONPayloadClassification(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("html-payload-class")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Contains(t, helpersSrc, "var classifyHTMLPayload func(trimmed []byte) error")
	require.Contains(t, helpersSrc, "func htmlLooksLikeAuthFailure(trimmed []byte) bool")
	require.Contains(t, helpersSrc, "func classifyHTMLTransportError(err error) error")
	require.Contains(t, helpersSrc, "returned HTML instead of JSON")
	require.Contains(t, helpersSrc, "API returned HTML instead of JSON; the request may have reached a web page or the wrong endpoint")
	require.NotContains(t, helpersSrc, "if len(trimmed) > 0 && trimmed[0] == '<' {\n\t\treturn authErr(")

	inlineTest := `package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNonJSONPayloadErrorClassifiesBareHTMLAsAPIError(t *testing.T) {
	err := nonJSONPayloadError(json.RawMessage("<!doctype html><html><head><title>Console</title></head><body></body></html>"))
	requireCLIError(t, err, 5)
	msg := err.Error()
	if !strings.Contains(msg, "HTML instead of JSON") {
		t.Errorf("message must name the HTML/non-JSON payload, got: %s", msg)
	}
	if strings.Contains(msg, "not authenticated") || strings.Contains(msg, "Set your API key") {
		t.Errorf("bare HTML must not be reported as an auth failure, got: %s", msg)
	}
}

func TestNonJSONPayloadErrorKeepsAuthClassWhenHTMLHasEvidence(t *testing.T) {
	cases := []string{
		"<html><body>401 Unauthorized</body></html>",
		"<html><body>403 Forbidden</body></html>",
		"<html><title>Login</title><body>session expired</body></html>",
		"<html><body>not authenticated</body></html>",
		"<html><body>invalid api key</body></html>",
	}
	for _, body := range cases {
		err := nonJSONPayloadError(json.RawMessage(body))
		if !requireCLIError(t, err, 4) {
			continue
		}
		if !strings.Contains(err.Error(), "not authenticated or session expired") {
			t.Errorf("auth-evident HTML %q should keep the auth message, got: %s", body, err)
		}
	}
}

func TestNonJSONPayloadErrorHonorsCLISpecificClassifier(t *testing.T) {
	t.Cleanup(func() { classifyHTMLPayload = nil })
	classifyHTMLPayload = func(trimmed []byte) error {
		if strings.Contains(string(trimmed), "KNOWN-CONSOLE") {
			return apiErr(fmt.Errorf("the console answered with its own HTML page instead of JSON; check base_url"))
		}
		return nil
	}

	console := json.RawMessage("<html><title>KNOWN-CONSOLE</title><body>session expired</body></html>")
	err := nonJSONPayloadError(console)
	if !requireCLIError(t, err, 5) {
		return
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Errorf("CLI-specific recogniser must win over generic auth evidence, got: %s", err)
	}

	other := json.RawMessage("<html><body>401 Unauthorized</body></html>")
	err = nonJSONPayloadError(other)
	requireCLIError(t, err, 4)
}

func TestNonJSONPayloadErrorEmptyAndOpaqueBodiesAreAPIErrors(t *testing.T) {
	requireCLIError(t, nonJSONPayloadError(json.RawMessage("")), 5)
	requireCLIError(t, nonJSONPayloadError(json.RawMessage("not-json")), 5)
}

func TestClassifyAPIErrorOnlyClassifiesClientHTMLAsAPIError(t *testing.T) {
	err := classifyAPIErrorOnly(fmt.Errorf("GET https://example.test/info: expected JSON, API returned HTML instead of JSON: HTML document (120 bytes): Console"))
	if !requireCLIError(t, err, 5) {
		return
	}
	if strings.Contains(err.Error(), "not authenticated") || strings.Contains(err.Error(), "Set your API key") {
		t.Errorf("client HTML rejection must not be reported as an auth failure, got: %s", err)
	}
}

func TestClassifyAPIErrorOnlyKeepsAuthWhenHTMLErrorHasEvidence(t *testing.T) {
	err := classifyAPIErrorOnly(fmt.Errorf("GET https://example.test/me: expected JSON, API returned HTML instead of JSON: HTML document (80 bytes): 401 Unauthorized"))
	requireCLIError(t, err, 4)
}

func TestClassifyAPIErrorOnlyHonorsCLISpecificClassifier(t *testing.T) {
	t.Cleanup(func() { classifyHTMLPayload = nil })
	classifyHTMLPayload = func(trimmed []byte) error {
		if strings.Contains(string(trimmed), "Console") {
			return apiErr(fmt.Errorf("the console answered with its own HTML page instead of JSON; check base_url"))
		}
		return nil
	}
	err := classifyAPIErrorOnly(fmt.Errorf("GET https://example.test/info: expected JSON, API returned HTML instead of JSON: HTML document (120 bytes): Console"))
	if !requireCLIError(t, err, 5) {
		return
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Errorf("CLI-specific recogniser must win on the client HTML path, got: %s", err)
	}
}

func TestClassifyAPIErrorOnlyHTTP401StillWinsOverHTML(t *testing.T) {
	err := classifyAPIErrorOnly(fmt.Errorf("HTTP 401: expected JSON, API returned HTML instead of JSON: HTML document (40 bytes): Console"))
	requireCLIError(t, err, 4)
}

func TestClassifyAPIErrorOnlyIgnoresAuthWordsInURL(t *testing.T) {
	err := classifyAPIErrorOnly(fmt.Errorf("GET https://example.test/unauthorized: expected JSON, API returned HTML instead of JSON: HTML document (120 bytes): Console"))
	if !requireCLIError(t, err, 5) {
		return
	}
	if strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("auth markers in the request URL must not classify bare HTML as auth, got: %s", err)
	}
}

func requireCLIError(t *testing.T, err error, wantCode int) bool {
	t.Helper()
	var ce *cliError
	if !errors.As(err, &ce) {
		t.Errorf("want a *cliError, got %T (%v)", err, err)
		return false
	}
	if ce.code != wantCode {
		t.Errorf("exit code = %d, want %d (%v)", ce.code, wantCode, err)
		return false
	}
	return true
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "nonjson_payload_classification_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestNonJSONPayloadError|^TestClassifyAPIErrorOnly", "-count=1")
}

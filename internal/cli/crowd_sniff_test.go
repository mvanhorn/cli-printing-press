package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/crowdsniff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSource implements crowdSniffSource for testing.
type mockSource struct {
	result     crowdsniff.SourceResult
	err        error
	calledWith string // captures the apiName argument
}

func (m *mockSource) Discover(_ context.Context, apiName string) (crowdsniff.SourceResult, error) {
	m.calledWith = apiName
	return m.result, m.err
}

func endpointsResult(baseURL string, endpoints ...crowdsniff.DiscoveredEndpoint) crowdsniff.SourceResult {
	var candidates []string
	if baseURL != "" {
		candidates = []string{baseURL}
	}
	return crowdsniff.SourceResult{
		Endpoints:         endpoints,
		BaseURLCandidates: candidates,
	}
}

func TestCrowdSniffCmd_MissingAPIFlag(t *testing.T) {
	t.Parallel()
	cmd := newCrowdSniffCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag")
}

func TestCrowdSniffCmd_HelpOutput(t *testing.T) {
	t.Parallel()
	cmd := newCrowdSniffCmd()
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	assert.NoError(t, err)
}

func TestRunCrowdSniff_HappyPath(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "test-spec.yaml")
	var stdout bytes.Buffer

	src := &mockSource{result: endpointsResult("https://api.example.com",
		crowdsniff.DiscoveredEndpoint{Method: "GET", Path: "/v1/users", SourceTier: crowdsniff.TierOfficialSDK, SourceName: "sdk"},
		crowdsniff.DiscoveredEndpoint{Method: "POST", Path: "/v1/users", SourceTier: crowdsniff.TierOfficialSDK, SourceName: "sdk"},
	)}

	opts := crowdSniffOptions{
		sources: []crowdSniffSource{src},
		stdout:  &stdout,
		stderr:  &bytes.Buffer{},
	}

	err := runCrowdSniff(context.Background(), "example", "https://api.example.com", outputPath, false, opts)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "Spec written to")
	assert.Contains(t, stdout.String(), "2 endpoints")

	// Verify API name was passed through to source.
	assert.Equal(t, "example", src.calledWith)

	// Verify spec file was written with correct content.
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "base_url: https://api.example.com")
	assert.Contains(t, string(data), "name: example")
}

func TestRunCrowdSniff_JSONOutput(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "test-spec.yaml")
	var stdout bytes.Buffer

	opts := crowdSniffOptions{
		sources: []crowdSniffSource{
			&mockSource{result: endpointsResult("https://api.example.com",
				crowdsniff.DiscoveredEndpoint{Method: "GET", Path: "/users", SourceTier: crowdsniff.TierCodeSearch, SourceName: "gh"},
			)},
		},
		stdout: &stdout,
		stderr: &bytes.Buffer{},
	}

	err := runCrowdSniff(context.Background(), "test", "https://api.example.com", outputPath, true, opts)
	require.NoError(t, err)

	var jsonOut map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &jsonOut))
	assert.Equal(t, outputPath, jsonOut["spec_path"])
	assert.Equal(t, float64(1), jsonOut["endpoints"])
	assert.Equal(t, float64(1), jsonOut["resources"])
	tierBreakdown, ok := jsonOut["tier_breakdown"].(map[string]any)
	require.True(t, ok, "tier_breakdown should be a map")
	assert.Equal(t, float64(1), tierBreakdown[crowdsniff.TierCodeSearch])
}

func TestRunCrowdSniff_NoEndpointsDiscovered(t *testing.T) {
	t.Parallel()

	opts := crowdSniffOptions{
		sources: []crowdSniffSource{
			&mockSource{result: crowdsniff.SourceResult{}},
		},
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}

	err := runCrowdSniff(context.Background(), "obscure-api", "", "", false, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no endpoints discovered")
}

func TestRunCrowdSniff_NoBaseURL(t *testing.T) {
	t.Parallel()

	opts := crowdSniffOptions{
		sources: []crowdSniffSource{
			&mockSource{result: crowdsniff.SourceResult{
				Endpoints: []crowdsniff.DiscoveredEndpoint{
					{Method: "GET", Path: "/users", SourceTier: crowdsniff.TierCodeSearch, SourceName: "gh"},
				},
				// No BaseURLCandidates.
			}},
		},
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}

	err := runCrowdSniff(context.Background(), "test", "", "", false, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not determine base URL")
	assert.Contains(t, err.Error(), "--base-url")
}

func TestRunCrowdSniff_NonHTTPSBaseURL(t *testing.T) {
	t.Parallel()

	opts := crowdSniffOptions{
		sources: []crowdSniffSource{
			&mockSource{result: endpointsResult("http://api.example.com",
				crowdsniff.DiscoveredEndpoint{Method: "GET", Path: "/users", SourceTier: crowdsniff.TierCodeSearch, SourceName: "gh"},
			)},
		},
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}

	err := runCrowdSniff(context.Background(), "test", "", "", false, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base URL must use HTTPS")
}

func TestRunCrowdSniff_BaseURLFlagOverridesCandidate(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "test-spec.yaml")

	opts := crowdSniffOptions{
		sources: []crowdSniffSource{
			&mockSource{result: endpointsResult("https://wrong.example.com",
				crowdsniff.DiscoveredEndpoint{Method: "GET", Path: "/users", SourceTier: crowdsniff.TierCodeSearch, SourceName: "gh"},
			)},
		},
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}

	err := runCrowdSniff(context.Background(), "test", "https://correct.example.com", outputPath, false, opts)
	require.NoError(t, err)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "https://correct.example.com")
	assert.NotContains(t, string(data), "https://wrong.example.com")
}

func TestRunCrowdSniff_SourceErrorGracefulDegradation(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "test-spec.yaml")
	var stderr bytes.Buffer

	opts := crowdSniffOptions{
		sources: []crowdSniffSource{
			// First source fails.
			&mockSource{err: fmt.Errorf("npm registry down")},
			// Second source succeeds.
			&mockSource{result: endpointsResult("https://api.example.com",
				crowdsniff.DiscoveredEndpoint{Method: "GET", Path: "/users", SourceTier: crowdsniff.TierCodeSearch, SourceName: "gh"},
			)},
		},
		stdout: &bytes.Buffer{},
		stderr: &stderr,
	}

	err := runCrowdSniff(context.Background(), "test", "", outputPath, false, opts)
	require.NoError(t, err)

	// Warning logged for failed source.
	assert.Contains(t, stderr.String(), "warning")
	assert.Contains(t, stderr.String(), "npm registry down")

	// Spec still written from the successful source with correct content.
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "/users")
	assert.Contains(t, string(data), "https://api.example.com")
}

func TestRunCrowdSniff_AllSourcesFail(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	opts := crowdSniffOptions{
		sources: []crowdSniffSource{
			&mockSource{err: fmt.Errorf("npm down")},
			&mockSource{err: fmt.Errorf("github down")},
		},
		stdout: &bytes.Buffer{},
		stderr: &stderr,
	}

	err := runCrowdSniff(context.Background(), "test", "https://api.example.com", "", false, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no endpoints discovered")

	// Both warnings should be logged.
	assert.Contains(t, stderr.String(), "npm down")
	assert.Contains(t, stderr.String(), "github down")
}

func TestRunCrowdSniff_OutputDirCreated(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "nested", "dir")
	outputPath := filepath.Join(outputDir, "test-spec.yaml")

	opts := crowdSniffOptions{
		sources: []crowdSniffSource{
			&mockSource{result: endpointsResult("https://api.example.com",
				crowdsniff.DiscoveredEndpoint{Method: "GET", Path: "/users", SourceTier: crowdsniff.TierCodeSearch, SourceName: "gh"},
			)},
		},
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}

	err := runCrowdSniff(context.Background(), "test", "", outputPath, false, opts)
	require.NoError(t, err)

	_, err = os.Stat(outputPath)
	assert.NoError(t, err)
}

func TestRunCrowdSniff_CmdIntegration(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "test-spec.yaml")

	cmd := newCrowdSniffCmdWithOptions(crowdSniffOptions{
		sources: []crowdSniffSource{
			&mockSource{result: endpointsResult("https://api.example.com",
				crowdsniff.DiscoveredEndpoint{Method: "GET", Path: "/v1/users", SourceTier: crowdsniff.TierOfficialSDK, SourceName: "sdk"},
			)},
		},
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	})

	cmd.SetArgs([]string{"--api", "example", "--output", outputPath})
	err := cmd.Execute()
	require.NoError(t, err)

	_, err = os.Stat(outputPath)
	assert.NoError(t, err)
}

func TestValidateCrowdSniffAPIName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		api     string
		wantErr string
	}{
		{name: "valid name", api: "notion", wantErr: ""},
		{name: "valid domain", api: "api.notion.com", wantErr: ""},
		{name: "valid URL", api: "https://api.notion.com/v1", wantErr: ""},
		{name: "empty", api: "", wantErr: "required"},
		{name: "whitespace only", api: "   ", wantErr: "required"},
		{name: "newline injection", api: "notion\nHost: evil.com", wantErr: "invalid characters"},
		{name: "null byte", api: "notion\x00evil", wantErr: "invalid characters"},
		{name: "path traversal", api: "../../.ssh/evil", wantErr: "path traversal"},
		{name: "backslash traversal", api: `..\..\evil`, wantErr: "path traversal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateCrowdSniffAPIName(tt.api)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestDefaultCrowdSniffCachePath(t *testing.T) {
	t.Parallel()

	path := defaultCrowdSniffCachePath("notion")
	assert.Contains(t, path, "crowd-sniff")
	assert.Contains(t, path, "notion-spec.yaml")
}

func TestRunCrowdSniff_JSONOutput_IncludesParamCount(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "test-spec.yaml")
	var stdout bytes.Buffer

	opts := crowdSniffOptions{
		sources: []crowdSniffSource{
			&mockSource{result: endpointsResult("https://api.example.com",
				crowdsniff.DiscoveredEndpoint{
					Method:     "GET",
					Path:       "/v1/games",
					SourceTier: crowdsniff.TierOfficialSDK,
					SourceName: "sdk",
					Params: []crowdsniff.DiscoveredParam{
						{Name: "steamid", Type: "string", Required: true},
						{Name: "count", Type: "integer", Required: false},
					},
				},
				crowdsniff.DiscoveredEndpoint{
					Method:     "GET",
					Path:       "/v1/users",
					SourceTier: crowdsniff.TierOfficialSDK,
					SourceName: "sdk",
					Params: []crowdsniff.DiscoveredParam{
						{Name: "limit", Type: "integer", Required: false},
					},
				},
			)},
		},
		stdout: &stdout,
		stderr: &bytes.Buffer{},
	}

	err := runCrowdSniff(context.Background(), "test", "https://api.example.com", outputPath, true, opts)
	require.NoError(t, err)

	var jsonOut map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &jsonOut))
	// 2 params on first endpoint + 1 param on second endpoint = 3 total
	assert.Equal(t, float64(3), jsonOut["param_count"], "expected param_count to be 3")
}

func TestRunCrowdSniff_ParamsInWrittenSpec(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "test-spec.yaml")

	opts := crowdSniffOptions{
		sources: []crowdSniffSource{
			&mockSource{result: endpointsResult("https://api.example.com",
				crowdsniff.DiscoveredEndpoint{
					Method:     "GET",
					Path:       "/v1/games",
					SourceTier: crowdsniff.TierOfficialSDK,
					SourceName: "sdk",
					Params: []crowdsniff.DiscoveredParam{
						{Name: "steamid", Type: "string", Required: true},
						{Name: "include_appinfo", Type: "boolean", Required: false, Default: "true"},
					},
				},
			)},
		},
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
	}

	err := runCrowdSniff(context.Background(), "test", "https://api.example.com", outputPath, false, opts)
	require.NoError(t, err)

	// Read the written spec YAML and verify params are present.
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	specContent := string(data)

	assert.Contains(t, specContent, "params:")
	assert.Contains(t, specContent, "name: steamid")
	assert.Contains(t, specContent, "name: include_appinfo")
	assert.Contains(t, specContent, "type: boolean")
	assert.Contains(t, specContent, "required: true")
}

func TestIsHTTPS(t *testing.T) {
	t.Parallel()

	assert.True(t, isHTTPS("https://api.example.com"))
	assert.True(t, isHTTPS("HTTPS://API.EXAMPLE.COM"))
	assert.False(t, isHTTPS("http://api.example.com"))
	assert.False(t, isHTTPS("ftp://api.example.com"))
	assert.False(t, isHTTPS(""))
	assert.False(t, isHTTPS("not-a-url"))
}

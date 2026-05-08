package cli

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCmdConsumesTrafficAnalysis(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	analysisPath := filepath.Join(dir, "traffic-analysis.json")
	outputDir := filepath.Join(dir, "analysisapp")

	require.NoError(t, os.WriteFile(specPath, []byte(`name: analysisapp
description: Analysis app API
version: 0.1.0
base_url: https://api.example.com
auth:
  type: none
config:
  format: toml
  path: ~/.config/analysisapp-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))
	require.NoError(t, os.WriteFile(analysisPath, []byte(`{
  "version": "1",
  "summary": {
    "target_url": "https://example.com/app?token=secret#frag",
    "entry_count": 4,
    "api_entry_count": 2
  },
  "protocols": [
    {"label": "rest_json", "confidence": 0.9}
  ],
  "generation_hints": ["requires_browser_auth"],
  "warnings": [
    {"type": "weak_schema_evidence", "message": "Only one response shape was captured.", "confidence": 0.7}
  ],
  "candidate_commands": [
    {"name": "items-list", "rationale": "Observed successful list traffic.", "confidence": 0.8}
  ]
}`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
		"--spec-source", "browser-sniffed",
		"--traffic-analysis", analysisPath,
	})

	require.NoError(t, cmd.Execute())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "## Discovery Signals")
	assert.Contains(t, string(readme), "Target observed: https://example.com/app")
	assert.NotContains(t, string(readme), "token=secret")
	assert.Contains(t, string(readme), "requires_browser_auth")
	assert.Contains(t, string(readme), "weak_schema_evidence")

	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(skill), "## Discovery Signals")
	assert.Contains(t, string(skill), "requires_browser_auth")

	agentContext, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "agent_context.go"))
	require.NoError(t, err)
	assert.Contains(t, string(agentContext), `Source:        "traffic-analysis"`)
	assert.Contains(t, string(agentContext), `TargetURL:     "https://example.com/app"`)
	assert.NotContains(t, string(agentContext), "token=secret")
	assert.Contains(t, string(agentContext), `requires_browser_auth`)
	_, err = parser.ParseFile(token.NewFileSet(), "agent_context.go", agentContext, parser.ParseComments)
	require.NoError(t, err)
}

func TestGenerateCmdDoesNotCarryPlannedNovelFeaturesIntoManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	researchDir := filepath.Join(dir, "research")
	outputDir := filepath.Join(dir, "novelapp")
	require.NoError(t, os.MkdirAll(researchDir, 0o755))

	require.NoError(t, os.WriteFile(specPath, []byte(`name: novelapp
description: Novel app API
version: 0.1.0
base_url: https://api.example.com
auth:
  type: none
config:
  format: toml
  path: ~/.config/novelapp-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "research.json"), []byte(`{
  "api_name": "novelapp",
  "novelty_score": 8,
  "alternatives": [],
  "gaps": [],
  "patterns": [],
  "recommendation": "proceed",
  "researched_at": "2026-04-25T00:00:00Z",
  "novel_features": [
    {
      "name": "Item insight",
      "command": "items insight",
      "description": "Summarize item state",
      "rationale": "Requires local correlation"
    }
  ]
}`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
		"--research-dir", researchDir,
	})

	require.NoError(t, cmd.Execute())

	data, err := os.ReadFile(filepath.Join(outputDir, pipeline.CLIManifestFilename))
	require.NoError(t, err)
	var manifest pipeline.CLIManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	assert.Empty(t, manifest.NovelFeatures)
}

func TestGenerateCmdCarriesVerifiedNovelFeaturesIntoManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	researchDir := filepath.Join(dir, "research")
	outputDir := filepath.Join(dir, "verifiedapp")
	require.NoError(t, os.MkdirAll(researchDir, 0o755))

	require.NoError(t, os.WriteFile(specPath, []byte(`name: verifiedapp
description: Verified app API
version: 0.1.0
base_url: https://api.example.com
auth:
  type: none
config:
  format: toml
  path: ~/.config/verifiedapp-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "research.json"), []byte(`{
  "api_name": "verifiedapp",
  "novelty_score": 8,
  "alternatives": [],
  "gaps": [],
  "patterns": [],
  "recommendation": "proceed",
  "researched_at": "2026-04-25T00:00:00Z",
  "novel_features": [
    {
      "name": "Planned insight",
      "command": "items planned",
      "description": "Planned but not verified",
      "rationale": "Requires local correlation"
    }
  ],
  "novel_features_built": [
    {
      "name": "Built insight",
      "command": "items insight",
      "description": "Summarize item state",
      "rationale": "Requires local correlation"
    }
  ]
}`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
		"--research-dir", researchDir,
	})

	require.NoError(t, cmd.Execute())

	data, err := os.ReadFile(filepath.Join(outputDir, pipeline.CLIManifestFilename))
	require.NoError(t, err)
	var manifest pipeline.CLIManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	require.Len(t, manifest.NovelFeatures, 1)
	assert.Equal(t, "Built insight", manifest.NovelFeatures[0].Name)
	assert.Equal(t, "items insight", manifest.NovelFeatures[0].Command)
}

func TestGenerateCmdAppliesBrowserClearanceReachability(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	analysisPath := filepath.Join(dir, "traffic-analysis.json")
	outputDir := filepath.Join(dir, "clearanceapp")

	require.NoError(t, os.WriteFile(specPath, []byte(`name: clearanceapp
description: Clearance app API
version: 0.1.0
base_url: https://www.producthunt.com
auth:
  type: none
config:
  format: toml
  path: ~/.config/clearanceapp-pp-cli/config.toml
resources:
  posts:
    description: Manage posts
    endpoints:
      list:
        method: GET
        path: /frontend/graphql
        description: List posts
`), 0o644))
	require.NoError(t, os.WriteFile(analysisPath, []byte(`{
  "version": "1",
  "summary": {
    "target_url": "https://www.producthunt.com",
    "entry_count": 1,
    "api_entry_count": 1
  },
  "reachability": {
    "mode": "browser_clearance_http",
    "confidence": 0.9,
    "reasons": ["managed bot challenge observed"]
  },
  "generation_hints": ["browser_clearance_required", "graphql_persisted_query"]
}`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
		"--traffic-analysis", analysisPath,
	})

	require.NoError(t, cmd.Execute())

	gomod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(gomod), "github.com/enetx/surf")

	authGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	assert.Contains(t, string(authGo), "auth login --chrome")
	assert.Contains(t, string(authGo), "auth login --browser")
	assert.Contains(t, string(authGo), "auth refresh")
	assert.Contains(t, string(authGo), "auth refresh-queries")
	assert.Contains(t, string(authGo), ".producthunt.com")
	assert.Contains(t, string(authGo), "Could not close temporary browser capture session")
	assert.NotContains(t, string(authGo), "newAuthCloseBrowserCmd")

	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(clientGo), `req.Header.Set("User-Agent"`)
	assert.Contains(t, string(clientGo), `"github.com/enetx/surf"`)
	assert.Contains(t, string(clientGo), "ForceHTTP3()")
	assert.NotContains(t, string(clientGo), "runBrowserUseFetch")
	assert.NotContains(t, string(clientGo), "runAgentBrowserFetch")
	assert.NotContains(t, string(clientGo), "browser runtime required")

	doctorGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(doctorGo), "doctorBrowserRuntimeStatus")

	configGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(configGo), "BrowserRuntime")

	agentContext, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "agent_context.go"))
	require.NoError(t, err)
	assert.Contains(t, string(agentContext), `Reachability:  "browser_clearance_http (90% confidence)"`)
}

func TestGenerateCmdDoesNotRequireBrowserProofForPostOnlyClearance(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	analysisPath := filepath.Join(dir, "traffic-analysis.json")
	outputDir := filepath.Join(dir, "clearancepostapp")

	require.NoError(t, os.WriteFile(specPath, []byte(`name: clearancepostapp
description: Clearance POST app API
version: 0.1.0
base_url: https://www.producthunt.com
auth:
  type: none
config:
  format: toml
  path: ~/.config/clearancepostapp-pp-cli/config.toml
resources:
  graphql:
    description: GraphQL endpoint
    endpoints:
      query:
        method: POST
        path: /frontend/graphql
        description: Run GraphQL query
        body:
          - name: body
            type: object
            required: true
`), 0o644))
	require.NoError(t, os.WriteFile(analysisPath, []byte(`{
  "version": "1",
  "summary": {
    "target_url": "https://www.producthunt.com",
    "entry_count": 1,
    "api_entry_count": 1
  },
  "reachability": {
    "mode": "browser_clearance_http",
    "confidence": 0.9,
    "reasons": ["managed bot challenge observed"]
  },
  "generation_hints": ["browser_clearance_required", "graphql_persisted_query"]
}`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
		"--traffic-analysis", analysisPath,
	})

	require.NoError(t, cmd.Execute())

	clientGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(clientGo), `"github.com/enetx/surf"`)
	assert.Contains(t, string(clientGo), "ForceHTTP3()")
	assert.NotContains(t, string(clientGo), "runBrowserUseFetch")
	assert.NotContains(t, string(clientGo), "runAgentBrowserFetch")

	authGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(authGo), "no browser-session validation endpoint configured")
	assert.NotContains(t, string(authGo), "browser-session-proof.json")
	assert.NotContains(t, string(authGo), "newAuthCloseBrowserCmd")
	assert.NotContains(t, string(authGo), "auth close-browser")
}

func TestGenerateCmdRejectsBrowserRequiredReachabilityWithoutReplayableTransport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	analysisPath := filepath.Join(dir, "traffic-analysis.json")
	outputDir := filepath.Join(dir, "browserrequiredapp")

	require.NoError(t, os.WriteFile(specPath, []byte(`name: browserrequiredapp
description: Browser required app API
version: 0.1.0
base_url: https://www.example.com
auth:
  type: none
config:
  format: toml
  path: ~/.config/browserrequiredapp-pp-cli/config.toml
resources:
  pages:
    description: Manage pages
    endpoints:
      list:
        method: GET
        path: /app
        description: List pages
`), 0o644))
	require.NoError(t, os.WriteFile(analysisPath, []byte(`{
  "version": "1",
  "summary": {
    "target_url": "https://www.example.com/app",
    "entry_count": 1,
    "api_entry_count": 1
  },
  "reachability": {
    "mode": "browser_required",
    "confidence": 0.85,
    "reasons": ["page-context execution required"]
  },
  "generation_hints": ["requires_page_context"]
}`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
		"--traffic-analysis", analysisPath,
	})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires live browser page-context execution")
	assert.Contains(t, err.Error(), "not a shippable printed CLI runtime")
}

func TestMergeSpecsPrefersReplayableBrowserTransportOverUnshippablePageContext(t *testing.T) {
	t.Parallel()

	runtimeSpec := &spec.APISpec{
		Name:          "runtime",
		Version:       "0.1.0",
		BaseURL:       "https://runtime.example.com",
		HTTPTransport: "browser-runtime",
		Resources:     map[string]spec.Resource{},
		Types:         map[string]spec.TypeDef{},
	}
	chromeSpec := &spec.APISpec{
		Name:          "chrome",
		Version:       "0.1.0",
		BaseURL:       "https://chrome.example.com",
		HTTPTransport: spec.HTTPTransportBrowserChrome,
		Resources:     map[string]spec.Resource{},
		Types:         map[string]spec.TypeDef{},
	}

	mergedRuntimeFirst := mergeSpecs([]*spec.APISpec{runtimeSpec, chromeSpec}, "merged")
	assert.Equal(t, spec.HTTPTransportBrowserChrome, mergedRuntimeFirst.HTTPTransport)

	mergedRuntimeLast := mergeSpecs([]*spec.APISpec{chromeSpec, runtimeSpec}, "merged")
	assert.Equal(t, spec.HTTPTransportBrowserChrome, mergedRuntimeLast.HTTPTransport)

	httpSpec := &spec.APISpec{
		Name:          "http",
		Version:       "0.1.0",
		BaseURL:       "https://http.example.com",
		HTTPTransport: spec.HTTPTransportBrowserHTTP,
		Resources:     map[string]spec.Resource{},
		Types:         map[string]spec.TypeDef{},
	}
	standardSpec := &spec.APISpec{
		Name:          "standard",
		Version:       "0.1.0",
		BaseURL:       "https://standard.example.com",
		HTTPTransport: spec.HTTPTransportStandard,
		Resources:     map[string]spec.Resource{},
		Types:         map[string]spec.TypeDef{},
	}

	mergedHTTP := mergeSpecs([]*spec.APISpec{standardSpec, httpSpec}, "merged")
	assert.Equal(t, spec.HTTPTransportBrowserHTTP, mergedHTTP.HTTPTransport)

	mergedChrome := mergeSpecs([]*spec.APISpec{httpSpec, chromeSpec}, "merged")
	assert.Equal(t, spec.HTTPTransportBrowserChrome, mergedChrome.HTTPTransport)
}

func TestNormalizeHTTPTransportAllowsBrowserChromeH3(t *testing.T) {
	t.Parallel()

	got, err := normalizeHTTPTransport(spec.HTTPTransportBrowserHTTP)
	require.NoError(t, err)
	assert.Equal(t, spec.HTTPTransportBrowserHTTP, got)

	got, err = normalizeHTTPTransport(spec.HTTPTransportBrowserChromeH3)
	require.NoError(t, err)
	assert.Equal(t, spec.HTTPTransportBrowserChromeH3, got)

	_, err = normalizeHTTPTransport("browser-chrome-http3")
	require.ErrorContains(t, err, "browser-chrome-h3")

	_, err = normalizeHTTPTransport("browser-runtime")
	require.ErrorContains(t, err, "--transport must be one of")
}

func TestGenerateCmdInfersTrafficAnalysisForSniffedSpec(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "sample-spec.yaml")
	analysisPath := filepath.Join(dir, "sample-spec-traffic-analysis.json")
	outputDir := filepath.Join(dir, "implicitanalysis")

	require.NoError(t, os.WriteFile(specPath, []byte(`name: implicitanalysis
description: Implicit analysis API
version: 0.1.0
base_url: https://api.example.com
spec_source: sniffed
auth:
  type: none
config:
  format: toml
  path: ~/.config/implicitanalysis-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))
	require.NoError(t, os.WriteFile(analysisPath, []byte(`{
  "version": "1",
  "summary": {"entry_count": 2, "api_entry_count": 1},
  "generation_hints": ["weak_schema_confidence"]
}`), 0o600))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
	})

	require.NoError(t, cmd.Execute())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "weak_schema_confidence")

	gomod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(gomod), "github.com/enetx/surf")
}

func TestGenerateCmdAllowsSniffedSpecWithoutTrafficAnalysis(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "sample-spec.yaml")
	outputDir := filepath.Join(dir, "nosidecar")

	require.NoError(t, os.WriteFile(specPath, []byte(`name: nosidecar
description: No sidecar API
version: 0.1.0
base_url: https://api.example.com
spec_source: sniffed
auth:
  type: none
config:
  format: toml
  path: ~/.config/nosidecar-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
	})

	require.NoError(t, cmd.Execute())
	require.FileExists(t, filepath.Join(outputDir, "README.md"))
}

func TestGenerateCmdTransportOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "sample-spec.yaml")
	outputDir := filepath.Join(dir, "standardtransport")

	require.NoError(t, os.WriteFile(specPath, []byte(`name: standardtransport
description: Standard transport API
version: 0.1.0
base_url: https://api.example.com
spec_source: sniffed
auth:
  type: none
config:
  format: toml
  path: ~/.config/standardtransport-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
		"--transport", "standard",
	})

	require.NoError(t, cmd.Execute())

	gomod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	require.NoError(t, err)
	assert.NotContains(t, string(gomod), "github.com/enetx/surf")
}

func TestGenerateCmdRejectsPageContextTrafficAnalysisEvenWithTransportOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "sample-spec.yaml")
	analysisPath := filepath.Join(dir, "sample-spec-traffic-analysis.json")
	outputDir := filepath.Join(dir, "pagecontext")

	require.NoError(t, os.WriteFile(specPath, []byte(`name: pagecontext
description: Page context API
version: 0.1.0
base_url: https://api.example.com
spec_source: sniffed
auth:
  type: none
config:
  format: toml
  path: ~/.config/pagecontext-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))
	require.NoError(t, os.WriteFile(analysisPath, []byte(`{
  "version": "1",
  "reachability": {"mode": "browser_required"}
}`), 0o600))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
		"--transport", "browser-chrome",
	})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires live browser page-context execution")
	assert.NoFileExists(t, filepath.Join(outputDir, "README.md"))
}

func TestGenerateCmdRejectsInvalidTrafficAnalysis(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	analysisPath := filepath.Join(dir, "traffic-analysis.json")
	outputDir := filepath.Join(dir, "badanalysis")

	require.NoError(t, os.WriteFile(specPath, []byte(`name: badanalysis
description: Bad analysis API
version: 0.1.0
base_url: https://api.example.com
auth:
  type: none
config:
  format: toml
  path: ~/.config/badanalysis-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))
	require.NoError(t, os.WriteFile(analysisPath, []byte(`{"summary":{"entry_count":1}}`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
		"--traffic-analysis", analysisPath,
	})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "traffic analysis missing version")
}

func TestGenerateCmdRejectsTrafficAnalysisWithPlan(t *testing.T) {
	t.Parallel()

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--plan", filepath.Join(t.TempDir(), "plan.md"),
		"--traffic-analysis", filepath.Join(t.TempDir(), "traffic-analysis.json"),
	})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--traffic-analysis cannot be used with --plan")
}

func TestGenerateCmdHonorsExplicitOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	outputDir := filepath.Join(dir, "user-chosen-path")
	derivedDir := filepath.Join(dir, "explicitoutput")

	require.NoError(t, os.WriteFile(specPath, []byte(`name: explicitoutput
description: Explicit output API
version: 0.1.0
base_url: https://api.example.com
auth:
  type: none
config:
  format: toml
  path: ~/.config/explicitoutput-pp-cli/config.toml
resources:
  items:
    description: Manage items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`), 0o644))

	cmd := newGenerateCmd()
	cmd.SetArgs([]string{
		"--spec", specPath,
		"--output", outputDir,
		"--validate=false",
		"--force",
	})

	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(outputDir, "go.mod"),
		"generated files should land at the user-supplied --output path")
	assert.NoDirExists(t, derivedDir,
		"the spec-derived directory must not be created when --output is explicit")
}

func TestEnrichSpecFromCatalogCopiesGenerationMetadata(t *testing.T) {
	apiSpec := &spec.APISpec{Name: "test-api"}

	enrichSpecFromCatalogEntry(apiSpec, &catalog.Entry{
		DisplayName: "Test.API",
		OwnerName:   "Trevin Chow",
		MCP: spec.MCPConfig{
			Transport:     []string{"stdio", "http"},
			Orchestration: "code",
			EndpointTools: "hidden",
		},
	})

	assert.Equal(t, "Test.API", apiSpec.DisplayName)
	assert.Equal(t, "Trevin Chow", apiSpec.OwnerName)
	assert.Equal(t, []string{"stdio", "http"}, apiSpec.MCP.Transport)
	assert.Equal(t, "code", apiSpec.MCP.Orchestration)
	assert.Equal(t, "hidden", apiSpec.MCP.EndpointTools)
}

func TestEnrichSpecFromCatalogMatchesSpecURLWhenSlugDiffers(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:                        "cloud-run-admin",
		DisplayName:                 "Cloud Run Admin",
		DisplayNameDerivedFromTitle: true,
	}

	enrichSpecFromCatalog(apiSpec, "https://api.apis.guru/v2/specs/googleapis.com/run/v2/openapi.yaml")

	assert.Equal(t, "Google Cloud Run", apiSpec.DisplayName)
	assert.False(t, apiSpec.DisplayNameDerivedFromTitle)
	assert.Equal(t, "cloud", apiSpec.Category)
	assert.Equal(t, "https://cloud.google.com/run/docs/reference/rest", apiSpec.WebsiteURL)
}

func TestEnrichSpecFromCatalogReplacesTitleDerivedDisplayName(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:                        "trigger-dev",
		DisplayName:                 "Trigger Dev",
		DisplayNameDerivedFromTitle: true,
	}

	enrichSpecFromCatalogEntry(apiSpec, &catalog.Entry{
		DisplayName: "Trigger.dev",
	})

	assert.Equal(t, "Trigger.dev", apiSpec.DisplayName)
	assert.False(t, apiSpec.DisplayNameDerivedFromTitle)
}

func TestEnrichSpecFromCatalogKeepsExplicitDisplayName(t *testing.T) {
	apiSpec := &spec.APISpec{
		Name:        "trigger-dev",
		DisplayName: "Spec.dev",
	}

	enrichSpecFromCatalogEntry(apiSpec, &catalog.Entry{
		DisplayName: "Trigger.dev",
	})

	assert.Equal(t, "Spec.dev", apiSpec.DisplayName)
}

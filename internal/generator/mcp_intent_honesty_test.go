package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateMCPIntentAnnotationsAndHonestDescription(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("intent-honesty")
	apiSpec.Resources = map[string]spec.Resource{
		"availability": {
			Description: "Award availability",
			Endpoints: map[string]spec.Endpoint{
				"search": {Method: "GET", Path: "/search", Description: "Search award availability"},
				"book":   {Method: "POST", Path: "/book", Description: "Hold a trip"},
				"cancel": {Method: "DELETE", Path: "/book/{id}", Description: "Cancel a trip"},
			},
		},
	}
	apiSpec.MCP = spec.MCPConfig{
		Intents: []spec.Intent{
			{
				Name:        "find_best_award",
				Description: "Find the best award, then fetch bookable trip detail for the top result. Defaults to business",
				Params: []spec.IntentParam{
					{Name: "origin", Type: "string", Required: true, Description: "Origin airport"},
					{Name: "cabin", Type: "string", Description: "Cabin class"},
				},
				Steps: []spec.IntentStep{
					{
						Endpoint: "availability.search",
						Bind:     map[string]string{"origin": "${input.origin}", "cabin": "${input.cabin}"},
						Capture:  "results",
					},
				},
				Returns: "results",
			},
			{
				Name:        "hold_trip",
				Description: "Hold a trip",
				Params: []spec.IntentParam{
					{Name: "id", Type: "string", Required: true, Description: "Trip id"},
				},
				Steps: []spec.IntentStep{
					{
						Endpoint: "availability.book",
						Bind:     map[string]string{"id": "${input.id}"},
						Capture:  "hold",
					},
				},
				Returns: "hold",
			},
			{
				Name:        "cancel_trip",
				Description: "Cancel a trip",
				Params: []spec.IntentParam{
					{Name: "id", Type: "string", Required: true, Description: "Trip id"},
				},
				Steps: []spec.IntentStep{
					{
						Endpoint: "availability.cancel",
						Bind:     map[string]string{"id": "${input.id}"},
						Capture:  "cancelled",
					},
				},
				Returns: "cancelled",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	body := readGeneratedFile(t, outputDir, "internal", "mcp", "intents.go")
	assert.Contains(t, body, `mcplib.WithReadOnlyHintAnnotation(true)`)
	assert.Contains(t, body, `mcplib.WithIdempotentHintAnnotation(true)`)
	assert.Contains(t, body, `mcplib.WithDestructiveHintAnnotation(true)`)
	assert.NotContains(t, body, "then fetch bookable trip detail")
	assert.NotContains(t, body, "Defaults to business")
	assert.Contains(t, body, `input["cabin"] = "business"`)
	assert.Contains(t, body, "Search award availability")

	const runtimeTest = `package mcp

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestIntentSafetyMetadata(t *testing.T) {
	s := server.NewMCPServer("intent-honesty", "test")
	RegisterIntents(s)
	tools := s.ListTools()

	find, ok := tools["find_best_award"]
	if !ok {
		t.Fatalf("find_best_award missing: %#v", tools)
	}
	if find.Tool.Annotations.ReadOnlyHint == nil || !*find.Tool.Annotations.ReadOnlyHint {
		t.Fatalf("find_best_award readOnlyHint = %v, want true", find.Tool.Annotations.ReadOnlyHint)
	}
	if find.Tool.Annotations.DestructiveHint == nil || *find.Tool.Annotations.DestructiveHint {
		t.Fatalf("find_best_award destructiveHint = %v, want false", find.Tool.Annotations.DestructiveHint)
	}
	if find.Tool.Annotations.IdempotentHint == nil || !*find.Tool.Annotations.IdempotentHint {
		t.Fatalf("find_best_award idempotentHint = %v, want true", find.Tool.Annotations.IdempotentHint)
	}
	if strings.Contains(find.Tool.Description, "then fetch bookable trip detail") {
		t.Fatalf("description overclaims unimplemented step: %q", find.Tool.Description)
	}
	if strings.Contains(find.Tool.Description, "Defaults to") {
		t.Fatalf("description still advertises an unapplied default: %q", find.Tool.Description)
	}

	hold, ok := tools["hold_trip"]
	if !ok {
		t.Fatalf("hold_trip missing")
	}
	if hold.Tool.Annotations.ReadOnlyHint != nil && *hold.Tool.Annotations.ReadOnlyHint {
		t.Fatalf("hold_trip must not be read-only")
	}
	if hold.Tool.Annotations.DestructiveHint == nil || *hold.Tool.Annotations.DestructiveHint {
		t.Fatalf("hold_trip destructiveHint = %v, want false", hold.Tool.Annotations.DestructiveHint)
	}
	if hold.Tool.Annotations.OpenWorldHint == nil || !*hold.Tool.Annotations.OpenWorldHint {
		t.Fatalf("hold_trip openWorldHint = %v, want true", hold.Tool.Annotations.OpenWorldHint)
	}

	cancel, ok := tools["cancel_trip"]
	if !ok {
		t.Fatalf("cancel_trip missing")
	}
	if cancel.Tool.Annotations.ReadOnlyHint != nil && *cancel.Tool.Annotations.ReadOnlyHint {
		t.Fatalf("cancel_trip must not be read-only")
	}
	if cancel.Tool.Annotations.DestructiveHint == nil || !*cancel.Tool.Annotations.DestructiveHint {
		t.Fatalf("cancel_trip destructiveHint = %v, want true", cancel.Tool.Annotations.DestructiveHint)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "intent_honesty_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "TestIntentSafetyMetadata", "-count=1")
}

func TestGenerateMCPIntentInvalidTypedDefaultStillCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("typed-defaults")
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/records", Description: "List records"},
			},
		},
	}
	apiSpec.MCP = spec.MCPConfig{
		Intents: []spec.Intent{
			{
				Name:        "search_records",
				Description: "Search records",
				Params: []spec.IntentParam{
					{Name: "limit", Type: "integer", Default: "12x"},
					{Name: "offset", Type: "integer", Default: "20"},
					{Name: "include", Type: "boolean", Default: "maybe"},
					{Name: "compact", Type: "boolean", Default: "false"},
				},
				Steps: []spec.IntentStep{
					{
						Endpoint: "records.list",
						Bind: map[string]string{
							"limit":   "${input.limit}",
							"offset":  "${input.offset}",
							"include": "${input.include}",
							"compact": "${input.compact}",
						},
						Capture: "records",
					},
				},
				Returns: "records",
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	body := readGeneratedFile(t, outputDir, "internal", "mcp", "intents.go")
	assert.NotContains(t, body, "= 12x")
	assert.NotContains(t, body, `input["limit"]`)
	assert.Contains(t, body, `input["offset"] = 20`)
	assert.NotContains(t, body, `input["include"]`)
	assert.Contains(t, body, `input["compact"] = false`)
	requireGeneratedCompiles(t, outputDir)
}

func TestGenerateConfigLoadIgnoresUnresolvedMCPBPlaceholder(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcpbplaceholder")
	apiSpec.BaseURL = "https://api.example.com"
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, src, `cliutil.EnvOverride("MCPBPLACEHOLDER_BASE_URL")`)

	const runtimeTest = `package config

import (
	"path/filepath"
	"testing"
)

func TestLoadIgnoresUnresolvedMCPBPlaceholder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("MCPBPLACEHOLDER_BASE_URL", "${user_config.mcpbplaceholder_base_url}")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BaseURL != "https://api.example.com" {
		t.Fatalf("BaseURL = %q, want spec default", cfg.BaseURL)
	}
}

func TestLoadAppliesRealBaseURLOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("MCPBPLACEHOLDER_BASE_URL", "https://staging.example.com")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BaseURL != "https://staging.example.com" {
		t.Fatalf("BaseURL = %q, want staging override", cfg.BaseURL)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "mcpb_placeholder_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/config", "-run", "TestLoad(IgnoresUnresolvedMCPBPlaceholder|AppliesRealBaseURLOverride)", "-count=1")
}

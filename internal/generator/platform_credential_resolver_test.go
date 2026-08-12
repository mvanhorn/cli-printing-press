package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedPlatformCredentialResolverContract(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("provider-neutral-resolver")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	generatedFiles := []string{
		"internal/platform/profile.go",
		"internal/platform/conformance_test.go",
		"internal/cli/platform_client.go",
		"internal/cli/platform_cli_test.go",
		"AGENTS.md",
	}
	for _, relativePath := range generatedFiles {
		generated := readGeneratedFile(t, outputDir, relativePath)
		for _, forbidden := range []string{
			"OnePasswordResolver",
			"1password-pp-cli",
			"PRINTING_PRESS_ONEPASSWORD_CLI",
			"platformResolverFactory",
			"auth.credential_resolvers",
			"CredentialResolverRegistry",
		} {
			assert.NotContains(t, generated, forbidden, relativePath)
		}
	}

	profile := readGeneratedFile(t, outputDir, "internal", "platform", "profile.go")
	assert.NotContains(t, profile, "op://")

	client := readGeneratedFile(t, outputDir, "internal", "cli", "platform_client.go")
	assert.Contains(t, client, "CredentialResolverFactory")
	assert.Contains(t, client, "ValidateSourceProfile")

	agents := readGeneratedFile(t, outputDir, "AGENTS.md")
	assert.Contains(t, agents, "CredentialResolverFactory")
	assert.Contains(t, agents, "ValidateSourceProfile")
	assert.Contains(t, agents, "preserved hand-authored file")
	assert.NotContains(t, agents, "OnePassword")

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedPlatformCredentialResolverSupportsPreservedDownstreamAdapter(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	specPath := filepath.Join(repoRoot, "testdata", "stytch.yaml")
	apiSpec, err := spec.Parse(specPath)
	require.NoError(t, err)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	modulePath := generatedModulePath(t, outputDir)
	adapterPath := filepath.Join(outputDir, "internal", "cli", "fixture_platform_source.go")
	require.NoError(t, os.WriteFile(adapterPath, []byte(fixturePlatformAdapterSource(modulePath)), 0o644))

	customAdapterBeforeReprint, err := os.ReadFile(adapterPath)
	require.NoError(t, err)
	profilePath := filepath.Join(outputDir, "internal", "platform", "profile.go")
	profileBeforeReprint, err := os.ReadFile(profilePath)
	require.NoError(t, err)
	legacyProfile := append([]byte("// legacy OnePasswordResolver marker\n"), profileBeforeReprint...)
	require.NoError(t, os.WriteFile(profilePath, legacyProfile, 0o644))

	command := exec.Command("go", "run", "./cmd/cli-printing-press", "generate", "--spec", specPath, "--output", outputDir, "--force", "--validate=false")
	command.Dir = repoRoot
	home := t.TempDir()
	command.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"GOMODCACHE="+goEnvValue(t, "GOMODCACHE"),
		"GOCACHE="+goEnvValue(t, "GOCACHE"),
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))

	customAdapterAfterReprint, err := os.ReadFile(adapterPath)
	require.NoError(t, err)
	assert.Equal(t, customAdapterBeforeReprint, customAdapterAfterReprint, "reprint must retain the downstream adapter file")
	refreshedProfile, err := os.ReadFile(profilePath)
	require.NoError(t, err)
	assert.NotContains(t, string(refreshedProfile), "OnePasswordResolver")
	requireGeneratedCompiles(t, outputDir)

	testPath := filepath.Join(outputDir, "internal", "cli", "fixture_platform_source_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(fixturePlatformAdapterTestSource(modulePath)), 0o644))
	mcpTestPath := filepath.Join(outputDir, "internal", "mcp", "fixture_platform_source_test.go")
	require.NoError(t, os.WriteFile(mcpTestPath, []byte(fixturePlatformMCPTestSource(modulePath)), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "./internal/mcp", "-run", "TestFixturePlatform", "-count=1")
}

func generatedModulePath(t *testing.T, outputDir string) string {
	t.Helper()

	goMod := readGeneratedFile(t, outputDir, "go.mod")
	line := strings.SplitN(goMod, "\n", 2)[0]
	modulePath := strings.TrimSpace(strings.TrimPrefix(line, "module "))
	require.NotEmpty(t, modulePath)
	return modulePath
}

func fixturePlatformAdapterSource(modulePath string) string {
	return strings.ReplaceAll(`package cli

import (
	"context"
	"fmt"

	"__MODULE_PATH__/internal/platform"
)

var fixturePlatformCredential []byte

type fixturePlatformResolver struct{}

func (fixturePlatformResolver) Resolve(_ context.Context, reference string) ([]byte, error) {
	if reference != "op://fixture/opaque/token" {
		return nil, fmt.Errorf("unexpected fixture reference %q", reference)
	}
	return []byte("fixture-secret"), nil
}

type fixturePlatformAdapter struct{}

func (fixturePlatformAdapter) ProbeIdentity(_ context.Context, credentials platform.ResolvedCredentials, source platform.SourceProfile) (platform.ObservedIdentity, error) {
	fixturePlatformCredential = credentials["credential"]
	if string(fixturePlatformCredential) != "fixture-secret" {
		return platform.ObservedIdentity{}, fmt.Errorf("fixture resolver was not used")
	}
	if source.CredentialRef != "op://fixture/opaque/token" {
		return platform.ObservedIdentity{}, fmt.Errorf("selected reference was changed")
	}
	return platform.ObservedIdentity{BaseURL: "https://fixture.tenant.example"}, nil
}

func (fixturePlatformAdapter) EndpointClass() string { return "GET /fixture/identity" }

func registerFixturePlatformSource() {
	registerPlatformSource(platformSourceRegistration{
		Source:  "fixture",
		Adapter: fixturePlatformAdapter{},
		CredentialResolverFactory: func() platform.CredentialResolver {
			return fixturePlatformResolver{}
		},
		ValidateSourceProfile: func(source platform.SourceProfile) error {
			if source.CredentialRef != "op://fixture/opaque/token" {
				return fmt.Errorf("fixture validator received a changed reference")
			}
			return nil
		},
	})
}

func RegisterFixturePlatformSourceForTest() { registerFixturePlatformSource() }

func FixturePlatformCredentialsZero() bool {
	if fixturePlatformCredential == nil {
		return false
	}
	for _, value := range fixturePlatformCredential {
		if value != 0 {
			return false
		}
	}
	return true
}
`, "__MODULE_PATH__", modulePath)
}

func fixturePlatformAdapterTestSource(modulePath string) string {
	return strings.ReplaceAll(`package cli

import (
	"bytes"
	"context"
	"os"
	"testing"

	"__MODULE_PATH__/internal/platform"
)

func TestFixturePlatformSourceResolvesThroughCLIAndMCP(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("PRINTING_PRESS_CLIENT_PROFILE", "tenant-a")

	err := platform.SaveProfile(&platform.Profile{
		SchemaVersion: platform.ProfileSchemaVersion,
		Name:          "tenant-a",
		Sources: map[string]platform.SourceProfile{
			"fixture": {
				CredentialRef:   "op://fixture/opaque/token",
				ExpectedBaseURL: "https://fixture.tenant.example",
			},
			"sibling": {
				CredentialRef:   "opaque://sibling/value",
				ExpectedBaseURL: "https://sibling.tenant.example",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	previousRegistration := registeredPlatformSource
	t.Cleanup(func() { registeredPlatformSource = previousRegistration })
	registerFixturePlatformSource()

	previousArgs := os.Args
	t.Cleanup(func() { os.Args = previousArgs })
	os.Args = []string{"preserved-platform-adapter-pp-cli", "whoami", "--client-profile", "tenant-a", "--json"}
	if err := Execute(); err != nil {
		t.Fatal(err)
	}
	if !FixturePlatformCredentialsZero() {
		t.Fatal("CLI execution did not zero the resolver-returned credential buffer")
	}

	if err := BindMCPServerProfile(); err != nil {
		t.Fatal(err)
	}
	session, err := VerifyMCPInvocation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if session.GateOutcome != platform.GateVerified {
		t.Fatalf("MCP gate outcome = %q, want %q", session.GateOutcome, platform.GateVerified)
	}
	if got := string(session.Credentials["credential"]); got != "fixture-secret" {
		t.Fatalf("MCP resolver value = %q, want fixture-secret", got)
	}
	session.ZeroCredentials()
	if !bytes.Equal(fixturePlatformCredential, make([]byte, len(fixturePlatformCredential))) {
		t.Fatal("MCP verification did not zero the resolver-returned credential buffer")
	}
}
`, "__MODULE_PATH__", modulePath)
}

func fixturePlatformMCPTestSource(modulePath string) string {
	return strings.ReplaceAll(`package mcp

import (
	"context"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"__MODULE_PATH__/internal/cli"
	"__MODULE_PATH__/internal/platform"
)

func TestFixturePlatformMCPWrapperZeroesCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("PRINTING_PRESS_CLIENT_PROFILE", "tenant-a")

	if err := platform.SaveProfile(&platform.Profile{
		SchemaVersion: platform.ProfileSchemaVersion,
		Name:          "tenant-a",
		Sources: map[string]platform.SourceProfile{
			"fixture": {CredentialRef: "op://fixture/opaque/token", ExpectedBaseURL: "https://fixture.tenant.example"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	cli.RegisterFixturePlatformSourceForTest()
	if err := cli.BindMCPServerProfile(); err != nil {
		t.Fatal(err)
	}

	mcpServer := server.NewMCPServer("fixture", "test")
	result, err := requireFreshTenantGate(mcpServer, func(ctx context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		session := platform.SessionFromContext(ctx)
		if session == nil || string(session.Credentials["credential"]) != "fixture-secret" {
			return mcplib.NewToolResultError("fixture credential was not available to the MCP handler"), nil
		}
		return mcplib.NewToolResultText("ok"), nil
	})(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Name: "fixture"}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("MCP wrapper result=%#v err=%v", result, err)
	}
	if !cli.FixturePlatformCredentialsZero() {
		t.Fatal("MCP wrapper did not zero the resolver-returned credential buffer")
	}
}
`, "__MODULE_PATH__", modulePath)
}

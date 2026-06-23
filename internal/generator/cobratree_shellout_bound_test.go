package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateCobratreeShellOutBoundsResultBytes verifies the generated
// cobratree shell-out path caps raw companion-CLI stdout to the MCP
// tool-result byte budget. Typed endpoint tools enforce the same ceiling with
// a JSON-aware envelope; the shell-out path previously returned unbounded raw
// stdout, so a verbose command could blow the budget the typed tools protect.
func TestGenerateCobratreeShellOutBoundsResultBytes(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("shellout-bound")
	outputDir := filepath.Join(t.TempDir(), "shellout-bound-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "mcp", "cobratree", "bound_shell_result_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package cobratree

import (
	"strings"
	"testing"
)

func TestBoundShellResultCapsOversizedOutput(t *testing.T) {
	small := "small output"
	if got := boundShellResult(small); got != small {
		t.Fatalf("small output must pass through unchanged: got %q", got)
	}

	// Oversized inputs must always fit the byte budget once wrapped in the JSON
	// truncation envelope, including JSON-heavy output whose characters expand
	// under json.Marshal escaping (quotes -> \", backslashes -> \\, control
	// chars -> \uXXXX). A raw-byte preview limit would overshoot for these.
	for name, in := range map[string]string{
		"plain ascii":   strings.Repeat("x", 200000),
		"all quotes":    strings.Repeat("\"", 200000),
		"all backslash": strings.Repeat("\\", 200000),
		"all control":   strings.Repeat("\x01", 200000),
	} {
		got := boundShellResult(in)
		if len(got) > mcpShellResultMaxBytes {
			t.Fatalf("%s: oversized output must be capped to the budget %d, got %d bytes", name, mcpShellResultMaxBytes, len(got))
		}
		if !strings.Contains(got, "truncated") {
			t.Fatalf("%s: truncated result must carry a truncation marker, got prefix %q", name, got[:min(200, len(got))])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/mcp/cobratree", "-run", "TestBoundShellResultCapsOversizedOutput", "-count=1")
}

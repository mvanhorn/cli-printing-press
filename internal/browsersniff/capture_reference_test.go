package browsersniff

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserSniffReferenceGraphQLInterceptorCapturesRequestBodies(t *testing.T) {
	t.Parallel()

	snippet := extractBrowserUseGraphQLSnippet(t)
	runNodeSnippet(t, snippet+`
await window.fetch(new Request('https://example.com/graphql', {
  method: 'POST',
  body: JSON.stringify({operationName: 'RequestObjectOp', variables: {prompt: 'x'}}),
  headers: {'content-type': 'application/json'}
}));
await window.fetch('https://example.com/graphql', {
  method: 'POST',
  body: JSON.stringify({operationName: 'LegacyOptionsOp', variables: {page: 1}})
});
await new Promise(resolve => setTimeout(resolve, 0));
window.__gqlOps.sort((a, b) => a.op.localeCompare(b.op));
assertEqual(window.__gqlOps, [
  {op: 'LegacyOptionsOp', vars: ['page']},
  {op: 'RequestObjectOp', vars: ['prompt']}
]);
`)
}

func TestBrowserSniffReferencePrimaryInterceptorCapturesRequestBodies(t *testing.T) {
	t.Parallel()

	snippet := extractBrowserUsePrimaryCaptureSnippet(t)
	runNodeSnippet(t, snippet+`
await window.fetch(new Request('https://example.com/api', {
  method: 'POST',
  body: JSON.stringify({prompt: 'request object'}),
  headers: {'content-type': 'application/json'}
}));
await window.fetch('https://example.com/legacy', {
  method: 'POST',
  body: JSON.stringify({prompt: 'legacy options'})
});
await new Promise(resolve => setTimeout(resolve, 0));
assertEqual(window.__capture_bodies['POST https://example.com/api'].request_body, '{"prompt":"request object"}');
assertEqual(window.__capture_bodies['POST https://example.com/api'].response_body, '{"ok":true}');
assertEqual(window.__capture_bodies['POST https://example.com/api'].response_status, 200);
assertEqual(window.__capture_bodies['POST https://example.com/api'].response_content_type, 'application/json');
assertEqual(window.__capture_bodies['POST https://example.com/legacy'].request_body, '{"prompt":"legacy options"}');
assertEqual(window.__capture_bodies['POST https://example.com/legacy'].response_body, '{"ok":true}');
`)
}

func TestBrowserSniffReferenceChromeMCPInterceptorCapturesRequestBodies(t *testing.T) {
	t.Parallel()

	snippet := extractChromeMCPFetchSnippet(t)
	runNodeSnippet(t, snippet+`
await window.fetch(new Request('https://example.com/api', {
  method: 'POST',
  body: JSON.stringify({prompt: 'request object'}),
  headers: {'content-type': 'application/json'}
}));
await window.fetch('https://example.com/legacy', {
  method: 'POST',
  body: JSON.stringify({prompt: 'legacy options'})
});
await new Promise(resolve => setTimeout(resolve, 0));
assertEqual(window.__capture_bodies['POST https://example.com/api'].request_body, '{"prompt":"request object"}');
assertEqual(window.__capture_bodies['POST https://example.com/api'].response_body, '{"ok":true}');
assertEqual(window.__capture_bodies['POST https://example.com/legacy'].request_body, '{"prompt":"legacy options"}');
assertEqual(window.__capture_bodies['POST https://example.com/legacy'].response_body, '{"ok":true}');
`)
}

func extractBrowserUsePrimaryCaptureSnippet(t *testing.T) string {
	t.Helper()

	content := readBrowserSniffReference(t)
	var matches []string
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `browser-use eval "window.__capture_bodies={};`) {
			continue
		}
		snippet, err := strconv.Unquote(strings.TrimPrefix(line, "browser-use eval "))
		require.NoError(t, err)
		matches = append(matches, snippet)
	}

	require.Len(t, matches, 1, "browser-use primary body capture snippet should have one canonical copy")
	return matches[0]
}

func extractBrowserUseGraphQLSnippet(t *testing.T) string {
	t.Helper()

	content := readBrowserSniffReference(t)
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `browser-use eval "window.__gqlOps=[];`) {
			continue
		}
		snippet, err := strconv.Unquote(strings.TrimPrefix(line, "browser-use eval "))
		require.NoError(t, err)
		return snippet
	}

	t.Fatal("browser-use GraphQL interceptor snippet not found")
	return ""
}

func extractChromeMCPFetchSnippet(t *testing.T) string {
	t.Helper()

	content := readBrowserSniffReference(t)
	marker := "// Run via mcp__claude-in-chrome__javascript_tool after navigating to <site>"
	markerIndex := strings.Index(content, marker)
	require.NotEqual(t, -1, markerIndex, "chrome-MCP interceptor snippet marker not found")

	codeStart := strings.LastIndex(content[:markerIndex], "```javascript")
	require.NotEqual(t, -1, codeStart, "chrome-MCP javascript fence start not found")
	codeStart += len("```javascript")
	codeEnd := strings.Index(content[markerIndex:], "\n```")
	require.NotEqual(t, -1, codeEnd, "chrome-MCP javascript fence end not found")

	return strings.TrimSpace(content[codeStart : markerIndex+codeEnd])
}

func readBrowserSniffReference(t *testing.T) string {
	t.Helper()

	path := filepath.Join("..", "..", "skills", "printing-press", "references", "browser-sniff-capture.md")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func runNodeSnippet(t *testing.T, snippet string) {
	t.Helper()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required to execute browser-sniff reference snippets")
	}

	script := `
globalThis.window = globalThis;
window.fetch = async function() {
  return new Response('{"ok":true}', {status: 200, headers: {'content-type': 'application/json'}});
};
function assertEqual(actual, expected) {
  const actualJSON = JSON.stringify(actual);
  const expectedJSON = JSON.stringify(expected);
  if (actualJSON !== expectedJSON) {
    throw new Error("expected " + expectedJSON + ", got " + actualJSON);
  }
}
` + snippet

	cmd := exec.Command("node", "--input-type=module", "-e", script)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assert.NotContains(t, string(out), "Error")
}

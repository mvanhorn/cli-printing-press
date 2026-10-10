package generator

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedQueryStringUsesPercent20 proves a printed CLI sends spaces in
// query strings as %20 and a literal plus as %2B, while a form body still
// uses application/x-www-form-urlencoded (+ for spaces).
func TestGeneratedQueryStringUsesPercent20(t *testing.T) {
	t.Parallel()

	type captured struct {
		rawQuery    string
		body        string
		contentType string
	}
	seen := make(chan captured, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		seen <- captured{
			rawQuery:    r.URL.RawQuery,
			body:        string(body),
			contentType: r.Header.Get("Content-Type"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("qspace")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.BaseURL = server.URL
	apiSpec.Resources = map[string]spec.Resource{
		"records": {
			Description: "Records",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/records",
					Description: "List records",
					Params: []spec.Param{
						{Name: "filter", Type: "string", Description: "Filter expression"},
					},
				},
			},
		},
		"notes": {
			Description: "Notes",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:             "POST",
					Path:               "/notes",
					Description:        "Create a note",
					RequestContentType: "application/x-www-form-urlencoded",
					Body: []spec.Param{
						{Name: "note", Type: "string", Description: "Note text"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.NotContains(t, clientSrc, "req.URL.RawQuery = q.Encode()")
	require.Contains(t, clientSrc, "req.URL.RawQuery = cliutil.EncodeQuery(q)")
	require.Contains(t, clientSrc, "encoded := cliutil.EncodeQuery(params)")
	require.Contains(t, clientSrc, "body.Fields.Encode()")
	require.NotContains(t, clientSrc, "cliutil.EncodeQuery(body.Fields)")
	require.Contains(t, clientSrc, `key += "|query=" + query.Encode()`)

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	home := t.TempDir()
	env := isolatedQueryTestEnv(home)
	wait := func() captured {
		t.Helper()
		select {
		case got := <-seen:
			return got
		case <-time.After(15 * time.Second):
			t.Fatal("timed out waiting for the generated CLI to reach the test server")
			return captured{}
		}
	}
	_, _, err := runGeneratedBinaryEnv(t, binaryPath, env, "records", "--filter", `Title="Lab Probe Net"`, "--json")
	require.NoError(t, err)
	space := wait()
	require.Contains(t, space.rawQuery, `filter=Title%3D%22Lab%20Probe%20Net%22`)
	require.NotContains(t, space.rawQuery, "+")
	require.Empty(t, space.body)

	_, _, err = runGeneratedBinaryEnv(t, binaryPath, env, "records", "--filter", "a+b", "--json")
	require.NoError(t, err)
	plus := wait()
	require.Equal(t, "filter=a%2Bb", plus.rawQuery)

	_, _, err = runGeneratedBinaryEnv(t, binaryPath, env, "notes", "--note", "Lab Probe", "--json")
	require.NoError(t, err)
	form := wait()
	require.Contains(t, form.contentType, "application/x-www-form-urlencoded")
	require.Equal(t, "note=Lab+Probe", form.body)
	require.NotContains(t, form.body, "%20")

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestGetWithHeadersValuesPreservesRepeatedQueryParams", "-count=1")
	runGoCommand(t, outputDir, "test", "./internal/cliutil", "-run", "TestEncodeQuery", "-count=1")
}

// TestGeneratedCodeOrchQueryStringUsesPercent20 proves a write-method query
// built by codeOrchSplitQuery reaches the server with %20, because that path
// is appended to the URL and is not re-encoded by the JSON body call.
func TestGeneratedCodeOrchQueryStringUsesPercent20(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("qorch")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Disabled = true
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources = map[string]spec.Resource{
		"notes": {
			Description: "Notes",
			Endpoints: map[string]spec.Endpoint{
				"update": {
					Method:      "PUT",
					Path:        "/notes/{id}",
					Description: "Update a note",
					Params: []spec.Param{
						{Name: "id", Type: "string", Positional: true, PathParam: true, Description: "Note ID"},
						{Name: "note", In: "query", Type: "string", Description: "Note query"},
					},
					Body: []spec.Param{
						{Name: "text", Type: "string", Description: "Note body"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Contains(t, src, "cliutil.EncodeQuery(uv)")
	require.NotContains(t, src, "uv.Encode()")

	runtimeTest := `package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestCodeOrchQuerySpaceEncoding(t *testing.T) {
	var rawQuery, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		body = string(payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer server.Close()
	t.Setenv("QORCH_BASE_URL", server.URL)
	// Blank overrides an inherited PRINTING_PRESS_VERIFY=1. That mode skips
	// this PUT before it reaches the test server.
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")

	request := mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "notes.update",
		"params": map[string]any{
			"id": "1", "note": "Lab Probe+Net", "text": "body text",
		},
	}}}
	result, err := handleCodeOrchExecute(context.Background(), request)
	if err != nil || result.IsError {
		t.Fatalf("code orchestration request failed: result=%#v err=%v", result, err)
	}
	if rawQuery != "note=Lab%20Probe%2BNet" {
		t.Fatalf("raw query = %q, want note=Lab%%20Probe%%2BNet", rawQuery)
	}
	if !strings.Contains(body, "\"text\":\"body text\"") {
		t.Fatalf("JSON body = %q, want the text field", body)
	}
	if strings.Contains(body, "note=") || strings.Contains(body, "%20") {
		t.Fatalf("query encoding leaked into body: %q", body)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_query_space_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandWithEnv(t, outputDir, clearedHarnessEnv(), "test", "./internal/mcp", "-run", "TestCodeOrchQuerySpaceEncoding", "-count=1")
}

// clearedHarnessEnv blanks Printing Press harness variables. Callers append
// this slice to os.Environ(); omitting a key leaves an inherited
// PRINTING_PRESS_VERIFY=1 in force, and mutating requests then return
// without contacting the test server.
func clearedHarnessEnv() []string {
	return []string{
		"PRINTING_PRESS_VERIFY=",
		"PRINTING_PRESS_VERIFY_LIVE_HTTP=",
		"PRINTING_PRESS_DOGFOOD=",
	}
}

func isolatedQueryTestEnv(home string) []string {
	env := make([]string, 0, len(os.Environ())+9)
	for _, entry := range os.Environ() {
		switch {
		case strings.HasPrefix(entry, "HOME="),
			strings.HasPrefix(entry, "USERPROFILE="),
			strings.HasPrefix(entry, "XDG_"),
			strings.HasPrefix(entry, "PRINTING_PRESS_VERIFY="),
			strings.HasPrefix(entry, "PRINTING_PRESS_VERIFY_LIVE_HTTP="),
			strings.HasPrefix(entry, "PRINTING_PRESS_DOGFOOD="):
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
	)
	return append(env, clearedHarnessEnv()...)
}

func TestQueryEncodingChildEnvClearsInheritedHarnessVars(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "1")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "1")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "1")

	home := t.TempDir()
	script := `printf '%s|%s|%s|%s' "$PRINTING_PRESS_VERIFY" "$PRINTING_PRESS_VERIFY_LIVE_HTTP" "$PRINTING_PRESS_DOGFOOD" "$HOME"`

	isolated := exec.Command("sh", "-c", script)
	isolated.Env = append(os.Environ(), isolatedQueryTestEnv(home)...)
	out, err := isolated.Output()
	require.NoError(t, err)
	require.Equal(t, "|||"+home, string(out))

	cleared := exec.Command("sh", "-c", script)
	cleared.Env = append(os.Environ(), clearedHarnessEnv()...)
	out, err = cleared.Output()
	require.NoError(t, err)
	parts := strings.Split(string(out), "|")
	require.Len(t, parts, 4)
	require.Empty(t, parts[0])
	require.Empty(t, parts[1])
	require.Empty(t, parts[2])
}

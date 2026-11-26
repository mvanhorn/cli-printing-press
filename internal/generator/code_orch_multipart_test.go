package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// A multipart/form-data endpoint reached through the code-orchestration
// surface must upload its file params as file parts instead of sending a JSON
// body the endpoint rejects.
func TestCodeOrchestrationSendsMultipartUploads(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("corchupload")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources["packages"] = spec.Resource{
		Description: "Packages",
		Endpoints: map[string]spec.Endpoint{
			"import": {
				Method:             "POST",
				Path:               "/packages/import",
				Description:        "Import a package file",
				RequestContentType: "multipart/form-data",
				Body: []spec.Param{
					{Name: "package", Type: "string", Format: "binary", Required: true, Description: "Package file"},
					{Name: "attachment", BodyName: "file", Type: "string", Format: "binary", Description: "Extra file under a renamed wire field"},
					{Name: "projectId", Type: "string", Description: "Target project"},
				},
			},
			"replace": {
				Method:             "PUT",
				Path:               "/packages/replace",
				Description:        "Replace a package file",
				RequestContentType: "multipart/form-data",
				Body: []spec.Param{
					{Name: "payload", Type: "string", Format: "binary", Required: true, Description: "Replacement file"},
					{Name: "note", Type: "string", Description: "Replacement note"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "corchupload-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Regexp(t, `Multipart:\s+true,\s+FileParams:\s+\[\]string\{"package", "file"\}`, src,
		"multipart endpoint must mark its binary body param as a file part")
	require.Regexp(t, `ID:\s+"packages.replace",\s+Method:\s+"PUT",[\s\S]*?FileParams:\s+\[\]string\{"payload"\}`, src,
		"PUT multipart endpoint must upload its file param")
	require.Regexp(t, `if ep\.Multipart \{\s+out\["multipart"\] = map\[string\]any\{\s+"file_params":\s*ep\.FileParams,\s+"file_value":\s*"base64-encoded file content",`, src,
		"endpoint metadata must name multipart file params and say their values are base64 content")
	require.Contains(t, src, `"filename_param":`,
		"endpoint metadata must advertise the optional <param>_filename companion")

	runtimeTest := `package mcp

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestCodeOrchestrationSendsMultipartUploadsRuntime(t *testing.T) {
	var gotMethod, gotPath, gotType, gotFile, gotRenamed, gotField, gotPayload, gotNote, gotFileName, gotCompanion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotType = r.Header.Get("Content-Type")
		gotFile, gotRenamed, gotField, gotPayload, gotNote, gotFileName, gotCompanion = "", "", "", "", "", "", ""
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		gotField = r.FormValue("projectId")
		gotNote = r.FormValue("note")
		gotCompanion = r.FormValue("package_filename")
		if f, fh, err := r.FormFile("package"); err == nil {
			b, _ := io.ReadAll(f)
			gotFile = string(b)
			gotFileName = fh.Filename
		}
		if f, _, err := r.FormFile("file"); err == nil {
			b, _ := io.ReadAll(f)
			gotRenamed = string(b)
		}
		if f, _, err := r.FormFile("payload"); err == nil {
			b, _ := io.ReadAll(f)
			gotPayload = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer server.Close()
	t.Setenv("CORCHUPLOAD_BASE_URL", server.URL)

	// Over MCP a file param must carry base64-encoded content, not a local
	// path: this is the server-side confinement that blocks arbitrary file
	// reads. The server still receives the decoded bytes.
	enc := base64.StdEncoding.EncodeToString([]byte("PACKAGE-BYTES"))
	result, err := handleCodeOrchExecute(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "packages.import",
		"confirm":     true,
		"params":      map[string]any{"package": enc, "file": enc, "projectId": "p1", "package_filename": "archive.zip"},
	}}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("upload failed: result=%#v err=%v", result, err)
	}
	if gotMethod != http.MethodPost || !strings.HasPrefix(gotType, "multipart/form-data") {
		t.Fatalf("import hit %s content type %q", gotMethod, gotType)
	}
	if gotFile != "PACKAGE-BYTES" || gotRenamed != "PACKAGE-BYTES" || gotField != "p1" {
		t.Fatalf("server saw package=%q file=%q projectId=%q", gotFile, gotRenamed, gotField)
	}
	if gotFileName != "archive.zip" {
		t.Fatalf("package upload filename = %q, want archive.zip", gotFileName)
	}
	if gotCompanion != "" {
		t.Fatalf("package_filename leaked as a form field: %q", gotCompanion)
	}

	importMeta := codeOrchEndpointMetadata(findCodeOrchEndpoint("packages.import"))
	mp, _ := importMeta["multipart"].(map[string]any)
	files, _ := mp["file_params"].([]string)
	if mp["file_value"] != "base64-encoded file content" || len(files) != 2 || files[0] != "package" || files[1] != "file" {
		t.Fatalf("import metadata multipart = %#v", importMeta["multipart"])
	}

	result, err = handleCodeOrchExecute(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "packages.replace",
		"confirm":     true,
		"params":      map[string]any{"payload": enc, "note": "n1"},
	}}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("put upload failed: result=%#v err=%v", result, err)
	}
	if gotMethod != http.MethodPut || gotPath != "/packages/replace" || !strings.HasPrefix(gotType, "multipart/form-data") {
		t.Fatalf("put hit %s %s content type %q", gotMethod, gotPath, gotType)
	}
	if gotPayload != "PACKAGE-BYTES" || gotNote != "n1" {
		t.Fatalf("put saw payload=%q note=%q", gotPayload, gotNote)
	}
	replaceMeta := codeOrchEndpointMetadata(findCodeOrchEndpoint("packages.replace"))
	rp, _ := replaceMeta["multipart"].(map[string]any)
	replaceFiles, _ := rp["file_params"].([]string)
	if rp["file_value"] != "base64-encoded file content" || len(replaceFiles) != 1 || replaceFiles[0] != "payload" {
		t.Fatalf("replace metadata multipart = %#v", replaceMeta["multipart"])
	}
}

func TestReplayCarriedMultipartLimit(t *testing.T) {
	fields := map[string]string{}
	body := map[string]any{}
	binding := []mcpParamBinding{{PublicName: "per-page", WireName: "per_page", Location: "body"}}
	replayCarriedPageLimit(binding, map[string]string{}, body, "per_page", 20, true, fields)
	if fields["per_page"] != "20" {
		t.Fatalf("multipart limit = %#v", fields)
	}
	if _, ok := body["per_page"]; ok {
		t.Fatal("multipart limit was written as JSON")
	}
	fields["per_page"] = "5"
	replayCarriedPageLimit(binding, map[string]string{}, body, "per_page", 20, true, fields)
	if fields["per_page"] != "5" {
		t.Fatalf("explicit multipart limit = %#v", fields["per_page"])
	}
	jsonBody := map[string]any{}
	replayCarriedPageLimit(binding, map[string]string{}, jsonBody, "per_page", 20, false, map[string]string{})
	if jsonBody["per_page"] != 20 {
		t.Fatalf("non-multipart body limit = %#v", jsonBody["per_page"])
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_multipart_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^(TestCodeOrchestrationSendsMultipartUploadsRuntime|TestReplayCarriedMultipartLimit)$", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

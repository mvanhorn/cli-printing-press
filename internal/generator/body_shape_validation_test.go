package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedBodyFlagsRejectWrongJSONShapesBeforeTransport(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bodyshapeflag")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:             "POST",
					Path:               "/items",
					Description:        "Create item",
					RequestContentType: "application/json",
					Body: []spec.Param{
						{Name: "metadata", Type: "object"},
						{Name: "tags", Type: "array"},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	runGoCommand(t, outputDir, "mod", "tidy")

	behaviorTest := `package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBodyFlagsRejectWrongJSONShapesBeforeTransport(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "object rejects scalar",
			args: []string{"--metadata", "207769210"},
			want: "--metadata must be a JSON object",
		},
		{
			name: "array rejects object",
			args: []string{"--tags", "{}"},
			want: "--tags must be a JSON array",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			t.Setenv("BODYSHAPEFLAG_BASE_URL", server.URL)
			flags := &rootFlags{asJSON: true}
			cmd := newRootCmd(flags)
			cmd.SetArgs(append([]string{"items", "create"}, tc.args...))
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
			if calls != 0 {
				t.Fatalf("server called %d time(s), want 0", calls)
			}
		})
	}
}

func TestBodyFlagsAcceptMatchingJSONShapes(t *testing.T) {
	var calls int
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer server.Close()

	t.Setenv("BODYSHAPEFLAG_BASE_URL", server.URL)
	flags := &rootFlags{asJSON: true}
	cmd := newRootCmd(flags)
	cmd.SetArgs([]string{
		"items", "create",
		"--metadata", "{\"source\":\"test\"}",
		"--tags", "[\"blue\"]",
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("server called %d time(s), want 1", calls)
	}
	if metadata, ok := gotBody["metadata"].(map[string]any); !ok || metadata["source"] != "test" {
		t.Fatalf("metadata body = %#v, want object with source=test", gotBody["metadata"])
	}
	if tags, ok := gotBody["tags"].([]any); !ok || len(tags) != 1 || tags[0] != "blue" {
		t.Fatalf("tags body = %#v, want [blue]", gotBody["tags"])
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "body_shape_validation_test.go"), []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestBodyFlags", "-count=1")
}

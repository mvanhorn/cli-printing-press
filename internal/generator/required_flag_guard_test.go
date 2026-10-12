package generator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlagRequiredUnsatisfiedExpr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    spec.Param
		want string
	}{
		{
			name: "required string",
			p:    spec.Param{Name: "instance-key", Type: "string", Required: true},
			want: `!cmd.Flags().Changed("instance-key") && flagInstanceKey == "" && !requiredInputWaived()`,
		},
		{
			name: "required string with alias",
			p: spec.Param{
				Name:     "s",
				FlagName: "address",
				Aliases:  []string{"s"},
				Type:     "string",
				Required: true,
			},
			want: `!(cmd.Flags().Changed("address") || cmd.Flags().Changed("s")) && flagS == "" && !requiredInputWaived()`,
		},
		{
			name: "required integer",
			p:    spec.Param{Name: "year", Type: "integer", Required: true},
			want: `!cmd.Flags().Changed("year") && flagYear == 0 && !requiredInputWaived()`,
		},
		{
			name: "required bool uses string zero",
			p:    spec.Param{Name: "enabled", Type: "boolean", Required: true},
			want: `!cmd.Flags().Changed("enabled") && flagEnabled == "" && !requiredInputWaived()`,
		},
		{
			name: "global-scope string keeps env-default value check",
			p: spec.Param{
				Name:        "TenantFilter",
				Type:        "string",
				Required:    true,
				GlobalScope: true,
			},
			want: `!cmd.Flags().Changed("tenant-filter") && flagTenantFilter == "" && !requiredInputWaived()`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, flagRequiredUnsatisfiedExpr(tt.p))
		})
	}
}

func TestBodyRequiredChecksHonorResolvedValue(t *testing.T) {
	t.Parallel()

	got := bodyRequiredChecks(spec.Endpoint{
		Body: []spec.Param{
			{Name: "name", Type: "string", Required: true},
			{Name: "visibility", Type: "string", Required: true},
			{Name: "filter", Type: "array", Required: true},
			{Name: "store_code", FlagName: "store-code", Aliases: []string{"code"}, Type: "string", Required: true},
		},
	}, "\t\t\t")
	assert.Contains(t, got, `!cmd.Flags().Changed("name") && bodyName == "" && !requiredInputWaived()`)
	assert.Contains(t, got, `!cmd.Flags().Changed("visibility") && bodyVisibility == "" && !requiredInputWaived()`)
	assert.Contains(t, got, `!cmd.Flags().Changed("filter") && bodyFilter == "" && !requiredInputWaived()`)
	assert.Contains(t, got, `!(cmd.Flags().Changed("store-code") || cmd.Flags().Changed("code")) && bodyStoreCode == "" && !requiredInputWaived()`)
}

func TestGeneratedOutput_RequiredFlagGuardHonorsResolvedValue(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "reqguard",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Learn:   spec.LearnConfig{Disabled: true},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/reqguard-pp-cli/config.toml"},
		Types: map[string]spec.TypeDef{
			"Tenant": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "name", Type: "string"},
				},
			},
		},
		Resources: map[string]spec.Resource{
			"tenants": {
				Description: "Tenant records",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/tenants",
						Description: "List tenants",
						Response:    spec.ResponseDef{Type: "array", Item: "Tenant"},
						Params: []spec.Param{
							{
								Name:        "instance-key",
								Type:        "string",
								Required:    true,
								Description: "Tenant instance key",
							},
							{
								Name:        "label",
								Type:        "string",
								Description: "Optional label",
							},
						},
					},
					"get": {
						Method:      "GET",
						Path:        "/tenants/{id}",
						Description: "Get tenant",
						Response:    spec.ResponseDef{Type: "object", Item: "Tenant"},
						Params: []spec.Param{{
							Name:       "id",
							Type:       "string",
							Required:   true,
							Positional: true,
						}},
					},
				},
			},
			"whoami": {
				Description: "Current tenant",
				Endpoints: map[string]spec.Endpoint{
					"get": {
						Method:      "GET",
						Path:        "/whoami",
						Description: "Current tenant identity",
						Response:    spec.ResponseDef{Type: "object", Item: "Tenant"},
						Params: []spec.Param{
							{
								Name:        "instance-key",
								Type:        "string",
								Required:    true,
								Description: "Tenant instance key",
							},
							{
								Name:        "label",
								Type:        "string",
								Description: "Optional label",
							},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "reqguard-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	guard := `!cmd.Flags().Changed("instance-key") && flagInstanceKey == "" && !requiredInputWaived()`
	endpointSrc := readGeneratedFile(t, outputDir, "internal", "cli", "tenants_list.go")
	assert.Contains(t, endpointSrc, guard)
	assert.Contains(t, endpointSrc, `return fmt.Errorf("required flag \"%s\" not set", "instance-key")`)
	assert.NotContains(t, endpointSrc, `return fmt.Errorf("required flag \"%s\" not set", "label")`)
	assert.NotContains(t, endpointSrc, `.MarkFlagRequired("`)

	promotedSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_whoami.go")
	assert.Contains(t, promotedSrc, guard)
	assert.Contains(t, promotedSrc, `return fmt.Errorf("required flag \"%s\" not set", "instance-key")`)
	assert.NotContains(t, promotedSrc, `return fmt.Errorf("required flag \"%s\" not set", "label")`)

	requireGeneratedCompiles(t, outputDir)

	inlineTest := `package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRequiredFlagGuardResolvedAndMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("instance-key"); got != "tenant-a" {
			t.Fatalf("instance-key query = %q, want tenant-a", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/whoami" {
			_, _ = w.Write([]byte(` + "`" + `{"id":"1","name":"Ada"}` + "`" + `))
			return
		}
		_, _ = w.Write([]byte(` + "`" + `[{"id":"1","name":"Ada"}]` + "`" + `))
	}))
	t.Cleanup(server.Close)
	t.Setenv("REQGUARD_BASE_URL", server.URL)

	cases := []struct {
		name     string
		path     []string
		args     []string
		setValue bool
		value    string
		wantErr  string
	}{
		{name: "endpoint missing", path: []string{"tenants", "list"}, args: []string{"tenants", "list", "--label", "x", "--json"}, wantErr: ` + "`" + `required flag "instance-key" not set` + "`" + `},
		{name: "promoted missing", path: []string{"whoami"}, args: []string{"whoami", "--label", "x", "--json"}, wantErr: ` + "`" + `required flag "instance-key" not set` + "`" + `},
		{name: "endpoint resolved", path: []string{"tenants", "list"}, args: []string{"tenants", "list", "--label", "x", "--json"}, setValue: true, value: "tenant-a"},
		{name: "promoted resolved", path: []string{"whoami"}, args: []string{"whoami", "--label", "x", "--json"}, setValue: true, value: "tenant-a"},
		{name: "endpoint empty resolve", path: []string{"tenants", "list"}, args: []string{"tenants", "list", "--label", "x", "--json"}, setValue: true, value: "", wantErr: ` + "`" + `required flag "instance-key" not set` + "`" + `},
		{name: "endpoint cli flag", path: []string{"tenants", "list"}, args: []string{"tenants", "list", "--instance-key", "tenant-a", "--label", "x", "--json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := RootCmd()
			if tc.setValue {
				if err := attachResolvedFlag(root, tc.path, "instance-key", tc.value); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(tc.args)
			err := root.Execute()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("execute: %v; stderr=%s stdout=%s", err, stderr.String(), stdout.String())
				}
				return
			}
			if err == nil {
				t.Fatalf("expected %q, got success; stdout=%s", tc.wantErr, stdout.String())
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func attachResolvedFlag(root *cobra.Command, path []string, name, value string) error {
	cmd, _, err := root.Find(path)
	if err != nil {
		return err
	}
	cmd.PreRunE = func(c *cobra.Command, _ []string) error {
		f := c.Flags().Lookup(name)
		if f == nil {
			return fmt.Errorf("flag %q not found", name)
		}
		if err := f.Value.Set(value); err != nil {
			return err
		}
		if f.Changed {
			return fmt.Errorf("Value.Set unexpectedly set Changed on %q", name)
		}
		return nil
	}
	return nil
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "required_flag_guard_runtime_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestRequiredFlagGuardResolvedAndMissing", "-count=1")
}

func TestGeneratedOutput_DryRunEnforcesRequiredInput(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	apiSpec := &spec.APISpec{
		Name:    "dryreq",
		Version: "0.1.0",
		BaseURL: server.URL,
		Auth:    spec.AuthConfig{Type: "none"},
		Learn:   spec.LearnConfig{Disabled: true},
		Config:  spec.ConfigSpec{Format: "toml", Path: "~/.config/dryreq-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"tenants": {
				Description: "Tenant records",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/tenants",
						Description: "List tenants",
						Params: []spec.Param{
							{Name: "instance-key", Type: "string", Required: true, Description: "Tenant instance key"},
							{Name: "label", Type: "string", Description: "Optional label"},
						},
					},
					"get": {
						Method:      "GET",
						Path:        "/tenants/{id}",
						Description: "Get a tenant",
						Params: []spec.Param{
							{Name: "id", Type: "string", Required: true, Positional: true, Description: "Tenant ID"},
						},
					},
				},
			},
			"notes": {
				Description: "Notes",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/notes",
						Description: "List notes",
					},
					"create": {
						Method:      "POST",
						Path:        "/notes",
						Description: "Create a note",
						Body: []spec.Param{
							{Name: "name", Type: "string", Required: true, Description: "Note name"},
							{Name: "note", Type: "string", Description: "Optional note"},
						},
					},
				},
			},
			"uploads": {
				Description: "Uploads",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/uploads",
						Description: "List uploads",
					},
					"create": {
						Method:             "POST",
						Path:               "/uploads",
						Description:        "Upload raw bytes",
						RequestContentType: "application/octet-stream",
						BodyRequired:       true,
						Params: []spec.Param{
							{Name: "upload-id", Type: "string", Required: true, Description: "Upload ID"},
						},
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	uploadSrc := readGeneratedFile(t, outputDir, "internal", "cli", "uploads_create.go")
	assert.Contains(t, uploadSrc, `if rawBodyFile == "" && !stdinBody && !requiredInputWaived() {`)
	assert.Contains(t, readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go"), "func requiredInputWaived() bool")

	bin := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", bin, "./cmd/"+naming.CLI(apiSpec.Name))

	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte{}, 0o644))
	base := harnessFreeEnv("DRYREQ_BASE_URL=" + server.URL)
	prefix := []string{"--config", configPath}

	const missingKey = `required flag "instance-key" not set`
	code, out := runPrintedCLI(t, bin, base, append(prefix, "tenants", "list", "--label", "x", "--dry-run")...)
	require.Equal(t, 2, code, out)
	require.Contains(t, out, missingKey)
	require.Equal(t, int32(0), hits.Load())

	code, out = runPrintedCLI(t, bin, base, append(prefix, "tenants", "list", "--label", "x")...)
	require.Equal(t, 2, code, out)
	require.Contains(t, out, missingKey)

	code, out = runPrintedCLI(t, bin, base, append(prefix, "tenants", "list", "--dry-run")...)
	require.Equal(t, 2, code, out)
	require.Contains(t, out, missingKey)
	require.NotContains(t, out, "dry_run")

	code, out = runPrintedCLI(t, bin, base, append(prefix, "tenants", "list", "--instance-key", "tenant-a", "--dry-run")...)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "dry_run")
	require.Equal(t, int32(0), hits.Load(), "a complete --dry-run must not send the request")

	code, out = runPrintedCLI(t, bin, withEnv(base, "PRINTING_PRESS_VERIFY=1"), append(prefix, "tenants", "list", "--dry-run")...)
	require.Equal(t, 0, code, out)
	require.Equal(t, int32(0), hits.Load())

	code, out = runPrintedCLI(t, bin, withEnv(base, "PRINTING_PRESS_DOGFOOD=1"), append(prefix, "tenants", "list", "--dry-run")...)
	require.Equal(t, 0, code, out)
	require.Equal(t, int32(0), hits.Load())

	const missingName = `required flag "name" not set`
	code, out = runPrintedCLI(t, bin, base, append(prefix, "notes", "create", "--note", "extra", "--dry-run")...)
	require.Equal(t, 2, code, out)
	require.Contains(t, out, missingName)
	require.Equal(t, int32(0), hits.Load())

	code, out = runPrintedCLI(t, bin, base, append(prefix, "notes", "create", "--note", "extra")...)
	require.Equal(t, 2, code, out)
	require.Contains(t, out, missingName)

	code, out = runPrintedCLI(t, bin, base, append(prefix, "notes", "create", "--name", "Ada", "--dry-run")...)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "dry_run")
	require.Equal(t, int32(0), hits.Load(), "a complete create --dry-run must not send the request")

	code, out = runPrintedCLI(t, bin, withEnv(base, "PRINTING_PRESS_VERIFY=1"), append(prefix, "notes", "create", "--dry-run")...)
	require.Equal(t, 0, code, out)
	require.Equal(t, int32(0), hits.Load())

	const missingRaw = "raw request body required: pass --file <path> or --stdin"
	code, out = runPrintedCLI(t, bin, base, append(prefix, "uploads", "create", "--upload-id", "u1", "--dry-run")...)
	require.Contains(t, out, missingRaw)
	codeWithout, outWithout := runPrintedCLI(t, bin, base, append(prefix, "uploads", "create", "--upload-id", "u1")...)
	require.Equal(t, codeWithout, code, "dry-run raw-body error code = %d (%s), live = %d (%s)", code, out, codeWithout, outWithout)
	require.Contains(t, outWithout, missingRaw)
	require.Equal(t, int32(0), hits.Load())

	bodyFile := filepath.Join(t.TempDir(), "body.bin")
	require.NoError(t, os.WriteFile(bodyFile, []byte("hi"), 0o644))
	code, out = runPrintedCLI(t, bin, base, append(prefix, "uploads", "create", "--upload-id", "u1", "--file", bodyFile, "--dry-run")...)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "dry_run")
	require.Equal(t, int32(0), hits.Load())
}

func harnessFreeEnv(extra ...string) []string {
	skip := map[string]struct{}{
		"PRINTING_PRESS_VERIFY":           {},
		"PRINTING_PRESS_VERIFY_LIVE_HTTP": {},
		"PRINTING_PRESS_DOGFOOD":          {},
	}
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := skip[key]; ok {
			continue
		}
		out = append(out, entry)
	}
	return append(out, extra...)
}

func withEnv(base []string, extra ...string) []string {
	out := make([]string, len(base), len(base)+len(extra))
	copy(out, base)
	return append(out, extra...)
}

func runPrintedCLI(t *testing.T, bin string, env []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "args %v output:\n%s", args, out)
	return exitErr.ExitCode(), string(out)
}

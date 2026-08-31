package generator

import (
	"os"
	"path/filepath"
	"testing"

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
			want: `!cmd.Flags().Changed("instance-key") && flagInstanceKey == "" && !flags.dryRun`,
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
			want: `!(cmd.Flags().Changed("address") || cmd.Flags().Changed("s")) && flagS == "" && !flags.dryRun`,
		},
		{
			name: "required integer",
			p:    spec.Param{Name: "year", Type: "integer", Required: true},
			want: `!cmd.Flags().Changed("year") && flagYear == 0 && !flags.dryRun`,
		},
		{
			name: "required bool uses string zero",
			p:    spec.Param{Name: "enabled", Type: "boolean", Required: true},
			want: `!cmd.Flags().Changed("enabled") && flagEnabled == "" && !flags.dryRun`,
		},
		{
			name: "global-scope string keeps env-default value check",
			p: spec.Param{
				Name:        "TenantFilter",
				Type:        "string",
				Required:    true,
				GlobalScope: true,
			},
			want: `!cmd.Flags().Changed("tenant-filter") && flagTenantFilter == "" && !flags.dryRun`,
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
	assert.Contains(t, got, `!cmd.Flags().Changed("name") && bodyName == "" && !flags.dryRun`)
	assert.Contains(t, got, `!cmd.Flags().Changed("visibility") && bodyVisibility == "" && !flags.dryRun`)
	assert.Contains(t, got, `!cmd.Flags().Changed("filter") && bodyFilter == "" && !flags.dryRun`)
	assert.Contains(t, got, `!(cmd.Flags().Changed("store-code") || cmd.Flags().Changed("code")) && bodyStoreCode == "" && !flags.dryRun`)
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

	guard := `!cmd.Flags().Changed("instance-key") && flagInstanceKey == "" && !flags.dryRun`
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

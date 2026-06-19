package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParamIsConstDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		p    spec.Param
		want bool
	}{
		{
			name: "single-value enum with matching string default",
			p:    spec.Param{Name: "Command", Type: "string", Enum: []string{"namecheap.domains.getList"}, Default: "namecheap.domains.getList"},
			want: true,
		},
		{
			name: "single-value enum with matching integer default",
			p:    spec.Param{Name: "version", Type: "integer", Enum: []string{"2"}, Default: 2},
			want: true,
		},
		{
			name: "single-value enum with matching boolean default",
			p:    spec.Param{Name: "strict", Type: "boolean", Enum: []string{"true"}, Default: true},
			want: true,
		},
		{
			name: "single-value enum with matching float default preserving fractional form",
			p:    spec.Param{Name: "version", Type: "number", Enum: []string{"2.0"}, Default: float64(2.0)},
			want: true,
		},
		{
			name: "single-value enum with matching float default carrying real fraction",
			p:    spec.Param{Name: "ratio", Type: "number", Enum: []string{"1.5"}, Default: float64(1.5)},
			want: true,
		},
		{
			name: "single-value float enum that does not match the default",
			p:    spec.Param{Name: "ratio", Type: "number", Enum: []string{"1.5"}, Default: float64(2.5)},
			want: false,
		},
		{
			name: "single-value enum with mismatched default is not const",
			p:    spec.Param{Name: "Command", Type: "string", Enum: []string{"a"}, Default: "b"},
			want: false,
		},
		{
			name: "multi-value enum with matching default is not const",
			p:    spec.Param{Name: "order", Type: "string", Enum: []string{"asc", "desc"}, Default: "asc"},
			want: false,
		},
		{
			name: "single-value enum with no default is not const",
			p:    spec.Param{Name: "Command", Type: "string", Enum: []string{"namecheap.x"}},
			want: false,
		},
		{
			name: "no enum but default is not const",
			p:    spec.Param{Name: "limit", Type: "integer", Default: 25},
			want: false,
		},
		{
			name: "no enum and no default is not const",
			p:    spec.Param{Name: "name", Type: "string"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, paramIsConstDefault(tt.p))
		})
	}
}

func TestDefaultValDropsEmptyDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    spec.Param
		want string
	}{
		{
			name: "array empty string default",
			p:    spec.Param{Name: "classification", Type: "array", Default: ""},
			want: `""`,
		},
		{
			name: "string empty default",
			p:    spec.Param{Name: "keyword", Type: "string", Default: ""},
			want: `""`,
		},
		{
			name: "string enum prose default dropped",
			p: spec.Param{
				Name:    "includeTBA",
				Type:    "string",
				Default: "no if date parameter sent, yes otherwise",
				Enum:    []string{"yes", " no", " only"},
			},
			want: `""`,
		},
		{
			name: "string enum trimmed member default kept",
			p: spec.Param{
				Name:    "includeTBA",
				Type:    "string",
				Default: "no",
				Enum:    []string{"yes", " no", " only"},
			},
			want: `"no"`,
		},
		{
			name: "string enum padded default trimmed to match validator",
			p: spec.Param{
				Name:    "includeTBA",
				Type:    "string",
				Default: " no",
				Enum:    []string{"yes", " no", " only"},
			},
			want: `"no"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, defaultVal(tt.p))
		})
	}
}

// TestGenerateMarksConstDefaultFlagsHidden is the end-to-end check that
// when an endpoint Param has a single-value enum whose only value equals
// the default, the generated command registers the flag (so the wire-side
// default still flows) but marks it hidden so --help does not list a flag
// whose only valid value is the default. This is the single-URL routing
// API shape, where an operation is selected via a fixed query param.
func TestGenerateMarksConstDefaultFlagsHidden(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("const-default")
	constParam := spec.Param{
		Name:        "Command",
		Type:        "string",
		Description: "Constant operation selector",
		Required:    true,
		Enum:        []string{"namecheap.domains.getList"},
		Default:     "namecheap.domains.getList",
	}
	multiEnumParam := spec.Param{
		Name:        "SortBy",
		Type:        "string",
		Description: "Sort field",
		Enum:        []string{"name", "created"},
		Default:     "name",
	}

	apiSpec.Resources["domains"] = spec.Resource{
		Description: "Manage domains",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/",
				Description: "List domains",
				Params:      []spec.Param{constParam, multiEnumParam},
			},
			"renew": {
				Method:      "POST",
				Path:        "/",
				Description: "Renew a domain",
				Params:      []spec.Param{constParam},
			},
		},
	}
	apiSpec.Resources["dns"] = spec.Resource{
		Description: "Manage DNS",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:      "GET",
				Path:        "/",
				Description: "List DNS records",
				Params:      []spec.Param{constParam},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "const-default-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliDir := filepath.Join(outputDir, "internal", "cli")

	endpointSrc, err := os.ReadFile(filepath.Join(cliDir, "domains_list.go"))
	require.NoError(t, err)
	endpointGot := string(endpointSrc)

	require.Contains(t, endpointGot, `cmd.Flags().StringVar(&flagCommand, "command",`,
		"per-endpoint template: const-default flag must still be registered so its default is sent on the wire")
	require.Contains(t, endpointGot, `_ = cmd.Flags().MarkHidden("command")`,
		"per-endpoint template: const-default flag must be marked hidden")
	require.Contains(t, endpointGot, `cmd.Flags().StringVar(&flagSortBy, "sort-by",`,
		"per-endpoint template: multi-value-enum flag must remain visible")
	require.NotContains(t, endpointGot, `_ = cmd.Flags().MarkHidden("sort-by")`,
		"per-endpoint template: multi-value-enum flag must not be marked hidden")
	// A required flag with a wired default never needs a runtime "required flag not set"
	// check — the template gates that emission on `(not .Default)`. Lock that interaction
	// so a future template edit re-enabling the check for Required:true would not produce
	// a generated binary that errors at runtime on a hidden const-default flag.
	require.NotContains(t, endpointGot, `required flag "command" not set`,
		"per-endpoint template: required-check must be suppressed for const-default (Default is set)")

	promotedSrc, err := os.ReadFile(filepath.Join(cliDir, "promoted_dns.go"))
	require.NoError(t, err)
	promotedGot := string(promotedSrc)

	require.Contains(t, promotedGot, `cmd.Flags().StringVar(&flagCommand, "command",`,
		"promoted template: const-default flag must still be registered so its default is sent on the wire")
	require.Contains(t, promotedGot, `_ = cmd.Flags().MarkHidden("command")`,
		"promoted template: const-default flag must be marked hidden")
}

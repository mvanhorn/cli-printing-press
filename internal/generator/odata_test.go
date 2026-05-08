package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateODataFunctionURLComposerBehavior(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "odata-pp-cli")
	gen := New(odataGeneratorSpec(), outputDir)
	require.NoError(t, gen.Generate())

	behaviorTest := `package cli

import "testing"

func TestODataFunctionCallFormatting(t *testing.T) {
	tests := []struct {
		name string
		path string
		params []odataFunctionParam
		want string
	}{
		{
			name: "single string param",
			path: "/CurrentPeriod",
			params: []odataFunctionParam{{Name: "ModuleCode", Value: "OE", Type: "string", Set: true}},
			want: "/CurrentPeriod(ModuleCode='OE')",
		},
		{
			name: "multiple mixed params",
			path: "/INInvoice('477')/Print",
			params: []odataFunctionParam{
				{Name: "Destination", Value: "Disk File", Type: "string", Set: true},
				{Name: "Copies", Value: 2, Type: "integer", Set: true},
				{Name: "Preview", Value: true, Type: "boolean", Set: true},
			},
			want: "/INInvoice('477')/Print(Destination='Disk%20File',Copies=2,Preview=true)",
		},
		{
			name: "strings with special chars and apostrophes",
			path: "/Reports/Print",
			params: []odataFunctionParam{{Name: "FileName", Value: "Bob's A/B, C.pdf", Type: "string", Set: true}},
			want: "/Reports/Print(FileName='Bob%27%27s%20A%2FB%2C%20C.pdf')",
		},
		{
			name: "guid param is not quoted",
			path: "/Things/Lookup",
			params: []odataFunctionParam{{Name: "RequestID", Value: "550e8400-e29b-41d4-a716-446655440000", Type: "string", Format: "uuid", Set: true}},
			want: "/Things/Lookup(RequestID=550e8400-e29b-41d4-a716-446655440000)",
		},
		{
			name: "guid keyed parent keeps generated call params on final segment",
			path: "/Things(550e8400-e29b-41d4-a716-446655440000)/Lookup",
			params: []odataFunctionParam{{Name: "Mode", Value: "full", Type: "string", Set: true}},
			want: "/Things(550e8400-e29b-41d4-a716-446655440000)/Lookup(Mode='full')",
		},
		{
			name: "empty call still gets parentheses",
			path: "/DefaultLocationCode",
			params: nil,
			want: "/DefaultLocationCode()",
		},
		{
			name: "path ending with empty call receives generated params",
			path: "/CurrentPeriod()",
			params: []odataFunctionParam{{Name: "ModuleCode", Value: "AR", Type: "string", Set: true}},
			want: "/CurrentPeriod(ModuleCode='AR')",
		},
	}
	for _, tt := range tests {
		if got := appendODataFunctionCall(tt.path, tt.params); got != tt.want {
			t.Fatalf("%s: got %s want %s", tt.name, got, tt.want)
		}
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "odata_behavior_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(behaviorTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestODataFunctionCallFormatting")
}

func TestGenerateODataActionFlagsBecomeJSONBody(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "odata-pp-cli")
	gen := New(odataGeneratorSpec(), outputDir)
	require.NoError(t, gen.Generate())

	data, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "promoted_orders.go"))
	require.NoError(t, err)
	source := string(data)

	assert.Contains(t, source, `cmd.Flags().StringVar(&bodyDocumentDate, "document-date", "", "Document date")`)
	assert.Contains(t, source, `cmd.Flags().IntVar(&bodyPeriodID, "period-id", 0, "Period id")`)
	assert.Contains(t, source, `cmd.Flags().StringVar(&bodyLines, "lines", "", "Lines")`)
	assert.Contains(t, source, `cmd.Flags().StringVar(&bodyOptions, "options", "", "Options")`)
	assert.Contains(t, source, `if !cmd.Flags().Changed("document-date") && !flags.dryRun {`)
	assert.NotContains(t, source, `if !cmd.Flags().Changed("period-id") && !flags.dryRun {`)
	assert.Contains(t, source, `if cmd.Flags().Changed("period-id") || bodyPeriodID != 0 {`)
	assert.Contains(t, source, `body["PeriodID"] = bodyPeriodID`)
	assert.Contains(t, source, `var parsedLines any`)
	assert.Contains(t, source, `var parsedOptions any`)
	assert.NotContains(t, source, `params["PeriodID"]`)
}

func TestGenerateODataQueryOptionsOnReadCommands(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "odata-pp-cli")
	gen := New(odataGeneratorSpec(), outputDir)
	require.NoError(t, gen.Generate())

	data, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "customers_list.go"))
	require.NoError(t, err)
	source := string(data)

	assert.Contains(t, source, `cmd.Flags().IntVar(&flagODataTop, "top", 0, "OData $top query option")`)
	assert.Contains(t, source, `cmd.Flags().StringVar(&flagODataFilter, "filter", "", "OData $filter query option")`)
	assert.Contains(t, source, `cmd.Flags().BoolVar(&flagODataCount, "count", false, "OData $count query option")`)
	assert.Contains(t, source, `params["$top"] = fmt.Sprintf("%v", flagODataTop)`)
	assert.Contains(t, source, `params["$filter"] = fmt.Sprintf("%v", flagODataFilter)`)
	assert.Contains(t, source, `params["$select"] = flags.selectFields`)

	functionData, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "promoted_periods.go"))
	require.NoError(t, err)
	functionSource := string(functionData)
	assert.NotContains(t, functionSource, `"top"`)
	assert.Contains(t, functionSource, `appendODataFunctionCall`)
	assert.False(t, strings.Contains(functionSource, `params["ModuleCode"]`), "function params must not be serialized as query params")
}

func TestGenerateODataStoreSubcommandsUnwrapResponseBeforeSelect(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "odata-store-pp-cli")
	gen := New(odataGeneratorSpec(), outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	data, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "customers_list.go"))
	require.NoError(t, err)
	source := string(data)

	assert.Contains(t, source, `data = extractResponseData(data)`)
	assert.Contains(t, source, `countItems = []json.RawMessage{data}`)
	assert.Contains(t, source, `if flags.csv {`)
	assert.Contains(t, source, `return printOutputWithFlags(cmd.OutOrStdout(), data, flags)`)
}

func TestGenerateNonODataStoreSubcommandsDoNotUseODataEnvelopeBypass(t *testing.T) {
	apiSpec := odataGeneratorSpec()
	apiSpec.Name = "plain"
	apiSpec.BaseURL = "https://api.example.com/v1"
	apiSpec.OData = false

	outputDir := filepath.Join(t.TempDir(), "plain-store-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	data, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "customers_list.go"))
	require.NoError(t, err)
	source := string(data)

	assert.NotContains(t, source, `data = extractResponseData(data)`)
	assert.NotContains(t, source, `countItems = []json.RawMessage{data}`)
}

func odataGeneratorSpec() *spec.APISpec {
	return &spec.APISpec{
		Name:    "odata",
		Version: "1.0.0",
		BaseURL: "https://api.example.com/odata4",
		OData:   true,
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/odata-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"customers": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/Customers",
						Description: "List customers",
					},
					"get": {
						Method:      "GET",
						Path:        "/Customers('{CustomerCode}')",
						Description: "Get customer",
						Params: []spec.Param{
							{Name: "CustomerCode", Type: "string", Required: true, Positional: true, Description: "Customer code"},
						},
					},
				},
			},
			"orders": {
				Endpoints: map[string]spec.Endpoint{
					"create": {
						Method:         "POST",
						Path:           "/OEOrder('{DocumentID}')/GenerateInvoice",
						Description:    "OEOrder/GenerateInvoice is an ODATA action - use POST to call",
						ODataOperation: spec.ODataOperationAction,
						Params: []spec.Param{
							{Name: "DocumentID", Type: "integer", Required: true, Positional: true, Description: "Document id"},
							{Name: "DocumentDate", Type: "string", Required: true, Description: "Document date"},
							{Name: "PeriodID", Type: "integer", Description: "Period id"},
							{Name: "Lines", Type: "array", Description: "Lines"},
							{Name: "Options", Type: "object", Description: "Options"},
						},
					},
				},
			},
			"periods": {
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/CurrentPeriod",
						Description:    "CurrentPeriod is an ODATA function - use GET to call",
						ODataOperation: spec.ODataOperationFunction,
						Params: []spec.Param{
							{Name: "ModuleCode", Type: "string", Required: true, Description: "Module code"},
						},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{},
	}
}

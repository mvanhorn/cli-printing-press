package generator

import (
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestCodeOrchRoutesQueryParamsOnWriteMethods guards the write-method
// query/body split. Spec-declared in:query params on POST/PUT/PATCH must be
// emitted into codeOrchEndpoint.QueryParams and routed to the URL query
// string by codeOrchSplitQuery — never dumped into the JSON body.
//
// Regression guard for the latent defect found alongside the write-body fix:
// before this, handleCodeOrchExecute only built a query map for GET/DELETE,
// so a write endpoint's in:query param silently ended up in the body and the
// API ignored or rejected it.
func TestCodeOrchRoutesQueryParamsOnWriteMethods(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("qsplit")
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources["ledger"] = spec.Resource{
		Description: "Ledger",
		Endpoints: map[string]spec.Endpoint{
			"voucher-update": {
				Method:      "PUT",
				Path:        "/ledger/voucher/{id}",
				Description: "Update a voucher; sendToLedger is an in-query flag",
				Params: []spec.Param{
					{Name: "sendToLedger", Type: "string", Description: "Post to ledger immediately (query)"},
				},
				Body: []spec.Param{
					{Name: "voucherDescription", Type: "string", Description: "Voucher text (body)"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "qsplit-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")

	// Struct carries the new field.
	require.Regexp(t, `QueryParams\s+\[\]string`, src,
		"codeOrchEndpoint must declare a QueryParams field")
	// The PUT endpoint emits its in:query param into QueryParams.
	require.Regexp(t, `QueryParams:\s*\[\]string\{\s*"sendToLedger"\s*,?\s*\}`, src,
		"PUT endpoint must list sendToLedger in QueryParams")
	// A body param must never be classified as a query param.
	require.NotRegexp(t, `QueryParams:\s*\[\]string\{[^}]*voucherDescription[^}]*\}`, src,
		"body param must not appear inside any QueryParams literal")
	// The split helper exists and is wired into the write path.
	require.Contains(t, src, "func codeOrchSplitQuery(",
		"codeOrchSplitQuery helper must be emitted")
	require.Contains(t, src, "codeOrchSplitQuery(ep.QueryParams, params)",
		"the write branch must route query params via codeOrchSplitQuery")
}

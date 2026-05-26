package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndpointClientSideFilters(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("filterdocs")
	apiSpec.SpecSource = "docs"
	apiSpec.Types = map[string]spec.TypeDef{
		"FundingRate": {
			Fields: []spec.TypeField{
				{Name: "symbol", Type: "string"},
				{Name: "query", Type: "string"},
				{Name: "rate", Type: "string"},
			},
		},
	}
	endpoint := spec.Endpoint{
		Method: "GET",
		Path:   "/funding_rate/batch",
		Params: []spec.Param{
			{Name: "symbols", Type: "string"},
			{Name: "limit", Type: "integer"},
			{Name: "query", Type: "string"},
		},
		Response: spec.ResponseDef{Type: "array", Item: "FundingRate"},
	}

	filters := endpointClientSideFilters(apiSpec, endpoint)
	require.Len(t, filters, 1)
	assert.Equal(t, "symbols", filters[0].Param.Name)
	assert.Equal(t, "symbol", filters[0].Field)

	apiSpec.SpecSource = "official"
	assert.Empty(t, endpointClientSideFilters(apiSpec, endpoint), "official specs should keep trusting server-side filter semantics")

	apiSpec.SpecSource = "docs"
	endpoint.Pagination = &spec.Pagination{Type: "cursor"}
	assert.Empty(t, endpointClientSideFilters(apiSpec, endpoint), "paged endpoints cannot be safely narrowed after fetching one page")

	endpoint.Pagination = nil
	endpoint.Response.Type = "object"
	assert.Empty(t, endpointClientSideFilters(apiSpec, endpoint), "object responses should not use list filtering")

	endpoint.Response.Type = "array"
	endpoint.Path = "/funding_rates"
	assert.Empty(t, endpointClientSideFilters(apiSpec, endpoint), "ordinary docs-derived list endpoints should keep trusting server-side filters")
}

func TestEndpointClientSideFiltersTriesLaterCandidatesAfterSeenField(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("filterdocs")
	apiSpec.SpecSource = "docs"
	apiSpec.Types = map[string]spec.TypeDef{
		"CurrencyRate": {
			Fields: []spec.TypeField{
				{Name: "currencies", Type: "string"},
				{Name: "currency", Type: "string"},
			},
		},
	}
	endpoint := spec.Endpoint{
		Method: "GET",
		Path:   "/currency/batch",
		Params: []spec.Param{
			{Name: "currency_list", URLName: "currencies", Type: "string"},
			{Name: "currencies", Type: "string"},
		},
		Response: spec.ResponseDef{Type: "array", Item: "CurrencyRate"},
	}

	filters := endpointClientSideFilters(apiSpec, endpoint)
	require.Len(t, filters, 2)
	assert.Equal(t, "currency_list", filters[0].Param.Name)
	assert.Equal(t, "currencies", filters[0].Field)
	assert.Equal(t, "currencies", filters[1].Param.Name)
	assert.Equal(t, "currency", filters[1].Field)
}

func TestSingularClientSideFilterNameLeavesBareSuffixes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ses", singularClientSideFilterName("ses"))
	assert.Equal(t, "class", singularClientSideFilterName("classes"))
}

func TestGeneratedDocsClientSideFilterNarrowsIgnoredBatchParam(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("filterdocs")
	apiSpec.SpecSource = "docs"
	apiSpec.Types = map[string]spec.TypeDef{
		"FundingRate": {
			Fields: []spec.TypeField{
				{Name: "symbol", Type: "string"},
				{Name: "rate", Type: "string"},
			},
		},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"market": {
			Description: "Market data",
			Endpoints: map[string]spec.Endpoint{
				"get-funding-rate-batch": {
					Method:      "GET",
					Path:        "/api/v1/futures/market/funding_rate/batch",
					Description: "Get all funding rates",
					Params: []spec.Param{{
						Name:        "symbols",
						Type:        "string",
						Description: "Comma-separated symbols",
					}, {
						Name:        "limit",
						Type:        "integer",
						Description: "Maximum rows",
					}},
					Response: spec.ResponseDef{Type: "array", Item: "FundingRate"},
				},
				"get-funding-rate": {
					Method:      "GET",
					Path:        "/api/v1/futures/market/funding_rate",
					Description: "Get one funding rate",
					Params: []spec.Param{{
						Name:        "symbol",
						Type:        "string",
						Required:    true,
						Positional:  true,
						Description: "Symbol",
					}},
					Response: spec.ResponseDef{Type: "object", Item: "FundingRate"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "filterdocs-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	inlineTest := `package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDocsClientSideFilterNarrowsIgnoredBatchParam(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("symbols"); got != "BTCUSDT" {
			t.Fatalf("symbols query = %q, want BTCUSDT", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"code":0,"data":[{"symbol":"BTCUSDT","rate":"0.01"},{"symbol":"ETHUSDT","rate":"0.02"}],"msg":"success"}` + "`" + `))
	}))
	defer server.Close()
	t.Setenv("FILTERDOCS_BASE_URL", server.URL)

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"market", "get-funding-rate-batch", "--symbols", "BTCUSDT", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute command: %v; stderr=%s", err, stderr.String())
	}

	var payload struct {
		Results struct {
			Data []struct {
				Symbol string ` + "`" + `json:"symbol"` + "`" + `
			} ` + "`" + `json:"data"` + "`" + `
		} ` + "`" + `json:"results"` + "`" + `
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("parse output: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(payload.Results.Data) != 1 || payload.Results.Data[0].Symbol != "BTCUSDT" {
		t.Fatalf("filtered data = %+v, want only BTCUSDT; stdout=%s", payload.Results.Data, stdout.String())
	}
}

func TestDocsClientSideFilterRunsBeforeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("symbols"); got != "BTCUSDT" {
			t.Fatalf("symbols query = %q, want BTCUSDT", got)
		}
		if got := r.URL.Query().Get("limit"); got != "1" {
			t.Fatalf("limit query = %q, want 1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `[{"symbol":"ETHUSDT","rate":"0.02"},{"symbol":"BTCUSDT","rate":"0.01"}]` + "`" + `))
	}))
	defer server.Close()
	t.Setenv("FILTERDOCS_BASE_URL", server.URL)

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"market", "get-funding-rate-batch", "--symbols", "BTCUSDT", "--limit", "1", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute command: %v; stderr=%s", err, stderr.String())
	}

	var payload struct {
		Results []struct {
			Symbol string ` + "`" + `json:"symbol"` + "`" + `
		} ` + "`" + `json:"results"` + "`" + `
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("parse output: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(payload.Results) != 1 || payload.Results[0].Symbol != "BTCUSDT" {
		t.Fatalf("filtered data = %+v, want only BTCUSDT; stdout=%s", payload.Results, stdout.String())
	}
}

func TestDocsClientSideFilterFailsOpenWhenResponseFieldIsAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("symbols"); got != "BTCUSDT" {
			t.Fatalf("symbols query = %q, want BTCUSDT", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"data":[{"pair":"BTCUSDT","rate":"0.01"},{"pair":"ETHUSDT","rate":"0.02"}]}` + "`" + `))
	}))
	defer server.Close()
	t.Setenv("FILTERDOCS_BASE_URL", server.URL)

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"market", "get-funding-rate-batch", "--symbols", "BTCUSDT", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute command: %v; stderr=%s", err, stderr.String())
	}

	var payload struct {
		Results []struct {
			Pair string ` + "`" + `json:"pair"` + "`" + `
		} ` + "`" + `json:"results"` + "`" + `
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("parse output: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(payload.Results) != 2 {
		t.Fatalf("filtered data = %+v, want original two rows; stdout=%s", payload.Results, stdout.String())
	}
}

func TestDocsClientSideFilterKeepsRowsWithMissingField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("symbols"); got != "BTCUSDT" {
			t.Fatalf("symbols query = %q, want BTCUSDT", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `[{"symbol":"BTCUSDT","rate":"0.01"},{"pair":"legacy","rate":"0.02"},{"symbol":"ETHUSDT","rate":"0.03"}]` + "`" + `))
	}))
	defer server.Close()
	t.Setenv("FILTERDOCS_BASE_URL", server.URL)

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"market", "get-funding-rate-batch", "--symbols", "BTCUSDT", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute command: %v; stderr=%s", err, stderr.String())
	}

	var payload struct {
		Results []map[string]string ` + "`" + `json:"results"` + "`" + `
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("parse output: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(payload.Results) != 2 {
		t.Fatalf("filtered data = %+v, want matching row plus row without symbol; stdout=%s", payload.Results, stdout.String())
	}
	if payload.Results[0]["symbol"] != "BTCUSDT" || payload.Results[1]["pair"] != "legacy" {
		t.Fatalf("filtered data = %+v, want BTCUSDT plus legacy row; stdout=%s", payload.Results, stdout.String())
	}
}
	`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "docs_client_side_filter_test.go"), []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestDocsClientSideFilter", "-count=1")
}

func TestGeneratedPromotedDocsClientSideFilterRunsBeforeLimit(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("promotedfilterdocs")
	apiSpec.SpecSource = "docs"
	apiSpec.Types = map[string]spec.TypeDef{
		"FundingRate": {
			Fields: []spec.TypeField{
				{Name: "symbol", Type: "string"},
				{Name: "rate", Type: "string"},
			},
		},
	}
	apiSpec.Resources = map[string]spec.Resource{
		"market": {
			Description: "Market data",
			Endpoints: map[string]spec.Endpoint{
				"get-funding-rate-batch": {
					Method:      "GET",
					Path:        "/api/v1/futures/market/funding_rate/batch",
					Description: "Get all funding rates",
					Params: []spec.Param{{
						Name:        "symbols",
						Type:        "string",
						Description: "Comma-separated symbols",
					}, {
						Name:        "limit",
						Type:        "integer",
						Description: "Maximum rows",
					}},
					Response: spec.ResponseDef{Type: "array", Item: "FundingRate"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "promotedfilterdocs-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	inlineTest := `package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPromotedDocsClientSideFilterRunsBeforeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("symbols"); got != "BTCUSDT" {
			t.Fatalf("symbols query = %q, want BTCUSDT", got)
		}
		if got := r.URL.Query().Get("limit"); got != "1" {
			t.Fatalf("limit query = %q, want 1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `[{"symbol":"ETHUSDT","rate":"0.02"},{"symbol":"BTCUSDT","rate":"0.01"}]` + "`" + `))
	}))
	defer server.Close()
	t.Setenv("PROMOTEDFILTERDOCS_BASE_URL", server.URL)

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"market", "--symbols", "BTCUSDT", "--limit", "1", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute command: %v; stderr=%s", err, stderr.String())
	}

	var payload struct {
		Results []struct {
			Symbol string ` + "`" + `json:"symbol"` + "`" + `
		} ` + "`" + `json:"results"` + "`" + `
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("parse output: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if len(payload.Results) != 1 || payload.Results[0].Symbol != "BTCUSDT" {
		t.Fatalf("filtered data = %+v, want only BTCUSDT; stdout=%s", payload.Results, stdout.String())
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "promoted_docs_client_side_filter_test.go"), []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestPromotedDocsClientSideFilterRunsBeforeLimit", "-count=1")
}

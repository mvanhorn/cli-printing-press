package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectODataUsesBasePathAndRawMetadata(t *testing.T) {
	assert.True(t, DetectOData(&APISpec{BasePath: "/odata/v4"}, nil))
	assert.True(t, DetectOData(&APISpec{BaseURL: "https://api.example.com/odata/v4"}, nil))
	assert.True(t, DetectOData(&APISpec{}, []byte(`{"properties":{"@odata.context":{"type":"string"}}}`)))
	assert.False(t, DetectOData(&APISpec{BasePath: "/api/v1"}, nil))
}

func TestApplyODataConventionsMarksFunctionsAndMovesActionParamsToBody(t *testing.T) {
	api := &APISpec{
		OData: true,
		Resources: map[string]Resource{
			"invoice": {
				Endpoints: map[string]Endpoint{
					"print": {
						Method:      "GET",
						Path:        "/INInvoice('{DocumentID}')/Print",
						Description: "INInvoice/Print is an ODATA function and Action - use either GET or POST to call",
						Params: []Param{
							{Name: "DocumentID", Type: "integer", Positional: true},
							{Name: "Destination", Type: "string"},
						},
					},
					"create": {
						Method:      "POST",
						Path:        "/OEOrder('{DocumentID}')/GenerateInvoice",
						Description: "OEOrder/GenerateInvoice is an ODATA action - use POST to call",
						Params: []Param{
							{Name: "DocumentID", Type: "integer", Positional: true},
							{Name: "DocumentDate", Type: "string"},
							{Name: "PeriodID", Type: "integer"},
						},
					},
				},
			},
		},
	}

	ApplyODataConventions(api)

	print := api.Resources["invoice"].Endpoints["print"]
	require.Equal(t, ODataOperationFunction, print.ODataOperation)
	assert.Len(t, print.Params, 2, "function flags stay as params so the URL composer can consume them")

	action := api.Resources["invoice"].Endpoints["create"]
	require.Equal(t, ODataOperationAction, action.ODataOperation)
	require.Len(t, action.Params, 1)
	assert.Equal(t, "DocumentID", action.Params[0].Name)
	assert.Equal(t, []Param{
		{Name: "DocumentDate", Type: "string"},
		{Name: "PeriodID", Type: "integer"},
	}, action.Body)
}

func TestApplyODataConventionsAddsReadQueryOptionsOnlyToListAndGet(t *testing.T) {
	api := &APISpec{
		OData: true,
		Resources: map[string]Resource{
			"customers": {
				Endpoints: map[string]Endpoint{
					"list": {Method: "GET", Path: "/Customers"},
					"get": {
						Method: "GET",
						Path:   "/Customers('{CustomerCode}')",
						Params: []Param{{Name: "CustomerCode", Type: "string", Positional: true}},
					},
					"current": {
						Method:         "GET",
						Path:           "/CurrentPeriod",
						Description:    "CurrentPeriod is an ODATA function - use GET to call",
						ODataOperation: ODataOperationFunction,
					},
					"create": {Method: "POST", Path: "/Customers"},
				},
			},
		},
	}

	ApplyODataConventions(api)

	list := api.Resources["customers"].Endpoints["list"]
	assert.Contains(t, paramFlagNames(list.Params), "top")
	assert.Contains(t, paramFlagNames(list.Params), "filter")
	assert.Contains(t, paramFlagNames(list.Params), "orderby")
	assert.Contains(t, paramWireNames(list.Params), "$top")
	assert.Contains(t, paramWireNames(list.Params), "$filter")

	get := api.Resources["customers"].Endpoints["get"]
	assert.Contains(t, paramFlagNames(get.Params), "expand")

	current := api.Resources["customers"].Endpoints["current"]
	assert.NotContains(t, paramFlagNames(current.Params), "top")

	create := api.Resources["customers"].Endpoints["create"]
	assert.Empty(t, create.Params)
}

func TestApplyODataConventionsInfersPrintBinaryContentTypes(t *testing.T) {
	api := &APISpec{
		OData: true,
		Resources: map[string]Resource{
			"invoices": {
				Endpoints: map[string]Endpoint{
					"print": {
						Method:      "GET",
						Path:        "/INInvoice('{DocumentID}')/Print",
						Description: "INInvoice/Print is an ODATA function - use GET to call",
						Params: []Param{
							{Name: "DocumentID", Type: "integer", Positional: true},
							{Name: "Format", Type: "string", Enum: []string{"Adobe PDF", "CSV File", "Excel XLSX File"}},
						},
					},
					"plain": {
						Method: "GET",
						Path:   "/INInvoice",
					},
				},
			},
		},
	}

	ApplyODataConventions(api)

	print := api.Resources["invoices"].Endpoints["print"]
	assert.Equal(t, []string{
		"application/pdf",
		"text/csv",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	}, print.ResponseContentTypes)
	assert.True(t, print.HasBinaryResponse())

	plain := api.Resources["invoices"].Endpoints["plain"]
	assert.Empty(t, plain.ResponseContentTypes)
	assert.False(t, plain.HasBinaryResponse())
}

func paramFlagNames(params []Param) []string {
	out := make([]string, 0, len(params))
	for _, p := range params {
		out = append(out, p.PublicInputName())
	}
	return out
}

func paramWireNames(params []Param) []string {
	out := make([]string, 0, len(params))
	for _, p := range params {
		out = append(out, p.Name)
	}
	return out
}

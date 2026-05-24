package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicParamAuditJSONInventoriesDecisionRequiredParams(t *testing.T) {
	specPath := writePublicParamAuditSpec(t, `
name: public-param-test
description: Public param test
version: "0.1.0"
base_url: https://api.example.com
auth:
  type: none
resources:
  stores:
    description: Store operations
    endpoints:
      find:
        method: GET
        path: /stores
        description: Find nearby stores
        params:
          - name: s
            type: string
            required: true
            description: Street address
          - name: city
            type: string
            required: true
            description: City name
        response:
          type: array
          item: Store
types:
  Store:
    fields:
      - name: id
        type: string
`)

	var out bytes.Buffer
	cmd := newPublicParamAuditCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--spec", specPath, "--json"})
	require.NoError(t, cmd.Execute())

	var ledger pipeline.PublicParamAuditLedger
	require.NoError(t, json.Unmarshal(out.Bytes(), &ledger))
	assert.Equal(t, pipeline.PublicParamAuditSummary{Total: 1, Pending: 1}, ledger.Summary)
	require.Len(t, ledger.Findings, 1)
	assert.Equal(t, "stores.find.params.s", ledger.Findings[0].ID)
	assert.Equal(t, "Street address", ledger.Findings[0].Description)
}

func TestPublicParamAuditStrictFailsOnlyForUnreviewedCandidates(t *testing.T) {
	specPath := writePublicParamAuditSpec(t, `
name: public-param-test
description: Public param test
version: "0.1.0"
base_url: https://api.example.com
auth:
  type: none
resources:
  stores:
    description: Store operations
    endpoints:
      find:
        method: GET
        path: /stores
        description: Find nearby stores
        params:
          - name: s
            type: string
            required: true
            description: Street address
        response:
          type: array
          item: Store
types:
  Store:
    fields:
      - name: id
        type: string
`)

	cmd := newPublicParamAuditCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--spec", specPath, "--strict"})
	err := cmd.Execute()
	require.Error(t, err)
	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitGenerationError, exitErr.Code)

	ledgerPath := filepath.Join(t.TempDir(), "public-param-audit.json")
	initial := pipeline.NewPublicParamAuditLedger([]pipeline.PublicParamAuditFinding{{
		ID:             "stores.find.params.s",
		Decision:       pipeline.PublicParamDecisionSkip,
		SourceEvidence: "The API docs describe s as the public search syntax.",
		SkipReason:     "The one-letter token is documented as user-authored query syntax, not a hidden abbreviation.",
	}})
	data, marshalErr := json.Marshal(initial)
	require.NoError(t, marshalErr)
	require.NoError(t, os.WriteFile(ledgerPath, data, 0o644))

	cmd = newPublicParamAuditCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--spec", specPath, "--ledger", ledgerPath, "--strict"})
	require.NoError(t, cmd.Execute())
}

func TestPublicParamAuditStrictPassesWhenFlagNameIsInSpec(t *testing.T) {
	specPath := writePublicParamAuditSpec(t, `
name: public-param-test
description: Public param test
version: "0.1.0"
base_url: https://api.example.com
auth:
  type: none
resources:
  stores:
    description: Store operations
    endpoints:
      find:
        method: GET
        path: /stores
        description: Find nearby stores
        params:
          - name: s
            flag_name: street
            aliases: [s]
            type: string
            required: true
            description: Street address
        response:
          type: array
          item: Store
types:
  Store:
    fields:
      - name: id
        type: string
`)

	cmd := newPublicParamAuditCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--spec", specPath, "--strict"})
	require.NoError(t, cmd.Execute())
}

func TestPublicParamAuditOpenAPIURLNameOverrideInventoriesWireName(t *testing.T) {
	specPath := writePublicParamAuditSpec(t, `
openapi: 3.0.3
info:
  title: Public Param URL Name API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /records:
    get:
      operationId: queryRecords
      x-param-url-names:
        limit: "$limit"
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
      responses:
        "200":
          description: ok
`)

	var out bytes.Buffer
	cmd := newPublicParamAuditCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--spec", specPath, "--json"})
	require.NoError(t, cmd.Execute())

	var ledger pipeline.PublicParamAuditLedger
	require.NoError(t, json.Unmarshal(out.Bytes(), &ledger))
	require.Len(t, ledger.Findings, 1)
	assert.Equal(t, pipeline.PublicParamAuditSummary{Total: 1, Resolved: 1}, ledger.Summary)
	assert.Equal(t, "records.query.params.$limit", ledger.Findings[0].ID)
	assert.Equal(t, "$limit", ledger.Findings[0].WireName)
	assert.Equal(t, "limit", ledger.Findings[0].CurrentPublicName)
	assert.Equal(t, []string{"operator-like-wire-name"}, ledger.Findings[0].Reasons)
}

func TestPublicParamAuditStrictRefsDisablesLenientLocalSchemaStubs(t *testing.T) {
	specPath := writePublicParamAuditSpec(t, `
openapi: 3.0.3
info:
  title: Public Param Missing Ref API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /stores:
    get:
      operationId: findStores
      parameters:
        - name: s
          in: query
          schema:
            type: string
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/MissingStore"
components:
  schemas: {}
`)

	cmd := newPublicParamAuditCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--spec", specPath, "--lenient"})
	require.NoError(t, cmd.Execute())

	cmd = newPublicParamAuditCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--spec", specPath, "--lenient", "--strict-refs"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing local schema refs: MissingStore")
}

func writePublicParamAuditSpec(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

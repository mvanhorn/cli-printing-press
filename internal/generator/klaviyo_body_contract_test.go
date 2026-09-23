package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/stretchr/testify/require"
)

func TestGenerateKlaviyoStableCampaignBodyContracts(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`
openapi: 3.0.2
info:
  title: Klaviyo API
  version: 2026-07-15
servers:
  - url: https://a.klaviyo.com
paths:
  /api/campaigns:
    post:
      operationId: create_campaign
      summary: Create Campaign
      requestBody:
        required: true
        content:
          application/vnd.api+json:
            schema:
              $ref: '#/components/schemas/CampaignCreateQuery'
      responses:
        '201':
          description: Created
  /api/campaign-message-assign-template:
    post:
      operationId: assign_template_to_campaign_message
      summary: Assign Template to Campaign Message
      requestBody:
        required: true
        content:
          application/vnd.api+json:
            schema:
              $ref: '#/components/schemas/CampaignMessageAssignTemplateQuery'
      responses:
        '200':
          description: Assigned
components:
  schemas:
    CampaignCreateQuery:
      type: object
      required: [data]
      properties:
        data:
          type: object
          required: [type, attributes]
          properties:
            type:
              type: string
              enum: [campaign]
            attributes:
              type: object
              required: [name, audiences, campaign-messages]
              properties:
                name:
                  type: string
                audiences:
                  type: object
                  required: [included]
                  properties:
                    included:
                      type: array
                      items: {type: string}
                    excluded:
                      type: array
                      items: {type: string}
                campaign-messages:
                  type: object
                  required: [data]
                  properties:
                    data:
                      type: array
                      items:
                        type: object
                        required: [type, attributes]
                        properties:
                          type: {type: string, enum: [campaign-message]}
                          attributes:
                            type: object
                            required: [definition]
                            properties:
                              definition:
                                type: object
                                required: [channel]
                                properties:
                                  channel: {type: string, enum: [email]}
                                  label: {type: string}
                                  content:
                                    type: object
                                    properties:
                                      subject: {type: string}
                                      preview_text: {type: string}
                                      from_email: {type: string}
                                      from_label: {type: string}
                                      reply_to_email: {type: string}
    CampaignMessageAssignTemplateQuery:
      type: object
      required: [data]
      properties:
        data:
          type: object
          required: [type, id, relationships]
          properties:
            type: {type: string, enum: [campaign-message]}
            id: {type: string}
            relationships:
              type: object
              required: [template]
              properties:
                template:
                  type: object
                  required: [data]
                  properties:
                    data:
                      type: object
                      required: [type, id]
                      properties:
                        type: {type: string, enum: [template]}
                        id: {type: string}
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), "klaviyo-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	campaign := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_campaigns.go")
	require.Contains(t, campaign, `"data-attributes-audiences"`)
	require.Contains(t, campaign, `"data-attributes-campaign-messages"`)
	require.Contains(t, campaign, `nestedDataAttributes["audiences"] = asMap`)
	require.Contains(t, campaign, `nestedDataAttributes["campaign-messages"] = asMap`)

	assignment := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_campaign-message-assign-template.go")
	require.Contains(t, assignment, `"data-relationships-template"`)
	require.Contains(t, assignment, `nestedDataRelationships["template"] = asMap`)
	require.NotContains(t, assignment, `body := map[string]any{}`)

	runtimeTest := `package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func executeKlaviyoCommand(t *testing.T, args ...string) error {
	t.Helper()
	var flags rootFlags
	cmd := newRootCmd(&flags)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestKlaviyoStableCampaignBodiesOnWire(t *testing.T) {
	requests := map[string]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode %s body: %v", r.URL.Path, err)
		}
		requests[r.URL.Path] = body
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_, _ = w.Write([]byte("{\"data\":{\"type\":\"ok\",\"id\":\"1\"}}"))
	}))
	defer server.Close()
	t.Setenv("KLAVIYO_BASE_URL", server.URL)

	campaignMessages := ` + "`" + `{"data":[{"type":"campaign-message","attributes":{"definition":{"channel":"email","label":"Launch","content":{"subject":"Subject","preview_text":"Preview","from_email":"from@example.com","from_label":"Sender","reply_to_email":"reply@example.com"}}}}]}` + "`" + `
	if err := executeKlaviyoCommand(t,
		"campaigns",
		"--data-type", "campaign",
		"--data-attributes-name", "Launch",
		"--data-attributes-audiences", ` + "`" + `{"included":["list-1"],"excluded":[]}` + "`" + `,
		"--data-attributes-campaign-messages", campaignMessages,
	); err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	if err := executeKlaviyoCommand(t,
		"campaign-message-assign-template",
		"--data-type", "campaign-message",
		"--data-id", "message-1",
		"--data-relationships-template", ` + "`" + `{"data":{"type":"template","id":"template-1"}}` + "`" + `,
	); err != nil {
		t.Fatalf("assign template: %v", err)
	}

	var wantCampaign map[string]any
	if err := json.Unmarshal([]byte(` + "`" + `{"data":{"type":"campaign","attributes":{"name":"Launch","audiences":{"included":["list-1"],"excluded":[]},"campaign-messages":{"data":[{"type":"campaign-message","attributes":{"definition":{"channel":"email","label":"Launch","content":{"subject":"Subject","preview_text":"Preview","from_email":"from@example.com","from_label":"Sender","reply_to_email":"reply@example.com"}}}}]}}}}` + "`" + `), &wantCampaign); err != nil {
		t.Fatal(err)
	}
	var wantAssignment map[string]any
	if err := json.Unmarshal([]byte(` + "`" + `{"data":{"type":"campaign-message","id":"message-1","relationships":{"template":{"data":{"type":"template","id":"template-1"}}}}}` + "`" + `), &wantAssignment); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(requests["/api/campaigns"], wantCampaign) {
		t.Fatalf("campaign body = %#v, want %#v", requests["/api/campaigns"], wantCampaign)
	}
	if !reflect.DeepEqual(requests["/api/campaign-message-assign-template"], wantAssignment) {
		t.Fatalf("assignment body = %#v, want %#v", requests["/api/campaign-message-assign-template"], wantAssignment)
	}
}

func TestKlaviyoBoundaryObjectRequiresNestedData(t *testing.T) {
	err := executeKlaviyoCommand(t,
		"campaigns",
		"--data-type", "campaign",
		"--data-attributes-name", "Launch",
		"--data-attributes-audiences", "{\"included\":[\"list-1\"]}",
		"--data-attributes-campaign-messages", "{}",
	)
	if err == nil {
		t.Fatal("expected missing nested data error")
	}
	if !strings.Contains(err.Error(), "missing required field") || !strings.Contains(err.Error(), "data") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestKlaviyoAssignmentDryRunPrintsExactBody(t *testing.T) {
	previous := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	err = executeKlaviyoCommand(t,
		"--dry-run", "--json", "campaign-message-assign-template",
		"--data-type", "campaign-message",
		"--data-id", "message-1",
		"--data-relationships-template", ` + "`" + `{"data":{"type":"template","id":"template-1"}}` + "`" + `,
	)
	_ = writer.Close()
	os.Stderr = previous
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	text := string(output)
	start := strings.Index(text, "  Body:\n")
	end := strings.Index(text, "\n\n(dry run - no request sent)")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("dry-run body not found in %q", text)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(text[start+len("  Body:\n"):end]), &got); err != nil {
		t.Fatalf("decode dry-run body: %v", err)
	}
	want := map[string]any{"data": map[string]any{
		"type": "campaign-message", "id": "message-1",
		"relationships": map[string]any{
			"template": map[string]any{"data": map[string]any{"type": "template", "id": "template-1"}},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dry-run body = %#v, want %#v", got, want)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "klaviyo_body_contract_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestKlaviyo", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

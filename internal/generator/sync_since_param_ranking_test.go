package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedSinceParamRanking(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(sinceRankFixture))
	require.NoError(t, err)

	profile := profiler.Profile(apiSpec)
	sales := requireSinceResource(t, profile, "/sales")
	events := requireSinceResource(t, profile, "/events")
	issues := requireSinceResource(t, profile, "/issues")
	bounces := requireSinceResource(t, profile, "/bounces")
	users := requireSinceResource(t, profile, "/users")
	audits := requireSinceResource(t, profile, "/audits")
	revisions := requireSinceResource(t, profile, "/revisions")
	changes := requireSinceResource(t, profile, "/changes")

	assert.Equal(t, "after", sales.param)
	assert.Equal(t, "date", sales.format)
	assert.Equal(t, "page_key", sales.cursor)

	assert.Equal(t, "updatedMin", events.param)
	assert.Empty(t, events.format)
	assert.Equal(t, "pageToken", events.cursor)

	assert.Equal(t, "updated_on", issues.param)
	assert.Equal(t, "operator:>=:date", issues.format)

	assert.Equal(t, "fromdate", bounces.param)
	assert.Equal(t, "local-datetime:America/New_York", bounces.format)

	assert.Empty(t, users.param)
	assert.Empty(t, audits.param)
	assert.Empty(t, revisions.param)
	assert.Equal(t, "since", changes.param)
	assert.Equal(t, "date-time", changes.format)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	src := string(syncSrc)

	sinceBody := generatedFunctionBody(t, src, "func syncResourceSinceParam(resource string) string")
	formatBody := generatedFunctionBody(t, src, "func syncResourceSinceParamFormat(resource string) string")
	assert.Contains(t, sinceBody, fmt.Sprintf("case %q:", sales.name))
	assert.Contains(t, sinceBody, `return "after"`)
	assert.Contains(t, sinceBody, fmt.Sprintf("case %q:", events.name))
	assert.Contains(t, sinceBody, `return "updatedMin"`)
	assert.Contains(t, sinceBody, fmt.Sprintf("case %q:", issues.name))
	assert.Contains(t, sinceBody, `return "updated_on"`)
	assert.NotContains(t, sinceBody, `return "start_date"`)
	assert.Contains(t, sinceBody, fmt.Sprintf("case %q:", bounces.name))
	assert.Contains(t, sinceBody, `return "fromdate"`)
	assert.Contains(t, sinceBody, fmt.Sprintf("case %q:", changes.name))
	assert.Contains(t, sinceBody, `return "since"`)
	assert.NotContains(t, sinceBody, fmt.Sprintf("case %q:", users.name))
	assert.NotContains(t, sinceBody, fmt.Sprintf("case %q:", audits.name))
	assert.NotContains(t, sinceBody, fmt.Sprintf("case %q:", revisions.name))

	assert.Contains(t, formatBody, `return "date"`)
	assert.Contains(t, formatBody, `return "operator:>=:date"`)
	assert.Contains(t, formatBody, `return "local-datetime:America/New_York"`)
	assert.Contains(t, formatBody, `return "date-time"`)
	assert.NotContains(t, formatBody, fmt.Sprintf("case %q:", events.name))

	assert.Contains(t, src, `_ "time/tzdata"`)
	assert.Contains(t, src, "endpoint has no usable incremental temporal filter")
	assert.NotContains(t, src, "declares no temporal filter")
	assert.NotContains(t, src, "does not declare a temporal filter parameter")
	assert.Contains(t, src, `"reason":"resource_not_incremental"`)

	testSrc := fmt.Sprintf(`package cli

import "testing"

func TestFormatSyncSinceValueFromRankedSpec(t *testing.T) {
	if got := syncResourceSinceParam(%q); got != "after" {
		t.Fatalf("sales since = %%q", got)
	}
	if got := syncResourceSinceParamFormat(%q); got != "date" {
		t.Fatalf("sales format = %%q", got)
	}
	if got := syncResourceSinceParam(%q); got != "updatedMin" {
		t.Fatalf("events since = %%q", got)
	}
	if got := syncResourceSinceParamFormat(%q); got != "" {
		t.Fatalf("events format = %%q", got)
	}
	if got := syncResourceSinceParam(%q); got != "updated_on" {
		t.Fatalf("issues since = %%q", got)
	}
	if got := syncResourceSinceParamFormat(%q); got != "operator:>=:date" {
		t.Fatalf("issues format = %%q", got)
	}
	if got := syncResourceSinceParam(%q); got != "fromdate" {
		t.Fatalf("bounces since = %%q", got)
	}
	if got := syncResourceSinceParamFormat(%q); got != "local-datetime:America/New_York" {
		t.Fatalf("bounces format = %%q", got)
	}
	if got := syncResourceSinceParam(%q); got != "" {
		t.Fatalf("users since = %%q", got)
	}
	if got := syncResourceSinceParam(%q); got != "since" {
		t.Fatalf("changes since = %%q", got)
	}
	if got := syncResourceSinceParamFormat(%q); got != "date-time" {
		t.Fatalf("changes format = %%q", got)
	}

	checks := []struct {
		value  string
		format string
		want   string
	}{
		{"2026-06-07T12:34:56Z", "date", "2026-06-07"},
		{"2026-06-07T12:34:56.789Z", "date", "2026-06-07"},
		{"2026-06-07", "date", "2026-06-07"},
		{"not-a-date-value", "date", "not-a-date-value"},
		{"2026-06-07T12:34:56Z", "date-time", "2026-06-07T12:34:56Z"},
		{"2026-06-07T12:34:56Z", "Date", "2026-06-07"},
		{"2026-06-07T12:34:56Z", "odata-conditions:_info/lastUpdated", "_info/lastUpdated > [2026-06-07T12:34:56Z]"},
		{"2026-06-07T12:34:56Z", "odata-conditions:", "2026-06-07T12:34:56Z"},
		{"2026-06-07T12:34:56Z", "odata-conditions:  ", "2026-06-07T12:34:56Z"},
		{"2026-06-07T12:34:56Z", "operator:>=:date", ">=2026-06-07"},
		{"2026-06-07", "operator:>=:date", ">=2026-06-07"},
		{"2026-06-07T12:34:56.789Z", "operator:>=:date", ">=2026-06-07"},
		{"not-a-date-value", "operator:>=:date", "not-a-date-value"},
		{"not-a-date-value", "operator:>=:nope", "not-a-date-value"},
		{"2026-06-07T12:34:56Z", "operator:", "2026-06-07T12:34:56Z"},
		{"2026-06-07T12:34:56Z", "operator::date", "2026-06-07T12:34:56Z"},
		{"2026-06-07T12:34:56Z", "operator:>=:date-time", ">=2026-06-07T12:34:56Z"},
		{"2026-06-07T12:34:56.789Z", "operator:>=:date-time", ">=2026-06-07T12:34:56.789Z"},
		{"2026-06-07", "operator:>=:date-time", "2026-06-07"},
		{"2026-01-15T15:00:00Z", "local-datetime:America/New_York", "2026-01-15T10:00:00"},
		{"2026-07-15T15:00:00Z", "local-datetime:America/New_York", "2026-07-15T11:00:00"},
		{"2026-01-15T15:00:00.123456789Z", "local-datetime:America/New_York", "2026-01-15T10:00:00"},
		{"2026-01-15T15:00:00Z", "local-datetime:America/Chicago", "2026-01-15T09:00:00"},
		{"2026-01-15T15:00:00Z", "local-datetime:America/Denver", "2026-01-15T08:00:00"},
		{"2026-01-15T15:00:00Z", "local-datetime:America/Los_Angeles", "2026-01-15T07:00:00"},
		{"2026-01-15T15:00:00Z", "local-datetime:UTC", "2026-01-15T15:00:00"},
		{"2026-01-15T15:00:00Z", "local-datetime:Not/AZone", "2026-01-15T15:00:00Z"},
		{"2026-01-15T15:00:00Z", "local-datetime:", "2026-01-15T15:00:00Z"},
		{"not-a-date-value", "local-datetime:America/New_York", "not-a-date-value"},
		{"2026-06-07T12:34:56Z", "", "2026-06-07T12:34:56Z"},
	}
	for _, tc := range checks {
		if got := formatSyncSinceValue(tc.value, tc.format); got != tc.want {
			t.Fatalf("formatSyncSinceValue(%%q, %%q) = %%q, want %%q", tc.value, tc.format, got, tc.want)
		}
	}
}
`, sales.name, sales.name, events.name, events.name, issues.name, issues.name, bounces.name, bounces.name, users.name, changes.name, changes.name)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_since_format_rank_test.go"), []byte(testSrc), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestFormatSyncSinceValueFromRankedSpec$")
}

type sinceResource struct {
	name   string
	param  string
	format string
	cursor string
}

func requireSinceResource(t *testing.T, profile *profiler.APIProfile, path string) sinceResource {
	t.Helper()
	for _, resource := range profile.SyncableResources {
		if resource.Path == path {
			return sinceResource{resource.Name, resource.SinceParam, resource.SinceParamFormat, resource.PaginationCursorParam}
		}
	}
	for _, resource := range profile.DependentSyncResources {
		if resource.Path == path {
			return sinceResource{resource.Name, resource.SinceParam, resource.SinceParamFormat, resource.PaginationCursorParam}
		}
	}
	var have []string
	for _, resource := range profile.SyncableResources {
		have = append(have, "sync "+resource.Name+" "+resource.Path)
	}
	for _, resource := range profile.DependentSyncResources {
		have = append(have, "dep "+resource.Name+" "+resource.Path)
	}
	t.Fatalf("path %s not syncable; have %s", path, strings.Join(have, "; "))
	return sinceResource{}
}

const sinceRankFixture = `
openapi: 3.0.3
info:
  title: Since Rank Fixture
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /sales:
    get:
      operationId: listSales
      parameters:
        - name: after
          in: query
          description: Return records after this date
          schema: {type: string, format: date}
        - name: before
          in: query
          description: Return records before this date
          schema: {type: string, format: date}
        - name: page_key
          in: query
          schema: {type: string}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ListEnvelope"
  /events:
    get:
      operationId: listEvents
      parameters:
        - name: timeMin
          in: query
          description: Lower bound 2011-06-03T10:00:00-07:00 or 2011-06-03T10:00:00Z
          schema: {type: string}
        - name: timeMax
          in: query
          description: Upper bound 2011-06-03T10:00:00-07:00
          schema: {type: string}
        - name: updatedMin
          in: query
          description: Lower bound for an event's last modification time (as a RFC3339 timestamp).
          schema: {type: string}
        - name: pageToken
          in: query
          schema: {type: string}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ListEnvelope"
  /issues:
    get:
      operationId: listIssues
      parameters:
        - name: start_date
          in: query
          description: "Filter by start date. Examples: >=2024-01-01"
          schema: {type: string}
        - name: updated_on
          in: query
          description: "Filter by last updated date. Examples: >=2024-01-01"
          schema: {type: string}
        - name: created_on
          in: query
          description: "Filter by creation date. Examples: >=2024-01-01"
          schema: {type: string}
        - name: due_date
          in: query
          schema: {type: string}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ListEnvelope"
  /bounces:
    get:
      operationId: listBounces
      parameters:
        - name: fromdate
          in: query
          description: Filter messages starting from the date specified. e.g. 2014-02-01 or 2014-02-01T10:00:00 (Eastern Time)
          schema: {type: string}
        - name: todate
          in: query
          description: Filter messages up to the date specified. e.g. 2014-02-01
          schema: {type: string}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ListEnvelope"
  /users:
    get:
      operationId: listUsers
      parameters:
        - name: email
          in: query
          schema: {type: string}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ListEnvelope"
  /audits:
    get:
      operationId: listAudits
      parameters:
        - name: updated_before
          in: query
          schema: {type: string, format: date-time}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ListEnvelope"
  /revisions:
    get:
      operationId: listRevisions
      parameters:
        - name: updatedMin
          in: header
          description: Lower bound for last modification time
          schema: {type: string, format: date-time}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ListEnvelope"
  /changes:
    get:
      operationId: listChanges
      parameters:
        - name: updated_fields
          in: query
          schema: {type: string}
        - name: since
          in: query
          schema: {type: string, format: date-time}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ListEnvelope"
components:
  schemas:
    ListEnvelope:
      type: object
      properties:
        data:
          type: array
          items:
            type: object
            properties:
              id: {type: string}
`

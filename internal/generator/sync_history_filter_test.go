package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func syncHistoryFilterSpec(name string, statusDefault string, enum []string, syncParams map[string]string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"orders": {
			Description: "Orders",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/v2/orders",
					Response:   spec.ResponseDef{Type: "array"},
					SyncParams: syncParams,
					Params: []spec.Param{
						{Name: "status", In: "query", Type: "string", Default: statusDefault, Enum: enum},
					},
				},
			},
		},
	}
	return apiSpec
}

// TestGeneratedSyncWidensOpenStatusDefault is the generated-output proof that a
// list endpoint whose spec default is status=open does not seed that slice as
// the complete resource. Sync sends the all-history enum value instead;
// endpoint commands keep the documented default.
func TestGeneratedSyncWidensOpenStatusDefault(t *testing.T) {
	t.Parallel()

	apiSpec := syncHistoryFilterSpec("syncwiden", "open", []string{"open", "closed", "all"}, nil)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaults := extractSyncParamDefaultsBlock(t, syncSrc, "orders")
	assert.Contains(t, defaults, `"status": "all"`,
		"sync must send the all-history enum value, not the list default status=open")
	assert.NotContains(t, defaults, `"status": "open"`)
	assert.NotContains(t, syncSrc, "default_filter_hides_history",
		"a widened filter is not a silent-empty risk; do not emit the 0-row warning helper")

	requireGeneratedCompiles(t, outputDir)

	inlineTest := fmt.Sprintf(`package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	%q
)

type historyFilterClient struct {
	got  []map[string]string
	body json.RawMessage
}

func (c *historyFilterClient) Get(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	c.got = append(c.got, copied)
	if len(c.body) == 0 {
		return json.RawMessage("[{\"id\":\"filled-1\"}]"), nil
	}
	return c.body, nil
}

func (*historyFilterClient) RateLimit() float64 { return 0 }

func openHistoryFilterStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %%v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSyncOrdersSendsAllHistoryStatus(t *testing.T) {
	db := openHistoryFilterStore(t)
	client := &historyFilterClient{}
	res := syncResource(context.Background(), client, db, "orders", "", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %%v", res.Err)
	}
	if len(client.got) == 0 {
		t.Fatal("no request issued")
	}
	if got := client.got[0]["status"]; got != "all" {
		t.Fatalf("status = %%q, want %%q (sync must not send the list default status=open)", got, "all")
	}
	if res.Count != 1 {
		t.Fatalf("count = %%d, want 1", res.Count)
	}
}

func TestSyncOrdersUserParamStillOverridesWiden(t *testing.T) {
	db := openHistoryFilterStore(t)
	client := &historyFilterClient{}
	userParams, err := parseSyncUserParams([]string{"status=open"}, nil, nil)
	if err != nil {
		t.Fatalf("parseSyncUserParams: %%v", err)
	}
	res := syncResource(context.Background(), client, db, "orders", "", false, 1, false, false, userParams, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %%v", res.Err)
	}
	if got := client.got[0]["status"]; got != "open" {
		t.Fatalf("status = %%q, want %%q (--param must win over the widened default)", got, "open")
	}
}

func TestSyncOrdersGenuineEmptyAfterWidenIsNotAWarning(t *testing.T) {
	db := openHistoryFilterStore(t)
	client := &historyFilterClient{body: json.RawMessage("[]")}
	var events bytes.Buffer
	res := syncResource(context.Background(), client, db, "orders", "", false, 1, false, false, nil, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error: %%v", res.Err)
	}
	if res.Warn != nil {
		t.Fatalf("warn = %%v, want nil for a genuine empty resource after widen", res.Warn)
	}
	if strings.Contains(events.String(), "default_filter_hides_history") {
		t.Fatalf("events = %%s, want no history-hiding warning after widen", events.String())
	}
}
`, naming.CLI(apiSpec.Name)+"/internal/store")
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_history_widen_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestSyncOrders(SendsAllHistoryStatus|UserParamStillOverridesWiden|GenuineEmptyAfterWidenIsNotAWarning)")
}

// TestGeneratedSyncWarnsWhenOpenDefaultCannotWiden covers the fallback: the
// spec declares status=open but the enum has no all/any value, so sending a
// guessed widen could 400. Sync keeps the declared default and converts a
// silent 0-row success into a named warning.
func TestGeneratedSyncWarnsWhenOpenDefaultCannotWiden(t *testing.T) {
	t.Parallel()

	apiSpec := syncHistoryFilterSpec("syncwarn", "open", []string{"open", "closed"}, nil)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaults := extractSyncParamDefaultsBlock(t, syncSrc, "orders")
	assert.Contains(t, defaults, `"status": "open"`,
		"without an all-history enum value sync must not guess a widen")
	assert.Contains(t, syncSrc, "default_filter_hides_history")
	assert.Contains(t, syncSrc, "emitDefaultFilterHistoryWarning")

	requireGeneratedCompiles(t, outputDir)

	inlineTest := fmt.Sprintf(`package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	%q
)

type warnFilterClient struct{}

func (*warnFilterClient) Get(_ context.Context, _ string, _ map[string]string) (json.RawMessage, error) {
	return json.RawMessage("[]"), nil
}

func (*warnFilterClient) RateLimit() float64 { return 0 }

func TestSyncOrdersZeroRowsWarnsOnOpenDefault(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %%v", err)
	}
	t.Cleanup(func() { db.Close() })
	var events bytes.Buffer
	res := syncResource(context.Background(), &warnFilterClient{}, db, "orders", "", false, 1, false, false, nil, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error: %%v", res.Err)
	}
	if res.Warn == nil {
		t.Fatal("want a warning when 0 rows land under status=open")
	}
	if !strings.Contains(events.String(), "default_filter_hides_history") {
		t.Fatalf("events = %%s, want default_filter_hides_history", events.String())
	}
	if !strings.Contains(events.String(), "\"param\":\"status\"") {
		t.Fatalf("events = %%s, want param status", events.String())
	}
}

func TestSyncOrdersZeroRowsNoWarnWhenUserOverrides(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %%v", err)
	}
	t.Cleanup(func() { db.Close() })
	userParams, err := parseSyncUserParams([]string{"status=closed"}, nil, nil)
	if err != nil {
		t.Fatalf("parseSyncUserParams: %%v", err)
	}
	var events bytes.Buffer
	res := syncResource(context.Background(), &warnFilterClient{}, db, "orders", "", false, 1, false, false, userParams, &events)
	if res.Err != nil {
		t.Fatalf("syncResource error: %%v", res.Err)
	}
	if res.Warn != nil {
		t.Fatalf("warn = %%v, want nil when --param overrode the hiding default", res.Warn)
	}
	if strings.Contains(events.String(), "default_filter_hides_history") {
		t.Fatalf("events = %%s, want no history-hiding warning after --param override", events.String())
	}
}
`, naming.CLI(apiSpec.Name)+"/internal/store")
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_history_warn_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestSyncOrdersZeroRows(WarnsOnOpenDefault|NoWarnWhenUserOverrides)")
}

func TestGeneratedSyncParamsOverlayWithoutEnumAll(t *testing.T) {
	t.Parallel()

	apiSpec := syncHistoryFilterSpec("syncparams", "open", []string{"open", "closed"}, map[string]string{"status": "all"})
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	defaults := extractSyncParamDefaultsBlock(t, syncSrc, "orders")
	assert.Contains(t, defaults, `"status": "all"`,
		"sync_params must overlay a default the enum could not widen")
	assert.NotContains(t, syncSrc, "default_filter_hides_history")

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedSyncSendsOpenAPISyncParamsAndWidensState(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(`
openapi: 3.0.3
info:
  title: Sync History OpenAPI
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /issues:
    get:
      operationId: listIssues
      parameters:
        - name: state
          in: query
          schema:
            type: string
            default: open
            enum: [open, closed, all]
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
  /orders:
    get:
      operationId: listOrders
      x-sync-params:
        status: all
      parameters:
        - name: status
          in: query
          schema:
            type: string
            default: open
            enum: [open, closed]
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
`))
	require.NoError(t, err)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	issues := extractSyncParamDefaultsBlock(t, syncSrc, "issues")
	assert.Contains(t, issues, `"state": "all"`,
		"OpenAPI state=open must widen to the all-history enum value")
	orders := extractSyncParamDefaultsBlock(t, syncSrc, "orders")
	assert.Contains(t, orders, `"status": "all"`,
		"x-sync-params must reach generated sync even when the enum has no all value")

	requireGeneratedCompiles(t, outputDir)
}

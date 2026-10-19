package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestNarrowedSyncPruneAndCheckpoints generates one CLI and runs the
// contract: a narrowed full walk must not delete rows it did not cover,
// a different tenant must not resume the previous cursor, and an
// un-narrowed single-tenant walk must still prune.
func TestNarrowedSyncPruneAndCheckpoints(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("scopeguard")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.BaseURL = "https://{server}.example.com"
	apiSpec.EndpointTemplateVars = []string{"server", "api_version"}
	apiSpec.SyncPathContextVars = []string{"networkId"}
	apiSpec.Resources = map[string]spec.Resource{
		"clients": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/clients",
					Response:   spec.ResponseDef{Type: "array", Item: "Client"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
					Params: []spec.Param{
						{Name: "since", Type: "string"},
						{Name: "dates", Type: "string"},
						{Name: "account_id", Type: "string", GlobalScope: true},
					},
				},
			},
		},
		"projects": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/projects",
					Response:   spec.ResponseDef{Type: "array", Item: "Project"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
				},
				"get": {
					Method:   "GET",
					Path:     "/projects/{projectId}",
					Response: spec.ResponseDef{Type: "object", Item: "Project"},
					IDField:  "id",
				},
			},
		},
		"modules": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/projects/{projectId}/modules",
					Response:   spec.ResponseDef{Type: "array", Item: "Module"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
				},
			},
		},
		"transactions": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/transactions",
					Response:   spec.ResponseDef{Type: "array", Item: "Transaction"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
					Params: []spec.Param{
						{Name: "end_date", Type: "string", Required: true, Default: "2024-01-31"},
					},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Client":      {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		"Project":     {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		"Module":      {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
		"Transaction": {Fields: []spec.TypeField{{Name: "id", Type: "string"}, {Name: "name", Type: "string"}}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	helperSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "helpers.go"))
	require.NoError(t, err)
	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	for _, needle := range []string{
		"BindSyncScope",
		"ReadSyncScope",
		"syncPruneSkipReason",
		"required_date_window",
		"since_window",
		"date_range",
		"param_filter",
		"global_param",
		"resource_param",
		"tenant_scope",
	} {
		require.Contains(t, string(syncSrc), needle, needle)
	}
	require.Contains(t, string(storeSrc), "scope_key TEXT NOT NULL DEFAULT ''")
	require.Contains(t, string(storeSrc), "const StoreSchemaVersion = 13")
	require.Contains(t, string(helperSrc), "func syncCheckpointScope(")
	require.Contains(t, string(clientSrc), "func (c *Client) CurrentTemplateVars()")

	inlineTest := `package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type recordedCall struct {
	path   string
	params map[string]string
}

type scopeClient struct {
	template map[string]string
	dry      bool
	calls    []recordedCall
	body     func(path string, params map[string]string) json.RawMessage
}

func (s *scopeClient) Get(_ context.Context, path string, params map[string]string) (json.RawMessage, error) {
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	s.calls = append(s.calls, recordedCall{path: path, params: copied})
	if s.dry {
		return json.RawMessage("{\"dry_run\":true}"), nil
	}
	if s.body != nil {
		return s.body(path, copied), nil
	}
	return json.RawMessage("[]"), nil
}

func (s *scopeClient) RateLimit() float64 { return 0 }
func (s *scopeClient) IsDryRun() bool      { return s.dry }
func (s *scopeClient) CurrentTemplateVars() map[string]string {
	if len(s.template) == 0 {
		return nil
	}
	return s.template
}

type plainClient struct {
	body  func(path string, params map[string]string) json.RawMessage
	calls []recordedCall
}

func (p *plainClient) Get(_ context.Context, path string, params map[string]string) (json.RawMessage, error) {
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	p.calls = append(p.calls, recordedCall{path: path, params: copied})
	if p.body != nil {
		return p.body(path, copied), nil
	}
	return json.RawMessage("[]"), nil
}

func (p *plainClient) RateLimit() float64 { return 0 }

func openDB(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedRows(t *testing.T, db *store.Store, resource string, rows ...map[string]any) {
	t.Helper()
	var batch []json.RawMessage
	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		batch = append(batch, raw)
	}
	if _, _, err := db.UpsertBatch(resource, batch); err != nil {
		t.Fatalf("seed %s: %v", resource, err)
	}
}

func hasRow(t *testing.T, db *store.Store, resource, id string) bool {
	t.Helper()
	rows, err := db.List(resource, 0)
	if err != nil {
		t.Fatalf("list %s: %v", resource, err)
	}
	for _, raw := range rows {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if fmt.Sprint(obj["id"]) == id {
			return true
		}
	}
	return false
}

func arr(ids ...string) json.RawMessage {
	items := make([]any, 0, len(ids))
	for _, id := range ids {
		items = append(items, map[string]any{"id": id, "name": id})
	}
	raw, _ := json.Marshal(items)
	return raw
}

func envelope(id, cursor string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"items":       []any{map[string]any{"id": id, "name": id}},
		"next_cursor": cursor,
		"has_more":    true,
	})
	return raw
}

func runScope(c *scopeClient, db *store.Store, resource, since string, full bool, maxPages int, prune bool, dates string, params *syncUserParams) (syncResult, string) {
	var buf bytes.Buffer
	res := syncResource(context.Background(), c, db, resource, since, full, maxPages, false, prune, dates, params, &buf)
	return res, buf.String()
}

func runPlain(c *plainClient, db *store.Store, resource, since string, full bool, maxPages int, prune bool, dates string, params *syncUserParams) (syncResult, string) {
	var buf bytes.Buffer
	res := syncResource(context.Background(), c, db, resource, since, full, maxPages, false, prune, dates, params, &buf)
	return res, buf.String()
}

func mustOK(t *testing.T, res syncResult, events string) {
	t.Helper()
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("sync err=%v warn=%v events=%s", res.Err, res.Warn, events)
	}
}

func hasSkip(events, resource, reason string) bool {
	for _, line := range strings.Split(events, "\n") {
		if strings.Contains(line, "\"event\":\"reconcile_skipped\"") &&
			strings.Contains(line, "\"resource\":\""+resource+"\"") &&
			strings.Contains(line, "\"reason\":\""+reason+"\"") {
			return true
		}
	}
	return false
}

func hasReconcile(events, resource string) bool {
	for _, line := range strings.Split(events, "\n") {
		if strings.Contains(line, "\"event\":\"reconcile\",") && strings.Contains(line, "\"resource\":\""+resource+"\"") {
			return true
		}
	}
	return false
}

func withPath(t *testing.T, values map[string]string) {
	t.Helper()
	prev := syncPathContextValues
	syncPathContextValues = values
	t.Cleanup(func() { syncPathContextValues = prev })
}

func modulesDep(t *testing.T) dependentResourceDef {
	t.Helper()
	for _, dep := range dependentResourceDefs() {
		if dep.Name == "modules" {
			return dep
		}
	}
	t.Fatal("modules dependent was not generated")
	return dependentResourceDef{}
}

func TestScopeKeyBranches(t *testing.T) {
	if got := syncCheckpointScope("clients", false, nil, map[string]string{"api_version": "2024-01-01", "server": "a"}, nil); got != "template:server=a" {
		t.Fatalf("version placeholder leaked into scope: %q", got)
	}
	if got := syncCheckpointScope("clients", false, nil, map[string]string{"api-version": "1", "version": "2"}, nil); got != "" {
		t.Fatalf("version aliases leaked: %q", got)
	}
	if got := syncCheckpointScope("clients", false, nil, map[string]string{"server": "a\nb"}, nil); got != "template:server=a b" {
		t.Fatalf("newline in template value: %q", got)
	}
	if got := syncCheckpointScope("clients", false, nil, map[string]string{"server": "  "}, nil); got != "" {
		t.Fatalf("blank template value: %q", got)
	}
	withPath(t, map[string]string{"networkId": "net-a", "api_version": "1", "empty": "  "})
	if got := syncCheckpointScope("clients", false, nil, nil, syncActivePathContext()); got != "path:networkId=net-a" {
		t.Fatalf("path scope: %q", got)
	}
	params := &syncUserParams{flatGlobal: map[string]string{"statuses": "Online", "account_id": "acct"}}
	depKey := syncCheckpointScope("modules", true, params, nil, nil)
	if depKey != "param:account_id=acct" {
		t.Fatalf("dependent scope key = %q", depKey)
	}
	flatKey := syncCheckpointScope("clients", false, params, nil, nil)
	if flatKey != "param:account_id=acct" {
		t.Fatalf("flat scope key = %q", flatKey)
	}
	if got := syncCheckpointScope("clients", false, &syncUserParams{
		trueGlobal:  map[string]string{"sort": "name:asc"},
		perResource: map[string]map[string]string{"clients": {"sort": "name:desc"}},
	}, nil, nil); got != "" {
		t.Fatalf("filter params leaked into scope: %q", got)
	}
	if !syncParamIsDateWindow("end_date") || !syncParamIsDateWindow("report_date") || !syncParamIsDateWindow("End-Date") {
		t.Fatal("date-like names must count as a window")
	}
	if syncParamIsDateWindow("format") || syncParamIsDateWindow("statuses") {
		t.Fatal("non-date names must not count as a window")
	}
	if got := syncPruneSkipReason("clients", false, "", "", &syncUserParams{flatGlobal: map[string]string{"account_id": "a", "statuses": "Online"}}, "flat_global", ""); got != "tenant_scope" {
		t.Fatalf("global-scope param reason %q", got)
	}
	if got := syncPruneSkipReason("clients", false, "", "", &syncUserParams{flatGlobal: map[string]string{"statuses": "Online"}}, "flat_global", ""); got != "param_filter" {
		t.Fatalf("param reason %q", got)
	}
	if got := syncPruneSkipReason("modules", true, "", "", &syncUserParams{flatGlobal: map[string]string{"statuses": "Online"}}, "per_parent", ""); got != "" {
		t.Fatalf("--param must not narrow a dependent walk, got %q", got)
	}
	if got := syncPruneSkipReason("modules", true, "", "", &syncUserParams{trueGlobal: map[string]string{"region": "us"}}, "per_parent", ""); got != "global_param" {
		t.Fatalf("global-param reason %q", got)
	}
	if got := syncPruneSkipReason("modules", true, "", "", &syncUserParams{perResource: map[string]map[string]string{"modules": {"status": "Online"}}}, "per_parent", ""); got != "resource_param" {
		t.Fatalf("resource-param reason %q", got)
	}
	if got := syncPruneSkipReason("clients", false, "2020-01-01T00:00:00Z", "", nil, "flat_global", ""); got != "since_window" {
		t.Fatalf("since reason %q", got)
	}
	if got := syncPruneSkipReason("clients", false, "  ", "", nil, "flat_global", ""); got != "" {
		t.Fatalf("blank since reason %q", got)
	}
	if got := syncPruneSkipReason("clients", false, "", "20240101-20240131", nil, "flat_global", ""); got != "date_range" {
		t.Fatalf("dates reason %q", got)
	}
	if got := syncPruneSkipReason("transactions", false, "", "", nil, "flat_global", ""); got != "required_date_window" {
		t.Fatalf("required date reason %q", got)
	}
	if got := syncPruneSkipReason("clients", false, "", "", nil, "flat_global", ""); got != "" {
		t.Fatalf("unnarrowed reason %q", got)
	}
	if got := syncPruneSkipReason("clients", false, "", "", nil, "flat", "template:server=a"); got != "" {
		t.Fatalf("tenant-column partition must still prune, got %q", got)
	}
	if got := syncPruneSkipReason("clients", false, "", "", nil, "flat_global", "template:server=a"); got != "tenant_scope" {
		t.Fatalf("template scope reason %q", got)
	}
	if got := syncPruneSkipReason("clients", false, "", "", nil, "per_parent", "path:networkId=n"); got != "tenant_scope" {
		t.Fatalf("path scope reason %q", got)
	}
}

func TestCheckpointResetAndLegacyUpgrade(t *testing.T) {
	db := openDB(t)
	stamp := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.SaveSyncStateAt("clients", "cur-a", 4, stamp); err != nil {
		t.Fatal(err)
	}
	cursor, gotStamp, count, err := db.BindSyncScope("clients", "")
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "cur-a" || !gotStamp.Equal(stamp) || count != 4 {
		t.Fatalf("matching empty scope = %q %s %d", cursor, gotStamp, count)
	}
	var complete int
	if err := db.DB().QueryRow("SELECT last_attempt_complete FROM sync_state WHERE resource_type = 'clients'").Scan(&complete); err != nil {
		t.Fatal(err)
	}
	if complete != 1 {
		t.Fatalf("matching bind changed completion marker to %d", complete)
	}
	cursor, gotStamp, count, err = db.ReadSyncScope("clients", "template:server=b")
	if err != nil || cursor != "" || !gotStamp.IsZero() || count != 0 {
		t.Fatalf("dry-run mismatch = %q %s %d err=%v", cursor, gotStamp, count, err)
	}
	cursor, _, _, err = db.GetSyncState("clients")
	if err != nil || cursor != "cur-a" {
		t.Fatalf("dry-run mismatch cleared stored cursor %q err=%v", cursor, err)
	}
	cursor, _, count, err = db.BindSyncScope("clients", "template:server=b")
	if err != nil || cursor != "" || count != 0 {
		t.Fatalf("bind mismatch = %q count=%d err=%v", cursor, count, err)
	}
	if err := db.SaveSyncProgress("clients", "cur-b", 2); err != nil {
		t.Fatal(err)
	}
	var scope, saved string
	if err := db.DB().QueryRow("SELECT scope_key, IFNULL(last_cursor, '') FROM sync_state WHERE resource_type = 'clients'").Scan(&scope, &saved); err != nil {
		t.Fatal(err)
	}
	if scope != "template:server=b" || saved != "cur-b" {
		t.Fatalf("progress after bind = scope %q cursor %q", scope, saved)
	}
	cursor, _, _, err = db.BindSyncScope("clients", "")
	if err != nil || cursor != "" {
		t.Fatalf("switch back returned %q err=%v", cursor, err)
	}

	missing, _, _, err := db.ReadSyncScope("absent", "template:server=a")
	if err != nil || missing != "" {
		t.Fatalf("read missing = %q err=%v", missing, err)
	}
	var n int
	if err := db.DB().QueryRow("SELECT COUNT(*) FROM sync_state WHERE resource_type = 'absent'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("read missing inserted %d rows, err=%v", n, err)
	}
	if _, _, _, err := db.BindSyncScope("absent", "template:server=a"); err != nil {
		t.Fatal(err)
	}
	if err := db.DB().QueryRow("SELECT scope_key, IFNULL(last_cursor, '') FROM sync_state WHERE resource_type = 'absent'").Scan(&scope, &saved); err != nil {
		t.Fatal(err)
	}
	if scope != "template:server=a" || saved != "" {
		t.Fatalf("bind insert = scope %q cursor %q", scope, saved)
	}
}

func TestLegacyScopeColumnKeepsCursor(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"CREATE TABLE sync_state (resource_type TEXT PRIMARY KEY, last_cursor TEXT, last_synced_at DATETIME, total_count INTEGER DEFAULT 0, last_attempt_complete INTEGER NOT NULL DEFAULT 0)",
		"INSERT INTO sync_state(resource_type, last_cursor, last_synced_at, total_count, last_attempt_complete) VALUES ('clients', 'cur-old', '2020-01-01T00:00:00Z', 3, 1)",
		"PRAGMA user_version = 12",
	} {
		if _, err := raw.Exec(stmt); err != nil {
			raw.Close()
			t.Fatalf("seed %s: %v", stmt, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defer db.Close()
	cursor, _, count, err := db.BindSyncScope("clients", "")
	if err != nil || cursor != "cur-old" || count != 3 {
		t.Fatalf("upgraded empty scope = %q count=%d err=%v", cursor, count, err)
	}
	cursor, _, _, err = db.BindSyncScope("clients", "template:server=b")
	if err != nil || cursor != "" {
		t.Fatalf("upgraded mismatch = %q err=%v", cursor, err)
	}
}

func TestClosedStoreScopeErrors(t *testing.T) {
	db := openDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := db.BindSyncScope("clients", ""); err == nil {
		t.Fatal("bind on closed store succeeded")
	}
	if _, _, _, err := db.ReadSyncScope("clients", "template:server=b"); err == nil {
		t.Fatal("read on closed store succeeded")
	}
	if err := db.ClearSyncScopeIfCurrent("clients", ""); err == nil {
		t.Fatal("clear on closed store succeeded")
	}
}

func TestFullClearKeepsOtherScope(t *testing.T) {
	db := openDB(t)
	if err := db.SaveSyncStateAt("clients", "cur-a", 4, time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := db.BindSyncScope("clients", "template:server=a"); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSyncProgress("clients", "cur-a", 4); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearSyncScopeIfCurrent("clients", "template:server=b"); err != nil {
		t.Fatal(err)
	}
	cursor, _, _, err := db.GetSyncState("clients")
	if err != nil || cursor != "cur-a" {
		t.Fatalf("other scope clear changed cursor to %q err=%v", cursor, err)
	}
	if err := db.ClearSyncScopeIfCurrent("missing", ""); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.DB().QueryRow("SELECT COUNT(*) FROM sync_state WHERE resource_type = 'missing'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("clear missing inserted %d err=%v", n, err)
	}
	if err := db.ClearSyncScopeIfCurrent("clients", "template:server=a"); err != nil {
		t.Fatal(err)
	}
	cursor, gotStamp, count, err := db.GetSyncState("clients")
	if err != nil || cursor != "" || !gotStamp.IsZero() || count != 0 {
		t.Fatalf("matching clear = %q %s %d err=%v", cursor, gotStamp, count, err)
	}
}

func TestUnnarrowedFullPrunes(t *testing.T) {
	db := openDB(t)
	seedRows(t, db, "clients", map[string]any{"id": "keep", "name": "keep"}, map[string]any{"id": "ghost", "name": "ghost"})
	client := &plainClient{body: func(string, map[string]string) json.RawMessage { return arr("keep") }}
	res, events := runPlain(client, db, "clients", "", true, 0, true, "", nil)
	mustOK(t, res, events)
	if !hasRow(t, db, "clients", "keep") || hasRow(t, db, "clients", "ghost") {
		t.Fatal("unnarrowed --full must delete rows the API no longer returns")
	}
	if !hasReconcile(events, "clients") || hasSkip(events, "clients", "tenant_scope") {
		t.Fatalf("events=%s", events)
	}
}

func TestVersionPlaceholderStillPrunes(t *testing.T) {
	db := openDB(t)
	seedRows(t, db, "clients", map[string]any{"id": "keep", "name": "keep"}, map[string]any{"id": "ghost", "name": "ghost"})
	client := &scopeClient{
		template: map[string]string{"api_version": "2024-01-01"},
		body:     func(string, map[string]string) json.RawMessage { return arr("keep") },
	}
	res, events := runScope(client, db, "clients", "", true, 0, true, "", nil)
	mustOK(t, res, events)
	if hasRow(t, db, "clients", "ghost") {
		t.Fatal("api_version alone must not keep rows the API no longer returns")
	}
	if !hasReconcile(events, "clients") {
		t.Fatalf("events=%s", events)
	}
}

func TestTenantTemplateLeavesOtherTenant(t *testing.T) {
	db := openDB(t)
	client := &scopeClient{template: map[string]string{"server": "a"}, body: func(string, map[string]string) json.RawMessage { return arr("client-a") }}
	res, events := runScope(client, db, "clients", "", true, 0, true, "", nil)
	mustOK(t, res, events)
	client.template = map[string]string{"server": "b"}
	client.body = func(string, map[string]string) json.RawMessage { return arr("client-b") }
	res, events = runScope(client, db, "clients", "", true, 0, true, "", nil)
	mustOK(t, res, events)
	if !hasRow(t, db, "clients", "client-a") || !hasRow(t, db, "clients", "client-b") {
		t.Fatal("tenant B full sync deleted tenant A rows")
	}
	if !hasSkip(events, "clients", "tenant_scope") {
		t.Fatalf("events=%s", events)
	}
}

func TestPathContextLeavesOtherNetwork(t *testing.T) {
	db := openDB(t)
	withPath(t, map[string]string{"networkId": "net-a"})
	client := &scopeClient{body: func(string, map[string]string) json.RawMessage { return arr("client-a") }}
	res, events := runScope(client, db, "clients", "", true, 0, true, "", nil)
	mustOK(t, res, events)
	syncPathContextValues = map[string]string{"networkId": "net-b"}
	client.body = func(string, map[string]string) json.RawMessage { return arr("client-b") }
	res, events = runScope(client, db, "clients", "", true, 0, true, "", nil)
	mustOK(t, res, events)
	if !hasRow(t, db, "clients", "client-a") || !hasRow(t, db, "clients", "client-b") {
		t.Fatal("path-context B deleted network A rows")
	}
	if !hasSkip(events, "clients", "tenant_scope") {
		t.Fatalf("events=%s", events)
	}
}

func TestParamFilterDoesNotDeleteUnfiltered(t *testing.T) {
	db := openDB(t)
	seedRows(t, db, "clients", map[string]any{"id": "online", "name": "online"}, map[string]any{"id": "offline", "name": "offline"})
	client := &scopeClient{body: func(string, map[string]string) json.RawMessage { return arr("online") }}
	params := &syncUserParams{flatGlobal: map[string]string{"statuses": "Online"}}
	res, events := runScope(client, db, "clients", "", true, 0, true, "", params)
	mustOK(t, res, events)
	if !hasRow(t, db, "clients", "offline") || !hasRow(t, db, "clients", "online") {
		t.Fatal("--param filtered --full deleted rows outside the filter")
	}
	if !hasSkip(events, "clients", "param_filter") {
		t.Fatalf("events=%s", events)
	}
	if len(client.calls) == 0 || client.calls[0].params["statuses"] != "Online" {
		t.Fatalf("request params=%v", client.calls)
	}
}

func TestGlobalParamSkipsPrune(t *testing.T) {
	db := openDB(t)
	seedRows(t, db, "clients", map[string]any{"id": "keep", "name": "keep"}, map[string]any{"id": "ghost", "name": "ghost"})
	client := &scopeClient{body: func(string, map[string]string) json.RawMessage { return arr("keep") }}
	params := &syncUserParams{trueGlobal: map[string]string{"region": "us"}}
	res, events := runScope(client, db, "clients", "", true, 0, true, "", params)
	mustOK(t, res, events)
	if !hasRow(t, db, "clients", "ghost") {
		t.Fatal("--global-param --full deleted rows outside the filter")
	}
	if !hasSkip(events, "clients", "global_param") {
		t.Fatalf("events=%s", events)
	}
	if len(client.calls) == 0 || client.calls[0].params["region"] != "us" {
		t.Fatalf("request params=%v", client.calls)
	}
}

func TestSinceAndDatesSkipPrune(t *testing.T) {
	db := openDB(t)
	seedRows(t, db, "clients", map[string]any{"id": "keep", "name": "keep"}, map[string]any{"id": "ghost", "name": "ghost"})
	client := &plainClient{body: func(string, map[string]string) json.RawMessage { return arr("keep") }}
	res, events := runPlain(client, db, "clients", "2024-01-01T00:00:00Z", true, 0, true, "", nil)
	mustOK(t, res, events)
	if !hasRow(t, db, "clients", "ghost") {
		t.Fatal("--since --full deleted rows outside the window")
	}
	if !hasSkip(events, "clients", "since_window") {
		t.Fatalf("events=%s", events)
	}
	if len(client.calls) == 0 || client.calls[0].params["since"] == "" {
		t.Fatalf("since was not sent: %+v", client.calls)
	}

	db = openDB(t)
	seedRows(t, db, "clients", map[string]any{"id": "keep", "name": "keep"}, map[string]any{"id": "ghost", "name": "ghost"})
	client = &plainClient{body: func(string, map[string]string) json.RawMessage { return arr("keep") }}
	res, events = runPlain(client, db, "clients", "", true, 0, true, "20240101-20240131", nil)
	mustOK(t, res, events)
	if !hasRow(t, db, "clients", "ghost") {
		t.Fatal("--dates --full deleted rows outside the window")
	}
	if !hasSkip(events, "clients", "date_range") {
		t.Fatalf("events=%s", events)
	}
	if len(client.calls) == 0 || client.calls[0].params["dates"] != "20240101-20240131" {
		t.Fatalf("dates was not sent: %+v", client.calls)
	}
}

func TestIgnoredSinceStillPrunes(t *testing.T) {
	db := openDB(t)
	seedRows(t, db, "projects", map[string]any{"id": "keep", "name": "keep"}, map[string]any{"id": "ghost", "name": "ghost"})
	client := &plainClient{body: func(string, map[string]string) json.RawMessage { return arr("keep") }}
	res, events := runPlain(client, db, "projects", "2024-01-01T00:00:00Z", true, 0, true, "", nil)
	mustOK(t, res, events)
	if hasRow(t, db, "projects", "ghost") {
		t.Fatal("--since on a resource with no temporal filter must still prune a complete walk")
	}
	if !strings.Contains(events, "resource_not_incremental") || !hasReconcile(events, "projects") {
		t.Fatalf("events=%s", events)
	}
}

func TestRequiredDateWindowSkipsPrune(t *testing.T) {
	db := openDB(t)
	seedRows(t, db, "transactions", map[string]any{"id": "ghost", "name": "ghost"})
	client := &plainClient{body: func(string, map[string]string) json.RawMessage { return arr("tx-new") }}
	res, events := runPlain(client, db, "transactions", "", true, 0, true, "", nil)
	mustOK(t, res, events)
	if !hasRow(t, db, "transactions", "tx-new") || !hasRow(t, db, "transactions", "ghost") {
		t.Fatal("required date window must keep rows outside the window and still store the response")
	}
	if !hasSkip(events, "transactions", "required_date_window") {
		t.Fatalf("events=%s", events)
	}
	if len(client.calls) == 0 || client.calls[0].params["end_date"] != "2024-01-31" {
		t.Fatalf("end_date default was not sent: %+v", client.calls)
	}
}

func TestGlobalScopeEnvIsolatesCursor(t *testing.T) {
	t.Setenv("SCOPEGUARD_ACCOUNT_ID", "acct-a")
	db := openDB(t)
	params := &syncUserParams{}
	applySyncGlobalScopeEnvDefaults(params)
	client := &scopeClient{body: func(string, map[string]string) json.RawMessage { return envelope("a1", "cur-a") }}
	res, events := runScope(client, db, "clients", "", false, 1, false, "", params)
	mustOK(t, res, events)
	cursor, _, _, err := db.GetSyncState("clients")
	if err != nil || cursor != "cur-a" {
		t.Fatalf("account A cursor=%q err=%v events=%s", cursor, err, events)
	}
	t.Setenv("SCOPEGUARD_ACCOUNT_ID", "acct-b")
	params = &syncUserParams{}
	applySyncGlobalScopeEnvDefaults(params)
	client.calls = nil
	client.body = func(string, map[string]string) json.RawMessage { return envelope("b1", "cur-b") }
	res, events = runScope(client, db, "clients", "", false, 1, false, "", params)
	mustOK(t, res, events)
	if len(client.calls) == 0 || client.calls[0].params["after"] != "" || client.calls[0].params["account_id"] != "acct-b" {
		t.Fatalf("account B reused a cursor: %+v", client.calls)
	}
	cursor, _, _, err = db.GetSyncState("clients")
	if err != nil || cursor != "cur-b" {
		t.Fatalf("account B cursor=%q err=%v", cursor, err)
	}
	t.Setenv("SCOPEGUARD_ACCOUNT_ID", "acct-a")
	params = &syncUserParams{}
	applySyncGlobalScopeEnvDefaults(params)
	client.calls = nil
	client.body = func(string, map[string]string) json.RawMessage { return envelope("a2", "cur-a2") }
	res, events = runScope(client, db, "clients", "", false, 1, false, "", params)
	mustOK(t, res, events)
	if len(client.calls) == 0 || client.calls[0].params["after"] != "" {
		t.Fatalf("switching back reused the other account cursor: %+v", client.calls)
	}
}

func TestTemplateScopeIsolatesCursor(t *testing.T) {
	db := openDB(t)
	client := &scopeClient{
		template: map[string]string{"server": "a"},
		body:     func(string, map[string]string) json.RawMessage { return envelope("a1", "cur-a") },
	}
	res, events := runScope(client, db, "clients", "", false, 1, false, "", nil)
	mustOK(t, res, events)
	cursor, _, _, err := db.GetSyncState("clients")
	if err != nil || cursor != "cur-a" {
		t.Fatalf("tenant A cursor=%q err=%v events=%s", cursor, err, events)
	}
	client.template = map[string]string{"server": "b"}
	client.calls = nil
	client.body = func(string, map[string]string) json.RawMessage { return envelope("b1", "cur-b") }
	res, events = runScope(client, db, "clients", "", false, 1, false, "", nil)
	mustOK(t, res, events)
	if len(client.calls) == 0 || client.calls[0].params["after"] != "" {
		t.Fatalf("tenant B reused tenant A cursor: %+v events=%s", client.calls, events)
	}
	client.dry = true
	client.template = map[string]string{"server": "a"}
	client.calls = nil
	res, events = runScope(client, db, "clients", "", false, 1, false, "", nil)
	mustOK(t, res, events)
	cursor, _, _, err = db.GetSyncState("clients")
	if err != nil || cursor != "cur-b" {
		t.Fatalf("dry-run reset stored cursor to %q err=%v", cursor, err)
	}
	if len(client.calls) == 0 || client.calls[0].params["after"] != "" {
		t.Fatalf("dry-run sent a cursor: %+v", client.calls)
	}
}

func seedModules(t *testing.T, db *store.Store) {
	t.Helper()
	seedRows(t, db, "projects", map[string]any{"id": "p1", "name": "P"})
	seedRows(t, db, "modules",
		map[string]any{"id": "mod-keep", "name": "keep", "project": "p1"},
		map[string]any{"id": "mod-other", "name": "other", "project": "p1"},
	)
}

func TestDependentParamDoesNotPruneSiblings(t *testing.T) {
	db := openDB(t)
	seedModules(t, db)
	dep := modulesDep(t)
	if dep.ReconcileMode != "per_parent" {
		t.Fatalf("reconcile mode %q", dep.ReconcileMode)
	}
	client := &scopeClient{body: func(path string, params map[string]string) json.RawMessage {
		if strings.Contains(path, "/modules") {
			return arr("mod-keep")
		}
		return arr()
	}}
	params := &syncUserParams{perResource: map[string]map[string]string{"modules": {"status": "Online"}}}
	var buf bytes.Buffer
	res := syncDependentResource(context.Background(), client, db, dep, "", true, 0, false, true, "", params, &buf, 1)
	mustOK(t, res, buf.String())
	if !hasRow(t, db, "modules", "mod-other") || !hasRow(t, db, "modules", "mod-keep") {
		t.Fatal("resource-param walk pruned a sibling the filter did not return")
	}
	if !hasSkip(buf.String(), "modules", "resource_param") {
		t.Fatalf("events=%s", buf.String())
	}
	sawStatus := false
	for _, call := range client.calls {
		if call.params["status"] == "Online" {
			sawStatus = true
		}
	}
	if !sawStatus {
		t.Fatalf("resource-param was not sent: %+v", client.calls)
	}
}

func TestDependentUnnarrowedPrunesSibling(t *testing.T) {
	db := openDB(t)
	seedModules(t, db)
	dep := modulesDep(t)
	client := &plainClient{body: func(path string, _ map[string]string) json.RawMessage {
		if strings.Contains(path, "/modules") {
			return arr("mod-keep")
		}
		return arr()
	}}
	var buf bytes.Buffer
	res := syncDependentResource(context.Background(), client, db, dep, "", true, 0, false, true, "", nil, &buf, 1)
	mustOK(t, res, buf.String())
	if hasRow(t, db, "modules", "mod-other") || !hasRow(t, db, "modules", "mod-keep") {
		t.Fatal("un-narrowed per-parent --full must delete the sibling the API no longer returns")
	}
	if !hasReconcile(buf.String(), "modules") {
		t.Fatalf("events=%s", buf.String())
	}
}

func TestDependentFlatParamStillPrunes(t *testing.T) {
	db := openDB(t)
	seedModules(t, db)
	dep := modulesDep(t)
	client := &scopeClient{body: func(path string, params map[string]string) json.RawMessage {
		if strings.Contains(path, "/modules") {
			if params["statuses"] != "" {
				t.Errorf("--param reached a dependent request: %v", params)
			}
			return arr("mod-keep")
		}
		return arr()
	}}
	params := &syncUserParams{flatGlobal: map[string]string{"statuses": "Online"}}
	var buf bytes.Buffer
	res := syncDependentResource(context.Background(), client, db, dep, "", true, 0, false, true, "", params, &buf, 1)
	mustOK(t, res, buf.String())
	if hasRow(t, db, "modules", "mod-other") {
		t.Fatal("--param does not narrow dependent walks, so per-parent prune must still delete the absent sibling")
	}
	if !hasReconcile(buf.String(), "modules") {
		t.Fatalf("events=%s", buf.String())
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "narrowed_prune_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "-count=1", "-run", "Test(ScopeKeyBranches|CheckpointResetAndLegacyUpgrade|LegacyScopeColumnKeepsCursor|ClosedStoreScopeErrors|FullClearKeepsOtherScope|UnnarrowedFullPrunes|VersionPlaceholderStillPrunes|TenantTemplateLeavesOtherTenant|PathContextLeavesOtherNetwork|ParamFilterDoesNotDeleteUnfiltered|GlobalParamSkipsPrune|SinceAndDatesSkipPrune|IgnoredSinceStillPrunes|RequiredDateWindowSkipsPrune|GlobalScopeEnvIsolatesCursor|TemplateScopeIsolatesCursor|DependentParamDoesNotPruneSiblings|DependentUnnarrowedPrunesSibling|DependentFlatParamStillPrunes)$", "./internal/cli")
}

// TestFlatTenantColumnPruneIgnoresTemplateScope proves a base-URL placeholder
// does not disable tenant-column partition prune. The partition is already
// the tenant; rows the API dropped from that partition must still go.
func TestFlatTenantColumnPruneIgnoresTemplateScope(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("flatscope")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.BaseURL = "https://{server}.example.com"
	apiSpec.EndpointTemplateVars = []string{"server"}
	apiSpec.Resources = map[string]spec.Resource{
		"projects": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:            "GET",
					Path:              "/projects",
					Response:          spec.ResponseDef{Type: "array", Item: "Project"},
					Pagination:        &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:           "id",
					TenantScopeColumn: "workspace",
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Project": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "workspace", Type: "string"},
				{Name: "name", Type: "string"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	inlineTest := `package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type flatScopeClient struct {
	items []json.RawMessage
}

func (f *flatScopeClient) Get(context.Context, string, map[string]string) (json.RawMessage, error) {
	payload, _ := json.Marshal(f.items)
	return json.RawMessage(payload), nil
}

func (f *flatScopeClient) RateLimit() float64 { return 0 }

func (f *flatScopeClient) CurrentTemplateVars() map[string]string {
	return map[string]string{"server": "a"}
}

func TestFlatTemplateScopeStillPrunesPartition(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var batch []json.RawMessage
	for _, row := range []map[string]any{
		{"id": "p1", "workspace": "ws-A", "name": "p1"},
		{"id": "p2", "workspace": "ws-A", "name": "p2"},
		{"id": "p3", "workspace": "ws-B", "name": "p3"},
	} {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		batch = append(batch, raw)
	}
	if _, _, err := db.UpsertBatch("projects", batch); err != nil {
		t.Fatal(err)
	}
	keep, _ := json.Marshal(map[string]any{"id": "p1", "workspace": "ws-A", "name": "p1"})
	prev := resolveTenantID
	resolveTenantID = func() string { return "ws-A" }
	defer func() { resolveTenantID = prev }()
	var events bytes.Buffer
	res := syncResource(context.Background(), &flatScopeClient{items: []json.RawMessage{keep}}, db, "projects", "", true, 0, false, true, nil, &events)
	if res.Err != nil || res.Warn != nil {
		t.Fatalf("sync err=%v warn=%v events=%s", res.Err, res.Warn, events.String())
	}
	for _, id := range []string{"p1", "p3"} {
		if _, err := db.Get("projects", id); err != nil {
			t.Fatalf("%s missing: %v events=%s", id, err, events.String())
		}
	}
	if _, err := db.Get("projects", "p2"); err == nil {
		t.Fatalf("p2 was kept; a template var must not disable tenant-column prune events=%s", events.String())
	}
	if !strings.Contains(events.String(), "\"event\":\"reconcile\",") || strings.Contains(events.String(), "tenant_scope") {
		t.Fatalf("events=%s", events.String())
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "flat_template_scope_test.go"), []byte(inlineTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "-count=1", "-run", "^TestFlatTemplateScopeStillPrunesPartition$", "./internal/cli")
}

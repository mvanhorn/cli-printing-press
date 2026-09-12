package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requiredQueryParamsSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Cache.Enabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Response: spec.ResponseDef{Type: "array"}},
			},
		},
		"availability": {
			Description: "Availability scoped by source",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/availability",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{{
						Name:     "source",
						In:       "query",
						Type:     "string",
						Required: true,
						Enum:     []string{"united", "delta", "aeroplan"},
					}},
				},
			},
		},
		"routes": {
			Description: "Route search",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/routes",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "origin_airport", In: "query", Type: "string", Required: true},
						{Name: "destination_airport", In: "query", Type: "string", Required: true},
					},
				},
			},
		},
		"exports": {
			Description: "Exports scoped by format",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/exports",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{{
						Name:     "format",
						In:       "query",
						Type:     "string",
						Required: true,
					}},
				},
			},
		},
		"events": {
			Description: "Events filtered by since",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/events",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{{
						Name:     "since",
						In:       "query",
						Type:     "string",
						Required: true,
					}},
				},
			},
		},
	}
	return apiSpec
}

func TestGeneratedSyncSkipsUnfilledRequiredQueryParams(t *testing.T) {
	t.Parallel()

	apiSpec := requiredQueryParamsSpec("refreshskip")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, "func syncResourceRequiredQueryParams(resource string) []string {")
	assert.Contains(t, syncSrc, "func unfilledRequiredSyncQueryParams(")
	assert.Contains(t, syncSrc, "errMissingRequiredQueryParams")
	assert.Contains(t, syncSrc, "missing_required_params")

	required := generatedFunctionBody(t, syncSrc, "func syncResourceRequiredQueryParams(resource string) []string")
	assert.Contains(t, required, `case "availability":`)
	assert.Contains(t, required, `"source"`)
	assert.Contains(t, required, `"origin_airport"`)
	assert.Contains(t, required, `"destination_airport"`)
	assert.Contains(t, required, `case "exports":`)
	assert.Contains(t, required, `"format"`,
		"required format with no default must stay in the skip guard")
	assert.Contains(t, required, `case "events":`)
	assert.Contains(t, required, `"since"`,
		"required since is conditional and must stay in the skip guard")

	defaults := generatedFunctionBody(t, syncSrc, "func defaultSyncResources() []string")
	assert.Contains(t, defaults, `"items"`)
	assert.NotContains(t, defaults, `"availability"`,
		"required enum filters that are not entity-type selectors stay out of default sync")
	assert.NotContains(t, defaults, `"routes"`)

	autoSrc := readGeneratedFile(t, outputDir, "internal", "cli", "auto_refresh.go")
	assert.Contains(t, autoSrc, "skipped_missing_required_params")
	assert.Contains(t, autoSrc, "unfilledRequiredSyncQueryParams")
	assert.Contains(t, autoSrc, "storedCount == 0",
		"auto-refresh must discard an empty-store watermark before treating since as filled")
	assert.Contains(t, autoSrc, "errors.Is(result.Warn, errMissingRequiredQueryParams)",
		"auto-refresh must not ignore a missing-required-params skip as a successful refresh")

	requireGeneratedCompiles(t, outputDir)

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"` + naming.CLI(apiSpec.Name) + `/internal/cliutil"
	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type requiredParamClient struct {
	got []map[string]string
}

func (c *requiredParamClient) Get(_ context.Context, _ string, params map[string]string) (json.RawMessage, error) {
	copied := map[string]string{}
	for k, v := range params {
		copied[k] = v
	}
	c.got = append(c.got, copied)
	return json.RawMessage(` + "`" + `[{"id":"one"}]` + "`" + `), nil
}

func (*requiredParamClient) RateLimit() float64 { return 0 }

func openRequiredParamStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSyncSkipsWhenRequiredQueryParamsUnfilled(t *testing.T) {
	db := openRequiredParamStore(t)
	client := &requiredParamClient{}

	res := syncResource(context.Background(), client, db, "availability", "", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if !errors.Is(res.Warn, errMissingRequiredQueryParams) {
		t.Fatalf("Warn = %v, want missing required query params", res.Warn)
	}
	if len(client.got) != 0 {
		t.Fatalf("issued %d request(s), want none when required params are unknown", len(client.got))
	}
	if _, last, _, err := db.GetSyncState("availability"); err != nil {
		t.Fatalf("GetSyncState: %v", err)
	} else if !last.IsZero() {
		t.Fatalf("last_synced_at = %v, want unchanged zero after skip", last)
	}
	var checkpoints int
	if err := db.DB().QueryRow("SELECT COUNT(*) FROM sync_state WHERE resource_type='availability'").Scan(&checkpoints); err != nil || checkpoints != 0 {
		t.Fatalf("preflight created checkpoint: count=%d error=%v", checkpoints, err)
	}
	watermark := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.SaveSyncStateAt("availability", "saved-cursor", 7, watermark); err != nil { t.Fatal(err) }
	res = syncResource(context.Background(), client, db, "availability", "", false, 1, false, false, nil, nil)
	if res.Err != nil || !errors.Is(res.Warn, errMissingRequiredQueryParams) || len(client.got) != 0 { t.Fatalf("skip result=%+v calls=%d", res, len(client.got)) }
	cursor, stamp, count, err := db.GetSyncState("availability")
	var complete int
	markerErr := db.DB().QueryRow("SELECT last_attempt_complete FROM sync_state WHERE resource_type='availability'").Scan(&complete)
	if err != nil || markerErr != nil || cursor != "saved-cursor" || !stamp.Equal(watermark) || count != 7 || complete != 1 {
		t.Fatalf("preflight changed checkpoint: %q %s count=%d complete=%d errors=%v/%v", cursor, stamp, count, complete, err, markerErr)
	}
}

func TestSyncSkipsWhenRequiredFormatUnfilled(t *testing.T) {
	db := openRequiredParamStore(t)
	client := &requiredParamClient{}

	res := syncResource(context.Background(), client, db, "exports", "", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if !errors.Is(res.Warn, errMissingRequiredQueryParams) {
		t.Fatalf("Warn = %v, want missing required query params for format", res.Warn)
	}
	if len(client.got) != 0 {
		t.Fatalf("issued %d request(s), want none when required format is unknown", len(client.got))
	}
}

func TestSyncSkipsWhenRequiredSinceUnfilledOnFirstSync(t *testing.T) {
	db := openRequiredParamStore(t)
	client := &requiredParamClient{}

	res := syncResource(context.Background(), client, db, "events", "", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if !errors.Is(res.Warn, errMissingRequiredQueryParams) {
		t.Fatalf("Warn = %v, want missing required since on first sync", res.Warn)
	}
	if len(client.got) != 0 {
		t.Fatalf("issued %d request(s), want none when required since is unknown", len(client.got))
	}
}

func TestSyncSkipsWhenRequiredSinceUnfilledOnFullSync(t *testing.T) {
	db := openRequiredParamStore(t)
	client := &requiredParamClient{}
	if err := db.SaveSyncStateAt("events", "", 1, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("seed sync_state: %v", err)
	}

	res := syncResource(context.Background(), client, db, "events", "", true, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if !errors.Is(res.Warn, errMissingRequiredQueryParams) {
		t.Fatalf("Warn = %v, want missing required since on full sync", res.Warn)
	}
	if len(client.got) != 0 {
		t.Fatalf("issued %d request(s), want none when full sync withholds since", len(client.got))
	}
}

func TestSyncSkipsWhenRequiredSinceUnfilledOnEmptyCache(t *testing.T) {
	db := openRequiredParamStore(t)
	client := &requiredParamClient{}
	if err := db.SaveSyncStateAt("events", "", 1, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("seed empty-cache watermark: %v", err)
	}
	count, err := db.Count("events")
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 0 {
		t.Fatalf("events row count = %d, want 0", count)
	}

	res := syncResource(context.Background(), client, db, "events", "", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if !errors.Is(res.Warn, errMissingRequiredQueryParams) {
		t.Fatalf("Warn = %v, want missing required since when empty store clears the watermark", res.Warn)
	}
	if len(client.got) != 0 {
		t.Fatalf("issued %d request(s), want none when empty cache discards last_synced_at", len(client.got))
	}
}

func TestSyncSendsWhenRequiredSinceFilledFromFlag(t *testing.T) {
	db := openRequiredParamStore(t)
	client := &requiredParamClient{}

	res := syncResource(context.Background(), client, db, "events", "2020-01-02T03:04:05Z", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if res.Warn != nil {
		t.Fatalf("Warn = %v, want success when --since fills required since", res.Warn)
	}
	if len(client.got) == 0 {
		t.Fatal("no request issued")
	}
	if got := client.got[0]["since"]; got == "" {
		t.Fatal("since missing on --since request")
	}
}

func TestSyncSendsWhenRequiredSinceFilledFromWatermark(t *testing.T) {
	db := openRequiredParamStore(t)
	client := &requiredParamClient{}
	watermark := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	if err := db.Upsert("events", "existing", []byte(` + "`" + `{"id":"existing"}` + "`" + `)); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := db.SaveSyncStateAt("events", "", 1, watermark); err != nil {
		t.Fatalf("seed sync_state: %v", err)
	}

	res := syncResource(context.Background(), client, db, "events", "", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if res.Warn != nil {
		t.Fatalf("Warn = %v, want success when since comes from last_synced_at", res.Warn)
	}
	if len(client.got) == 0 {
		t.Fatal("no request issued")
	}
	if got := client.got[0]["since"]; got == "" {
		t.Fatal("since missing on incremental request")
	}
}

func TestSyncSendsWhenRequiredQueryParamsFilled(t *testing.T) {
	db := openRequiredParamStore(t)
	client := &requiredParamClient{}
	userParams, err := parseSyncUserParams([]string{"source=united"}, nil, nil)
	if err != nil {
		t.Fatalf("parseSyncUserParams: %v", err)
	}

	res := syncResource(context.Background(), client, db, "availability", "", false, 1, false, false, userParams, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if res.Warn != nil {
		t.Fatalf("Warn = %v, want success when required params are filled", res.Warn)
	}
	if len(client.got) == 0 {
		t.Fatal("no request issued")
	}
	if got := client.got[0]["source"]; got != "united" {
		t.Fatalf("source = %q, want united", got)
	}
}

func TestSyncWithoutRequiredParamsStillRequests(t *testing.T) {
	db := openRequiredParamStore(t)
	client := &requiredParamClient{}

	res := syncResource(context.Background(), client, db, "items", "", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource error: %v", res.Err)
	}
	if len(client.got) == 0 {
		t.Fatal("no request issued for a resource with no required query params")
	}
}

func TestAutoRefreshSkipsMissingRequiredParamsHonestly(t *testing.T) {
	home := t.TempDir()
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("set home override: %v", err)
	}
	defer restore()

	dbPath := defaultDBPath("refreshskip-pp-cli")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	staleAt := time.Now().UTC().Add(-200 * time.Hour)
	if err := db.SaveSyncStateAt("availability", "", 1, staleAt); err != nil {
		t.Fatalf("seed sync_state: %v", err)
	}
	db.Close()

	meta := autoRefreshIfStale(context.Background(), &rootFlags{dataSource: "auto"}, []string{"availability"})
	if meta.Ran {
		t.Fatalf("Ran = true, want false when required params are unknown")
	}
	if meta.Reason != "skipped_missing_required_params" {
		t.Fatalf("Reason = %q, want skipped_missing_required_params (must not claim refreshed)", meta.Reason)
	}

	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer db.Close()
	_, last, _, err := db.GetSyncState("availability")
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if last.UTC().Truncate(time.Second) != staleAt.Truncate(time.Second) {
		t.Fatalf("last_synced_at = %v, want %v (skip must not rewrite freshness)", last, staleAt)
	}
}

func TestAutoRefreshSkipsEntireSetWhenAnyResourceMissingParams(t *testing.T) {
	home := t.TempDir()
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("set home override: %v", err)
	}
	defer restore()

	dbPath := defaultDBPath("refreshskip-pp-cli")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	staleAt := time.Now().UTC().Add(-200 * time.Hour)
	if err := db.SaveSyncStateAt("items", "", 1, staleAt); err != nil {
		t.Fatalf("seed items sync_state: %v", err)
	}
	if err := db.SaveSyncStateAt("availability", "", 1, staleAt); err != nil {
		t.Fatalf("seed availability sync_state: %v", err)
	}
	db.Close()

	meta := autoRefreshIfStale(context.Background(), &rootFlags{dataSource: "auto"}, []string{"items", "availability"})
	if meta.Ran {
		t.Fatalf("Ran = true, want false when any resource is missing required params")
	}
	if meta.Reason != "skipped_missing_required_params" {
		t.Fatalf("Reason = %q, want skipped_missing_required_params", meta.Reason)
	}

	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer db.Close()
	_, itemsLast, _, err := db.GetSyncState("items")
	if err != nil {
		t.Fatalf("GetSyncState items: %v", err)
	}
	if itemsLast.UTC().Truncate(time.Second) != staleAt.Truncate(time.Second) {
		t.Fatalf("items last_synced_at = %v, want %v (mixed skip must not refresh fillable siblings)", itemsLast, staleAt)
	}
}

func TestAutoRefreshDoesNotClaimRefreshWhenEmptyCacheClearsWatermark(t *testing.T) {
	home := t.TempDir()
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("set home override: %v", err)
	}
	defer restore()

	dbPath := defaultDBPath("refreshskip-pp-cli")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	staleAt := time.Now().UTC().Add(-200 * time.Hour)
	if err := db.SaveSyncStateAt("events", "", 1, staleAt); err != nil {
		t.Fatalf("seed empty-cache watermark: %v", err)
	}
	count, err := db.Count("events")
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 0 {
		t.Fatalf("events row count = %d, want 0", count)
	}
	db.Close()

	meta := autoRefreshIfStale(context.Background(), &rootFlags{dataSource: "auto"}, []string{"events"})
	if meta.Ran {
		t.Fatal("auto-refresh claimed Ran with an empty cache and no --since")
	}
	if meta.Reason != "skipped_missing_required_params" {
		t.Fatalf("Reason = %q, want skipped_missing_required_params (must not claim refreshed)", meta.Reason)
	}

	db, err = store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer db.Close()
	_, last, _, err := db.GetSyncState("events")
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if last.UTC().Truncate(time.Second) != staleAt.Truncate(time.Second) {
		t.Fatalf("last_synced_at = %v, want unchanged %v", last, staleAt)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_required_query_params_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestSync(SkipsWhenRequiredQueryParamsUnfilled|SkipsWhenRequiredFormatUnfilled|SkipsWhenRequiredSinceUnfilledOnFirstSync|SkipsWhenRequiredSinceUnfilledOnFullSync|SkipsWhenRequiredSinceUnfilledOnEmptyCache|SendsWhenRequiredSinceFilledFromFlag|SendsWhenRequiredSinceFilledFromWatermark|SendsWhenRequiredQueryParamsFilled|WithoutRequiredParamsStillRequests)|TestAutoRefresh(SkipsMissingRequiredParamsHonestly|SkipsEntireSetWhenAnyResourceMissingParams|DoesNotClaimRefreshWhenEmptyCacheClearsWatermark)")
}

func TestGeneratedSyncOmitsRequiredQueryHelperWhenNone(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("noreqparams")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.NotContains(t, syncSrc, "syncResourceRequiredQueryParams",
		"the required-param lookup and its call sites must be gated on the same condition")
	assert.NotContains(t, syncSrc, "errMissingRequiredQueryParams")
}

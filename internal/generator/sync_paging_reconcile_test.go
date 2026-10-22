package generator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two incomplete flat walks (cursor_unavailable and stuck_page) are the warned
// pair. A later OData page that stores every row is a successful sync.
func TestGeneratedSyncIncompleteWalkAndODataPaging(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/keyset-devices":
			_, _ = w.Write(jsonIDPage("key", 0, 100))
		case "/stuck-pages":
			_, _ = w.Write(jsonIDPage("stuck", 0, 100))
		case "/short-pages":
			_, _ = w.Write(jsonIDPage("short", 0, 3))
		case "/bounded-pages":
			_, _ = w.Write(jsonHasMore(jsonIDPage("bound", 0, 100), false))
		case "/continuing-pages":
			start, _ := strconv.Atoi(q.Get("offset"))
			_, _ = w.Write(jsonIDPage("cont", start, 100))
		case "/audit-logs", "/dollar-logs":
			start, end, ok := odataWindow(q, 250)
			if !ok {
				_, _ = w.Write(jsonIDPage("partial", 0, 1))
				return
			}
			_, _ = w.Write(jsonIDPage("log", start, end-start))
		case "/counted-rows":
			start, _ := strconv.Atoi(q.Get("offset"))
			size, _ := strconv.Atoi(q.Get("count"))
			if size <= 0 {
				size = 1
			}
			if start+size > 150 {
				size = 150 - start
			}
			if size < 0 {
				size = 0
			}
			_, _ = w.Write(jsonIDPage("counted", start, size))
		case "/bool-count", "/cursor-only":
			_, _ = w.Write(jsonIDPage("tiny", 0, 2))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	api := mustParseOpenAPI(t, pagingOpenAPI)
	api.Name = "syncpagebugs"
	api.Auth = spec.AuthConfig{Type: "none"}
	api.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/syncpagebugs-pp-cli/config.toml"}
	profile := profiler.Profile(api)
	names := syncableNamesByPath(profile)

	outputDir, binary := buildGeneratedCLI(t, api)
	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assertPaginationCase(t, syncSrc, names["/offsets"], "offset", "offset", "limit")
	assertPaginationCase(t, syncSrc, names["/pages"], "page", "page", "per_page")
	assertPaginationCase(t, syncSrc, names["/cursors"], "after", "cursor", "limit")

	env := syncBugCLIEnv(t, api.Name, srv.URL)
	incomplete, incompleteErr := runSync(t, binary, env, "--resources", names["/keyset-devices"]+","+names["/stuck-pages"], "--db", filepath.Join(t.TempDir(), "incomplete.db"))
	require.NotZero(t, incompleteErr, "stderr:\n%s", incomplete)
	warned, success, completes := syncSummary(t, incomplete)
	assert.Equal(t, 2, warned, incomplete)
	assert.Equal(t, 0, success, incomplete)
	assert.Empty(t, completes, incomplete)
	assert.Contains(t, incomplete, "cursor_unavailable")
	assert.Contains(t, incomplete, "stuck_page")

	short, shortCode := runSync(t, binary, env, "--resources", names["/short-pages"], "--db", filepath.Join(t.TempDir(), "short.db"))
	require.Zero(t, shortCode, short)
	warned, _, completes = syncSummary(t, short)
	assert.Equal(t, 0, warned, short)
	assert.NotEmpty(t, completes, short)

	bounded, boundedCode := runSync(t, binary, env, "--resources", names["/bounded-pages"], "--db", filepath.Join(t.TempDir(), "bounded.db"))
	require.Zero(t, boundedCode, bounded)
	warned, _, completes = syncSummary(t, bounded)
	assert.Equal(t, 0, warned, bounded)
	assert.NotEmpty(t, completes, bounded)

	capped, cappedCode := runSync(t, binary, env, "--resources", names["/continuing-pages"], "--max-pages", "1", "--db", filepath.Join(t.TempDir(), "capped.db"))
	require.Zero(t, cappedCode, capped)
	warned, success, completes = syncSummary(t, capped)
	assert.Equal(t, 0, warned, capped)
	assert.Equal(t, 1, success, capped)
	assert.NotEmpty(t, completes, capped)

	mu.Lock()
	queries = nil
	mu.Unlock()
	odata, odataCode := runSync(t, binary, env, "--resources", names["/audit-logs"]+","+names["/dollar-logs"], "--db", filepath.Join(t.TempDir(), "odata.db"))
	require.Zero(t, odataCode, odata)
	_, success, _ = syncSummary(t, odata)
	assert.Equal(t, 2, success, odata)
	assert.Contains(t, odata, `"total_records":500`)
	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	assert.True(t, queryValue(got, "/audit-logs", "$top", "100"), "requests:\n%s", strings.Join(got, "\n"))
	assert.True(t, queryValue(got, "/audit-logs", "$skip", "100"), "requests:\n%s", strings.Join(got, "\n"))
	assert.True(t, queryValue(got, "/dollar-logs", "$top", "100"), "requests:\n%s", strings.Join(got, "\n"))
	assert.True(t, queryValue(got, "/dollar-logs", "$skip", "100"), "requests:\n%s", strings.Join(got, "\n"))
	assert.False(t, queryKeyPresent(got, "count") || queryKeyPresent(got, "$count"), "requests:\n%s", strings.Join(got, "\n"))
	for _, line := range got {
		u, err := url.Parse(line)
		require.NoError(t, err)
		_, empty := u.Query()[""]
		assert.False(t, empty, "empty query key in %s", line)
	}

	mu.Lock()
	queries = nil
	mu.Unlock()
	counted, countedCode := runSync(t, binary, env, "--resources", names["/counted-rows"], "--db", filepath.Join(t.TempDir(), "counted.db"))
	require.Zero(t, countedCode, counted)
	assert.Contains(t, counted, `"total_records":150`)
	mu.Lock()
	got = append([]string(nil), queries...)
	mu.Unlock()
	assert.True(t, queryValue(got, "/counted-rows", "count", "100"), "requests:\n%s", strings.Join(got, "\n"))

	mu.Lock()
	queries = nil
	mu.Unlock()
	cursorOnly, cursorCode := runSync(t, binary, env, "--resources", names["/cursor-only"]+","+names["/bool-count"], "--db", filepath.Join(t.TempDir(), "cursor.db"))
	require.Zero(t, cursorCode, cursorOnly)
	mu.Lock()
	got = append([]string(nil), queries...)
	mu.Unlock()
	assert.False(t, queryKeyPresent(got, "count") || queryKeyPresent(got, "$count"), "requests:\n%s", strings.Join(got, "\n"))
	for _, line := range got {
		u, err := url.Parse(line)
		require.NoError(t, err)
		_, empty := u.Query()[""]
		assert.False(t, empty, "empty query key in %s", line)
	}
}

func TestGeneratedSyncReconcileUsesStampedParentID(t *testing.T) {
	t.Parallel()

	var noteA, pingA int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/devices":
			_, _ = w.Write([]byte(`[{"id":"dev-a"},{"id":"dev-b"}]`))
		case "/devices/dev-a/notifications":
			noteA++
			if noteA == 1 {
				_, _ = w.Write([]byte(`{"Data":[{"id":"n1","title":"one"},{"id":"n2","title":"two"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"Data":[{"id":"n1","title":"one"}]}`))
		case "/devices/dev-b/notifications":
			_, _ = w.Write([]byte(`{"Data":[{"id":"n3","title":"three"},{"id":"n4","title":"four"}]}`))
		case "/devices/dev-a/pings":
			pingA++
			if pingA == 1 {
				_, _ = w.Write([]byte(`[{"id":"p1","device":"dev-a"},{"id":"p2","device":"dev-a"}]`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":"p1","device":"dev-a"}]`))
		case "/devices/dev-b/pings":
			_, _ = w.Write([]byte(`[{"id":"p3","device":"dev-b"},{"id":"p4","device":"dev-b"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	api := mustParseOpenAPI(t, reconcileOpenAPI)
	api.Name = "syncrecon"
	api.Auth = spec.AuthConfig{Type: "none"}
	api.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/syncrecon-pp-cli/config.toml"}
	profile := profiler.Profile(api)
	devices := syncableNamesByPath(profile)["/devices"]
	require.NotEmpty(t, devices)
	notes := dependentByPath(profile, "/devices/{deviceId}/notifications")
	pings := dependentByPath(profile, "/devices/{deviceId}/pings")
	require.NotEmpty(t, notes)
	require.NotEmpty(t, pings)

	outputDir, binary := buildGeneratedCLI(t, api)
	env := syncBugCLIEnv(t, api.Name, srv.URL)
	db := filepath.Join(t.TempDir(), "recon.db")
	first, firstCode := runSync(t, binary, env, "--full", "--resources", devices, "--db", db)
	require.Zero(t, firstCode, first)
	second, secondCode := runSync(t, binary, env, "--full", "--resources", devices, "--db", db)
	require.Zero(t, secondCode, second)

	deleted, ok := reconcileDeleted(second, notes, "dev-a")
	require.True(t, ok, second)
	assert.Equal(t, 1, deleted, second)
	deleted, ok = reconcileDeleted(second, notes, "dev-b")
	require.True(t, ok, second)
	assert.Equal(t, 0, deleted, second)
	deleted, ok = reconcileDeleted(second, pings, "dev-a")
	require.True(t, ok, second)
	assert.Equal(t, 1, deleted, second)

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	assert.Contains(t, syncSrc, "GenericScopeJSONPath: \"$.parent_id\"")
	assert.Contains(t, syncSrc, "GenericScopeJSONPath: \"$.device\"")
}

func TestGeneratedSyncSkipsUnbindableTwoPlaceholderChild(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/scripts":
			_, _ = w.Write([]byte(`[{"id":"s1"}]`))
		case "/orgs":
			_, _ = w.Write([]byte(`[{"id":"org-1","devices_id":"dev-9"}]`))
		default:
			_, _ = w.Write([]byte(`[{"id":"child-1"}]`))
		}
	}))
	t.Cleanup(srv.Close)

	api := mustParseOpenAPI(t, fanoutOpenAPI)
	api.Name = "syncfanout"
	api.Auth = spec.AuthConfig{Type: "none"}
	api.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/syncfanout-pp-cli/config.toml"}

	_, binary := buildGeneratedCLI(t, api)
	env := syncBugCLIEnv(t, api.Name, srv.URL)
	out, code := runSync(t, binary, env, "--db", filepath.Join(t.TempDir(), "fanout.db"))
	require.Zero(t, code, out)
	mu.Lock()
	got := append([]string(nil), paths...)
	mu.Unlock()
	joined := strings.Join(got, "\n")
	assert.Contains(t, joined, "/scripts/s1/runs", joined)
	assert.Contains(t, joined, "/orgs/org-1/devices/dev-9/alerts", joined)
	assert.NotContains(t, joined, "executions", joined)
}

func TestGeneratedSyncCustomWireNameKeepsOffsetAndPageClass(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var queries []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		queries = append(queries, q)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		n := 2
		switch r.URL.Path {
		case "/starts":
			if q.Get("start") != "" {
				n = 1
			}
		case "/indexes":
			if q.Get("pageIndex") != "" {
				n = 1
			}
		default:
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(jsonIDPage(strings.TrimPrefix(r.URL.Path, "/"), 0, n))
	}))
	t.Cleanup(srv.Close)

	api := minimalSpec("wireclass")
	api.Auth = spec.AuthConfig{Type: "none"}
	api.BaseURL = srv.URL
	api.Resources = map[string]spec.Resource{
		"starts": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/starts",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "offset", In: "query", Type: "integer", URLName: "start"},
						{Name: "limit", In: "query", Type: "integer", Default: 2},
					},
				},
			},
		},
		"indexes": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/indexes",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "page", In: "query", Type: "integer", URLName: "pageIndex"},
						{Name: "per_page", In: "query", Type: "integer", URLName: "pageSize", Default: 2},
					},
				},
			},
		},
	}

	_, binary := buildGeneratedCLI(t, api)
	env := syncBugCLIEnv(t, api.Name, srv.URL)
	db := filepath.Join(t.TempDir(), "sync.db")
	out, code := runSync(t, binary, env, "--db", db)
	require.Equal(t, 0, code, out)
	warned, success, completes := syncSummary(t, out)
	assert.Equal(t, 0, warned, out)
	assert.Equal(t, 2, success, out)
	assert.Len(t, completes, 2, out)

	mu.Lock()
	defer mu.Unlock()
	var sawStart, sawPage bool
	for _, q := range queries {
		if _, ok := q["offset"]; ok {
			t.Errorf("request used the flag name offset: %v", q)
		}
		if _, ok := q["page"]; ok {
			t.Errorf("request used the flag name page: %v", q)
		}
		if q.Get("start") == "2" {
			sawStart = true
		}
		if q.Get("pageIndex") == "2" {
			sawPage = true
		}
	}
	assert.True(t, sawStart, "offset walk did not send start=2: %v", queries)
	assert.True(t, sawPage, "page walk did not send pageIndex=2: %v", queries)
}

func mustParseOpenAPI(t *testing.T, doc string) *spec.APISpec {
	t.Helper()
	api, err := openapi.Parse([]byte(doc))
	require.NoError(t, err)
	return api
}

func syncableNamesByPath(profile *profiler.APIProfile) map[string]string {
	out := map[string]string{}
	for _, resource := range profile.SyncableResources {
		out[resource.Path] = resource.Name
	}
	return out
}

func dependentByPath(profile *profiler.APIProfile, path string) string {
	for _, dep := range profile.DependentSyncResources {
		if dep.Path == path {
			return dep.Name
		}
	}
	return ""
}

func buildGeneratedCLI(t *testing.T, api *spec.APISpec) (string, string) {
	t.Helper()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(api.Name))
	require.NoError(t, New(api, outputDir).Generate())
	runGoCommand(t, outputDir, "mod", "tidy")
	binary := filepath.Join(outputDir, naming.CLI(api.Name))
	runGoCommand(t, outputDir, "build", "-o", binary, "./cmd/"+naming.CLI(api.Name))
	runGoCommand(t, outputDir, "build", "./...")
	return outputDir, binary
}

func syncBugCLIEnv(t *testing.T, name, baseURL string) []string {
	t.Helper()
	home := t.TempDir()
	for _, sub := range []string{".config", ".local/share", ".local/state", ".cache"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, sub), 0o755))
	}
	env := isolatedCLIEnv(t, home, naming.EnvPrefix(name)+"_BASE_URL="+baseURL)
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "PRINTING_PRESS_") {
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}

func runSync(t *testing.T, binary string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(binary, append([]string{"sync", "--json", "--no-learn"}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "sync failed to start: %v\n%s", err, out)
	return string(out), exitErr.ExitCode()
}

func syncSummary(t *testing.T, out string) (warned, success int, completes []string) {
	t.Helper()
	var saw bool
	for _, ev := range ndjsonEvents(out) {
		switch ev["event"] {
		case "sync_summary":
			saw = true
			warned = jsonInt(ev["warned"])
			success = jsonInt(ev["success"])
		case "sync_complete":
			completes = append(completes, fmt.Sprint(ev["resource"]))
		}
	}
	require.True(t, saw, "missing sync_summary in:\n%s", out)
	return warned, success, completes
}

func reconcileDeleted(out, resource, scope string) (int, bool) {
	for _, ev := range ndjsonEvents(out) {
		if ev["event"] != "reconcile" || ev["resource"] != resource || ev["scope"] != scope {
			continue
		}
		return jsonInt(ev["deleted"]), true
	}
	return 0, false
}

func ndjsonEvents(out string) []map[string]any {
	var events []map[string]any
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) == nil {
			events = append(events, ev)
		}
	}
	return events
}

func jsonInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func assertPaginationCase(t *testing.T, src, resource, cursor, cursorType, limit string) {
	t.Helper()
	require.NotEmpty(t, resource)
	idx := strings.Index(src, "case \""+resource+"\":")
	require.NotEqual(t, -1, idx, "missing pagination case for %s", resource)
	end := strings.Index(src[idx:], "\n\tcase ")
	if end < 0 {
		end = strings.Index(src[idx:], "\n\tdefault:")
	}
	require.Positive(t, end)
	block := src[idx : idx+end]
	assert.Regexp(t, regexp.MustCompile(`cursorParam:\s+"`+regexp.QuoteMeta(cursor)+`"`), block)
	assert.Regexp(t, regexp.MustCompile(`cursorType:\s+"`+regexp.QuoteMeta(cursorType)+`"`), block)
	assert.Regexp(t, regexp.MustCompile(`limitParam:\s+"`+regexp.QuoteMeta(limit)+`"`), block)
	assert.Regexp(t, regexp.MustCompile(`limit:\s+100`), block)
}

func queryValue(lines []string, path, key, want string) bool {
	for _, line := range lines {
		u, err := url.Parse(line)
		if err != nil || u.Path != path {
			continue
		}
		if u.Query().Get(key) == want {
			return true
		}
	}
	return false
}

func queryKeyPresent(lines []string, key string) bool {
	for _, line := range lines {
		u, err := url.Parse(line)
		if err != nil {
			continue
		}
		if _, ok := u.Query()[key]; ok {
			return true
		}
	}
	return false
}

func jsonIDPage(prefix string, start, n int) []byte {
	var b strings.Builder
	b.WriteByte('[')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":"%s-%d"}`, prefix, start+i)
	}
	b.WriteByte(']')
	return []byte(b.String())
}

func jsonHasMore(items []byte, hasMore bool) []byte {
	flag := "false"
	if hasMore {
		flag = "true"
	}
	return []byte(`{"data":` + string(items) + `,"has_more":` + flag + `}`)
}

func odataWindow(q url.Values, total int) (start, end int, ok bool) {
	raw := q.Get("$top")
	if raw == "" {
		return 0, 0, false
	}
	size, err := strconv.Atoi(raw)
	if err != nil || size <= 0 {
		return 0, 0, false
	}
	start, _ = strconv.Atoi(q.Get("$skip"))
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end = min(start+size, total)
	return start, end, true
}

const pagingOpenAPI = `openapi: 3.0.3
info:
  title: Sync Page Bugs
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /keyset-devices:
    get:
      parameters:
        - {name: pageSize, in: query, schema: {type: integer}}
        - {name: after, in: query, schema: {type: string}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /stuck-pages:
    get:
      parameters:
        - {name: limit, in: query, schema: {type: integer}}
        - {name: offset, in: query, schema: {type: integer}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /short-pages:
    get:
      parameters:
        - {name: limit, in: query, schema: {type: integer}}
        - {name: offset, in: query, schema: {type: integer}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /bounded-pages:
    get:
      parameters:
        - {name: pageSize, in: query, schema: {type: integer}}
        - {name: after, in: query, schema: {type: string}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /continuing-pages:
    get:
      parameters:
        - {name: limit, in: query, schema: {type: integer}}
        - {name: offset, in: query, schema: {type: integer}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /audit-logs:
    get:
      parameters:
        - {name: top, in: query, schema: {type: integer}, x-url-name: "$top"}
        - {name: skip, in: query, schema: {type: integer}, x-url-name: "$skip"}
        - {name: count, in: query, schema: {type: boolean}, x-url-name: "$count"}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /dollar-logs:
    get:
      parameters:
        - {name: "$top", in: query, schema: {type: integer}}
        - {name: "$skip", in: query, schema: {type: integer}}
        - {name: "$.xgafv", in: query, schema: {type: string}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /counted-rows:
    get:
      parameters:
        - {name: count, in: query, schema: {type: integer}}
        - {name: offset, in: query, schema: {type: integer}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /bool-count:
    get:
      parameters:
        - {name: count, in: query, schema: {type: boolean}}
        - {name: offset, in: query, schema: {type: integer}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /cursor-only:
    get:
      parameters:
        - {name: after, in: query, schema: {type: string}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /offsets:
    get:
      parameters:
        - {name: limit, in: query, schema: {type: integer}}
        - {name: offset, in: query, schema: {type: integer}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /pages:
    get:
      parameters:
        - {name: page, in: query, schema: {type: integer}}
        - {name: per_page, in: query, schema: {type: integer}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
  /cursors:
    get:
      parameters:
        - {name: after, in: query, schema: {type: string}}
        - {name: limit, in: query, schema: {type: integer}}
      responses: { "200": {description: ok, content: {application/json: {schema: {type: array, items: {$ref: "#/components/schemas/Row"}}}}}}
components:
  schemas:
    Row:
      type: object
      properties:
        id: {type: string}
`

const reconcileOpenAPI = `openapi: 3.0.3
info:
  title: Sync Reconcile
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /devices:
    get:
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
                    id: {type: string}
  /devices/{deviceId}/notifications:
    get:
      x-pp-resource: devices
      parameters:
        - {name: deviceId, in: path, required: true, schema: {type: string}}
        - {name: limit, in: query, schema: {type: integer}}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  Data:
                    type: array
                    items:
                      type: object
                      properties:
                        id: {type: string}
                        title: {type: string}
  /devices/{deviceId}/pings:
    get:
      x-pp-resource: devices
      parameters:
        - {name: deviceId, in: path, required: true, schema: {type: string}}
        - {name: limit, in: query, schema: {type: integer}}
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
                    id: {type: string}
                    device: {type: string}
`

const fanoutOpenAPI = `openapi: 3.0.3
info:
  title: Sync Fanout
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /scripts:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: array, items: {type: object, properties: {id: {type: string}}}}
  /scripts/{scriptId}/runs:
    get:
      parameters:
        - {name: scriptId, in: path, required: true, schema: {type: string}}
        - {name: limit, in: query, schema: {type: integer}}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: array, items: {type: object, properties: {id: {type: string}}}}
  /scripts/{scriptId}/device/{deviceId}/executions:
    get:
      parameters:
        - {name: scriptId, in: path, required: true, schema: {type: string}}
        - {name: deviceId, in: path, required: true, schema: {type: string}}
        - {name: limit, in: query, schema: {type: integer}}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: array, items: {type: object, properties: {id: {type: string}}}}
  /orgs:
    get:
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
                    id: {type: string}
                    devices_id: {type: string}
  /orgs/{orgId}/devices/{deviceId}/alerts:
    get:
      x-pp-sync-walker:
        parent: orgs
        key_param: orgId
        key_field: id
      parameters:
        - {name: orgId, in: path, required: true, schema: {type: string}}
        - {name: deviceId, in: path, required: true, schema: {type: string}}
        - {name: limit, in: query, schema: {type: integer}}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: array, items: {type: object, properties: {id: {type: string}}}}
`

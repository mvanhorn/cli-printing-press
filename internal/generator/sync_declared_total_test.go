package generator

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// A declared row count plus the distinct ids storage would keep proves a
// cursor walk ended. The count is not an absence proof, so prune stays off.
func TestGeneratedSyncDeclaredTotalProvesCursorEnd(t *testing.T) {
	t.Parallel()

	flat := flatCountCases()
	reports := reportCountPhases()
	h := newCountEndHandler(flat, reports)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	api := mustParseOpenAPI(t, countEndOpenAPI)
	api.Name = "synccountend"
	api.Auth = spec.AuthConfig{Type: "none"}
	api.Config = spec.ConfigSpec{Format: "toml", Path: "~/.config/synccountend-pp-cli/config.toml"}

	profile := profiler.Profile(api)
	rows := namesByPath(profile, "/rows")
	require.Equal(t, "page_token", rows.PaginationCursorType)
	require.Equal(t, 10, rows.PaginationPageSize)
	require.Equal(t, profiler.ReconcileModeFlatGlobal, rows.ReconcileMode)
	require.NotEmpty(t, rows.IDField)
	accounts := namesByPath(profile, "/accounts")
	require.NotEmpty(t, accounts.Name)
	reportName := dependentByPath(profile, "/accounts/{accountId}/reports")
	require.NotEmpty(t, reportName)
	var report profiler.DependentResource
	for _, dep := range profile.DependentSyncResources {
		if dep.Path == "/accounts/{accountId}/reports" {
			report = dep
			break
		}
	}
	require.Equal(t, "page_token", report.PaginationCursorType)
	require.Equal(t, 10, report.PaginationPageSize)
	require.Equal(t, profiler.ReconcileModePerParent, report.ReconcileMode)

	_, binary := buildGeneratedCLI(t, api)
	env := syncBugCLIEnv(t, api.Name, srv.URL)

	syncRows := func(t *testing.T, dbPath string, phase int, extra ...string) (string, int) {
		t.Helper()
		h.setPhase("/rows", phase)
		args := append([]string{"--db", dbPath, "--full", "--resources", rows.Name}, extra...)
		return runSync(t, binary, env, args...)
	}

	for i, tc := range flat {
		t.Run(tc.name, func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "sync.db")
			out, code := syncRows(t, db, i)
			if tc.name == "missing ids" {
				assert.NotZero(t, code, out)
				assert.Zero(t, attemptComplete(t, db, rows.Name))
				return
			}
			if tc.complete {
				assertCountComplete(t, out, code, db, rows.Name, tc.ids)
				if tc.skipReason != "" {
					assert.Equal(t, tc.skipReason, reconcileSkip(t, out, rows.Name))
				}
				if tc.deleted >= 0 {
					deleted, ok := reconcileDeleted(out, rows.Name, "*")
					assert.True(t, ok, "prune did not run:\n%s", out)
					assert.Equal(t, tc.deleted, deleted)
				}
				return
			}
			assertCountIncomplete(t, out, code, db, rows.Name)
		})
	}

	full := phaseNamed(flat, "full last page")
	t.Run("limit requested is the declared maximum", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "sync.db")
		_, _ = syncRows(t, db, full)
		assertLimit(t, h, "/rows", "10")
	})

	t.Run("count proven prune keeps unseen rows", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "sync.db")
		out, code := syncRows(t, db, full)
		assertCountComplete(t, out, code, db, rows.Name, idTexts(1, 20))

		out, code = syncRows(t, db, phaseNamed(flat, "prune gap"))
		assertCountComplete(t, out, code, db, rows.Name, idTexts(1, 30))
		assert.Equal(t, "end_proven_by_count", reconcileSkip(t, out, rows.Name))
		_, pruned := reconcileDeleted(out, rows.Name, "*")
		assert.False(t, pruned, "count-proven walk deleted rows:\n%s", out)
	})

	t.Run("explicit has more false still prunes", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "sync.db")
		out, code := syncRows(t, db, full)
		assertCountComplete(t, out, code, db, rows.Name, idTexts(1, 20))

		out, code = syncRows(t, db, phaseNamed(flat, "explicit has more false"))
		assertCountComplete(t, out, code, db, rows.Name, idTexts(1, 10))
		deleted, ok := reconcileDeleted(out, rows.Name, "*")
		assert.True(t, ok, "natural end skipped prune:\n%s", out)
		assert.Equal(t, 10, deleted)
	})

	t.Run("max pages one does not finish the walk", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "sync.db")
		out, code := syncRows(t, db, full, "--max-pages", "1")
		require.Zero(t, code, out)
		assert.Zero(t, attemptComplete(t, db, rows.Name))
		assert.Equal(t, idTexts(1, 10), storedResourceIDs(t, db, rows.Name))
		assert.Contains(t, out, "max_pages_cap")
	})

	t.Run("max pages lands on a count proven last page", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "sync.db")
		out, code := syncRows(t, db, full, "--max-pages", "2")
		assertCountComplete(t, out, code, db, rows.Name, idTexts(1, 20))
		assert.NotContains(t, out, "max_pages_cap_hit")
	})

	syncReports := func(t *testing.T, dbPath string, phase int, extra ...string) (string, int) {
		t.Helper()
		h.setPhase("/accounts/acct-1/reports", phase)
		args := append([]string{"--db", dbPath, "--full", "--resources", accounts.Name + "," + reportName}, extra...)
		return runSync(t, binary, env, args...)
	}

	t.Run("dependent count proven end skips prune", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "sync.db")
		out, code := syncReports(t, db, 0)
		assertCountComplete(t, out, code, db, reportName, idTexts(1, 20))
		assert.Equal(t, "end_proven_by_count", reconcileSkip(t, out, reportName))

		out, code = syncReports(t, db, 1)
		assertCountComplete(t, out, code, db, reportName, idTexts(1, 30))
		assert.Equal(t, "end_proven_by_count", reconcileSkip(t, out, reportName))
		_, pruned := reconcileDeleted(out, reportName, "acct-1")
		assert.False(t, pruned, "dependent prune deleted rows:\n%s", out)
	})

	t.Run("dependent max pages", func(t *testing.T) {
		capped := filepath.Join(t.TempDir(), "capped.db")
		out, code := syncReports(t, capped, 0, "--max-pages", "1")
		require.Zero(t, code, out)
		assert.Zero(t, attemptComplete(t, capped, reportName))
		assert.Equal(t, idTexts(1, 10), storedResourceIDs(t, capped, reportName))
		assert.Contains(t, out, "max_pages_cap")

		finished := filepath.Join(t.TempDir(), "finished.db")
		out, code = syncReports(t, finished, 0, "--max-pages", "2")
		assertCountComplete(t, out, code, finished, reportName, idTexts(1, 20))
	})
}

type countCase struct {
	name       string
	pages      []countPage
	complete   bool
	ids        []string
	skipReason string
	deleted    int
}

func flatCountCases() []countCase {
	return []countCase{
		{name: "full last page", pages: countPages("pagination", "total_count", "20", idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), skipReason: "end_proven_by_count", deleted: -1},
		{name: "one page exceeds requested limit", pages: []countPage{counted("", "", idRange(1, 25), "pagination", "total_count", "25")}, complete: true, ids: idTexts(1, 25), skipReason: "end_proven_by_count", deleted: -1},
		{name: "duplicates stay incomplete", pages: countPages("pagination", "total_count", "20", idRange(1, 10), idRange(6, 15)), deleted: -1},
		{name: "moved count stays incomplete", pages: []countPage{counted("", "p2", idRange(1, 10), "pagination", "total_count", "30"), counted("p2", "", idRange(21, 30), "pagination", "total_count", "20")}, deleted: -1},
		{name: "prune gap", pages: countPages("pagination", "total_count", "20", idRange(1, 10), idRange(21, 30)), complete: true, deleted: -1},
		{name: "no declared count", pages: countPages("", "", "", idRange(1, 10), idRange(11, 20)), deleted: -1},
		{name: "short page without a count", pages: []countPage{{ids: idRange(1, 3)}}, complete: true, ids: idTexts(1, 3), deleted: 0},
		{name: "explicit has more false", pages: []countPage{hasMorePage(idRange(1, 10), false)}, complete: true, deleted: -1},
		{name: "string count", pages: countPages("pagination", "total_count", `"20"`, idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "padded string count", pages: countPages("pagination", "total_count", `" 20 "`, idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "rational string count", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", `"20/2"`)}, deleted: -1},
		{name: "hex string count", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", `"0xa"`)}, deleted: -1},
		{name: "decimal string count", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", `"20.0"`)}, deleted: -1},
		{name: "exponent string count", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", `"2e1"`)}, deleted: -1},
		{name: "float count", pages: countPages("pagination", "total_count", "20.0", idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "fractional count", pages: []countPage{counted("", "", idRange(1, 11), "pagination", "total_count", "10.5")}, deleted: -1},
		{name: "near integer fraction", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", "10.0000000000000001")}, deleted: -1},
		{name: "exponent integer count", pages: countPages("pagination", "total_count", "2e1", idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "zero count", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", "0")}, deleted: -1},
		{name: "negative count", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", "-10")}, deleted: -1},
		{name: "bool count", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", "true")}, deleted: -1},
		{name: "null count", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", "null")}, deleted: -1},
		{name: "huge count", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", "9223372036854775808")}, deleted: -1},
		{name: "bare top level total", pages: []countPage{counted("", "", idRange(1, 10), "", "total", "10")}, deleted: -1},
		{name: "pagination total", pages: countPages("pagination", "total", "20", idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "meta total count", pages: countPages("meta", "total_count", "20", idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "paging totalCount", pages: countPages("paging", "totalCount", "20", idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "page info total", pages: countPages("page_info", "total", "20", idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "top level total_count", pages: countPages("", "total_count", "20", idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "top level totalCount", pages: countPages("", "totalCount", "20", idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "pageInfo total", pages: countPages("pageInfo", "total", "20", idRange(1, 10), idRange(11, 20)), complete: true, ids: idTexts(1, 20), deleted: -1},
		{name: "count only on first page", pages: []countPage{counted("", "p2", idRange(1, 10), "pagination", "total_count", "20"), {token: "p2", ids: idRange(11, 20)}}, deleted: -1},
		{name: "count only on later page", pages: []countPage{{next: "p2", ids: idRange(1, 10)}, counted("p2", "", idRange(11, 20), "pagination", "total_count", "20")}, deleted: -1},
		{name: "distinct ids below count", pages: []countPage{counted("", "", idRange(1, 10), "pagination", "total_count", "25")}, deleted: -1},
		{name: "within page duplicate still reaches distinct total", pages: []countPage{counted("", "", []int{1, 1, 2, 3, 4, 5, 6, 7, 8, 9}, "pagination", "total_count", "9")}, complete: true, ids: idTexts(1, 9), deleted: -1},
		{name: "missing ids", pages: []countPage{countedBlanks(10, "pagination", "total_count", "10")}, deleted: -1},
	}
}

func reportCountPhases() [][]countPage {
	return [][]countPage{
		countPages("pagination", "total_count", "20", idRange(1, 10), idRange(11, 20)),
		countPages("pagination", "total_count", "20", idRange(1, 10), idRange(21, 30)),
	}
}

func phaseNamed(cases []countCase, name string) int {
	for i, tc := range cases {
		if tc.name == name {
			return i
		}
	}
	panic("missing count case " + name)
}

func namesByPath(profile *profiler.APIProfile, path string) profiler.SyncableResource {
	for _, resource := range profile.SyncableResources {
		if resource.Path == path {
			return resource
		}
	}
	return profiler.SyncableResource{}
}

func assertCountComplete(t *testing.T, out string, code int, dbPath, resource string, ids []string) {
	t.Helper()
	require.Zero(t, code, out)
	assert.NotContains(t, out, "pagination_cursor_missing")
	assert.NotContains(t, out, "cursor_unavailable")
	assert.Equal(t, 1, attemptComplete(t, dbPath, resource), out)
	if ids != nil {
		assert.Equal(t, ids, storedResourceIDs(t, dbPath, resource))
	}
}

func assertCountIncomplete(t *testing.T, out string, code int, dbPath, resource string) {
	t.Helper()
	assert.NotZero(t, code, out)
	assert.Zero(t, attemptComplete(t, dbPath, resource), out)
	assert.Contains(t, out, "cursor_unavailable")
}

func attemptComplete(t *testing.T, dbPath, resource string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var complete int
	err = db.QueryRow(`SELECT last_attempt_complete FROM sync_state WHERE resource_type = ?`, resource).Scan(&complete)
	if err == sql.ErrNoRows {
		return 0
	}
	require.NoError(t, err)
	return complete
}

func storedResourceIDs(t *testing.T, dbPath, resource string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	rows, err := db.Query(`SELECT CAST(json_extract(data, '$.id') AS TEXT) FROM resources WHERE resource_type = ? ORDER BY CAST(json_extract(data, '$.id') AS INTEGER)`, resource)
	require.NoError(t, err)
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}

func reconcileSkip(t *testing.T, out, resource string) string {
	t.Helper()
	for _, ev := range ndjsonEvents(out) {
		if ev["event"] == "reconcile_skipped" && ev["resource"] == resource {
			return fmt.Sprint(ev["reason"])
		}
	}
	t.Fatalf("no reconcile_skipped for %s in:\n%s", resource, out)
	return ""
}

func assertLimit(t *testing.T, h *countEndHandler, path, want string) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	limits := h.limits[path]
	require.NotEmpty(t, limits, path)
	for _, got := range limits {
		assert.Equal(t, want, got, path)
	}
}

type countPage struct {
	token   string
	next    string
	ids     []int
	blanks  int
	where   string
	key     string
	raw     string
	hasMore *bool
}

func (p countPage) body() []byte {
	items := make([]any, 0, len(p.ids)+p.blanks)
	for _, id := range p.ids {
		items = append(items, map[string]any{"id": id})
	}
	for range p.blanks {
		items = append(items, map[string]any{"name": "blank"})
	}
	env := map[string]any{"data": items}
	if p.hasMore != nil {
		env["has_more"] = *p.hasMore
	}
	if p.key != "" {
		target := env
		if p.where != "" {
			inner, _ := env[p.where].(map[string]any)
			if inner == nil {
				inner = map[string]any{}
				env[p.where] = inner
			}
			target = inner
		}
		target[p.key] = json.RawMessage(p.raw)
	}
	if p.next != "" {
		pag, _ := env["pagination"].(map[string]any)
		if pag == nil {
			pag = map[string]any{}
			env["pagination"] = pag
		}
		pag["next_page_token"] = p.next
	}
	body, err := json.Marshal(env)
	if err != nil {
		panic(err)
	}
	return body
}

type countEndHandler struct {
	mu      sync.Mutex
	flat    []countCase
	reports [][]countPage
	phase   map[string]int
	limits  map[string][]string
}

func newCountEndHandler(flat []countCase, reports [][]countPage) *countEndHandler {
	return &countEndHandler{
		flat:    flat,
		reports: reports,
		phase:   map[string]int{},
		limits:  map[string][]string{},
	}
}

func (h *countEndHandler) setPhase(path string, phase int) {
	h.mu.Lock()
	h.phase[path] = phase
	h.mu.Unlock()
}

func (h *countEndHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/accounts" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"acct-1"}]`))
		return
	}
	token := r.URL.Query().Get("page_token")
	h.mu.Lock()
	phase := h.phase[r.URL.Path]
	var pages []countPage
	switch r.URL.Path {
	case "/rows":
		if phase >= 0 && phase < len(h.flat) {
			pages = h.flat[phase].pages
		}
	case "/accounts/acct-1/reports":
		if phase >= 0 && phase < len(h.reports) {
			pages = h.reports[phase]
		}
	}
	if pages != nil {
		h.limits[r.URL.Path] = append(h.limits[r.URL.Path], r.URL.Query().Get("limit"))
	}
	h.mu.Unlock()
	if pages == nil {
		http.NotFound(w, r)
		return
	}
	for _, page := range pages {
		if page.token == token {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(page.body())
			return
		}
	}
	http.Error(w, "unexpected page_token "+token, http.StatusInternalServerError)
}

func countPages(where, key, raw string, first, second []int) []countPage {
	return []countPage{
		counted("", "p2", first, where, key, raw),
		counted("p2", "", second, where, key, raw),
	}
}

func counted(token, next string, ids []int, where, key, raw string) countPage {
	return countPage{token: token, next: next, ids: ids, where: where, key: key, raw: raw}
}

func countedBlanks(n int, where, key, raw string) countPage {
	return countPage{blanks: n, where: where, key: key, raw: raw}
}

func hasMorePage(ids []int, hasMore bool) countPage {
	return countPage{
		ids:     ids,
		where:   "pagination",
		key:     "total_count",
		raw:     "10",
		hasMore: &hasMore,
	}
}

func idTexts(from, to int) []string {
	ids := idRange(from, to)
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = fmt.Sprint(id)
	}
	return out
}

func idRange(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

const countEndOpenAPI = `openapi: 3.0.3
info:
  title: Sync Count End
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /accounts:
    get:
      x-resource-id: id
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
  /rows:
    get:
      x-resource-id: id
      parameters:
        - name: page_token
          in: query
          schema:
            type: string
        - name: limit
          in: query
          schema:
            type: integer
            maximum: 10
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      type: object
                      properties:
                        id: {type: integer}
  /accounts/{accountId}/reports:
    get:
      x-resource-id: id
      parameters:
        - name: accountId
          in: path
          required: true
          schema:
            type: string
        - name: page_token
          in: query
          schema:
            type: string
        - name: limit
          in: query
          schema:
            type: integer
            maximum: 10
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      type: object
                      properties:
                        id: {type: integer}
`

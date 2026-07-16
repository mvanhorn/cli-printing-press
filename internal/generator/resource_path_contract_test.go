package generator

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedResourceCommandsUseAuthoritativePaths(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("resource-path-contract")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Resources = map[string]spec.Resource{
		"dns-zones": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:       "GET",
					Path:         "/v3/dnsZones",
					ResponsePath: "dnsZones",
					Response:     spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{
						Type:           "offset",
						LimitParam:     "limit",
						CursorParam:    "offset",
						NextCursorPath: "meta.next",
						HasMoreField:   "meta.more",
					},
				},
				"create": {
					Method:   "POST",
					Path:     "/v3/dnsZones",
					Body:     []spec.Param{{Name: "name", Type: "string"}},
					Response: spec.ResponseDef{Type: "object"},
				},
				"get": {
					Method: "GET", Path: "/v3/dnsZones/{zoneId}", Response: spec.ResponseDef{Type: "object"},
				},
				"download": {
					Method: "GET", Path: "/v3/dnsZones/{zoneId}/download", Response: spec.ResponseDef{Type: "object"},
				},
			},
		},
		"scoped-records": {
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/v3/zones/{zone_id}/records",
					Response: spec.ResponseDef{Type: "array"},
				},
			},
		},
		"detail-only": {
			Endpoints: map[string]spec.Endpoint{
				"get": {Method: "GET", Path: "/v3/detail", ResponsePath: "data.detail", Response: spec.ResponseDef{Type: "object"}},
			},
		},
		"shared-a": {Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/v3/shared", Response: spec.ResponseDef{Type: "array"}},
		}},
		"shared-b": {Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/v3/shared", Response: spec.ResponseDef{Type: "array"}},
		}},
		"cursor-zones": {Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET", Path: "/v3/cursorZones", ResponsePath: "items", Response: spec.ResponseDef{Type: "array"},
				Pagination: &spec.Pagination{Type: "cursor", CursorParam: "after", LimitParam: "limit", NextCursorPath: "meta.next", HasMoreField: "meta.more"},
			},
		}},
		"suffixed-details": {Endpoints: map[string]spec.Endpoint{
			"list":   {Method: "GET", Path: "/v3/items", Response: spec.ResponseDef{Type: "array"}},
			"detail": {Method: "GET", Path: "/v3/items/{itemId}/details", Response: spec.ResponseDef{Type: "object"}},
		}},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true, Import: true, Sync: true, Tail: true, Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	resourcePaths := readResourceContractGeneratedFile(t, outputDir, "internal", "cli", "resource_paths.go")
	require.Contains(t, resourcePaths, `"dns-zones": "/v3/dnsZones"`)
	require.Regexp(t, `"dns-zones":\s+"/v3/dnsZones/\{zoneId\}"`, resourcePaths)
	require.Regexp(t, `"suffixed-details":\s+"/v3/items/\{itemId\}/details"`, resourcePaths)
	require.NotContains(t, resourcePaths, `"scoped-records"`)
	require.NotContains(t, resourcePaths, `"detail-only"`)
	require.NotContains(t, resourcePaths, `"shared-a"`)
	require.NotContains(t, resourcePaths, `"shared-b"`)
	require.Contains(t, resourcePaths, `responsePath: "dnsZones"`)
	require.Contains(t, resourcePaths, `cursorParam: "offset"`)
	require.Contains(t, resourcePaths, `limitParam: "limit"`)
	require.Contains(t, resourcePaths, `nextCursorPath: "meta.next"`)
	require.Contains(t, resourcePaths, `hasMoreField: "meta.more"`)

	tail := readResourceContractGeneratedFile(t, outputDir, "internal", "cli", "tail.go")
	require.Contains(t, tail, "Args:        cobra.RangeArgs(0, 1)")
	require.Contains(t, tail, "path, err := resourceReadPath(resource)")
	require.Contains(t, tail, "extractResourcePage(data, config)")
	require.NotContains(t, tail, `path := "/" + resource`)

	export := readResourceContractGeneratedFile(t, outputDir, "internal", "cli", "export.go")
	require.Contains(t, export, "Args: cobra.RangeArgs(1, 2)")
	require.Contains(t, export, "path, err := resourceReadPath(resource)")
	require.Contains(t, export, "resourceDetailPath(resource, cliutil.EscapePathParam(args[1]))")
	require.Contains(t, export, "for {")
	require.NotContains(t, export, `path := "/" + resource`)

	importSrc := readResourceContractGeneratedFile(t, outputDir, "internal", "cli", "import.go")
	require.Contains(t, importSrc, "path, err := resourceWritePath(resource)")
	require.NotContains(t, importSrc, `path := "/" + resource`)

	syncSrc := readResourceContractGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, "func syncResourcePath(resource string)")
	require.NotContains(t, resourcePaths, "func syncResourcePath(resource string)")

	behaviorTest := `package cli

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestResourceExportUsesMappedPathEnvelopeAndPagination(t *testing.T) {
	requests := 0
	cursorRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/v3/cursorZones" {
			cursorRequests++
			if r.URL.Query().Get("after") == "" {
				fmt.Fprint(w, ` + "`" + `{"items":[{"id":1}],"meta":{"next":"cursor-2","more":true}}` + "`" + `)
				return
			}
			if r.URL.Query().Get("after") != "cursor-2" { t.Fatalf("after = %q", r.URL.Query().Get("after")) }
			fmt.Fprint(w, ` + "`" + `{"items":[{"id":2}],"meta":{"more":false}}` + "`" + `)
			return
		}
		if r.URL.Path == "/v3/dnsZones/zone-1" {
			fmt.Fprint(w, ` + "`" + `{"id":"zone-1","tags":["a","b"]}` + "`" + `)
			return
		}
		if r.URL.Path == "/v3/items/item-1/details" {
			fmt.Fprint(w, ` + "`" + `{"id":"item-1","detail":true}` + "`" + `)
			return
		}
		if r.URL.Path != "/v3/dnsZones" {
			t.Fatalf("path = %q, want mapped path", r.URL.Path)
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		fmt.Fprint(w, ` + "`" + `{"dnsZones":[` + "`" + `)
		if offset < 600 {
			for i := 0; i < 100; i++ {
				if i > 0 { fmt.Fprint(w, ",") }
				fmt.Fprintf(w, ` + "`" + `{"id":%d}` + "`" + `, offset+i)
			}
		}
		fmt.Fprint(w, ` + "`" + `],"totalResults":600}` + "`" + `)
	}))
	defer server.Close()
	t.Setenv("RESOURCE_PATH_CONTRACT_BASE_URL", server.URL)

	output := filepath.Join(t.TempDir(), "zones.jsonl")
	cmd := newExportCmd(&rootFlags{})
	cmd.SetArgs([]string{"dns-zones", "--format", "jsonl", "--output", output})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("export: %v", err)
	}
	f, err := os.Open(output)
	if err != nil { t.Fatal(err) }
	defer f.Close()
	scanner := bufio.NewScanner(f)
	lines := 0
	for scanner.Scan() { lines++ }
	if err := scanner.Err(); err != nil { t.Fatal(err) }
	if lines != 600 { t.Fatalf("lines = %d, want 600", lines) }
	if requests != 7 { t.Fatalf("requests = %d, want 7 pages including terminal empty page", requests) }

	single := filepath.Join(t.TempDir(), "zone.jsonl")
	cmd = newExportCmd(&rootFlags{})
	cmd.SetArgs([]string{"dns-zones", "zone-1", "--format", "jsonl", "--output", single})
	if err := cmd.Execute(); err != nil { t.Fatalf("single export: %v", err) }
	singleData, err := os.ReadFile(single)
	if err != nil { t.Fatal(err) }
	if strings.TrimSpace(string(singleData)) != ` + "`" + `{"id":"zone-1","tags":["a","b"]}` + "`" + ` {
		t.Fatalf("single export = %s", singleData)
	}

	suffixed := filepath.Join(t.TempDir(), "item-details.jsonl")
	cmd = newExportCmd(&rootFlags{})
	cmd.SetArgs([]string{"suffixed-details", "item-1", "--format", "jsonl", "--output", suffixed})
	if err := cmd.Execute(); err != nil { t.Fatalf("suffixed detail export: %v", err) }
	suffixedData, err := os.ReadFile(suffixed)
	if err != nil { t.Fatal(err) }
	if strings.TrimSpace(string(suffixedData)) != ` + "`" + `{"id":"item-1","detail":true}` + "`" + ` {
		t.Fatalf("suffixed detail export = %s", suffixedData)
	}

	cursorOutput := filepath.Join(t.TempDir(), "cursor.jsonl")
	cmd = newExportCmd(&rootFlags{})
	cmd.SetArgs([]string{"cursor-zones", "--format", "jsonl", "--output", cursorOutput})
	if err := cmd.Execute(); err != nil { t.Fatalf("cursor export: %v", err) }
	cursorData, err := os.ReadFile(cursorOutput)
	if err != nil { t.Fatal(err) }
	if cursorRequests != 2 || len(strings.Split(strings.TrimSpace(string(cursorData)), "\n")) != 2 {
		t.Fatalf("cursor requests=%d output=%s", cursorRequests, cursorData)
	}
}

func TestResourceExportAllowsIdenticalOffsetPages(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		fmt.Fprint(w, ` + "`" + `{"dnsZones":[` + "`" + `)
		if offset < 200 {
			for i := 0; i < 100; i++ {
				if i > 0 { fmt.Fprint(w, ",") }
				fmt.Fprint(w, ` + "`" + `{"id":1}` + "`" + `)
			}
		}
		fmt.Fprint(w, ` + "`" + `],"totalResults":200}` + "`" + `)
	}))
	defer server.Close()
	t.Setenv("RESOURCE_PATH_CONTRACT_BASE_URL", server.URL)

	output := filepath.Join(t.TempDir(), "identical-offset-pages.jsonl")
	cmd := newExportCmd(&rootFlags{})
	cmd.SetArgs([]string{"dns-zones", "--format", "jsonl", "--output", output})
	if err := cmd.Execute(); err != nil { t.Fatalf("export: %v", err) }
	data, err := os.ReadFile(output)
	if err != nil { t.Fatal(err) }
	if requests != 3 || len(strings.Split(strings.TrimSpace(string(data)), "\n")) != 200 {
		t.Fatalf("requests=%d output lines=%d", requests, len(strings.Split(strings.TrimSpace(string(data)), "\n")))
	}
}

func TestResourceTailUsesMappedPathEnvelopeAndRejectsExtraArgs(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, ` + "`" + `{"dnsZones":[{"id":1}],"totalResults":1}` + "`" + `)
	}))
	defer server.Close()
	t.Setenv("RESOURCE_PATH_CONTRACT_BASE_URL", server.URL)

	original := os.Stdout
	readEnd, writeEnd, err := os.Pipe()
	if err != nil { t.Fatal(err) }
	os.Stdout = writeEnd
	cmd := newTailCmd(&rootFlags{})
	cmd.SetArgs([]string{"dns-zones", "--follow=false"})
	err = cmd.Execute()
	os.Stdout = original
	_ = writeEnd.Close()
	if err != nil { t.Fatalf("tail: %v", err) }
	tailOutput, err := io.ReadAll(readEnd)
	_ = readEnd.Close()
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(tailOutput), ` + "`" + `"data":{"id":1}` + "`" + `) {
		t.Fatalf("tail output = %s", tailOutput)
	}
	if gotPath != "/v3/dnsZones" { t.Fatalf("path = %q", gotPath) }

	cmd = newTailCmd(&rootFlags{})
	cmd.SetArgs([]string{"dns-zones", "extra"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "between 0 and 1 arg") {
		t.Fatalf("expected arity error, got %v", err)
	}
}

func TestResourceImportUsesMappedWritePath(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, ` + "`" + `{"ok":true}` + "`" + `)
	}))
	defer server.Close()
	t.Setenv("RESOURCE_PATH_CONTRACT_BASE_URL", server.URL)

	input := filepath.Join(t.TempDir(), "zones.jsonl")
	if err := os.WriteFile(input, []byte("{\"name\":\"example\"}\n"), 0o600); err != nil { t.Fatal(err) }
	cmd := newImportCmd(&rootFlags{})
	cmd.SetArgs([]string{"dns-zones", "--input", input})
	if err := cmd.Execute(); err != nil { t.Fatalf("import: %v", err) }
	if gotPath != "/v3/dnsZones" { t.Fatalf("path = %q", gotPath) }
}

func TestResourcePaginationUsesDeclaredFieldsAndStableOffsetStride(t *testing.T) {
	data := []byte(` + "`" + `{"dnsZones":[{"id":1}],"meta":{"next":"cursor-2","more":true}}` + "`" + `)
	config := resourceReadConfig{
		responsePath: "dnsZones", paginationType: "cursor", cursorParam: "after",
		limitParam: "limit", nextCursorPath: "meta.next", hasMoreField: "meta.more", pageSize: 100,
	}
	items, next, more := extractResourcePage(data, config)
	if len(items) != 1 || next != "cursor-2" || !more {
		t.Fatalf("items=%d next=%q more=%v", len(items), next, more)
	}
	params := resourcePageParams(resourceReadConfig{
		paginationType: "offset", cursorParam: "offset", limitParam: "limit", pageSize: 100,
	}, "", 2, 50)
	if params["offset"] != "200" || params["limit"] != "50" {
		t.Fatalf("params = %#v, want offset 200 and limit 50", params)
	}
	pageParams := resourcePageParams(resourceReadConfig{
		paginationType: "page", cursorParam: "page", limitParam: "limit", pageSize: 100,
	}, "", 1, 50)
	if pageParams["page"] != "2" || pageParams["limit"] != "100" {
		t.Fatalf("page params = %#v, want page 2 and stable limit 100", pageParams)
	}
	for _, tc := range []struct {
		name, current, next string
		hasMore bool
	}{
		{name: "missing", current: "cursor-1", hasMore: true},
		{name: "repeated", current: "cursor-1", next: "cursor-1", hasMore: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := resourceNextCursor(config, tc.current, tc.next, tc.hasMore); err == nil {
				t.Fatal("expected non-advanceable cursor error")
			}
		})
	}

	nested := []byte(` + "`" + `{"success":true,"data":{"users":[{"id":1}],"next_cursor":"nested-2"}}` + "`" + `)
	items, next, more = extractResourcePage(nested, resourceReadConfig{})
	if len(items) != 1 || next != "nested-2" || !more {
		t.Fatalf("nested items=%d next=%q more=%v", len(items), next, more)
	}
	detail := []byte(` + "`" + `{"id":1,"children":[{"id":2}]}` + "`" + `)
	items, _, _ = extractResourcePage(detail, resourceReadConfig{})
	if items != nil { t.Fatalf("detail object misclassified as %d list items", len(items)) }
}

func TestResourcePaginationStopsWhenDeclaredHasMoreIsFalse(t *testing.T) {
	data := []byte(` + "`" + `{"dnsZones":[{"id":1}],"meta":{"next":"cursor-2","more":false}}` + "`" + `)
	config := resourceReadConfig{
		responsePath: "dnsZones", paginationType: "cursor", cursorParam: "after",
		nextCursorPath: "meta.next", hasMoreField: "meta.more",
	}
	_, next, more := extractResourcePage(data, config)
	if next != "" || more {
		t.Fatalf("next=%q more=%v, want empty cursor and false", next, more)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "resource_path_contract_test.go"), []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestResource", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestImportIsNotEmittedForParentScopedCreateOnly(t *testing.T) {
	apiSpec := minimalSpec("scoped-create-only")
	apiSpec.Resources = map[string]spec.Resource{
		"parents": {
			SubResources: map[string]spec.Resource{
				"children": {
					Endpoints: map[string]spec.Endpoint{
						"create": {Method: "POST", Path: "/parents/{parent_id}/children"},
					},
				},
			},
		},
	}
	set := constrainVisionTemplates(apiSpec, VisionTemplateSet{Import: true}, &profiler.APIProfile{}, io.Discard)
	require.False(t, set.Import)
}

func readResourceContractGeneratedFile(t *testing.T, root string, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	require.NoError(t, err)
	return string(data)
}

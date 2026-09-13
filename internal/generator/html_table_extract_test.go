package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const htmlTableBannerFixture = `<table class="tbl">
  <tr><th colspan="4">Q1 2026 Summary (thousands)</th></tr>
  <tr><th>Region</th><th>Units</th><th>Revenue</th><th>Margin</th></tr>
  <tr><td>North</td><td>120</td><td>4500</td><td>0.21</td></tr>
  <tr><td>South</td><td>95</td><td>3100</td><td>0.18</td></tr>
  <tr><td>East</td><td>143</td><td>5200</td><td>0.24</td></tr>
</table>`

const htmlTableBidAskFixture = `<table>
  <tr><th rowspan="2">SYMBOL</th><th colspan="2">BID</th><th colspan="2">ASK</th></tr>
  <tr><th>VOLUME</th><th>PRICE</th><th>VOLUME</th><th>PRICE</th></tr>
  <tr><td>OGDC</td><td>500</td><td>314.00</td><td>700</td><td>314.50</td></tr>
</table>`

const htmlTableSimpleFixture = `<table><thead><tr><th>Player</th><th>Salary</th></tr></thead><tbody><tr><td>Ada Lovelace</td><td>$100</td></tr></tbody></table>`

func htmlTableExtractSpec(name string, limit int) *spec.APISpec {
	extract := &spec.HTMLExtract{Mode: spec.HTMLExtractModeTable}
	if limit > 0 {
		extract.Limit = limit
	}
	s := &spec.APISpec{
		Name:    name,
		Version: "0.1.0",
		BaseURL: "https://example.test",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/" + name + "-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"report": {
				Description: "HTML tables",
				Endpoints: map[string]spec.Endpoint{
					"table_mode": {
						Method:         "GET",
						Path:           "/report",
						Description:    "Fetch header-keyed table rows",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract:    extract,
						Response:       spec.ResponseDef{Type: "array", Item: "html_table_row"},
					},
				},
			},
		},
	}
	s.Learn.Disabled = true
	return s
}

func TestGeneratedHTMLTableExtractsSpannedHeaders(t *testing.T) {
	t.Parallel()

	apiSpec := htmlTableExtractSpec("tableprobe", 0)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "html_extract.go")
	assert.Contains(t, src, "func expandHTMLTableHeaderKeys(")
	assert.Contains(t, src, "func htmlSpanAttr(")
	assert.NotContains(t, src, "headers := rawRows[0].cells")

	inlineTest := `package cli

import (
	"encoding/json"
	"testing"
)

func TestHTMLTableSpanFixtures(t *testing.T) {
	banner, err := extractHTMLResponse([]byte(` + "`" + htmlTableBannerFixture + "`" + `), htmlExtractionOptions{Mode: "table"})
	if err != nil {
		t.Fatalf("banner extract: %v", err)
	}
	var bannerRows []map[string]string
	if err := json.Unmarshal(banner, &bannerRows); err != nil {
		t.Fatalf("banner json: %v\n%s", err, banner)
	}
	if len(bannerRows) != 3 {
		t.Fatalf("banner rows = %d, want 3: %s", len(bannerRows), banner)
	}
	if bannerRows[0]["Region"] != "North" || bannerRows[0]["Units"] != "120" || bannerRows[0]["Revenue"] != "4500" || bannerRows[0]["Margin"] != "0.21" {
		t.Fatalf("banner row[0] = %#v", bannerRows[0])
	}
	if _, ok := bannerRows[0]["Q1 2026 Summary (thousands)"]; ok {
		t.Fatalf("banner caption became a column key: %#v", bannerRows[0])
	}

	bidAsk, err := extractHTMLResponse([]byte(` + "`" + htmlTableBidAskFixture + "`" + `), htmlExtractionOptions{Mode: "table"})
	if err != nil {
		t.Fatalf("bid/ask extract: %v", err)
	}
	var bidAskRows []map[string]string
	if err := json.Unmarshal(bidAsk, &bidAskRows); err != nil {
		t.Fatalf("bid/ask json: %v\n%s", err, bidAsk)
	}
	if len(bidAskRows) != 1 {
		t.Fatalf("bid/ask rows = %d, want 1: %s", len(bidAskRows), bidAsk)
	}
	row := bidAskRows[0]
	if row["SYMBOL"] != "OGDC" {
		t.Fatalf("SYMBOL = %q, want OGDC: %#v", row["SYMBOL"], row)
	}
	if row["bid_volume"] != "500" {
		t.Fatalf("bid_volume = %q, want 500: %#v", row["bid_volume"], row)
	}
	if row["bid_price"] != "314.00" {
		t.Fatalf("bid_price = %q, want 314.00: %#v", row["bid_price"], row)
	}
	if row["ask_volume"] != "700" {
		t.Fatalf("ask_volume = %q, want 700: %#v", row["ask_volume"], row)
	}
	if row["ask_price"] != "314.50" {
		t.Fatalf("ask_price = %q, want 314.50: %#v", row["ask_price"], row)
	}
	if row["BID"] == "500" || row["ASK"] == "314.00" {
		t.Fatalf("values landed under grouping keys: %#v", row)
	}

	simple, err := extractHTMLResponse([]byte(` + "`" + htmlTableSimpleFixture + "`" + `), htmlExtractionOptions{Mode: "table"})
	if err != nil {
		t.Fatalf("simple extract: %v", err)
	}
	var simpleRows []map[string]string
	if err := json.Unmarshal(simple, &simpleRows); err != nil {
		t.Fatalf("simple json: %v\n%s", err, simple)
	}
	if len(simpleRows) != 1 || simpleRows[0]["Player"] != "Ada Lovelace" || simpleRows[0]["Salary"] != "$100" {
		t.Fatalf("simple row = %#v", simpleRows)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "html_table_span_test.go"), []byte(inlineTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestHTMLTableSpanFixtures$", "-count=1")
}

func TestGeneratedHTMLTableCommandMatchesIssueRepros(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/banner", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(htmlTableBannerFixture))
	})
	mux.HandleFunc("/bidask", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(htmlTableBidAskFixture))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	apiSpec := htmlTableExtractSpec("tablerepro", 0)
	apiSpec.BaseURL = server.URL
	apiSpec.Resources["report"] = spec.Resource{
		Description: "HTML tables",
		Endpoints: map[string]spec.Endpoint{
			"banner": {
				Method:         "GET",
				Path:           "/banner",
				Description:    "Colspan banner table",
				ResponseFormat: spec.ResponseFormatHTML,
				HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModeTable},
				Response:       spec.ResponseDef{Type: "array", Item: "html_table_row"},
			},
			"bidask": {
				Method:         "GET",
				Path:           "/bidask",
				Description:    "Two-level bid/ask table",
				ResponseFormat: spec.ResponseFormatHTML,
				HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModeTable},
				Response:       spec.ResponseDef{Type: "array", Item: "html_table_row"},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	env := append(os.Environ(), "TABLEREPRO_BASE_URL="+server.URL, "HOME="+t.TempDir())

	cmd := exec.Command(binaryPath, "report", "banner", "--json")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	bannerRows := decodeHTMLTableRows(t, out)
	require.Len(t, bannerRows, 3)
	assert.Equal(t, "North", bannerRows[0]["Region"])
	assert.Equal(t, "120", bannerRows[0]["Units"])
	assert.Equal(t, "4500", bannerRows[0]["Revenue"])
	assert.Equal(t, "0.21", bannerRows[0]["Margin"])
	assert.NotContains(t, bannerRows[0], "Q1 2026 Summary (thousands)")

	cmd = exec.Command(binaryPath, "report", "bidask", "--agent")
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	var envelope struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out, &envelope), string(out))
	require.Len(t, envelope.Results, 1)
	row := envelope.Results[0]
	assert.Equal(t, "OGDC", row["SYMBOL"])
	assert.Equal(t, "500", row["bid_volume"])
	assert.Equal(t, "314.00", row["bid_price"])
	assert.Equal(t, "700", row["ask_volume"])
	assert.Equal(t, "314.50", row["ask_price"])
	assert.NotEqual(t, "500", row["BID"])
	assert.NotEqual(t, "314.00", row["ASK"])
}

func TestGeneratedHTMLTableLimitSurfacesTruncation(t *testing.T) {
	t.Parallel()

	var body strings.Builder
	body.WriteString(`<table><tr><th>N</th><th>Label</th></tr>`)
	for i := 1; i <= 60; i++ {
		body.WriteString(`<tr><td>`)
		body.WriteString(strconv.Itoa(i))
		body.WriteString(`</td><td>row-`)
		body.WriteString(strconv.Itoa(i))
		body.WriteString(`</td></tr>`)
	}
	body.WriteString(`</table>`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body.String()))
	}))
	t.Cleanup(server.Close)

	uncapped := htmlTableExtractSpec("tableuncapped", 0)
	uncapped.BaseURL = server.URL
	uncappedDir := filepath.Join(t.TempDir(), naming.CLI(uncapped.Name))
	require.NoError(t, New(uncapped, uncappedDir).Generate())
	requireGeneratedCompiles(t, uncappedDir)
	uncappedBin := filepath.Join(uncappedDir, naming.CLI(uncapped.Name))
	runGoCommand(t, uncappedDir, "build", "-o", uncappedBin, "./cmd/"+naming.CLI(uncapped.Name))

	cmd := exec.Command(uncappedBin, "report", "table-mode", "--json")
	cmd.Env = append(os.Environ(), "TABLEUNCAPPED_BASE_URL="+server.URL, "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	allRows := decodeHTMLTableRows(t, out)
	require.Len(t, allRows, 60, "Limit 0 must not silently cap table rows at 50")

	capped := htmlTableExtractSpec("tablecapped", 50)
	capped.BaseURL = server.URL
	cappedDir := filepath.Join(t.TempDir(), naming.CLI(capped.Name))
	require.NoError(t, New(capped, cappedDir).Generate())
	requireGeneratedCompiles(t, cappedDir)
	cappedBin := filepath.Join(cappedDir, naming.CLI(capped.Name))
	runGoCommand(t, cappedDir, "build", "-o", cappedBin, "./cmd/"+naming.CLI(capped.Name))

	cmd = exec.Command(cappedBin, "report", "table-mode", "--json")
	cmd.Env = append(os.Environ(), "TABLECAPPED_BASE_URL="+server.URL, "HOME="+t.TempDir())
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Contains(t, string(out), `"event":"truncated"`, "capped extract must emit a truncation signal")
	assert.Contains(t, string(out), `"reason":"html_table_limit"`)
	cappedRows := decodeHTMLTableRows(t, out)
	require.Len(t, cappedRows, 50)
}

func decodeHTMLTableRows(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	payload := lastJSONValue(raw)
	var rows []map[string]any
	if err := json.Unmarshal(payload, &rows); err == nil {
		return rows
	}
	var envelope struct {
		Results []map[string]any `json:"results"`
		Data    []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &envelope), string(raw))
	if envelope.Results != nil {
		return envelope.Results
	}
	return envelope.Data
}

func lastJSONValue(raw []byte) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if i := strings.LastIndex(trimmed, "\n{"); i >= 0 {
		return []byte(strings.TrimSpace(trimmed[i+1:]))
	}
	if i := strings.LastIndex(trimmed, "\n["); i >= 0 {
		return []byte(strings.TrimSpace(trimmed[i+1:]))
	}
	if i := strings.IndexAny(trimmed, "{["); i >= 0 {
		return []byte(trimmed[i:])
	}
	return []byte(trimmed)
}

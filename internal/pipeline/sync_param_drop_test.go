package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
)

// seedSyncParamDropFixture writes a minimal printed-CLI layout: a syncer
// .go file under internal/syncer/, and a traffic-analysis.json next to
// it. Returns (cliDir, trafficAnalysisPath) for passing into
// CheckSyncParamDrop.
func seedSyncParamDropFixture(t *testing.T, syncerFile string, analysis browsersniff.TrafficAnalysis) (string, string) {
	t.Helper()

	root := t.TempDir()
	syncerDir := filepath.Join(root, "internal", "syncer")
	if err := os.MkdirAll(syncerDir, 0o755); err != nil {
		t.Fatalf("mkdir syncer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(syncerDir, "syncer.go"), []byte(syncerFile), 0o644); err != nil {
		t.Fatalf("write syncer.go: %v", err)
	}

	analysis.Version = "1"
	data, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		t.Fatalf("marshal analysis: %v", err)
	}
	analysisPath := filepath.Join(root, "traffic-analysis.json")
	if err := os.WriteFile(analysisPath, data, 0o644); err != nil {
		t.Fatalf("write analysis: %v", err)
	}

	return root, analysisPath
}

func makeCapture(method, path string, keys ...string) browsersniff.TrafficAnalysis {
	fields := make([]browsersniff.ShapeField, 0, len(keys))
	for _, k := range keys {
		fields = append(fields, browsersniff.ShapeField{Name: k})
	}
	return browsersniff.TrafficAnalysis{
		EndpointClusters: []browsersniff.EndpointCluster{
			{
				Method:       method,
				Path:         path,
				RequestShape: browsersniff.ShapeSummary{Fields: fields},
			},
		},
	}
}

// AC-positive-1: capture has more keys than the code passes; gate flags
// the dropped keys.
func TestCheckSyncParamDrop_DropsFlagged(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error {
	return nil
}

func sync(client *Client) error {
	return client.Get("/menu", map[string]string{
		"a": "1",
		"b": "2",
	})
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture("GET", "/menu", "a", "b", "c", "d"))
	got := CheckSyncParamDrop(cliDir, analysisPath)
	if got.Skipped {
		t.Fatalf("Skipped: want false, got true")
	}
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("Findings: want 1, got %d (%+v)", len(got.Findings), got.Findings)
	}
	f := got.Findings[0]
	if f.Path != "/menu" {
		t.Errorf("Path: want /menu, got %s", f.Path)
	}
	if f.Method != "GET" {
		t.Errorf("Method: want GET, got %s", f.Method)
	}
	wantDropped := []string{"c", "d"}
	if strings.Join(f.DroppedKeys, ",") != strings.Join(wantDropped, ",") {
		t.Errorf("DroppedKeys: want %v, got %v", wantDropped, f.DroppedKeys)
	}
	formatted := FormatSyncParamDropFinding(f)
	if !strings.Contains(formatted, "dropped params: c, d") {
		t.Errorf("formatted finding missing dropped param list: %q", formatted)
	}
}

// AC-positive-2: code passes more keys than the capture observed; gate
// does NOT flag (the extra-keys-from-code case is outside the gate's
// remit — a CLI can intentionally surface advanced flags the public UI
// never exercises).
func TestCheckSyncParamDrop_ExtraCodeKeys_NotFlagged(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client) error {
	return client.Get("/menu", map[string]string{
		"a": "1", "b": "2", "c": "3", "d": "4", "e": "5",
	})
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture("GET", "/menu", "a", "b", "c", "d"))
	got := CheckSyncParamDrop(cliDir, analysisPath)
	if got.Skipped {
		t.Fatalf("Skipped: want false, got true")
	}
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Findings) != 0 {
		t.Fatalf("Findings: want 0, got %d (%+v)", len(got.Findings), got.Findings)
	}
}

// AC-positive-3: the `// pp:sync-params-intentional-subset` comment on
// the line above the call suppresses the gate.
func TestCheckSyncParamDrop_SuppressionComment_NotFlagged(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client) error {
	// pp:sync-params-intentional-subset reason=plan-preselect-only
	return client.Get("/menu", map[string]string{
		"a": "1", "b": "2",
	})
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture("GET", "/menu", "a", "b", "c", "d"))
	got := CheckSyncParamDrop(cliDir, analysisPath)
	if got.Skipped {
		t.Fatalf("Skipped: want false, got true")
	}
	if got.Suppressed != 1 {
		t.Errorf("Suppressed: want 1, got %d", got.Suppressed)
	}
	if len(got.Findings) != 0 {
		t.Fatalf("Findings: want 0, got %d (%+v)", len(got.Findings), got.Findings)
	}
}

// AC-negative: path does not appear in the capture at all — synthetic /
// transcendence-only endpoint. No flag.
func TestCheckSyncParamDrop_UncapturedPath_NotFlagged(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client) error {
	return client.Get("/synthetic", map[string]string{
		"a": "1",
	})
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture("GET", "/different-path", "a", "b", "c"))
	got := CheckSyncParamDrop(cliDir, analysisPath)
	if got.Skipped {
		t.Fatalf("Skipped: want false, got true")
	}
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Findings) != 0 {
		t.Fatalf("Findings: want 0, got %d (%+v)", len(got.Findings), got.Findings)
	}
}

// Regression: Factor75's reported shape — 5 params in code, 11 in the
// capture, lowercase-with-dashes wire keys vs Go-field-style keys.
// Verifies the normalization correctly bridges `product-sku` (wire)
// against `ProductSku` (Go field).
func TestCheckSyncParamDrop_Factor75Shape(t *testing.T) {
	src := `package syncer

type MenuParams struct {
	Week         string
	Country      string
	Locale       string
	Subscription string
	ProductSku   string
}

type Client struct{}

func (c *Client) Get(path string, params interface{}) error { return nil }

func sync(client *Client) error {
	return client.Get("/gw/my-deliveries/menu", MenuParams{
		Week:         "w1",
		Country:      "us",
		Locale:       "en",
		Subscription: "s1",
		ProductSku:   "sku1",
	})
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture(
		"GET", "/gw/my-deliveries/menu",
		"week", "country", "locale", "subscription", "product-sku",
		"servings", "delivery-option", "postcode", "preference",
		"customerPlanId", "include-future-feedback",
	))
	got := CheckSyncParamDrop(cliDir, analysisPath)
	if got.Skipped {
		t.Fatalf("Skipped: want false, got true")
	}
	if len(got.Findings) != 1 {
		t.Fatalf("Findings: want 1, got %d (%+v)", len(got.Findings), got.Findings)
	}
	f := got.Findings[0]
	wantDropped := map[string]bool{
		"servings":                true,
		"delivery-option":         true,
		"postcode":                true,
		"preference":              true,
		"customerPlanId":          true,
		"include-future-feedback": true,
	}
	if len(f.DroppedKeys) != len(wantDropped) {
		t.Fatalf("DroppedKeys count: want %d, got %d (%v)", len(wantDropped), len(f.DroppedKeys), f.DroppedKeys)
	}
	for _, k := range f.DroppedKeys {
		if !wantDropped[k] {
			t.Errorf("unexpected dropped key %q", k)
		}
	}
}

func TestCheckSyncParamDrop_NoTrafficAnalysis_Skipped(t *testing.T) {
	root := t.TempDir()
	got := CheckSyncParamDrop(root, "")
	if !got.Skipped {
		t.Fatalf("Skipped: want true, got false (%+v)", got)
	}
}

func TestCheckSyncParamDrop_NoSyncerDir_Skipped(t *testing.T) {
	root := t.TempDir()
	analysis := makeCapture("GET", "/menu", "a", "b")
	analysis.Version = "1"
	data, _ := json.MarshalIndent(analysis, "", "  ")
	analysisPath := filepath.Join(root, "traffic-analysis.json")
	_ = os.WriteFile(analysisPath, data, 0o644)
	got := CheckSyncParamDrop(root, analysisPath)
	if !got.Skipped {
		t.Fatalf("Skipped: want true, got false (%+v)", got)
	}
}

func TestNormalizeParamKey(t *testing.T) {
	cases := map[string]string{
		"product-sku":             "productsku",
		"ProductSku":              "productsku",
		"customerPlanId":          "customerplanid",
		"include-future-feedback": "includefuturefeedback",
		"a":                       "a",
		"":                        "",
	}
	for in, want := range cases {
		got := normalizeParamKey(in)
		if got != want {
			t.Errorf("normalizeParamKey(%q): want %q, got %q", in, want, got)
		}
	}
}

func TestCanonicalSyncPath(t *testing.T) {
	cases := map[string]string{
		"/menu":                        "/menu",
		"menu":                         "/menu",
		"https://api.example.com/menu": "/menu",
		"/menu?week=1&country=us":      "/menu",
		"https://x.com/menu?week=1":    "/menu",
		"":                             "",
		"https://api.example.com":      "",
		"https://api.example.com/":     "/",
	}
	for in, want := range cases {
		got := canonicalSyncPath(in)
		if got != want {
			t.Errorf("canonicalSyncPath(%q): want %q, got %q", in, want, got)
		}
	}
}

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

func TestCheckSyncParamDrop_MapIdentifierKeys_UnrecognizedNotFlagged(t *testing.T) {
	src := `package syncer

const (
	paramWeek = "week"
	paramCountry = "country"
)

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client) error {
	return client.Get("/menu", map[string]string{
		paramWeek: "w1", paramCountry: "us",
	})
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture("GET", "/menu", "week", "country"))
	got := CheckSyncParamDrop(cliDir, analysisPath)
	if got.Skipped {
		t.Fatalf("Skipped: want false, got true")
	}
	if got.Checked != 0 {
		t.Fatalf("Checked: want 0, got %d", got.Checked)
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

func TestCheckSyncParamDrop_MultilineSuppressionComment_NotFlagged(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client) error {
	/* pp:sync-params-intentional-subset
	   reason=plan-preselect-only */
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

func TestCheckSyncParamDrop_RecursesIntoNestedSyncerPackages(t *testing.T) {
	root := t.TempDir()
	nestedDir := filepath.Join(root, "internal", "syncer", "cart")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested syncer: %v", err)
	}
	src := `package cart

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client) error {
	return client.Get("/cart", map[string]string{
		"a": "1",
	})
}
`
	if err := os.WriteFile(filepath.Join(nestedDir, "cart_sync.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write nested syncer: %v", err)
	}
	analysis := makeCapture("GET", "/cart", "a", "b")
	analysis.Version = "1"
	data, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		t.Fatalf("marshal analysis: %v", err)
	}
	analysisPath := filepath.Join(root, "traffic-analysis.json")
	if err := os.WriteFile(analysisPath, data, 0o644); err != nil {
		t.Fatalf("write analysis: %v", err)
	}

	got := CheckSyncParamDrop(root, analysisPath)
	if got.Skipped {
		t.Fatalf("Skipped: want false, got true")
	}
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("Findings: want 1, got %d (%+v)", len(got.Findings), got.Findings)
	}
}

func TestCheckSyncParamDrop_StdlibHTTPPackageCallIgnored(t *testing.T) {
	src := `package syncer

import "net/http"

func sync() error {
	_, err := http.Get("/menu")
	return err
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture("GET", "/menu", "a", "b"))
	got := CheckSyncParamDrop(cliDir, analysisPath)
	if got.Skipped {
		t.Fatalf("Skipped: want false, got true")
	}
	if got.Checked != 0 {
		t.Fatalf("Checked: want 0, got %d", got.Checked)
	}
	if len(got.Findings) != 0 {
		t.Fatalf("Findings: want 0, got %d (%+v)", len(got.Findings), got.Findings)
	}
}

// Regression: an `*http.Client` variable named `h` calling the stdlib
// `(*http.Client).Get(url)` shape (one arg, no params) must not be
// treated as a recognized client call. Otherwise callPassedKeys would
// return []string{} (explicit zero-key) and the walker would flag every
// captured key for that path as dropped. Mirrors the package-level
// http.Get(url) carve-out for the receiver-named-h case.
func TestCheckSyncParamDrop_StdlibHTTPClientVarNamedH_NotFlagged(t *testing.T) {
	src := `package syncer

import "net/http"

func sync() error {
	h := &http.Client{}
	_, err := h.Get("/menu")
	return err
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture("GET", "/menu", "a", "b"))
	got := CheckSyncParamDrop(cliDir, analysisPath)
	if got.Skipped {
		t.Fatalf("Skipped: want false, got true")
	}
	if got.Checked != 0 {
		t.Fatalf("Checked: want 0, got %d", got.Checked)
	}
	if len(got.Findings) != 0 {
		t.Fatalf("Findings: want 0, got %d (%+v)", len(got.Findings), got.Findings)
	}
}

// Regression: an empty map literal — `client.Get("/menu", map[string]string{})` —
// must be treated as an explicit zero-key call: counted toward Checked,
// and every captured key for that path reported as dropped. Previously
// the empty map returned (nil, true) from extractCompositeLiteralKeys,
// which the walker silently bypassed via its `passedKeys == nil` guard,
// hiding the exact drop the gate is designed to flag.
func TestCheckSyncParamDrop_EmptyMapLiteral_FlagsAllCaptured(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client) error {
	return client.Get("/menu", map[string]string{})
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture("GET", "/menu", "a", "b", "c"))
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
	wantDropped := []string{"a", "b", "c"}
	if strings.Join(f.DroppedKeys, ",") != strings.Join(wantDropped, ",") {
		t.Errorf("DroppedKeys: want %v, got %v", wantDropped, f.DroppedKeys)
	}
	if len(f.PassedKeys) != 0 {
		t.Errorf("PassedKeys: want empty, got %v", f.PassedKeys)
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

// Named-map AC1: ident arg whose declaration is a single composite
// literal in the same function — walker must follow the ident back to
// the declaration and read the literal keys. Drops two of four captured
// keys; gate flags them.
func TestCheckSyncParamDrop_NamedMap_LiteralOnly_DropsFlagged(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client) error {
	menuParams := map[string]string{
		"a": "1",
		"b": "2",
	}
	return client.Get("/menu", menuParams)
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
	wantDropped := []string{"c", "d"}
	if strings.Join(f.DroppedKeys, ",") != strings.Join(wantDropped, ",") {
		t.Errorf("DroppedKeys: want %v, got %v", wantDropped, f.DroppedKeys)
	}
}

// Named-map AC2: ident arg whose declaration mixes an initial literal
// with subsequent `m["k"] = v` assignments. The full union of keys must
// be visible to the gate. Drops one captured key vs the union.
func TestCheckSyncParamDrop_NamedMap_LiteralPlusAssignments_DropsFlagged(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client, sub string, sku string) error {
	menuParams := map[string]string{
		"week":   "w1",
		"locale": "en",
	}
	menuParams["subscription"] = sub
	menuParams["product-sku"] = sku
	return client.Get("/menu", menuParams)
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture(
		"GET", "/menu",
		"week", "locale", "subscription", "product-sku", "servings",
	))
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
	wantDropped := []string{"servings"}
	if strings.Join(f.DroppedKeys, ",") != strings.Join(wantDropped, ",") {
		t.Errorf("DroppedKeys: want %v, got %v", wantDropped, f.DroppedKeys)
	}
}

// Named-map AC3: keys added inside `if` branches must count as present
// (loud-when-uncertain beats mute). A capture-key set wider than the
// union of all branches still produces a drop finding.
func TestCheckSyncParamDrop_NamedMap_ConditionalBranchesCountAsPresent(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client, subID int, sku string, meals int) error {
	menuParams := map[string]string{
		"week":    "w1",
		"country": "us",
		"locale":  "en",
	}
	if subID != 0 {
		menuParams["subscription"] = "s"
	}
	if sku != "" {
		menuParams["product-sku"] = sku
	}
	if meals > 0 {
		menuParams["servings"] = "x"
	} else {
		menuParams["delivery-option"] = "y"
	}
	return client.Get("/menu", menuParams)
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture(
		"GET", "/menu",
		"week", "country", "locale", "subscription", "product-sku",
		"servings", "delivery-option", "postcode", "preference",
	))
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
	wantDropped := map[string]bool{"postcode": true, "preference": true}
	if len(f.DroppedKeys) != len(wantDropped) {
		t.Fatalf("DroppedKeys count: want %d, got %d (%v)", len(wantDropped), len(f.DroppedKeys), f.DroppedKeys)
	}
	for _, k := range f.DroppedKeys {
		if !wantDropped[k] {
			t.Errorf("unexpected dropped key %q", k)
		}
	}
	// Sanity: keys added inside branches MUST appear in the resolved
	// passed-key set, not just the initial literal three.
	wantPassed := map[string]bool{
		"week": true, "country": true, "locale": true,
		"subscription": true, "product-sku": true,
		"servings": true, "delivery-option": true,
	}
	if len(f.PassedKeys) != len(wantPassed) {
		t.Fatalf("PassedKeys count: want %d, got %d (%v)", len(wantPassed), len(f.PassedKeys), f.PassedKeys)
	}
	for _, k := range f.PassedKeys {
		if !wantPassed[k] {
			t.Errorf("unexpected passed key %q", k)
		}
	}
}

// Named-map AC4: full coverage — every captured key is reachable across
// the union of the literal + all branches. Gate MUST NOT fire.
func TestCheckSyncParamDrop_NamedMap_AllCapturedKeysPresent_NotFlagged(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client) error {
	menuParams := map[string]string{
		"week":         "w1",
		"country":      "us",
		"locale":       "en",
		"subscription": "s",
		"product-sku":  "sku",
	}
	menuParams["servings"] = "4"
	menuParams["delivery-option"] = "weekly"
	if true {
		menuParams["postcode"] = "00000"
		menuParams["preference"] = "veg"
	}
	menuParams["customerPlanId"] = "p1"
	menuParams["include-future-feedback"] = "true"
	return client.Get("/menu", menuParams)
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture(
		"GET", "/menu",
		"week", "country", "locale", "subscription", "product-sku",
		"servings", "delivery-option", "postcode", "preference",
		"customerPlanId", "include-future-feedback",
	))
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

// Regression: index assignments to a name that has no declaration in
// the same function (e.g. a parameter or an outer-scope map) must not
// produce a confidently wrong key set. The resolver returns no signal
// and the call is silently skipped — matching the legacy "unrecognized"
// behavior for shapes we can't reason about.
func TestCheckSyncParamDrop_NamedMap_NoLocalDeclaration_NotFlagged(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func sync(client *Client, menuParams map[string]string) error {
	menuParams["week"] = "w1"
	return client.Get("/menu", menuParams)
}
`
	cliDir, analysisPath := seedSyncParamDropFixture(t, src, makeCapture("GET", "/menu", "week", "country", "locale"))
	got := CheckSyncParamDrop(cliDir, analysisPath)
	if got.Skipped {
		t.Fatalf("Skipped: want false, got true")
	}
	if got.Checked != 0 {
		t.Fatalf("Checked: want 0, got %d", got.Checked)
	}
	if len(got.Findings) != 0 {
		t.Fatalf("Findings: want 0, got %d (%+v)", len(got.Findings), got.Findings)
	}
}

// Regression: a package-level `var syncFn = func(...) {...}` holding a
// function literal is still walked. Under an earlier refactor of
// walkSyncParamDropCalls that iterated `file.Decls` and filtered for
// *ast.FuncDecl only, this shape regressed from checked to silently
// unchecked. The walker must also descend into *ast.GenDecl /
// *ast.ValueSpec values that are *ast.FuncLit.
func TestCheckSyncParamDrop_PackageLevelFuncLit_DropsFlagged(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

var syncFn = func(client *Client) error {
	menuParams := map[string]string{
		"a": "1",
		"b": "2",
	}
	return client.Get("/menu", menuParams)
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
	wantDropped := []string{"c", "d"}
	if strings.Join(f.DroppedKeys, ",") != strings.Join(wantDropped, ",") {
		t.Errorf("DroppedKeys: want %v, got %v", wantDropped, f.DroppedKeys)
	}
}

// Regression: a nested closure that re-declares a same-named map must
// be resolved against its own scope, not the outer function's. The
// outer `params` holds one key; the inner closure's `params` holds
// three. The inner call passes the inner map, so the gate must read
// 3 passed keys (not 4 from a union with the outer map) and flag the
// captured-only key as dropped. Without scope-isolation in
// resolveNamedMapKeys, the outer "a" would silently union into the
// inner key set and hide drops inside the closure.
func TestCheckSyncParamDrop_NestedClosure_SameNamedMap_NotUnioned(t *testing.T) {
	src := `package syncer

type Client struct{}

func (c *Client) Get(path string, params map[string]string) error { return nil }

func runSync(client *Client) error {
	params := map[string]string{"a": "1"}
	_ = params
	inner := func() error {
		params := map[string]string{
			"a": "1",
			"b": "2",
			"c": "3",
		}
		return client.Get("/menu", params)
	}
	return inner()
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
	wantPassed := map[string]bool{"a": true, "b": true, "c": true}
	if len(f.PassedKeys) != len(wantPassed) {
		t.Fatalf("PassedKeys count: want %d, got %d (%v)", len(wantPassed), len(f.PassedKeys), f.PassedKeys)
	}
	for _, k := range f.PassedKeys {
		if !wantPassed[k] {
			t.Errorf("unexpected passed key %q (closure scope leaked into outer)", k)
		}
	}
	wantDropped := []string{"d"}
	if strings.Join(f.DroppedKeys, ",") != strings.Join(wantDropped, ",") {
		t.Errorf("DroppedKeys: want %v, got %v", wantDropped, f.DroppedKeys)
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

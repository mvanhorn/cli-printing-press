package artifacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Detector behavior
// ---------------------------------------------------------------------------

func TestFindPII_CardLast4(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		expectKinds []string
	}{
		{name: "ending-in", line: `"display": "card ending in 4242"`, expectKinds: []string{PIIKindCardLast4}},
		{name: "mask-asterisks", line: `"masked": "****-****-****-4321"`, expectKinds: []string{PIIKindCardLast4}},
		{name: "mask-x", line: `"masked": "xxxx-xxxx-xxxx-9999"`, expectKinds: []string{PIIKindCardLast4}},
		{name: "visa-context", line: `"brand": "visa 9876"`, expectKinds: []string{PIIKindCardLast4}},
		{name: "numeric-placeholder-a-still-flags", line: `"brand": "visa 1234"`, expectKinds: []string{PIIKindCardLast4}},
		{name: "numeric-placeholder-b-still-flags", line: `"brand": "amex 5678"`, expectKinds: []string{PIIKindCardLast4}},
		{name: "non-numeric-last4-placeholder", line: `"brand": "visa LAST4"`, expectKinds: nil},
		{name: "no-context-bare-4digits", line: `* 1234 changelog bullet`, expectKinds: nil},
		{name: "no-context-year", line: `"version": "2024"`, expectKinds: nil},
		{name: "no-context-port", line: `"port": "8080"`, expectKinds: nil},
		// Regression: "card" as a substring of a larger word must not match.
		// The \b boundary in the regex is the guard.
		{name: "no-substring-discard", line: `discard 1234`, expectKinds: nil},
		{name: "no-substring-wildcard", line: `wildcard 9999`, expectKinds: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanLine(t, tt.line, "test.json")
			assertKinds(t, got, tt.expectKinds, PIIKindCardLast4)
		})
	}
}

func TestFindPII_OrderIDs(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		expectKinds []string
	}{
		{name: "physical-order", line: `"order_id": "123-4567890-1234567"`, expectKinds: []string{PIIKindOrderID}},
		{name: "digital-order", line: `"order_id": "D01-4567890-1234567"`, expectKinds: []string{PIIKindOrderID}},
		{name: "synthetic-physical-order", line: `"order_id": "111-1111111-1111111"`, expectKinds: nil},
		{name: "synthetic-digital-order", line: `"order_id": "D01-1111111-1111111"`, expectKinds: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanLine(t, tt.line, "test.json")
			assertKinds(t, got, tt.expectKinds, PIIKindOrderID)
		})
	}
}

func TestFindPII_ASINs(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		expectKinds []string
	}{
		{name: "real-asin", line: `"asin": "B012345678"`, expectKinds: []string{PIIKindASIN}},
		{name: "synthetic-asin", line: `"asin": "B0EXAMPLE1"`, expectKinds: nil},
		{name: "not-asin", line: `"sku": "A012345678"`, expectKinds: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanLine(t, tt.line, "test.json")
			assertKinds(t, got, tt.expectKinds, PIIKindASIN)
		})
	}
}

func TestFindPII_Email(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		expectKinds []string
	}{
		{name: "standard", line: `"email": "alice@example.com"`, expectKinds: []string{PIIKindEmail}},
		{name: "plus-tag", line: `"email": "alice+tag@example.com"`, expectKinds: []string{PIIKindEmail}},
		{name: "no-tld", line: `"handle": "alice@example"`, expectKinds: nil},
		{name: "missing-at", line: `"site": "example.com"`, expectKinds: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanLine(t, tt.line, "test.json")
			assertKinds(t, got, tt.expectKinds, PIIKindEmail)
		})
	}
}

func TestFindPII_PhoneUS(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		expectKinds []string
	}{
		{name: "parens-space-dash", line: `"phone": "(415) 555-0123"`, expectKinds: []string{PIIKindPhoneUS}},
		{name: "all-dashes", line: `"phone": "415-555-0123"`, expectKinds: []string{PIIKindPhoneUS}},
		{name: "country-code", line: `"phone": "+1 415 555 0123"`, expectKinds: []string{PIIKindPhoneUS}},
		{name: "version-string", line: `"version": "1.2.3"`, expectKinds: nil},
		{name: "ip-address", line: `"addr": "192.168.1.1"`, expectKinds: nil},
		// NANP-shape filters — area code and exchange code must each
		// start with 2-9. Regression for the dataforseo retro: 10-digit
		// product UPCs and coordinate-shaped numerics false-positived.
		{name: "no-product-upc-leading-zero", line: `"upc": "0190074442"`, expectKinds: nil},
		{name: "no-coordinate-leading-one", line: `"lng": 106.0512973`, expectKinds: nil},
		{name: "no-epoch-timestamp", line: `"updated_at": 1700000000`, expectKinds: nil},
		// Boundary cases that prove the constraint is on the leading
		// digit of each quadrant, not on the whole string.
		{name: "no-area-code-leading-zero", line: `"phone": "015-555-0123"`, expectKinds: nil},
		{name: "no-area-code-leading-one", line: `"phone": "115-555-0123"`, expectKinds: nil},
		{name: "no-exchange-leading-zero", line: `"phone": "415-055-0123"`, expectKinds: nil},
		{name: "no-exchange-leading-one", line: `"phone": "415-155-0123"`, expectKinds: nil},
		{name: "area-code-212-valid", line: `"phone": "(212) 555-0123"`, expectKinds: []string{PIIKindPhoneUS}},
		{name: "area-code-900-valid", line: `"phone": "(900) 234-5678"`, expectKinds: []string{PIIKindPhoneUS}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanLine(t, tt.line, "test.json")
			assertKinds(t, got, tt.expectKinds, PIIKindPhoneUS)
		})
	}
}

func TestFindPII_ZipPlus4(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		expectKinds []string
	}{
		{name: "standard", line: `"zip": "62701-1234"`, expectKinds: []string{PIIKindZipPlus4}},
		{name: "five-only", line: `"zip": "62701"`, expectKinds: nil},
		{name: "different-shape", line: `"id": "abc-12345"`, expectKinds: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanLine(t, tt.line, "test.json")
			assertKinds(t, got, tt.expectKinds, PIIKindZipPlus4)
		})
	}
}

func TestFindPII_PostalAddress(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		expectKinds []string
	}{
		{name: "main-street-allcaps", line: `"address": "1234 MAIN STREET"`, expectKinds: []string{PIIKindPostalAddress}},
		{name: "ave-allcaps", line: `"line1": "567 PARK AVE"`, expectKinds: []string{PIIKindPostalAddress}},
		{name: "blvd-allcaps", line: `"line1": "890 SUNSET BLVD"`, expectKinds: []string{PIIKindPostalAddress}},
		// Title-Case is the default API shape (Amazon/Shopify/Stripe/FedEx).
		{name: "main-street-title", line: `"address": "1234 Main Street"`, expectKinds: []string{PIIKindPostalAddress}},
		{name: "ave-title", line: `"line1": "567 Park Ave"`, expectKinds: []string{PIIKindPostalAddress}},
		{name: "drive-lowercase-suffix", line: `"line1": "890 Sunset drive"`, expectKinds: []string{PIIKindPostalAddress}},
		{name: "synthetic-address", line: `"address": "123 Test St, Anytown, ST 12345"`, expectKinds: nil},
		{name: "no-number", line: `"page": "SEE README.MD"`, expectKinds: nil},
		{name: "no-suffix", line: `"line": "1234 MAIN"`, expectKinds: nil},
		// Regression guards: conversational prose where the name words
		// are lowercase must not match (the leading [A-Z] is the guard).
		{name: "no-way-in-prose", line: `2 surfaces a clean way`, expectKinds: nil},
		{name: "no-way-mixed-case", line: `1 changes generator behavior in a way`, expectKinds: nil},
		// Fully-lowercase real address is the documented gap.
		{name: "lowercase-real-address-missed", line: `"address": "1234 main street"`, expectKinds: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanLine(t, tt.line, "test.json")
			assertKinds(t, got, tt.expectKinds, PIIKindPostalAddress)
		})
	}
}

// ---------------------------------------------------------------------------
// File scoping
// ---------------------------------------------------------------------------

func TestFindPII_FileScoping(t *testing.T) {
	root := t.TempDir()
	// Same PII shape planted in different file types
	pii := `"email": "leak@example.com"`
	write(t, filepath.Join(root, "in-scope.json"), pii)
	write(t, filepath.Join(root, "in-scope.yaml"), pii)
	write(t, filepath.Join(root, "in-scope.md"), pii)
	write(t, filepath.Join(root, "in_scope_test.go"), pii)
	write(t, filepath.Join(root, "out-of-scope.go"), pii)
	write(t, filepath.Join(root, "out-of-scope.txt"), pii)
	write(t, filepath.Join(root, "out-of-scope.lock"), pii)

	findings, err := FindPII(root)
	require.NoError(t, err)

	files := uniqueFiles(findings)
	assert.ElementsMatch(t, []string{
		"in-scope.json", "in-scope.yaml", "in-scope.md", "in_scope_test.go",
	}, files)
}

func TestFindPII_DirScoping(t *testing.T) {
	root := t.TempDir()
	pii := `"phone": "(415) 555-0123"`
	// .manuscripts and testdata are in scope regardless of extension
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".manuscripts", "run1"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "testdata", "fixtures"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal"), 0755))
	write(t, filepath.Join(root, ".manuscripts", "run1", "raw.har"), pii)
	write(t, filepath.Join(root, "testdata", "fixtures", "sample.txt"), pii)
	write(t, filepath.Join(root, "internal", "client.go"), pii) // not in scope

	findings, err := FindPII(root)
	require.NoError(t, err)

	files := uniqueFiles(findings)
	assert.Contains(t, files, ".manuscripts/run1/raw.har")
	assert.Contains(t, files, "testdata/fixtures/sample.txt")
	assert.NotContains(t, files, "internal/client.go")
}

func TestFindPII_ExcludedFiles(t *testing.T) {
	root := t.TempDir()
	pii := `"email": "leak@example.com"`
	write(t, filepath.Join(root, "tools-manifest.json"), pii)
	write(t, filepath.Join(root, "data.json"), pii)

	findings, err := FindPII(root)
	require.NoError(t, err)

	files := uniqueFiles(findings)
	assert.NotContains(t, files, "tools-manifest.json")
	assert.Contains(t, files, "data.json")
}

// CLI-root vendor spec files (the source the operator passed to --spec)
// are exempt because vendor-published `example:` blocks are not customer
// PII. The exemption is depth-1 only; a spec.yaml nested under
// .manuscripts/ is captured content and stays in scope.
func TestFindPII_RootVendorSpecExempt(t *testing.T) {
	root := t.TempDir()
	pii := `"email": "jenny@example.com"`
	write(t, filepath.Join(root, "spec.yaml"), pii)
	write(t, filepath.Join(root, "spec.yml"), pii)
	write(t, filepath.Join(root, "spec.json"), pii)
	// Sibling yaml at the root must still scan — only the literal
	// vendor-spec basenames are exempt.
	write(t, filepath.Join(root, "config.yaml"), pii)

	findings, err := FindPII(root)
	require.NoError(t, err)

	files := uniqueFiles(findings)
	assert.NotContains(t, files, "spec.yaml")
	assert.NotContains(t, files, "spec.yml")
	assert.NotContains(t, files, "spec.json")
	assert.Contains(t, files, "config.yaml")
}

// Negative: nested spec.yaml files are captured content, not vendor
// source. They stay in scope so browser-sniff captures keep flagging.
// Two scope re-entry paths are exercised:
//   - high-risk dirs (.manuscripts/, testdata/) match via highRiskDirGlobs
//   - arbitrary subdirs (output/) match via the *.yaml entry in
//     highRiskFileGlobs; pinned here as a regression guard against a
//     future tweak that broadens the exemption from depth-1 to all paths
//
// The companion exemption added in #1356 only triggers when the file
// looks like an OpenAPI/Swagger root document. Files without that
// marker still scan even when nested under .manuscripts/.
func TestFindPII_NestedSpecYamlStillScans(t *testing.T) {
	root := t.TempDir()
	pii := `"email": "captured@victim.com"`
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".manuscripts", "run1", "research"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "testdata"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "output"), 0755))
	write(t, filepath.Join(root, ".manuscripts", "run1", "research", "spec.yaml"), pii)
	write(t, filepath.Join(root, "testdata", "spec.yaml"), pii)
	write(t, filepath.Join(root, "output", "spec.yaml"), pii)

	findings, err := FindPII(root)
	require.NoError(t, err)

	files := uniqueFiles(findings)
	assert.Contains(t, files, ".manuscripts/run1/research/spec.yaml")
	assert.Contains(t, files, "testdata/spec.yaml")
	assert.Contains(t, files, "output/spec.yaml")
}

// #1356: Vendor-published OpenAPI/Swagger specs archived under
// .manuscripts/ are documentation source — `example:` values look like
// PII but are public. The content-based exemption triggers off the
// version-marker shape so basenames like apps/calendars.json or
// pushpress-v3.yaml (which no filename glob would reliably cover) are
// also exempted.
func TestFindPII_ManuscriptsVendorSpecExempt(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".manuscripts", "run1", "research", "apps"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".manuscripts", "run1", "research"), 0755))

	// OpenAPI 3.x JSON with documentation-shape PII.
	openapiJSON := `{
  "openapi": "3.0.3",
  "info": {"title": "Calendars"},
  "paths": {
    "/users": {
      "post": {
        "requestBody": {
          "content": {
            "application/json": {
              "example": {"email": "user1@testemail.com", "phone": "(415) 555-0123"}
            }
          }
        }
      }
    }
  }
}`
	write(t, filepath.Join(root, ".manuscripts", "run1", "research", "apps", "calendars.json"), openapiJSON)

	// OpenAPI 3.x YAML with documentation-shape PII.
	openapiYAML := `openapi: 3.0.0
info:
  title: PushPress v3
paths:
  /bookings:
    post:
      requestBody:
        content:
          application/json:
            example:
              email: info@acme.com
              phone: 1-800-555-1234
              address: 123 Main Street
`
	write(t, filepath.Join(root, ".manuscripts", "run1", "research", "pushpress-v3.yaml"), openapiYAML)

	// Swagger 2.0 JSON with documentation-shape PII.
	swaggerJSON := `{
  "swagger": "2.0",
  "info": {"title": "Legacy"},
  "paths": {"/u": {"get": {"responses": {"200": {"examples": {"application/json": {"email": "x@y.com"}}}}}}}
}`
	write(t, filepath.Join(root, ".manuscripts", "run1", "research", "legacy.json"), swaggerJSON)

	findings, err := FindPII(root)
	require.NoError(t, err)

	files := uniqueFiles(findings)
	assert.NotContains(t, files, ".manuscripts/run1/research/apps/calendars.json",
		"OpenAPI 3.x JSON in manuscripts must be exempt")
	assert.NotContains(t, files, ".manuscripts/run1/research/pushpress-v3.yaml",
		"OpenAPI 3.x YAML in manuscripts must be exempt")
	assert.NotContains(t, files, ".manuscripts/run1/research/legacy.json",
		"Swagger 2.0 JSON in manuscripts must be exempt")
}

// Negative regression: vendor-spec content detection only applies inside
// .manuscripts/. A file at docs/api.yaml or testdata/openapi.json with
// OpenAPI markers still scans — those are committed, hand-curated
// artifacts that could legitimately accumulate real PII.
func TestFindPII_VendorSpecOutsideManuscriptsStillScans(t *testing.T) {
	root := t.TempDir()
	openapiYAML := `openapi: 3.0.0
info:
  title: Public API
paths:
  /u:
    post:
      requestBody:
        content:
          application/json:
            example:
              email: leaked@victim.com
`
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "testdata"), 0755))
	write(t, filepath.Join(root, "docs", "api.yaml"), openapiYAML)
	write(t, filepath.Join(root, "testdata", "openapi.json"), `{"openapi":"3.0.0","info":{"title":"x"},"paths":{"/u":{"post":{"requestBody":{"content":{"application/json":{"example":{"email":"leaked@victim.com"}}}}}}}}`)

	findings, err := FindPII(root)
	require.NoError(t, err)

	files := uniqueFiles(findings)
	assert.Contains(t, files, "docs/api.yaml",
		"vendor-spec exemption must not bypass docs/ committed artifacts")
	assert.Contains(t, files, "testdata/openapi.json",
		"vendor-spec exemption must not bypass testdata fixtures")
}

// Negative regression: HARs and session-state captures under
// .manuscripts/ keep scanning. They have no OpenAPI marker, so
// looksLikeVendorAPISpec returns false and the exemption never fires.
func TestFindPII_NonSpecManuscriptCapturesStillScan(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".manuscripts", "run1", "discovery"), 0755))

	har := `{
  "log": {
    "version": "1.2",
    "entries": [
      {"request": {"url": "https://api.example.com"}, "response": {"content": {"text": "{\"email\":\"real@user.com\",\"phone\":\"(415) 555-0123\"}"}}}
    ]
  }
}`
	write(t, filepath.Join(root, ".manuscripts", "run1", "discovery", "session.har"), har)

	sessionState := `{
  "cookies": [{"name": "auth_token", "value": "secret"}],
  "user": {"email": "real@user.com", "address": "1234 Main Street"}
}`
	write(t, filepath.Join(root, ".manuscripts", "run1", "discovery", "session-state.json"), sessionState)

	findings, err := FindPII(root)
	require.NoError(t, err)

	files := uniqueFiles(findings)
	assert.Contains(t, files, ".manuscripts/run1/discovery/session.har")
	assert.Contains(t, files, ".manuscripts/run1/discovery/session-state.json")
}

// The exemption probes a head window for the marker. A file that buries
// `openapi: 3.0` deep past the probe window does not bypass scanning —
// the head-window bound is intentional so an attacker (or large
// hand-edited proof) cannot smuggle PII past the gate by prepending
// arbitrary unrelated content.
func TestLooksLikeVendorAPISpec(t *testing.T) {
	tests := []struct {
		name  string
		probe string
		want  bool
	}{
		{"openapi-3.0-json", `{"openapi": "3.0.3"}`, true},
		{"openapi-3.1-json", `{"openapi":"3.1.0"}`, true},
		{"openapi-2.0-json", `{"openapi": "2.0"}`, true},
		{"swagger-2.0-json", `{"swagger": "2.0"}`, true},
		{"openapi-3.0-json-pretty", "{\n  \"openapi\": \"3.0.3\",\n  \"info\": {}\n}", true},
		{"openapi-yaml", "openapi: 3.0.0\ninfo:\n  title: x", true},
		{"openapi-yaml-quoted", `openapi: "3.0.0"`, true},
		{"swagger-yaml", "swagger: '2.0'\ninfo:\n  title: x", true},
		{"plain-json", `{"users": [{"email": "x@y.com"}]}`, false},
		{"plain-yaml", "users:\n  - email: x@y.com\n", false},
		{"prose-mentioning-openapi", "This file describes the openapi: 3.0 schema we built.", false},
		{"openapi-mid-line-yaml", "  openapi: 3.0", false},
		{"openapi-future-version-string", `{"openapi": "4.0"}`, false},
		// Anchor regressions: only root-level keys may signal a vendor
		// spec. A non-spec JSON file whose nested payload contains an
		// `openapi`/`swagger` field must not trip the exemption.
		{"json-nested-openapi-field", `{"user_data": {"extras": {"openapi": "3.0.0", "email": "real@user.com"}}}`, false},
		{"json-info-before-openapi", `{"info": {"title": "x"}, "openapi": "3.0.0"}`, false},
		{"json-swagger-nested", `{"meta": {"swagger": "2.0"}}`, false},
		// YAML anchor regressions: PII-shaped values in earlier root
		// keys must not be exempted just because a later root key looks
		// like an OpenAPI/Swagger version marker.
		{"yaml-pii-before-openapi", "captured_at: 2026-05-15\nuser_email: real@user.com\nopenapi: 3.0.0", false},
		{"yaml-swagger-after-meta", "tenant: acme\nswagger: '2.0'", false},
		// YAML lead-in allowed: optional document marker, directives,
		// comment lines, and blank lines before the version marker.
		{"yaml-with-document-marker", "---\nopenapi: 3.0.0", true},
		{"yaml-with-comment-prefix", "# Vendor-published OpenAPI spec\nopenapi: 3.0.0", true},
		{"yaml-with-directive-and-marker", "%YAML 1.2\n---\nopenapi: 3.0.0", true},
		{"yaml-with-blank-lead", "\nopenapi: 3.0.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, looksLikeVendorAPISpec([]byte(tt.probe)))
		})
	}
}

// Anchor regression at the file walk level: a JSON file under
// .manuscripts/ whose first 8 KB contains a nested "openapi": "3.0.0"
// (but no root-level marker) must still scan. Pins the JSON anchor's
// intent so a future weakening of the regex surfaces here as a real
// PII gate regression instead of a silent bypass.
func TestFindPII_NestedOpenAPIFieldStillScans(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".manuscripts", "run1", "research"), 0755))
	nestedJSON := `{
  "captured_at": "2026-05-15",
  "payload": {
    "request": {"openapi": "3.0.0"},
    "response": {"email": "real@user.com", "phone": "(415) 555-0123"}
  }
}`
	write(t, filepath.Join(root, ".manuscripts", "run1", "research", "captured.json"), nestedJSON)

	findings, err := FindPII(root)
	require.NoError(t, err)
	assert.Contains(t, uniqueFiles(findings), ".manuscripts/run1/research/captured.json",
		"nested openapi field must not bypass PII scanning")
}

// YAML parallel: a research-notes YAML under .manuscripts/ that lists
// real captured PII in earlier root keys and a later `openapi: 3.0.0`
// root key must still scan. Without the document-start anchor on the
// YAML marker, the column-0 `openapi:` line would trip the exemption
// and silently exempt the PII above it.
func TestFindPII_YAMLPIIBeforeOpenAPIStillScans(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".manuscripts", "run1", "research"), 0755))
	notes := `captured_at: 2026-05-15
user_email: real@user.com
phone: (415) 555-0123
openapi: 3.0.0
`
	write(t, filepath.Join(root, ".manuscripts", "run1", "research", "notes.yaml"), notes)

	findings, err := FindPII(root)
	require.NoError(t, err)
	assert.Contains(t, uniqueFiles(findings), ".manuscripts/run1/research/notes.yaml",
		"YAML with PII before a root-level openapi marker must not bypass PII scanning")
}

func TestFindPII_BinaryFileSkip(t *testing.T) {
	root := t.TempDir()
	// Planted PII in a "json" file with embedded nulls (mimics binary)
	bin := []byte("\"email\": \"leak@example.com\"\x00\x00\x00binary content")
	require.NoError(t, os.WriteFile(filepath.Join(root, "blob.json"), bin, 0644))

	findings, err := FindPII(root)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestFindPII_StableOrder(t *testing.T) {
	root := t.TempDir()
	content := strings.Join([]string{
		`"email": "alice@example.com"`,  // line 1, kind email
		`"address": "1234 MAIN STREET"`, // line 2, kind postal-address
		`"phone": "(415) 555-0123"`,     // line 3, kind phone-us
	}, "\n")
	write(t, filepath.Join(root, "data.json"), content)

	findings, err := FindPII(root)
	require.NoError(t, err)
	require.Len(t, findings, 3)
	assert.Equal(t, 1, findings[0].Line)
	assert.Equal(t, 2, findings[1].Line)
	assert.Equal(t, 3, findings[2].Line)
}

// ---------------------------------------------------------------------------
// Finding-ID stability
// ---------------------------------------------------------------------------

func TestPIIFindingID_Stable(t *testing.T) {
	f := PIIFinding{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: `alice@example.com`}
	id1 := PIIFindingID(f)
	id2 := PIIFindingID(f)
	assert.Equal(t, id1, id2)
	assert.Len(t, id1, 12)
}

func TestPIIFindingID_NormalizationTolerance(t *testing.T) {
	base := PIIFinding{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: `ALICE@example.com`}
	whitespace := PIIFinding{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: `alice@example.com`}
	assert.Equal(t, PIIFindingID(base), PIIFindingID(whitespace))
}

func TestPIIFindingID_LineSensitive(t *testing.T) {
	a := PIIFinding{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: `alice@example.com`}
	b := PIIFinding{Kind: PIIKindEmail, File: "a.json", Line: 6, MatchedSpan: `alice@example.com`}
	assert.NotEqual(t, PIIFindingID(a), PIIFindingID(b))
}

func TestPIIFindingID_SpanSensitive(t *testing.T) {
	a := PIIFinding{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: `alice@example.com`}
	b := PIIFinding{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: `bob@example.com`}
	assert.NotEqual(t, PIIFindingID(a), PIIFindingID(b))
}

// ---------------------------------------------------------------------------
// Ledger I/O
// ---------------------------------------------------------------------------

func TestReadPIILedger_Missing(t *testing.T) {
	dir := t.TempDir()
	got := ReadPIILedger(dir)
	assert.Nil(t, got)
}

func TestReadWritePIILedger_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := &PIILedger{
		Timestamp:           time.Now().UTC(),
		CLIDir:              dir,
		FindingsCountBefore: 3,
		Findings: []PIIFinding{
			{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: "alice@example.com"},
		},
	}
	require.NoError(t, WritePIILedger(dir, original))

	got := ReadPIILedger(dir)
	require.NotNil(t, got)
	assert.Equal(t, 3, got.FindingsCountBefore)
	assert.Len(t, got.Findings, 1)
}

func TestRunPIIAudit_RedactsCLIDirInLedger(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "captured.json"), `{"email":"leak@example.com"}`)

	_, err := RunPIIAudit(dir)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, PIILedgerFilename))
	require.NoError(t, err)
	assert.NotContains(t, string(data), dir)

	got := ReadPIILedger(dir)
	require.NotNil(t, got)
	assert.Equal(t, filepath.Join(CLIDirPlaceholder, filepath.Base(dir)), got.CLIDir)
}

func TestReadPIILedger_CorruptDeletesFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, PIILedgerFilename), []byte("not json"), 0644))

	got := ReadPIILedger(dir)
	assert.Nil(t, got)
	_, err := os.Stat(filepath.Join(dir, PIILedgerFilename))
	assert.True(t, os.IsNotExist(err), "corrupt ledger should be deleted")
}

func TestReadPIILedger_StalePreservesContent(t *testing.T) {
	dir := t.TempDir()
	stale := &PIILedger{
		Timestamp:           time.Now().Add(-2 * PIILedgerStaleAfter).UTC(),
		CLIDir:              dir,
		FindingsCountBefore: 7,
		Findings: []PIIFinding{
			{Kind: PIIKindEmail, File: "a.json", Line: 1, MatchedSpan: "x@y.z", Status: PIIStatusAccepted},
		},
	}
	data, err := json.MarshalIndent(stale, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, PIILedgerFilename), data, 0644))

	got := ReadPIILedger(dir)
	require.NotNil(t, got, "stale ledger must be preserved (auto-deletion silently destroyed accumulated accepts)")
	assert.Equal(t, 7, got.FindingsCountBefore)
	assert.True(t, IsStalePIILedger(got))
	_, statErr := os.Stat(filepath.Join(dir, PIILedgerFilename))
	assert.NoError(t, statErr, "ledger file must survive on disk for resumable agent state")
}

// ---------------------------------------------------------------------------
// Reconcile
// ---------------------------------------------------------------------------

func TestReconcilePIILedger_PreservesAcceptedState(t *testing.T) {
	previous := &PIILedger{
		Findings: []PIIFinding{
			{
				Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: "alice@example.com",
				Status: PIIStatusAccepted, Category: PIICategoryDocumentationExample,
				EvidenceContext: "in example block",
			},
		},
	}
	current := []PIIFinding{
		{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: "alice@example.com"},
	}
	delta := ReconcilePIILedger(previous, current)
	assert.True(t, delta.HasPrevious)
	assert.Empty(t, delta.Added)
	assert.Empty(t, delta.Resolved)
	assert.Equal(t, PIIStatusAccepted, current[0].Status)
	assert.Equal(t, PIICategoryDocumentationExample, current[0].Category)
	assert.Equal(t, "in example block", current[0].EvidenceContext)
}

func TestReconcilePIILedger_ResolvedAndAdded(t *testing.T) {
	previous := &PIILedger{
		Findings: []PIIFinding{
			{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: "alice@example.com"},
			{Kind: PIIKindEmail, File: "b.json", Line: 1, MatchedSpan: "fixed@example.com"},
		},
	}
	current := []PIIFinding{
		{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: "alice@example.com"},
		{Kind: PIIKindPhoneUS, File: "c.json", Line: 2, MatchedSpan: "(415) 555-0123"},
	}
	delta := ReconcilePIILedger(previous, current)
	assert.True(t, delta.HasPrevious)
	require.Len(t, delta.Resolved, 1)
	assert.Equal(t, "b.json", delta.Resolved[0].File)
	require.Len(t, delta.Added, 1)
	assert.Equal(t, PIIKindPhoneUS, delta.Added[0].Kind)
}

// ---------------------------------------------------------------------------
// Enforcement primitives
// ---------------------------------------------------------------------------

func TestMissingPIIAcceptFields(t *testing.T) {
	tests := []struct {
		name    string
		finding PIIFinding
		missing bool
	}{
		{
			name: "valid-accept",
			finding: PIIFinding{
				Status: PIIStatusAccepted, Category: PIICategoryAttribution,
				EvidenceContext: "printer_name field",
			},
			missing: false,
		},
		{
			name:    "empty-category",
			finding: PIIFinding{Status: PIIStatusAccepted, EvidenceContext: "ctx"},
			missing: true,
		},
		{
			name: "invalid-category",
			finding: PIIFinding{
				Status: PIIStatusAccepted, Category: "bogus", EvidenceContext: "ctx",
			},
			missing: true,
		},
		{
			name:    "empty-evidence",
			finding: PIIFinding{Status: PIIStatusAccepted, Category: PIICategoryPlaceName},
			missing: true,
		},
		{
			name: "other-no-note",
			finding: PIIFinding{
				Status: PIIStatusAccepted, Category: PIICategoryOther, EvidenceContext: "ctx",
			},
			missing: true,
		},
		{
			name: "other-with-note",
			finding: PIIFinding{
				Status: PIIStatusAccepted, Category: PIICategoryOther,
				EvidenceContext: "ctx", Note: "explanation",
			},
			missing: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.missing, missingPIIAcceptFields(tt.finding))
		})
	}
}

func TestDetectPIIDuplicateRationales(t *testing.T) {
	mk := func(kind, note, category string, i int) PIIFinding {
		return PIIFinding{
			Kind: kind, File: "a.json", Line: i, MatchedSpan: "x",
			Status: PIIStatusAccepted, Note: note, Category: category,
			EvidenceContext: "ctx",
		}
	}

	t.Run("under-threshold", func(t *testing.T) {
		findings := []PIIFinding{
			mk(PIIKindEmail, "false positive", PIICategoryOther, 1),
			mk(PIIKindEmail, "false positive", PIICategoryOther, 2),
			mk(PIIKindEmail, "false positive", PIICategoryOther, 3),
		}
		groups := detectPIIDuplicateRationales(findings, PIIDuplicateRationaleThreshold)
		assert.Empty(t, groups, "3 < threshold 5")
	})

	t.Run("at-threshold-does-not-fire", func(t *testing.T) {
		var findings []PIIFinding
		for i := 1; i <= 5; i++ {
			findings = append(findings, mk(PIIKindEmail, "false positive", PIICategoryOther, i))
		}
		groups := detectPIIDuplicateRationales(findings, PIIDuplicateRationaleThreshold)
		assert.Empty(t, groups, "exactly 5 accepts must not fire (gate uses > threshold, not >=)")
	})

	t.Run("over-threshold", func(t *testing.T) {
		var findings []PIIFinding
		for i := 1; i <= 6; i++ {
			findings = append(findings, mk(PIIKindEmail, "false positive", PIICategoryOther, i))
		}
		groups := detectPIIDuplicateRationales(findings, PIIDuplicateRationaleThreshold)
		require.Len(t, groups, 1)
		assert.Len(t, groups[0].Findings, 6)
	})

	t.Run("category-split", func(t *testing.T) {
		// 6 accepts share the note text but split across two categories;
		// each cluster has 3, neither exceeds threshold.
		findings := []PIIFinding{
			mk(PIIKindEmail, "same note", PIICategoryPlaceName, 1),
			mk(PIIKindEmail, "same note", PIICategoryPlaceName, 2),
			mk(PIIKindEmail, "same note", PIICategoryPlaceName, 3),
			mk(PIIKindEmail, "same note", PIICategoryCorporateName, 4),
			mk(PIIKindEmail, "same note", PIICategoryCorporateName, 5),
			mk(PIIKindEmail, "same note", PIICategoryCorporateName, 6),
		}
		groups := detectPIIDuplicateRationales(findings, PIIDuplicateRationaleThreshold)
		assert.Empty(t, groups, "category split keeps each cluster under threshold")
	})
}

func TestCheckPIIAllAcceptedNoFixes(t *testing.T) {
	accepted := func(i int) PIIFinding {
		return PIIFinding{
			Kind: PIIKindEmail, File: "a.json", Line: i, MatchedSpan: "x",
			Status: PIIStatusAccepted, Category: PIICategoryPlaceName,
			EvidenceContext: "ctx",
		}
	}

	t.Run("baseline-below-threshold", func(t *testing.T) {
		var findings []PIIFinding
		for i := 1; i <= 5; i++ {
			findings = append(findings, accepted(i))
		}
		previous := &PIILedger{FindingsCountBefore: 5}
		assert.Nil(t, checkPIIAllAcceptedNoFixes(findings, previous))
	})

	t.Run("all-accepted-equals-baseline-fires", func(t *testing.T) {
		var findings []PIIFinding
		for i := 1; i <= 12; i++ {
			findings = append(findings, accepted(i))
		}
		previous := &PIILedger{FindingsCountBefore: 12}
		issue := checkPIIAllAcceptedNoFixes(findings, previous)
		require.NotNil(t, issue)
		assert.Equal(t, 12, issue.Baseline)
		assert.Equal(t, 12, issue.Accepted)
	})

	t.Run("fixes-applied-doesnt-fire", func(t *testing.T) {
		var findings []PIIFinding
		for i := 1; i <= 6; i++ {
			findings = append(findings, accepted(i))
		}
		previous := &PIILedger{FindingsCountBefore: 12}
		assert.Nil(t, checkPIIAllAcceptedNoFixes(findings, previous), "6 < 12 means 6 fixed in source")
	})

	t.Run("pending-doesnt-fire", func(t *testing.T) {
		var findings []PIIFinding
		for i := 1; i <= 12; i++ {
			f := accepted(i)
			if i == 12 {
				f.Status = ""
			}
			findings = append(findings, f)
		}
		previous := &PIILedger{FindingsCountBefore: 12}
		assert.Nil(t, checkPIIAllAcceptedNoFixes(findings, previous))
	})

	t.Run("no-previous-fires-on-wholesale-first-run-accept", func(t *testing.T) {
		var findings []PIIFinding
		for i := 1; i <= 12; i++ {
			findings = append(findings, accepted(i))
		}
		// First run with no prior baseline: 12 findings, all accepted,
		// no source work happened — this is the wholesale-accept punt.
		issue := checkPIIAllAcceptedNoFixes(findings, nil)
		require.NotNil(t, issue)
		assert.Equal(t, 12, issue.Current)
		assert.Equal(t, 12, issue.Accepted)
	})

	t.Run("incremental-additions-trigger-gate", func(t *testing.T) {
		// Regression for adversarial #12 (was: gate compared current vs
		// baseline; when new findings appeared, current != baseline so
		// gate never fired even if every finding was accepted). New
		// gate fires when current >= threshold and all accepted, unless
		// fixes happened (current < baseline).
		var findings []PIIFinding
		for i := 1; i <= 15; i++ {
			findings = append(findings, accepted(i))
		}
		previous := &PIILedger{FindingsCountBefore: 10}
		// current (15) > baseline (10) AND all accepted: this is the
		// incremental-add bypass scenario — gate must fire.
		issue := checkPIIAllAcceptedNoFixes(findings, previous)
		require.NotNil(t, issue)
		assert.Equal(t, 15, issue.Current)
	})
}

func TestEvaluatePIICompletion_Clean(t *testing.T) {
	findings := []PIIFinding{
		{
			Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: "x",
			Status: PIIStatusAccepted, Category: PIICategoryDocumentationExample,
			EvidenceContext: "in example block",
		},
	}
	previous := &PIILedger{FindingsCountBefore: 1}
	c := EvaluatePIICompletion(findings, previous)
	assert.False(t, c.HasGateFailure())
	assert.Nil(t, c.NextPending)
}

func TestEvaluatePIICompletion_PendingShownAsNext(t *testing.T) {
	findings := []PIIFinding{
		{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: "x"},
	}
	c := EvaluatePIICompletion(findings, nil)
	require.NotNil(t, c.NextPending)
	assert.Equal(t, PIIKindEmail, c.NextPending.Kind)
}

// ---------------------------------------------------------------------------
// Format helpers
// ---------------------------------------------------------------------------

func TestPIIPendingCount(t *testing.T) {
	findings := []PIIFinding{
		{Kind: PIIKindEmail, File: "a", Line: 1, MatchedSpan: "x"},
		{
			Kind: PIIKindEmail, File: "b", Line: 2, MatchedSpan: "y",
			Status: PIIStatusAccepted, Category: PIICategoryPlaceName,
			EvidenceContext: "ctx",
		},
	}
	assert.Equal(t, 1, PIIPendingCount(findings))
}

func TestFormatPIIFindings_PendingOnly(t *testing.T) {
	findings := []PIIFinding{
		{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: "leak@example.com"},
		{
			Kind: PIIKindEmail, File: "b.json", Line: 3, MatchedSpan: "ok@example.com",
			Status: PIIStatusAccepted, Category: PIICategoryDocumentationExample,
			EvidenceContext: "ctx",
		},
	}
	out := FormatPIIFindings(findings)
	assert.Contains(t, out, "a.json:5")
	assert.NotContains(t, out, "b.json:3")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func scanLine(t *testing.T, line, filename string) []PIIFinding {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, filename), line+"\n")
	findings, err := FindPII(root)
	require.NoError(t, err)
	return findings
}

func assertKinds(t *testing.T, findings []PIIFinding, expected []string, focus string) {
	t.Helper()
	var actual []string
	for _, f := range findings {
		// Filter to detector we're testing so cross-detector overlap
		// (e.g., phone regex catching a ZIP-like number) doesn't break
		// the focused assertion.
		if f.Kind == focus {
			actual = append(actual, f.Kind)
		}
	}
	if expected == nil {
		assert.Empty(t, actual)
		return
	}
	assert.Equal(t, expected, actual)
}

func uniqueFiles(findings []PIIFinding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range findings {
		if !seen[f.File] {
			seen[f.File] = true
			out = append(out, f.File)
		}
	}
	return out
}

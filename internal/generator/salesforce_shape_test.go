package generator

// Patch 2: field_naming: salesforce → Id and DeveloperName in genericIDFieldFallbacks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// minimalSalesforceSpec builds a minimal spec with field_naming: salesforce
// and a syncable list endpoint so sync.go and store.go are generated.
func minimalSalesforceSpec() *spec.APISpec {
	s := minimalSpec("sf-shape-test")
	s.FieldNaming = spec.FieldNamingSalesforce
	s.Resources = map[string]spec.Resource{
		"objects": {
			Description: "Salesforce SObjects",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/sobjects",
					Description: "List SObjects",
					Response:    spec.ResponseDef{Type: "array"},
					Pagination:  &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:     "Id",
				},
			},
		},
	}
	return s
}

// TestFieldNamingSalesforce_ConstValue ensures the constant is the string "salesforce".
func TestFieldNamingSalesforce_ConstValue(t *testing.T) {
	t.Parallel()
	if spec.FieldNamingSalesforce != "salesforce" {
		t.Errorf("FieldNamingSalesforce = %q; want \"salesforce\"", spec.FieldNamingSalesforce)
	}
}

// TestFieldNamingSalesforce_SyncIncludesIdFallbacks verifies that when
// field_naming: salesforce is set, the generated sync.go includes "Id" and
// "DeveloperName" in genericIDFieldFallbacks.
func TestFieldNamingSalesforce_SyncIncludesIdFallbacks(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSalesforceSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	content := string(syncSrc)

	fallbackLine := extractFallbackLine(content, "genericIDFieldFallbacks")
	if fallbackLine == "" {
		t.Fatal("genericIDFieldFallbacks var not found in sync.go")
	}
	if !strings.Contains(fallbackLine, `"Id"`) {
		t.Errorf("sync.go genericIDFieldFallbacks missing \"Id\"; got: %s", fallbackLine)
	}
	if !strings.Contains(fallbackLine, `"DeveloperName"`) {
		t.Errorf("sync.go genericIDFieldFallbacks missing \"DeveloperName\"; got: %s", fallbackLine)
	}
}

// TestFieldNamingSalesforce_StoreIncludesIdFallbacks verifies the same for store.go.
func TestFieldNamingSalesforce_StoreIncludesIdFallbacks(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSalesforceSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "store.go"))
	require.NoError(t, err)
	content := string(storeSrc)

	fallbackLine := extractFallbackLine(content, "genericIDFieldFallbacks")
	if fallbackLine == "" {
		t.Fatal("genericIDFieldFallbacks var not found in store.go")
	}
	if !strings.Contains(fallbackLine, `"Id"`) {
		t.Errorf("store.go genericIDFieldFallbacks missing \"Id\"; got: %s", fallbackLine)
	}
	if !strings.Contains(fallbackLine, `"DeveloperName"`) {
		t.Errorf("store.go genericIDFieldFallbacks missing \"DeveloperName\"; got: %s", fallbackLine)
	}
}

// TestFieldNamingDefault_NoSalesforceFallbacks verifies that without
// field_naming: salesforce the baseline fallback list is unchanged.
func TestFieldNamingDefault_NoSalesforceFallbacks(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("default-naming")
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:     "GET",
					Path:       "/items",
					Response:   spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
					IDField:    "id",
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	syncSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	fallbackLine := extractFallbackLine(string(syncSrc), "genericIDFieldFallbacks")
	if strings.Contains(fallbackLine, `"DeveloperName"`) {
		t.Errorf("default spec should NOT have DeveloperName in fallbacks; got: %s", fallbackLine)
	}
}

// extractFallbackLine returns the source line containing the genericIDFieldFallbacks
// variable declaration, or empty string if not found.
func extractFallbackLine(src, varName string) string {
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "var "+varName) {
			return line
		}
	}
	return ""
}

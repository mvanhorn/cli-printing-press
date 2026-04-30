package catalog

import (
	"testing"
	"testing/fstest"

	catalogfs "github.com/mvanhorn/cli-printing-press/v3/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEntry(t *testing.T) {
	data := []byte(`
name: test-api
display_name: Test API
description: Test API for catalog parser validation
category: developer-tools
spec_url: https://example.com/openapi.yaml
spec_format: yaml
openapi_version: "3.0"
tier: community
verified_date: "2026-03-23"
homepage: https://example.com
notes: Example fixture.
`)

	entry, err := ParseEntry(data)
	require.NoError(t, err)

	assert.Equal(t, "test-api", entry.Name)
	assert.Equal(t, "Test API", entry.DisplayName)
	assert.Equal(t, "Test API for catalog parser validation", entry.Description)
	assert.Equal(t, "developer-tools", entry.Category)
	assert.Equal(t, "https://example.com/openapi.yaml", entry.SpecURL)
	assert.Equal(t, "yaml", entry.SpecFormat)
	assert.Equal(t, "3.0", entry.OpenAPIVersion)
	assert.Equal(t, "community", entry.Tier)
	assert.Equal(t, "2026-03-23", entry.VerifiedDate)
	assert.Equal(t, "https://example.com", entry.Homepage)
	assert.Equal(t, "Example fixture.", entry.Notes)
}

func TestValidateEntry(t *testing.T) {
	base := Entry{
		Name:        "test-api",
		DisplayName: "Test API",
		Description: "A valid catalog entry",
		Category:    "developer-tools",
		SpecURL:     "https://example.com/openapi.yaml",
		SpecFormat:  "yaml",
		Tier:        "official",
	}

	tests := []struct {
		name    string
		mutate  func(*Entry)
		wantErr string
	}{
		{
			name: "empty name",
			mutate: func(e *Entry) {
				e.Name = ""
			},
			wantErr: "name is required",
		},
		{
			name: "invalid name format",
			mutate: func(e *Entry) {
				e.Name = "Not_Kebab"
			},
			wantErr: "name must be lowercase kebab-case",
		},
		{
			name: "invalid category",
			mutate: func(e *Entry) {
				e.Category = "finance"
			},
			wantErr: "category must be one of",
		},
		{
			name: "non https spec url",
			mutate: func(e *Entry) {
				e.SpecURL = "http://example.com/openapi.yaml"
			},
			wantErr: `spec_url must start with "https://"`,
		},
		{
			name: "invalid spec format",
			mutate: func(e *Entry) {
				e.SpecFormat = "xml"
			},
			wantErr: "spec_format must be one of",
		},
		{
			name: "invalid tier",
			mutate: func(e *Entry) {
				e.Tier = "partner"
			},
			wantErr: "tier must be one of",
		},
		{
			name: "missing display_name",
			mutate: func(e *Entry) {
				e.DisplayName = ""
			},
			wantErr: "display_name is required",
		},
		{
			name: "missing description",
			mutate: func(e *Entry) {
				e.Description = ""
			},
			wantErr: "description is required",
		},
		{
			name: "invalid spec_source",
			mutate: func(e *Entry) {
				e.SpecSource = "guessed"
			},
			wantErr: "spec_source must be one of",
		},
		{
			name: "invalid client_pattern",
			mutate: func(e *Entry) {
				e.ClientPattern = "soap"
			},
			wantErr: "client_pattern must be one of",
		},
		{
			name: "invalid http_transport",
			mutate: func(e *Entry) {
				e.HTTPTransport = "lynx"
			},
			wantErr: "http_transport must be one of",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := base
			tt.mutate(&entry)

			err := entry.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAllPublicCategoriesAreValid(t *testing.T) {
	publicCategories := []string{
		"ai", "auth", "cloud", "commerce", "developer-tools", "devices",
		"food-and-dining", "marketing", "media-and-entertainment", "monitoring",
		"payments", "productivity", "project-management", "sales-and-crm",
		"social-and-messaging", "travel", "other",
	}
	base := Entry{
		Name:        "test-api",
		DisplayName: "Test API",
		Description: "A valid catalog entry",
		SpecURL:     "https://example.com/openapi.yaml",
		SpecFormat:  "yaml",
		Tier:        "official",
	}
	for _, cat := range publicCategories {
		t.Run(cat, func(t *testing.T) {
			entry := base
			entry.Category = cat
			assert.NoError(t, entry.Validate())
		})
	}
}

func TestExampleCategoryStillValid(t *testing.T) {
	entry := Entry{
		Name:        "test-api",
		DisplayName: "Test API",
		Description: "A valid catalog entry",
		Category:    "example",
		SpecURL:     "https://example.com/openapi.yaml",
		SpecFormat:  "yaml",
		Tier:        "official",
	}
	assert.NoError(t, entry.Validate())
}

func TestOldCategoriesRejected(t *testing.T) {
	base := Entry{
		Name:        "test-api",
		DisplayName: "Test API",
		Description: "A valid catalog entry",
		SpecURL:     "https://example.com/openapi.yaml",
		SpecFormat:  "yaml",
		Tier:        "official",
	}
	for _, cat := range []string{"email", "crm", "communication"} {
		t.Run(cat, func(t *testing.T) {
			entry := base
			entry.Category = cat
			err := entry.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "category must be one of")
		})
	}
}

func TestCategoryErrorMessageExcludesExample(t *testing.T) {
	entry := Entry{
		Name:        "test-api",
		DisplayName: "Test API",
		Description: "A valid catalog entry",
		Category:    "invalid-cat",
		SpecURL:     "https://example.com/openapi.yaml",
		SpecFormat:  "yaml",
		Tier:        "official",
	}
	err := entry.Validate()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "example")
}

func TestSniffedEntryValid(t *testing.T) {
	f := false
	entry := Entry{
		Name:          "test-sniffed",
		DisplayName:   "Test Sniffed API",
		Description:   "A sniffed catalog entry",
		Category:      "developer-tools",
		SpecURL:       "https://example.com/specs/sniffed.yaml",
		SpecFormat:    "yaml",
		Tier:          "community",
		SpecSource:    "sniffed",
		AuthRequired:  &f,
		ClientPattern: "proxy-envelope",
		HTTPTransport: "browser-chrome-h3",
	}
	assert.NoError(t, entry.Validate())
}

func TestCustomSpecFormatValid(t *testing.T) {
	f := false
	entry := Entry{
		Name:         "producthunt",
		DisplayName:  "Product Hunt",
		Description:  "Find, monitor, and export Product Hunt launches for launch research",
		Category:     "marketing",
		SpecURL:      "https://example.com/producthunt-spec.yaml",
		SpecFormat:   "custom",
		Tier:         "community",
		SpecSource:   "sniffed",
		AuthRequired: &f,
	}
	assert.NoError(t, entry.Validate())
}

func TestOptionalFieldsOmittedValid(t *testing.T) {
	// spec_source, auth_required, and client_pattern should all be optional
	entry := Entry{
		Name:        "test-minimal",
		DisplayName: "Minimal API",
		Description: "A minimal catalog entry without new fields",
		Category:    "developer-tools",
		SpecURL:     "https://example.com/openapi.yaml",
		SpecFormat:  "yaml",
		Tier:        "official",
	}
	assert.NoError(t, entry.Validate())
	assert.Empty(t, entry.SpecSource)
	assert.Nil(t, entry.AuthRequired)
	assert.Empty(t, entry.ClientPattern)
	assert.Empty(t, entry.HTTPTransport)
}

func TestWrapperOnlyEntryValid(t *testing.T) {
	entry := Entry{
		Name:        "google-flights",
		DisplayName: "Google Flights",
		Description: "Flight search via reverse-engineered wrapper libraries",
		Category:    "other",
		Tier:        "community",
		WrapperLibraries: []WrapperLibrary{
			{
				Name:            "krisukox/google-flights-api",
				URL:             "https://github.com/krisukox/google-flights-api",
				Language:        "go",
				License:         "MIT",
				IntegrationMode: "native",
			},
		},
	}
	assert.NoError(t, entry.Validate())
	assert.True(t, entry.IsWrapperOnly())
}

func TestWrapperEntryRequiresIntegrationMode(t *testing.T) {
	entry := Entry{
		Name:        "test-wrapper",
		DisplayName: "Test",
		Description: "Test",
		Category:    "other",
		Tier:        "community",
		WrapperLibraries: []WrapperLibrary{
			{Name: "lib", URL: "https://github.com/example/lib", Language: "go"},
		},
	}
	err := entry.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integration_mode is required")
}

func TestWrapperEntryRejectsInvalidIntegrationMode(t *testing.T) {
	entry := Entry{
		Name:        "test-wrapper",
		DisplayName: "Test",
		Description: "Test",
		Category:    "other",
		Tier:        "community",
		WrapperLibraries: []WrapperLibrary{
			{Name: "lib", URL: "https://github.com/example/lib", Language: "go", IntegrationMode: "ffi"},
		},
	}
	err := entry.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integration_mode must be one of")
}

func TestWrapperEntryRequiresHTTPSURL(t *testing.T) {
	entry := Entry{
		Name:        "test-wrapper",
		DisplayName: "Test",
		Description: "Test",
		Category:    "other",
		Tier:        "community",
		WrapperLibraries: []WrapperLibrary{
			{Name: "lib", URL: "http://example.com", Language: "go", IntegrationMode: "native"},
		},
	}
	err := entry.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `url must start with "https://"`)
}

func TestSpecURLRequiredWhenNoWrapperLibraries(t *testing.T) {
	entry := Entry{
		Name:        "test-api",
		DisplayName: "Test",
		Description: "Test",
		Category:    "other",
		Tier:        "community",
	}
	err := entry.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec_url is required")
}

func TestEmbeddedCatalogParsesWrapperOnlyEntries(t *testing.T) {
	entry, err := LookupFS(catalogfs.FS, "google-flights")
	require.NoError(t, err)
	assert.True(t, entry.IsWrapperOnly())
	assert.NotEmpty(t, entry.WrapperLibraries)
}

func TestPublicCategoriesExcludeExample(t *testing.T) {
	categories := PublicCategories()
	assert.NotContains(t, categories, "example")
	assert.Contains(t, categories, "developer-tools")
	assert.Contains(t, categories, "other")
}

func TestIsPublicCategory(t *testing.T) {
	assert.True(t, IsPublicCategory("developer-tools"))
	assert.True(t, IsPublicCategory("other"))
	assert.False(t, IsPublicCategory("example"))
	assert.False(t, IsPublicCategory("banana"))
}

func TestParseDir(t *testing.T) {
	entries, err := ParseDir("../../testdata/catalog")
	require.NoError(t, err)
	require.Len(t, entries, 2)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}

	assert.Contains(t, names, "test-api")
	assert.Contains(t, names, "petstore")
}

func TestParseFSEmbeddedCatalog(t *testing.T) {
	entries, err := ParseFS(catalogfs.FS)
	require.NoError(t, err)
	assert.Greater(t, len(entries), 0)
}

func TestLookupFSFindsStripe(t *testing.T) {
	entry, err := LookupFS(catalogfs.FS, "stripe")
	require.NoError(t, err)
	assert.Equal(t, "stripe", entry.Name)
	assert.Equal(t, "https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.json", entry.SpecURL)
}

func TestLookupFSFindsProductHunt(t *testing.T) {
	entry, err := LookupFS(catalogfs.FS, "producthunt")
	require.NoError(t, err)
	assert.Equal(t, "producthunt", entry.Name)
	assert.Equal(t, "Product Hunt", entry.DisplayName)
	assert.Equal(t, "marketing", entry.Category)
	assert.Equal(t, "community", entry.Tier)
	assert.Equal(t, "custom", entry.SpecFormat)
	assert.Equal(t, "sniffed", entry.SpecSource)
	require.NotNil(t, entry.AuthRequired)
	assert.False(t, *entry.AuthRequired)
}

func TestLookupFSNotFound(t *testing.T) {
	_, err := LookupFS(catalogfs.FS, "nonexistent-api")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `catalog entry "nonexistent-api" not found`)
}

func TestParseFSEmptyFS(t *testing.T) {
	emptyFS := fstest.MapFS{}
	entries, err := ParseFS(emptyFS)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestCatalogFSContainsYAMLFiles(t *testing.T) {
	// Integration: verify the embedded FS from the catalog package is importable
	// and contains YAML files.
	entries, err := catalogfs.FS.ReadDir(".")
	require.NoError(t, err)

	var yamlCount int
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > 5 && e.Name()[len(e.Name())-5:] == ".yaml" {
			yamlCount++
		}
	}
	assert.Greater(t, yamlCount, 0)
}

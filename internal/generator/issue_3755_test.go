package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestIssue3755DeepResourceNamesStayUnique(t *testing.T) {
	apiSpec := issue3755Spec()
	profile := profiler.Profile(apiSpec)
	require.Len(t, profile.DependentSyncResources, 9)

	expectedNames := map[string]string{
		"/dashboard/{client_id}/google-ads/accounts/{account_id}/campaigns":                                          "dashboard_google_ads_accounts_campaigns",
		"/dashboard/{client_id}/google-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups":                   "dashboard_google_ads_accounts_campaigns_adgroups",
		"/dashboard/{client_id}/google-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups/{ad_group_id}/ads": "dashboard_google_ads_accounts_campaigns_adgroups_ads",
		"/dashboard/{client_id}/meta-ads/accounts/{account_id}/campaigns":                                            "dashboard_meta_ads_accounts_campaigns",
		"/dashboard/{client_id}/meta-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups":                     "dashboard_meta_ads_accounts_campaigns_adgroups",
		"/dashboard/{client_id}/meta-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups/{ad_group_id}/ads":   "dashboard_meta_ads_accounts_campaigns_adgroups_ads",
		"/dashboard/{client_id}/tiktok-ads/accounts/{account_id}/campaigns":                                          "dashboard_tiktok_ads_accounts_campaigns",
		"/dashboard/{client_id}/tiktok-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups":                   "dashboard_tiktok_ads_accounts_campaigns_adgroups",
		"/dashboard/{client_id}/tiktok-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups/{ad_group_id}/ads": "dashboard_tiktok_ads_accounts_campaigns_adgroups_ads",
	}

	seen := make(map[string]string, len(profile.DependentSyncResources))
	namesByPath := make(map[string]string, len(profile.DependentSyncResources))
	for _, resource := range profile.DependentSyncResources {
		if previousPath, exists := seen[resource.Name]; exists {
			t.Fatalf("dependent resource name %q is shared by %q and %q", resource.Name, previousPath, resource.Path)
		}
		seen[resource.Name] = resource.Path
		namesByPath[resource.Path] = resource.Name
	}
	require.Equal(t, expectedNames, namesByPath)

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true, Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	syncSource, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "sync.go"))
	require.NoError(t, err)
	require.NotEmpty(t, syncSource)
	for _, name := range expectedNames {
		require.Contains(t, string(syncSource), fmt.Sprintf("case %q:", name))
	}
	requireGeneratedCompiles(t, outputDir)
}

func issue3755Spec() *spec.APISpec {
	list := func(path string) spec.Endpoint {
		return spec.Endpoint{
			Method:     "GET",
			Path:       path,
			Response:   spec.ResponseDef{Type: "array", Item: "Record"},
			Pagination: &spec.Pagination{CursorParam: "after", LimitParam: "limit"},
		}
	}

	return &spec.APISpec{
		Name:    "shared-leaf-resources",
		Version: "1.0.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/shared-leaf-resources-pp-cli/config.toml",
		},
		Types: map[string]spec.TypeDef{
			"Record": {Fields: []spec.TypeField{{Name: "id", Type: "string"}}},
		},
		Resources: map[string]spec.Resource{
			"dashboard": {
				Endpoints: map[string]spec.Endpoint{
					"list": list("/dashboard"),
				},
				SubResources: map[string]spec.Resource{
					"google_ads": {
						Endpoints: map[string]spec.Endpoint{
							"campaigns": list("/dashboard/{client_id}/google-ads/accounts/{account_id}/campaigns"),
							"adgroups":  list("/dashboard/{client_id}/google-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups"),
							"ads":       list("/dashboard/{client_id}/google-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups/{ad_group_id}/ads"),
						},
					},
					"meta_ads": {
						Endpoints: map[string]spec.Endpoint{
							"campaigns": list("/dashboard/{client_id}/meta-ads/accounts/{account_id}/campaigns"),
							"adgroups":  list("/dashboard/{client_id}/meta-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups"),
							"ads":       list("/dashboard/{client_id}/meta-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups/{ad_group_id}/ads"),
						},
					},
					"tiktok_ads": {
						Endpoints: map[string]spec.Endpoint{
							"campaigns": list("/dashboard/{client_id}/tiktok-ads/accounts/{account_id}/campaigns"),
							"adgroups":  list("/dashboard/{client_id}/tiktok-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups"),
							"ads":       list("/dashboard/{client_id}/tiktok-ads/accounts/{account_id}/campaigns/{campaign_id}/adgroups/{ad_group_id}/ads"),
						},
					},
				},
			},
		},
	}
}

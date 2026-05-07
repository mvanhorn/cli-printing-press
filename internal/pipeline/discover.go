package pipeline

import (
	"fmt"
	"strings"

	catalogfs "github.com/mvanhorn/cli-printing-press/v4/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
)

// KnownSpec holds metadata about a known API spec.
type KnownSpec struct {
	URL         string
	SandboxSafe bool
}

// KnownSpecs maps common API names to their OpenAPI spec URLs.
var KnownSpecs = map[string]KnownSpec{
	"petstore": {
		URL:         "https://petstore3.swagger.io/api/v3/openapi.json",
		SandboxSafe: true,
	},
	"gmail": {
		URL:         "https://raw.githubusercontent.com/APIs-guru/openapi-directory/main/APIs/googleapis.com/gmail/v1/openapi.yaml",
		SandboxSafe: false,
	},
	"calendar": {
		URL:         "https://raw.githubusercontent.com/APIs-guru/openapi-directory/main/APIs/googleapis.com/calendar/v3/openapi.yaml",
		SandboxSafe: false,
	},
	"drive": {
		URL:         "https://raw.githubusercontent.com/APIs-guru/openapi-directory/main/APIs/googleapis.com/drive/v3/openapi.yaml",
		SandboxSafe: false,
	},
	"sheets": {
		URL:         "https://raw.githubusercontent.com/APIs-guru/openapi-directory/main/APIs/googleapis.com/sheets/v4/openapi.yaml",
		SandboxSafe: false,
	},
	"youtube": {
		URL:         "https://raw.githubusercontent.com/APIs-guru/openapi-directory/main/APIs/googleapis.com/youtube/v3/openapi.yaml",
		SandboxSafe: false,
	},
	"stripe": {
		URL:         "https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.json",
		SandboxSafe: false,
	},
	"twilio": {
		URL:         "https://raw.githubusercontent.com/twilio/twilio-oai/main/spec/json/twilio_api_v2010.json",
		SandboxSafe: false,
	},
	"sendgrid": {
		URL:         "https://raw.githubusercontent.com/sendgrid/sendgrid-oai/main/oai_stoplight.json",
		SandboxSafe: false,
	},
	"github": {
		URL:         "https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json",
		SandboxSafe: false,
	},
	"discord": {
		URL:         "https://raw.githubusercontent.com/discord/discord-api-spec/main/specs/openapi.json",
		SandboxSafe: false,
	},
	"digitalocean": {
		URL:         "https://api-engineering.nyc3.cdn.digitaloceanspaces.com/spec-ci/DigitalOcean-public.v2.yaml",
		SandboxSafe: false,
	},
	"slack": {
		URL:         "https://raw.githubusercontent.com/APIs-guru/openapi-directory/main/APIs/slack.com/1.7.0/openapi.yaml",
		SandboxSafe: false,
	},
	"asana": {
		URL:         "https://raw.githubusercontent.com/APIs-guru/openapi-directory/main/APIs/asana.com/1.0/openapi.yaml",
		SandboxSafe: false,
	},
	"hubspot": {
		URL:         "https://raw.githubusercontent.com/APIs-guru/openapi-directory/main/APIs/hubspot.com/crm/v3/openapi.yaml",
		SandboxSafe: false,
	},
	"openai": {
		URL:         "https://raw.githubusercontent.com/openai/openai-openapi/master/openapi.yaml",
		SandboxSafe: false,
	},
	"anthropic": {
		URL:         "https://raw.githubusercontent.com/anthropics/anthropic-cookbook/main/misc/anthropic.openapi.yaml",
		SandboxSafe: false,
	},
	"cloudflare": {
		URL:         "https://raw.githubusercontent.com/cloudflare/api-schemas/main/openapi.json",
		SandboxSafe: false,
	},
	"flyio": {
		URL:         "https://docs.machines.dev/spec/openapi3.json",
		SandboxSafe: false,
	},
	"jira": {
		URL:         "https://api.apis.guru/v2/specs/atlassian.com/jira/1001.0.0-SNAPSHOT/openapi.json",
		SandboxSafe: false,
	},
	"launchdarkly": {
		URL:         "https://app.launchdarkly.com/api/v2/openapi.json",
		SandboxSafe: false,
	},
	"sentry": {
		URL:         "https://raw.githubusercontent.com/getsentry/sentry-api-schema/main/openapi-derefed.json",
		SandboxSafe: false,
	},
	"spotify": {
		URL:         "https://api.apis.guru/v2/specs/spotify.com/sonallux/2023.2.27/openapi.json",
		SandboxSafe: false,
	},
	"supabase": {
		URL:         "https://api.supabase.com/api/v1-json",
		SandboxSafe: false,
	},
	"telegram": {
		URL:         "https://api.apis.guru/v2/specs/telegram.org/5.0.0/openapi.json",
		SandboxSafe: false,
	},
	"trello": {
		URL:         "https://api.apis.guru/v2/specs/trello.com/1.0/openapi.json",
		SandboxSafe: false,
	},
	"vercel": {
		URL:         "https://api.apis.guru/v2/specs/vercel.com/0.0.1/openapi.json",
		SandboxSafe: false,
	},
}

// ApisGuruPattern builds an apis-guru URL for a provider and version.
func ApisGuruPattern(provider, version string) string {
	return fmt.Sprintf("https://raw.githubusercontent.com/APIs-guru/openapi-directory/main/APIs/%s/%s/openapi.yaml", provider, version)
}

// DiscoverSpec finds the OpenAPI spec URL for a given API name.
// Returns the URL and a source description.
func DiscoverSpec(apiName string) (string, string, error) {
	normalized := strings.ToLower(strings.TrimSpace(apiName))

	if entry, err := catalog.LookupFS(catalogfs.FS, normalized); err == nil {
		if entry.SpecURL == "" {
			return "", "", fmt.Errorf("catalog entry %q does not define spec_url - try providing a URL with --spec", apiName)
		}
		return entry.SpecURL, catalogSpecSource(entry), nil
	}

	// Check known specs for aliases that do not have catalog entries.
	if spec, ok := KnownSpecs[normalized]; ok {
		return spec.URL, "known-specs registry", nil
	}

	// Try apis-guru with common version patterns - return the first one
	// (caller should validate with an HTTP fetch).
	versions := []string{"v1", "v2", "v3", "1.0", "2.0"}
	if len(versions) > 0 {
		url := ApisGuruPattern(normalized+".com", versions[0])
		return url, "apis-guru (unverified, needs fetch validation)", nil
	}

	return "", "", fmt.Errorf("could not find OpenAPI spec for %q - try providing a URL with --spec", apiName)
}

func catalogSpecSource(entry *catalog.Entry) string {
	if source := strings.TrimSpace(entry.SpecSource); source != "" {
		return "catalog entry (" + source + ")"
	}
	if tier := strings.TrimSpace(entry.Tier); tier != "" {
		return "catalog entry (" + tier + ")"
	}
	return "catalog entry"
}

// IsSandboxSafe returns true if the API is known to have a safe test/sandbox environment.
func IsSandboxSafe(apiName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(apiName))
	if spec, ok := KnownSpecs[normalized]; ok {
		return spec.SandboxSafe
	}
	return false
}

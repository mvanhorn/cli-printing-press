package browsersniff

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v3/internal/discovery"
)

type TestFixture struct {
	EndpointName string
	Method       string
	Path         string
	ParamNames   []string
	BodyFields   []string
	HasAuth      bool
}

type FixtureSet struct {
	APIName  string
	BaseURL  string
	Fixtures []TestFixture
}

func GenerateFixtures(capture *EnrichedCapture) *FixtureSet {
	if capture == nil {
		return &FixtureSet{}
	}

	apiEntries, _ := ClassifyEntries(capture.Entries)
	groups := DeduplicateEndpoints(apiEntries)

	baseURL := mostCommonBaseURL(apiEntries)
	if baseURL == "" {
		baseURL = normalizeBaseURL(capture.TargetURL)
	}

	nameSource := capture.TargetURL
	if nameSource == "" {
		nameSource = baseURL
	}

	fixtureSet := &FixtureSet{
		APIName:  deriveNameFromURL(nameSource),
		BaseURL:  baseURL,
		Fixtures: make([]TestFixture, 0, len(groups)),
	}

	for _, group := range groups {
		if len(group.Entries) == 0 {
			continue
		}

		fixture := SanitizeForFixture(group.Entries[0])
		fixture.EndpointName = discovery.EndpointName(group.Method, group.NormalizedPath)

		paramNames := make(map[string]struct{})
		bodyFields := make(map[string]struct{})

		for _, entry := range group.Entries {
			entryFixture := SanitizeForFixture(entry)
			if fixture.Path == "" {
				fixture.Path = entryFixture.Path
			}
			if entryFixture.HasAuth {
				fixture.HasAuth = true
			}

			for _, name := range entryFixture.ParamNames {
				paramNames[name] = struct{}{}
			}
			for _, name := range entryFixture.BodyFields {
				bodyFields[name] = struct{}{}
				paramNames[name] = struct{}{}
			}
		}

		fixture.ParamNames = sortedKeys(paramNames)
		fixture.BodyFields = sortedKeys(bodyFields)
		fixtureSet.Fixtures = append(fixtureSet.Fixtures, fixture)
	}

	return fixtureSet
}

func SanitizeForFixture(entry EnrichedEntry) TestFixture {
	fixture := TestFixture{
		Method: strings.ToUpper(strings.TrimSpace(entry.Method)),
		Path:   extractPath(entry.URL),
	}

	parsedURL, err := url.Parse(entry.URL)
	if err == nil {
		queryNames := make(map[string]struct{}, len(parsedURL.Query()))
		for name := range parsedURL.Query() {
			queryNames[name] = struct{}{}
		}
		fixture.ParamNames = sortedKeys(queryNames)
	}

	contentType := strings.ToLower(getHeaderValue(entry.RequestHeaders, "Content-Type"))
	bodyFields := extractBodyFieldNames(entry.RequestBody, contentType)
	fixture.BodyFields = bodyFields
	fixture.ParamNames = mergeSortedNames(fixture.ParamNames, bodyFields)
	fixture.HasAuth = hasAuthHeaders(entry.RequestHeaders)

	return fixture
}

func extractBodyFieldNames(body string, contentType string) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	switch {
	case strings.Contains(contentType, "json"):
		var value any
		if err := json.Unmarshal([]byte(body), &value); err != nil {
			return nil
		}

		root := topLevelObject(value)
		if root == nil {
			return nil
		}

		fields := make([]string, 0, len(root))
		for key := range root {
			fields = append(fields, key)
		}
		sort.Strings(fields)
		return fields
	case strings.Contains(contentType, "form-urlencoded"):
		values := ParseFormBody(body)
		return sortedKeysFromMap(values)
	default:
		return nil
	}
}

func hasAuthHeaders(headers map[string]string) bool {
	for name := range headers {
		if isAuthHeader(name) {
			return true
		}
	}

	return false
}

func isAuthHeader(name string) bool {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	switch lowerName {
	case "authorization", "proxy-authorization", "x-api-key", "api-key", "x-auth-token", "cookie", "x-amz-security-token":
		return true
	default:
		return false
	}
}

func mergeSortedNames(a []string, b []string) []string {
	names := make(map[string]struct{}, len(a)+len(b))
	for _, name := range a {
		names[name] = struct{}{}
	}
	for _, name := range b {
		names[name] = struct{}{}
	}

	return sortedKeys(names)
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeysFromMap(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

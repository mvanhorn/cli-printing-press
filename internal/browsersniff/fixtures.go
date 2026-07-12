package browsersniff

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/discovery"
	"github.com/mvanhorn/cli-printing-press/v4/internal/piiplaceholders"
)

type FixtureValue struct {
	Name  string
	Value string
}

type TestFixture struct {
	EndpointName string
	Method       string
	Path         string
	ParamNames   []string
	BodyFields   []string
	ParamSamples []FixtureValue
	BodySamples  []FixtureValue
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
		paramSamples := make(map[string]string)
		bodySamples := make(map[string]string)

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
			mergeFixtureSamples(paramSamples, entryFixture.ParamSamples)
			mergeFixtureSamples(bodySamples, entryFixture.BodySamples)
		}

		fixture.ParamNames = sortedKeys(paramNames)
		fixture.BodyFields = sortedKeys(bodyFields)
		fixture.ParamSamples = sortedFixtureValues(paramSamples)
		fixture.BodySamples = sortedFixtureValues(bodySamples)
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
		querySamples := make(map[string]string, len(parsedURL.Query()))
		for name, values := range parsedURL.Query() {
			queryNames[name] = struct{}{}
			if len(values) > 0 {
				querySamples[name] = syntheticFixtureValue(name, values[0])
			}
		}
		fixture.ParamNames = sortedKeys(queryNames)
		fixture.ParamSamples = sortedFixtureValues(querySamples)
	}

	contentType := strings.ToLower(getHeaderValue(entry.RequestHeaders, "Content-Type"))
	bodyFields, bodySamples := extractBodyFieldSamples(entry.RequestBody, contentType)
	fixture.BodyFields = bodyFields
	fixture.BodySamples = sortedFixtureValues(bodySamples)
	fixture.ParamNames = mergeSortedNames(fixture.ParamNames, bodyFields)
	mergeFixtureSamplesMap := map[string]string{}
	mergeFixtureSamples(mergeFixtureSamplesMap, fixture.ParamSamples)
	mergeFixtureSamples(mergeFixtureSamplesMap, fixture.BodySamples)
	fixture.ParamSamples = sortedFixtureValues(mergeFixtureSamplesMap)
	fixture.HasAuth = hasAuthHeaders(entry.RequestHeaders)

	return fixture
}

func extractBodyFieldSamples(body string, contentType string) ([]string, map[string]string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, nil
	}

	switch {
	case strings.Contains(contentType, "json"):
		var value any
		if err := json.Unmarshal([]byte(body), &value); err != nil {
			return nil, nil
		}

		root := topLevelObject(value)
		if root == nil {
			return nil, nil
		}

		fields := make([]string, 0, len(root))
		samples := make(map[string]string, len(root))
		for key := range root {
			fields = append(fields, key)
			samples[key] = syntheticFixtureValue(key, root[key])
		}
		sort.Strings(fields)
		return fields, samples
	case strings.Contains(contentType, "form-urlencoded"):
		values := ParseFormBody(body)
		samples := make(map[string]string, len(values))
		for key, value := range values {
			samples[key] = syntheticFixtureValue(key, value)
		}
		return sortedKeysFromMap(values), samples
	default:
		return nil, nil
	}
}

var (
	moneyValueRE     = regexp.MustCompile(`^\$?\d+(?:\.\d{2})?$`)
	dateValueRE      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	cardLast4ValueRE = regexp.MustCompile(`^\d{4}$`)
)

func syntheticFixtureValue(name string, raw any) string {
	value := strings.TrimSpace(fmt.Sprint(raw))
	nameLower := strings.ToLower(name)
	switch {
	case piiplaceholders.OrderIDPattern().MatchString(value):
		if strings.HasPrefix(strings.ToUpper(value), "D01-") {
			return piiplaceholders.SyntheticDigitalOrderID
		}
		return piiplaceholders.SyntheticOrderID
	case piiplaceholders.ASINPattern().MatchString(value):
		return piiplaceholders.PrimarySyntheticASIN
	case cardLast4ValueRE.MatchString(value) && strings.Contains(nameLower, "last"):
		return piiplaceholders.SyntheticCardLast4
	case moneyValueRE.MatchString(value) && (strings.Contains(nameLower, "money") || strings.Contains(nameLower, "amount") || strings.Contains(nameLower, "price") || strings.Contains(nameLower, "total")):
		return piiplaceholders.SyntheticMoney
	case dateValueRE.MatchString(value) || isDateFixtureField(nameLower):
		return piiplaceholders.SyntheticDate
	default:
		return "example-value"
	}
}

func isDateFixtureField(nameLower string) bool {
	normalized := strings.ReplaceAll(nameLower, "-", "_")
	return normalized == "date" || strings.HasSuffix(normalized, "_date")
}

func mergeFixtureSamples(samples map[string]string, values []FixtureValue) {
	for _, value := range values {
		if value.Name == "" {
			continue
		}
		if _, exists := samples[value.Name]; !exists {
			samples[value.Name] = value.Value
		}
	}
}

func sortedFixtureValues(samples map[string]string) []FixtureValue {
	if len(samples) == 0 {
		return nil
	}
	names := make([]string, 0, len(samples))
	for name := range samples {
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]FixtureValue, 0, len(names))
	for _, name := range names {
		values = append(values, FixtureValue{Name: name, Value: samples[name]})
	}
	return values
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

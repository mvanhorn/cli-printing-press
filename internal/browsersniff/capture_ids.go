package browsersniff

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/piiplaceholders"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

const syntheticIDMarker = "example"

var (
	uuidInTextPattern       = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`)
	prefixedIDInTextPattern = regexp.MustCompile(`\b[a-z]{1,5}_[A-Za-z0-9]{8,}\b`)
)

// Live HAR captures copy session resource ids into spec defaults. Those
// values must not ship in public prints; hashes, dispatch discriminators,
// and vendor-prefix tokens stay so secret detection and GraphQL persisted
// queries keep working.
func SanitizeSpecCapturedResourceIDs(apiSpec *spec.APISpec) {
	if apiSpec == nil {
		return
	}
	apiSpec.Resources = sanitizeCapturedResourceIDResources(apiSpec.Resources)
}

func sanitizeCapturedResourceIDResources(resources map[string]spec.Resource) map[string]spec.Resource {
	if len(resources) == 0 {
		return resources
	}
	for name, resource := range resources {
		resources[name] = sanitizeCapturedResourceIDResource(resource)
	}
	return resources
}

func sanitizeCapturedResourceIDResource(resource spec.Resource) spec.Resource {
	if len(resource.Endpoints) > 0 {
		for name, endpoint := range resource.Endpoints {
			resource.Endpoints[name] = sanitizeCapturedResourceIDEndpoint(endpoint)
		}
	}
	resource.SubResources = sanitizeCapturedResourceIDResources(resource.SubResources)
	return resource
}

func sanitizeCapturedResourceIDEndpoint(endpoint spec.Endpoint) spec.Endpoint {
	sanitizeCapturedResourceIDParams(endpoint.Params)
	sanitizeCapturedResourceIDParams(endpoint.Body)
	endpoint.Example = replaceCapturedResourceIDsInText(endpoint.Example)
	endpoint.HappyArgs = replaceCapturedResourceIDsInText(endpoint.HappyArgs)
	endpoint.HappyStdin = sanitizeCapturedResourceIDJSONString(endpoint.HappyStdin)
	return endpoint
}

func sanitizeCapturedResourceIDParams(params []spec.Param) {
	for i := range params {
		sanitizeCapturedResourceIDParam(&params[i])
	}
}

func sanitizeCapturedResourceIDParam(param *spec.Param) {
	if param == nil {
		return
	}
	if !param.DispatchParam {
		if _, ok := capturedResourceIDScalar(param.Default); ok {
			param.Default = nil
		} else {
			param.Default = sanitizeCapturedResourceIDValue(param.Default)
		}
	}
	param.Example = sanitizeCapturedResourceIDValue(param.Example)
	sanitizeCapturedResourceIDParams(param.Fields)
}

func capturedResourceIDScalar(value any) (string, bool) {
	s, ok := value.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if !isCapturedResourceID(s) {
		return "", false
	}
	return s, true
}

func sanitizeCapturedResourceIDJSONString(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return value
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return replaceCapturedResourceIDsInText(value)
	}
	sanitized := sanitizeCapturedResourceIDValue(parsed)
	out, err := json.Marshal(sanitized)
	if err != nil {
		return replaceCapturedResourceIDsInText(value)
	}
	return string(out)
}

func sanitizeCapturedResourceIDValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if skipCapturedResourceIDKey(key) {
				continue
			}
			typed[key] = sanitizeCapturedResourceIDValue(child)
		}
		return typed
	case []any:
		for i, child := range typed {
			typed[i] = sanitizeCapturedResourceIDValue(child)
		}
		return typed
	case string:
		if isCapturedResourceID(typed) {
			return syntheticCapturedResourceID(typed)
		}
		return typed
	default:
		return value
	}
}

func skipCapturedResourceIDKey(key string) bool {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", "")) {
	case "sha256hash", "hash", "digest", "checksum":
		return true
	default:
		return false
	}
}

func isCapturedResourceID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || isSyntheticCapturedResourceID(value) {
		return false
	}
	if hashSegmentPattern.MatchString(value) {
		return false
	}
	if uuidSegmentPattern.MatchString(value) {
		return true
	}
	if prefixedIDPattern.MatchString(value) && !capturedResourceIDSecretPrefix(value) && looksOpaqueID(value) {
		return true
	}
	return longAlnumIDPattern.MatchString(value) && looksOpaqueID(value)
}

func capturedResourceIDSecretPrefix(value string) bool {
	prefix, _, ok := strings.Cut(value, "_")
	if !ok {
		return false
	}
	switch prefix {
	case "ghp", "gho", "ghs", "sk":
		return true
	default:
		return false
	}
}

func isSyntheticCapturedResourceID(value string) bool {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, piiplaceholders.SyntheticUUID) {
		return true
	}
	_, tail, ok := strings.Cut(value, "_")
	if ok && strings.HasPrefix(tail, syntheticIDMarker) {
		return true
	}
	return strings.HasPrefix(value, syntheticIDMarker)
}

func syntheticCapturedResourceID(value string) string {
	value = strings.TrimSpace(value)
	if uuidSegmentPattern.MatchString(value) {
		return piiplaceholders.SyntheticUUID
	}
	if prefix, tail, ok := strings.Cut(value, "_"); ok && prefixedIDPattern.MatchString(value) {
		return prefix + "_" + padSyntheticIDTail(len(tail))
	}
	return padSyntheticIDTail(len(value))
}

func padSyntheticIDTail(length int) string {
	if length <= len(syntheticIDMarker) {
		return syntheticIDMarker
	}
	return syntheticIDMarker + strings.Repeat("0", length-len(syntheticIDMarker))
}

func replaceCapturedResourceIDsInText(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	out := uuidInTextPattern.ReplaceAllString(value, piiplaceholders.SyntheticUUID)
	return prefixedIDInTextPattern.ReplaceAllStringFunc(out, func(match string) string {
		if !isCapturedResourceID(match) {
			return match
		}
		return syntheticCapturedResourceID(match)
	})
}

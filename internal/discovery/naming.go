package discovery

import (
	"fmt"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
)

func ResourceKey(path string) (string, string) {
	segments := SignificantSegments(path)
	if len(segments) == 0 {
		return "", ""
	}

	// Use only the first significant segment as the resource key.
	// This prevents slashes in resource names which break the generator's
	// filepath.Join and Cobra Use field.
	return segments[0], segments[len(segments)-1]
}

func SignificantSegments(path string) []string {
	parts := strings.Split(path, "/")
	segments := make([]string, 0, len(parts))
	for _, segment := range parts {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			continue
		}
		if segment == "api" || isVersionSegment(segment) {
			continue
		}
		segments = append(segments, segment)
	}

	return segments
}

func isVersionSegment(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, r := range segment[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func EndpointName(method string, normalizedPath string) string {
	resource := "endpoint"
	segments := SignificantSegments(normalizedPath)
	if len(segments) > 0 {
		resource = strings.ReplaceAll(segments[len(segments)-1], "-", "_")
	}

	switch strings.ToUpper(method) {
	case "GET":
		if strings.Contains(normalizedPath, "{") {
			return "get_" + resource
		}
		return "list_" + resource
	case "POST":
		return "create_" + resource
	case "PUT", "PATCH":
		return "update_" + resource
	case "DELETE":
		return "delete_" + resource
	default:
		return strings.ToLower(method) + "_" + resource
	}
}

func UniqueEndpointName(endpoints map[string]spec.Endpoint, base string) string {
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s_%d", base, i)
		if _, exists := endpoints[name]; !exists {
			return name
		}
	}
}

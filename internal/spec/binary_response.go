package spec

import "strings"

// HasBinaryResponses reports whether any endpoint in the spec can return a
// file/download payload that needs raw streaming.
func (s *APISpec) HasBinaryResponses() bool {
	if s == nil {
		return false
	}
	return resourcesHaveBinaryResponses(s.Resources)
}

func resourcesHaveBinaryResponses(resources map[string]Resource) bool {
	for _, resource := range resources {
		for _, endpoint := range resource.Endpoints {
			if endpoint.HasBinaryResponse() {
				return true
			}
		}
		if resourcesHaveBinaryResponses(resource.SubResources) {
			return true
		}
	}
	return false
}

// HasBinaryResponse reports whether an endpoint can return a non-JSON payload
// such as a PDF, CSV, or XLSX. Error-only JSON responses do not disqualify the
// endpoint from binary handling.
func (e Endpoint) HasBinaryResponse() bool {
	for _, contentType := range e.ResponseContentTypes {
		if isBinaryContentType(contentType) {
			return true
		}
	}
	return false
}

// HasJSONResponse reports whether an endpoint declares a JSON success payload.
func (e Endpoint) HasJSONResponse() bool {
	if len(e.ResponseContentTypes) == 0 {
		return true
	}
	for _, contentType := range e.ResponseContentTypes {
		if isJSONContentType(contentType) {
			return true
		}
	}
	return false
}

// NeedsAcceptFlag reports whether callers should be able to choose between a
// declared JSON representation and a binary representation.
func (e Endpoint) NeedsAcceptFlag() bool {
	return e.HasBinaryResponse() && e.HasJSONResponse()
}

// DefaultBinaryAccept returns the first declared non-JSON response MIME type.
func (e Endpoint) DefaultBinaryAccept() string {
	for _, contentType := range e.ResponseContentTypes {
		normalized := normalizeContentType(contentType)
		if normalized != "" && isBinaryContentType(normalized) {
			return normalized
		}
	}
	return "application/octet-stream"
}

func isBinaryContentType(contentType string) bool {
	normalized := normalizeContentType(contentType)
	if normalized == "" || isJSONContentType(normalized) {
		return false
	}
	if strings.HasPrefix(normalized, "image/") ||
		strings.HasPrefix(normalized, "audio/") ||
		strings.HasPrefix(normalized, "video/") {
		return true
	}
	switch normalized {
	case "application/pdf",
		"application/octet-stream",
		"application/zip",
		"application/gzip",
		"application/x-gzip",
		"application/x-tar",
		"application/x-zip-compressed",
		"text/csv",
		"application/csv":
		return true
	}
	if strings.Contains(normalized, "spreadsheet") ||
		strings.Contains(normalized, "excel") ||
		strings.Contains(normalized, "wordprocessingml") ||
		strings.Contains(normalized, "presentationml") {
		return true
	}
	return false
}

func isJSONContentType(contentType string) bool {
	normalized := normalizeContentType(contentType)
	return normalized == "" ||
		normalized == "application/json" ||
		normalized == "text/json" ||
		strings.HasSuffix(normalized, "+json")
}

func normalizeContentType(contentType string) string {
	contentType = strings.TrimSpace(strings.ToLower(contentType))
	if i := strings.Index(contentType, ";"); i >= 0 {
		contentType = strings.TrimSpace(contentType[:i])
	}
	return contentType
}

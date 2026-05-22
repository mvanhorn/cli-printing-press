package browsersniff

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func ParseHAR(path string) (*HAR, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	var har HAR
	if err := json.Unmarshal(data, &har); err != nil {
		return nil, fmt.Errorf("parsing har json: %w", err)
	}

	return &har, nil
}

func ParseEnriched(path string) (*EnrichedCapture, error) {
	capture, err := LoadCapture(path)
	if err != nil {
		return nil, err
	}

	return capture, nil
}

func ParseCapture(path string) ([]EnrichedEntry, string, error) {
	capture, err := LoadCapture(path)
	if err != nil {
		return nil, "", err
	}

	return capture.Entries, capture.TargetURL, nil
}

func convertHAREntry(entry HAREntry) EnrichedEntry {
	headers := make(map[string]string, len(entry.Request.Headers))
	for _, header := range entry.Request.Headers {
		headers[header.Name] = header.Value
	}
	responseHeaders := make(map[string]string, len(entry.Response.Headers))
	for _, header := range entry.Response.Headers {
		responseHeaders[header.Name] = header.Value
	}

	requestBody := ""
	if entry.Request.PostData != nil {
		requestBody = entry.Request.PostData.Text
	}

	return EnrichedEntry{
		Method:              entry.Request.Method,
		URL:                 entry.Request.URL,
		HTTPVersion:         pickHARHTTPVersion(entry),
		StartedDateTime:     entry.StartedDateTime,
		DurationMS:          entry.Time,
		RequestBody:         requestBody,
		ResponseBody:        entry.Response.Content.Text,
		ResponseStatus:      entry.Response.Status,
		ResponseContentType: entry.Response.Content.MimeType,
		RequestHeaders:      headers,
		ResponseHeaders:     responseHeaders,
	}
}

// pickHARHTTPVersion returns the response httpVersion when present,
// falling back to the request httpVersion. HAR exporters vary: Chrome
// fills both, some intermediaries fill only one. The response wire
// version is the more accurate signal for "what the origin spoke" so
// it wins when both are set.
func pickHARHTTPVersion(entry HAREntry) string {
	if v := strings.TrimSpace(entry.Response.HTTPVersion); v != "" {
		return v
	}
	return strings.TrimSpace(entry.Request.HTTPVersion)
}

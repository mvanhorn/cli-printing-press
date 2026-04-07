package megamcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// maxResponseBody is the maximum response body size the handler will read (10MB).
const maxResponseBody = 10 * 1024 * 1024

// MakeToolHandler returns an MCP tool handler function that makes HTTP requests
// to APIs using the tools manifest data. It handles auth, parameter routing
// (path/query/body), required headers, and response classification.
func MakeToolHandler(manifest *ToolsManifest, tool ManifestTool, httpClient *http.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug := manifest.APIName

		// 1. Fail-closed auth check.
		if !tool.NoAuth && manifest.Auth.Type != "" && manifest.Auth.Type != "none" {
			if !hasAuthConfigured(manifest) {
				return mcp.NewToolResultError(
					fmt.Sprintf("Authentication not configured for this API — call setup_guide(%q) for instructions", slug),
				), nil
			}
		}

		// 2. Build URL: substitute path params into the path template.
		args := req.GetArguments()
		apiPath := tool.Path

		for _, param := range tool.Params {
			if param.Location != "path" {
				continue
			}
			val, ok := args[param.Name]
			if !ok || val == nil {
				continue
			}
			encoded := url.PathEscape(fmt.Sprintf("%v", val))
			apiPath = strings.Replace(apiPath, "{"+param.Name+"}", encoded, 1)
		}

		// Check for unsubstituted path placeholders.
		if idx := strings.Index(apiPath, "{"); idx >= 0 {
			end := strings.Index(apiPath[idx:], "}")
			if end > 0 {
				var missing []string
				remaining := apiPath
				for {
					start := strings.Index(remaining, "{")
					if start < 0 {
						break
					}
					close := strings.Index(remaining[start:], "}")
					if close < 0 {
						break
					}
					placeholder := remaining[start+1 : start+close]
					missing = append(missing, placeholder)
					remaining = remaining[start+close+1:]
				}
				return mcp.NewToolResultError(
					fmt.Sprintf("Missing required path parameter(s): %s", strings.Join(missing, ", ")),
				), nil
			}
		}

		// 3. Build query string.
		queryValues := url.Values{}
		for _, param := range tool.Params {
			if param.Location != "query" {
				continue
			}
			val, ok := args[param.Name]
			if !ok || val == nil {
				continue
			}
			queryValues.Set(param.Name, fmt.Sprintf("%v", val))
		}

		// 4. Build body for POST/PUT/PATCH.
		var bodyReader io.Reader
		method := strings.ToUpper(tool.Method)
		if method == "POST" || method == "PUT" || method == "PATCH" {
			bodyParams := make(map[string]any)
			for _, param := range tool.Params {
				if param.Location != "body" {
					continue
				}
				val, ok := args[param.Name]
				if !ok || val == nil {
					continue
				}
				bodyParams[param.Name] = val
			}
			if len(bodyParams) > 0 {
				bodyBytes, err := json.Marshal(bodyParams)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Error serializing request body: %v", err)), nil
				}
				bodyReader = bytes.NewReader(bodyBytes)
			}
		}

		// Construct the full URL.
		baseURL := strings.TrimRight(manifest.BaseURL, "/")
		fullURL := baseURL + apiPath

		if len(queryValues) > 0 {
			fullURL += "?" + queryValues.Encode()
		}

		// 5. Post-assembly URL validation: verify host matches base URL.
		assembledURL, err := url.Parse(fullURL)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid assembled URL: %v", err)), nil
		}
		baseURLParsed, err := url.Parse(baseURL)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid base URL: %v", err)), nil
		}
		if assembledURL.Host != baseURLParsed.Host {
			return mcp.NewToolResultError(
				fmt.Sprintf("URL host mismatch: expected %q but got %q — possible injection attempt", baseURLParsed.Host, assembledURL.Host),
			), nil
		}

		// Build HTTP request.
		httpReq, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error creating HTTP request: %v", err)), nil
		}

		// 6. Set headers.
		// Auth header or query param.
		if manifest.Auth.In == "query" {
			paramName, paramValue, authErr := BuildAuthQueryParam(manifest)
			if authErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error building auth query param: %v", authErr)), nil
			}
			if paramName != "" && paramValue != "" {
				q := httpReq.URL.Query()
				q.Set(paramName, paramValue)
				httpReq.URL.RawQuery = q.Encode()
			}
		} else {
			headerName, headerValue, authErr := BuildAuthHeader(manifest)
			if authErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error building auth header: %v", authErr)), nil
			}
			if headerName != "" && headerValue != "" {
				httpReq.Header.Set(headerName, headerValue)
			}
		}

		// Required headers from manifest.
		for _, h := range manifest.RequiredHeaders {
			httpReq.Header.Set(h.Name, h.Value)
		}

		// Per-tool header overrides.
		for _, h := range tool.HeaderOverrides {
			httpReq.Header.Set(h.Name, h.Value)
		}

		// Content-Type for body-bearing methods.
		if method == "POST" || method == "PUT" || method == "PATCH" {
			httpReq.Header.Set("Content-Type", "application/json")
		}

		httpReq.Header.Set("User-Agent", "printing-press-mcp")

		// 7. Make request.
		resp, err := httpClient.Do(httpReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Network error: %v", err)), nil
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error reading response body: %v", err)), nil
		}

		// 8. Handle response.
		respText := string(respBody)

		// Success (2xx).
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return mcp.NewToolResultText(respText), nil
		}

		// 401/403 — generic auth error, no env var names, redact credentials.
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			redacted := RedactCredentials(respText, manifest)
			return mcp.NewToolResultError(
				fmt.Sprintf("Authentication error (HTTP %d): %s\n\nAuthentication not configured — call setup_guide(%q) for instructions",
					resp.StatusCode, SanitizeText(redacted, 500), slug),
			), nil
		}

		// 429 — rate limited, surface response body.
		if resp.StatusCode == 429 {
			return mcp.NewToolResultError(
				fmt.Sprintf("Rate limited (HTTP 429): %s", SanitizeText(respText, 1000)),
			), nil
		}

		// Other 4xx — return API error with credential redaction.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			redacted := RedactCredentials(respText, manifest)
			return mcp.NewToolResultError(
				fmt.Sprintf("API error (HTTP %d): %s", resp.StatusCode, SanitizeText(redacted, 1000)),
			), nil
		}

		// 5xx — server error.
		return mcp.NewToolResultError(
			fmt.Sprintf("Server error (HTTP %d): %s", resp.StatusCode, SanitizeText(respText, 500)),
		), nil
	}
}

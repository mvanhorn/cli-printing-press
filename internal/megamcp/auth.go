package megamcp

import (
	"fmt"
	"os"
	"strings"
)

// ApplyAuthFormat expands {PLACEHOLDER} tokens in the format string with values
// from the envVars map. envVars is keyed by env var name, values are from os.Getenv.
// Also supports semantic placeholders: {token} and {access_token} map to the first
// env var value. Rejects format strings with unrecognized placeholders.
func ApplyAuthFormat(format string, envVars map[string]string) (string, error) {
	if format == "" {
		// No format string — return the first env var value directly.
		for _, v := range envVars {
			return v, nil
		}
		return "", nil
	}

	result := format

	replacements := make(map[string]string)
	validPlaceholders := make(map[string]bool)
	var firstValue string
	for name, value := range envVars {
		replacements[name] = value
		validPlaceholders["{"+name+"}"] = true
		if firstValue == "" {
			firstValue = value
		}
	}
	if firstValue != "" {
		replacements["token"] = firstValue
		replacements["access_token"] = firstValue
		validPlaceholders["{token}"] = true
		validPlaceholders["{access_token}"] = true
	}

	// Validate all placeholders in the format string before substitution.
	for i := 0; i < len(result); {
		idx := strings.Index(result[i:], "{")
		if idx < 0 {
			break
		}
		idx += i
		closeIdx := strings.Index(result[idx:], "}")
		if closeIdx < 0 {
			break
		}
		placeholder := result[idx : idx+closeIdx+1]
		if !validPlaceholders[placeholder] {
			return "", fmt.Errorf("unrecognized placeholder %s in auth format %q", placeholder, format)
		}
		i = idx + closeIdx + 1
	}

	// Perform substitutions.
	for key, value := range replacements {
		result = strings.ReplaceAll(result, "{"+key+"}", value)
	}

	return result, nil
}

// BuildAuthHeader reads env vars from manifest.Auth.EnvVars via os.Getenv,
// applies format string expansion, and returns the header name and value.
// Returns empty strings if no auth configured (type is "none" or empty).
func BuildAuthHeader(manifest *ToolsManifest) (headerName string, headerValue string, err error) {
	if manifest == nil || manifest.Auth.Type == "" || manifest.Auth.Type == "none" {
		return "", "", nil
	}

	// Build envVars map from os.Getenv.
	envVars := make(map[string]string)
	for _, envName := range manifest.Auth.EnvVars {
		val := os.Getenv(envName)
		if val != "" {
			envVars[envName] = val
		}
	}

	// If no env vars are set, return empty — caller decides if that's an error.
	if len(envVars) == 0 {
		return "", "", nil
	}

	// Determine the header name.
	headerName = manifest.Auth.Header
	if headerName == "" {
		headerName = "Authorization"
	}

	// Apply the format string, or construct a default value.
	if manifest.Auth.Format != "" {
		headerValue, err = ApplyAuthFormat(manifest.Auth.Format, envVars)
		if err != nil {
			return "", "", fmt.Errorf("applying auth format: %w", err)
		}
	} else {
		// No format string — use the first env var value directly.
		// For bearer_token, default to "Bearer {value}".
		for _, envName := range manifest.Auth.EnvVars {
			val := envVars[envName]
			if val != "" {
				if manifest.Auth.Type == "bearer_token" {
					headerValue = "Bearer " + val
				} else {
					headerValue = val
				}
				break
			}
		}
	}

	return headerName, headerValue, nil
}

// BuildAuthQueryParam constructs the auth query parameter name and value
// for APIs that use In:"query" auth. Returns empty strings if not applicable.
func BuildAuthQueryParam(manifest *ToolsManifest) (paramName string, paramValue string, err error) {
	if manifest == nil || manifest.Auth.Type == "" || manifest.Auth.Type == "none" {
		return "", "", nil
	}
	if manifest.Auth.In != "query" {
		return "", "", nil
	}

	// Build envVars map from os.Getenv.
	envVars := make(map[string]string)
	for _, envName := range manifest.Auth.EnvVars {
		val := os.Getenv(envName)
		if val != "" {
			envVars[envName] = val
		}
	}

	if len(envVars) == 0 {
		return "", "", nil
	}

	// The query param name comes from auth.header (or defaults to "api_key").
	paramName = manifest.Auth.Header
	if paramName == "" {
		paramName = "api_key"
	}

	// Apply format or use raw value.
	if manifest.Auth.Format != "" {
		paramValue, err = ApplyAuthFormat(manifest.Auth.Format, envVars)
		if err != nil {
			return "", "", fmt.Errorf("applying auth format: %w", err)
		}
	} else {
		for _, envName := range manifest.Auth.EnvVars {
			val := envVars[envName]
			if val != "" {
				paramValue = val
				break
			}
		}
	}

	return paramName, paramValue, nil
}

// RedactCredentials replaces known credential values (from env vars) with
// [REDACTED] in the given string. Used before returning 4xx error bodies
// to prevent credential leakage.
func RedactCredentials(body string, manifest *ToolsManifest) string {
	if manifest == nil {
		return body
	}
	for _, envName := range manifest.Auth.EnvVars {
		val := os.Getenv(envName)
		if val != "" && len(val) >= 4 {
			body = strings.ReplaceAll(body, val, "[REDACTED]")
		}
	}
	return body
}

// hasAuthConfigured checks whether the required auth env vars are set.
func hasAuthConfigured(manifest *ToolsManifest) bool {
	if manifest == nil || manifest.Auth.Type == "" || manifest.Auth.Type == "none" {
		return true // No auth needed.
	}
	for _, envName := range manifest.Auth.EnvVars {
		if os.Getenv(envName) != "" {
			return true
		}
	}
	return false
}

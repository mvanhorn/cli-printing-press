package crowdsniff

import (
	"regexp"
	"strings"
	"unicode"
)

// EnrichWithParams does a second pass over SDK source content, finding the params
// object literal after each HTTP method call and extracting param names, types,
// and required/optional status.
//
// This function operates on full content (not line-by-line) — a deliberate
// architectural asymmetry with GrepEndpoints. GrepEndpoints works line-by-line
// because endpoint discovery only needs method+path from a single line.
// Param extraction needs multi-line object literals and backward scanning for
// function signatures, which requires full-content access.
//
// EnrichWithParams must match raw URL patterns in the content independently,
// then correlate to DiscoveredEndpoint entries by comparing cleaned paths.
// It does NOT substring-match endpoint.Path against content.
func EnrichWithParams(content string, endpoints []DiscoveredEndpoint) []DiscoveredEndpoint {
	if len(endpoints) == 0 {
		return endpoints
	}

	// Build a lookup from (method, path) -> index into endpoints slice.
	// We'll collect params per endpoint index.
	type epKey struct {
		method string
		path   string
	}
	epIndex := make(map[epKey]int)
	for i, ep := range endpoints {
		k := epKey{method: ep.Method, path: ep.Path}
		// First occurrence wins (consistent with dedup).
		if _, exists := epIndex[k]; !exists {
			epIndex[k] = i
		}
	}

	// Independently scan content for HTTP method calls using the same patterns.
	methodIndexes := httpMethodCall.FindAllStringSubmatchIndex(content, -1)

	for _, methodIdx := range methodIndexes {
		method := strings.ToUpper(content[methodIdx[2]:methodIdx[3]])
		if !validHTTPMethods[method] {
			continue
		}

		// Find the first URL path after this method call's opening paren.
		remainder := content[methodIdx[1]:]
		pathMatchIdx := urlPathLiteral.FindStringSubmatchIndex(remainder)
		if pathMatchIdx == nil {
			continue
		}

		rawPath := remainder[pathMatchIdx[2]:pathMatchIdx[3]]
		cleaned := cleanPath(rawPath)

		k := epKey{method: method, path: cleaned}
		idx, found := epIndex[k]
		if !found {
			continue
		}

		// Position in content where the URL path literal ends (after closing quote).
		urlEndPos := methodIdx[1] + pathMatchIdx[1]

		// Extract params object starting from after the URL.
		params := extractParamsFromPosition(content, urlEndPos)
		if params == nil {
			continue
		}

		// Look backward for function signature to determine required/optional.
		funcStart := max(methodIdx[0]-maxFuncSignatureLookback, 0)
		preamble := content[funcStart:methodIdx[0]]
		requiredNames := extractRequiredFromSignature(preamble)

		// Apply required/optional from function signature correlation.
		for i := range params {
			if _, isReq := requiredNames[params[i].Name]; isReq {
				params[i].Required = true
			}
		}

		// Only set params on the first match for this endpoint.
		// Avoids cross-product if the same endpoint path appears multiple times.
		if endpoints[idx].Params == nil {
			endpoints[idx].Params = params
		}
	}

	return endpoints
}

// maxParamScanDistance is the maximum distance forward from the URL match to
// look for the opening brace of a params object.
const maxParamScanDistance = 500

// maxFuncSignatureLookback is the maximum distance backward from the HTTP call
// to scan for a function definition when correlating required/optional params.
// 1000 bytes covers most real-world SDK methods while avoiding full-file scans.
const maxFuncSignatureLookback = 1000

// extractParamsFromPosition scans forward from urlEndPos in content for a
// params object literal ({ key1: val1, key2: val2 }) and extracts the keys.
// Returns nil if no params object is found or if the object is malformed.
func extractParamsFromPosition(content string, urlEndPos int) []DiscoveredParam {
	// Scan forward for ", {" or ",\n  {" pattern.
	scanEnd := min(urlEndPos+maxParamScanDistance, len(content))
	region := content[urlEndPos:scanEnd]

	braceStart := findParamsObjectStart(region)
	if braceStart < 0 {
		return nil
	}

	// braceStart is relative to urlEndPos, convert to absolute.
	absStart := urlEndPos + braceStart

	// Run brace-matching scanner from absStart.
	block := extractBraceBlock(content, absStart)
	if block == "" {
		return nil
	}

	return extractKeysFromBlock(block)
}

// findParamsObjectStart finds the position of the opening brace for a params
// object in the region after a URL match. Looks for a comma followed by
// optional whitespace/newlines then an opening brace.
// Returns the position of '{' relative to region start, or -1 if not found.
func findParamsObjectStart(region string) int {
	foundComma := false
	for i, ch := range region {
		if !foundComma {
			switch ch {
			case ',':
				foundComma = true
			case ')':
				// Closing paren before finding comma means no params.
				return -1
			}
			continue
		}
		// After comma, skip whitespace/newlines until we find '{' or give up.
		switch {
		case ch == '{':
			return i
		case !unicode.IsSpace(ch):
			// Non-whitespace, non-brace after comma — not a params object.
			// Could be a second string argument or variable.
			return -1
		}
	}
	return -1
}

// extractBraceBlock starts at content[pos] which must be '{', and uses a
// brace-matching scanner to find the closing '}'. Returns the content between
// the braces (exclusive), or "" if unbalanced.
// Tracks string literal and comment state to skip braces inside quotes and comments.
func extractBraceBlock(content string, pos int) string {
	if pos >= len(content) || content[pos] != '{' {
		return ""
	}

	depth := 0
	inString := false
	var stringChar byte
	inLineComment := false
	inBlockComment := false

	scanEnd := min(pos+maxParamScanDistance, len(content))

	for i := pos; i < scanEnd; i++ {
		ch := content[i]

		// Line comment: skip until newline.
		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		// Block comment: skip until */.
		if inBlockComment {
			if ch == '*' && i+1 < scanEnd && content[i+1] == '/' {
				inBlockComment = false
				i++ // skip the '/'
			}
			continue
		}

		// Track string literal state.
		if inString {
			if ch == '\\' {
				i++ // skip escaped character
				continue
			}
			if ch == stringChar {
				inString = false
			}
			continue
		}

		// Detect comment start.
		if ch == '/' && i+1 < scanEnd {
			next := content[i+1]
			if next == '/' {
				inLineComment = true
				i++ // skip second '/'
				continue
			}
			if next == '*' {
				inBlockComment = true
				i++ // skip '*'
				continue
			}
		}

		if ch == '\'' || ch == '"' || ch == '`' {
			inString = true
			stringChar = ch
			continue
		}

		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				// Return content between outer braces.
				return content[pos+1 : i]
			}
		}
	}

	// Unbalanced — return empty.
	return ""
}

// extractKeysFromBlock parses the content between outer braces of an object
// literal and extracts top-level keys with type inference.
// Handles: shorthand properties, key-value pairs, nested objects (skipped),
// and trailing commas.
func extractKeysFromBlock(block string) []DiscoveredParam {
	var params []DiscoveredParam
	seen := make(map[string]bool)

	entries := splitObjectEntries(block)
	for _, entry := range entries {
		entry = stripComments(entry)
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		key, value := parseObjectEntry(entry)
		if key == "" {
			continue
		}
		// Skip keys that start with spread operator.
		if strings.HasPrefix(key, "...") {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		paramType := inferType(key, value)
		params = append(params, DiscoveredParam{
			Name: key,
			Type: paramType,
		})
	}

	if len(params) == 0 {
		return nil
	}
	return params
}

// splitObjectEntries splits an object literal body into entries, respecting
// nested braces, string literals, and comments. Only splits on commas at depth 0.
func splitObjectEntries(block string) []string {
	var entries []string
	depth := 0
	inString := false
	var stringChar byte
	inLineComment := false
	inBlockComment := false
	start := 0

	for i := 0; i < len(block); i++ {
		ch := block[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && i+1 < len(block) && block[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString {
			if ch == '\\' {
				i++
				continue
			}
			if ch == stringChar {
				inString = false
			}
			continue
		}

		// Detect comment start.
		if ch == '/' && i+1 < len(block) {
			next := block[i+1]
			if next == '/' {
				inLineComment = true
				i++
				continue
			}
			if next == '*' {
				inBlockComment = true
				i++
				continue
			}
		}

		if ch == '\'' || ch == '"' || ch == '`' {
			inString = true
			stringChar = ch
			continue
		}

		if ch == '{' || ch == '[' || ch == '(' {
			depth++
		} else if ch == '}' || ch == ']' || ch == ')' {
			depth--
		} else if ch == ',' && depth == 0 {
			entries = append(entries, block[start:i])
			start = i + 1
		}
	}
	// Last entry (no trailing comma).
	if start < len(block) {
		entries = append(entries, block[start:])
	}
	return entries
}

// stripComments removes JavaScript line comments (//) and block comments (/* */)
// from a string, preserving content outside comments. Respects string literals.
func stripComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	var stringChar byte

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if inString {
			b.WriteByte(ch)
			if ch == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			if ch == stringChar {
				inString = false
			}
			continue
		}

		if ch == '\'' || ch == '"' || ch == '`' {
			inString = true
			stringChar = ch
			b.WriteByte(ch)
			continue
		}

		if ch == '/' && i+1 < len(s) {
			next := s[i+1]
			if next == '/' {
				// Line comment: skip to end of line.
				for i+1 < len(s) && s[i+1] != '\n' {
					i++
				}
				continue
			}
			if next == '*' {
				// Block comment: skip to */.
				i += 2
				for i+1 < len(s) {
					if s[i] == '*' && s[i+1] == '/' {
						i++
						break
					}
					i++
				}
				continue
			}
		}

		b.WriteByte(ch)
	}

	return b.String()
}

// parseObjectEntry parses a single object entry and returns (key, value).
// Handles:
//   - "key: value" → ("key", "value")
//   - "key" (shorthand) → ("key", "")
func parseObjectEntry(entry string) (string, string) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", ""
	}

	// Find the first colon that's not inside a string or nested structure.
	colonIdx := findTopLevelColon(entry)
	if colonIdx < 0 {
		// Shorthand property: bare identifier.
		key := strings.TrimSpace(entry)
		if isValidIdentifier(key) {
			return key, ""
		}
		return "", ""
	}

	key := strings.TrimSpace(entry[:colonIdx])
	value := strings.TrimSpace(entry[colonIdx+1:])
	if isValidIdentifier(key) {
		return key, value
	}
	return "", ""
}

// findTopLevelColon finds the position of the first colon at depth 0 in entry,
// respecting string literals. Returns -1 if not found.
func findTopLevelColon(entry string) int {
	inString := false
	var stringChar byte

	for i := 0; i < len(entry); i++ {
		ch := entry[i]

		if inString {
			if ch == '\\' {
				i++
				continue
			}
			if ch == stringChar {
				inString = false
			}
			continue
		}

		if ch == '\'' || ch == '"' || ch == '`' {
			inString = true
			stringChar = ch
			continue
		}

		if ch == ':' {
			return i
		}
	}
	return -1
}

// isValidIdentifier checks if s is a valid JS identifier (letters, digits,
// underscores, $, starting with a non-digit).
func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, ch := range s {
		if i == 0 {
			if !unicode.IsLetter(ch) && ch != '_' && ch != '$' {
				return false
			}
		} else {
			if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' && ch != '$' {
				return false
			}
		}
	}
	return true
}

// inferType applies heuristic rules to determine the parameter type from its
// key name and value expression.
func inferType(key, value string) string {
	// Value-based heuristics first (more specific).
	if value != "" {
		if strings.Contains(value, ".join(") {
			return "string"
		}
		trimVal := strings.TrimSpace(value)
		if trimVal == "true" || trimVal == "false" {
			return "boolean"
		}
		if isNumericLiteral(trimVal) {
			return "integer"
		}
	}

	// Key-name-based heuristics.
	if integerKeyName.MatchString(key) {
		return "integer"
	}

	return "string"
}

// integerKeyName matches param names that are typically integers.
// Uses word boundaries to avoid matching substrings like "accountId".
var integerKeyName = regexp.MustCompile(`^(count|limit|offset|page|maxlength)$`)

// isNumericLiteral checks if a string looks like a numeric literal.
func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	// Allow leading minus for negative numbers.
	start := 0
	if s[0] == '-' {
		start = 1
	}
	if start >= len(s) {
		return false
	}
	hasDigit := false
	for i := start; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			hasDigit = true
		} else if s[i] == '.' {
			// Allow decimal point.
			continue
		} else {
			return false
		}
	}
	return hasDigit
}

// Function signature patterns for backward scanning.
var funcSignaturePatterns = []*regexp.Regexp{
	// async function name(...)
	regexp.MustCompile(`async\s+function\s+\w+\s*\(([^)]*)\)`),
	// function name(...)
	regexp.MustCompile(`function\s+\w+\s*\(([^)]*)\)`),
	// name(...) { — class method shorthand
	regexp.MustCompile(`\w+\s*\(([^)]*)\)\s*\{`),
	// name = async (...) or name = function(...)
	regexp.MustCompile(`\w+\s*=\s*(?:async\s+)?(?:function)?\s*\(([^)]*)\)`),
}

// extractRequiredFromSignature scans backward (the preamble before the HTTP
// call) for the nearest function definition and extracts which parameter names
// are required (positional args without defaults).
// Returns a set of required parameter names, or nil if no function signature
// is found.
func extractRequiredFromSignature(preamble string) map[string]bool {
	// Find the last function signature in the preamble (nearest to the HTTP call).
	var bestMatch string
	bestPos := -1

	for _, pat := range funcSignaturePatterns {
		allMatches := pat.FindAllStringSubmatchIndex(preamble, -1)
		for _, idx := range allMatches {
			if idx[0] > bestPos {
				bestPos = idx[0]
				bestMatch = preamble[idx[2]:idx[3]]
			}
		}
	}

	if bestMatch == "" {
		return nil
	}

	return parseSignatureParams(bestMatch)
}

// parseSignatureParams parses a function parameter list string and returns
// a set of names that are required (positional, no default value).
//
// Handles:
//   - Simple: "steamid" → required
//   - With default: "count = 10" → optional
//   - Destructured: "{ opt1 = true } = {}" → opt1 is optional
//   - Mixed: "id, { opt1 = true } = {}" → id is required, opt1 is optional
func parseSignatureParams(paramList string) map[string]bool {
	required := make(map[string]bool)
	paramList = strings.TrimSpace(paramList)
	if paramList == "" {
		return required
	}

	// Split on top-level commas (respecting nested braces).
	parts := splitObjectEntries(paramList)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if this is a destructured param: { key1 = default, key2 } = {}
		if strings.HasPrefix(part, "{") {
			// Extract names from the destructured object.
			// These are all optional (they have defaults or are in a default-assigned object).
			continue
		}

		// Check for default assignment: name = value
		if found := strings.Contains(part, "="); found {
			// Has a default → optional, skip.
			continue
		}

		// Simple positional parameter.
		name := strings.TrimSpace(part)
		if isValidIdentifier(name) {
			required[name] = true
		}
	}

	return required
}

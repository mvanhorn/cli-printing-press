package artifacts

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// printingPressManifestFilename is the generator-written CLI manifest.
// The pipeline package owns the canonical constant; this copy stays local
// so the PII scan does not import pipeline (pipeline already imports artifacts).
const printingPressManifestFilename = ".printing-press.json"

const (
	// Digit runs shorter than 8 are example ids, counts, and pagination.
	// Longer than 17 exceeds NACHA account-number width and is usually a
	// request token. False positives here block promote of a correct CLI.
	accountIDDigitMin = 8
	accountIDDigitMax = 17
	// Mixed tokens cover sniffed alphanumeric ids and IBANs (34 chars).
	accountIDMixedMin = 8
	accountIDMixedMax = 34
)

// accountIdentifierPhrases are joined consecutive key tokens. Matching is
// a suffix of the key path, so account.history.id does not match and
// digitalAccountIdentifier / account.number do.
var accountIdentifierPhrases = map[string]struct{}{
	"accountid":          {},
	"accountidentifier":  {},
	"accountnumber":      {},
	"accountno":          {},
	"accountnum":         {},
	"acctid":             {},
	"acctidentifier":     {},
	"acctnumber":         {},
	"acctno":             {},
	"partyid":            {},
	"partyidentifier":    {},
	"partynumber":        {},
	"partyno":            {},
	"partynum":           {},
	"customerid":         {},
	"customeridentifier": {},
	"customernumber":     {},
	"customerno":         {},
	"customernum":        {},
}

var accountIDPlaceholderPrefixes = []string{
	"acct_", "acct-", "acc_", "acc-",
	"account_", "account-",
	"cus_", "cust_", "pty_", "party_",
	"test_", "test-", "example_", "example-",
	"sample_", "sample-", "fake_", "fake-",
	"dummy_", "dummy-", "placeholder_", "placeholder-",
	"your_", "your-",
}

// RFC 4122 example UUID and the all-zero UUID, compared after hyphens are stripped.
var wellKnownAccountIDPlaceholders = map[string]struct{}{
	"550e8400e29b41d4a716446655440000": {},
	"00000000000000000000000000000000": {},
}

type accountHit struct {
	line int
	col  int
	span string
}

func scanAccountIdentifiers(relSlash, content string) []PIIFinding {
	hits := scanJSONAccountIdentifiers(content)
	hits = append(hits, scanYAMLAccountIdentifiers(content)...)
	hits = dedupeAccountHits(hits)
	findings := make([]PIIFinding, 0, len(hits))
	for _, h := range hits {
		findings = append(findings, PIIFinding{
			Kind:        PIIKindAccountID,
			File:        relSlash,
			Line:        h.line,
			Column:      h.col,
			MatchedSpan: h.span,
		})
	}
	return findings
}

func dedupeAccountHits(hits []accountHit) []accountHit {
	if len(hits) < 2 {
		return hits
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].line != hits[j].line {
			return hits[i].line < hits[j].line
		}
		if hits[i].col != hits[j].col {
			return hits[i].col < hits[j].col
		}
		return hits[i].span < hits[j].span
	})
	out := make([]accountHit, 0, len(hits))
	lineStart := 0
	for _, h := range hits {
		if len(out) == 0 || out[len(out)-1].line != h.line {
			lineStart = len(out)
		}
		dup := false
		for _, prev := range out[lineStart:] {
			if prev.span != h.span {
				continue
			}
			d := h.col - prev.col
			if d < 0 {
				d = -d
			}
			if d <= 3 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, h)
		}
	}
	return out
}

func isAccountIdentifierKeyPath(path []string) bool {
	var tokens []string
	for _, key := range path {
		tokens = append(tokens, splitAccountKey(key)...)
	}
	n := len(tokens)
	if n == 0 {
		return false
	}
	if _, ok := accountIdentifierPhrases[tokens[n-1]]; ok {
		return true
	}
	if n >= 2 {
		if _, ok := accountIdentifierPhrases[tokens[n-2]+tokens[n-1]]; ok {
			return true
		}
	}
	return false
}

func splitAccountKey(key string) []string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		tokens = append(tokens, strings.ToLower(cur.String()))
		cur.Reset()
	}
	runes := []rune(key)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				flush()
			} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				flush()
			}
		}
		cur.WriteRune(r)
	}
	flush()
	return tokens
}

func isAccountIdentifierValue(raw string) bool {
	v := strings.TrimSpace(raw)
	// A leading plus is still the same digit run ("+4839201756").
	v = strings.TrimPrefix(v, "+")
	if v == "" || isAccountIDPlaceholder(v) {
		return false
	}
	if strings.ContainsAny(v, " \t\r\n") {
		return false
	}
	if isBareDigits(v) {
		if len(v) < accountIDDigitMin || len(v) > accountIDDigitMax {
			return false
		}
		return !isTrivialDigitRun(v)
	}
	if !isAccountIDMixedToken(v) {
		return false
	}
	compact := strings.ReplaceAll(v, "-", "")
	if len(compact) < accountIDMixedMin || len(compact) > accountIDMixedMax {
		return false
	}
	if !hasAccountIDLetter(compact) || !hasAccountIDDigit(compact) {
		return false
	}
	if _, ok := wellKnownAccountIDPlaceholders[strings.ToLower(compact)]; ok {
		return false
	}
	return !isAccountIDPlaceholder(compact)
}

func isAccountIDMixedToken(v string) bool {
	if v == "" || v[0] == '-' || v[len(v)-1] == '-' {
		return false
	}
	prevHyphen := false
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if ch == '-' {
			if prevHyphen {
				return false
			}
			prevHyphen = true
			continue
		}
		prevHyphen = false
		if (ch < '0' || ch > '9') && (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') {
			return false
		}
	}
	return true
}

func hasAccountIDLetter(s string) bool {
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
			return true
		}
	}
	return false
}

func hasAccountIDDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

func isTrivialDigitRun(s string) bool {
	switch s {
	case "1234567890", "0123456789", "9876543210":
		return true
	}
	if s == "" {
		return true
	}
	allSame := true
	asc := true
	desc := true
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			allSame = false
		}
		if s[i] != s[i-1]+1 {
			asc = false
		}
		if s[i]+1 != s[i-1] {
			desc = false
		}
	}
	return allSame || asc || desc
}

func isAccountIDPlaceholder(v string) bool {
	if strings.ContainsAny(v, "<>{}*$%") {
		return true
	}
	lower := strings.ToLower(v)
	switch lower {
	case "example", "placeholder", "sample", "dummy", "fake", "test",
		"redacted", "changeme", "todo", "none", "null", "undefined",
		"nil", "n/a", "na", "xxx", "xxxx", "xxxxx", "unknown",
		"account", "accountid", "customer", "customerid", "party", "partyid":
		return true
	}
	for _, p := range accountIDPlaceholderPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	for _, s := range []string{"_example", "-example", "_test", "-test", "_placeholder", "-placeholder"} {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	for _, needle := range []string{"example", "placeholder", "redacted", "changeme", "dummy"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// cliSpecSourceIsSniffed reports whether this CLI tree was printed from a
// browser-sniffed spec. The manifest may nest the field; the root spec
// only counts when spec_source is a top-level key, so an example buried
// in a vendor document does not drop the exemption.
func cliSpecSourceIsSniffed(root string) bool {
	if manifestSpecSourceIsSniffed(filepath.Join(root, printingPressManifestFilename)) {
		return true
	}
	for name := range rootVendorSpecFiles {
		if rootSpecFileIsSniffed(filepath.Join(root, name)) {
			return true
		}
	}
	return false
}

func manifestSpecSourceIsSniffed(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	data = bytes.TrimSpace(data)
	data = bytes.TrimPrefix(data, []byte("\uFEFF"))
	if len(data) == 0 {
		return false
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return false
	}
	return jsonValueHasSniffedSpecSource(v)
}

func jsonValueHasSniffedSpecSource(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if specSourceValueIsSniffed(k, child) {
				return true
			}
			if jsonValueHasSniffedSpecSource(child) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(t, jsonValueHasSniffedSpecSource)
	}
	return false
}

func specSourceValueIsSniffed(key string, value any) bool {
	if normalizeSpecSourceKey(key) != "specsource" {
		return false
	}
	s, ok := value.(string)
	return ok && strings.EqualFold(strings.TrimSpace(s), "sniffed")
}

func normalizeSpecSourceKey(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if r == '_' || r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func rootSpecFileIsSniffed(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return false
	}
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		if v, ok := jsonTopLevelSpecSource(data); ok && strings.EqualFold(strings.TrimSpace(v), "sniffed") {
			return true
		}
	}
	return rootYAMLSpecSourceIsSniffed(data)
}

func jsonTopLevelSpecSource(data []byte) (string, bool) {
	data = bytes.TrimSpace(data)
	data = bytes.TrimPrefix(data, []byte("\uFEFF"))
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return "", false
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return "", false
		}
		key, ok := keyTok.(string)
		if !ok {
			return "", false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return "", false
		}
		if normalizeSpecSourceKey(key) != "specsource" {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		return s, true
	}
	return "", false
}

func rootYAMLSpecSourceIsSniffed(data []byte) bool {
	rest := data
	for len(rest) > 0 {
		line := rest
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			line = rest[:i]
			rest = rest[i+1:]
		} else {
			rest = nil
		}
		if specSourceLineIsSniffed(string(line)) {
			return true
		}
	}
	return false
}

func specSourceLineIsSniffed(line string) bool {
	line = strings.TrimPrefix(strings.TrimRight(line, "\r"), "\uFEFF")
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
		return false
	}
	key, value, ok := splitYAMLKV(line)
	if !ok || normalizeSpecSourceKey(key) != "specsource" {
		return false
	}
	scalar, ok := rawYAMLScalar(stripYAMLComment(value))
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(scalar), "sniffed")
}

type jsCursor struct {
	s    string
	i    int
	line int
	col  int
	hits *[]accountHit
}

func scanJSONAccountIdentifiers(content string) []accountHit {
	var hits []accountHit
	c := &jsCursor{s: content, line: 1, col: 1, hits: &hits}
	c.skipWS()
	start := c.i
	switch c.peek() {
	case '{':
		c.parseObject(nil)
	case '[':
		c.parseArray(nil)
	default:
		c.scanEmbedded()
		return hits
	}
	c.skipWS()
	if c.i > start && c.i >= len(c.s) {
		return hits
	}
	c.scanEmbedded()
	return hits
}

func (c *jsCursor) scanEmbedded() {
	for c.i < len(c.s) {
		c.skipWS()
		if c.i >= len(c.s) {
			return
		}
		switch c.peek() {
		case '"':
			if _, ok := c.readJSONString(); !ok {
				c.next()
			}
		case '{':
			before := c.i
			if !c.parseObject(nil) && c.i == before {
				c.next()
			}
		default:
			c.next()
		}
	}
}

func (c *jsCursor) peek() byte {
	if c.i >= len(c.s) {
		return 0
	}
	return c.s[c.i]
}

func (c *jsCursor) next() {
	if c.i >= len(c.s) {
		return
	}
	if c.s[c.i] == '\n' {
		c.line++
		c.col = 1
	} else {
		c.col++
	}
	c.i++
}

func (c *jsCursor) skipWS() {
	for c.i < len(c.s) {
		switch c.s[c.i] {
		case ' ', '\t', '\n', '\r':
			c.next()
		default:
			return
		}
	}
}

func (c *jsCursor) parseObject(path []string) bool {
	if c.peek() != '{' {
		return false
	}
	c.next()
	for {
		c.skipWS()
		if c.i >= len(c.s) {
			return false
		}
		if c.peek() == '}' {
			c.next()
			return true
		}
		key, ok := c.parseKey()
		if !ok {
			return false
		}
		c.skipWS()
		if c.peek() != ':' {
			return false
		}
		c.next()
		c.skipWS()
		next := append(append([]string{}, path...), key)
		if !c.parseValue(next) {
			return false
		}
		c.skipWS()
		if c.peek() == ',' {
			c.next()
			continue
		}
		if c.peek() == '}' {
			c.next()
			return true
		}
		return false
	}
}

func (c *jsCursor) parseArray(path []string) bool {
	if c.peek() != '[' {
		return false
	}
	c.next()
	for {
		c.skipWS()
		if c.i >= len(c.s) {
			return false
		}
		if c.peek() == ']' {
			c.next()
			return true
		}
		if !c.parseValue(path) {
			return false
		}
		c.skipWS()
		if c.peek() == ',' {
			c.next()
			continue
		}
		if c.peek() == ']' {
			c.next()
			return true
		}
		return false
	}
}

func (c *jsCursor) parseValue(path []string) bool {
	if c.i >= len(c.s) {
		return false
	}
	switch c.peek() {
	case '"':
		line, col := c.line, c.col
		s, ok := c.readJSONString()
		if !ok {
			return false
		}
		c.consider(path, s, line, col+1)
		return true
	case '\'':
		line, col := c.line, c.col
		s, ok := c.readSingleQuoted()
		if !ok {
			return false
		}
		c.consider(path, s, line, col+1)
		return true
	case '{':
		return c.parseObject(path)
	case '[':
		return c.parseArray(path)
	case 't':
		if c.consumeLiteral("true") {
			return true
		}
		return c.takeBareScalar(path)
	case 'f':
		if c.consumeLiteral("false") {
			return true
		}
		return c.takeBareScalar(path)
	case 'n':
		if c.consumeLiteral("null") {
			return true
		}
		return c.takeBareScalar(path)
	default:
		if c.peek() == '-' || isDigitByte(c.peek()) {
			line, col := c.line, c.col
			s, ok := c.readNumber()
			if ok {
				c.consider(path, s, line, col)
			}
			return ok
		}
		if isLetterByte(c.peek()) {
			return c.takeBareScalar(path)
		}
		return false
	}
}

func (c *jsCursor) parseKey() (string, bool) {
	switch c.peek() {
	case '"':
		return c.readJSONString()
	case '\'':
		return c.readSingleQuoted()
	default:
		return c.readBareKey()
	}
}

func (c *jsCursor) readJSONString() (string, bool) {
	if c.peek() != '"' {
		return "", false
	}
	start := c.i
	c.next()
	for c.i < len(c.s) {
		ch := c.s[c.i]
		if ch == '\\' {
			c.next()
			if c.i < len(c.s) {
				c.next()
			}
			continue
		}
		if ch == '"' {
			raw := c.s[start : c.i+1]
			c.next()
			unq, ok := decodeQuotedScalar(raw)
			if !ok {
				return "", false
			}
			return unq, true
		}
		if ch == '\n' {
			return "", false
		}
		c.next()
	}
	return "", false
}

func (c *jsCursor) readSingleQuoted() (string, bool) {
	if c.peek() != '\'' {
		return "", false
	}
	c.next()
	var b strings.Builder
	for c.i < len(c.s) {
		ch := c.s[c.i]
		if ch == '\'' {
			if c.i+1 < len(c.s) && c.s[c.i+1] == '\'' {
				b.WriteByte('\'')
				c.next()
				c.next()
				continue
			}
			c.next()
			return b.String(), true
		}
		if ch == '\n' {
			return "", false
		}
		b.WriteByte(ch)
		c.next()
	}
	return "", false
}

func (c *jsCursor) takeBareScalar(path []string) bool {
	line, col := c.line, c.col
	s, ok := c.readBareScalar()
	if !ok {
		return false
	}
	// "AB12CD34 is the sample" is prose. A prefix before the space is not
	// the value; only a token that ends the flow entry counts.
	c.skipWS()
	if c.i < len(c.s) {
		switch c.peek() {
		case ',', '}', ']', '#':
		default:
			return true
		}
	}
	c.consider(path, s, line, col)
	return true
}

func (c *jsCursor) readBareScalar() (string, bool) {
	if !isLetterByte(c.peek()) {
		return "", false
	}
	start := c.i
	for c.i < len(c.s) && isBareScalarCont(c.s[c.i]) {
		c.next()
	}
	if c.i == start {
		return "", false
	}
	return c.s[start:c.i], true
}

func (c *jsCursor) readBareKey() (string, bool) {
	if !isBareKeyStart(c.peek()) {
		return "", false
	}
	start := c.i
	for c.i < len(c.s) && isBareKeyCont(c.s[c.i]) {
		c.next()
	}
	if c.i == start {
		return "", false
	}
	return c.s[start:c.i], true
}

func (c *jsCursor) readNumber() (string, bool) {
	start := c.i
	if c.peek() == '-' {
		c.next()
	}
	if !isDigitByte(c.peek()) {
		return "", false
	}
	for isDigitByte(c.peek()) {
		c.next()
	}
	if c.peek() == '.' || c.peek() == 'e' || c.peek() == 'E' {
		if c.peek() == '.' {
			c.next()
			for isDigitByte(c.peek()) {
				c.next()
			}
		}
		if c.peek() == 'e' || c.peek() == 'E' {
			c.next()
			if c.peek() == '+' || c.peek() == '-' {
				c.next()
			}
			for isDigitByte(c.peek()) {
				c.next()
			}
		}
		return "", true
	}
	if c.s[start] == '-' {
		return "", true
	}
	return c.s[start:c.i], true
}

func (c *jsCursor) consumeLiteral(lit string) bool {
	if !strings.HasPrefix(c.s[c.i:], lit) {
		return false
	}
	end := c.i + len(lit)
	if end < len(c.s) && isBareKeyCont(c.s[end]) {
		return false
	}
	for range len(lit) {
		c.next()
	}
	return true
}

func (c *jsCursor) consider(path []string, value string, line, col int) {
	if line < 1 {
		line = 1
	}
	if col < 1 {
		col = 1
	}
	if !isAccountIdentifierKeyPath(path) || !isAccountIdentifierValue(value) {
		return
	}
	*c.hits = append(*c.hits, accountHit{line: line, col: col, span: value})
}

func isDigitByte(b byte) bool {
	return b >= '0' && b <= '9'
}

func isLetterByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isBareScalarCont(b byte) bool {
	return isLetterByte(b) || isDigitByte(b) || b == '-' || b == '_'
}

func isBareKeyStart(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '_'
}

func isBareKeyCont(b byte) bool {
	return isBareKeyStart(b) || isDigitByte(b) || b == '_' || b == '-' || b == '.'
}

type yamlFrame struct {
	indent int
	key    string
}

func scanYAMLAccountIdentifiers(content string) []accountHit {
	var hits []accountHit
	lines := strings.Split(content, "\n")
	var stack []yamlFrame
	for i, line := range lines {
		raw := strings.TrimRight(line, "\r")
		if strings.TrimSpace(raw) == "" {
			continue
		}
		body := strings.TrimSpace(raw)
		if body == "}" || body == "}," || body == "]" || body == "]," {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		indent := indentOf(raw)
		// Only a column-0 marker starts a new document. An indented
		// "---" inside a block scalar is content, not a boundary.
		if indent == 0 && yamlDocMarker(body) {
			stack = nil
			continue
		}
		if comment, ok := strings.CutPrefix(body, "#"); ok {
			recordYAMLLine(strings.TrimSpace(comment), raw, frameKeys(stack), i+1, &hits)
			continue
		}
		if item, ok := strings.CutPrefix(body, "- "); ok {
			body = strings.TrimSpace(item)
			indent += 2
		}
		key, value, ok := splitYAMLKV(body)
		if !ok {
			if len(stack) > 0 && indent > stack[len(stack)-1].indent {
				path := frameKeys(stack)
				if scalar, ok := rawYAMLScalar(body); ok {
					considerAccountHit(&hits, path, scalar, i+1, valueColumn(raw, scalar))
				}
				stack = stack[:len(stack)-1]
			}
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		path := append(frameKeys(stack), key)
		recordYAMLValue(&hits, path, value, raw, i+1)
		if yamlValueOpensNest(value) {
			stack = append(stack, yamlFrame{indent: indent, key: key})
		}
	}
	return hits
}

func recordYAMLLine(body, raw string, parent []string, line int, hits *[]accountHit) {
	key, value, ok := splitYAMLKV(body)
	if !ok {
		return
	}
	path := append(append([]string{}, parent...), key)
	recordYAMLValue(hits, path, value, raw, line)
}

func recordYAMLValue(hits *[]accountHit, path []string, value, raw string, line int) {
	if flow := flowFragment(value); flow != "" {
		scanFlowAccountIdentifiers(flow, line, valueColumn(raw, flow), path, hits)
	}
	scalar, ok := rawYAMLScalar(stripYAMLComment(value))
	if !ok {
		return
	}
	considerAccountHit(hits, path, scalar, line, valueColumn(raw, scalar))
}

func scanFlowAccountIdentifiers(fragment string, line, col int, path []string, hits *[]accountHit) {
	c := &jsCursor{s: fragment, line: line, col: col, hits: hits}
	c.skipWS()
	switch c.peek() {
	case '{':
		c.parseObject(path)
	case '[':
		c.parseArray(path)
	}
}

func considerAccountHit(hits *[]accountHit, path []string, value string, line, col int) {
	if !isAccountIdentifierKeyPath(path) || !isAccountIdentifierValue(value) {
		return
	}
	if col < 1 {
		col = 1
	}
	*hits = append(*hits, accountHit{line: line, col: col, span: value})
}

func frameKeys(stack []yamlFrame) []string {
	keys := make([]string, len(stack))
	for i, f := range stack {
		keys[i] = f.key
	}
	return keys
}

func indentOf(line string) int {
	n := 0
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			break
		}
		n++
	}
	return n
}

func splitYAMLKV(body string) (string, string, bool) {
	body = strings.TrimSpace(body)
	key, value, ok := strings.Cut(body, ":")
	if !ok || strings.TrimSpace(key) == "" {
		return "", "", false
	}
	key = unquoteYAMLKey(strings.TrimSpace(key))
	if key == "" || strings.ContainsAny(key, "\n") {
		return "", "", false
	}
	return key, strings.TrimSpace(value), true
}

func unquoteYAMLKey(key string) string {
	if len(key) >= 2 {
		if key[0] == '"' && key[len(key)-1] == '"' {
			if unq, err := strconv.Unquote(key); err == nil {
				return unq
			}
			return key[1 : len(key)-1]
		}
		if key[0] == '\'' && key[len(key)-1] == '\'' {
			return strings.ReplaceAll(key[1:len(key)-1], "''", "'")
		}
	}
	return key
}

func stripYAMLComment(value string) string {
	if before, _, ok := strings.Cut(value, " #"); ok {
		return strings.TrimSpace(before)
	}
	return value
}

func rawYAMLScalar(value string) (string, bool) {
	v := strings.TrimSpace(value)
	if v == "" || v == "~" || v == "|" || v == ">" || strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">") {
		return "", false
	}
	if strings.HasPrefix(v, "{") || strings.HasPrefix(v, "[") {
		return "", false
	}
	v = strings.TrimRight(v, ",")
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		unq, err := strconv.Unquote(v)
		if err != nil {
			return v[1 : len(v)-1], true
		}
		return unq, true
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'"), true
	}
	lower := strings.ToLower(v)
	switch lower {
	case "null", "true", "false", "~":
		return "", false
	}
	return v, true
}

func yamlValueOpensNest(value string) bool {
	v := strings.TrimSpace(value)
	switch v {
	case "", "|", ">", "|-", "|+", ">-", ">+":
		return true
	}
	if v != "" && (v[0] == '{' || v[0] == '[') && flowFragment(v) == "" {
		return true
	}
	return false
}

func decodeQuotedScalar(raw string) (string, bool) {
	var unq string
	// JSON accepts \/; strconv.Unquote does not. YAML flow strings also
	// use Go-style \xHH, which encoding/json rejects.
	if err := json.Unmarshal([]byte(raw), &unq); err == nil {
		return unq, true
	}
	unq, err := strconv.Unquote(raw)
	if err != nil {
		return "", false
	}
	return unq, true
}

func yamlDocMarker(body string) bool {
	for _, marker := range []string{"---", "..."} {
		rest, ok := strings.CutPrefix(body, marker)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "" || strings.HasPrefix(rest, "#") {
			return true
		}
	}
	return false
}

func flowFragment(value string) string {
	v := strings.TrimSpace(value)
	if v == "" || (v[0] != '{' && v[0] != '[') {
		return ""
	}
	var stack []byte
	var inStr byte
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if inStr != 0 {
			if ch == '\\' && inStr == '"' && i+1 < len(v) {
				i++
				continue
			}
			if ch == inStr {
				// YAML single quotes escape as ''.
				if inStr == '\'' && i+1 < len(v) && v[i+1] == '\'' {
					i++
					continue
				}
				inStr = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inStr = ch
		case '{', '[':
			stack = append(stack, ch)
		case '}', ']':
			if len(stack) == 0 {
				return ""
			}
			open := stack[len(stack)-1]
			if (ch == '}' && open != '{') || (ch == ']' && open != '[') {
				return ""
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return v[:i+1]
			}
		}
	}
	return ""
}

func valueColumn(line, scalar string) int {
	if scalar == "" {
		return 1
	}
	colon := strings.Index(line, ":")
	if colon < 0 {
		if rel := strings.Index(line, scalar); rel >= 0 {
			return rel + 1
		}
		return 1
	}
	rel := strings.Index(line[colon+1:], scalar)
	if rel < 0 {
		return colon + 2
	}
	return colon + 1 + rel + 1
}

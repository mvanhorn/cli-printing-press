package client

import (
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"output-safety-pp-cli/internal/config"
)

func TestCredentialEchoMasksEncodedForms(t *testing.T) {
	const secret = "p+q/r=stuvWX12"
	c := newEchoClient(secret)
	for _, sample := range encodedEchoes(secret) {
		got := c.maskCredentialText("seen "+sample.text+" end", secret)
		if credentialRecoverable(got, secret) {
			t.Fatalf("%s still recoverable: %q", sample.name, got)
		}
		if sample.strict && got != "seen **** end" {
			t.Fatalf("%s = %q, want seen **** end", sample.name, got)
		}
	}
}

func TestCredentialEchoAPIErrorMasksEncodedBody(t *testing.T) {
	const secret = "p+q/r=stuvWX12"
	c := newEchoClient(secret)
	body := base64.StdEncoding.EncodeToString([]byte(secret))
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("echo " + body)),
			Request:    req,
		}, nil
	})}
	_, err := c.Get(context.Background(), "/items", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if credentialRecoverable(err.Error(), secret) {
		t.Fatalf("API error recoverable: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "returned HTTP 403") {
		t.Fatalf("status line dropped: %q", err.Error())
	}
}

func TestCredentialEchoMasksLabeledBase64(t *testing.T) {
	const secret = "Wk8mQ2pL9vBn3cDe"
	c := newEchoClient(secret)
	body := "echo=" + base64.StdEncoding.EncodeToString([]byte("credential:"+secret))
	got := c.maskCredentialText(body, secret)
	if credentialRecoverable(got, secret) {
		t.Fatalf("labeled base64 still recoverable: %q", got)
	}
	if got != "****" {
		t.Fatalf("labeled base64 = %q, want ****", got)
	}
	padded := "echo=" + base64.StdEncoding.EncodeToString([]byte("abcdef:"+secret))
	got = c.maskCredentialText("seen "+padded+" end", secret)
	if credentialRecoverable(got, secret) || got != "seen **** end" {
		t.Fatalf("padded labeled base64 = %q", got)
	}
	other := "echo=" + base64.StdEncoding.EncodeToString([]byte("credential:hello-world"))
	if got = c.maskCredentialText(other, secret); got != other {
		t.Fatalf("unrelated labeled base64 changed: %q", got)
	}
}

func TestCredentialEchoNegativeCases(t *testing.T) {
	const secret = "Wk8mQ2pL9vBn3cDe"
	c := newEchoClient(secret)
	if got := c.maskCredentialText("forbidden", secret); got != "forbidden" {
		t.Fatalf("unrelated body changed: %q", got)
	}
	if got := c.maskCredentialText("code Wk8mQ2pL is only a prefix", secret); got != "code Wk8mQ2pL is only a prefix" {
		t.Fatalf("prefix over-masked: %q", got)
	}
	if got := c.maskCredentialText("other wk8mq2pl9vbn3cde case", secret); credentialRecoverable(got, secret) {
		t.Fatalf("case variant recoverable: %q", got)
	}
	if got := c.maskCredentialText("", secret); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := c.maskCredentialText("code 0123456789abcdef is fine", secret); got != "code 0123456789abcdef is fine" {
		t.Fatalf("unrelated hex changed: %q", got)
	}
	other := base64.StdEncoding.EncodeToString([]byte("hello-world"))
	if got := c.maskCredentialText("blob "+other, secret); got != "blob "+other {
		t.Fatalf("unrelated base64 changed: %q", got)
	}
}

type echoSample struct {
	name   string
	text   string
	strict bool
}

func encodedEchoes(secret string) []echoSample {
	q := url.QueryEscape(secret)
	p := url.PathEscape(secret)
	b64 := base64.StdEncoding.EncodeToString([]byte(secret))
	return []echoSample{
		{name: "literal", text: secret, strict: true},
		{name: "json-slash", text: strings.ReplaceAll(secret, "/", `\/`), strict: true},
		{name: "query", text: q, strict: true},
		{name: "path", text: p, strict: true},
		{name: "double-query", text: url.QueryEscape(q), strict: true},
		{name: "lower-percent", text: lowerPercentHex(q), strict: true},
		{name: "html-partial", text: strings.NewReplacer("+", "&#43;", "/", "&#47;", "=", "&#61;").Replace(secret), strict: true},
		{name: "html-hex", text: strings.NewReplacer("+", "&#x2B;", "/", "&#x2F;", "=", "&#x3D;").Replace(secret), strict: true},
		{name: "html-padded", text: strings.NewReplacer("+", "&#0043;", "/", "&#0047;", "=", "&#0061;").Replace(secret), strict: true},
		{name: "html-full", text: htmlAllDecimal(secret), strict: true},
		{name: "html-double", text: strings.ReplaceAll(htmlAllDecimal(secret), "&", "&amp;"), strict: true},
		{name: "base64", text: b64, strict: true},
		{name: "base64-raw", text: base64.RawStdEncoding.EncodeToString([]byte(secret)), strict: true},
		{name: "base64url", text: base64.URLEncoding.EncodeToString([]byte(secret)), strict: true},
		{name: "base64url-raw", text: base64.RawURLEncoding.EncodeToString([]byte(secret)), strict: true},
		{name: "base64-query", text: base64.StdEncoding.EncodeToString([]byte(q)), strict: true},
		{name: "hex-lower", text: hex.EncodeToString([]byte(secret)), strict: true},
		{name: "hex-upper", text: strings.ToUpper(hex.EncodeToString([]byte(secret))), strict: true},
		{name: "hex-mixed", text: mixedHex(hex.EncodeToString([]byte(secret))), strict: true},
		{name: "utf16-hex", text: hex.EncodeToString(utf16BE(secret)), strict: true},
		{name: "json-u", text: jsonUEscape(secret, false), strict: true},
		{name: "json-u-upper", text: jsonUEscape(secret, true), strict: true},
		{name: "rot13", text: rot13(secret), strict: true},
		{name: "fullwidth", text: fullwidthASCII(secret), strict: true},
		{name: "percent-u", text: percentU(secret), strict: true},
		{name: "quoted-printable", text: quotedPrintable(secret), strict: true},
		{name: "base32", text: base32.StdEncoding.EncodeToString([]byte(secret)), strict: true},
		{name: "utf7-plus", text: strings.ReplaceAll(secret, "+", "+-"), strict: true},
		{name: "casefold", text: strings.ToUpper(secret), strict: true},
		{name: "mime-base64", text: "\n" + mimeWrap(b64) + "\n", strict: false},
		{name: "zero-width", text: insertEvery(secret, "\u200b"), strict: false},
		{name: "spaces", text: insertEvery(secret, " "), strict: false},
		{name: "mixed-html-percent", text: strings.ReplaceAll(q, "%2B", "&#43;"), strict: false},
		{name: "base64-utf16", text: base64.StdEncoding.EncodeToString(utf16BE(secret)), strict: true},
		{name: "base64-utf16-le", text: base64.StdEncoding.EncodeToString(utf16LE(secret)), strict: true},
		{name: "slash-x", text: slashX(secret, false), strict: true},
		{name: "slash-x-upper", text: slashX(secret, true), strict: true},
		{name: "percent-all", text: percentAll(secret), strict: true},
		{name: "html-hex-all", text: htmlHexAll(secret, false), strict: true},
		{name: "html-hex-all-X", text: strings.ReplaceAll(htmlHexAll(secret, true), "&#x", "&#X"), strict: true},
		{name: "base32-hex", text: base32.HexEncoding.EncodeToString([]byte(secret)), strict: true},
		{name: "double-base64", text: base64.StdEncoding.EncodeToString([]byte(b64)), strict: true},
		{name: "html-X-partial", text: strings.NewReplacer("+", "&#X2B;", "/", "&#X2F;", "=", "&#X3D;").Replace(secret), strict: false},
		{name: "html-hex-padded", text: strings.NewReplacer("+", "&#x002B;", "/", "&#x002F;", "=", "&#x003D;").Replace(secret), strict: false},
		{name: "percent-base64", text: percentAll(b64), strict: false},
		{name: "fullwidth-hex", text: fullwidthASCII(hex.EncodeToString([]byte(secret))), strict: false},
		{name: "mixed-percent-case", text: mixedPercentCase(q), strict: false},
		{name: "zwnj", text: insertEvery(secret, "\u200c"), strict: false},
		{name: "qp-soft-break", text: secret[:8] + "=\r\n" + secret[8:], strict: false},
		{name: "spaced-base64", text: insertEvery(b64, " "), strict: false},
		{name: "rot13-base64", text: rot13(b64), strict: false},
		{name: "overlong-utf8", text: overlongASCII(secret), strict: false},
		{name: "lower-query-case", text: strings.ToLower(q), strict: false},
		{name: "b64-of-percent", text: base64.StdEncoding.EncodeToString([]byte(percentAll(secret))), strict: false},
		{name: "mime-crlf", text: "\r\n" + strings.ReplaceAll(mimeWrap(b64), "\n", "\r\n") + "\r\n", strict: false},
		{name: "json-u-zwsp", text: jsonUZWSP(secret), strict: false},
	}
}

func newEchoClient(secret string) *Client {
	c := New(&config.Config{BaseURL: "https://api.example.invalid", AuthHeaderVal: secret}, time.Second, 0)
	c.NoCache = true
	return c
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func credentialRecoverable(text, secret string) bool {
	if secret == "" {
		return false
	}
	seen := map[string]struct{}{}
	var walk func(string, int) bool
	walk = func(s string, depth int) bool {
		if strings.Contains(strings.ToLower(s), strings.ToLower(secret)) {
			return true
		}
		if depth >= 5 || len(seen) > 96 || len(s) > 65536 {
			return false
		}
		if _, ok := seen[s]; ok {
			return false
		}
		seen[s] = struct{}{}
		for _, next := range recoverVariants(s) {
			if walk(next, depth+1) {
				return true
			}
		}
		return false
	}
	return walk(text, 0)
}

func recoverVariants(s string) []string {
	var out []string
	add := func(v string) {
		if v != "" && v != s {
			out = append(out, v)
		}
	}
	add(html.UnescapeString(s))
	if v, ok := percentDecode(s, true); ok {
		add(v)
	}
	if v, ok := percentDecode(s, false); ok {
		add(v)
	}
	if v, ok := jsonUnescape(s); ok {
		add(v)
	}
	if v, ok := qpDecode(s); ok {
		add(v)
	}
	if v, ok := foldFullwidth(s); ok {
		add(v)
	}
	if v, ok := stripMarks(s); ok {
		add(v)
	}
	if v, ok := stripSpaces(s); ok {
		add(v)
	}
	if v := rot13(s); v != s {
		add(v)
	}
	if v, ok := decodePercentU(s); ok {
		add(v)
	}
	if v, ok := decodeSlashX(s); ok {
		add(v)
	}
	if v, ok := decodeOverlong(s); ok {
		add(v)
	}
	for _, v := range utf16Views(s) {
		add(v)
	}
	for _, tok := range decodedTokens(s) {
		add(tok)
	}
	return out
}

func decodedTokens(s string) []string {
	var out []string
	addBytes := func(b []byte) {
		if len(b) > 0 {
			out = append(out, string(b))
		}
	}
	decodeB64 := func(tok string) {
		if len(tok) < 8 {
			return
		}
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if b, err := enc.DecodeString(tok); err == nil {
				addBytes(b)
			}
		}
		upper := strings.ToUpper(strings.TrimRight(tok, "="))
		if b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(upper); err == nil {
			addBytes(b)
		}
	}
	for _, tok := range scanRuns(s, isB64Byte, false) {
		decodeB64(tok)
	}
	for _, joined := range joinPureLines(s, isB64Byte) {
		decodeB64(joined)
	}
	for _, tok := range scanRuns(s, isHexByte, false) {
		if len(tok) < 16 || len(tok)%2 != 0 {
			continue
		}
		if b, err := hex.DecodeString(tok); err == nil {
			addBytes(b)
			if len(b)%2 == 0 {
				addBytes(utf16ToUTF8(b, true))
				addBytes(utf16ToUTF8(b, false))
			}
		}
	}
	return out
}

func joinPureLines(s string, ok func(byte) bool) []string {
	var out []string
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		joined := strings.Join(cur, "")
		if len(joined) >= 8 {
			out = append(out, joined)
		}
		cur = nil
	}
	for _, line := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(line)
		if trim != "" && pureRun(trim, ok) {
			cur = append(cur, trim)
			continue
		}
		flush()
	}
	flush()
	return out
}

func pureRun(s string, ok func(byte) bool) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !ok(s[i]) {
			return false
		}
	}
	return true
}

func scanRuns(s string, ok func(byte) bool, allowWS bool) []string {
	var runs []string
	start := -1
	for i := 0; i <= len(s); i++ {
		good := i < len(s) && (ok(s[i]) || (allowWS && isWS(s[i])))
		if good {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			runs = append(runs, s[start:i])
			start = -1
		}
	}
	return runs
}

func isB64Byte(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' || c == '-' || c == '_'
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isWS(c byte) bool { return c == ' ' || c == '\n' || c == '\r' || c == '\t' }

func percentDecode(s string, plusIsSpace bool) (string, bool) {
	var b strings.Builder
	changed := false
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHexByte(s[i+1]) && isHexByte(s[i+2]) {
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
			changed = true
			continue
		}
		if plusIsSpace && s[i] == '+' {
			b.WriteByte(' ')
			changed = true
			continue
		}
		b.WriteByte(s[i])
	}
	if !changed {
		return s, false
	}
	return b.String(), true
}

func jsonUnescape(s string) (string, bool) {
	if !strings.Contains(s, `\`) {
		return s, false
	}
	var b strings.Builder
	changed := false
	for i := 0; i < len(s); {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}
		switch s[i+1] {
		case 'u':
			if i+6 <= len(s) && hex4(s[i+2:i+6]) {
				r := hexRune(s[i+2 : i+6])
				if utf16.IsSurrogate(r) && i+12 <= len(s) && s[i+6] == '\\' && s[i+7] == 'u' && hex4(s[i+8:i+12]) {
					lo := hexRune(s[i+8 : i+12])
					if dec := utf16.DecodeRune(r, lo); dec != utf8.RuneError {
						b.WriteRune(dec)
						i += 12
						changed = true
						continue
					}
				}
				b.WriteRune(r)
				i += 6
				changed = true
				continue
			}
		case '\\', '/', '"':
			b.WriteByte(s[i+1])
			i += 2
			changed = true
			continue
		case 'n':
			b.WriteByte('\n')
			i += 2
			changed = true
			continue
		case 'r':
			b.WriteByte('\r')
			i += 2
			changed = true
			continue
		case 't':
			b.WriteByte('\t')
			i += 2
			changed = true
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	if !changed {
		return s, false
	}
	return b.String(), true
}

func hex4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		if !isHexByte(s[i]) {
			return false
		}
	}
	return true
}

func hexRune(s string) rune {
	return rune(unhex(s[0]))<<12 | rune(unhex(s[1]))<<8 | rune(unhex(s[2]))<<4 | rune(unhex(s[3]))
}

func qpDecode(s string) (string, bool) {
	if !strings.Contains(s, "=") {
		return s, false
	}
	var b strings.Builder
	changed := false
	for i := 0; i < len(s); i++ {
		if s[i] == '=' && i+1 < len(s) && (s[i+1] == '\n' || (s[i+1] == '\r' && i+2 < len(s) && s[i+2] == '\n')) {
			if s[i+1] == '\r' {
				i += 2
			} else {
				i++
			}
			changed = true
			continue
		}
		if s[i] == '=' && i+2 < len(s) && isHexByte(s[i+1]) && isHexByte(s[i+2]) {
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
			changed = true
			continue
		}
		b.WriteByte(s[i])
	}
	if !changed {
		return s, false
	}
	return b.String(), true
}

func foldFullwidth(s string) (string, bool) {
	var b strings.Builder
	changed := false
	for _, r := range s {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
			changed = true
		case r == 0x3000:
			b.WriteByte(' ')
			changed = true
		default:
			b.WriteRune(r)
		}
	}
	if !changed {
		return s, false
	}
	return b.String(), true
}

func stripMarks(s string) (string, bool) {
	var b strings.Builder
	changed := false
	for _, r := range s {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff', '\u2060', '\u180e':
			changed = true
		default:
			b.WriteRune(r)
		}
	}
	if !changed {
		return s, false
	}
	return b.String(), true
}

func stripSpaces(s string) (string, bool) {
	var b strings.Builder
	changed := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\u00a0' || (r >= 0x2000 && r <= 0x200a) || r == 0x202f || r == 0x205f || r == 0x3000 {
			changed = true
			continue
		}
		b.WriteRune(r)
	}
	if !changed {
		return s, false
	}
	return b.String(), true
}

func decodePercentU(s string) (string, bool) {
	if !strings.Contains(strings.ToLower(s), "%u") {
		return s, false
	}
	var b strings.Builder
	changed := false
	for i := 0; i < len(s); {
		if i+6 <= len(s) && s[i] == '%' && (s[i+1] == 'u' || s[i+1] == 'U') && hex4(s[i+2:i+6]) {
			b.WriteRune(hexRune(s[i+2 : i+6]))
			i += 6
			changed = true
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	if !changed {
		return s, false
	}
	return b.String(), true
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

func lowerPercentHex(s string) string {
	b := []byte(s)
	for i := 0; i+2 < len(b); i++ {
		if b[i] == '%' && isHexByte(b[i+1]) && isHexByte(b[i+2]) {
			if b[i+1] >= 'A' && b[i+1] <= 'F' {
				b[i+1] += 'a' - 'A'
			}
			if b[i+2] >= 'A' && b[i+2] <= 'F' {
				b[i+2] += 'a' - 'A'
			}
			i += 2
		}
	}
	return string(b)
}

func htmlAllDecimal(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteString("&#")
		b.WriteString(itoa(int(r)))
		b.WriteByte(';')
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d [20]byte
	i := len(d)
	for n > 0 {
		i--
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d[i:])
}

func jsonUEscape(s string, upper bool) string {
	digits := "0123456789abcdef"
	if upper {
		digits = "0123456789ABCDEF"
	}
	var b strings.Builder
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			writeU(&b, digits, rune(0xD800+(r>>10)))
			writeU(&b, digits, rune(0xDC00+(r&0x3FF)))
			continue
		}
		writeU(&b, digits, r)
	}
	return b.String()
}

func writeU(b *strings.Builder, digits string, r rune) {
	b.WriteString(`\u`)
	b.WriteByte(digits[(r>>12)&0xF])
	b.WriteByte(digits[(r>>8)&0xF])
	b.WriteByte(digits[(r>>4)&0xF])
	b.WriteByte(digits[r&0xF])
}

func rot13(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune('a' + (r-'a'+13)%26)
		case r >= 'A' && r <= 'Z':
			b.WriteRune('A' + (r-'A'+13)%26)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fullwidthASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x21 && r <= 0x7E {
			b.WriteRune(r + 0xFEE0)
			continue
		}
		if r == ' ' {
			b.WriteRune(0x3000)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func percentU(s string) string {
	digits := "0123456789ABCDEF"
	var b strings.Builder
	for _, r := range s {
		b.WriteString("%u")
		b.WriteByte(digits[(r>>12)&0xF])
		b.WriteByte(digits[(r>>8)&0xF])
		b.WriteByte(digits[(r>>4)&0xF])
		b.WriteByte(digits[r&0xF])
	}
	return b.String()
}

func quotedPrintable(s string) string {
	digits := "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 33 && c <= 126 && c != '=' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('=')
		b.WriteByte(digits[c>>4])
		b.WriteByte(digits[c&0xF])
	}
	return b.String()
}

func utf16BE(s string) []byte { return utf16Bytes(s, true) }

func utf16LE(s string) []byte { return utf16Bytes(s, false) }

func utf16Bytes(s string, be bool) []byte {
	var b []byte
	for _, r := range s {
		var units []uint16
		if r > 0xFFFF {
			r -= 0x10000
			units = []uint16{uint16(0xD800 + (r >> 10)), uint16(0xDC00 + (r & 0x3FF))}
		} else {
			units = []uint16{uint16(r)}
		}
		for _, u := range units {
			if be {
				b = append(b, byte(u>>8), byte(u))
			} else {
				b = append(b, byte(u), byte(u>>8))
			}
		}
	}
	return b
}

func slashX(s string, upper bool) string {
	digits := "0123456789abcdef"
	if upper {
		digits = "0123456789ABCDEF"
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteString(`\x`)
		b.WriteByte(digits[s[i]>>4])
		b.WriteByte(digits[s[i]&0x0F])
	}
	return b.String()
}

func percentAll(s string) string {
	digits := "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteByte('%')
		b.WriteByte(digits[s[i]>>4])
		b.WriteByte(digits[s[i]&0x0F])
	}
	return b.String()
}

func htmlHexAll(s string, upper bool) string {
	digits := "0123456789abcdef"
	if upper {
		digits = "0123456789ABCDEF"
	}
	var b strings.Builder
	for _, r := range s {
		b.WriteString("&#x")
		b.WriteByte(digits[(r>>4)&0xF])
		b.WriteByte(digits[r&0xF])
		b.WriteByte(';')
	}
	return b.String()
}

func mixedPercentCase(s string) string {
	b := []byte(s)
	upper := true
	for i := 0; i+2 < len(b); i++ {
		if b[i] != '%' || !isHexByte(b[i+1]) || !isHexByte(b[i+2]) {
			continue
		}
		for _, j := range []int{i + 1, i + 2} {
			if b[j] >= 'A' && b[j] <= 'F' && !upper {
				b[j] += 'a' - 'A'
			}
			if b[j] >= 'a' && b[j] <= 'f' && upper {
				b[j] -= 'a' - 'A'
			}
		}
		upper = !upper
		i += 2
	}
	return string(b)
}

func overlongASCII(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		b = append(b, 0xC0|(c>>6), 0x80|(c&0x3F))
	}
	return string(b)
}

func jsonUZWSP(s string) string {
	parts := make([]string, 0, len(s)*2)
	for _, r := range s {
		parts = append(parts, jsonUEscape(string(r), false), `\u200b`)
	}
	return strings.Join(parts, "")
}

func decodeOverlong(s string) (string, bool) {
	b := []byte(s)
	var out strings.Builder
	changed := false
	for i := 0; i < len(b); {
		r, size, over := overlongRune(b[i:])
		if size < 1 {
			size = 1
		}
		if over {
			changed = true
			out.WriteRune(r)
		} else if r == utf8.RuneError && size == 1 {
			out.WriteByte(b[i])
		} else {
			out.Write(b[i : i+size])
		}
		i += size
	}
	if !changed {
		return s, false
	}
	return out.String(), true
}

func overlongRune(b []byte) (rune, int, bool) {
	if len(b) == 0 {
		return 0, 0, false
	}
	r, size := utf8.DecodeRune(b)
	if r != utf8.RuneError || size != 1 {
		return r, size, false
	}
	c := b[0]
	if c&0xE0 == 0xC0 && len(b) >= 2 && b[1]&0xC0 == 0x80 {
		return rune(c&0x1F)<<6 | rune(b[1]&0x3F), 2, true
	}
	if c&0xF0 == 0xE0 && len(b) >= 3 && b[1]&0xC0 == 0x80 && b[2]&0xC0 == 0x80 {
		return rune(c&0x0F)<<12 | rune(b[1]&0x3F)<<6 | rune(b[2]&0x3F), 3, true
	}
	return utf8.RuneError, 1, false
}

func decodeSlashX(s string) (string, bool) {
	if !strings.Contains(strings.ToLower(s), `\x`) {
		return s, false
	}
	var b strings.Builder
	changed := false
	for i := 0; i < len(s); {
		if i+4 <= len(s) && s[i] == '\\' && (s[i+1] == 'x' || s[i+1] == 'X') && isHexByte(s[i+2]) && isHexByte(s[i+3]) {
			b.WriteByte(unhex(s[i+2])<<4 | unhex(s[i+3]))
			i += 4
			changed = true
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	if !changed {
		return s, false
	}
	return b.String(), true
}

func utf16Views(s string) []string {
	if len(s) < 4 || len(s)%2 != 0 || !strings.Contains(s, "\x00") {
		return nil
	}
	var out []string
	for _, be := range []bool{true, false} {
		if decoded := string(utf16ToUTF8([]byte(s), be)); decoded != "" && decoded != s {
			out = append(out, decoded)
		}
	}
	return out
}

func utf16ToUTF8(b []byte, be bool) []byte {
	if len(b)%2 != 0 {
		return nil
	}
	var out []byte
	for i := 0; i < len(b); i += 2 {
		var u uint16
		if be {
			u = uint16(b[i])<<8 | uint16(b[i+1])
		} else {
			u = uint16(b[i+1])<<8 | uint16(b[i])
		}
		out = append(out, string(rune(u))...)
	}
	return out
}

func mimeWrap(s string) string {
	var b strings.Builder
	for len(s) > 16 {
		b.WriteString(s[:16])
		b.WriteByte('\n')
		s = s[16:]
	}
	b.WriteString(s)
	return b.String()
}

func mixedHex(s string) string {
	b := []byte(s)
	for i := range b {
		if i%2 == 0 && b[i] >= 'a' && b[i] <= 'f' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}

func insertEvery(s, sep string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteRune(r)
	}
	return b.String()
}

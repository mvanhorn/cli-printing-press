package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeHomepage(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "public https homepage", raw: "https://www.gohighlevel.com", want: "https://www.gohighlevel.com"},
		{name: "trims surrounding whitespace", raw: "  https://openrouter.ai/  ", want: "https://openrouter.ai/"},
		{name: "keeps a path", raw: "https://example.com/product", want: "https://example.com/product"},
		{name: "empty", raw: "", want: ""},
		{name: "whitespace only", raw: "   ", want: ""},
		{name: "http scheme", raw: "http://example.com", want: ""},
		{name: "ftp scheme", raw: "ftp://x.com", want: ""},
		{name: "no host", raw: "https://", want: ""},
		{name: "not a url", raw: "not a url", want: ""},
		{name: "localhost", raw: "https://localhost", want: ""},
		{name: "localhost with port", raw: "https://localhost:3000", want: ""},
		{name: "ipv4 loopback", raw: "https://127.0.0.1", want: ""},
		{name: "other ipv4 loopback", raw: "https://127.0.0.2", want: ""},
		{name: "ipv6 loopback", raw: "https://[::1]", want: ""},
		{name: "private ipv4", raw: "https://192.168.1.10", want: ""},
		{name: "link-local ipv4", raw: "https://169.254.1.10", want: ""},
		{name: "unspecified address", raw: "https://0.0.0.0", want: ""},
		{name: "public ip address", raw: "https://93.184.215.14", want: "https://93.184.215.14"},
		{name: "localhost with trailing dot", raw: "https://localhost.", want: ""},
		{name: "localhost subdomain", raw: "https://app.localhost", want: ""},
		{name: "shortened ipv4", raw: "https://127.1", want: ""},
		{name: "decimal ipv4", raw: "https://2130706433", want: ""},
		{name: "hex ipv4", raw: "https://0x7f.0.0.1", want: ""},
		{name: "fully qualified public host", raw: "https://example.com.", want: "https://example.com."},
		{name: "internationalized domain", raw: "https://пример.рф", want: "https://пример.рф"},
		{name: "punycode domain", raw: "https://xn--e1afmkfd.xn--p1ai", want: "https://xn--e1afmkfd.xn--p1ai"},
		{name: "template placeholder host", raw: "https://{tenant}.example.com", want: ""},
		{name: "angle placeholder", raw: "https://<your-domain>.com", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeHomepage(tt.raw))
		})
	}
}

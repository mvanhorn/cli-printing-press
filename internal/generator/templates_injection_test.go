package generator

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedGoEscapesInjectedSpecValues is the dynamic counterpart to the
// static TestGoTemplatesEscapeSpecValuesInStringLiterals guard: it generates a
// CLI from a spec whose URL/auth/header/path fields carry a bare double-quote
// payload and asserts every emitted .go file still parses as valid Go. The
// payload `inj"ect` is chosen so that ANY unescaped emission into a Go double-
// quoted string literal (`"inj"ect"`) is a syntax error — so a clean parse of
// every file is proof the spec value was escaped (via printf %q / oneline), and
// could not break out of the literal into executable Go. This is the supply-
// chain RCE guard for printed CLIs built from untrusted (sniffed/imported) specs.
func TestGeneratedGoEscapesInjectedSpecValues(t *testing.T) {
	t.Parallel()

	const q = `inj"ect`
	const qurl = `https://x.example/` + q

	variants := []struct {
		name   string
		mutate func(s *spec.APISpec)
	}{
		{"api-key-headers", func(s *spec.APISpec) {
			s.WebsiteURL = qurl
			s.HealthCheckPath = "/" + q
			s.Auth.KeyURL = qurl
			s.Auth.Header = "X-" + q
			s.Auth.Format = "Bearer {token}" + q
			s.RequiredHeaders = []spec.RequiredHeader{{Name: "H" + q, Value: "V" + q}}
		}},
		{"api-key-query", func(s *spec.APISpec) {
			s.Auth.In = "query"
			s.Auth.Header = "apikey" + q
			s.Auth.KeyURL = qurl
		}},
		{"oauth2", func(s *spec.APISpec) {
			s.Auth.Type = "oauth2"
			s.Auth.OAuth2Grant = "authorization_code"
			s.Auth.AuthorizationURL = qurl
			s.Auth.TokenURL = qurl
			s.Auth.KeyURL = qurl
			s.Auth.Scopes = []string{"read", "scope" + q}
			s.WebsiteURL = qurl
		}},
		{"paginated-query-params", func(s *spec.APISpec) {
			s.Resources = map[string]spec.Resource{
				"items": {
					Description: "Manage items",
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method: "GET", Path: "/items", Description: "List items",
							ResponsePath: "data." + q,
							Params: []spec.Param{
								{Name: "weird", URLName: "wire" + q, Type: "string"},
							},
							Pagination: &spec.Pagination{
								Type: "cursor", LimitParam: "lim" + q, CursorParam: "cur" + q,
								NextCursorPath: "next." + q, HasMoreField: "more" + q,
							},
						},
					},
				},
			}
		}},
		{"session-handshake", func(s *spec.APISpec) {
			s.Auth.Type = "session_handshake"
			s.Auth.BootstrapURL = qurl
			s.Auth.SessionTokenURL = qurl
			s.Auth.TokenParamName = q
			s.Auth.TokenParamIn = "query"
			s.Auth.TokenFormat = "json"
			s.Auth.TokenJSONPath = "data." + q
			s.BaseURL = qurl
		}},
		{"composed-browser", func(s *spec.APISpec) {
			s.Auth.Type = "composed"
			s.Auth.RequiresBrowserSession = true
			s.Auth.CookieDomain = "." + q + ".com"
			s.Auth.BrowserSessionValidationPath = "/" + q
			s.Auth.BrowserSessionValidationMethod = "GET"
			s.Auth.Cookies = []string{"sid"}
		}},
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			s := minimalSpec("inj-" + v.name)
			v.mutate(s)

			dir := filepath.Join(t.TempDir(), "out")
			require.NoError(t, New(s, dir).Generate(), "generation must succeed even with quote-bearing spec values")

			parsed := 0
			walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || !strings.HasSuffix(p, ".go") {
					return nil
				}
				src, readErr := os.ReadFile(p)
				require.NoError(t, readErr)
				rel, _ := filepath.Rel(dir, p)
				if _, perr := parser.ParseFile(token.NewFileSet(), p, src, parser.AllErrors); perr != nil {
					assert.NoError(t, perr, "generated %s must be valid Go — a spec value broke out of a string literal", rel)
				}
				parsed++
				return nil
			})
			require.NoError(t, walkErr)
			assert.Positive(t, parsed, "expected generated .go files to parse")
		})
	}
}

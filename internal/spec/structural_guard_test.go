package spec

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func guardBaseSpec() *APISpec {
	return &APISpec{
		Name:    "guard-test",
		Version: "1.0.0",
		BaseURL: "https://api.example.com",
		Auth:    AuthConfig{Type: "api_key", Header: "Authorization", Format: "Bearer {token}", EnvVars: []string{"X_TOKEN"}},
		Resources: map[string]Resource{
			"items": {Endpoints: map[string]Endpoint{
				"list": {Method: "GET", Path: "/items"},
			}},
		},
	}
}

func TestGuardStructuralStringsRejectsInjection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(s *APISpec)
		field  string
	}{
		{"base_url quote", func(s *APISpec) { s.BaseURL = `https://x"+pwn()+"` }, "BaseURL"},
		{"auth header quote", func(s *APISpec) { s.Auth.Header = `X-"evil` }, "Auth.Header"},
		{"auth keyurl backtick", func(s *APISpec) { s.Auth.KeyURL = "https://x`evil" }, "Auth.KeyURL"},
		{"auth tokenurl backslash", func(s *APISpec) { s.Auth.TokenURL = `https://x\evil` }, "Auth.TokenURL"},
		{"scope quote", func(s *APISpec) { s.Auth.Scopes = []string{`read"x`} }, "Auth.Scopes"},
		{"env var name quote", func(s *APISpec) { s.Auth.EnvVars = []string{`X"Y`} }, "Auth.EnvVars"},
		// Env var names become Go *identifiers* (config struct fields, TOML keys)
		// in generated code, so they cannot be made safe by template-layer %q —
		// this parse-time rejection is their authoritative defense.
		{"env var spec name quote", func(s *APISpec) { s.Auth.EnvVarSpecs = []AuthEnvVar{{Name: `Z"q`}} }, "EnvVarSpecs"},
		{"resource name quote", func(s *APISpec) {
			s.Resources = map[string]Resource{`it"ems`: {Endpoints: map[string]Endpoint{"list": {Method: "GET", Path: "/i"}}}}
		}, "Resources"},
		{"endpoint method newline", func(s *APISpec) {
			r := s.Resources["items"]
			r.Endpoints = map[string]Endpoint{"list": {Method: "GET\n", Path: "/items"}}
			s.Resources["items"] = r
		}, "Method"},
		{"cookie domain quote", func(s *APISpec) { s.Auth.CookieDomain = `.x".com` }, "CookieDomain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := guardBaseSpec()
			tc.mutate(s)
			err := GuardStructuralStrings(s)
			require.Error(t, err, "injection in %s must be rejected", tc.field)
			assert.Contains(t, err.Error(), tc.field)
		})
	}
}

func TestGuardStructuralStringsAllowsProse(t *testing.T) {
	t.Parallel()

	s := guardBaseSpec()
	// Prose / emit-time-escaped fields legitimately carry quotes, backslashes,
	// and newlines; the guard must not reject them.
	s.Description = `He said "hi"; path C:\temp` + "\nsecond line"
	s.Auth.Instructions = `Settings → Tokens → "Generate new"`
	s.Auth.Title = `API "key"`
	r := s.Resources["items"]
	ep := r.Endpoints["list"]
	ep.Description = "Returns \"items\"\nas JSON"
	ep.Params = []Param{{Name: "q", Type: "string", Description: `search "term"`, Default: `a"b\c`}}
	r.Endpoints["list"] = ep
	s.Resources["items"] = r

	require.NoError(t, GuardStructuralStrings(s), "prose/escaped-at-emit fields must be allowed to contain quotes")
}

func TestGuardStructuralStringsRawStringFields(t *testing.T) {
	t.Parallel()

	// rawstring fields are emitted inside Go backtick raw strings: quotes,
	// backslashes and newlines are legal there, but a backtick breaks out.
	allow := guardBaseSpec()
	allow.Streaming.SubscribeShape = "{\"action\":\"subscribe\",\n\"path\":\"a\\b\"}"
	require.NoError(t, GuardStructuralStrings(allow), "rawstring field may contain quotes/backslash/newline")

	reject := guardBaseSpec()
	reject.Streaming.SubscribeShape = "{\"x\":\"`+pwn()+`\"}"
	err := GuardStructuralStrings(reject)
	require.Error(t, err, "a backtick in a rawstring field must be rejected")
	assert.Contains(t, err.Error(), "SubscribeShape")
}

func TestGuardStructuralStringsParseRejectsInternalSpec(t *testing.T) {
	t.Parallel()

	// End-to-end through ParseBytes: a base_url breakout payload must be rejected
	// at parse time (not silently carried into generation).
	yaml := `
name: pwn
version: 1.0.0
base_url: 'https://x"+pwn()+"'
auth:
  type: api_key
  header: Authorization
  env_vars: [X_TOKEN]
resources:
  items:
    endpoints:
      list:
        method: GET
        path: /items
`
	_, err := ParseBytes([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "disallowed character")
}

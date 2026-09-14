package browsersniff

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactHeaders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		headers       map[string]string
		wantRedacted  map[string]string
		wantRedacList []string
	}{
		{
			name: "authorization redacted",
			headers: map[string]string{
				"Authorization": "Bearer eyJtok.foo.bar",
				"Accept":        "application/json",
			},
			wantRedacted: map[string]string{
				"Authorization": RedactedSentinel,
				"Accept":        "application/json",
			},
			wantRedacList: []string{"authorization"},
		},
		{
			name:          "no auth headers preserves map and returns nil list",
			headers:       map[string]string{"Accept": "application/json"},
			wantRedacted:  map[string]string{"Accept": "application/json"},
			wantRedacList: nil,
		},
		{
			name: "cookie set-cookie csrf api-key all redacted",
			headers: map[string]string{
				"Cookie":       "session=x",
				"Set-Cookie":   "session=x; Path=/",
				"X-CSRF-Token": "csrf",
				"X-API-Key":    "k",
			},
			wantRedacted: map[string]string{
				"Cookie":       RedactedSentinel,
				"Set-Cookie":   RedactedSentinel,
				"X-CSRF-Token": RedactedSentinel,
				"X-API-Key":    RedactedSentinel,
			},
			wantRedacList: []string{"cookie", "set-cookie", "x-api-key", "x-csrf-token"},
		},
		{
			name: "contains-token contains-secret contains-signature patterns",
			headers: map[string]string{
				"X-Auth-Token":    "t",
				"X-Hub-Secret":    "s",
				"X-Sig-Signature": "g",
				"X-Trace-Id":      "non-auth",
			},
			wantRedacted: map[string]string{
				"X-Auth-Token":    RedactedSentinel,
				"X-Hub-Secret":    RedactedSentinel,
				"X-Sig-Signature": RedactedSentinel,
				"X-Trace-Id":      "non-auth",
			},
			wantRedacList: []string{"x-auth-token", "x-hub-secret", "x-sig-signature"},
		},
		{
			name:          "empty input returns empty output",
			headers:       map[string]string{},
			wantRedacted:  map[string]string{},
			wantRedacList: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotHeaders, gotList := RedactHeaders(tc.headers)
			assert.Equal(t, tc.wantRedacted, gotHeaders)
			assert.Equal(t, tc.wantRedacList, gotList)
		})
	}
}

func TestRedactJSONBody_KeyBased(t *testing.T) {
	t.Parallel()

	body := `{"id":42,"name":"widget","api_key":"sk_live_x","nested":{"accessToken":"abc","ok":true}}`
	redacted, paths := RedactJSONBody(body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(redacted), &parsed))
	assert.Equal(t, RedactedSentinel, parsed["api_key"])
	assert.Equal(t, float64(42), parsed["id"])
	assert.Equal(t, "widget", parsed["name"])

	nested, ok := parsed["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, RedactedSentinel, nested["accessToken"])
	assert.Equal(t, true, nested["ok"])

	assert.Contains(t, paths, "api_key")
	assert.Contains(t, paths, "nested.accessToken")
}

func TestRedactJSONBody_KeyVariantsCollapse(t *testing.T) {
	t.Parallel()

	body := `{"api_key":"a","apiKey":"b","api-key":"c","API-Key":"d"}`
	redacted, paths := RedactJSONBody(body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(redacted), &parsed))
	for _, key := range []string{"api_key", "apiKey", "api-key", "API-Key"} {
		assert.Equal(t, RedactedSentinel, parsed[key], "key %q should be redacted", key)
	}
	assert.Len(t, paths, 4)
}

func TestRedactJSONBody_ValuePatterns(t *testing.T) {
	t.Parallel()

	body := `{"token_field":"eyJhbGc.eyJzdWI.sig","contact":"user@example.com","line":"+14155551212"}`
	redacted, paths := RedactJSONBody(body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(redacted), &parsed))
	assert.Equal(t, RedactedSentinel, parsed["contact"])
	assert.Equal(t, RedactedSentinel, parsed["line"])
	// "token_field" key isn't in the canonical body-key set so the value
	// itself triggers redaction via the JWT regex.
	assert.Equal(t, RedactedSentinel, parsed["token_field"])

	hasJWT, hasEmail, hasPhone := false, false, false
	for _, p := range paths {
		switch p {
		case "token_field.pattern:jwt":
			hasJWT = true
		case "contact.pattern:email":
			hasEmail = true
		case "line.pattern:phone":
			hasPhone = true
		}
	}
	assert.True(t, hasJWT, "expected JWT pattern path")
	assert.True(t, hasEmail, "expected email pattern path")
	assert.True(t, hasPhone, "expected phone pattern path")
}

func TestRedactJSONBody_ArraysAndNesting(t *testing.T) {
	t.Parallel()

	body := `{"items":[{"password":"p1"},{"password":"p2","other":"keep"}]}`
	redacted, paths := RedactJSONBody(body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(redacted), &parsed))
	items := parsed["items"].([]any)
	require.Len(t, items, 2)
	first := items[0].(map[string]any)
	second := items[1].(map[string]any)
	assert.Equal(t, RedactedSentinel, first["password"])
	assert.Equal(t, RedactedSentinel, second["password"])
	assert.Equal(t, "keep", second["other"])

	assert.Contains(t, paths, "items[0].password")
	assert.Contains(t, paths, "items[1].password")
}

func TestRedactJSONBody_NonJSONFallback(t *testing.T) {
	t.Parallel()

	body := "Token: eyJabc.def.ghi and email user@example.com submitted"
	redacted, paths := RedactJSONBody(body)

	assert.Contains(t, redacted, RedactedSentinel)
	assert.NotContains(t, redacted, "eyJabc.def.ghi")
	assert.NotContains(t, redacted, "user@example.com")
	assert.Contains(t, paths, "pattern:jwt")
	assert.Contains(t, paths, "pattern:email")
}

func TestRedactJSONBody_EmptyBodyReturnsEmpty(t *testing.T) {
	t.Parallel()

	got, paths := RedactJSONBody("")
	assert.Equal(t, "", got)
	assert.Nil(t, paths)

	got2, paths2 := RedactJSONBody("   ")
	assert.Equal(t, "   ", got2)
	assert.Nil(t, paths2)
}

func TestRedactJSONBody_NestedAuthorizationHeaderValue(t *testing.T) {
	t.Parallel()

	const basicBlob = "QWxpY2U6c2VjcmV0MTIz"
	body := `{"name":"headers","formulaMap":{"Content-Type":"\"application/json\"","Authorization":"\"Basic ` + basicBlob + `\""}}`
	redacted, paths := RedactJSONBody(body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(redacted), &parsed))
	formula, ok := parsed["formulaMap"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, RedactedSentinel, formula["Authorization"])
	assert.Equal(t, `"application/json"`, formula["Content-Type"])
	assert.NotContains(t, redacted, basicBlob)
	assert.Contains(t, paths, "formulaMap.Authorization")
}

func TestRedactJSONBody_AuthSchemeValueWithoutHeaderKey(t *testing.T) {
	t.Parallel()

	body := `{"note":"Bearer sk_live_abcdefghijklmnop"}`
	redacted, paths := RedactJSONBody(body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(redacted), &parsed))
	assert.Equal(t, RedactedSentinel, parsed["note"])
	assert.Contains(t, paths, "note.pattern:auth-scheme")
}

func TestRedactJSONBody_UserPassBase64Value(t *testing.T) {
	t.Parallel()

	const blob = "QWxpY2U6c2VjcmV0MTIz"
	body := `{"value":"` + blob + `","keep":"ok"}`
	redacted, paths := RedactJSONBody(body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(redacted), &parsed))
	assert.Equal(t, RedactedSentinel, parsed["value"])
	assert.Equal(t, "ok", parsed["keep"])
	assert.Contains(t, paths, "value.pattern:basic-credential")
}

func TestRedactJSONBody_PreservesURLPathBase64Segments(t *testing.T) {
	t.Parallel()

	const pathSeg = "QWxpY2U6c2VjcmV0MTIz"
	body := `{"url":"https://cdn.example.com/objects/` + pathSeg + `/meta","host":"cdn.example.com","path":"/objects/` + pathSeg + `/meta"}`
	redacted, paths := RedactJSONBody(body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(redacted), &parsed))
	assert.Equal(t, "https://cdn.example.com/objects/"+pathSeg+"/meta", parsed["url"])
	assert.Equal(t, "cdn.example.com", parsed["host"])
	assert.Equal(t, "/objects/"+pathSeg+"/meta", parsed["path"])
	assert.Empty(t, paths)
}

func TestRedactJSONBody_RedactsURLQueryAndUserinfoCredentials(t *testing.T) {
	t.Parallel()

	const jwt = "eyJhbGc.eyJzdWI.signaturevalue"
	const pathSeg = "YWJjZGVmZ2hpamtsbW5vcA"
	const pass = "s3cret-pass"
	body := `{"url":"https://alice:` + pass + `@cdn.example.com/objects/` + pathSeg + `/meta?access_token=` + jwt + `&ok=1","href":"/callback?access_token=` + jwt + `&page=2"}`
	redacted, paths := RedactJSONBody(body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(redacted), &parsed))
	urlVal, _ := parsed["url"].(string)
	hrefVal, _ := parsed["href"].(string)
	assert.NotContains(t, urlVal, jwt)
	assert.NotContains(t, urlVal, pass)
	assert.NotContains(t, hrefVal, jwt)
	assert.Contains(t, urlVal, "/objects/"+pathSeg+"/meta")
	assert.Contains(t, urlVal, "ok=1")
	assert.Contains(t, hrefVal, "page=2")
	assert.Contains(t, urlVal, url.QueryEscape(RedactedSentinel))
	assert.Contains(t, hrefVal, url.QueryEscape(RedactedSentinel))
	assert.Contains(t, paths, "url.pattern:url-credential")
	assert.Contains(t, paths, "href.pattern:url-credential")
}

func TestRedactJSONBody_PhoneRequiresPlusPrefix(t *testing.T) {
	t.Parallel()

	// Long digit run without leading + should not redact (avoid false positives
	// on timestamps, IDs, order numbers).
	body := `{"order_number":"15551234567","line":"+14155551212"}`
	redacted, _ := RedactJSONBody(body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(redacted), &parsed))
	assert.Equal(t, "15551234567", parsed["order_number"])
	assert.Equal(t, RedactedSentinel, parsed["line"])
}

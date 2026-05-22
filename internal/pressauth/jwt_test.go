package pressauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// buildJWT assembles a header.payload.signature triple where signature is a
// fixed string ("sig"). The signature is never verified, so the test only
// needs the three-segment shape to be valid.
func buildJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	return header + "." + payload + "." + sig
}

// jwtWithRawPayload lets a test inject a non-JSON or hand-crafted payload
// segment. Used to drive the JSON-parse-failed and not-base64 branches.
func jwtWithRawPayload(payloadSeg string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	return header + "." + payloadSeg + "." + sig
}

func TestJWTDecodeAndExp(t *testing.T) {
	future := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Second)

	tests := []struct {
		name      string
		token     string
		wantExp   time.Time
		wantZero  bool
		wantErr   error
		errSubstr string
	}{
		{
			name:    "valid exp 30 minutes ahead",
			token:   buildJWT(t, map[string]any{"exp": future.Unix(), "sub": "user-1"}),
			wantExp: future,
		},
		{
			name:     "valid claims with no exp",
			token:    buildJWT(t, map[string]any{"sub": "user-1"}),
			wantZero: true,
		},
		{
			name:    "two-segment value is not a JWT",
			token:   "header.payload",
			wantErr: ErrNotJWTShape,
		},
		{
			name:    "empty value is not a JWT",
			token:   "",
			wantErr: ErrNotJWTShape,
		},
		{
			name:    "three segments but payload is not base64-URL",
			token:   "aaa.!!!notbase64!!!.bbb",
			wantErr: ErrJWTBodyUndecodable,
		},
		{
			name:      "three segments but payload is not JSON",
			token:     jwtWithRawPayload(base64.RawURLEncoding.EncodeToString([]byte("not-json"))),
			errSubstr: "parsing JWT claims",
		},
		{
			name:      "exp claim is a string instead of a number",
			token:     buildJWT(t, map[string]any{"exp": "tomorrow"}),
			errSubstr: "exp claim is not a number",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claims, err := DecodeJWT(tc.token)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("DecodeJWT err = %v, want errors.Is %v", err, tc.wantErr)
				}
				return
			}
			if err != nil && tc.errSubstr == "" {
				t.Fatalf("DecodeJWT err = %v, want nil", err)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("DecodeJWT err = %v, want substr %q", err, tc.errSubstr)
				}
				return
			}

			exp, err := Exp(claims)
			if tc.errSubstr != "" {
				if err == nil {
					t.Fatalf("Exp err = nil, want substr %q", tc.errSubstr)
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("Exp err = %v, want substr %q", err, tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Exp err = %v, want nil", err)
			}
			if tc.wantZero {
				if !exp.IsZero() {
					t.Errorf("Exp = %v, want zero time", exp)
				}
				return
			}
			if !exp.Equal(tc.wantExp) {
				t.Errorf("Exp = %v, want %v", exp, tc.wantExp)
			}
		})
	}
}

func TestExtractJWT(t *testing.T) {
	future := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Second)
	rawJWT := buildJWT(t, map[string]any{"exp": future.Unix()})

	// Build the URL-encoded JSON wrapper shape (e.g. Alaska Airlines
	// guestsession). The wrapper is %7b%22AccessToken%22%3a%22<JWT>%22%7d
	// when fully URL-encoded.
	wrapperJSON := fmt.Sprintf(`{"AccessToken":"%s","IdToken":"id-tok"}`, rawJWT)
	wrappedCookie := url.QueryEscape(wrapperJSON)

	wrapperMissingAccessJSON := `{"IdToken":"id-tok"}`
	wrappedMissing := url.QueryEscape(wrapperMissingAccessJSON)

	tests := []struct {
		name      string
		cookie    string
		wantToken string
		errSubstr string
		wantErr   error
	}{
		{
			name:      "raw JWT passes through",
			cookie:    rawJWT,
			wantToken: rawJWT,
		},
		{
			name:      "URL-encoded JSON wrapper with AccessToken returns inner JWT",
			cookie:    wrappedCookie,
			wantToken: rawJWT,
		},
		{
			name:      "URL-encoded JSON wrapper missing AccessToken",
			cookie:    wrappedMissing,
			errSubstr: "no JWT found in cookie wrapper",
		},
		{
			name:    "empty cookie value",
			cookie:  "",
			wantErr: ErrNotJWTShape,
		},
		{
			name:    "garbage value is not a JWT or wrapper",
			cookie:  "completely-not-a-jwt",
			wantErr: ErrNotJWTShape,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractJWT(tc.cookie)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ExtractJWT err = %v, want errors.Is %v", err, tc.wantErr)
				}
				return
			}
			if tc.errSubstr != "" {
				if err == nil {
					t.Fatalf("ExtractJWT err = nil, want substr %q", tc.errSubstr)
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("ExtractJWT err = %v, want substr %q", err, tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractJWT err = %v, want nil", err)
			}
			if got != tc.wantToken {
				t.Errorf("ExtractJWT = %q, want %q", got, tc.wantToken)
			}
		})
	}
}

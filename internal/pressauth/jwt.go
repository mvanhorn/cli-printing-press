package pressauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ErrNotJWTShape signals that the input does not match the header.payload.signature
// three-segment form. Callers map this to ExitInvalidJWT so the user sees a
// clear "the carrier cookie isn't a JWT" message instead of a JSON parse error.
var ErrNotJWTShape = errors.New("not a JWT shape")

// ErrJWTBodyUndecodable signals that a three-segment value's body did not
// base64-URL decode. Separated from ErrNotJWTShape so we can distinguish
// "string was never a JWT" from "string was almost a JWT but the body is
// corrupt".
var ErrJWTBodyUndecodable = errors.New("could not decode JWT body")

// DecodeJWT returns the claims object from a JWT body. Signature verification
// is intentionally skipped: press-auth captured the cookie from a real
// browser session, so the issuer is already trusted. JWT format is
// header.payload.signature; the payload segment is base64-URL JSON.
func DecodeJWT(token string) (map[string]any, error) {
	if token == "" {
		return nil, ErrNotJWTShape
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrNotJWTShape
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJWTBodyUndecodable, err)
	}
	var claims map[string]any
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, fmt.Errorf("parsing JWT claims: %w", err)
	}
	return claims, nil
}

// Exp extracts the JWT `exp` claim as a time.Time. JWT exp is documented as
// "seconds since the Unix epoch" (RFC 7519 §4.1.4); JSON decodes numeric
// values as float64, so we accept any numeric type but reject strings.
// A missing exp returns the zero time and a nil error so callers can treat
// it as "no known expiry" (some refresh tokens omit exp).
func Exp(claims map[string]any) (time.Time, error) {
	raw, ok := claims["exp"]
	if !ok {
		return time.Time{}, nil
	}
	switch v := raw.(type) {
	case float64:
		return time.Unix(int64(v), 0).UTC(), nil
	case int64:
		return time.Unix(v, 0).UTC(), nil
	case int:
		return time.Unix(int64(v), 0).UTC(), nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return time.Time{}, fmt.Errorf("exp claim is not a number: %w", err)
		}
		return time.Unix(n, 0).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("exp claim is not a number (got %T)", raw)
	}
}

// ExtractJWT returns the JWT inside a carrier cookie value. Two shapes are
// accepted:
//
//  1. The raw JWT token (header.payload.signature).
//  2. URL-encoded JSON wrapper with an `AccessToken` field — the shape used
//     by sites like Alaska Airlines that pack multiple tokens into one cookie
//     (e.g. `%7b%22AccessToken%22%3a%22<JWT>%22%7d`).
//
// The wrapper path is tried first when the cookie looks URL-encoded, because
// the wrapper contains a JWT verbatim and would otherwise pass the shallow
// three-segment shape check.
func ExtractJWT(cookieValue string) (string, error) {
	if cookieValue == "" {
		return "", ErrNotJWTShape
	}
	if looksURLEncodedJSON(cookieValue) {
		if token, err := extractFromWrapper(cookieValue); err == nil {
			return token, nil
		} else if errors.Is(err, errWrapperMissingAccessToken) {
			return "", err
		}
	}
	if _, err := DecodeJWT(cookieValue); err == nil {
		return cookieValue, nil
	}
	// Last-ditch: maybe the value is URL-encoded JSON but didn't match the
	// quick heuristic. Try wrapper extraction explicitly.
	if token, err := extractFromWrapper(cookieValue); err == nil {
		return token, nil
	} else if errors.Is(err, errWrapperMissingAccessToken) {
		return "", err
	}
	return "", ErrNotJWTShape
}

// errWrapperMissingAccessToken is returned by extractFromWrapper when the
// value parses as URL-encoded JSON but does not contain an AccessToken
// field. Distinct from "not a wrapper at all" so ExtractJWT can stop
// trying alternative shapes once we know the input was a wrapper.
var errWrapperMissingAccessToken = errors.New("no JWT found in cookie wrapper")

// looksURLEncodedJSON returns true when the value starts with the
// URL-encoded form of `{`. Cheap heuristic that lets ExtractJWT prefer the
// wrapper path for values like alaskaair's guestsession cookie.
func looksURLEncodedJSON(s string) bool {
	return strings.HasPrefix(s, "%7b") || strings.HasPrefix(s, "%7B")
}

// extractFromWrapper URL-decodes a value, JSON-parses the result, and
// returns the `AccessToken` field if it is a non-empty JWT. Failure modes:
// not URL-decodable, not JSON, missing AccessToken, AccessToken not a JWT.
func extractFromWrapper(value string) (string, error) {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return "", ErrNotJWTShape
	}
	var wrapper map[string]any
	if err := json.Unmarshal([]byte(decoded), &wrapper); err != nil {
		return "", ErrNotJWTShape
	}
	token, ok := wrapper["AccessToken"].(string)
	if !ok || token == "" {
		return "", errWrapperMissingAccessToken
	}
	if _, err := DecodeJWT(token); err != nil {
		return "", fmt.Errorf("AccessToken in wrapper is not a JWT: %w", err)
	}
	return token, nil
}

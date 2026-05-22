package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
)

// isSwagger2SpecJSON reports whether normalized JSON spec bytes describe a
// Swagger 2.0 (OpenAPI v2) document.
//
// Real-world Swagger 2.0 specs (Tripletex, NetSuite REST, Salesforce Tooling)
// frequently contain circular $ref chains through `definitions/`. Routing them
// through openapi2conv.ToV3 before kin-openapi's OpenAPI 3 resolver runs
// avoids the runaway memory + 15-30 minute hangs documented in the retro
// (issue #1241): the conversion rewrites `#/definitions/X` to
// `#/components/schemas/X` and lets the existing cycle-aware OpenAPI 3 code
// path do the resolution work.
//
// Uses a streaming JSON decoder to find the top-level `swagger` key and stop
// reading. Substring scanning is unsafe at this stage: normalizeSpecData
// round-trips through encoding/json, which sorts map keys alphabetically, so
// "swagger" lands AFTER "definitions" and "paths" in the serialized output —
// far past any reasonable head window for a multi-MB spec like Tripletex.
func isSwagger2SpecJSON(data []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	// Expect a top-level object.
	tok, err := dec.Token()
	if err != nil {
		return false
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return false
	}
	// Walk top-level keys. For each key whose value we don't need, skip the
	// value by reading and discarding tokens until the value's depth balances.
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return false
		}
		key, ok := keyTok.(string)
		if !ok {
			return false
		}
		valTok, err := dec.Token()
		if err != nil {
			return false
		}
		if key == "swagger" {
			value, ok := valTok.(string)
			return ok && value == "2.0"
		}
		// If the value is a container, drain it. Scalars consumed one token
		// by the call above and need no further work.
		if delim, ok := valTok.(json.Delim); ok && (delim == '{' || delim == '[') {
			if err := skipJSONValue(dec, 1); err != nil {
				return false
			}
		}
	}
	return false
}

// skipJSONValue consumes JSON tokens from dec until the container depth
// returns to zero. depth must start at 1 because the caller has already read
// the opening `{` or `[`.
func skipJSONValue(dec *json.Decoder, depth int) error {
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return err
			}
			return err
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

// loadSwagger2AsOpenAPI3 parses normalized Swagger 2.0 JSON bytes into an
// openapi2.T, converts the result to an openapi3.T, and returns the converted
// document. The returned doc behaves identically to one produced by
// openapi3.Loader.LoadFromData on the equivalent OpenAPI 3 spec, which lets
// the downstream parser code path stay format-agnostic.
//
// Emits a single stderr line so operators can see why a long-running phase
// happened on Swagger 2.0 input. The retro called out the silent multi-minute
// phase as the biggest UX issue in the failure mode this function exists to
// fix.
func loadSwagger2AsOpenAPI3(data []byte, lenient bool, location *url.URL) (*openapi3.T, error) {
	var doc2 openapi2.T
	if err := json.Unmarshal(data, &doc2); err != nil {
		return nil, fmt.Errorf("loading Swagger 2.0 spec: %w", err)
	}

	// Reuse the OpenAPI 3 loader configuration so the conversion path honors
	// the same external-ref policy (strict file-URI guard + YAML->JSON
	// normalization for referenced files) as the direct OpenAPI 3 load path.
	loader := newConfiguredOpenAPI3Loader(lenient, location)

	fmt.Fprintf(os.Stderr, "info: converting Swagger 2.0 spec to OpenAPI 3 before parsing\n")

	doc3, err := openapi2conv.ToV3WithLoader(&doc2, loader, location)
	if err != nil {
		return nil, fmt.Errorf("converting Swagger 2.0 to OpenAPI 3: %w", err)
	}
	return doc3, nil
}

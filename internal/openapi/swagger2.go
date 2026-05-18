package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
)

// isSwagger2SpecJSON reports whether normalized JSON spec bytes describe a
// Swagger 2.0 (OpenAPI v2) document. The parser pipeline always normalizes
// spec input to JSON via normalizeSpecData before reaching loadOpenAPIDoc, so
// a top-level `"swagger": "2.0"` substring is a reliable signal.
//
// Real-world Swagger 2.0 specs (Tripletex, NetSuite REST, Salesforce Tooling)
// frequently contain circular $ref chains through `definitions/`. Routing them
// through openapi2conv.ToV3 before kin-openapi's OpenAPI 3 resolver runs
// avoids the runaway memory + 15-30 minute hangs documented in the retro
// (issue #1241): the conversion rewrites `#/definitions/X` to
// `#/components/schemas/X` and lets the existing cycle-aware OpenAPI 3 code
// path do the resolution work.
func isSwagger2SpecJSON(data []byte) bool {
	// Tolerate leading whitespace and a leading `{` before the first key.
	// normalizeSpecData emits compact JSON via encoding/json so the key order
	// reflects raw map iteration; sniff for the field anywhere in the first
	// ~4KB instead of insisting on the first key position.
	head := data
	if len(head) > 4096 {
		head = head[:4096]
	}
	if !bytes.Contains(head, []byte(`"swagger"`)) {
		return false
	}
	// Disambiguate from OpenAPI 3 specs that mention "swagger" as a string
	// value (description text, x-* extension keys). Look for the value form
	// `"swagger":"2.0"` or `"swagger": "2.0"` produced by encoding/json's
	// canonical-ish output. encoding/json drops spaces after the colon, but
	// keep both variants for defensive parsing.
	if bytes.Contains(head, []byte(`"swagger":"2.0"`)) ||
		bytes.Contains(head, []byte(`"swagger": "2.0"`)) ||
		bytes.Contains(head, []byte(`"swagger":"2"`)) ||
		bytes.Contains(head, []byte(`"swagger": "2"`)) {
		return true
	}
	return false
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

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = lenient || location != nil

	fmt.Fprintf(os.Stderr, "info: converting Swagger 2.0 spec to OpenAPI 3 before parsing\n")

	doc3, err := openapi2conv.ToV3WithLoader(&doc2, loader, location)
	if err != nil {
		return nil, fmt.Errorf("converting Swagger 2.0 to OpenAPI 3: %w", err)
	}
	return doc3, nil
}

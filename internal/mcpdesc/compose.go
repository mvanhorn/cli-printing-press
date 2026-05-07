// Package mcpdesc composes MCP tool descriptions from a parsed spec
// endpoint, producing a verb-led action sentence followed by Required
// and Optional parameter lines and a Returns clause keyed off the HTTP
// method and response shape. The goal is a baseline description that
// gives an agent enough to choose and call a tool without trial-and-
// error, leaving polish to handle only the residual that needs hand-
// tuning.
//
// Used by the runtime tools.go template (per-tool description at MCP
// registration time) and the tools-manifest.json writer (per-tool
// description that audit, override agents, and MCP hosts read). The
// override sidecar (mcp-descriptions.json) takes precedence wherever
// present; this package supplies the baseline.
package mcpdesc

import (
	"fmt"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// optionalListMax caps how many optional params Compose lists inline
// before truncating to "(plus N more)". Three is enough to convey the
// common filters/options without bloating the description; agents that
// need the full list read tools-manifest.json's params field.
const optionalListMax = 3

// defaultValueMaxLen caps an inline "(default: X)" annotation. Very
// long defaults (large objects, multi-line strings) are skipped rather
// than bloating the description; the agent can read the spec for full
// detail.
const defaultValueMaxLen = 30

// HTTP method constants. Method strings are matched at multiple
// composition sites; defining them once keeps spelling consistent and
// gives the compiler a chance to catch typos in any future site.
const (
	methodPOST   = "POST"
	methodPATCH  = "PATCH"
	methodPUT    = "PUT"
	methodDELETE = "DELETE"
)

type Input struct {
	Endpoint    spec.Endpoint
	NoAuth      bool
	AuthType    string
	PublicCount int
	TotalCount  int
}

// Compose returns the composed description ready for storage in
// tools-manifest.json or registration in tools.go. Auth annotation is
// delegated to naming.MCPDescription so the (public)/(requires auth)
// suffix logic stays single-sourced.
//
// Two pre-composition signals shape the output:
//
//   - "Required:" / "Optional:" with the colon (case-insensitive):
//     structural marker of a hand-tuned override (typically from
//     mcpoverrides.Apply). Full pass-through — Compose only applies
//     auth and method-marker normalization.
//
//   - Informal "returns " mention in the spec description: keep
//     composing Required/Optional from spec, but skip the generated
//     Returns clause to avoid "Returns X. Returns the X." doubling.
func Compose(in Input) string {
	desc := in.Endpoint.Description

	var composed string
	if hasStructuralOverride(desc) {
		composed = composeAction(desc)
	} else {
		var parts []string
		if action := composeAction(desc); action != "" {
			parts = append(parts, action)
		}
		if required := composeRequired(in.Endpoint); required != "" {
			parts = append(parts, required)
		}
		if optional := composeOptional(in.Endpoint); optional != "" {
			parts = append(parts, optional)
		}
		if !mentionsReturn(desc) {
			if returns := composeReturns(in.Endpoint); returns != "" {
				parts = append(parts, returns)
			}
		}
		composed = strings.Join(parts, " ")
	}

	composed = appendMethodMarker(composed, in.Endpoint.Method)
	return naming.MCPDescription(composed, in.NoAuth, in.AuthType, in.PublicCount, in.TotalCount)
}

// hasStructuralOverride detects the colon-terminated markers an
// override author actually writes: "Required:" or "Optional:". The
// colon is the structural cue. Substring "returns " is too noisy —
// spec authors mention return shape in narrative prose constantly
// ("Returns the deleted resource"), and treating those as overrides
// would skip Required/Optional composition for valid spec content.
func hasStructuralOverride(desc string) bool {
	lower := strings.ToLower(desc)
	return strings.Contains(lower, "required:") || strings.Contains(lower, "optional:")
}

// mentionsReturn detects an informal "returns" mention in the action
// sentence. Used only to suppress the generated Returns clause —
// Required/Optional composition still runs.
func mentionsReturn(desc string) bool {
	return strings.Contains(strings.ToLower(desc), "returns ")
}

func composeAction(desc string) string {
	s := strings.TrimSpace(desc)
	if s == "" {
		return ""
	}
	last := s[len(s)-1]
	if last != '.' && last != '!' && last != '?' {
		s += "."
	}
	return s
}

// composeRequired renders the "Required:" line. Path params count as
// required regardless of CLI default — they're structurally in the
// URL and treating them as optional disagrees with the spec's
// parameters[]. Defaulted params gain "(default: X)" annotations so
// the agent knows what the runtime fills in when the param is omitted.
func composeRequired(ep spec.Endpoint) string {
	names := collectParams(ep, isRequiredParam)
	if len(names) == 0 {
		return ""
	}
	return "Required: " + strings.Join(names, ", ") + "."
}

// composeOptional renders the "Optional:" line, capped at
// optionalListMax with "(plus N more)" when more are present. Path
// params never appear here.
func composeOptional(ep spec.Endpoint) string {
	names := collectParams(ep, isOptionalParam)
	if len(names) == 0 {
		return ""
	}
	if len(names) > optionalListMax {
		hidden := len(names) - optionalListMax
		return fmt.Sprintf("Optional: %s (plus %d more).", strings.Join(names[:optionalListMax], ", "), hidden)
	}
	return "Optional: " + strings.Join(names, ", ") + "."
}

// collectParams walks ep.Params followed by ep.Body, returning the
// formatted names of params for which include is true. Single iteration
// site so the path-param-always-required rule and the body/query rules
// stay aligned.
func collectParams(ep spec.Endpoint, include func(spec.Param) bool) []string {
	var names []string
	for _, p := range ep.Params {
		if include(p) {
			names = append(names, formatParam(p))
		}
	}
	for _, p := range ep.Body {
		if include(p) {
			names = append(names, formatParam(p))
		}
	}
	return names
}

// isRequiredParam classifies path params as required regardless of
// the spec's Required flag (they're URL slots) and trusts the flag
// for everything else. ep.Body fields don't carry the path-param
// annotation, so the path-param branch only fires for ep.Params.
func isRequiredParam(p spec.Param) bool {
	if p.Positional || p.PathParam {
		return true
	}
	return p.Required
}

func isOptionalParam(p spec.Param) bool {
	if p.Positional || p.PathParam {
		return false
	}
	return !p.Required
}

// composeReturns derives a return-shape clause from the response type
// and HTTP method. Method-specific markers (Destructive, Partial
// update) are added later by appendMethodMarker so they apply
// uniformly to both fresh-composed and override paths.
func composeReturns(ep spec.Endpoint) string {
	method := strings.ToUpper(ep.Method)
	respType := ep.Response.Type
	item := strings.TrimSpace(ep.Response.Item)

	switch {
	case respType == "array" && item != "":
		return "Returns array of " + item + "."
	case respType == "array":
		return "Returns array."
	case respType == "object" && item != "":
		switch method {
		case methodPOST:
			return "Returns the new " + item + "."
		case methodPATCH, methodPUT:
			return "Returns the updated " + item + "."
		default:
			return "Returns the " + item + "."
		}
	}
	return ""
}

// appendMethodMarker adds the method-specific safety/semantic marker
// when it isn't already present. Applies uniformly to fresh-composed
// and override paths so DELETE always carries "Destructive." even
// when an override author left it off.
//
// PATCH only gets "Partial update." when no Returns clause is
// present — "Returns the updated X." already conveys partial-update
// semantics.
func appendMethodMarker(desc, method string) string {
	if desc == "" {
		return desc
	}
	lower := strings.ToLower(desc)
	switch strings.ToUpper(method) {
	case methodDELETE:
		if !strings.Contains(lower, "destructive") {
			return desc + " Destructive."
		}
	case methodPATCH:
		if !strings.Contains(lower, "partial update") && !strings.Contains(lower, "returns") {
			return desc + " Partial update."
		}
	}
	return desc
}

func formatParam(p spec.Param) string {
	if p.Default == nil {
		return p.PublicInputName()
	}
	val, ok := formatDefault(p.Default)
	if !ok {
		return p.PublicInputName()
	}
	return p.PublicInputName() + " (default: " + val + ")"
}

// formatDefault returns the value and true when usable inline, or
// false when empty or unsuitable for annotation (oversize, multi-line).
func formatDefault(v any) (string, bool) {
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" || len(s) > defaultValueMaxLen {
		return "", false
	}
	if strings.ContainsAny(s, "\n\r") {
		return "", false
	}
	return s, true
}

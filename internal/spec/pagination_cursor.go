package spec

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// ResolveCursorRequestParams fills an empty Pagination.CursorParam from the
// endpoint's declared query parameters when a response cursor path is set.
// cursor_field and next_cursor_path name that response path. An empty request
// slot makes --all repeat the first page and exit 0. An explicit cursor_param
// is preserved, including values that do not match a declared query parameter.
// A dotted response path is not guessed onto a differently named query
// parameter. Offset and page pagination are left unchanged. When no request
// parameter can be determined, every endpoint is left unchanged and the error
// names each endpoint that still cannot send the cursor back.
func (s *APISpec) ResolveCursorRequestParams() error {
	if s == nil {
		return nil
	}
	var problems []string
	var fills []cursorParamFill
	walkCursorPagination(s.Resources, "", nil, func(loc string, endpoint *Endpoint) {
		param, err := resolveEndpointCursorRequestParam(endpoint)
		if err != nil {
			problems = append(problems, loc+": "+err.Error())
			return
		}
		if param == "" || endpoint == nil || endpoint.Pagination == nil {
			return
		}
		// The walk copies endpoint values out of the resource map. Pagination
		// is a pointer, so the fill updates the spec rather than that copy.
		fills = append(fills, cursorParamFill{pagination: endpoint.Pagination, param: param})
	})
	if len(problems) > 0 {
		slices.Sort(problems)
		return fmt.Errorf("cursor pagination is missing a request parameter:\n%s", strings.Join(problems, "\n"))
	}
	for _, fill := range fills {
		fill.pagination.CursorParam = fill.param
	}
	return nil
}

type cursorParamFill struct {
	pagination *Pagination
	param      string
}

func walkCursorPagination(resources map[string]Resource, resourceName string, subPath []string, visit func(string, *Endpoint)) {
	for _, name := range sortedStringKeys(resources) {
		resource := resources[name]
		locResource := resourceName
		var nextSub []string
		if resourceName == "" {
			locResource = name
		} else {
			nextSub = append(append([]string{}, subPath...), name)
		}
		for _, eName := range sortedStringKeys(resource.Endpoints) {
			endpoint := resource.Endpoints[eName]
			visit(cursorEndpointLocation(locResource, nextSub, eName), &endpoint)
		}
		if len(resource.SubResources) == 0 {
			continue
		}
		walkCursorPagination(resource.SubResources, locResource, nextSub, visit)
	}
}

func cursorEndpointLocation(resource string, sub []string, endpoint string) string {
	if len(sub) == 0 {
		return fmt.Sprintf("resource %q endpoint %q", resource, endpoint)
	}
	return fmt.Sprintf("resource %q sub-resource %q endpoint %q", resource, strings.Join(sub, "/"), endpoint)
}

func resolveEndpointCursorRequestParam(endpoint *Endpoint) (string, error) {
	if endpoint == nil || !paginationNeedsCursorRequestParam(endpoint.Pagination) {
		return "", nil
	}
	path := strings.TrimSpace(endpoint.Pagination.NextCursorPath)
	leaf, simple := cursorResponseLeaf(path)
	if param, err := oneCursorWire(path, cursorLeafParams(endpoint, leaf)); err != nil || param != "" {
		return param, err
	}
	if !simple {
		// Dotted paths (pageInfo.endCursor) and other non-identifiers are not
		// mapped onto a differently named query parameter.
		return "", undeterminableCursorRequestParam(path)
	}
	if !simpleCursorFieldAllowsParamFallback(leaf) {
		return "", undeterminableCursorRequestParam(path)
	}
	if param, err := oneCursorWire(path, cursorShapedParams(cursorRequestCandidates(endpoint))); err != nil || param != "" {
		return param, err
	}
	return "", undeterminableCursorRequestParam(path)
}

func oneCursorWire(path string, params []Param) (string, error) {
	switch wires := uniqueCursorWireNames(params); len(wires) {
	case 0:
		return "", nil
	case 1:
		return wires[0], nil
	default:
		return "", ambiguousCursorRequestParam(path, wires)
	}
}

func paginationNeedsCursorRequestParam(p *Pagination) bool {
	if p == nil || strings.TrimSpace(p.CursorParam) != "" || strings.TrimSpace(p.NextCursorPath) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(p.Type)) {
	case "", "cursor", "page_token":
		return true
	default:
		return false
	}
}

func cursorResponseLeaf(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if i := strings.LastIndex(path, "."); i >= 0 {
		return strings.TrimSpace(path[i+1:]), false
	}
	return path, simpleCursorIdentifier(path)
}

func simpleCursorIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}

// Single-segment fields that are not themselves cursor-shaped (offset, page)
// stay exact-match only, so a response field is not mapped onto a differently
// named query parameter.
func simpleCursorFieldAllowsParamFallback(leaf string) bool {
	lower := strings.ToLower(strings.TrimSpace(leaf))
	if strings.Contains(lower, "cursor") || strings.Contains(lower, "token") || strings.Contains(lower, "continuation") {
		return true
	}
	switch lower {
	case "after", "before", "next", "starting_after":
		return true
	default:
		return false
	}
}

func cursorRequestCandidates(endpoint *Endpoint) []Param {
	if endpoint == nil {
		return nil
	}
	out := make([]Param, 0, len(endpoint.Params))
	for _, p := range endpoint.Params {
		if cursorRequestCandidate(p) {
			out = append(out, p)
		}
	}
	return out
}

func cursorRequestCandidate(p Param) bool {
	if p.Positional || p.PathParam || p.HasDateFormat() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(p.In)) {
	case "", "query":
	default:
		return false
	}
	switch strings.ToLower(strings.TrimSpace(p.Type)) {
	case "array", "object":
		return false
	}
	_, ok := cursorWireName(p)
	return ok
}

func cursorLeafParams(endpoint *Endpoint, leaf string) []Param {
	if leaf == "" {
		return nil
	}
	var out []Param
	for _, p := range cursorRequestCandidates(endpoint) {
		if cursorLeafMatches(p, leaf) {
			out = append(out, p)
		}
	}
	return out
}

func cursorLeafMatches(p Param, leaf string) bool {
	if strings.EqualFold(strings.TrimSpace(p.Name), leaf) {
		return true
	}
	wire, ok := cursorWireName(p)
	return ok && strings.EqualFold(strings.TrimSpace(wire), leaf)
}

func cursorShapedParams(params []Param) []Param {
	out := make([]Param, 0, len(params))
	for _, p := range params {
		if isCursorShapedRequestName(p.Name) || isCursorShapedRequestName(p.WireName()) {
			out = append(out, p)
		}
	}
	return out
}

var cursorShapedRequestNames = map[string]struct{}{
	"after":              {},
	"before":             {},
	"starting_after":     {},
	"page_token":         {},
	"pagetoken":          {},
	"page_key":           {},
	"pagekey":            {},
	"next_token":         {},
	"nexttoken":          {},
	"continuation":       {},
	"continuation_token": {},
}

func isCursorShapedRequestName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	if _, ok := cursorShapedRequestNames[lower]; ok {
		return true
	}
	return strings.Contains(lower, "cursor")
}

func cursorWireName(p Param) (string, bool) {
	wire := p.WireName()
	if strings.TrimSpace(wire) == "" {
		return "", false
	}
	return wire, true
}

func uniqueCursorWireNames(params []Param) []string {
	seen := make(map[string]struct{}, len(params))
	names := make([]string, 0, len(params))
	for _, p := range params {
		wire, ok := cursorWireName(p)
		if !ok {
			continue
		}
		if _, exists := seen[wire]; exists {
			continue
		}
		seen[wire] = struct{}{}
		names = append(names, wire)
	}
	slices.Sort(names)
	return names
}

func ambiguousCursorRequestParam(path string, wires []string) error {
	names := append([]string(nil), wires...)
	slices.Sort(names)
	names = slices.Compact(names)
	return fmt.Errorf("cursor pagination response path %q matches more than one query parameter (%s); set cursor_param explicitly", path, strings.Join(names, ", "))
}

func undeterminableCursorRequestParam(path string) error {
	return fmt.Errorf("cursor pagination response path %q has no request parameter; set cursor_param to the query parameter that sends the cursor back on follow-up requests", path)
}

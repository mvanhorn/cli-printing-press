package generator

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// mcpPageSettings is the generator-side shape of the emitted mcpPageConfig.
// ExposeOpaqueCursor is not emitted: it only decides whether the tool schema
// grows a synthetic cursor input.
type mcpPageSettings struct {
	CursorParam          string
	NextCursorPath       string
	HasMoreField         string
	ContinuationInput    string
	CursorInBody         bool
	ExternalContinuation bool
	ExposeOpaqueCursor   bool
	// BodyPath is the full request-body path, including the leaf, when the
	// cursor is nested. Empty means CursorParam is a top-level body or query key.
	BodyPath []string
}

type mcpEndpointInput struct {
	public   string
	wire     string
	inBody   bool
	bodyPath []string
}

// mcpToolPageConfig renders the page config for one operation. GET cursor
// lists keep the historical two-field literal. Read-only non-GET lists get
// the same opaque resume when pagination is declared or the operation is a
// cursor list / continuation pair. Mutating POSTs stay empty so a truncated
// response is never replayed.
func mcpToolPageConfig(resource spec.Resource, opName string, endpoint spec.Endpoint) string {
	return renderMCPPageConfig(deriveMCPPageSettings(resource, opName, endpoint))
}

func mcpExposeOpaqueCursor(resource spec.Resource, opName string, endpoint spec.Endpoint) bool {
	return deriveMCPPageSettings(resource, opName, endpoint).ExposeOpaqueCursor
}

func deriveMCPPageSettings(resource spec.Resource, opName string, endpoint spec.Endpoint) mcpPageSettings {
	if strings.EqualFold(strings.TrimSpace(endpoint.Method), "GET") {
		if !mcpEndpointPageable(endpoint) {
			return mcpPageSettings{}
		}
		return mcpPageSettings{
			CursorParam:        strings.TrimSpace(endpoint.Pagination.CursorParam),
			NextCursorPath:     mcpNextCursorPath(endpoint),
			ExposeOpaqueCursor: true,
		}
	}
	if endpoint.UsesBinaryResponse() || endpoint.UsesTextResponse() || !endpointIsReadCommand(endpoint, opName) {
		return mcpPageSettings{}
	}
	inputs := mcpEndpointInputs(endpoint)
	if endpoint.Pagination != nil && strings.TrimSpace(endpoint.Pagination.CursorParam) != "" {
		return settingsFromDeclaredPagination(endpoint, inputs)
	}
	if in, ok := findExactlyOneCursorInput(inputs); ok && (len(inputs) == 1 || mcpCursorListShaped(opName, endpoint, inputs)) {
		return settingsFromCursorInput(endpoint, in, inputs)
	}
	if settings, ok := externalContinuationSettings(resource, opName, endpoint, inputs); ok {
		return settings
	}
	return mcpPageSettings{}
}

func mcpNextCursorPath(endpoint spec.Endpoint) string {
	if endpoint.Pagination == nil {
		return ""
	}
	nextCursorPath := endpoint.Pagination.NextCursorPath
	paginationType := strings.ToLower(strings.TrimSpace(endpoint.Pagination.Type))
	if strings.TrimSpace(nextCursorPath) == "" && paginationType != "offset" && paginationType != "page" {
		nextCursorPath = endpoint.Pagination.CursorParam
	}
	return nextCursorPath
}

func settingsFromDeclaredPagination(endpoint spec.Endpoint, inputs []mcpEndpointInput) mcpPageSettings {
	cursorParam := strings.TrimSpace(endpoint.Pagination.CursorParam)
	hasMore := strings.TrimSpace(endpoint.Pagination.HasMoreField)
	paginationType := strings.ToLower(strings.TrimSpace(endpoint.Pagination.Type))
	if hasMore == "" && paginationType != "offset" && paginationType != "page" {
		hasMore = "has_more"
	}
	cursorInBody := true
	continuation := cursorParam
	var bodyPath []string
	if in, ok := findInputByWireOrPublic(inputs, cursorParam); ok {
		cursorInBody = in.inBody
		continuation = in.public
		bodyPath = slices.Clone(in.bodyPath)
	}
	if cursorInBody && !endpointCanReplayBodyCursor(endpoint) {
		return mcpPageSettings{}
	}
	return mcpPageSettings{
		CursorParam:        cursorParam,
		NextCursorPath:     mcpNextCursorPath(endpoint),
		HasMoreField:       hasMore,
		ContinuationInput:  continuation,
		CursorInBody:       cursorInBody,
		BodyPath:           bodyPath,
		ExposeOpaqueCursor: !mcpHasPublicCursor(inputs),
	}
}

func settingsFromCursorInput(endpoint spec.Endpoint, in mcpEndpointInput, inputs []mcpEndpointInput) mcpPageSettings {
	if in.inBody && !endpointCanReplayBodyCursor(endpoint) {
		return mcpPageSettings{}
	}
	return mcpPageSettings{
		CursorParam:        in.wire,
		NextCursorPath:     in.wire,
		HasMoreField:       "has_more",
		ContinuationInput:  in.public,
		CursorInBody:       in.inBody,
		BodyPath:           slices.Clone(in.bodyPath),
		ExposeOpaqueCursor: !mcpHasPublicCursor(inputs),
	}
}

// Opaque JSON and non-form raw bodies are sent as a blob the replay path does
// not rewrite, so a cursor that only lives there cannot be paged.
func endpointCanReplayBodyCursor(endpoint spec.Endpoint) bool {
	return !endpoint.BodyJSONFallback && !endpoint.UsesRawRequestBody()
}

func externalContinuationSettings(resource spec.Resource, opName string, endpoint spec.Endpoint, selfInputs []mcpEndpointInput) (mcpPageSettings, bool) {
	listPath := strings.TrimRight(canonicalRPCPath(endpoint.Path), "/")
	names := make([]string, 0, len(resource.Endpoints))
	for name, ep := range resource.Endpoints {
		if name == opName {
			continue
		}
		if isContinuationSibling(opName, name) || continuationPathMatches(listPath, ep.Path) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	seen := map[string]struct{}{}
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		ep := resource.Endpoints[name]
		if ep.UsesBinaryResponse() || ep.UsesTextResponse() || !endpointIsReadCommand(ep, name) {
			continue
		}
		in, ok := soleCursorInput(mcpEndpointInputs(ep))
		if !ok {
			continue
		}
		return mcpPageSettings{
			NextCursorPath:       in.wire,
			HasMoreField:         "has_more",
			ContinuationInput:    in.public,
			ExternalContinuation: true,
			ExposeOpaqueCursor:   !mcpHasPublicCursor(selfInputs),
		}, true
	}
	return mcpPageSettings{}, false
}

func isContinuationSibling(listName, candidate string) bool {
	if strings.EqualFold(candidate, listName+"-continue") || strings.EqualFold(candidate, listName+"_continue") {
		return true
	}
	if !strings.ContainsAny(listName, "-_") && strings.EqualFold(candidate, listName+"Continue") {
		return true
	}
	return false
}

func continuationPathMatches(listPath, raw string) bool {
	if listPath == "" {
		return false
	}
	got := strings.TrimRight(canonicalRPCPath(raw), "/")
	return strings.EqualFold(got, listPath+"/continue")
}

func soleCursorInput(inputs []mcpEndpointInput) (mcpEndpointInput, bool) {
	if len(inputs) != 1 {
		return mcpEndpointInput{}, false
	}
	if !mcpCursorNamed(inputs[0]) {
		return mcpEndpointInput{}, false
	}
	return inputs[0], true
}

func findExactlyOneCursorInput(inputs []mcpEndpointInput) (mcpEndpointInput, bool) {
	var found mcpEndpointInput
	n := 0
	for _, in := range inputs {
		if mcpCursorNamed(in) {
			n++
			found = in
		}
	}
	if n != 1 {
		return mcpEndpointInput{}, false
	}
	return found, true
}

func findInputByWireOrPublic(inputs []mcpEndpointInput, name string) (mcpEndpointInput, bool) {
	name = strings.TrimSpace(name)
	for _, in := range inputs {
		if in.wire == name || in.public == name || strings.Join(in.bodyPath, ".") == name {
			return in, true
		}
	}
	return mcpEndpointInput{}, false
}

func mcpHasPublicCursor(inputs []mcpEndpointInput) bool {
	for _, in := range inputs {
		if in.public == "cursor" {
			return true
		}
	}
	return false
}

func mcpCursorListShaped(opName string, endpoint spec.Endpoint, inputs []mcpEndpointInput) bool {
	tokens := camelCaseTokens(strings.TrimSpace(opName))
	if len(tokens) > 0 {
		switch strings.ToLower(tokens[0]) {
		case "list", "search", "find", "query":
			return true
		}
	}
	for _, in := range inputs {
		if mcpLimitNamed(in.wire) || mcpLimitNamed(in.public) {
			return true
		}
	}
	return endpoint.Pagination != nil && strings.TrimSpace(endpoint.Pagination.LimitParam) != ""
}

func mcpCursorNamed(in mcpEndpointInput) bool {
	return mcpIsCursorName(in.wire) || mcpIsCursorName(in.public)
}

func mcpIsCursorName(name string) bool {
	switch mcpNormalizedPagingName(name) {
	case "cursor", "pagetoken", "nexttoken", "nextcursor", "after", "startingafter", "page[cursor]":
		return true
	default:
		return false
	}
}

func mcpLimitNamed(name string) bool {
	switch mcpNormalizedPagingName(name) {
	case "limit", "pagesize", "perpage", "maxresults", "take":
		return true
	default:
		return false
	}
}

func mcpNormalizedPagingName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.NewReplacer("_", "", "-", "").Replace(name)
}

func mcpEndpointInputs(endpoint spec.Endpoint) []mcpEndpointInput {
	out := make([]mcpEndpointInput, 0, len(endpoint.Params)+len(endpoint.Body))
	for _, p := range endpoint.Params {
		if p.PathParam || paramIsHeader(p) {
			continue
		}
		in := strings.ToLower(strings.TrimSpace(p.In))
		if in == "path" {
			continue
		}
		if (in == "" || in == "query") && strings.Contains(endpoint.Path, "{"+p.Name+"}") {
			continue
		}
		out = append(out, mcpEndpointInput{
			public: p.PublicInputName(),
			wire:   p.WireName(),
			inBody: false,
		})
	}
	if endpoint.BodyJSONFallback {
		return out
	}
	if bodyUsesFlatEmission(endpoint) {
		for _, p := range endpoint.Body {
			if p.Type == "object" && len(p.Fields) > 0 {
				continue
			}
			out = append(out, mcpEndpointInput{
				public: p.PublicInputName(),
				wire:   p.BodyWireName(),
				inBody: true,
			})
		}
		return out
	}
	appendMCPEndpointBodyInputs(&out, flattenCollidingBodyFields(endpoint.Body), 0, "", nil)
	return out
}

func appendMCPEndpointBodyInputs(out *[]mcpEndpointInput, body []spec.Param, depth int, flagPrefix string, bodyPath []string) {
	for _, p := range body {
		if p.Type == "object" && len(p.Fields) > 0 && depth+1 < maxBodyFlagDepth {
			nextPath := append(slices.Clone(bodyPath), p.BodyWireName())
			appendMCPEndpointBodyInputs(out, p.Fields, depth+1, joinFlag(flagPrefix, publicFlagName(p)), nextPath)
			continue
		}
		public := p.PublicInputName()
		if flagPrefix != "" {
			public = joinFlag(flagPrefix, publicFlagName(p))
		}
		in := mcpEndpointInput{
			public: public,
			wire:   p.BodyWireName(),
			inBody: true,
		}
		if len(bodyPath) > 0 {
			in.bodyPath = append(append([]string(nil), bodyPath...), p.BodyWireName())
		}
		*out = append(*out, in)
	}
}

func renderMCPPageConfig(s mcpPageSettings) string {
	if s.CursorParam == "" && s.NextCursorPath == "" && s.HasMoreField == "" && s.ContinuationInput == "" && !s.CursorInBody && !s.ExternalContinuation && len(s.BodyPath) == 0 {
		return "mcpPageConfig{}"
	}
	// GET lists only fill CursorParam and NextCursorPath. Keep that literal
	// byte-identical, including an explicit empty NextCursorPath for offset
	// and page pagination.
	if len(s.BodyPath) == 0 && s.HasMoreField == "" && s.ContinuationInput == "" && !s.CursorInBody && !s.ExternalContinuation {
		return fmt.Sprintf("mcpPageConfig{CursorParam: %q, NextCursorPath: %q}", s.CursorParam, s.NextCursorPath)
	}
	parts := make([]string, 0, 7)
	if s.CursorParam != "" {
		parts = append(parts, fmt.Sprintf("CursorParam: %q", s.CursorParam))
	}
	if s.NextCursorPath != "" {
		parts = append(parts, fmt.Sprintf("NextCursorPath: %q", s.NextCursorPath))
	}
	if s.HasMoreField != "" {
		parts = append(parts, fmt.Sprintf("HasMoreField: %q", s.HasMoreField))
	}
	if s.ContinuationInput != "" {
		parts = append(parts, fmt.Sprintf("ContinuationInput: %q", s.ContinuationInput))
	}
	if s.CursorInBody {
		parts = append(parts, "CursorInBody: true")
	}
	if len(s.BodyPath) > 0 {
		quoted := make([]string, len(s.BodyPath))
		for i, part := range s.BodyPath {
			quoted[i] = fmt.Sprintf("%q", part)
		}
		parts = append(parts, "BodyPath: []string{"+strings.Join(quoted, ", ")+"}")
	}
	if s.ExternalContinuation {
		parts = append(parts, "ExternalContinuation: true")
	}
	return "mcpPageConfig{" + strings.Join(parts, ", ") + "}"
}

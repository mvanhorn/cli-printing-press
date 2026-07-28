package generator

import (
	"fmt"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// validateMCPInputNames asserts, AFTER the flag-collision dedup pass has
// assigned final identifiers, that every model-facing MCP input name the
// templates will emit for each endpoint (a) matches the Anthropic tool
// property-key grammar spec.MCPPropertyKeyPattern and (b) is unique within
// its tool's input schema, including generator-reserved inputs. An illegal
// key bricks the calling agent session at schema load; a duplicate key makes
// one agent-supplied value silently fan into two wire locations. Collisions
// the dedup pass resolves never reach this check — only residual ones
// (e.g. 64-char clamp truncations, reserved-name hits) fail, loudly.
// It runs from prepareOutput, so both Generate and GenerateMCPSurface
// entry points are covered before anything renders.
func (g *Generator) validateMCPInputNames() error {
	if g.Spec == nil {
		return nil
	}
	vars := g.Spec.GlobalPathTemplateVars
	for _, resName := range sortedKeys(g.Spec.Resources) {
		res := g.Spec.Resources[resName]
		for _, epName := range sortedKeys(res.Endpoints) {
			ep := res.Endpoints[epName]
			if err := validateEndpointMCPInputNames(resName, epName, ep, effectiveEndpointPath(res, ep), vars); err != nil {
				return err
			}
		}
		for _, subName := range sortedKeys(res.SubResources) {
			sub := res.SubResources[subName]
			for _, epName := range sortedKeys(sub.Endpoints) {
				ep := sub.Endpoints[epName]
				if err := validateEndpointMCPInputNames(resName+"/"+subName, epName, ep, effectiveSubEndpointPath(res, sub, ep), vars); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validateEndpointMCPInputNames records every input name the MCP template
// will emit for one endpoint's tool — the generator-reserved inputs first
// (mirroring mcp_tools.go.tmpl's pageable cursor and raw-body blocks; the
// body_json fallback input arrives via mcpParamBindings, so it is NOT
// recorded as reserved here), then the same mcpParamBindings /
// mcpGlobalTemplateBindings producers the template renders from — and fails
// on the first illegal or duplicate key.
func validateEndpointMCPInputNames(resKey, epName string, ep spec.Endpoint, pathTemplate string, vars []string) error {
	type source struct{ label string }
	seen := map[string]source{}
	record := func(name, label string) error {
		if !spec.MCPPropertyKeyRe.MatchString(name) {
			return fmt.Errorf("resource %q endpoint %q: MCP input name %s is not a legal tool property key (must match %s, 1-64 chars); rename via flag_name or x-pp-param-url-names — an illegal key bricks the calling agent session at schema load", resKey, epName, truncateName(name), spec.MCPPropertyKeyPattern)
		}
		if prev, ok := seen[name]; ok {
			return fmt.Errorf("resource %q endpoint %q: MCP input names collide: %s and %s both emit schema key %q; rename one via flag_name or x-pp-param-url-names", resKey, epName, prev.label, label, name)
		}
		seen[name] = source{label: label}
		return nil
	}
	// Reserved, generator-injected inputs first — mirror mcp_tools.go.tmpl's
	// pageable-cursor and raw-body WithString emissions.
	if mcpEndpointPageable(ep) {
		if err := record("cursor", "the generated pagination input"); err != nil {
			return err
		}
	}
	if ep.UsesRawRequestBody() {
		for _, r := range []string{"body_base64", "content_type"} {
			if err := record(r, "the generated raw-body input"); err != nil {
				return err
			}
		}
	}
	// The template's own binding producers — validated set == emitted set.
	// The binding's WireName is the raw wire-side name, so the failure
	// message names both colliding sources by their wire identity.
	for _, b := range mcpParamBindings(ep, pathTemplate) {
		if err := record(b.PublicName, fmt.Sprintf("%s input from wire param %s", b.Location, truncateName(b.WireName))); err != nil {
			return err
		}
	}
	for _, b := range mcpGlobalTemplateBindings(ep, pathTemplate, vars) {
		if err := record(b.PublicName, fmt.Sprintf("global path-template var %s", truncateName(b.WireName))); err != nil {
			return err
		}
	}
	return nil
}

// truncateName keeps failure messages readable for hostile-length names while
// preserving the differing tail (clamp collisions differ only past char 64).
func truncateName(name string) string {
	if len(name) <= 80 {
		return fmt.Sprintf("%q", name)
	}
	return fmt.Sprintf("%q…%q (len %d)", name[:40], name[len(name)-24:], len(name))
}

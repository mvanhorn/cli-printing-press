// Package mcpoverrides reads the per-CLI MCP description override file
// (mcp-descriptions.json at the cli-dir root) and applies it to a parsed
// spec before generation. The override file is the sanctioned path for
// hand-authored MCP tool descriptions on typed endpoint tools: direct
// edits to internal/mcp/tools.go and tools-manifest.json are wiped on
// the next regen because both files carry the generator's DO-NOT-EDIT
// header. The override file is hand-editable and survives regeneration
// because it lives outside the generator's emit set; mcp-sync calls
// Apply on the parsed spec so both the manifest writer and the
// mcp_tools.go template see the overridden descriptions.
package mcpoverrides

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
)

// Filename is the per-CLI override file name. Lives at the cli-dir
// root alongside spec.yaml / spec.json / .printing-press.json.
const Filename = "mcp-descriptions.json"

// Overrides holds the user-authored description overrides keyed by
// MCP tool name. Tool name format matches tools-manifest.json's name
// field: snake_case resource name + "_" + snake_case endpoint name,
// optionally with a sub-resource segment in the middle (e.g.,
// "tags_create", "bookings_attendees_booking-add").
type Overrides struct {
	Descriptions map[string]string `json:"descriptions"`
}

// Load reads <cliDir>/mcp-descriptions.json. Returns an empty
// Overrides (not an error) when the file is absent — most CLIs don't
// have one and that's the expected steady state. Malformed JSON
// returns a wrapped error so the caller can decide whether to abort.
func Load(cliDir string) (Overrides, error) {
	path := filepath.Join(cliDir, Filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Overrides{}, nil
		}
		return Overrides{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var o Overrides
	if err := json.Unmarshal(data, &o); err != nil {
		return Overrides{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return o, nil
}

// Apply mutates parsed.Resources in place: for each endpoint whose
// computed tool name matches an override key, replace the endpoint's
// Description with the override text. Both the manifest writer and
// the mcp_tools.go template read from the same parsed spec, so this
// single in-place patch flows through to both surfaces.
//
// Returns the override keys that did not match any endpoint. A typo in
// the override file (`tags_creat` instead of `tags_create`, or a stale
// key from before a spec rename) would otherwise silently no-op; the
// caller should surface unmatched keys so the user can debug them.
//
// Tool name computation matches WriteToolsManifest's iteration:
// snake(resource) + "_" + snake(endpoint) for top-level endpoints,
// snake(resource) + "_" + snake(sub) + "_" + snake(endpoint) for
// sub-resource endpoints.
func (o Overrides) Apply(parsed *spec.APISpec) []string {
	if len(o.Descriptions) == 0 || parsed == nil {
		return nil
	}
	matched := make(map[string]bool, len(o.Descriptions))
	for rName, resource := range parsed.Resources {
		for eName, endpoint := range resource.Endpoints {
			name := ToolName(rName, "", eName)
			if override, ok := o.Descriptions[name]; ok {
				endpoint.Description = override
				resource.Endpoints[eName] = endpoint
				matched[name] = true
			}
		}
		for subName, sub := range resource.SubResources {
			for eName, endpoint := range sub.Endpoints {
				name := ToolName(rName, subName, eName)
				if override, ok := o.Descriptions[name]; ok {
					endpoint.Description = override
					sub.Endpoints[eName] = endpoint
					matched[name] = true
				}
			}
			resource.SubResources[subName] = sub
		}
		parsed.Resources[rName] = resource
	}
	var unmatched []string
	for name := range o.Descriptions {
		if !matched[name] {
			unmatched = append(unmatched, name)
		}
	}
	return unmatched
}

// ToolName composes the snake_case MCP tool name from a resource +
// optional sub-resource + endpoint name. Mirrors the iteration in
// pipeline.WriteToolsManifest and the mcp_tools.go template's NewTool
// call. Exported so the override loader and the manifest writer share
// one definition; if the naming scheme ever changes, both follow.
func ToolName(resource, sub, endpoint string) string {
	if sub == "" {
		return naming.Snake(resource) + "_" + naming.Snake(endpoint)
	}
	return naming.Snake(resource) + "_" + naming.Snake(sub) + "_" + naming.Snake(endpoint)
}

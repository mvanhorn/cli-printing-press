package generator

import (
	"sort"
	"strings"
)

// mcpBlockedDestinationFlagSet is the single list of Cobra flag names whose
// values choose a filesystem write destination. The printed cobratree walker
// emits it as blockedDestinationFlags, and recipe intents consult the same
// set so a README recipe cannot reintroduce a destination the walker blocks.
//
// Only unambiguous destination names belong here. Names that are often not
// paths (to, dest, target, file, path) stay available; a novel command
// declares those with mcp:write-flags when they are write sinks.
var mcpBlockedDestinationFlagSet = map[string]bool{
	"audit-dir":    true,
	"db":           true,
	"o":            true,
	"out":          true,
	"out-dir":      true,
	"out-file":     true,
	"output":       true,
	"output-dir":   true,
	"output-file":  true,
	"receipt-file": true,
}

func mcpBlockedDestinationFlagNames() []string {
	return sortedSetNames(mcpBlockedDestinationFlagSet)
}

// MCPBlockedDestinationFlag reports whether name is on the printed cobratree
// write-destination blocklist. Those flags are dropped from MCP schemas
// without an mcp:write-flags annotation. A leading "--" is ignored.
func MCPBlockedDestinationFlag(name string) bool {
	return mcpBlockedDestinationFlagSet[trimFlagName(name)]
}

func trimFlagName(name string) string {
	return strings.TrimLeft(strings.TrimSpace(name), "-")
}

func sortedSetNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

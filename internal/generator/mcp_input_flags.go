package generator

// mcpBlockedInputFlagSet is the single list of Cobra flag names whose values
// are filesystem paths opened for reading. The printed cobratree walker emits
// it as blockedInputFlags, and recipe intents consult the same set.
//
// Only unambiguous read-path names belong here. Names that are often not
// paths (file, path) stay available; a command declares those with mcp:read-flags.
var mcpBlockedInputFlagSet = map[string]bool{
	"cookies-file":        true,
	"input":               true,
	"input-file":          true,
	"notes-file":          true,
	"password-file":       true,
	"playbook-file":       true,
	"playbook-notes-file": true,
}

func mcpBlockedInputFlagNames() []string {
	return sortedSetNames(mcpBlockedInputFlagSet)
}

// MCPBlockedInputFlag reports whether name is on the printed cobratree
// read-path blocklist. Those flags are dropped from MCP schemas without an
// mcp:read-flags annotation. A leading "--" is ignored.
func MCPBlockedInputFlag(name string) bool {
	return mcpBlockedInputFlagSet[trimFlagName(name)]
}

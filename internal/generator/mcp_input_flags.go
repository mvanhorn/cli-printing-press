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

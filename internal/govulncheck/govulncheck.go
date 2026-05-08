package govulncheck

const (
	Name        = "govulncheck"
	ToolVersion = "v1.3.0"
	ToolModule  = "golang.org/x/vuln/cmd/govulncheck@" + ToolVersion
)

// Default mode keeps blocking focused on reachable findings, not dependency
// presence alone.
func GoRunArgs(args ...string) []string {
	goArgs := []string{"run", ToolModule}
	return append(goArgs, args...)
}

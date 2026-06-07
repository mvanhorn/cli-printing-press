package govulncheck

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

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

// ToolchainEnv pins GOTOOLCHAIN to the target module's toolchain so the
// `go run` invocation builds and runs govulncheck under the same Go version
// the scanned code targets. Without it, x/vuln's own go.mod (a lower floor)
// decides the toolchain, and a pre-1.26 govulncheck cannot typecheck
// generated code that uses Go 1.26 features (#2606).
func ToolchainEnv(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil
	}
	if mod.Toolchain != nil && mod.Toolchain.Name != "" {
		return []string{"GOTOOLCHAIN=" + mod.Toolchain.Name}
	}
	if mod.Go != nil && strings.Count(mod.Go.Version, ".") >= 2 {
		return []string{"GOTOOLCHAIN=go" + mod.Go.Version}
	}
	return nil
}

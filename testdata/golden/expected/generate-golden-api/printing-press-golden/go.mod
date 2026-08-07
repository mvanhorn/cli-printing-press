module printing-press-golden-pp-cli

go 1.26.5

toolchain go1.26.5

require (
	github.com/spf13/cobra v1.9.1
	github.com/spf13/pflag v1.0.6
	github.com/pelletier/go-toml/v2 v2.2.4
)
require modernc.org/sqlite v1.37.0
require github.com/mark3labs/mcp-go v0.47.0

// x/sys is a DIRECT dependency for token-bearing bundles: the read-time
// credentials-perms guard's Windows surface (internal/cliutil/creds_perms_windows.go)
// imports golang.org/x/sys/windows. Emitted as a direct require (no // indirect)
// so a freshly generated bundle's go.mod is correct out of the box, WITHOUT a
// manual `go mod tidy`. The version matches the transitive floor below so a
// single x/sys version is pinned. NOTE (go mod tidy GOOS caveat): the import is
// behind `//go:build windows`, so running `go mod tidy` under GOOS=linux/darwin
// re-marks this // indirect (that GOOS compiles no file that imports it); under
// GOOS=windows it stays direct. That is tolerated churn, NOT a bug to "fix".
require golang.org/x/sys v0.46.0

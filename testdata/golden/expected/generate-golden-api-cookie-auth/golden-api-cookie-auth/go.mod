module golden-api-cookie-auth-pp-cli

go 1.26.9

toolchain go1.26.9

require (
	github.com/gorilla/websocket v1.5.3
	github.com/spf13/cobra v1.9.1
	github.com/spf13/pflag v1.0.6
	github.com/pelletier/go-toml/v2 v2.2.4
)
require modernc.org/sqlite v1.37.0
require github.com/mark3labs/mcp-go v0.57.0

// x/sys is a DIRECT dependency for token-bearing bundles. The read-time
// credentials guard imports golang.org/x/sys/unix on non-Windows
// (creds_perms_unix.go; darwin's syscall package has no Openat) and
// golang.org/x/sys/windows on Windows. Emitted as a direct require (no
// // indirect) so a freshly generated bundle's go.mod is correct out of the
// box, WITHOUT a manual `go mod tidy`. The version matches the transitive
// floor below so a single x/sys version is pinned. `go mod tidy` on
// linux/darwin keeps the require direct because the unix import is compiled
// there; tidy on Windows keeps it direct because of the windows import.
require golang.org/x/sys v0.46.0

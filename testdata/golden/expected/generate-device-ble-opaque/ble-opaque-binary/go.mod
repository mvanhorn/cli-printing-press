module ble-opaque-binary-pp-cli

go 1.26.5

toolchain go1.26.5

require (
	github.com/mark3labs/mcp-go v0.47.0
	github.com/spf13/cobra v1.9.1
	tinygo.org/x/bluetooth v0.15.0
)

// Floor the transitively-pulled x/sys (via tinygo.org/x/bluetooth) above the
// vulnerable v0.31.0; tidy drops it for CLIs that pull no x/sys at all.
require golang.org/x/sys v0.46.0 // indirect

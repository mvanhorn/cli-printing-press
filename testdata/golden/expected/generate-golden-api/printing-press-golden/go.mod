module printing-press-golden-pp-cli

go 1.26

toolchain go1.26.4

require (
	github.com/spf13/cobra v1.9.1
	github.com/pelletier/go-toml/v2 v2.2.4
)
require modernc.org/sqlite v1.37.0
require github.com/mark3labs/mcp-go v0.47.0

// Floor the transitively-pulled x/sys (via modernc.org/sqlite) above the
// vulnerable v0.31.0; tidy drops it for CLIs that pull no x/sys at all.
require golang.org/x/sys v0.46.0 // indirect

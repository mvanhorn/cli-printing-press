module golden-api-cookie-auth-pp-cli

go 1.26.4

toolchain go1.26.4

require (
	github.com/gorilla/websocket v1.5.3
	github.com/spf13/cobra v1.9.1
)
require modernc.org/sqlite v1.37.0
require github.com/mark3labs/mcp-go v0.47.0

// Floor x/sys above the vulnerable v0.31.0. It is pulled only transitively
// (modernc.org/sqlite, golang.org/x/net, ...), so MVS needs this explicit
// floor; tidy drops it for CLIs that pull no x/sys at all.
require golang.org/x/sys v0.46.0 // indirect

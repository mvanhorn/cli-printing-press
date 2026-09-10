module browser-http-transport-pp-cli

go 1.26.6

toolchain go1.26.6

require (
	github.com/enetx/surf v1.0.199
	github.com/spf13/cobra v1.9.1
	github.com/spf13/pflag v1.0.6
	github.com/pelletier/go-toml/v2 v2.2.4
)
require modernc.org/sqlite v1.37.0
require github.com/mark3labs/mcp-go v0.57.0

// x/sys is a DIRECT dependency even without auth: filelock_windows.go
// imports golang.org/x/sys/windows. Emitted as a direct require (no
// // indirect) so a Windows cross-compile of a freshly generated bundle
// succeeds without a manual `go mod tidy`. The version matches the
// transitive floor. NOTE (go mod tidy GOOS caveat): the import is behind
// `//go:build windows`, so tidy under GOOS=linux/darwin re-marks this
// // indirect; under GOOS=windows it stays direct.
require golang.org/x/sys v0.46.0

// Floor the HTTP/3 transitive deps pulled in only via github.com/enetx/surf
// above their vulnerable versions (osv flags module presence; govulncheck
// reachability = 0 for these REST CLIs). Emitted only when the surf transport
// is present, so MVS keeps the floor; tidy drops it for CLIs without surf.
require golang.org/x/crypto v0.56.0 // indirect
require github.com/quic-go/quic-go v0.60.0 // indirect
// enetx/http2 v1.0.25+ reads http.Server.DisableClientPriority under
// //go:build go1.27. Surf still floors http at v1.0.28, which lacks that
// field; pin v1.0.29 so printed CLIs `go install` on Go 1.27. Keep in
// sync with safeEnetxHTTPVersion. regen-merge: fresh wins on this path,
// so a published v1.0.28 cannot re-enter.
require github.com/enetx/http v1.0.29 // indirect

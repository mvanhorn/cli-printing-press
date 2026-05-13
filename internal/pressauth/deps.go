package pressauth

// Anchor imports for the U3 (chromedp) dependency.
//
// chromedp is pinned in go.mod ahead of the unit that uses it so
// `go mod tidy` between unit landings doesn't drop the version pin and
// force a re-resolve. The blank import costs nothing at runtime — the
// linker drops any package that isn't referenced — but it keeps the
// dependency declaration live for go list / go.sum / vendor flows.
//
// When U3 lands and imports chromedp for real (subcommand body in
// login.go), delete this file entirely.

import (
	_ "github.com/chromedp/chromedp"
)

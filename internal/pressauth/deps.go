package pressauth

// Anchor imports for U2 (keychain) and U3 (chromedp) dependencies.
//
// These packages are pinned in go.mod ahead of the units that use them so
// that `go mod tidy` between unit landings doesn't drop the version pins
// and force a re-resolve. The blank imports cost nothing at runtime — the
// linker drops any package that isn't referenced — but they keep the
// dependency declarations live for go list / go.sum / vendor flows.
//
// Each blank import is replaced by a real import in the corresponding
// unit:
//   * U2  -> github.com/keybase/go-keychain (build-tagged darwin)
//   * U3  -> github.com/chromedp/chromedp
//
// When that unit lands, delete the matching blank import here.

import (
	_ "github.com/chromedp/chromedp"
	_ "github.com/keybase/go-keychain"
)

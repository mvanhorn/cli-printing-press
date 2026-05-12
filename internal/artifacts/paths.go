package artifacts

import (
	"path/filepath"
	"strings"
)

const (
	CLIDirPlaceholder   = "<cli-dir>"
	RunStatePlaceholder = "<runstate>"
)

// RedactCLIDirRoot preserves the slug (basename) so existing consumers
// that call filepath.Base still get a useful display name.
func RedactCLIDirRoot(cliDir string) string {
	if cliDir == "" {
		return ""
	}
	return filepath.Join(CLIDirPlaceholder, filepath.Base(cliDir))
}

// RedactPathUnderCLI strips $HOME prefixes before the value reaches a
// committed artifact, falling back to <runstate>/<basename> when p
// lives outside cliDir.
func RedactPathUnderCLI(cliDir, p string) string {
	if p == "" {
		return ""
	}
	if cliDir != "" {
		rel, err := filepath.Rel(cliDir, p)
		if err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return filepath.Join(CLIDirPlaceholder, rel)
		}
	}
	return filepath.Join(RunStatePlaceholder, filepath.Base(p))
}

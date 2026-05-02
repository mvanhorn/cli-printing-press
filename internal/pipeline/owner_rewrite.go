package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// RewriteOwner replaces oldOwner with newOwner in copyright headers across
// all rewriteExtensions files under dir. No-op when the owners are equal or
// either is empty.
//
// This exists for regen-merge --apply: fresh-generated trees carry whatever
// owner the runner's git config produced, but the destination tree may have
// a different attribution (e.g. flightgoat is matt-van-horn while a sweep
// run by trevin-chow would otherwise rewrite all 100 copied files to the
// wrong author). Mirrors RewriteModulePath: same dir-walk, same extension
// list, same idempotent semantics.
//
// Only the owner token in the framework-emitted copyright header is replaced
// (anchored on `// Copyright YYYY ` prefix and a trailing literal `.`); other
// prose mentions of the old owner are intentionally left alone to avoid
// corrupting hand-written content (issue trackers, attribution lists, etc.).
func RewriteOwner(dir, oldOwner, newOwner string) error {
	if oldOwner == "" || newOwner == "" || oldOwner == newOwner {
		return nil
	}

	// Bake the literal oldOwner into the pattern so the match itself enforces
	// the equality guard — no separate capture-and-compare step needed. The
	// $1/$2 backreferences preserve the prefix and trailing period verbatim.
	re := regexp.MustCompile(`(?m)^(//\s*Copyright\s+\d+\s+)` + regexp.QuoteMeta(oldOwner) + `(\.)`)
	replacement := []byte("${1}" + newOwner + "${2}")

	return filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !hasRewriteExtension(path) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		updated := re.ReplaceAll(content, replacement)
		if len(updated) == len(content) && string(updated) == string(content) {
			return nil
		}
		return os.WriteFile(path, updated, 0o644)
	})
}

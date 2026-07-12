package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
//
// Both header forms are handled: the legacy `Copyright YYYY <slug>.` and the
// current `Copyright YYYY <name> and contributors.`. The optional
// " and contributors" suffix is preserved verbatim — only the creator token
// is swapped.
func RewriteOwner(dir, oldOwner, newOwner string) error {
	if oldOwner == "" || newOwner == "" || oldOwner == newOwner {
		return nil
	}

	// Bake the literal oldOwner into the pattern so the match itself enforces
	// the equality guard — no separate capture-and-compare step needed. The
	// $1/$2/$3 backreferences preserve the prefix, the optional
	// " and contributors" suffix, and the trailing period verbatim. newOwner is
	// a display name now (not a sanitized slug), so escape any `$` in it to
	// `$$` — otherwise ReplaceAll would interpret `$1`/`${...}` inside the name
	// as backreferences and corrupt every rewritten header.
	escapedNew := strings.ReplaceAll(newOwner, "$", "$$")
	re := regexp.MustCompile(`(?m)^(//\s*Copyright\s+\d+\s+)` + regexp.QuoteMeta(oldOwner) + `( and contributors)?(\.)`)
	replacement := []byte("${1}" + escapedNew + "${2}${3}")

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

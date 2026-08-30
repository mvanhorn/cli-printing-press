package regenmerge

import (
	"path/filepath"
	"strings"
)

// generatedInputRefreshPaths are framework files that re-render from the
// current spec or research on every print. Same-spec --force used to keep
// a still-compiling older copy whenever value-drift fired, so a research-only
// or learn-block edit never replaced which.go, tools.go, or learn_init.go.
// Fresh wins while the generated marker is present; a markerless replacement
// stays on the ordinary authored path.
var generatedInputRefreshPaths = map[string]struct{}{
	"internal/cli/which.go":           {},
	"internal/cli/which_test.go":      {},
	"internal/cli/learn_init.go":      {},
	"internal/cli/learn_init_test.go": {},
	"internal/mcp/tools.go":           {},
}

func isGeneratedInputRefreshPath(rel string) bool {
	_, ok := generatedInputRefreshPaths[filepath.ToSlash(rel)]
	return ok
}

func isCLIRootGo(filename string) bool {
	rel := filepath.ToSlash(filename)
	return rel == "internal/cli/root.go" || strings.HasSuffix(rel, "/internal/cli/root.go")
}

// filterValueDriftAgainstBase drops decls whose published text still matches
// the original emission. Those differences are template/version rewrites, not
// hand-edits, so reprint can take fresh without inventing a new verdict.
func filterValueDriftAgainstBase(drift *ValueDrift, pubPath, basePath string) *ValueDrift {
	if drift == nil || basePath == "" {
		return drift
	}
	pubTexts := canonicalDeclTexts(pubPath)
	baseTexts := canonicalDeclTexts(basePath)
	if pubTexts == nil || baseTexts == nil {
		return drift
	}
	filtered := make(map[string]ValueDriftDelta, len(drift.Decls))
	for name, delta := range drift.Decls {
		if pubTexts[name] != "" && pubTexts[name] == baseTexts[name] {
			continue
		}
		filtered[name] = delta
	}
	if len(filtered) == 0 {
		return nil
	}
	return &ValueDrift{Decls: filtered}
}

// filterBodyDriftAgainstBase is the call-target counterpart of
// filterValueDriftAgainstBase.
func filterBodyDriftAgainstBase(drift *BodyDrift, pubPath, basePath string) *BodyDrift {
	if drift == nil || basePath == "" {
		return drift
	}
	pubTexts := canonicalDeclTexts(pubPath)
	baseTexts := canonicalDeclTexts(basePath)
	if pubTexts == nil || baseTexts == nil {
		return drift
	}
	filtered := make(map[string][]string, len(drift.Functions))
	for name, calls := range drift.Functions {
		if pubTexts[name] != "" && pubTexts[name] == baseTexts[name] {
			continue
		}
		filtered[name] = calls
	}
	if len(filtered) == 0 {
		return nil
	}
	return &BodyDrift{Functions: filtered}
}

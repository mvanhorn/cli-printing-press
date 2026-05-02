package regenmerge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Owner resolution mirrors generator/plan_generate.go's
// resolveOwnerForExisting precedence (manifest > copyright header > empty),
// but is duplicated here because the test files in package pipeline import
// generator (internal/pipeline/contracts_test.go); having the production
// generator package also import pipeline would create a build cycle when
// compiling pipeline's test binary. The two implementations should stay
// aligned; if test deps are reshaped to break the cycle, replace this with
// a shared helper.
//
// Used during --apply to discover (a) the destination tree's owner and (b)
// the fresh tree's owner, so RewriteOwner can replace fresh's attribution
// with the destination's across every file copied from fresh.

var copyrightOwnerRe = regexp.MustCompile(`(?m)^//\s*Copyright\s+\d+\s+([A-Za-z0-9_-]+)\.`)

// resolveOwnerForTree returns the owner attribution for a CLI tree at dir.
// Tiers (highest first):
//
//  1. .printing-press.json's `owner` field, if present and non-empty
//  2. parsed `// Copyright YYYY <owner>` line in internal/cli/root.go
//  3. "" (empty) — caller decides whether to skip rewrite or fall back to
//     a runner-controlled value
//
// Returns "" when no owner can be determined. Callers in --apply skip the
// rewrite step on empty so a missing-manifest tree doesn't corrupt headers.
func resolveOwnerForTree(dir string) string {
	if owner := readManifestOwner(dir); owner != "" {
		return owner
	}
	return parseCopyrightOwner(dir)
}

func readManifestOwner(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".printing-press.json"))
	if err != nil {
		return ""
	}
	var m struct {
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return strings.TrimSpace(m.Owner)
}

func parseCopyrightOwner(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "root.go"))
	if err != nil {
		return ""
	}
	if m := copyrightOwnerRe.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

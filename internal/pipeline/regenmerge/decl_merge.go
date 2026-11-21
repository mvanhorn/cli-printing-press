package regenmerge

import (
	"fmt"
	"go/token"
	"slices"
	"sort"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// bothSidesUnavailableNote is recorded when Classify has no base tree.
// Without that original emission, a published/fresh difference inside a
// shared declaration cannot be split into a hand edit versus a template change.
const bothSidesUnavailableNote = "no base tree: a shared declaration changed in the published tree cannot be distinguished from a template change"

const declSnippetLimit = 400

// bothSidesFuncPlan is the resolution of one function that differs from the
// base emission on both the published side and the fresh side.
type bothSidesFuncPlan struct {
	name       string
	merged     string
	conflict   DeclConflict
	isConflict bool
}

// bothSidesConflictError means at least one shared function changed on both
// sides and the line merge did not apply. A written destination keeps the
// fresh body for those functions.
type bothSidesConflictError struct {
	msg string
}

func (e *bothSidesConflictError) Error() string {
	if e == nil || e.msg == "" {
		return "shared functions changed on both sides and did not merge cleanly\npass regen-merge --allow-decl-conflicts to keep the fresh body for those functions"
	}
	return e.msg
}

// bothSidesUnsafeError means a both-sides function cannot be installed, and
// keeping the published file would put that function's published body back.
// --allow-decl-conflicts does not bypass it: the flag keeps the fresh body,
// which this file cannot do.
type bothSidesUnsafeError struct {
	msg string
}

func (e *bothSidesUnsafeError) Error() string {
	if e == nil || e.msg == "" {
		return "refusing to apply: a shared function changed on both sides and the fresh body cannot be kept"
	}
	return e.msg
}

func planBothSidesFuncs(pubPath, freshPath, basePath string) []bothSidesFuncPlan {
	if basePath == "" {
		return nil
	}
	fromPub := handEditedDeclNames(pubPath, freshPath, basePath)
	if len(fromPub) == 0 {
		return nil
	}
	bodyPub := bodyDeclTexts(pubPath)
	bodyFresh := bodyDeclTexts(freshPath)
	bodyBase := bodyDeclTexts(basePath)
	if bodyPub == nil || bodyFresh == nil || bodyBase == nil {
		return nil
	}
	var names []string
	for name := range fromPub {
		// Grouped const/var specs already merge per member. Whole-function
		// replacement is the path that drops an unrelated template edit.
		if !isOverlayFuncDeclName(name) {
			continue
		}
		baseBody := bodyBase[name]
		pubBody := bodyPub[name]
		freshBody := bodyFresh[name]
		if baseBody == "" || pubBody == "" || freshBody == "" {
			continue
		}
		if pubBody == baseBody || freshBody == baseBody || pubBody == freshBody {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	dotImport := fileUsesDotImport(pubPath) || fileUsesDotImport(freshPath) || fileUsesDotImport(basePath)
	pubAlias := loadImportAliases(pubPath)
	freshAlias := loadImportAliases(freshPath)
	baseAlias := loadImportAliases(basePath)
	plans := make([]bothSidesFuncPlan, 0, len(names))
	for _, name := range names {
		baseBody := bodyBase[name]
		pubBody := bodyPub[name]
		freshBody := bodyFresh[name]
		var merged string
		var hunk mergeHunk
		ok := !dotImport
		if ok {
			var qerr error
			baseBody, qerr = qualifyFuncSelectors(baseBody, baseAlias)
			if qerr == nil {
				pubBody, qerr = qualifyFuncSelectors(pubBody, pubAlias)
			}
			if qerr == nil {
				freshBody, qerr = qualifyFuncSelectors(freshBody, freshAlias)
			}
			if qerr != nil {
				ok = false
			}
		}
		if ok {
			merged, hunk, ok = merge3(baseBody, pubBody, freshBody)
		}
		plan := bothSidesFuncPlan{name: name}
		if ok && !acceptMergedFunc(name, merged) {
			ok = false
		}
		if ok {
			plan.merged = merged
		}
		if !ok {
			base, fresh, published := hunk.base, hunk.theirs, hunk.ours
			if strings.TrimSpace(base) == "" && strings.TrimSpace(fresh) == "" && strings.TrimSpace(published) == "" {
				base, fresh, published = baseBody, freshBody, pubBody
			}
			plan.isConflict = true
			plan.conflict = DeclConflict{
				Name:      name,
				Base:      clipDeclSnippet(base),
				Fresh:     clipDeclSnippet(fresh),
				Published: clipDeclSnippet(published),
			}
		}
		plans = append(plans, plan)
	}
	return plans
}

func bothSidesFromPlans(plans []bothSidesFuncPlan) *BothSides {
	if len(plans) == 0 {
		return nil
	}
	out := &BothSides{}
	for _, plan := range plans {
		if plan.isConflict {
			out.Conflicts = append(out.Conflicts, plan.conflict)
			continue
		}
		out.Merged = append(out.Merged, plan.name)
	}
	return out
}

func declConflictRefusal(files []FileClassification) error {
	var b strings.Builder
	n := 0
	for _, fc := range files {
		if fc.BothSides == nil {
			continue
		}
		for _, c := range fc.BothSides.Conflicts {
			if n == 0 {
				b.WriteString("refusing to apply: shared functions changed on both sides and did not merge cleanly:\n")
			}
			n++
			fmt.Fprintf(&b, "  %s: %s\n    base: %s\n    fresh: %s\n    published: %s\n", fc.Path, c.Name, c.Base, c.Fresh, c.Published)
		}
	}
	if n == 0 {
		return nil
	}
	b.WriteString("pass regen-merge --allow-decl-conflicts to keep the fresh body for those functions")
	return &bothSidesConflictError{msg: b.String()}
}

func conflictError(conflicts []DeclConflict) error {
	if len(conflicts) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("shared functions changed on both sides and did not merge cleanly:")
	for _, c := range conflicts {
		fmt.Fprintf(&b, "\n  %s\n    base: %s\n    fresh: %s\n    published: %s", c.Name, c.Base, c.Fresh, c.Published)
	}
	b.WriteString("\npass regen-merge --allow-decl-conflicts to keep the fresh body for those functions")
	return &bothSidesConflictError{msg: b.String()}
}

func acceptMergedFunc(name, text string) bool {
	decl, err := mergedDeclFromText(text)
	return err == nil && overlayDeclName(decl) == name
}

func mergedDeclFromText(text string) (dst.Decl, error) {
	src := "package mergeddecl\n" + text + "\n"
	file, err := decorator.Parse(src)
	if err != nil {
		return nil, err
	}
	var decls []dst.Decl
	for _, d := range file.Decls {
		if gd, ok := d.(*dst.GenDecl); ok && gd.Tok == token.IMPORT {
			continue
		}
		decls = append(decls, d)
	}
	if len(decls) != 1 {
		return nil, fmt.Errorf("merged declaration parsed as %d declarations", len(decls))
	}
	return decls[0], nil
}

func clipDeclSnippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= declSnippetLimit {
		return s
	}
	return s[:declSnippetLimit] + "..."
}

type mergeHunk struct {
	base, ours, theirs string
}

type lineEdit struct {
	baseLo, baseHi int
	lines          []string
}

// merge3 line-merges ours (published) and theirs (fresh) against base.
// A clean result has every edit from both sides. Overlapping unequal edits
// return ok false and the conflicting region.
func merge3(base, ours, theirs string) (string, mergeHunk, bool) {
	baseLines := splitKeepLines(base)
	oursLines := splitKeepLines(ours)
	theirsLines := splitKeepLines(theirs)
	oEdits := lineEdits(baseLines, oursLines)
	tEdits := lineEdits(baseLines, theirsLines)
	var out []string
	oi, ti := 0, 0
	i := 0
	for i < len(baseLines) || oi < len(oEdits) || ti < len(tEdits) {
		oStart := len(baseLines)
		if oi < len(oEdits) {
			oStart = oEdits[oi].baseLo
		}
		tStart := len(baseLines)
		if ti < len(tEdits) {
			tStart = tEdits[ti].baseLo
		}
		if i < oStart && i < tStart {
			end := min(oStart, tStart, len(baseLines))
			out = append(out, baseLines[i:end]...)
			i = end
			continue
		}
		if oi < len(oEdits) && ti < len(tEdits) && oStart == i && tStart == i {
			oe, te := oEdits[oi], tEdits[ti]
			if oe.baseHi == te.baseHi && linesEqual(oe.lines, te.lines) {
				out = append(out, oe.lines...)
				i = oe.baseHi
				oi++
				ti++
				continue
			}
			return "", conflictHunk(baseLines, oe, te), false
		}
		if oi < len(oEdits) && oStart == i && (ti >= len(tEdits) || tEdits[ti].baseLo >= oEdits[oi].baseHi) {
			oe := oEdits[oi]
			out = append(out, oe.lines...)
			i = oe.baseHi
			oi++
			continue
		}
		if ti < len(tEdits) && tStart == i && (oi >= len(oEdits) || oEdits[oi].baseLo >= tEdits[ti].baseHi) {
			te := tEdits[ti]
			out = append(out, te.lines...)
			i = te.baseHi
			ti++
			continue
		}
		if oi < len(oEdits) && ti < len(tEdits) {
			return "", conflictHunk(baseLines, oEdits[oi], tEdits[ti]), false
		}
		return "", mergeHunk{}, false
	}
	return strings.Join(out, "\n"), mergeHunk{}, true
}

func conflictHunk(base []string, ours, theirs lineEdit) mergeHunk {
	lo := max(min(ours.baseLo, theirs.baseLo), 0)
	hi := min(max(ours.baseHi, theirs.baseHi), len(base))
	if lo > hi {
		lo = hi
	}
	return mergeHunk{
		base:   strings.Join(base[lo:hi], "\n"),
		ours:   strings.Join(ours.lines, "\n"),
		theirs: strings.Join(theirs.lines, "\n"),
	}
}

func splitKeepLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func linesEqual(a, b []string) bool {
	return slices.Equal(a, b)
}

func lineEdits(base, edited []string) []lineEdit {
	n, m := len(base), len(edited)
	dp := make([][]int, n+1)
	for i := range dp {
		row := make([]int, m+1)
		dp[i] = row
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if base[i] == edited[j] {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
				continue
			}
			dp[i][j] = dp[i][j+1]
		}
	}
	var edits []lineEdit
	var cur *lineEdit
	flush := func() {
		if cur == nil {
			return
		}
		edits = append(edits, *cur)
		cur = nil
	}
	i, j := 0, 0
	for i < n || j < m {
		if i < n && j < m && base[i] == edited[j] && dp[i][j] == dp[i+1][j+1]+1 {
			flush()
			i++
			j++
			continue
		}
		if cur == nil {
			cur = &lineEdit{baseLo: i, baseHi: i}
		}
		if i < n && (j >= m || dp[i+1][j] >= dp[i][j+1]) {
			i++
			cur.baseHi = i
			continue
		}
		if j < m {
			cur.lines = append(cur.lines, edited[j])
			j++
			continue
		}
		break
	}
	flush()
	return edits
}

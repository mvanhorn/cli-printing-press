package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
)

type novelFeatureDocGroup struct {
	Name     string
	Features []NovelFeature
}

type syncedArtifact struct {
	Path   string
	Detail string
}

// SyncCLINarrativeDocs rewrites generated README/SKILL narrative blocks from
// the current research.json narrative. Empty narrative fields remove only the
// narrative-owned subsections; generic fallback prose is left intact.
func SyncCLINarrativeDocs(dir, apiName string, narrative *ReadmeNarrative) ([]syncedArtifact, error) {
	if narrative == nil {
		return nil, nil
	}

	var synced []syncedArtifact
	if strings.TrimSpace(narrative.Headline) != "" || strings.TrimSpace(narrative.ValueProp) != "" {
		changed, err := syncReadmeIntroNarrative(filepath.Join(dir, "README.md"), narrative)
		if err != nil {
			return nil, err
		}
		if changed {
			synced = append(synced, syncedArtifact{Path: "README.md", Detail: "Value Proposition"})
		}
	}

	if strings.TrimSpace(narrative.ValueProp) != "" {
		changed, err := syncSkillValueProp(filepath.Join(dir, "SKILL.md"), narrative.ValueProp)
		if err != nil {
			return nil, err
		}
		if changed {
			synced = append(synced, syncedArtifact{Path: "SKILL.md", Detail: "Value Proposition"})
		}
	}

	if len(narrative.QuickStart) > 0 {
		changed, err := syncMarkdownFeatureSection(
			filepath.Join(dir, "README.md"),
			"## Quick Start",
			renderQuickStartSection(narrative.QuickStart),
			[]string{"## Unique Features", "## Usage"},
		)
		if err != nil {
			return nil, err
		}
		if changed {
			synced = append(synced, syncedArtifact{Path: "README.md", Detail: "Quick Start"})
		}
	}

	if strings.TrimSpace(narrative.AuthNarrative) != "" {
		changed, err := syncReadmeAuthNarrative(filepath.Join(dir, "README.md"), narrative.AuthNarrative)
		if err != nil {
			return nil, err
		}
		if changed {
			synced = append(synced, syncedArtifact{Path: "README.md", Detail: "Authentication"})
		}

		changed, err = syncMarkdownFeatureSection(
			filepath.Join(dir, "SKILL.md"),
			"## Auth Setup",
			renderSkillAuthSetupSection(apiName, narrative.AuthNarrative),
			[]string{"## Agent Mode", "## Command Reference"},
		)
		if err != nil {
			return nil, err
		}
		if changed {
			synced = append(synced, syncedArtifact{Path: "SKILL.md", Detail: "Auth Setup"})
		}
	}

	changed, err := syncReadmeTroubleshoots(filepath.Join(dir, "README.md"), narrative.Troubleshoots)
	if err != nil {
		return nil, err
	}
	if changed {
		synced = append(synced, syncedArtifact{Path: "README.md", Detail: "Troubleshooting"})
	}

	changed, err = syncMarkdownFeatureSection(
		filepath.Join(dir, "SKILL.md"),
		"## Recipes",
		renderRecipesSection(narrative.Recipes),
		[]string{"## Auth Setup", "## Agent Mode", "## Command Reference"},
	)
	if err != nil {
		return nil, err
	}
	if changed {
		synced = append(synced, syncedArtifact{Path: "SKILL.md", Detail: "Recipes"})
	}

	return synced, nil
}

// SyncCLITranscendenceDocs rewrites generated README/SKILL transcendence
// blocks from dogfood-verified features. Empty verified sets remove the blocks.
func SyncCLITranscendenceDocs(dir string, features []NovelFeature) ([]syncedArtifact, error) {
	var synced []syncedArtifact
	changed, err := syncMarkdownFeatureSection(
		filepath.Join(dir, "README.md"),
		"## Unique Features",
		renderNovelFeatureDocSection("## Unique Features", features),
		[]string{"## Usage"},
	)
	if err != nil {
		return nil, err
	}
	if changed {
		synced = append(synced, syncedArtifact{Path: "README.md", Detail: "Unique Features"})
	}

	changed, err = syncMarkdownFeatureSection(
		filepath.Join(dir, "SKILL.md"),
		"## Unique Capabilities",
		renderNovelFeatureDocSection("## Unique Capabilities", features),
		[]string{"## HTTP Transport", "## Discovery Signals", "## Command Reference", "## Auth Setup"},
	)
	if err != nil {
		return nil, err
	}
	if changed {
		synced = append(synced, syncedArtifact{Path: "SKILL.md", Detail: "Unique Capabilities"})
	}
	return synced, nil
}

func syncReadmeAuthNarrative(path, authNarrative string) (bool, error) {
	return syncMarkdownFile(path, func(content string) string {
		heading := "## Authentication"
		staleHeading := ""
		if findMarkdownHeading(content, "## Optional: API Key") >= 0 {
			heading = "## Optional: API Key"
			staleHeading = "## Authentication"
		}
		content = replaceMarkdownSection(content, heading, renderAuthNarrativeSection(heading, authNarrative), []string{"## Quick Start"})
		if staleHeading != "" {
			content = replaceMarkdownSection(content, staleHeading, "", nil)
		}
		return content
	})
}

func syncReadmeIntroNarrative(path string, narrative *ReadmeNarrative) (bool, error) {
	return syncMarkdownFile(path, func(content string) string {
		return replaceReadmeIntroNarrative(content, narrative)
	})
}

func syncSkillValueProp(path, valueProp string) (bool, error) {
	valueProp = strings.TrimSpace(valueProp)
	if valueProp == "" {
		return false, nil
	}
	return syncMarkdownFile(path, func(content string) string {
		markerIdx := strings.Index(content, generator.SkillInstallSectionEndSubstr)
		if markerIdx < 0 {
			return content
		}
		start := markerIdx + len(generator.SkillInstallSectionEndSubstr)
		if next := strings.IndexByte(content[start:], '\n'); next >= 0 {
			start += next + 1
		}
		sectionEnd := findNextLevelTwoHeading(content, start)
		return joinMarkdownParts(content[:start], valueProp, content[sectionEnd:])
	})
}

func syncReadmeTroubleshoots(path string, troubleshoots []TroubleshootTip) (bool, error) {
	return syncMarkdownFile(path, func(content string) string {
		return replaceMarkdownSubsection(content, "## Troubleshooting", "### API-specific", renderTroubleshootSubsection(troubleshoots))
	})
}

func replaceReadmeIntroNarrative(content string, narrative *ReadmeNarrative) string {
	title := firstMarkdownHeading(content, 0, len(content), 1, 1)
	if title.Start < 0 {
		return content
	}
	start := title.Start + len(strings.SplitN(content[title.Start:], "\n", 2)[0])
	if start < len(content) && content[start] == '\n' {
		start++
	}
	end := findReadmeIntroEnd(content, start)
	replacement := renderReadmeIntroNarrative(narrative, existingReadmeIntroLead(content[start:end]))
	if strings.TrimSpace(replacement) == "" {
		return content
	}
	return joinMarkdownParts(content[:start], replacement, content[end:])
}

func renderReadmeIntroNarrative(narrative *ReadmeNarrative, fallbackLead string) string {
	if narrative == nil {
		return ""
	}
	var parts []string
	if headline := strings.TrimSpace(narrative.Headline); headline != "" {
		parts = append(parts, "**"+headline+"**")
	} else if fallbackLead = strings.TrimSpace(fallbackLead); fallbackLead != "" {
		parts = append(parts, fallbackLead)
	}
	if valueProp := strings.TrimSpace(narrative.ValueProp); valueProp != "" {
		parts = append(parts, valueProp)
	}
	return strings.Join(parts, "\n\n")
}

func existingReadmeIntroLead(intro string) string {
	for paragraph := range strings.SplitSeq(strings.TrimSpace(intro), "\n\n") {
		if paragraph = strings.TrimSpace(paragraph); paragraph != "" {
			return paragraph
		}
	}
	return ""
}

func findReadmeIntroEnd(content string, start int) int {
	end := len(content)
	for _, prefix := range []string{"Learn more at ", "Printed by "} {
		if idx := findLineWithPrefix(content, start, prefix); idx >= 0 && idx < end {
			end = idx
		}
	}
	if install := findMarkdownHeadingInRange(content, "## Install", start, len(content)); install >= 0 && install < end {
		end = install
	}
	if heading := firstMarkdownHeading(content, start, end, 2, 2); heading.Start >= 0 && heading.Start < end {
		end = heading.Start
	}
	return end
}

func findLineWithPrefix(content string, start int, prefix string) int {
	if start < 0 {
		start = 0
	}
	if start > len(content) {
		return -1
	}
	for lineStart := start; lineStart < len(content); {
		lineEnd := strings.IndexByte(content[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(content)
		} else {
			lineEnd += lineStart
		}
		if strings.HasPrefix(content[lineStart:lineEnd], prefix) {
			return lineStart
		}
		if lineEnd == len(content) {
			break
		}
		lineStart = lineEnd + 1
	}
	return -1
}

func renderQuickStartSection(steps []QuickStartStep) string {
	if len(steps) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Quick Start\n\n```bash\n")
	for _, step := range steps {
		if step.Comment != "" {
			b.WriteString("# ")
			b.WriteString(step.Comment)
			b.WriteString("\n")
		}
		b.WriteString(step.Command)
		b.WriteString("\n\n")
	}
	b.WriteString("```")
	return strings.TrimRight(b.String(), "\n")
}

func renderAuthNarrativeSection(heading, authNarrative string) string {
	authNarrative = strings.TrimSpace(authNarrative)
	if authNarrative == "" {
		return ""
	}
	if heading == "## Optional: API Key" {
		return heading + "\n\n**All core commands work without setup.** The API key below is only needed to unlock additional features.\n\n" + authNarrative
	}
	return heading + "\n\n" + authNarrative
}

func renderSkillAuthSetupSection(apiName, authNarrative string) string {
	authNarrative = strings.TrimSpace(authNarrative)
	if authNarrative == "" {
		return ""
	}
	cliName := strings.TrimSpace(apiName)
	if cliName == "" {
		cliName = "<cli>"
	} else if !strings.HasSuffix(cliName, "-pp-cli") {
		cliName += "-pp-cli"
	}
	if authNarrativeMentionsDoctor(authNarrative, cliName) {
		return "## Auth Setup\n\n" + authNarrative
	}
	return "## Auth Setup\n\n" + authNarrative + "\n\nRun `" + cliName + " doctor` to verify setup."
}

func authNarrativeMentionsDoctor(authNarrative, cliName string) bool {
	lower := strings.ToLower(authNarrative)
	cliName = strings.ToLower(strings.TrimSpace(cliName))
	for _, candidate := range []string{
		"`" + cliName + " doctor`",
		"`<cli> doctor`",
	} {
		if strings.Contains(lower, candidate) {
			return true
		}
	}
	return false
}

func renderTroubleshootSubsection(troubleshoots []TroubleshootTip) string {
	if len(troubleshoots) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("### API-specific\n")
	for _, tip := range troubleshoots {
		b.WriteString("- **")
		b.WriteString(tip.Symptom)
		b.WriteString("** \u2014 ")
		b.WriteString(tip.Fix)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderRecipesSection(recipes []Recipe) string {
	if len(recipes) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Recipes\n")
	for _, recipe := range recipes {
		b.WriteString("\n### ")
		b.WriteString(recipe.Title)
		b.WriteString("\n\n```bash\n")
		b.WriteString(recipe.Command)
		b.WriteString("\n```")
		if recipe.Explanation != "" {
			b.WriteString("\n\n")
			b.WriteString(recipe.Explanation)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// SyncCLIRootHighlights rewrites root --help Highlights from dogfood-verified
// features. It edits only the generated Long-string section so hand-authored
// command registration changes in root.go are left intact.
func SyncCLIRootHighlights(dir string, features []NovelFeature) (bool, error) {
	path := filepath.Join(dir, "internal", "cli", "root.go")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	updated := replaceRootLongHighlights(string(data), features)
	if updated == string(data) {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

func syncMarkdownFeatureSection(path, heading, replacement string, insertBefore []string) (bool, error) {
	return syncMarkdownFile(path, func(content string) string {
		return replaceMarkdownSection(content, heading, replacement, insertBefore)
	})
}

func syncMarkdownFile(path string, rewrite func(string) string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", path, err)
	}

	content := string(data)
	updated := rewrite(content)
	if updated == content {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

func renderNovelFeatureDocSection(heading string, features []NovelFeature) string {
	if len(features) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(heading)
	b.WriteString("\n\nThese capabilities aren't available in any other tool for this API.\n")

	if groups := groupNovelFeaturesForDocs(features); len(groups) > 0 {
		for _, group := range groups {
			b.WriteString("\n### ")
			b.WriteString(group.Name)
			b.WriteString("\n")
			for _, feature := range group.Features {
				writeNovelFeatureDocLine(&b, feature)
			}
		}
	} else {
		for _, feature := range features {
			writeNovelFeatureDocLine(&b, feature)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func writeNovelFeatureDocLine(b *strings.Builder, feature NovelFeature) {
	b.WriteString("- **`")
	b.WriteString(feature.Command)
	b.WriteString("`** \u2014 ")
	b.WriteString(feature.Description)
	b.WriteString("\n")
	if feature.WhyItMatters != "" {
		b.WriteString("\n  _")
		b.WriteString(feature.WhyItMatters)
		b.WriteString("_\n")
	}
	if feature.Example != "" {
		b.WriteString("\n  ```bash\n  ")
		b.WriteString(feature.Example)
		b.WriteString("\n  ```\n")
	}
}

func groupNovelFeaturesForDocs(features []NovelFeature) []novelFeatureDocGroup {
	canonGroup := func(s string) string {
		return strings.Join(strings.Fields(strings.ToLower(s)), " ")
	}

	anyGrouped := false
	for _, feature := range features {
		if canonGroup(feature.Group) != "" {
			anyGrouped = true
			break
		}
	}
	if !anyGrouped {
		return nil
	}

	order := []string{}
	displayName := map[string]string{}
	byGroup := map[string][]NovelFeature{}
	for _, feature := range features {
		display := feature.Group
		key := canonGroup(display)
		if key == "" {
			key = "more"
			display = "More"
		}
		if _, seen := byGroup[key]; !seen {
			order = append(order, key)
			displayName[key] = display
		}
		byGroup[key] = append(byGroup[key], feature)
	}

	out := make([]novelFeatureDocGroup, 0, len(order))
	for _, key := range order {
		out = append(out, novelFeatureDocGroup{Name: displayName[key], Features: byGroup[key]})
	}
	return out
}

func replaceMarkdownSection(content, heading, replacement string, insertBefore []string) string {
	start := findMarkdownHeading(content, heading)
	if start >= 0 {
		end := findNextLevelTwoHeading(content, start+len(heading))
		return joinMarkdownParts(content[:start], replacement, content[end:])
	}

	if strings.TrimSpace(replacement) == "" {
		return content
	}

	insertAt := -1
	for _, anchor := range insertBefore {
		if idx := findMarkdownHeading(content, anchor); idx >= 0 && (insertAt == -1 || idx < insertAt) {
			insertAt = idx
		}
	}
	if insertAt == -1 {
		return joinMarkdownParts(content, replacement, "")
	}
	return joinMarkdownParts(content[:insertAt], replacement, content[insertAt:])
}

func replaceMarkdownSubsection(content, parentHeading, subsectionHeading, replacement string) string {
	parentStart := findMarkdownHeading(content, parentHeading)
	if parentStart < 0 {
		return content
	}

	parentEnd := findNextLevelTwoHeading(content, parentStart+len(parentHeading))
	subStart := findMarkdownHeadingInRange(content, subsectionHeading, parentStart+len(parentHeading), parentEnd)
	if subStart >= 0 {
		subEnd := findNextMarkdownHeadingAtMost(content, subStart+len(subsectionHeading), 3, parentEnd)
		return joinMarkdownParts(content[:subStart], replacement, content[subEnd:])
	}

	if strings.TrimSpace(replacement) == "" {
		return content
	}
	return joinMarkdownParts(content[:parentEnd], replacement, content[parentEnd:])
}

func joinMarkdownParts(prefix, middle, suffix string) string {
	prefix = strings.TrimRight(prefix, "\n")
	middle = strings.Trim(middle, "\n")
	suffix = strings.TrimLeft(suffix, "\n")

	switch {
	case middle == "":
		if prefix == "" {
			if suffix == "" {
				return ""
			}
			return suffix
		}
		if suffix == "" {
			return prefix + "\n"
		}
		return prefix + "\n\n" + suffix
	case prefix == "" && suffix == "":
		return middle + "\n"
	case prefix == "":
		return middle + "\n\n" + suffix
	case suffix == "":
		return prefix + "\n\n" + middle + "\n"
	default:
		return prefix + "\n\n" + middle + "\n\n" + suffix
	}
}

func findMarkdownHeading(content, heading string) int {
	for _, candidate := range markdownHeadings(content, 0, len(content), 1, 6) {
		if candidate.Text == heading {
			return candidate.Start
		}
	}
	return -1
}

func findMarkdownHeadingInRange(content, heading string, start, end int) int {
	for _, candidate := range markdownHeadings(content, start, end, 1, 6) {
		if candidate.Text == heading {
			return candidate.Start
		}
	}
	return -1
}

func findNextLevelTwoHeading(content string, after int) int {
	if heading := firstMarkdownHeading(content, after, len(content), 2, 2); heading.Start >= 0 {
		return heading.Start
	}
	return len(content)
}

func findNextMarkdownHeadingAtMost(content string, after, maxLevel, limit int) int {
	if heading := firstMarkdownHeading(content, after, limit, 1, maxLevel); heading.Start >= 0 {
		return heading.Start
	}
	if limit > len(content) {
		return len(content)
	}
	return limit
}

type markdownHeading struct {
	Start int
	Level int
	Text  string
}

func firstMarkdownHeading(content string, start, end, minLevel, maxLevel int) markdownHeading {
	for _, heading := range markdownHeadings(content, start, end, minLevel, maxLevel) {
		return heading
	}
	return markdownHeading{Start: -1}
}

func markdownHeadings(content string, start, end, minLevel, maxLevel int) []markdownHeading {
	if start < 0 {
		start = 0
	}
	if end > len(content) {
		end = len(content)
	}
	if start > end {
		return nil
	}

	var headings []markdownHeading
	inFence := false
	fenceMarker := ""
	for lineStart := 0; lineStart < len(content); {
		lineEnd := strings.IndexByte(content[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(content)
		} else {
			lineEnd += lineStart
		}

		if lineStart >= end {
			break
		}

		line := content[lineStart:lineEnd]
		trimmed := strings.TrimLeft(line, " \t")
		if marker, ok := markdownFenceMarker(trimmed); ok {
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker[0] == fenceMarker[0] && len(marker) >= len(fenceMarker) {
				inFence = false
				fenceMarker = ""
			}
		} else if !inFence && lineStart >= start {
			if heading, ok := parseMarkdownHeading(line, lineStart, minLevel, maxLevel); ok {
				headings = append(headings, heading)
			}
		}

		if lineEnd == len(content) {
			break
		}
		lineStart = lineEnd + 1
	}
	return headings
}

func markdownFenceMarker(trimmedLine string) (string, bool) {
	if len(trimmedLine) < 3 {
		return "", false
	}
	ch := trimmedLine[0]
	if ch != '`' && ch != '~' {
		return "", false
	}
	end := 1
	for end < len(trimmedLine) && trimmedLine[end] == ch {
		end++
	}
	if end < 3 {
		return "", false
	}
	return trimmedLine[:end], true
}

func parseMarkdownHeading(line string, start, minLevel, maxLevel int) (markdownHeading, bool) {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level < minLevel || level > maxLevel || level >= len(line) || line[level] != ' ' {
		return markdownHeading{}, false
	}
	return markdownHeading{
		Start: start,
		Level: level,
		Text:  strings.TrimRight(line, " \t\r"),
	}, true
}

const rootHighlightsHeading = "Highlights (not in the official API docs):"

func replaceRootLongHighlights(content string, features []NovelFeature) string {
	longIdx := strings.Index(content, "Long:")
	if longIdx < 0 {
		return content
	}
	openRel := strings.Index(content[longIdx:], "`")
	if openRel < 0 {
		return content
	}
	bodyStart := longIdx + openRel + 1
	closeRel := strings.Index(content[bodyStart:], "`")
	if closeRel < 0 {
		return content
	}
	bodyEnd := bodyStart + closeRel

	body := content[bodyStart:bodyEnd]
	updatedBody := replaceRootLongHighlightsBody(body, renderRootHighlights(features))
	if updatedBody == body {
		return content
	}
	return content[:bodyStart] + updatedBody + content[bodyEnd:]
}

func replaceRootLongHighlightsBody(body, replacement string) string {
	headingStart := strings.Index(body, rootHighlightsHeading)
	if headingStart >= 0 {
		sectionEnd := findRootLongFooter(body, headingStart+len(rootHighlightsHeading))
		if sectionEnd < 0 {
			sectionEnd = len(body)
		}
		return joinRootLongParts(body[:headingStart], replacement, body[sectionEnd:])
	}
	if strings.TrimSpace(replacement) == "" {
		return body
	}
	footerStart := findRootLongFooter(body, 0)
	if footerStart < 0 {
		return joinRootLongParts(body, replacement, "")
	}
	return joinRootLongParts(body[:footerStart], replacement, body[footerStart:])
}

func findRootLongFooter(body string, after int) int {
	if after < 0 {
		after = 0
	}
	if after > len(body) {
		return -1
	}
	for _, marker := range []string{"\n\nAgent mode:", "\n\nAdd --agent"} {
		if idx := strings.Index(body[after:], marker); idx >= 0 {
			return after + idx + 2
		}
	}
	for _, marker := range []string{"Agent mode:", "Add --agent"} {
		if idx := strings.Index(body[after:], marker); idx >= 0 {
			return after + idx
		}
	}
	return -1
}

func renderRootHighlights(features []NovelFeature) string {
	if len(features) == 0 {
		return ""
	}
	shown := features
	overflow := 0
	if len(shown) > 15 {
		overflow = len(shown) - 15
		shown = shown[:15]
	}

	var b strings.Builder
	b.WriteString(rootHighlightsHeading)
	b.WriteString("\n")
	for _, feature := range shown {
		b.WriteString("  • ")
		b.WriteString(goRawSafe(feature.Command))
		b.WriteString("   ")
		b.WriteString(goRawSafe(truncateRunes(feature.Description, 200)))
		b.WriteString("\n")
	}
	if overflow > 0 {
		fmt.Fprintf(&b, "  …and %d more — see README.md for the full list\n", overflow)
	}
	return strings.TrimRight(b.String(), "\n")
}

func joinRootLongParts(prefix, middle, suffix string) string {
	prefix = strings.TrimRight(prefix, "\n")
	middle = strings.Trim(middle, "\n")
	suffix = strings.TrimLeft(suffix, "\n")

	switch {
	case middle == "":
		if prefix == "" {
			return suffix
		}
		if suffix == "" {
			return prefix
		}
		return prefix + "\n\n" + suffix
	case prefix == "" && suffix == "":
		return middle
	case prefix == "":
		return middle + "\n\n" + suffix
	case suffix == "":
		return prefix + "\n\n" + middle
	default:
		return prefix + "\n\n" + middle + "\n\n" + suffix
	}
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func goRawSafe(s string) string {
	return strings.ReplaceAll(s, "`", "'")
}

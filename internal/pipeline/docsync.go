package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type novelFeatureDocGroup struct {
	Name     string
	Features []NovelFeature
}

type syncedArtifact struct {
	Path   string
	Detail string
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
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", path, err)
	}

	updated := replaceMarkdownSection(string(data), heading, replacement, insertBefore)
	if updated == string(data) {
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
	offset := 0
	for {
		idx := strings.Index(content[offset:], heading)
		if idx == -1 {
			return -1
		}
		idx += offset
		beforeOK := idx == 0 || content[idx-1] == '\n'
		after := idx + len(heading)
		afterOK := after == len(content) || content[after] == '\n' || content[after] == '\r'
		if beforeOK && afterOK {
			return idx
		}
		offset = idx + len(heading)
	}
}

func findNextLevelTwoHeading(content string, after int) int {
	if after >= len(content) {
		return len(content)
	}
	if idx := strings.Index(content[after:], "\n## "); idx >= 0 {
		return after + idx + 1
	}
	return len(content)
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

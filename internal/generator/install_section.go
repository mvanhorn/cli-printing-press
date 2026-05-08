package generator

import (
	"fmt"
	"strings"
)

// SkillInstallSectionStartHeading is the canonical heading that opens the
// install/prerequisites section in a printed CLI's SKILL.md.
const SkillInstallSectionStartHeading = "## Prerequisites: Install the CLI"

// SkillInstallSectionEndSubstr is a fragment that uniquely identifies the
// last line of the canonical install section. The canonical block ends on
// the line that contains this substring; downstream content (narrative
// value-prop, "## When to Use This CLI", etc.) is owned by other parts of
// the template and is not enforced by the canonical-sections check.
const SkillInstallSectionEndSubstr = "Do not proceed with skill commands until verification succeeds."

// canonicalSkillInstallSectionFormat is the literal text the generator
// emits into a printed CLI's SKILL.md install section. Indexed verbs:
//
//	%[1]s — CLI slug (e.g. "linear" — produces linear-pp-cli)
//	%[2]s — catalog category (or "other" when empty)
//
// Stays in lockstep with internal/generator/templates/skill.md.tmpl via
// TestCanonicalSkillInstallSectionMatchesTemplate.
const canonicalSkillInstallSectionFormat = "## Prerequisites: Install the CLI\n" +
	"\n" +
	"This skill drives the `%[1]s-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:\n" +
	"\n" +
	"1. Install via the Printing Press installer:\n" +
	"   ```bash\n" +
	"   npx -y @mvanhorn/printing-press install %[1]s --cli-only\n" +
	"   ```\n" +
	"2. Verify: `%[1]s-pp-cli --version`\n" +
	"3. Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on `$PATH`.\n" +
	"\n" +
	"If the `npx` install fails (no Node, offline, etc.), fall back to a direct Go install (requires Go 1.26.3 or newer):\n" +
	"\n" +
	"```bash\n" +
	"go install github.com/mvanhorn/printing-press-library/library/%[2]s/%[1]s/cmd/%[1]s-pp-cli@latest\n" +
	"```\n" +
	"\n" +
	"If `--version` reports \"command not found\" after install, the install step did not put the binary on `$PATH`. Do not proceed with skill commands until verification succeeds.\n"

// CanonicalSkillInstallSection returns the exact text of the install/
// prerequisites section that the generator emits into a printed CLI's
// SKILL.md, given the CLI slug and catalog category (empty -> "other").
//
// The verify-skill canonical-sections check uses this function to detect
// post-publish edits to the install instructions. The function is the
// authoritative source post-generation; the template stays in sync via
// TestCanonicalSkillInstallSectionMatchesTemplate.
func CanonicalSkillInstallSection(name, category string) string {
	if category == "" {
		category = "other"
	}
	return fmt.Sprintf(canonicalSkillInstallSectionFormat, name, category)
}

// ExtractSkillInstallSection slices the install/prerequisites block out of
// a printed CLI's SKILL.md content. Returns the text from the start
// heading through the trailing newline of the end sentinel line.
//
// Returns ok=false when either delimiter is missing, signalling that the
// SKILL.md has been edited so heavily the canonical block is no longer
// recognizable — surfaced as a "section missing" finding by callers.
func ExtractSkillInstallSection(skill string) (string, bool) {
	startIdx := strings.Index(skill, SkillInstallSectionStartHeading)
	if startIdx == -1 {
		return "", false
	}
	if startIdx > 0 && skill[startIdx-1] != '\n' {
		return "", false
	}
	tail := skill[startIdx:]
	sentinelIdx := strings.Index(tail, SkillInstallSectionEndSubstr)
	if sentinelIdx == -1 {
		return "", false
	}
	rest := tail[sentinelIdx+len(SkillInstallSectionEndSubstr):]
	nlIdx := strings.Index(rest, "\n")
	if nlIdx == -1 {
		return tail, true
	}
	return tail[:sentinelIdx+len(SkillInstallSectionEndSubstr)+nlIdx+1], true
}

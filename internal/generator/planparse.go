package generator

import (
	"bufio"
	"regexp"
	"strings"
)

// PlanCommand represents a single command extracted from a plan document.
type PlanCommand struct {
	Name        string // e.g., "record" or "auth login"
	Description string
}

// Parent returns the parent command name for subcommands, or empty string for top-level.
func (c PlanCommand) Parent() string {
	parts := strings.Fields(c.Name)
	if len(parts) > 1 {
		return parts[0]
	}
	return ""
}

// Leaf returns the leaf command name (last word).
func (c PlanCommand) Leaf() string {
	parts := strings.Fields(c.Name)
	return parts[len(parts)-1]
}

// PlanSpec holds the parsed plan data used to drive generation.
type PlanSpec struct {
	CLIName     string
	Description string
	Commands    []PlanCommand
}

// ParsePlan extracts CLI metadata and command definitions from a markdown plan document.
// It is tolerant of different plan formats: command lists, implementation units, and
// architecture sections.
func ParsePlan(content string) *PlanSpec {
	ps := &PlanSpec{}
	scanner := bufio.NewScanner(strings.NewReader(content))

	// Patterns for extracting commands
	// Backtick command in list: - `record` - Description
	backtickCmd := regexp.MustCompile(`^\s*[-*]\s+` + "`([^`]+)`" + `\s*[-:]\s*(.*)`)
	// WU/implementation unit goal line: - **Goal:** description
	goalLine := regexp.MustCompile(`^\s*[-*]\s+\*\*Goal:\*\*\s*(.*)`)
	// WU heading: ### WU-X: Feature name
	wuHeading := regexp.MustCompile(`^###\s+WU-\d+:\s+(.*)`)
	// H1 heading for CLI name
	h1 := regexp.MustCompile(`^#\s+(.*)`)
	// H2 heading for section detection
	h2 := regexp.MustCompile(`^##\s+(.*)`)

	var currentSection string
	var currentWU string

	for scanner.Scan() {
		line := scanner.Text()

		// Extract CLI name from first H1 heading
		if ps.CLIName == "" {
			if m := h1.FindStringSubmatch(line); m != nil {
				ps.CLIName = cleanCLIName(m[1])
				continue
			}
		}

		// Track current H2 section
		if m := h2.FindStringSubmatch(line); m != nil {
			currentSection = strings.ToLower(strings.TrimSpace(m[1]))
			continue
		}

		// Track WU headings
		if m := wuHeading.FindStringSubmatch(line); m != nil {
			currentWU = strings.TrimSpace(m[1])
			continue
		}

		// Extract commands from backtick list items in command-related sections
		if m := backtickCmd.FindStringSubmatch(line); m != nil {
			cmdName := strings.TrimSpace(m[1])
			cmdDesc := strings.TrimSpace(m[2])
			if cmdName != "" {
				ps.Commands = append(ps.Commands, PlanCommand{
					Name:        cmdName,
					Description: cmdDesc,
				})
			}
			continue
		}

		// Extract commands from WU goal lines (use WU name as command)
		if currentWU != "" {
			if m := goalLine.FindStringSubmatch(line); m != nil {
				desc := strings.TrimSpace(m[1])
				// Derive a command name from the WU title
				cmdName := wuTitleToCommand(currentWU)
				if cmdName != "" {
					ps.Commands = append(ps.Commands, PlanCommand{
						Name:        cmdName,
						Description: desc,
					})
				}
				currentWU = ""
				continue
			}
		}

		// In architecture/commands sections, look for plain list items with descriptions
		if isCommandSection(currentSection) {
			if cmd := parseCommandListItem(line); cmd != nil {
				ps.Commands = append(ps.Commands, *cmd)
			}
		}
	}

	// Derive description from CLI name if not set
	if ps.Description == "" && ps.CLIName != "" {
		ps.Description = "CLI for " + ps.CLIName
	}

	// Deduplicate commands (same name)
	ps.Commands = deduplicateCommands(ps.Commands)

	return ps
}

// cleanCLIName extracts a usable CLI name from a heading.
func cleanCLIName(heading string) string {
	// Remove common suffixes that are meta-descriptions, not part of the tool name.
	// Order matters: strip longer suffixes first. "CLI" is always meta since
	// the generator adds its own "-pp-cli" suffix.
	name := heading
	for _, suffix := range []string{" Implementation Plan", " CLI Plan", " Plan", " CLI"} {
		name = strings.TrimSuffix(name, suffix)
	}
	name = strings.TrimSpace(name)
	// Convert to lowercase kebab-case
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == ' ' || r == '_' {
			return '-'
		}
		return -1
	}, name)
	// Collapse multiple hyphens
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	return name
}

// wuTitleToCommand converts a WU title to a plausible command name.
func wuTitleToCommand(title string) string {
	// e.g., "Screen recording" -> "screen-recording"
	name := strings.ToLower(strings.TrimSpace(title))
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == ' ' {
			return r
		}
		return -1
	}, name)
	name = strings.ReplaceAll(name, " ", "-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	return name
}

// isCommandSection returns true if the section heading looks like it contains command definitions.
func isCommandSection(section string) bool {
	for _, keyword := range []string{"command", "architecture", "cli", "subcommand", "usage"} {
		if strings.Contains(section, keyword) {
			return true
		}
	}
	return false
}

// parseCommandListItem parses a plain list item like "- record - Record screen"
// without backticks.
func parseCommandListItem(line string) *PlanCommand {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
		return nil
	}
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimPrefix(line, "* ")
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// Try splitting on " - " for "name - description" format
	if parts := strings.SplitN(line, " - ", 2); len(parts) == 2 {
		name := strings.TrimSpace(parts[0])
		desc := strings.TrimSpace(parts[1])
		// Only accept if name looks like a command (lowercase, no spaces except for subcommands)
		if looksLikeCommand(name) {
			return &PlanCommand{Name: name, Description: desc}
		}
	}

	// Try splitting on ": " for "name: description" format
	if parts := strings.SplitN(line, ": ", 2); len(parts) == 2 {
		name := strings.TrimSpace(parts[0])
		desc := strings.TrimSpace(parts[1])
		if looksLikeCommand(name) {
			return &PlanCommand{Name: name, Description: desc}
		}
	}

	return nil
}

// looksLikeCommand returns true if a string looks like a CLI command name.
func looksLikeCommand(s string) bool {
	if s == "" {
		return false
	}
	// Allow "auth login" style subcommands
	parts := strings.FieldsSeq(s)
	for part := range parts {
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
				return false
			}
		}
	}
	return true
}

// deduplicateCommands removes duplicate commands by name, keeping the first occurrence.
func deduplicateCommands(cmds []PlanCommand) []PlanCommand {
	seen := make(map[string]bool)
	var result []PlanCommand
	for _, cmd := range cmds {
		if !seen[cmd.Name] {
			seen[cmd.Name] = true
			result = append(result, cmd)
		}
	}
	return result
}

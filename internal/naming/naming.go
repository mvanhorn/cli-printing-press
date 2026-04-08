package naming

import (
	"regexp"
	"strings"
)

const (
	CurrentCLISuffix = "-pp-cli"
	LegacyCLISuffix  = "-cli"
	MCPSuffix        = "-pp-mcp"
)

func CLI(name string) string {
	return name + CurrentCLISuffix
}

func MCP(name string) string {
	return name + MCPSuffix
}

func LegacyCLI(name string) string {
	return name + LegacyCLISuffix
}

func ValidationBinary(name string) string {
	return CLI(name) + "-validation"
}

func DogfoodBinary(name string) string {
	return CLI(name) + "-dogfood"
}

func IsCLIDirName(name string) bool {
	trimmed := trimNumericRunSuffix(name)
	return strings.HasSuffix(trimmed, CurrentCLISuffix) || strings.HasSuffix(trimmed, LegacyCLISuffix)
}

func TrimCLISuffix(name string) string {
	name = trimNumericRunSuffix(name)

	switch {
	case strings.HasSuffix(name, CurrentCLISuffix):
		return strings.TrimSuffix(name, CurrentCLISuffix)
	case strings.HasSuffix(name, LegacyCLISuffix):
		return strings.TrimSuffix(name, LegacyCLISuffix)
	default:
		return name
	}
}

// LibraryDirName maps a CLI-style name to the corresponding library directory
// key while preserving rerun suffixes. Examples:
//   - "dub-pp-cli" -> "dub"
//   - "dub-pp-cli-2" -> "dub-2"
//   - "dub-2-pp-cli" -> "dub-2"
//
// Bare slug-keyed names are returned unchanged.
func LibraryDirName(name string) string {
	trimmed := trimNumericRunSuffix(name)

	switch {
	case strings.HasSuffix(trimmed, CurrentCLISuffix):
		return strings.Replace(name, CurrentCLISuffix, "", 1)
	case strings.HasSuffix(trimmed, LegacyCLISuffix):
		return strings.Replace(name, LegacyCLISuffix, "", 1)
	default:
		return name
	}
}

// slugRe matches the slug grammar: lowercase alphanumeric + hyphens, must start
// with an alphanumeric character. Accepts rerun suffixes like "dub-2".
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// IsValidLibraryDirName returns true if name is a valid library directory name.
// It accepts both legacy CLI directory names (e.g. "dub-pp-cli", "dub-pp-cli-2")
// and slug-keyed names (e.g. "dub", "cal-com", "dub-2"). It rejects empty strings,
// path separators, ".." components, and dotfiles. This is Layer 1 input validation;
// callers that use the name in filepath.Join must still apply Layer 2 containment.
func IsValidLibraryDirName(name string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	if strings.Contains(name, "/") || strings.Contains(name, string([]byte{0})) {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	// Accept legacy CLI directory names
	if IsCLIDirName(name) {
		return true
	}
	// Accept slug grammar
	return slugRe.MatchString(name)
}

func trimNumericRunSuffix(name string) string {
	idx := strings.LastIndex(name, "-")
	if idx == -1 {
		return name
	}

	suffix := name[idx+1:]
	if suffix == "" {
		return name
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return name
		}
	}
	return name[:idx]
}

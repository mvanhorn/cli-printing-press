package naming

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/mozillazg/go-unidecode"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var leadingMarkdownHeadingRE = regexp.MustCompile(`^#{1,6}\s+(.+)$`)

// ASCIIFold transliterates Unicode to ASCII via Unidecode tables (the
// same ones Django's slugify and Rails use). Apply at every chokepoint
// that turns user-supplied spec strings (titles, resource names,
// operationIds, schema names, path segments) into file/folder names or
// Go identifiers. Output preserves spacing/casing — downstream
// to{Snake,Kebab,Camel}Case still owns identifier shape.
func ASCIIFold(s string) string {
	// Pure-ASCII fast path. unidecode.Unidecode allocates a builder and
	// walks every rune unconditionally; for the common case of
	// well-behaved OpenAPI specs this fold runs thousands of times per
	// parse on inputs that are >99% ASCII. A byte scan suffices: any
	// non-ASCII codepoint has a continuation byte ≥0x80.
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return unidecode.Unidecode(s)
		}
	}
	return s
}

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

// HumanName turns a kebab-case slug into a space-separated title-cased
// string ("steam-web" → "Steam Web", "company-goat" → "Company Goat").
// Multi-byte rune safe via cases.Title; previous hand-rolled callers
// using `s[:1]` would slice mid-codepoint on accented inputs.
func HumanName(slug string) string {
	if slug == "" {
		return ""
	}
	return cases.Title(language.English).String(strings.ReplaceAll(slug, "-", " "))
}

// Slug normalizes display-ish input into the slug grammar used for API names.
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(ASCIIFold(s)))
	var b strings.Builder
	lastHyphen := true
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// IsSlug reports whether s matches the API slug grammar.
func IsSlug(s string) bool {
	return apiSlugRe.MatchString(s)
}

// SnakeIdentifier collapses a free-form command spec into a snake_case Go
// identifier safe to use as an MCP tool name. "funding --who" → "funding_who",
// "FUNDING-TREND" → "funding_trend". Used by the generator's mcpToolName
// template helper.
func SnakeIdentifier(s string) string {
	s = ASCIIFold(s)
	var b strings.Builder
	lastUnder := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnder = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			lastUnder = false
		default:
			if !lastUnder && b.Len() > 0 {
				b.WriteByte('_')
				lastUnder = true
			}
		}
	}
	return strings.TrimRight(b.String(), "_")
}

// EnvPrefix returns an ASCII-only shell-safe environment variable prefix.
// API display names and OpenAPI titles can contain accents or non-Latin
// scripts ("PokéAPI", "Cal.com", "1Password", "東京"); generated env vars
// must not.
func EnvPrefix(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range ASCIIFold(name) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "API"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "API_" + out
	}
	return out
}

// Snake converts CamelCase to snake_case for generated tool name segments.
// Hyphens are intentionally preserved to match the historical MCP template
// helper behavior.
func Snake(s string) string {
	s = ASCIIFold(s)
	var result strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			result.WriteRune('_')
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}

// EnvVarPlaceholder derives the placeholder name from an environment variable.
// DUB_TOKEN -> token, STYTCH_PROJECT_ID -> project_id.
func EnvVarPlaceholder(envVar string) string {
	parts := strings.Split(envVar, "_")
	if len(parts) <= 1 {
		return strings.ToLower(envVar)
	}
	lower := make([]string, 0, len(parts)-1)
	for _, p := range parts[1:] {
		lower = append(lower, strings.ToLower(p))
	}
	return strings.Join(lower, "_")
}

// OneLine normalizes generated descriptions for compact template and manifest
// output, then truncates anything longer than 120 chars. Use for callers that
// need a compact single-line form (param descriptions in `--help`, terminal
// summaries). For MCP tool descriptions where richer content is intentional —
// including hand-authored mcp-descriptions.json overrides — use OneLineNormalize
// instead, which does the same normalization without the length cap.
func OneLine(s string) string {
	return truncateOneLine(OneLineNormalize(s))
}

// OneLineNormalize collapses whitespace, newlines, and quotes into a
// single-line safe form without imposing a length cap. Use for content
// that's already curated for length (MCP tool descriptions, agent-authored
// overrides) where truncating would defeat the purpose.
func OneLineNormalize(s string) string {
	s = stripLeadingMarkdownHeading(s)
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, `"`, `'`)
	s = strings.ReplaceAll(s, "\\", "")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// CompactDescription produces compact human-facing copy for catalog, skill,
// Homebrew, and agent-context descriptions. Unlike OneLine, it preserves
// quotes and backslashes because callers are responsible for escaping in their
// target format.
func CompactDescription(s string) string {
	s = stripLeadingMarkdownHeading(s)
	s = collapseWhitespace(s)
	return truncateOneLine(s)
}

func collapseWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

func truncateOneLine(s string) string {
	if len(s) > 120 {
		cut := s[:117]
		if idx := strings.LastIndex(cut, " "); idx > 60 {
			s = cut[:idx] + "..."
		} else {
			s = cut + "..."
		}
	}
	return s
}

func stripLeadingMarkdownHeading(s string) string {
	normalized := strings.ReplaceAll(s, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		m := leadingMarkdownHeadingRE.FindStringSubmatch(trimmed)
		if m == nil {
			return s
		}
		rest := firstParagraphAfter(lines[i+1:])
		if rest != "" {
			return rest
		}
		return strings.TrimSpace(m[1])
	}
	return s
}

func firstParagraphAfter(lines []string) string {
	var paragraph []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		if len(paragraph) > 0 && leadingMarkdownHeadingRE.MatchString(trimmed) {
			break
		}
		paragraph = append(paragraph, line)
	}
	return strings.TrimSpace(strings.Join(paragraph, "\n"))
}

// MCPDescription builds an MCP tool description with optional minority-side
// auth annotation. It annotates only when an API has a mix of public and
// auth-required tools, and only the minority side gets annotated.
func MCPDescription(desc string, noAuth bool, authType string, publicCount, totalCount int) string {
	authCount := totalCount - publicCount
	mixed := publicCount > 0 && authCount > 0

	if mixed {
		if noAuth && publicCount < authCount {
			desc = desc + " (public)"
		} else if !noAuth && authCount < publicCount {
			switch authType {
			case "api_key":
				desc = desc + " (requires API key)"
			case "cookie", "composed":
				desc = desc + " (requires browser login)"
			case "oauth2", "bearer_token":
				desc = desc + " (requires auth)"
			default:
				desc = desc + " (requires auth)"
			}
		}
	}

	// MCP descriptions intentionally allow rich content (1-3 sentences naming
	// action, params, return shape, when to prefer). Length is curated by the
	// agent or by the spec; we do single-line normalization but not the
	// 120-char cap that OneLine imposes for compact display.
	return OneLineNormalize(desc)
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

// slugRe is the legacy library-dir compatibility grammar: lowercase
// alphanumeric + hyphens, must start with an alphanumeric character.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// apiSlugRe is stricter: hyphens must separate non-empty segments.
var apiSlugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

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

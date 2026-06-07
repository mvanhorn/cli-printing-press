package pipeline

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// rewriteExtensions lists file extensions that may contain Go import paths,
// module-path references (e.g., goreleaser ldflags), or install instructions.
var rewriteExtensions = []string{".go", ".yaml", ".yml", ".md"}

// RewriteModulePath replaces the Go module path in a CLI directory.
// It rewrites the module declaration in go.mod and import paths
// (oldPath/internal/...) in .go and .yaml files from oldPath to newPath.
//
// Only import-style references (oldPath + "/internal/") are replaced in
// source files. Bare CLI name occurrences in command strings, User-Agent
// headers, and config paths are intentionally left untouched.
func RewriteModulePath(dir, oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}

	// Rewrite go.mod module line
	gomodPath := filepath.Join(dir, "go.mod")
	gomod, err := os.ReadFile(gomodPath)
	if err != nil {
		return fmt.Errorf("reading go.mod: %w", err)
	}

	oldModule := "module " + oldPath
	newModule := "module " + newPath
	updated := strings.Replace(string(gomod), oldModule, newModule, 1)
	if updated == string(gomod) {
		return fmt.Errorf("go.mod does not contain expected module path %q", oldPath)
	}
	if err := os.WriteFile(gomodPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("writing go.mod: %w", err)
	}

	return RewriteModulePathReferences(dir, oldPath, newPath)
}

// RewriteModulePathReferences rewrites import-style module references without
// touching the go.mod module declaration. Use this when the caller has already
// written the final go.mod contents.
func RewriteModulePathReferences(dir, oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}

	// Replace known non-Go subpath references, plus Go import declarations.
	// This avoids corrupting command Use strings, User-Agent headers,
	// config paths, and other runtime literals that contain the CLI name.
	replacements := []struct{ old, new string }{
		{oldPath + "/internal/", newPath + "/internal/"}, // Go imports, goreleaser ldflags
	}

	return filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		if !hasRewriteExtension(path) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		result := string(content)
		if strings.HasSuffix(path, ".go") {
			result = rewriteGoImportModulePaths(result, oldPath, newPath)
		}
		for _, r := range replacements {
			result = replaceModulePathToken(result, r.old, r.new)
		}
		result = rewriteModuleInstallPaths(result, oldPath, newPath)
		result = rewriteGitHubRepoURLs(result, oldPath, newPath)
		if result == string(content) {
			return nil // no changes needed
		}

		// Reformat rewritten Go source. Swapping the module path changes
		// import-path length and alphabetical order, so a string-only
		// replace leaves the import block out of gofmt order. Without this
		// pass every published CLI's imports drift from gofmt-clean — the
		// generator emits clean code, but the publish-time rewrite undoes
		// it. Reformatting here keeps published output clean by construction.
		if strings.HasSuffix(path, ".go") {
			formatted, ferr := format.Source([]byte(result))
			if ferr != nil {
				return fmt.Errorf("gofmt after module-path rewrite of %s: %w", path, ferr)
			}
			result = string(formatted)
		}

		return os.WriteFile(path, []byte(result), 0o644)
	})
}

func rewriteGoImportModulePaths(content, oldPath, newPath string) string {
	lines := strings.SplitAfter(content, "\n")
	inImportBlock := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "import (") || strings.HasPrefix(trimmed, "import("):
			inImportBlock = true
			// A grouped import may carry a path on the same line
			// (e.g. import("oldmod")); rewrite it and, if the group also
			// closes on this line, leave the block.
			lines[i] = rewriteGoImportLine(line, oldPath, newPath)
			if strings.Contains(trimmed, ")") {
				inImportBlock = false
			}
		case inImportBlock && trimmed == ")":
			inImportBlock = false
		case strings.HasPrefix(trimmed, "//"):
			// Comment line (incl. inside an import block): never rewrite, so a
			// comment that happens to quote the old module path is untouched.
			continue
		case inImportBlock || strings.HasPrefix(trimmed, "import "):
			lines[i] = rewriteGoImportLine(line, oldPath, newPath)
		}
	}

	return strings.Join(lines, "")
}

func rewriteGoImportLine(line, oldPath, newPath string) string {
	for _, quote := range []string{`"`, "`"} {
		start := strings.Index(line, quote)
		if start == -1 {
			continue
		}
		end := strings.Index(line[start+len(quote):], quote)
		if end == -1 {
			continue
		}
		end += start + len(quote)

		importPath := line[start+len(quote) : end]
		rewritten, ok := rewriteModuleImportPath(importPath, oldPath, newPath)
		if !ok {
			return line
		}
		return line[:start+len(quote)] + rewritten + line[end:]
	}

	return line
}

func rewriteModuleImportPath(importPath, oldPath, newPath string) (string, bool) {
	if importPath == oldPath {
		return newPath, true
	}
	if strings.HasPrefix(importPath, oldPath+"/") {
		return newPath + strings.TrimPrefix(importPath, oldPath), true
	}
	return importPath, false
}

func replaceModulePathToken(content, oldToken, newToken string) string {
	var b strings.Builder
	for {
		idx := strings.Index(content, oldToken)
		if idx == -1 {
			b.WriteString(content)
			return b.String()
		}

		beforeOK := modulePathTokenBoundaryBefore(b.String(), content, idx)
		if beforeOK {
			b.WriteString(content[:idx])
			b.WriteString(newToken)
			content = content[idx+len(oldToken):]
			continue
		}

		b.WriteString(content[:idx+len(oldToken)])
		content = content[idx+len(oldToken):]
	}
}

func modulePathTokenBoundaryBefore(prefix, content string, idx int) bool {
	if idx == 0 && prefix == "" {
		return true
	}
	if idx > 0 {
		return isModulePathDelimiter(content[idx-1])
	}
	return isModulePathDelimiter(prefix[len(prefix)-1])
}

func isModulePathDelimiter(ch byte) bool {
	return ch == '"' || ch == '\'' || ch == '`' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '=' || ch == ':'
}

func hasRewriteExtension(path string) bool {
	for _, ext := range rewriteExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

func rewriteModuleInstallPaths(content, oldPath, newPath string) string {
	pattern := regexp.MustCompile(`(?:\S+/)?` + regexp.QuoteMeta(oldPath) + `/cmd/`)
	return pattern.ReplaceAllString(content, newPath+"/cmd/")
}

func rewriteGitHubRepoURLs(content, oldPath, newPath string) string {
	newRepoURL, ok := githubRepoURL(newPath)
	if !ok {
		return content
	}

	releasesPattern := regexp.MustCompile(`https://github\.com/[^/\s"]+/` + regexp.QuoteMeta(oldPath) + `/releases\b`)
	content = releasesPattern.ReplaceAllString(content, newRepoURL+"/releases")

	repoPattern := regexp.MustCompile(`https://github\.com/[^/\s"]+/` + regexp.QuoteMeta(oldPath) + `\b`)
	return repoPattern.ReplaceAllString(content, newRepoURL)
}

func githubRepoURL(modulePath string) (string, bool) {
	parts := strings.Split(modulePath, "/")
	if len(parts) < 3 || parts[0] != "github.com" {
		return "", false
	}

	return "https://" + strings.Join(parts[:3], "/"), true
}

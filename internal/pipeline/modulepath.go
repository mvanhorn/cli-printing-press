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

	// Only replace subpath references: oldPath/internal/... and oldPath/cmd/...
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
		for _, r := range replacements {
			result = strings.ReplaceAll(result, r.old, r.new)
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

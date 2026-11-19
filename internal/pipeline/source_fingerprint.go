package pipeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

// SourceFingerprint is the source snapshot recorded with a Phase 5 proof.
// The per-file hashes let publish report the paths that drifted; Digest is the
// compact binding used for the unchanged-tree fast path.
//
// legacyDigest and legacyFiles hash the same bytes without replacing the
// stamped runtime version literal. Markers recorded before that replacement
// still match them. New markers record Digest and Files.
type SourceFingerprint struct {
	Digest       string
	Files        map[string]string
	legacyDigest string
	legacyFiles  map[string]string
}

func (fp SourceFingerprint) matchesRecorded(digest string) bool {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return false
	}
	if digest == fp.Digest {
		return true
	}
	return fp.legacyDigest != "" && digest == fp.legacyDigest
}

// CaptureSourceFingerprint records the relevant source files under root.
func CaptureSourceFingerprint(root string) (SourceFingerprint, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return SourceFingerprint{}, fmt.Errorf("CLI source directory is empty")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return SourceFingerprint{}, fmt.Errorf("stat CLI source directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return SourceFingerprint{}, fmt.Errorf("CLI source path must not be a symlink: %s", root)
	}
	if !info.IsDir() {
		return SourceFingerprint{}, fmt.Errorf("CLI source path is not a directory: %s", root)
	}
	modulePath, modulePlaceholder := sourceFingerprintModuleIdentity(root)

	files := make(map[string]string)
	legacyFiles := make(map[string]string)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			// Exclusions are structural. A tree with no nested checkout or
			// module keeps the same digest, so markers recorded before the
			// rule still match. Lookalike names and symlinks are not markers.
			if sourceFingerprintSkipWalkDir(root, path, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("resolve source path %q: %w", path, err)
		}
		rel = filepath.ToSlash(rel)
		if !isSourceFingerprintFile(rel) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read source file %q: %w", rel, err)
		}
		// The version token has to be replaced while the file is still Go.
		// Module-path normalization rewrites import blocks into non-Go text,
		// and the version parser would then leave the literal in the hash.
		// The legacy hash is that module-normalized original, which is what
		// a marker recorded before the placeholder bound.
		versionData := normalizeStampedRuntimeVersionLiteral(rel, data)
		legacyData, err := normalizeSourceFingerprintModulePath(rel, data, modulePath, modulePlaceholder)
		if err != nil {
			return err
		}
		normalized := legacyData
		if !bytes.Equal(versionData, data) {
			normalized, err = normalizeSourceFingerprintModulePath(rel, versionData, modulePath, modulePlaceholder)
			if err != nil {
				return err
			}
		}
		digest := sha256.Sum256(normalized)
		legacySum := sha256.Sum256(legacyData)
		files[rel] = hex.EncodeToString(digest[:])
		legacyFiles[rel] = hex.EncodeToString(legacySum[:])
		return nil
	})
	if err != nil {
		return SourceFingerprint{}, fmt.Errorf("walk CLI source directory: %w", err)
	}

	return SourceFingerprint{
		Digest:       fingerprintDigest(files),
		Files:        files,
		legacyDigest: fingerprintDigest(legacyFiles),
		legacyFiles:  legacyFiles,
	}, nil
}

func fingerprintDigest(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	hash := sha256.New()
	for _, path := range paths {
		_, _ = hash.Write([]byte(path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(files[path]))
		_, _ = hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// stampedVersionFingerprintLiteral is the stand-in for the runtime version
// string. ldflags override that string, and PreserveStampedRuntimeVersion
// copies it after the Phase 5 marker is recorded. Only this token is omitted,
// and only in the files that copy touches. The rest of the line stays hashed.
const stampedVersionFingerprintLiteral = `"<printing-press-version>"`

func normalizeStampedRuntimeVersionLiteral(rel string, data []byte) []byte {
	if !isStampedRuntimeVersionFile(rel) {
		return data
	}
	decl, err := parseVersionDeclSource(rel, data)
	if err != nil || !decl.present {
		return data
	}
	return replaceStampedVersionLiteral(data, decl)
}

func isStampedRuntimeVersionFile(rel string) bool {
	rel = strings.ReplaceAll(filepath.ToSlash(rel), `\`, `/`)
	switch rel {
	case "internal/cli/root.go", "internal/cli/version.go":
		return true
	}
	const prefix = "cmd/"
	const suffix = "/main.go"
	if !strings.HasPrefix(rel, prefix) || !strings.HasSuffix(rel, suffix) {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(rel, prefix), suffix)
	if name == "" || strings.Contains(name, "/") {
		return false
	}
	return strings.HasSuffix(name, naming.MCPSuffix)
}

func replaceStampedVersionLiteral(data []byte, decl versionDecl) []byte {
	start, end := decl.litStart, decl.litEnd
	if start < 0 || end > len(data) || start >= end {
		return data
	}
	if string(data[start:end]) != decl.literal {
		return data
	}
	if string(data[start:end]) == stampedVersionFingerprintLiteral {
		return data
	}
	out := make([]byte, 0, len(data)-(end-start)+len(stampedVersionFingerprintLiteral))
	out = append(out, data[:start]...)
	out = append(out, stampedVersionFingerprintLiteral...)
	out = append(out, data[end:]...)
	return out
}

const publishedLibraryModulePrefix = "github.com/mvanhorn/printing-press-library/library/"

// The publish copy changes its module name and self-imports after acceptance.
// Canonicalizing only that trusted rewrite lets one proof bind both trees while
// all other source, dependencies, specs, and checksums remain fingerprinted.
func normalizeSourceFingerprintModulePath(rel string, data []byte, modulePath, placeholder string) ([]byte, error) {
	if modulePath == "" || placeholder == "" {
		return data, nil
	}

	switch {
	case rel == "go.mod":
		oldModule := "module " + modulePath
		updated := strings.Replace(string(data), oldModule, "module "+placeholder, 1)
		return []byte(updated), nil
	case strings.HasSuffix(strings.ToLower(rel), ".go"):
		return normalizeGoImportFingerprints(rel, data, modulePath, placeholder)
	default:
		return data, nil
	}
}

func sourceFingerprintModuleIdentity(root string) (string, string) {
	modulePath := readModulePath(root)
	manifest, err := ReadCLIManifest(root)
	if err != nil {
		return modulePath, ""
	}
	apiName := strings.TrimSpace(manifest.APIName)
	cliName := strings.TrimSpace(manifest.CLIName)
	if !isSafeSourceFingerprintIdentity(apiName) || !isSafeSourceFingerprintIdentity(cliName) {
		return modulePath, ""
	}
	placeholder := "printing.press/generated-cli/" + apiName + "/" + cliName
	if modulePath == cliName {
		return modulePath, placeholder
	}
	if moduleSuffix, ok := strings.CutPrefix(modulePath, publishedLibraryModulePrefix); ok {
		parts := strings.Split(moduleSuffix, "/")
		if len(parts) == 2 && isSafeSourceFingerprintIdentity(parts[0]) && parts[1] == apiName {
			return modulePath, placeholder
		}
	}
	return modulePath, ""
}

func isSafeSourceFingerprintIdentity(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\\`)
}

// Rewriting self-imports can reorder an import block, so that region needs a
// stable representation while every byte outside it remains proof-bound.
func normalizeGoImportFingerprints(rel string, data []byte, modulePath, placeholder string) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, data, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse source fingerprint file %q: %w", rel, err)
	}

	var output bytes.Buffer
	cursor := 0
	for _, declaration := range file.Decls {
		imports, ok := declaration.(*ast.GenDecl)
		if !ok || imports.Tok != token.IMPORT {
			continue
		}
		start := fset.PositionFor(imports.Pos(), false).Offset
		end := fset.PositionFor(imports.End(), false).Offset
		if start < cursor || end < start || end > len(data) {
			return nil, fmt.Errorf("invalid import span in source fingerprint file %q", rel)
		}
		output.Write(data[cursor:start])
		output.WriteString(canonicalImportFingerprint(file, fset, imports, modulePath, placeholder))
		cursor = end
	}
	if cursor == 0 {
		return data, nil
	}
	output.Write(data[cursor:])
	return output.Bytes(), nil
}

func canonicalImportFingerprint(file *ast.File, fset *token.FileSet, declaration *ast.GenDecl, modulePath, placeholder string) string {
	entries := make([]string, 0, len(declaration.Specs))
	attachedComments := make(map[*ast.CommentGroup]struct{})
	for _, item := range declaration.Specs {
		spec, ok := item.(*ast.ImportSpec)
		if !ok {
			continue
		}
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			importPath = spec.Path.Value
		}
		if importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/") {
			importPath = placeholder + strings.TrimPrefix(importPath, modulePath)
		}
		alias := ""
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		comments := make([]string, 0, 2)
		if spec.Doc != nil {
			comments = append(comments, "doc\x00"+spec.Doc.Text())
			attachedComments[spec.Doc] = struct{}{}
		}
		if spec.Comment != nil {
			comments = append(comments, "line\x00"+spec.Comment.Text())
			attachedComments[spec.Comment] = struct{}{}
		}
		entry := alias + "\x00" + importPath
		if len(comments) > 0 {
			entry += "\x00" + strings.Join(comments, "\x00")
		}
		entries = append(entries, entry)
	}
	sort.Strings(entries)

	comments := make([]string, 0)
	for _, group := range file.Comments {
		if group.Pos() < declaration.Pos() || group.End() > declaration.End() {
			continue
		}
		if _, attached := attachedComments[group]; !attached {
			comments = append(comments, group.Text())
		}
	}
	sort.Strings(comments)

	return "\n<printing-press-imports>\n" + strings.Join(entries, "\n") +
		"\n<printing-press-import-comments>\n" + strings.Join(comments, "\n") +
		"\n</printing-press-imports>"
}

func sourceFingerprintSkipDir(name string) bool {
	switch name {
	case ".git", ".manuscripts", ".printing-press":
		return true
	default:
		return false
	}
}

// The walk root is this CLI even when it has a go.mod and a worktree .git
// file. A nested directory is a different tree when it is its own git
// checkout or its own module.
func sourceFingerprintSkipWalkDir(root, path, name string) bool {
	if path == root {
		return false
	}
	if sourceFingerprintSkipDir(name) {
		return true
	}
	return sourceFingerprintSeparateTree(path)
}

func sourceFingerprintSeparateTree(dir string) bool {
	return sourceFingerprintModuleRoot(dir) || sourceFingerprintGitCheckout(dir)
}

func sourceFingerprintModuleRoot(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func sourceFingerprintGitCheckout(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if info.Mode().IsRegular() {
		return sourceFingerprintGitdirFile(gitPath)
	}
	if !info.IsDir() {
		return false
	}
	return sourceFingerprintGitHEAD(filepath.Join(gitPath, "HEAD"))
}

func sourceFingerprintGitdirFile(path string) bool {
	data, err := readFilePrefix(path, 128)
	if err != nil {
		return false
	}
	content := strings.TrimLeft(string(data), " \t\r\n")
	return strings.HasPrefix(content, "gitdir:")
}

func sourceFingerprintGitHEAD(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false
	}
	data, err := readFilePrefix(path, 128)
	if err != nil {
		return false
	}
	return sourceFingerprintGitHEADText(string(data))
}

func sourceFingerprintGitHEADText(text string) bool {
	text = strings.TrimSpace(text)
	if rest, ok := strings.CutPrefix(text, "ref: "); ok {
		return strings.TrimSpace(rest) != ""
	}
	if len(text) != 40 && len(text) != 64 {
		return false
	}
	_, err := hex.DecodeString(text)
	return err == nil
}

func readFilePrefix(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return data, err
}

func isSourceFingerprintFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(strings.ToLower(path), ".go") {
		return true
	}
	switch base {
	case "go.mod", "go.sum", "spec.json", "spec.yaml", "spec.yml":
		return true
	default:
		return false
	}
}

// A recorded hash matches when it equals the normalized hash or the legacy
// hash of that path. Version-literal normalization then does not show up as
// drift for a marker recorded either before or after it.
func changedRecordedSourceFiles(expected, current, legacy map[string]string) []string {
	seen := make(map[string]struct{}, len(expected)+len(current))
	for path := range expected {
		seen[path] = struct{}{}
	}
	for path := range current {
		seen[path] = struct{}{}
	}
	for path := range legacy {
		seen[path] = struct{}{}
	}

	changed := make([]string, 0)
	for path := range seen {
		got := expected[path]
		if got == current[path] || (legacy != nil && got == legacy[path]) {
			continue
		}
		changed = append(changed, path)
	}
	sort.Strings(changed)
	return changed
}

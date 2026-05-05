// Migrate SKILL.md frontmatter metadata: from JSON-encoded string to
// ClawHub-compliant nested YAML. One-time migration tool.
//
// Operates on a public-library tree containing library/*/*/SKILL.md and
// cli-skills/pp-*/SKILL.md files. For each file:
//
//   - If the frontmatter contains a single-line `metadata: '{"openclaw":...}'`,
//     parse the embedded JSON for env/primaryEnv, derive the canonical Go
//     module path from the file's actual filesystem location and sibling
//     `.printing-press.json` provenance, and replace that line with a
//     multi-line nested YAML block. The legacy `command:` field's path is
//     intentionally NOT preserved — it can be stale (CLIs moved between
//     categories without their SKILL.md being updated). The filesystem
//     location plus provenance is the canonical truth.
//
//   - If the frontmatter has no `metadata:` line at all (the instacart case),
//     synthesize a block from the same provenance lookup and insert it
//     before the closing `---`.
//
//   - If the frontmatter is already in the nested-YAML form, skip without
//     writing (idempotent).
//
// All non-metadata frontmatter content stays byte-identical: this tool does
// no yaml.v3 round-trip of the full frontmatter, only line-targeted text
// replacement of the metadata region.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "Print what would change without writing")
	verbose := flag.Bool("verbose", false, "Print per-file action lines")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: migrate-skill-metadata [flags] <library-root>\n\n")
		fmt.Fprintf(os.Stderr, "  library-root  path to a tree containing library/*/*/SKILL.md\n")
		fmt.Fprintf(os.Stderr, "                and cli-skills/pp-*/SKILL.md\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	root := flag.Arg(0)
	// Resolve symlinks on the root too. Each per-file path is run through
	// filepath.EvalSymlinks before the prefix check; if the root itself
	// is a symlink (common on macOS where /var -> /private/var, so any
	// mktemp-created tree under /var/folders/... resolves to /private/...
	// after EvalSymlinks), an unresolved root would fail the prefix check
	// for every file.
	absRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot resolve root path: %v\n", err)
		os.Exit(1)
	}
	absRoot, err = filepath.Abs(absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot absolutize root path: %v\n", err)
		os.Exit(1)
	}
	info, err := os.Stat(absRoot)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "error: %s is not a directory\n", absRoot)
		os.Exit(1)
	}

	report, err := run(absRoot, *dryRun, *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	report.print(os.Stdout, *dryRun)
	if report.errored > 0 {
		os.Exit(1)
	}
}

type report struct {
	migrated    int      // JSON-string -> nested YAML
	synthesized int      // no metadata: line -> synthesized block
	skipped     int      // already nested
	errored     int      // refused or failed
	errors      []string // file paths with reason
}

func (r *report) print(w *os.File, dryRun bool) {
	verb := "applied"
	if dryRun {
		verb = "would apply"
	}
	fmt.Fprintf(w, "Migration summary (%s):\n", verb)
	fmt.Fprintf(w, "  migrated:    %d\n", r.migrated)
	fmt.Fprintf(w, "  synthesized: %d\n", r.synthesized)
	fmt.Fprintf(w, "  skipped:     %d (already in nested YAML form)\n", r.skipped)
	fmt.Fprintf(w, "  errored:     %d\n", r.errored)
	for _, e := range r.errors {
		fmt.Fprintf(w, "    %s\n", e)
	}
}

func run(absRoot string, dryRun, verbose bool) (*report, error) {
	files, err := discoverSkillFiles(absRoot)
	if err != nil {
		return nil, err
	}
	if verbose {
		fmt.Printf("Discovered %d SKILL.md files under %s\n", len(files), absRoot)
	}

	r := &report{}
	for _, path := range files {
		// Path-traversal guard: every discovered path must resolve under absRoot
		// even after symlink evaluation. filepath.Abs alone Cleans `..` segments
		// but does not follow symlinks; a SKILL.md symlinked to a target outside
		// the root would slip through the prefix check without EvalSymlinks.
		abs, err := filepath.EvalSymlinks(path)
		if err != nil {
			r.errored++
			r.errors = append(r.errors, fmt.Sprintf("%s: cannot resolve path: %v", path, err))
			continue
		}
		abs, err = filepath.Abs(abs)
		if err != nil {
			r.errored++
			r.errors = append(r.errors, fmt.Sprintf("%s: cannot absolutize path: %v", path, err))
			continue
		}
		if !strings.HasPrefix(abs, absRoot+string(filepath.Separator)) {
			r.errored++
			r.errors = append(r.errors, fmt.Sprintf("%s: resolved path escapes root", path))
			continue
		}

		action, err := migrateFile(abs, absRoot, dryRun)
		switch {
		case err != nil:
			r.errored++
			r.errors = append(r.errors, fmt.Sprintf("%s: %v", relpath(abs, absRoot), err))
		case action == "migrated":
			r.migrated++
		case action == "synthesized":
			r.synthesized++
		case action == "skipped":
			r.skipped++
		}
		if verbose && err == nil {
			fmt.Printf("  %s: %s\n", relpath(abs, absRoot), action)
		}
	}
	return r, nil
}

func relpath(abs, root string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return rel
}

// discoverSkillFiles returns absolute paths to all SKILL.md files under
// library/*/*/ and cli-skills/pp-*/ inside the root. Output is sorted for
// deterministic processing.
func discoverSkillFiles(absRoot string) ([]string, error) {
	var out []string
	libraryGlob := filepath.Join(absRoot, "library", "*", "*", "SKILL.md")
	libMatches, err := filepath.Glob(libraryGlob)
	if err != nil {
		return nil, fmt.Errorf("globbing library: %w", err)
	}
	out = append(out, libMatches...)

	skillsGlob := filepath.Join(absRoot, "cli-skills", "pp-*", "SKILL.md")
	skillsMatches, err := filepath.Glob(skillsGlob)
	if err != nil {
		return nil, fmt.Errorf("globbing cli-skills: %w", err)
	}
	out = append(out, skillsMatches...)

	sort.Strings(out)
	return out, nil
}

// migrateFile applies the migration to one SKILL.md, returning the action
// taken: "migrated", "synthesized", "skipped", or "" plus an error. The
// install module path always comes from the file's actual filesystem
// location and sibling `.printing-press.json` provenance — never from the
// legacy JSON-string `command:` field, which can be stale (CLIs moved
// between categories without their SKILL.md being updated). This makes
// the tool produce correct output by default with no flag to remember.
func migrateFile(absPath, absRoot string, dryRun bool) (string, error) {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	frontStart, frontEnd, err := frontmatterBounds(content)
	if err != nil {
		return "", err
	}
	front := string(content[frontStart:frontEnd])

	jsonLineRe := regexp.MustCompile(`(?m)^metadata: '(.+)'\s*$`)
	if m := jsonLineRe.FindStringSubmatchIndex(front); m != nil {
		// Legacy JSON-string form. Parse for env/primaryEnv (which the legacy
		// JSON is the source of truth for) but always derive the module from
		// provenance + filesystem.
		jsonValue := front[m[2]:m[3]]
		lineStart := frontStart + m[0]
		lineEnd := frontStart + m[1]

		env, primaryEnv, err := parseLegacyOpenclawJSON(jsonValue)
		if err != nil {
			return "", fmt.Errorf("parse legacy JSON: %w", err)
		}
		cliName, category, dirName, err := lookupProvenance(absPath, absRoot)
		if err != nil {
			return "", fmt.Errorf("provenance lookup: %w", err)
		}
		newBlock := emitMetadataBlock(cliName, buildModulePath(category, dirName, cliName), env, primaryEnv)

		updated := append([]byte{}, content[:lineStart]...)
		updated = append(updated, []byte(newBlock)...)
		updated = append(updated, content[lineEnd:]...)
		if !dryRun {
			if err := atomicWrite(absPath, updated); err != nil {
				return "", err
			}
		}
		return "migrated", nil
	}

	// Idempotency: if the frontmatter already has a `metadata:` block
	// (multi-line nested YAML form), the regex won't match and we can skip.
	if strings.Contains(front, "\nmetadata:\n") || strings.HasPrefix(front, "metadata:\n") {
		return "skipped", nil
	}

	// No metadata field at all -- synthesis path.
	cliName, category, dirName, err := lookupProvenance(absPath, absRoot)
	if err != nil {
		return "", fmt.Errorf("synthesis lookup: %w", err)
	}
	block := buildMetadataBlock(cliName, category, dirName, nil, "")
	updated := append([]byte{}, content[:frontEnd]...)
	updated = append(updated, []byte(block)...)
	updated = append(updated, content[frontEnd:]...)
	if !dryRun {
		if err := atomicWrite(absPath, updated); err != nil {
			return "", err
		}
	}
	return "synthesized", nil
}

// frontmatterBounds returns the byte offsets of the start and end of the
// frontmatter region (between the first two `---` markers). The start
// includes the opening line, the end is the byte position of the closing
// `---` line (not past its newline).
func frontmatterBounds(content []byte) (int, int, error) {
	if !strings.HasPrefix(string(content), "---\n") {
		return 0, 0, fmt.Errorf("file does not begin with frontmatter delimiter")
	}
	rest := content[4:]
	idx := strings.Index(string(rest), "\n---\n")
	if idx == -1 {
		return 0, 0, fmt.Errorf("frontmatter has no closing delimiter")
	}
	// frontmatter region: from byte 0 (start of opening `---`) through end of
	// the line just before the closing `---`. We return the inner bounds:
	// start = 4 (past opening `---\n`), end = 4 + idx + 1 (past the newline
	// before the closing `---`).
	return 4, 4 + idx + 1, nil
}

// openclawJSON is the subset of the legacy `metadata:` JSON we extract.
// Only env and primaryEnv are used; the rest of the JSON (bins, install,
// kind, command, id, label) is read by encoding/json but discarded —
// module path comes from filesystem + provenance instead.
type openclawJSON struct {
	Requires struct {
		Env []string `json:"env,omitempty"`
	} `json:"requires"`
	PrimaryEnv string `json:"primaryEnv,omitempty"`
}

// parseLegacyOpenclawJSON extracts env/primaryEnv from a legacy metadata
// JSON string. Only malformed JSON is an error; structurally-unusual but
// parseable JSON returns whatever env/primaryEnv it carries (or zero
// values if absent). The migration is structurally robust without strict
// validation: module path comes from provenance, env-absence emits a
// no-auth block, and the loop never blocks on shape variation.
func parseLegacyOpenclawJSON(jsonStr string) (env []string, primaryEnv string, err error) {
	var meta struct {
		Openclaw openclawJSON `json:"openclaw"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &meta); err != nil {
		return nil, "", fmt.Errorf("parse JSON: %w", err)
	}
	return meta.Openclaw.Requires.Env, meta.Openclaw.PrimaryEnv, nil
}

// buildMetadataBlock is the synthesis-path emitter: derives the module
// from category + library directory name + cliName before delegating to
// emitMetadataBlock. Used when a SKILL.md has no metadata field at all and
// we synthesize one from .printing-press.json provenance.
//
// dirName is the slug-keyed directory the CLI lives in under library/<category>/.
// It can differ from cliName when the directory uses the slug-only convention
// (e.g., library/commerce/instacart/ vs cli_name "instacart-pp-cli") or the
// older binary-suffix convention (e.g., library/commerce/dominos-pp-cli/).
// Module path follows library/<category>/<dirName>/cmd/<cliName>.
func buildMetadataBlock(cliName, category, dirName string, env []string, primaryEnv string) string {
	module := buildModulePath(category, dirName, cliName)
	return emitMetadataBlock(cliName, module, env, primaryEnv)
}

// buildModulePath returns the canonical Go module path for an install
// entry. dirName is the library directory basename (slug-only or
// binary-suffix); cliName is the CLI binary name; both are needed because
// the directory and binary names diverge for some CLIs.
func buildModulePath(category, dirName, cliName string) string {
	if category == "" {
		category = "other"
	}
	if dirName == "" {
		dirName = cliName
	}
	return "github.com/mvanhorn/printing-press-library/library/" + category +
		"/" + dirName + "/cmd/" + cliName
}

// emitMetadataBlock returns the canonical multi-line YAML metadata block.
// Indentation uses 2 spaces. The block always ends with a newline so it can
// splice cleanly into a frontmatter. When env or primaryEnv are supplied,
// they are emitted under openclaw in the order: requires.bins, requires.env,
// primaryEnv, install.
//
// The single source of canonical formatting -- both the migration path
// (transformMetadataJSON) and synthesis path (buildMetadataBlock) route
// through here so the byte-shape stays identical.
func emitMetadataBlock(cliName, module string, env []string, primaryEnv string) string {
	var b strings.Builder
	b.WriteString("metadata:\n")
	b.WriteString("  openclaw:\n")
	b.WriteString("    requires:\n")
	b.WriteString("      bins:\n")
	b.WriteString("        - ")
	b.WriteString(cliName)
	b.WriteString("\n")
	if len(env) > 0 {
		b.WriteString("      env:\n")
		for _, e := range env {
			b.WriteString("        - ")
			b.WriteString(e)
			b.WriteString("\n")
		}
	}
	if primaryEnv != "" {
		b.WriteString("    primaryEnv: ")
		b.WriteString(primaryEnv)
		b.WriteString("\n")
	}
	b.WriteString("    install:\n")
	b.WriteString("      - kind: go\n")
	b.WriteString("        bins: [")
	b.WriteString(cliName)
	b.WriteString("]\n")
	b.WriteString("        module: ")
	b.WriteString(module)
	b.WriteString("\n")
	return b.String()
}

// lookupProvenance resolves cli_name, category, and the library directory
// basename for the SKILL.md at skillPath.
//
// Category and dirName always come from the file's filesystem position
// (library/<category>/<dirName>/SKILL.md or cli-skills/pp-<slug>/SKILL.md
// after pivoting through the registry). cli_name comes from the sibling
// `.printing-press.json` because the binary name and the directory name
// can diverge — older CLIs use library/<dir>/ where dir is binary-suffixed,
// newer CLIs use slug-only directories. .printing-press.json's `category`
// field is unreliable (often absent in older provenance files), so it's
// not consulted; the filesystem path is the canonical truth for category.
func lookupProvenance(skillPath, absRoot string) (cliName, category, dirName string, err error) {
	dir := filepath.Dir(skillPath)
	rel, err := filepath.Rel(absRoot, skillPath)
	if err != nil {
		return "", "", "", fmt.Errorf("relpath: %w", err)
	}
	parts := strings.Split(rel, string(filepath.Separator))

	// Library file: library/<category>/<dir>/SKILL.md
	if len(parts) == 4 && parts[0] == "library" && parts[3] == "SKILL.md" {
		category = parts[1]
		dirName = parts[2]
		manifestPath := filepath.Join(dir, ".printing-press.json")
		data, readErr := os.ReadFile(manifestPath)
		if readErr != nil {
			if errors.Is(readErr, fs.ErrNotExist) {
				return "", "", "", fmt.Errorf("missing sibling .printing-press.json (need cli_name)")
			}
			return "", "", "", fmt.Errorf("read sibling provenance: %w", readErr)
		}
		cn, perr := parseProvenanceCLIName(data)
		if perr != nil {
			return "", "", "", perr
		}
		return cn, category, dirName, nil
	}

	// cli-skills mirror: cli-skills/pp-<slug>/SKILL.md. Pivot through the
	// library tree to find the corresponding library entry whose category
	// and directory basename are the canonical source.
	if len(parts) == 3 && parts[0] == "cli-skills" && parts[2] == "SKILL.md" && strings.HasPrefix(parts[1], "pp-") {
		slug := strings.TrimPrefix(parts[1], "pp-")
		candidates := []string{
			filepath.Join(absRoot, "library", "*", slug, ".printing-press.json"),
			filepath.Join(absRoot, "library", "*", slug+"-pp-cli", ".printing-press.json"),
		}
		for _, pattern := range candidates {
			matches, globErr := filepath.Glob(pattern)
			if globErr != nil {
				return "", "", "", fmt.Errorf("glob %s: %w", pattern, globErr)
			}
			for _, m := range matches {
				data, readErr := os.ReadFile(m)
				if readErr != nil {
					continue
				}
				cn, perr := parseProvenanceCLIName(data)
				if perr != nil {
					return "", "", "", perr
				}
				libDir := filepath.Dir(m)
				libRel, err := filepath.Rel(absRoot, libDir)
				if err != nil {
					return "", "", "", fmt.Errorf("relpath libDir: %w", err)
				}
				libParts := strings.Split(libRel, string(filepath.Separator))
				if len(libParts) != 3 || libParts[0] != "library" {
					return "", "", "", fmt.Errorf("library entry at unexpected path: %s", libRel)
				}
				return cn, libParts[1], libParts[2], nil
			}
		}
		return "", "", "", fmt.Errorf("could not resolve provenance for cli-skills mirror pp-%s (no matching library/<cat>/%s or library/<cat>/%s-pp-cli)", slug, slug, slug)
	}

	return "", "", "", fmt.Errorf("path is neither library/<cat>/<dir>/SKILL.md nor cli-skills/pp-<slug>/SKILL.md: %s", rel)
}

func parseProvenanceCLIName(data []byte) (string, error) {
	var pp struct {
		CLIName string `json:"cli_name"`
	}
	if err := json.Unmarshal(data, &pp); err != nil {
		return "", fmt.Errorf("parse .printing-press.json: %w", err)
	}
	if pp.CLIName == "" {
		return "", fmt.Errorf(".printing-press.json missing cli_name")
	}
	return pp.CLIName, nil
}

// atomicWrite writes data to path via tmp + rename. Preserves the original
// file's mode so a 0644 SKILL.md doesn't silently become 0600 (CreateTemp's
// default) after rename. fsyncs the data before close so a crash between
// rename and dirty-page flush can't leave a zero-length file behind.
func atomicWrite(path string, data []byte) error {
	origInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat original: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".migrate-skill-metadata-*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Chmod(origInfo.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename tmp: %w", err)
	}
	return nil
}

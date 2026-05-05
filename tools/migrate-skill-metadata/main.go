// Migrate SKILL.md frontmatter metadata: from JSON-encoded string to
// ClawHub-compliant nested YAML. One-time migration tool.
//
// Operates on a public-library tree containing library/*/*/SKILL.md and
// cli-skills/pp-*/SKILL.md files. For each file:
//
//   - If the frontmatter contains a single-line `metadata: '{"openclaw":...}'`,
//     parse the embedded JSON, apply schema corrections (kind: shell -> go,
//     drop command/id/label, derive module from command), and replace that
//     line with a multi-line nested YAML block at canonical indentation.
//
//   - If the frontmatter has no `metadata:` line at all (the instacart case),
//     synthesize a block from the sibling `.printing-press.json` provenance
//     (library files) or from the corresponding library entry (cli-skills
//     mirrors) and insert it before the closing `---`.
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
	strict := flag.Bool("strict", true, "Refuse files whose JSON doesn't match the expected kind: shell + go-install shape")
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
	absRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot resolve root path: %v\n", err)
		os.Exit(1)
	}
	info, err := os.Stat(absRoot)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "error: %s is not a directory\n", absRoot)
		os.Exit(1)
	}

	report, err := run(absRoot, *dryRun, *strict, *verbose)
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

func run(absRoot string, dryRun, strict, verbose bool) (*report, error) {
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

		action, err := migrateFile(abs, absRoot, strict, dryRun)
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
// taken: "migrated", "synthesized", "skipped", or "" plus an error.
func migrateFile(absPath, absRoot string, strict, dryRun bool) (string, error) {
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
		// Idempotency check: if the line already starts with a multi-line
		// nested form, FindStringSubmatchIndex would not have matched. So
		// matching here means we have the legacy JSON-string form.
		jsonValue := front[m[2]:m[3]]
		// Translate the match indices, which are relative to `front`, into
		// the original content's coordinate space for splicing.
		lineStart := frontStart + m[0]
		lineEnd := frontStart + m[1]

		newBlock, err := transformMetadataJSON(jsonValue, strict)
		if err != nil {
			return "", fmt.Errorf("transform: %w", err)
		}

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

// installEntryJSON mirrors the legacy `install[]` entry shape we expect to
// find in current SKILL.md files. We read kind, command, and bins; id and
// label are silently ignored by encoding/json's unknown-field default since
// the migration drops them anyway.
type installEntryJSON struct {
	Kind    string   `json:"kind"`
	Command string   `json:"command,omitempty"`
	Bins    []string `json:"bins,omitempty"`
}

// openclawJSON is the embedded JSON shape we read out of the legacy
// `metadata:` string.
type openclawJSON struct {
	Requires struct {
		Bins []string `json:"bins,omitempty"`
		Env  []string `json:"env,omitempty"`
	} `json:"requires"`
	PrimaryEnv string             `json:"primaryEnv,omitempty"`
	Install    []installEntryJSON `json:"install,omitempty"`
}

// transformMetadataJSON parses a legacy metadata JSON string and emits the
// canonical multi-line nested-YAML block. The block ends with a newline so
// it slots in cleanly where the original single line lived.
func transformMetadataJSON(jsonStr string, strict bool) (string, error) {
	var meta struct {
		Openclaw openclawJSON `json:"openclaw"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &meta); err != nil {
		return "", fmt.Errorf("parse JSON: %w", err)
	}
	if len(meta.Openclaw.Requires.Bins) == 0 {
		return "", fmt.Errorf("openclaw.requires.bins missing")
	}
	if len(meta.Openclaw.Install) == 0 {
		return "", fmt.Errorf("openclaw.install[] missing")
	}

	// We only emit the first install entry's binary + module. Today every
	// printed CLI has exactly one install option (go install), so multi-entry
	// support is not yet exercised; revisit if multi-platform installs land.
	first := meta.Openclaw.Install[0]
	if strict {
		if first.Kind != "shell" {
			return "", fmt.Errorf("install[0].kind=%q (strict mode expects 'shell')", first.Kind)
		}
		if !strings.HasPrefix(first.Command, "go install ") {
			return "", fmt.Errorf("install[0].command does not start with 'go install '")
		}
	}

	module, err := deriveModule(first.Command)
	if err != nil {
		return "", fmt.Errorf("install[0]: %w", err)
	}
	bins := first.Bins
	if len(bins) == 0 {
		bins = meta.Openclaw.Requires.Bins
	}
	cliName := bins[0]
	return emitMetadataBlock(cliName, module, meta.Openclaw.Requires.Env, meta.Openclaw.PrimaryEnv), nil
}

// deriveModule strips "go install " prefix and "@latest" or "@<version>"
// suffix from a command string and returns the bare module path.
func deriveModule(command string) (string, error) {
	const prefix = "go install "
	if !strings.HasPrefix(command, prefix) {
		return "", fmt.Errorf("command does not start with 'go install '")
	}
	rest := strings.TrimPrefix(command, prefix)
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[:at]
	}
	if rest == "" {
		return "", fmt.Errorf("module path empty after stripping prefix/version")
	}
	return rest, nil
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
	if category == "" {
		category = "other"
	}
	if dirName == "" {
		dirName = cliName
	}
	module := "github.com/mvanhorn/printing-press-library/library/" + category +
		"/" + dirName + "/cmd/" + cliName
	return emitMetadataBlock(cliName, module, env, primaryEnv)
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
// basename for synthesis.
//
// For library/*/<dir>/SKILL.md, reads the sibling .printing-press.json and
// uses <dir> as the library directory basename.
// For cli-skills/pp-<slug>/SKILL.md, scans library/*/*/.printing-press.json
// for a directory matching <slug> or <slug>-pp-cli and uses that
// directory's basename. The basename can differ from cli_name (slug-only
// convention vs. older binary-suffix convention), so the module path must
// be derived from the actual filesystem location.
func lookupProvenance(skillPath, absRoot string) (cliName, category, dirName string, err error) {
	dir := filepath.Dir(skillPath)
	manifestPath := filepath.Join(dir, ".printing-press.json")
	data, readErr := os.ReadFile(manifestPath)
	if readErr == nil {
		cn, cat, perr := parseProvenance(data)
		if perr != nil {
			return "", "", "", perr
		}
		return cn, cat, filepath.Base(dir), nil
	}
	// Only fall through to cli-skills mirror lookup when the sibling manifest
	// genuinely doesn't exist. Other read errors (permission denied, EIO, EISDIR)
	// would otherwise produce a misleading 'not a pp-* directory' error from the
	// fallthrough path.
	if !errors.Is(readErr, fs.ErrNotExist) {
		return "", "", "", fmt.Errorf("read sibling provenance: %w", readErr)
	}
	// cli-skills mirror: derive slug, scan library/.
	base := filepath.Base(dir)
	slug := strings.TrimPrefix(base, "pp-")
	if slug == base {
		return "", "", "", fmt.Errorf("no sibling .printing-press.json and not a pp-* directory")
	}
	candidates := []string{
		filepath.Join(absRoot, "library", "*", slug, ".printing-press.json"),
		filepath.Join(absRoot, "library", "*", slug+"-pp-cli", ".printing-press.json"),
	}
	for _, pattern := range candidates {
		// filepath.Glob errors on ErrBadPattern only; the patterns above are
		// static and known-good, so an error here means the OS layer is broken
		// rather than the input. Surface it instead of silently skipping.
		matches, globErr := filepath.Glob(pattern)
		if globErr != nil {
			return "", "", "", fmt.Errorf("glob %s: %w", pattern, globErr)
		}
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			cn, cat, perr := parseProvenance(data)
			if perr != nil {
				return "", "", "", perr
			}
			return cn, cat, filepath.Base(filepath.Dir(m)), nil
		}
	}
	return "", "", "", fmt.Errorf("could not resolve provenance for cli-skills mirror %s", base)
}

func parseProvenance(data []byte) (cliName, category string, err error) {
	var pp struct {
		CLIName  string `json:"cli_name"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal(data, &pp); err != nil {
		return "", "", fmt.Errorf("parse .printing-press.json: %w", err)
	}
	if pp.CLIName == "" {
		return "", "", fmt.Errorf(".printing-press.json missing cli_name")
	}
	return pp.CLIName, pp.Category, nil
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

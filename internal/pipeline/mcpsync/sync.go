package mcpsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v2/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v2/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v2/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v2/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v2/internal/spec"
)

var (
	ErrHandEdited = errors.New("mcp tools.go appears hand-edited")
	// errAnnotationSoftFail signals the caller of ensureEndpointAnnotation
	// that the file could not be annotated for a non-fatal reason (hand-edit
	// or pre-existing Annotations map). The migration should warn and skip
	// rather than abort, because the runtime walker registers any
	// unannotated user-facing command as a shell-out tool — typed endpoint
	// annotation is an optimization, not a correctness requirement.
	errAnnotationSoftFail = errors.New("endpoint annotation skipped")
)

var endpointAnnotationLine = regexp.MustCompile(`(?m)^\s*Annotations: map\[string\]string\{"pp:endpoint": "[^"]+"\},\s*$`)

type Result struct {
	Changed bool
	Detail  string
}

type Options struct {
	Force bool
}

func Sync(cliDir string, opts Options) (Result, error) {
	state, err := pipeline.InspectMCPSurface(cliDir)
	if err != nil {
		return Result{}, err
	}
	if state.State == pipeline.MCPSurfaceHandEdited && !opts.Force {
		return Result{}, fmt.Errorf("%w: tools.go appears hand-edited; refusing to overwrite. Use --force to override at your own risk", ErrHandEdited)
	}
	// MCPSurfaceRuntime means the MCP source is already on the new walker
	// template and we don't need to migrate that. But we still refresh
	// metadata files (manifest.json, tools-manifest.json) because their
	// upstream sources (.printing-press.json description, spec auth, etc.)
	// may have changed since the last sync. Skipping these would silently
	// freeze stale descriptions/annotations through future regen.
	alreadyMigrated := state.State == pipeline.MCPSurfaceRuntime

	parsed, err := loadArchivedSpec(cliDir)
	if err != nil {
		return Result{}, err
	}
	// Preserve the existing manifest.json's display_name onto the parsed
	// spec when the spec itself doesn't carry one. Library CLIs printed
	// before spec.display_name existed (v1.x) lack the canonical source,
	// but the PR #145 codemod baked the right brand casing into
	// manifest.json from registry.json. Without this, mcp-sync's
	// regeneration drops "ESPN" back to the title-cased slug ("Espn"),
	// regressing both the MCP server identity (NewMCPServer first arg)
	// and the bundled manifest's display_name field.
	if parsed.DisplayName == "" {
		if existing := readExistingManifestDisplayName(cliDir); existing != "" {
			parsed.DisplayName = existing
		}
	}
	modulePath, err := readModulePath(cliDir)
	if err != nil {
		return Result{}, err
	}
	features := loadNovelFeatures(cliDir)
	if !alreadyMigrated {
		if err := ensureRootCmdExport(cliDir); err != nil {
			return Result{}, err
		}
		if err := ensureEndpointAnnotations(cliDir, parsed, features); err != nil {
			return Result{}, err
		}
		// Older generator templates split MCP handlers into a separate
		// internal/mcp/handlers.go file. The current template emits all
		// handlers (handleContext, handleSQL, handleSync, makeAPIHandler,
		// etc.) in tools.go. Leaving the stale handlers.go in place
		// during regen produces a "redeclared in this block" build error
		// because both files now define the same functions. Detect a
		// generator-marked handlers.go and remove it before regenerating;
		// refuse to delete a hand-edited one without --force.
		if err := removeStaleMCPHandlersFile(cliDir, opts.Force); err != nil {
			return Result{}, err
		}
		gen := generator.New(parsed, cliDir)
		gen.NovelFeatures = features
		gen.ModulePath = modulePath
		if err := gen.GenerateMCPSurfaceOnly(); err != nil {
			return Result{}, fmt.Errorf("rendering MCP surface: %w", err)
		}
	}
	if err := pipeline.WriteToolsManifest(cliDir, parsed); err != nil {
		return Result{}, fmt.Errorf("regenerating tools-manifest.json: %w", err)
	}
	// Regenerate the MCPB manifest too. The schema can drift between
	// generator releases (most recently: cli_binary was removed because
	// Claude Desktop strict-validates v0.3 keys). mcp-sync without this
	// step left every library CLI with a manifest that fails drag-drop
	// install in Claude Desktop.
	if err := pipeline.WriteMCPBManifest(cliDir); err != nil {
		return Result{}, fmt.Errorf("regenerating manifest.json: %w", err)
	}
	if alreadyMigrated {
		return Result{Changed: true, Detail: "refreshed manifest.json + tools-manifest.json from current spec/.printing-press.json"}, nil
	}
	return Result{Changed: true, Detail: "migrated MCP surface to runtime Cobra-tree mirror"}, nil
}

func loadArchivedSpec(cliDir string) (*spec.APISpec, error) {
	for _, name := range []string{"spec.yaml", "spec.yml", "spec.json", "schema.graphql", "schema.gql"} {
		path := filepath.Join(cliDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		if openapi.IsOpenAPI(data) {
			return openapi.ParseLenient(data)
		}
		if graphql.IsGraphQLSDL(data) {
			return graphql.ParseSDLBytes(path, data)
		}
		return spec.ParseBytes(data)
	}
	return nil, fmt.Errorf("missing archived spec (expected spec.yaml, spec.yml, spec.json, schema.graphql, or schema.gql)")
}

func loadNovelFeatures(cliDir string) []generator.NovelFeature {
	data, err := os.ReadFile(filepath.Join(cliDir, pipeline.CLIManifestFilename))
	if err != nil {
		return nil
	}
	var manifest pipeline.CLIManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}
	features := make([]generator.NovelFeature, 0, len(manifest.NovelFeatures))
	for _, nf := range manifest.NovelFeatures {
		features = append(features, generator.NovelFeature{
			Name:        nf.Name,
			Command:     nf.Command,
			Description: nf.Description,
		})
	}
	return features
}

func ensureEndpointAnnotations(cliDir string, parsed *spec.APISpec, features []generator.NovelFeature) error {
	tmp, err := os.MkdirTemp("", "printing-press-mcp-sync-*")
	if err != nil {
		return fmt.Errorf("creating endpoint annotation reference tree: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	gen := generator.New(parsed, tmp)
	gen.NovelFeatures = features
	if modulePath, err := readModulePath(cliDir); err == nil && modulePath != "" {
		gen.ModulePath = modulePath
	}
	if err := gen.Generate(); err != nil {
		return fmt.Errorf("rendering endpoint annotation reference tree: %w", err)
	}

	refRoot := filepath.Join(tmp, "internal", "cli")
	return filepath.WalkDir(refRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		line := endpointAnnotationLine.FindString(string(data))
		if line == "" {
			return nil
		}
		rel, err := filepath.Rel(tmp, path)
		if err != nil {
			return err
		}
		err = ensureEndpointAnnotation(filepath.Join(cliDir, rel), line)
		if err == nil {
			return nil
		}
		// Hand-edited or pre-existing-Annotations files can't have
		// endpoint annotations added safely. That's not fatal — the runtime
		// walker registers them as shell-out tools regardless. Warn and
		// move on so the rest of the migration completes.
		if errors.Is(err, errAnnotationSoftFail) {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			return nil
		}
		return err
	})
}

func ensureEndpointAnnotation(path, annotationLine string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading endpoint command %s: %w", path, err)
	}
	src := string(data)
	if strings.Contains(src, `"pp:endpoint"`) {
		return nil
	}
	if !strings.Contains(src, "Generated by CLI Printing Press") {
		return fmt.Errorf("%w: %s appears hand-edited; runtime walker will register it as a shell-out tool instead", errAnnotationSoftFail, path)
	}
	if strings.Contains(src, "\n\t\tAnnotations:") {
		return fmt.Errorf("%w: %s already has a Cobra annotation map without pp:endpoint; runtime walker will register it as a shell-out tool", errAnnotationSoftFail, path)
	}

	insertAt := -1
	if loc := regexp.MustCompile(`(?m)^\t\tExample: .*,\n`).FindStringIndex(src); loc != nil {
		insertAt = loc[1]
	} else if loc := regexp.MustCompile(`(?m)^\t\tRunE: func`).FindStringIndex(src); loc != nil {
		insertAt = loc[0]
	}
	if insertAt < 0 {
		return fmt.Errorf("%s does not match the generated endpoint command shape; cannot add endpoint MCP annotation", path)
	}

	if !strings.HasSuffix(annotationLine, "\n") {
		annotationLine += "\n"
	}
	next := src[:insertAt] + annotationLine + src[insertAt:]
	return writeFileAtomic(path, []byte(next))
}

func ensureRootCmdExport(cliDir string) error {
	path := filepath.Join(cliDir, "internal", "cli", "root.go")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading root.go: %w", err)
	}
	src := string(data)
	if strings.Contains(src, "func RootCmd() *cobra.Command") {
		return nil
	}
	if !strings.Contains(src, "Generated by CLI Printing Press") {
		return fmt.Errorf("root.go appears hand-edited; refusing to add RootCmd export")
	}

	executePrefix := "// Execute runs the CLI in non-interactive mode: never prompts, all values via flags or stdin.\nfunc Execute() error {\n\tvar flags rootFlags\n\n\trootCmd := &cobra.Command{"
	start := strings.Index(src, executePrefix)
	if start < 0 {
		executePrefix = "func Execute() error {\n\tvar flags rootFlags\n\n\trootCmd := &cobra.Command{"
		start = strings.Index(src, executePrefix)
	}
	if start < 0 {
		return fmt.Errorf("root.go does not match the generated Execute shape; cannot add RootCmd export automatically")
	}
	prolog := `// RootCmd returns the Cobra command tree without executing it. The MCP server
// uses this to mirror every user-facing command as an agent tool.
func RootCmd() *cobra.Command {
	var flags rootFlags
	return newRootCmd(&flags)
}

// Execute runs the CLI in non-interactive mode: never prompts, all values via flags or stdin.
func Execute() error {
	var flags rootFlags
	rootCmd := newRootCmd(&flags)

	err := rootCmd.Execute()
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		msg := err.Error()
		// Extract the flag name from the error message (e.g., "unknown flag: --foob")
		if idx := strings.Index(msg, "unknown flag: "); idx >= 0 {
			flagStr := strings.TrimSpace(msg[idx+len("unknown flag: "):])
			if suggestion := suggestFlag(flagStr, rootCmd); suggestion != "" {
				return fmt.Errorf("%w\nhint: did you mean --%s?", err, suggestion)
			}
		}
	}
	if err == nil && flags.deliverBuf != nil {
		if derr := Deliver(flags.deliverSink, flags.deliverBuf.Bytes(), flags.compact); derr != nil {
			fmt.Fprintf(os.Stderr, "warning: deliver to %s:%s failed: %v\n", flags.deliverSink.Scheme, flags.deliverSink.Target, derr)
			return derr
		}
	}
	return err
}

func newRootCmd(flags *rootFlags) *cobra.Command {
	rootCmd := &cobra.Command{`

	// After the Execute → newRootCmd refactor, `flags` is *rootFlags
	// rather than a struct value, so any bare `&flags` reference (passing
	// the whole struct's address) needs to drop the `&`. We must NOT
	// touch `&flags.someField` (taking the address of an individual
	// field) — those still compile because field access through a
	// pointer auto-dereferences. The earlier ReplaceAll-based approach
	// only caught `(&flags)` (single-arg or trailing-arg callsites) and
	// missed multi-arg shapes like `f(cmd.Context(), &flags, resources)`
	// and `f(x, &flags)`. The regex below matches `&flags` only when the
	// next character is something other than `.` or a word character —
	// i.e., a comma, close-paren, semicolon, brace, etc. — which
	// distinguishes "address of the whole struct" from "address of a
	// field" without false positives.
	bareFlagsRef := regexp.MustCompile(`&flags([^.\w]|$)`)
	tail := bareFlagsRef.ReplaceAllString(src[start+len(executePrefix):], "flags$1")
	src = src[:start] + prolog + tail

	exitStart := strings.LastIndex(src, "\n\terr := rootCmd.Execute()")
	exitEnd := strings.Index(src, "\nfunc ExitCode")
	if exitStart < 0 || exitEnd < 0 || exitStart > exitEnd {
		return fmt.Errorf("root.go does not match the generated Execute footer; cannot add RootCmd export automatically")
	}
	src = src[:exitStart] + "\n\treturn rootCmd\n}\n" + src[exitEnd:]

	return writeFileAtomic(path, []byte(src))
}

// readExistingManifestDisplayName returns the display_name from an existing
// manifest.json on disk if it's a real brand name rather than the
// title-cased slug fallback. Used by Sync to preserve PR #145 codemod
// brand-casing for library CLIs printed before spec.display_name existed.
func readExistingManifestDisplayName(cliDir string) string {
	manifestData, err := os.ReadFile(filepath.Join(cliDir, "manifest.json"))
	if err != nil {
		return ""
	}
	var existing struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(manifestData, &existing); err != nil {
		return ""
	}
	if existing.DisplayName == "" {
		return ""
	}
	// The derived form for old prints is the title-cased mcp-binary slug
	// minus the "-pp-mcp" suffix (e.g., "espn-pp-mcp" → "Espn"). If the
	// existing display_name matches that derived shape, treat it as no
	// brand info and fall through.
	derived := titleCaseFromSlug(strings.TrimSuffix(existing.Name, "-pp-mcp"))
	if existing.DisplayName == derived {
		return ""
	}
	return existing.DisplayName
}

// titleCaseFromSlug capitalizes the first rune of a slug. Approximates
// the spec.EffectiveDisplayName fallback for slugs without an explicit
// display_name (e.g., "espn" → "Espn"). Mirrors the case-detection logic
// readExistingManifestDisplayName uses to decide whether the existing
// manifest carries real brand information.
func titleCaseFromSlug(slug string) string {
	if slug == "" {
		return ""
	}
	runes := []rune(slug)
	if runes[0] >= 'a' && runes[0] <= 'z' {
		runes[0] -= 'a' - 'A'
	}
	return string(runes)
}

// removeStaleMCPHandlersFile deletes internal/mcp/handlers.go when it
// carries the generator's don't-edit marker. Older templates split MCP
// handlers across tools.go and handlers.go; the current template emits
// everything in tools.go. Leaving the stale file in place causes
// duplicate function definitions ("handleContext redeclared in this
// block"). When the file lacks the marker (hand-edited), refuse to
// delete without --force so we don't blow away custom logic.
func removeStaleMCPHandlersFile(cliDir string, force bool) error {
	path := filepath.Join(cliDir, "internal", "mcp", "handlers.go")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if !strings.Contains(string(data), "Generated by CLI Printing Press") && !force {
		return fmt.Errorf("%s appears hand-edited; refusing to remove. Use --force to override (this will delete your custom handlers)", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing stale %s: %w", path, err)
	}
	return nil
}

// readModulePath parses the cli's go.mod and returns the declared module
// path. mcp-sync needs this so the regenerated MCP source uses the actual
// import paths the rest of the CLI was built against. Library checkouts
// declare the full repo path; standalone publishes use the bare CLI name.
// Either way the existing go.mod is the source of truth.
func readModulePath(cliDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(cliDir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}
	for _, line := range strings.SplitN(string(data), "\n", 50) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module")), nil
		}
	}
	return "", fmt.Errorf("go.mod missing module declaration")
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temporary %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

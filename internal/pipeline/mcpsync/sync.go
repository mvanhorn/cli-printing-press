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

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/mcpoverrides"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
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

var endpointAnnotationLine = regexp.MustCompile(`(?m)^\s*Annotations: map\[string\]string\{"pp:endpoint": "[^"]+", "pp:method": "[^"]+", "pp:path": "[^"]+"(?:, "mcp:read-only": "true")?\},\s*$`)
var staleEndpointAnnotationLine = regexp.MustCompile(`(?m)^\s*Annotations: map\[string\]string\{"pp:endpoint": "[^"]+"(?:, "mcp:read-only": "true")?\},\s*$`)

type Result struct {
	Changed bool
	Detail  string
	// UnmatchedOverrideKeys lists keys from mcp-descriptions.json that
	// did not correspond to any endpoint in the spec — typos, stale
	// keys after a rename, or overrides for endpoints removed from the
	// spec. The library returns these so the CLI layer can surface them;
	// Sync itself does not print, keeping output formatting at the edge.
	UnmatchedOverrideKeys []string
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
	// Validate that spec.yaml.name matches the directory's basename.
	// Older library CLIs sometimes have drift (weather-goat's
	// spec.yaml.name = "weather"; open-meteo's name diverges similarly)
	// because the directory was renamed via emboss/republish but the
	// spec was never updated. Without this guard, the generator
	// faithfully creates spurious cmd/<spec.name>-pp-cli/ and
	// cmd/<spec.name>-pp-mcp/ directories alongside the canonical ones,
	// and emits server.NewMCPServer(<spec.name>) with the wrong identity.
	//
	// reconcileSpecNameWithDir auto-fixes the common case (internal YAML
	// spec with a stale top-level `name:` field) by rewriting the line in
	// place. For OpenAPI/GraphQL specs the fix is too invasive, so it
	// falls through to the validator's --force-required error.
	renamedFrom, err := reconcileSpecNameWithDir(cliDir, parsed)
	if err != nil && !opts.Force {
		return Result{}, err
	}
	if renamedFrom != "" {
		fmt.Fprintf(os.Stderr, "mcp-sync: rewrote spec.yaml name from %q to %q to match directory-derived slug\n", renamedFrom, parsed.Name)
	}
	// Preserve the existing manifest.json's display_name onto the parsed
	// spec when the spec itself doesn't carry one. Library CLIs printed
	// before spec.display_name existed (v1.x) lack the canonical source,
	// but the PR #145 codemod baked the right brand casing into
	// manifest.json from registry.json. Without this, mcp-sync's
	// regeneration drops "ESPN" back to the title-cased slug ("Espn"),
	// regressing both the MCP server identity (NewMCPServer first arg)
	// and the bundled manifest's display_name field.
	//
	// Falls through to the public library's registry.json when
	// manifest.json has no usable value (browser-sniffed CLIs that
	// never had a manifest, or a manifest already corrupted to the
	// slug form). The registry is the catalog source of truth for
	// brand names.
	if parsed.DisplayName == "" {
		if existing := readExistingManifestDisplayName(cliDir); existing != "" {
			parsed.DisplayName = existing
		} else if registryName := readRegistryDisplayName(parsed.Name); registryName != "" {
			parsed.DisplayName = registryName
		}
	}
	// Apply hand-authored MCP description overrides before generating
	// any surface that consumes endpoint.Description. Both the manifest
	// writer and the mcp_tools.go template read from this parsed spec,
	// so a single in-place patch flows through to both. The override
	// file is the sanctioned path for replacing thin spec-derived
	// descriptions; direct edits to internal/mcp/tools.go and
	// tools-manifest.json are wiped on regen because both files carry
	// the generator's DO-NOT-EDIT header.
	overrides, err := mcpoverrides.Load(cliDir)
	if err != nil {
		return Result{}, fmt.Errorf("loading mcp description overrides: %w", err)
	}
	unmatched := overrides.Apply(parsed)
	modulePath, err := readModulePath(cliDir)
	if err != nil {
		return Result{}, err
	}
	features := loadNovelFeatures(cliDir)
	// Migration-only steps run when the surface is on the legacy
	// template. Already-migrated CLIs skip these.
	if !alreadyMigrated {
		if err := ensureRootCmdExport(cliDir); err != nil {
			return Result{}, err
		}
		if err := ensureEndpointAnnotations(cliDir, parsed, features); err != nil {
			return Result{}, err
		}
		// Cobratree templates (mcp_tools.go.tmpl, main_mcp.go.tmpl, etc.)
		// use APIs added in mcp-go v0.47.0 (WithReadOnlyHintAnnotation,
		// req.GetArguments). Older generated CLIs pin a lower version
		// and won't compile after the surface regen below. Bump the
		// pin in go.mod before generation so the resulting CLI builds.
		bumpedFrom, err := ensureMCPGoMinVersion(cliDir)
		if err != nil {
			return Result{}, err
		}
		if bumpedFrom != "" {
			fmt.Fprintf(os.Stderr, "mcp-sync: bumped mark3labs/mcp-go in go.mod from %s to %s for cobratree compatibility\n", bumpedFrom, minMCPGoVersionForCobratree)
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
	}
	// Surface regen runs every sync — overrides applied above must reach
	// tools.go, and WriteToolsManifest below rewrites the manifest
	// unconditionally, so keeping tools.go in lockstep avoids drift.
	gen := generator.New(parsed, cliDir)
	gen.NovelFeatures = features
	gen.ModulePath = modulePath
	if err := gen.GenerateMCPSurface(); err != nil {
		return Result{}, fmt.Errorf("rendering MCP surface: %w", err)
	}
	if err := pipeline.WriteToolsManifest(cliDir, parsed); err != nil {
		return Result{}, fmt.Errorf("regenerating tools-manifest.json: %w", err)
	}
	// Refresh .printing-press.json's spec-derived fields before regenerating
	// manifest.json. WriteMCPBManifest reads provenance from disk, so
	// without this step spec.yaml updates to auth.key_url, auth.optional,
	// auth.env_vars, and similar never reach the MCPB Configure modal.
	// This staleness bit recipe-goat twice in one session — first when
	// auth.key_url was added (signup URL didn't surface), then again
	// when auth.optional was added (Required label didn't drop).
	if err := pipeline.RefreshCLIManifestFromSpec(cliDir, parsed); err != nil {
		return Result{}, fmt.Errorf("refreshing CLI manifest from spec: %w", err)
	}
	// Regenerate the MCPB manifest too. The schema can drift between
	// generator releases (most recently: cli_binary was removed because
	// Claude Desktop strict-validates v0.3 keys). mcp-sync without this
	// step left every library CLI with a manifest that fails drag-drop
	// install in Claude Desktop.
	if err := pipeline.WriteMCPBManifest(cliDir); err != nil {
		return Result{}, fmt.Errorf("regenerating manifest.json: %w", err)
	}
	detail := "migrated MCP surface to runtime Cobra-tree mirror"
	if alreadyMigrated {
		detail = "refreshed MCP surface, manifest.json, and tools-manifest.json from current spec / .printing-press.json / mcp-descriptions.json"
	}
	return Result{Changed: true, Detail: detail, UnmatchedOverrideKeys: unmatched}, nil
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
			return openapi.ParseWithPathLenient(data, path)
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
	if endpointAnnotationLine.MatchString(src) {
		return nil
	}
	annotationLine = strings.TrimSuffix(annotationLine, "\n")
	if staleEndpointAnnotationLine.MatchString(src) {
		next := staleEndpointAnnotationLine.ReplaceAllString(src, annotationLine)
		return writeFileAtomic(path, []byte(next))
	}
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

	annotationLine += "\n"
	next := src[:insertAt] + annotationLine + src[insertAt:]
	return writeFileAtomic(path, []byte(next))
}

// pkgContainsDecl reports whether any .go file in pkgDir contains decl.
// Skips _test.go files so test fixtures don't trigger false positives.
func pkgContainsDecl(pkgDir, decl string) (bool, error) {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pkgDir, name))
		if err != nil {
			return false, err
		}
		if strings.Contains(string(data), decl) {
			return true, nil
		}
	}
	return false, nil
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
	// Skip prolog blocks whose supporting types/functions aren't in the
	// existing source — older library CLIs predate suggestFlag and
	// Deliver and would otherwise fail to build with "undefined" errors.
	pkgDir := filepath.Join(cliDir, "internal", "cli")
	hasSuggestFlag, err := pkgContainsDecl(pkgDir, "func suggestFlag(")
	if err != nil {
		return fmt.Errorf("scanning cli package for suggestFlag: %w", err)
	}
	hasDeliverFunc, err := pkgContainsDecl(pkgDir, "func Deliver(")
	if err != nil {
		return fmt.Errorf("scanning cli package for Deliver: %w", err)
	}
	hasDeliver := hasDeliverFunc && strings.Contains(src, "deliverBuf")

	suggestBlock := ""
	if hasSuggestFlag {
		suggestBlock = `
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		msg := err.Error()
		// Extract the flag name from the error message (e.g., "unknown flag: --foob")
		if idx := strings.Index(msg, "unknown flag: "); idx >= 0 {
			flagStr := strings.TrimSpace(msg[idx+len("unknown flag: "):])
			if suggestion := suggestFlag(flagStr, rootCmd); suggestion != "" {
				return fmt.Errorf("%w\nhint: did you mean --%s?", err, suggestion)
			}
		}
	}`
	}
	deliverBlock := ""
	if hasDeliver {
		deliverBlock = `
	if err == nil && flags.deliverBuf != nil {
		if derr := Deliver(flags.deliverSink, flags.deliverBuf.Bytes(), flags.compact); derr != nil {
			fmt.Fprintf(os.Stderr, "warning: deliver to %s:%s failed: %v\n", flags.deliverSink.Scheme, flags.deliverSink.Target, derr)
			return derr
		}
	}`
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

	err := rootCmd.Execute()` + suggestBlock + deliverBlock + `
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

// readExistingManifestDisplayName returns the display_name from an
// existing manifest.json if it's a real brand name. The only form
// rejected is the bare lowercase slug we'd otherwise emit as last
// resort; everything else (ESPN, Wikipedia, Cal.com, Company GOAT,
// PokéAPI) is preserved.
func readExistingManifestDisplayName(cliDir string) string {
	manifestData, err := os.ReadFile(filepath.Join(cliDir, pipeline.MCPBManifestFilename))
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
	apiSlug := strings.TrimSuffix(existing.Name, "-pp-mcp")
	if existing.DisplayName == "" || existing.DisplayName == apiSlug {
		return ""
	}
	return existing.DisplayName
}

// registryDisplayNameMaxLen caps the length of registry.json's `api`
// field when used as a display_name. The field is overloaded — most
// entries hold a short brand name ("Cal.com", "Company GOAT") but a
// minority hold a full descriptive sentence (recipe-goat ships ~200
// chars). 40 fits every observed real brand name with margin.
const registryDisplayNameMaxLen = 40

// registrySchemaVersion is the registry.json schema this reader knows
// how to parse. A future schema bump (renamed fields, restructured
// entries) would silently mis-extract from a different shape; failing
// closed when the version doesn't match means a stale binary degrades
// to the HumanName fallback rather than emitting wrong values.
const registrySchemaVersion = 1

// readRegistryDisplayName looks up an API's brand name in the public
// library's registry.json. Returns "" when the env var
// PRINTING_PRESS_LIBRARY_PUBLIC is unset, the file is unreadable, the
// schema_version doesn't match, the slug isn't in the registry, or
// the api field is unusable (empty, equal to the slug, or too long
// to plausibly be a display_name).
//
// Last fallback in the display_name preservation chain: spec.yaml →
// manifest.json → registry.json → naming.HumanName. Filesystem
// scanning belongs in skill prose; the binary trusts the env var.
func readRegistryDisplayName(slug string) string {
	libDir := os.Getenv("PRINTING_PRESS_LIBRARY_PUBLIC")
	if libDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(libDir, "registry.json"))
	if err != nil {
		return ""
	}
	var registry struct {
		SchemaVersion int `json:"schema_version"`
		Entries       []struct {
			Name string `json:"name"`
			API  string `json:"api"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return ""
	}
	if registry.SchemaVersion != registrySchemaVersion {
		return ""
	}
	for _, e := range registry.Entries {
		if e.Name != slug {
			continue
		}
		api := strings.TrimSpace(e.API)
		if api == "" || api == slug || len(api) > registryDisplayNameMaxLen {
			return ""
		}
		return api
	}
	return ""
}

// validateSpecNameMatchesDir refuses to migrate when spec.yaml.name
// diverges from the slug derived from the CLI directory's basename
// (via naming.TrimCLISuffix, which strips both -pp-cli and legacy -cli
// forms). This catches the weather-goat / open-meteo class of drift
// where an old emboss/rename updated the directory but left
// spec.yaml.name behind, producing spurious cmd/<spec.name>-pp-{cli,mcp}/
// directories on regen and a wrong MCP server identity. Caller can pass
// --force to bypass when they know the divergence is intentional
// (e.g., a deliberate alias).
func validateSpecNameMatchesDir(cliDir string, parsed *spec.APISpec) error {
	if parsed == nil || parsed.Name == "" {
		return nil
	}
	expected := naming.TrimCLISuffix(filepath.Base(cliDir))
	if expected == parsed.Name {
		return nil
	}
	return fmt.Errorf(
		"spec.yaml name %q does not match directory-derived slug %q. "+
			"This produces spurious cmd/%s-pp-{cli,mcp}/ directories on regen and an incorrect MCP server identity. "+
			"Fix spec.yaml's `name:` field to match the directory slug, or pass --force to bypass",
		parsed.Name, expected, parsed.Name,
	)
}

// internalSpecNameLine matches the top-level `name:` key in an internal
// YAML spec. Anchored to start of line so it never matches nested keys.
var internalSpecNameLine = regexp.MustCompile(`(?m)^name:[ \t]*.*$`)

// reconcileSpecNameWithDir auto-fixes the common drift case (internal
// YAML spec whose top-level `name:` doesn't match the slug implied by
// the directory) by rewriting the line in place and updating
// parsed.Name. Falls back to validateSpecNameMatchesDir's
// --force-required error for OpenAPI and GraphQL specs, where
// rewriting info.title or schema metadata is too invasive to do
// silently. The non-empty renamedFrom return signals the caller to
// log the rename.
func reconcileSpecNameWithDir(cliDir string, parsed *spec.APISpec) (renamedFrom string, err error) {
	if parsed == nil || parsed.Name == "" {
		return "", nil
	}
	expected := naming.TrimCLISuffix(filepath.Base(cliDir))
	if expected == parsed.Name {
		return "", nil
	}
	specPath, data, ok := findInternalYAMLSpec(cliDir)
	if !ok || !internalSpecNameLine.Match(data) {
		return "", validateSpecNameMatchesDir(cliDir, parsed)
	}
	rewritten := internalSpecNameLine.ReplaceAll(data, []byte("name: "+expected))
	if err := writeFileAtomic(specPath, rewritten); err != nil {
		return "", fmt.Errorf("rewriting %s: %w", specPath, err)
	}
	oldName := parsed.Name
	parsed.Name = expected
	return oldName, nil
}

// findInternalYAMLSpec returns the first existing internal YAML spec
// in cliDir. OpenAPI YAML files are skipped because their identity
// derives from info.title rather than a top-level name field.
func findInternalYAMLSpec(cliDir string) (path string, data []byte, ok bool) {
	for _, candidate := range []string{"spec.yaml", "spec.yml"} {
		p := filepath.Join(cliDir, candidate)
		d, err := os.ReadFile(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", nil, false
		}
		if openapi.IsOpenAPI(d) {
			continue
		}
		return p, d, true
	}
	return "", nil, false
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

// mcpGoModulePath identifies the mcp-go module across regex/lookup
// sites. Single source so the import path stays in lockstep with the
// constant it pairs with.
const mcpGoModulePath = "github.com/mark3labs/mcp-go"

// minMCPGoVersionForCobratree is the floor mark3labs/mcp-go version
// the cobratree-era templates compile against. Pinned to whatever the
// generator's go.mod template currently emits — bump both together.
// TestMinMCPGoVersionMatchesGoModTemplate keeps them in lockstep.
const minMCPGoVersionForCobratree = "v0.47.0"

// ensureMCPGoMinVersion bumps the mark3labs/mcp-go require directive
// in go.mod to minMCPGoVersionForCobratree when the existing pin is
// older. Returns the prior version when a bump happened, the empty
// string when no change was needed (already current, or dep absent).
//
// Older library CLIs predating cobratree pin v0.26.0 or similar; the
// migration block above regenerates MCP source against the cobratree
// templates which call APIs (WithReadOnlyHintAnnotation, GetArguments)
// added in v0.47.0. Without the bump the regenerated CLI fails to
// compile and the sync silently leaves a broken state.
//
// Uses modfile.Parse so a `replace` directive pointing the module at
// a fork or local path is left alone — only the require entry moves.
func ensureMCPGoMinVersion(cliDir string) (priorVersion string, err error) {
	gomodPath := filepath.Join(cliDir, "go.mod")
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}
	mf, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", fmt.Errorf("parsing go.mod: %w", err)
	}
	var current string
	for _, r := range mf.Require {
		if r.Mod.Path == mcpGoModulePath {
			current = r.Mod.Version
			break
		}
	}
	if current == "" {
		return "", nil
	}
	if !semver.IsValid(current) || semver.Compare(current, minMCPGoVersionForCobratree) >= 0 {
		return "", nil
	}
	if err := mf.AddRequire(mcpGoModulePath, minMCPGoVersionForCobratree); err != nil {
		return "", fmt.Errorf("updating mcp-go require: %w", err)
	}
	rewritten, err := mf.Format()
	if err != nil {
		return "", fmt.Errorf("formatting go.mod: %w", err)
	}
	if err := writeFileAtomic(gomodPath, rewritten); err != nil {
		return "", fmt.Errorf("rewriting go.mod: %w", err)
	}
	return current, nil
}

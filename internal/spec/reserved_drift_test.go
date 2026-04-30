package spec_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/cli"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
)

// templateRoot resolves to internal/generator/templates from this test's
// working directory (Go runs each package's tests with cwd == package dir).
const templateRoot = "../generator/templates"

// addCommandRegex matches lines like `rootCmd.AddCommand(newDoctorCmd(...))`
// with a literal constructor identifier. Lines containing template
// expressions (`{{ }}`) are filtered out at the line level so per-resource
// ranges (`new{{camel $name}}Cmd`) do not contaminate the captured set.
var addCommandRegex = regexp.MustCompile(`rootCmd\.AddCommand\(new(\w+)Cmd\(`)

// constructorUseRegex finds a `func new<Name>Cmd(...)` declaration and the
// first `Use:` literal that follows it (the constructor's own top-level
// command Use, not subcommand Use literals which sit inside child
// constructors after their own func declarations).
var constructorUseRegex = regexp.MustCompile(
	`func new(\w+)Cmd[^{]*\{[\s\S]*?Use:\s*"([^"]+)"`,
)

// runtimeBuiltins are cobra runtime commands registered automatically with
// no template source. They appear in ReservedCobraUseNames but the drift
// test cannot derive them from templates.
var runtimeBuiltins = map[string]struct{}{
	"completion": {},
	"help":       {},
}

// cobratreeOnlyNames are entries in cobratree's frameworkCommands set that
// don't correspond to generator-emitted cobra commands (typed MCP tool
// names registered by the MCP walker). They appear in ReservedCobraUseNames
// via the subset rule, not via root.go.tmpl AddCommand.
var cobratreeOnlyNames = map[string]struct{}{
	"about": {},
	"sql":   {},
}

// workflowInsightConstructors are registered by root.go.tmpl via
// `range .WorkflowConstructors` / `range .InsightConstructors` — template
// ranges whose constructor names come from generator's
// commandConstructorForTemplate switch in internal/generator/generator.go.
// The static-AddCommand scan filters out lines containing `{{`, so these
// dynamic registrations need their own list. Update this set in lockstep
// with commandConstructorForTemplate.
var workflowInsightConstructors = map[string]struct{}{
	"Stale":   {},
	"Orphans": {},
	"Load":    {},
	"Health":  {},
	"Similar": {},
}

// TestReservedCobraUseNames_CoversFrameworkConstructors asserts that every
// framework constructor reachable from root.go.tmpl's static AddCommand
// sites has its Use verb in spec.ReservedCobraUseNames. Adding a new
// framework command to root.go.tmpl without an entry here fails this test.
func TestReservedCobraUseNames_CoversFrameworkConstructors(t *testing.T) {
	registered := allFrameworkConstructors(t)
	if len(registered) == 0 {
		t.Fatal("no framework constructors found in root.go.tmpl; regex or path likely wrong")
	}
	useByConstructor := readConstructorUseLiterals(t)

	for _, ctor := range registered {
		use, ok := useByConstructor[ctor]
		if !ok {
			t.Errorf("framework constructor new%sCmd is registered in root.go.tmpl but no `func new%sCmd ... Use:` definition was found in any template — drift test cannot verify the Use verb", ctor, ctor)
			continue
		}
		verb := strings.Fields(use)[0]
		if _, ok := spec.ReservedCobraUseNames[verb]; !ok {
			t.Errorf("framework constructor new%sCmd emits Use %q (verb %q); missing from spec.ReservedCobraUseNames. Add %q to the set in internal/spec/spec.go.", ctor, use, verb, verb)
		}
	}
}

// allFrameworkConstructors returns the union of static AddCommand
// constructors from root.go.tmpl plus the workflow/insight constructors
// registered via dynamic `range` blocks. Both classes are framework
// commands; the only difference is registration site (literal vs. range).
func allFrameworkConstructors(t *testing.T) []string {
	t.Helper()
	seen := make(map[string]struct{})
	for _, c := range readStaticConstructors(t) {
		seen[c] = struct{}{}
	}
	for c := range workflowInsightConstructors {
		seen[c] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// TestReservedCobraUseNames_NoStaleEntries asserts every entry in
// ReservedCobraUseNames is reachable from a known source: a framework
// constructor in root.go.tmpl, a cobra runtime built-in, or cobratree's
// frameworkCommands set. Removing a framework command must also remove
// the entry; this test catches the latter half.
func TestReservedCobraUseNames_NoStaleEntries(t *testing.T) {
	frameworkVerbs := frameworkVerbsFromRoot(t)

	for entry := range spec.ReservedCobraUseNames {
		if _, ok := frameworkVerbs[entry]; ok {
			continue
		}
		if _, ok := runtimeBuiltins[entry]; ok {
			continue
		}
		if _, ok := cobratreeOnlyNames[entry]; ok {
			continue
		}
		t.Errorf("ReservedCobraUseNames entry %q has no source: not a framework constructor verb, not a cobra runtime built-in, not a cobratree-only name. Remove it from spec.go, or document a new carve-out class in this test.", entry)
	}
}

// TestReservedCobraUseNames_CobratreeIsSubset asserts the cobratree
// frameworkCommands set (which the MCP walker uses to skip command
// registration) is a subset of ReservedCobraUseNames. Anything cobratree
// skips at MCP time would shadow at cobra time too.
func TestReservedCobraUseNames_CobratreeIsSubset(t *testing.T) {
	for name := range cli.FrameworkCommands {
		if _, ok := spec.ReservedCobraUseNames[name]; !ok {
			t.Errorf("cli.FrameworkCommands contains %q but spec.ReservedCobraUseNames does not. Add %q to spec.go so cobra collisions are blocked at parse time.", name, name)
		}
	}
}

// TestReservedCobraUseNames_NoSubcommandLeakage pins the negative: common
// subcommand verbs (e.g., `auth login`, `jobs list`) must not appear in
// ReservedCobraUseNames. Including them would falsely block very common
// API resource names. The constructor-resolution mechanism excludes them
// by construction; this test fails loudly if that mechanism regresses.
func TestReservedCobraUseNames_NoSubcommandLeakage(t *testing.T) {
	subcommandVerbs := []string{"login", "logout", "list", "archive", "prune", "refresh", "save", "use"}
	for _, verb := range subcommandVerbs {
		if _, ok := spec.ReservedCobraUseNames[verb]; ok {
			t.Errorf("ReservedCobraUseNames contains %q which is a subcommand verb (e.g., `auth login`, `jobs list`); blocking it would falsely reject very common API resource names.", verb)
		}
	}
}

// staticConstructorsCache and useLiteralsCache memoize the filesystem
// walks across the four drift-detection tests in this file. Without
// them, both readStaticConstructors and readConstructorUseLiterals run
// twice per test invocation (once via the direct test, once via
// frameworkVerbsFromRoot's stale-entry check), doubling the disk I/O.
var (
	staticConstructorsOnce  sync.Once
	staticConstructorsCache []string

	useLiteralsOnce  sync.Once
	useLiteralsCache map[string]string
)

// readStaticConstructors scans root.go.tmpl for literal AddCommand sites
// and returns the constructor names (e.g., "Doctor", "Auth"). Lines
// containing `{{` are skipped so per-resource template ranges do not
// contaminate the result. Memoized — see staticConstructorsCache.
func readStaticConstructors(t *testing.T) []string {
	t.Helper()
	staticConstructorsOnce.Do(func() {
		rootData, err := os.ReadFile(filepath.Join(templateRoot, "root.go.tmpl"))
		if err != nil {
			t.Fatalf("reading root.go.tmpl: %v", err)
		}

		seen := make(map[string]struct{})
		for line := range strings.SplitSeq(string(rootData), "\n") {
			if strings.Contains(line, "{{") {
				continue
			}
			for _, m := range addCommandRegex.FindAllStringSubmatch(line, -1) {
				if _, ok := seen[m[1]]; ok {
					continue
				}
				seen[m[1]] = struct{}{}
				staticConstructorsCache = append(staticConstructorsCache, m[1])
			}
		}
		sort.Strings(staticConstructorsCache)
	})
	return staticConstructorsCache
}

// readConstructorUseLiterals walks every .go.tmpl under templateRoot
// (recursively) and builds a map of constructor name → Use literal. Each
// constructor's first Use after its `func new<Name>Cmd` declaration is
// captured — subcommand Use literals sit inside child constructors after
// their own func declarations, so they're attributed to those children
// rather than leaking into the parent's entry. Memoized — see
// useLiteralsCache.
func readConstructorUseLiterals(t *testing.T) map[string]string {
	t.Helper()
	useLiteralsOnce.Do(func() {
		useLiteralsCache = make(map[string]string)
		err := filepath.WalkDir(templateRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go.tmpl") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range constructorUseRegex.FindAllStringSubmatch(string(data), -1) {
				ctor, use := m[1], m[2]
				if _, exists := useLiteralsCache[ctor]; exists {
					continue
				}
				useLiteralsCache[ctor] = use
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking templates: %v", err)
		}
	})
	return useLiteralsCache
}

// frameworkVerbsFromRoot returns the set of cobra Use verbs derived from
// root.go.tmpl's static AddCommand sites. Used by the stale-entry test to
// know which entries have a framework-constructor source.
func frameworkVerbsFromRoot(t *testing.T) map[string]struct{} {
	t.Helper()
	verbs := make(map[string]struct{})
	registered := allFrameworkConstructors(t)
	useByConstructor := readConstructorUseLiterals(t)
	for _, ctor := range registered {
		use, ok := useByConstructor[ctor]
		if !ok {
			continue
		}
		verbs[strings.Fields(use)[0]] = struct{}{}
	}
	return verbs
}

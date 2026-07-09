package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGenerateLearnPackageEmitsAllFiles verifies the learn package
// emission lands every expected file at the right path under
// internal/learn/. Mirrors the share-emission test pattern.
func TestGenerateLearnPackageEmitsAllFiles(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-emit")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-emit-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	wantFiles := []string{
		"internal/learn/doc.go",
		"internal/learn/normalize.go",
		"internal/learn/normalize_test.go",
		"internal/learn/match.go",
		"internal/learn/match_test.go",
		"internal/learn/recall.go",
		"internal/learn/recall_test.go",
		"internal/learn/teach.go",
		"internal/learn/teach_test.go",
		"internal/learn/teach_log.go",
		"internal/learn/teach_log_test.go",
		"internal/learn/preseed.go",
		"internal/learn/preseed_test.go",
		"internal/learn/playbooks.go",
		"internal/learn/playbooks_test.go",
		"internal/learn/promote.go",
		"internal/learn/promote_test.go",
		// U4: store-layer playbook accessors live in the store package
		// alongside store.go, gated under the same Store vision flag.
		"internal/store/playbooks.go",
		"internal/store/playbooks_test.go",
		"internal/learn/entities/config.go",
		"internal/learn/entities/config_test.go",
		"internal/learn/entities/extract.go",
		"internal/learn/entities/extract_test.go",
		"internal/learn/lookups/store.go",
		"internal/learn/lookups/store_test.go",
		"internal/learn/lookups/seeds.go",
		"internal/learn/lookups/seeds_test.go",
		"internal/learn/patterns/doc.go",
		"internal/learn/patterns/store.go",
		"internal/learn/patterns/store_test.go",
		"internal/learn/patterns/extract.go",
		"internal/learn/patterns/extract_test.go",
		"internal/learn/patterns/apply.go",
		"internal/learn/patterns/apply_test.go",
		// U7: teach.go and teach_test.go land alongside the rest of
		// the cobra command files in internal/cli/ so the learn
		// package itself stays cobra-free.
		"internal/cli/teach.go",
		"internal/cli/teach_test.go",
		// U7 (playbook surface): teach_playbook.go ships the standalone
		// playbook write commands (`teach-playbook`, `playbook list`,
		// `playbook amend`) alongside teach.go. Root.go.tmpl wires the
		// registration in a later unit.
		"internal/cli/teach_playbook.go",
		"internal/cli/teach_playbook_test.go",
		// U9: internal/cli/playbooks/ ships the embed.FS scaffold for
		// hand-authored playbook content (JSON + notes files). MANIFEST.md
		// keeps the //go:embed *.json *.md directive matching at least
		// one file when no authored content has shipped yet, so the
		// package compiles cleanly on a fresh print. The auto-install
		// path that walks this FS is owned by a later unit.
		"internal/cli/playbooks/embed.go",
		"internal/cli/playbooks/MANIFEST.md",
		// U10: playbook_init.go is the embed-FS auto-install path.
		// Walks playbooks.FS at first DB open, seeds learning_playbooks
		// under each query family derived from query_family_examples,
		// and tracks SeedVersion in a sentinel row. The test file
		// injects an fstest.MapFS so scenarios are independent of the
		// authored playbook content shipped under cli/playbooks/.
		"internal/cli/playbook_init.go",
		"internal/cli/playbook_init_test.go",
	}
	for _, rel := range wantFiles {
		_, err := os.Stat(filepath.Join(outputDir, rel))
		require.NoError(t, err, "expected emitted file %s", rel)
	}
}

// TestGenerateLearnPackageGatedOff verifies the learn package files do
// NOT emit when Learn.Enabled is false; pairs with the store-side gate
// already covered in learn_store_test.go.
func TestGenerateLearnPackageGatedOff(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-gated")
	// Post-flip: opt out so this test exercises the non-learn shape it asserts.
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-gated-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	_, err := os.Stat(filepath.Join(outputDir, "internal", "learn"))
	require.True(t, os.IsNotExist(err), "internal/learn must not exist when Learn.Enabled=false")

	// U7: teach.go in internal/cli/ is also gated off.
	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "teach.go"))
	require.True(t, os.IsNotExist(err), "internal/cli/teach.go must not exist when Learn.Enabled=false")

	// U7 (playbook surface): teach_playbook.go is gated off too.
	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "teach_playbook.go"))
	require.True(t, os.IsNotExist(err), "internal/cli/teach_playbook.go must not exist when Learn.Enabled=false")

	// U9: cli/playbooks/ embed.FS scaffold is gated off too.
	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "playbooks"))
	require.True(t, os.IsNotExist(err), "internal/cli/playbooks must not exist when Learn.Enabled=false")

	// U10: playbook_init.go is gated off too.
	_, err = os.Stat(filepath.Join(outputDir, "internal", "cli", "playbook_init.go"))
	require.True(t, os.IsNotExist(err), "internal/cli/playbook_init.go must not exist when Learn.Enabled=false")
}

// TestGenerateLearnPackageCompilesAndTests drives the emitted learn
// package through `go test ./internal/learn/...` to catch any template
// issue that produces shape-valid but uncompilable Go, plus any
// behavior regression in the ported logic.
func TestGenerateLearnPackageCompilesAndTests(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compile-and-test of emitted learn package skipped in -short mode")
	}

	apiSpec := minimalSpec("learn-built")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-built-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	runGoCommand(t, outputDir, "test", "./internal/learn/...")
}

// TestGenerateLearnCLICommandsCompileAndTest drives the emitted cobra
// surface (teach.go, teach_test.go) through `go test ./internal/cli/...`
// to catch any template issue that produces shape-valid but
// uncompilable Go, plus any behavior regression in the lifted command
// surface.
func TestGenerateLearnCLICommandsCompileAndTest(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compile-and-test of emitted learn cobra commands skipped in -short mode")
	}

	apiSpec := minimalSpec("learn-cli")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-cli-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	// The TestSkipLearnHook_* unit tests + the end-to-end teach/recall
	// tests all live in internal/cli/teach_test.go; running the whole
	// internal/cli/ test set is the agreed-upon verification path per
	// the U7 plan.
	//
	// U11 wired runPlaybookInitOnce + newTeachPlaybookCmd + newPlaybookCmd
	// into root.go.tmpl, so the TestTeachPlaybook_* and TestPlaybook*
	// tests from teach_playbook_test.go.tmpl now run alongside the rest.
	//
	// TestPlaybookInit_* (from playbook_init_test.go.tmpl) tests call
	// installPlaybooksFromEmbed directly with an injected fstest.MapFS,
	// so they don't depend on cobra registration and are included in
	// the filter.
	runGoCommand(t, outputDir, "test", "-run", "^(TestTeach|TestRecall|TestLearnings|TestSkipLearnHook|TestNewLearnConfig|TestInitLearn|TestPlaybook)", "./internal/cli/...")
}

// TestGenerateLearnInitWiresSpec verifies that the emitted learn_init.go
// translates a populated spec.Learn block (ticker patterns + stopwords +
// entity-lookup seeds) into the corresponding Go literals at the right
// call sites. Asserts the textual contract; the compile + behavior
// contract is covered by TestGenerateLearnCLICommandsCompileAndTest's
// run of the emitted CLI's own init tests.
func TestGenerateLearnInitWiresSpec(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-wired")
	apiSpec.Learn.Enabled = true
	apiSpec.Learn.TickerPatterns = []string{
		"^WIDGET-[A-Z0-9]+$",
	}
	apiSpec.Learn.Stopwords = []string{"odds", "wins"}
	apiSpec.Learn.EntityLookupSeeds = map[string][]spec.LookupSeed{
		"country": {
			{Canonical: "US", Aliases: []string{"USA", "America"}},
		},
	}
	outputDir := filepath.Join(t.TempDir(), "learn-wired-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	body, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "learn_init.go"))
	require.NoError(t, err)
	got := string(body)

	for _, want := range []string{
		"regexp.Compile(`^WIDGET-[A-Z0-9]+$`)",
		`cfg.RegisterTickerPattern(re)`,
		`cfg.RegisterStopwords(`,
		`"odds"`,
		`"wins"`,
		`map[string][]lookups.SeedConfig`,
		`"country"`,
		`{Canonical: "US", Values: []string{"USA", "America"}}`,
		`lookups.SeedFromConfig(db, seeds)`,
		`learnInitOnce`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("learn_init.go missing %q\n--- emitted ---\n%s", want, got)
		}
	}
}

// TestGenerateLearnInitEmptyConfigOmitsImports verifies the import gate:
// when Learn is enabled but no ticker patterns / seeds are declared,
// the emitted file must not pull in unused regexp / lookups imports
// that would fail the printed CLI's `go build`.
func TestGenerateLearnInitEmptyConfigOmitsImports(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-empty")
	apiSpec.Learn.Enabled = true
	// No TickerPatterns, Stopwords, or EntityLookupSeeds.
	outputDir := filepath.Join(t.TempDir(), "learn-empty-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	body, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "learn_init.go"))
	require.NoError(t, err)
	got := string(body)

	if strings.Contains(got, `"regexp"`) {
		t.Errorf("learn_init.go must not import regexp when no TickerPatterns declared\n--- emitted ---\n%s", got)
	}
	if strings.Contains(got, `/learn/lookups"`) {
		t.Errorf("learn_init.go must not import lookups when no EntityLookupSeeds declared\n--- emitted ---\n%s", got)
	}
}

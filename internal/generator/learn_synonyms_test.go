package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// extractStringMapLiteral pulls the entries of a `map[string]string{...}`
// literal that follows the given variable declaration in emitted Go
// source. Used to compare the default synonym maps the two normalizers
// ship, which must stay byte-identical because internal/store cannot
// import internal/learn/entities.
func extractStringMapLiteral(t *testing.T, src, varName string) map[string]string {
	t.Helper()
	start := strings.Index(src, varName)
	require.NotEqual(t, -1, start, "declaration %s not found in emitted source", varName)
	open := strings.Index(src[start:], "{")
	require.NotEqual(t, -1, open, "map literal for %s not found", varName)
	body := src[start+open:]
	end := strings.Index(body, "}")
	require.NotEqual(t, -1, end, "map literal for %s not closed", varName)
	body = body[:end]

	entryRe := regexp.MustCompile(`"((?:[^"\\]|\\.)*)":\s*"((?:[^"\\]|\\.)*)"`)
	out := map[string]string{}
	for _, m := range entryRe.FindAllStringSubmatch(body, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// TestGenerateLearnSynonymDefaultMapsMatch pins the two-normalizer
// contract at the emitted-source level: the read-side default synonym
// map (internal/learn/entities/config.go) and the write-side default
// map (internal/store/learnings.go) must carry identical entries, and
// both must include the canonical same-referent example.
func TestGenerateLearnSynonymDefaultMapsMatch(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-syn-defaults")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-syn-defaults-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	entitiesSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "learn", "entities", "config.go"))
	require.NoError(t, err)
	storeSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "store", "learnings.go"))
	require.NoError(t, err)

	readSide := extractStringMapLiteral(t, string(entitiesSrc), "defaultSynonyms")
	writeSide := extractStringMapLiteral(t, string(storeSrc), "defaultQuerySynonyms")

	require.NotEmpty(t, readSide, "read-side default synonym map must not be empty")
	require.Equal(t, readSide, writeSide,
		"read-side and write-side default synonym maps drifted; they must stay identical")

	require.Equal(t, "yesterday", readSide["last night"],
		"default map must fold the same-referent pair last night -> yesterday")
	// Same-referent only: nothing may fold across a day boundary.
	require.NotContains(t, readSide, "tonight", "tonight must never be a fold variant")
	for variant, canonical := range readSide {
		if variant != "last night" {
			require.NotEqual(t, "yesterday", canonical,
				"fold %q -> %q crosses a day boundary", variant, canonical)
		}
	}
}

// TestGenerateLearnSynonymsWiresSpec verifies spec-declared synonyms
// are stamped through BOTH registration sides in the emitted
// learn_init.go: entities.Config.RegisterSynonyms (read) and
// store.RegisterQuerySynonyms (write).
func TestGenerateLearnSynonymsWiresSpec(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-syn-wired")
	apiSpec.Learn.Enabled = true
	apiSpec.Learn.Synonyms = map[string]string{
		"foo bar": "baz",
		"qux":     "quux",
	}
	outputDir := filepath.Join(t.TempDir(), "learn-syn-wired-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	body, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "learn_init.go"))
	require.NoError(t, err)
	got := string(body)

	for _, want := range []string{
		"cfg.RegisterSynonyms(learnSynonyms)",
		"store.RegisterQuerySynonyms(learnSynonyms)",
		`"foo bar": "baz"`,
		`"qux":     "quux"`,
	} {
		require.Contains(t, got, want, "learn_init.go missing %q", want)
	}
}

// TestGenerateLearnSynonymsOmittedWhenUndeclared pins that a spec with
// no synonyms block emits no registration calls (defaults still apply
// inside each package; there is just nothing per-CLI to stamp).
func TestGenerateLearnSynonymsOmittedWhenUndeclared(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-syn-absent")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learn-syn-absent-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	body, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "learn_init.go"))
	require.NoError(t, err)
	got := string(body)

	require.NotContains(t, got, "RegisterSynonyms",
		"learn_init.go must not emit synonym registration when the spec declares none")
	require.NotContains(t, got, "RegisterQuerySynonyms",
		"learn_init.go must not emit store synonym registration when the spec declares none")
}

// TestGenerateLearnSynonymsCompileAndTest drives the emitted synonym
// behavior end to end in a generated CLI with spec-declared synonyms:
// the learn package tests (fold + dual-key playbook lookup + lazy
// rekey), the store tests (write-side fold + teach/recall symmetry),
// and the cli-package symmetry tests that exercise the stamped
// registration chain.
func TestGenerateLearnSynonymsCompileAndTest(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compile-and-test of emitted synonym folding skipped in -short mode")
	}

	apiSpec := minimalSpec("learn-syn-e2e")
	apiSpec.Learn.Enabled = true
	apiSpec.Learn.Synonyms = map[string]string{"foo bar": "baz"}
	outputDir := filepath.Join(t.TempDir(), "learn-syn-e2e-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	runGoCommand(t, outputDir, "test", "./internal/learn/...")
	// Full store suite: the write-side fold lives inside NormalizeQuery,
	// so every learnings/playbooks store test doubles as a regression
	// guard that folding didn't disturb non-synonym normalization.
	runGoCommand(t, outputDir, "test", "./internal/store")
	runGoCommand(t, outputDir, "test", "-run",
		"TestLearnNormalizers_SynonymFoldSymmetry|TestLearnConfig_SpecSynonymsRegisteredBothSides",
		"./internal/cli")
}

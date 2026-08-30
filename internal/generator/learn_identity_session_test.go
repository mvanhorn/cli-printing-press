package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func identifierLearnSpec(name string) *spec.APISpec {
	apiSpec := incidentReplaySpec(name)
	apiSpec.Learn.TickerPatterns = []string{
		`^EXAMPLE-[A-Z0-9]+(-[A-Z0-9]+)*$`,
		`^\d{1,3}(\.\d{1,3}){3}$`,
	}
	list := apiSpec.Resources["items"].Endpoints["list"]
	list.IDField = "id"
	res := apiSpec.Resources["items"]
	res.Endpoints["list"] = list
	apiSpec.Resources["items"] = res
	return apiSpec
}

// TestGenerateLearnIdentityAndSessionContract asserts the emitted
// teach/recall/journal/init files carry the identity-fold, identity-field
// map, ticker registration, and hashed harness session helpers, then
// compiles the generated module and runs the identifier-only + journal
// session tests against that print.
func TestGenerateLearnIdentityAndSessionContract(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("generated-output compile-and-test skipped in -short mode")
	}

	apiSpec := identifierLearnSpec("lident")
	outputDir := filepath.Join(t.TempDir(), "lident-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	normalize := readEmitted(t, outputDir, "internal", "learn", "normalize.go")
	require.Contains(t, normalize, "func QueryIdentityEntities(")

	recall := readEmitted(t, outputDir, "internal", "learn", "recall.go")
	require.Contains(t, recall, "queryIdentity := QueryIdentityEntities(normalized)")
	require.Contains(t, recall, "entitySlicesIntersect(queryIdentity, storedEntitySlice)")
	require.Contains(t, recall, "identifierOnlyScore(")
	require.Contains(t, recall, "leftoverTokensAreIdentityFragments(")

	teachCLI := readEmitted(t, outputDir, "internal", "cli", "teach.go")
	require.Contains(t, teachCLI, "learn.QueryIdentityEntities(normalized)")
	require.Contains(t, teachCLI, "learnIdentityFieldsFor(resourceType)")
	require.Contains(t, teachCLI, "ResourceTypeFields: learnResourceTypeFields()")

	initSrc := readEmitted(t, outputDir, "internal", "cli", "learn_init.go")
	require.Contains(t, initSrc, "store.RegisterTickerPatterns(tickerPatterns)")
	require.Contains(t, initSrc, `func learnResourceTypeFields()`)
	require.Contains(t, initSrc, `"items":`)
	require.Contains(t, initSrc, `"id"`)

	journal := readEmitted(t, outputDir, "internal", "learn", "journal.go")
	require.Contains(t, journal, "JournalHarnessSessionEnvVars")
	require.Contains(t, journal, `"h:"`)
	require.Contains(t, journal, "CODEX_SESSION_ID")
	requireGeneratedCompiles(t, outputDir)

	runGoCommand(t, outputDir, "test",
		"-run", "^(TestQueryIdentityEntities_|TestRecall_IdentifierOnlyTickerFindsRow|TestRecall_SameIdentifierDifferentIntentDoesNotSkipDiscovery|TestLeftoverTokensAreIdentityFragments|TestIdentifierOnlyScore_IncludesLeftoverIntent|TestValidateResourceShape_Ticker|TestNormalizeQuery_KeepsRegisteredTickersWhole|TestJournal_HarnessSessionIsHashed|TestJournal_LearnSessionOverridesHarness|TestTeachRecall_IdentifierOnlyFindsRow|TestLearnNormalizers_TickerKeepSymmetry|TestLearnIdentityFields_CommonFallback)$",
		"./internal/learn/...", "./internal/cli/...", "./internal/store/...")
}

func TestGenerateLearnIdentityEmitsPerResourceFields(t *testing.T) {
	t.Parallel()

	apiSpec := identifierLearnSpec("lfields")
	outputDir := filepath.Join(t.TempDir(), "lfields-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	initSrc := readEmitted(t, outputDir, "internal", "cli", "learn_init.go")
	require.Contains(t, initSrc, `"items": {`)
	// IDField leads the list, then the common identity keys.
	idxItems := strings.Index(initSrc, `"items": {`)
	require.GreaterOrEqual(t, idxItems, 0)
	window := initSrc[idxItems : idxItems+200]
	require.Contains(t, window, `"id"`)
	require.Contains(t, window, `"name"`)
}

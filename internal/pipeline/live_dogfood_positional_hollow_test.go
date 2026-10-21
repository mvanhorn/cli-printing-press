package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPositionalFixtureSkipReason(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", positionalFixtureSkipReason(""))
	assert.Equal(t, reasonRequiredParamFixture, positionalFixtureSkipReason(reasonRequiredParamFixture))
	wrapped := positionalFixtureSkipReason(`non-id positional "query" at depth 0`)
	assert.Equal(t, `non-id positional "query" at depth 0; set pp:happy-args`, wrapped)
	assert.Equal(t, wrapped, positionalFixtureSkipReason(wrapped))
}

func TestHappyArgsAreFlagOnly(t *testing.T) {
	t.Parallel()

	assert.False(t, happyArgsAreFlagOnly(liveDogfoodCommand{}))
	assert.False(t, happyArgsAreFlagOnly(liveDogfoodCommand{Annotations: map[string]string{happyArgsAnnotation: "   "}}))
	assert.False(t, happyArgsAreFlagOnly(liveDogfoodCommand{Annotations: map[string]string{happyArgsAnnotation: "not-a-fixture"}}))
	assert.False(t, happyArgsAreFlagOnly(liveDogfoodCommand{Annotations: map[string]string{happyArgsAnnotation: "item=1;--json"}}))
	assert.True(t, happyArgsAreFlagOnly(liveDogfoodCommand{Annotations: map[string]string{happyArgsAnnotation: "--commune=ixelles;--type=apartment"}}))
	assert.True(t, happyArgsAreFlagOnly(liveDogfoodCommand{Annotations: map[string]string{happyArgsAnnotation: "--json"}}))
}

func TestOmitOptionalPositionals(t *testing.T) {
	t.Parallel()

	yield := liveDogfoodCommand{
		Path: []string{"yield"},
		Help: joinHelp("yield [id-or-url]", `
      --type string      Type
      --commune string   Commune
  -l, --limit int        Max rows
      --json             Output JSON
`),
	}
	nested := liveDogfoodCommand{
		Path: []string{"widgets", "get"},
		Help: joinHelp("widgets get <widget-id> [note]", `
      --limit int   Max rows
`),
	}

	tests := []struct {
		name    string
		command liveDogfoodCommand
		args    []string
		want    []string
	}{
		{
			name:    "drops example optional and keeps spaced flags",
			command: yield,
			args:    []string{"yield", "listing-1", "--commune", "ixelles", "--type", "apartment"},
			want:    []string{"yield", "--commune", "ixelles", "--type", "apartment"},
		},
		{
			name:    "shorthand value is not a positional",
			command: yield,
			args:    []string{"yield", "listing-1", "-l", "5"},
			want:    []string{"yield", "-l", "5"},
		},
		{
			name:    "attached shorthand value does not eat the next arg",
			command: yield,
			args:    []string{"yield", "-l5", "listing-1"},
			want:    []string{"yield", "-l5"},
		},
		{
			name:    "unknown shorthand does not eat the next arg",
			command: yield,
			args:    []string{"yield", "-q", "listing-1", "--type", "apartment"},
			want:    []string{"yield", "-q", "--type", "apartment"},
		},
		{
			name:    "boolean flag does not eat the optional positional",
			command: yield,
			args:    []string{"yield", "--json", "listing-1"},
			want:    []string{"yield", "--json"},
		},
		{
			name:    "equals flag keeps a later extra positional",
			command: yield,
			args:    []string{"yield", "listing-1", "--type=apartment", "extra"},
			want:    []string{"yield", "--type=apartment", "extra"},
		},
		{
			name:    "required positional stays and optional is dropped",
			command: nested,
			args:    []string{"widgets", "get", "123", "note", "--limit", "1"},
			want:    []string{"widgets", "get", "123", "--limit", "1"},
		},
		{
			name:    "flag between required and optional",
			command: nested,
			args:    []string{"widgets", "get", "123", "--limit", "1", "note"},
			want:    []string{"widgets", "get", "123", "--limit", "1"},
		},
		{
			name:    "terminator keeps a later required-shaped extra",
			command: nested,
			args:    []string{"widgets", "get", "123", "note", "--", "kept"},
			want:    []string{"widgets", "get", "123", "--", "kept"},
		},
		{
			name:    "no optional specs leaves args unchanged",
			command: liveDogfoodCommand{Path: []string{"bare"}, Help: joinHelp("bare <dimension>", "")},
			args:    []string{"bare", "example-value"},
			want:    []string{"bare", "example-value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, omitOptionalPositionals(tt.command, tt.args))
		})
	}
}

func TestResolveCommandPositionalsFlagOnlyOmitsOptionalExample(t *testing.T) {
	t.Parallel()
	ctx := emptyResolveCtx()

	yield := liveDogfoodCommand{
		Path:        []string{"yield"},
		Help:        joinHelp("yield [id-or-url]", "      --commune string   Commune\n      --type string      Type\n"),
		Annotations: map[string]string{happyArgsAnnotation: "--commune=ixelles;--type=apartment"},
	}
	args, skipped, reason, _ := resolveCommandPositionals(yield, []string{"yield", "listing-1", "--commune", "ixelles", "--type", "apartment"}, 0, ctx)
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"yield", "--commune", "ixelles", "--type", "apartment"}, args)

	triage := liveDogfoodCommand{
		Path:        []string{"triage"},
		Help:        joinHelp("triage [saved-search]", "      --type string   Type\n"),
		Annotations: map[string]string{happyArgsAnnotation: "--type=apartment"},
	}
	args, skipped, reason, _ = resolveCommandPositionals(triage, []string{"triage", "--type", "apartment"}, 0, ctx)
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"triage", "--type", "apartment"}, args)

	bare := liveDogfoodCommand{
		Path:        []string{"bare"},
		Help:        joinHelp("bare <dimension>", "      --json   Output JSON\n"),
		Annotations: map[string]string{happyArgsAnnotation: "--json"},
	}
	_, skipped, reason, _ = resolveCommandPositionals(bare, []string{"bare", "example-value", "--json"}, 0, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, `non-id positional "dimension"`)
	assert.Contains(t, reason, happyArgsFixtureHint)
	assert.NotContains(t, reason, "fewer segments")
}

func TestResolveCommandPositionalsBlockedFixtureStaysExact(t *testing.T) {
	t.Parallel()
	ctx := emptyResolveCtx()
	cmd := liveDogfoodCommand{
		Path: []string{"widgets", "get"},
		Help: joinHelp("widgets get <id>", ""),
	}
	_, skipped, reason, _ := resolveCommandPositionals(cmd, []string{"widgets", "get", "example-value"}, 0, ctx)
	assert.True(t, skipped)
	assert.Equal(t, reasonRequiredParamFixture, reason)
}

func TestResolveLiveDogfoodHappyArgsFrameworkAndFixtures(t *testing.T) {
	t.Parallel()
	ctx := emptyResolveCtx()

	which := liveDogfoodCommand{
		Path: []string{"which"},
		Help: joinHelp("which [query]", "      --json   Output JSON\n"),
	}
	args, skipped, reason, _ := resolveLiveDogfoodHappyArgs(which, []string{"which", "stale tickets"}, 0, ctx)
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"which"}, args)

	which.Annotations = map[string]string{happyArgsAnnotation: "query=stale tickets"}
	args, skipped, reason, _ = resolveLiveDogfoodHappyArgs(which, []string{"which", "stale tickets"}, 1, ctx)
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"which", "stale tickets"}, args)

	whichRequired := liveDogfoodCommand{
		Path: []string{"which"},
		Help: joinHelp("which <id>", ""),
	}
	_, skipped, reason, _ = resolveLiveDogfoodHappyArgs(whichRequired, []string{"which", "example-value"}, 0, ctx)
	assert.True(t, skipped)
	assert.Equal(t, reasonRequiredParamFixture, reason)

	whichNamed := liveDogfoodCommand{
		Path: []string{"which"},
		Help: joinHelp("which <name>", ""),
	}
	_, skipped, reason, _ = resolveLiveDogfoodHappyArgs(whichNamed, []string{"which", "mine"}, 0, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, `non-id positional "name"`)
	assert.Contains(t, reason, happyArgsFixtureHint)

	tail := liveDogfoodCommand{
		Path: []string{"tail"},
		Help: joinHelp("tail [resource]", "      --json   Output JSON\n"),
	}
	args, skipped, reason, _ = resolveLiveDogfoodHappyArgs(tail, []string{"tail", "items"}, 0, ctx)
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"tail", "--json"}, args)

	tail.Annotations = map[string]string{happyArgsAnnotation: "resource=items;--follow=false"}
	args, skipped, reason, _ = resolveLiveDogfoodHappyArgs(tail, []string{"tail", "items", "--follow=false"}, 1, ctx)
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"tail", "items", "--follow=false"}, args)

	tailFlagOnly := liveDogfoodCommand{
		Path:        []string{"tail"},
		Help:        joinHelp("tail [resource]", "      --json    Output JSON\n      --follow  Follow\n"),
		Annotations: map[string]string{happyArgsAnnotation: "--follow=false"},
	}
	args, skipped, reason, _ = resolveLiveDogfoodHappyArgs(tailFlagOnly, []string{"tail", "items", "--follow=false"}, 0, ctx)
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"tail", "--follow=false"}, args)

	tailRequired := liveDogfoodCommand{
		Path: []string{"tail"},
		Help: joinHelp("tail <resource>", "      --json   Output JSON\n"),
	}
	_, skipped, reason, _ = resolveLiveDogfoodHappyArgs(tailRequired, []string{"tail", "items"}, 0, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, `non-id positional "resource"`)
	assert.Contains(t, reason, happyArgsFixtureHint)

	tailNoJSON := liveDogfoodCommand{
		Path: []string{"tail"},
		Help: joinHelp("tail [resource]", ""),
	}
	_, skipped, reason, _ = resolveLiveDogfoodHappyArgs(tailNoJSON, []string{"tail", "items"}, 0, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, happyArgsFixtureHint)

	dossier := liveDogfoodCommand{
		Path:        []string{"dossier"},
		Help:        joinHelp("dossier <codcom-o-nome>", ""),
		Annotations: map[string]string{happyArgsAnnotation: "codcom-o-nome=roma"},
	}
	args, skipped, reason, _ = resolveLiveDogfoodHappyArgs(dossier, []string{"dossier", "roma"}, 1, ctx)
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"dossier", "roma"}, args)

	swap := liveDogfoodCommand{
		Path:        []string{"swap"},
		Help:        joinHelp("swap <from-set> <to-set>", ""),
		Annotations: map[string]string{happyArgsAnnotation: "from-set=mdi;to-set=lucide"},
	}
	args, skipped, reason, _ = resolveLiveDogfoodHappyArgs(swap, []string{"swap", "mdi", "lucide"}, 2, ctx)
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"swap", "mdi", "lucide"}, args)

	mismatch := liveDogfoodCommand{
		Path: []string{"swap"},
		Help: joinHelp("swap <from-set> <to-set>", ""),
	}
	_, skipped, reason, _ = resolveLiveDogfoodHappyArgs(mismatch, []string{"swap", "mdi", "lucide"}, 0, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, "fewer segments than placeholders")
	assert.Contains(t, reason, happyArgsFixtureHint)

	equalCount := liveDogfoodCommand{
		Path: []string{"legge", "cronologia"},
		Help: joinHelp("legge cronologia <legisl> <numero>", ""),
	}
	_, skipped, reason, _ = resolveLiveDogfoodHappyArgs(equalCount, []string{"legge", "cronologia", "18", "1153"}, 0, ctx)
	assert.True(t, skipped)
	assert.Contains(t, reason, `non-id positional "legisl"`)
	assert.NotContains(t, reason, "fewer segments than placeholders")
	assert.Contains(t, reason, happyArgsFixtureHint)
}

func TestFinalizeLiveDogfoodCoveragePositionalHollowAndFrameworkExempt(t *testing.T) {
	researchDir := t.TempDir()
	require.NoError(t, writeResearchJSON(&ResearchResult{
		NovelFeatures: []NovelFeature{
			{Name: "Ghost", Command: "ghost"},
			{Name: "Bare", Command: "bare"},
			{Name: "Save", Command: "save"},
			{Name: "Search", Command: "search"},
			{Name: "Which", Command: "which"},
			{Name: "Profile save", Command: "profile save"},
			{Name: "Profile delete", Command: "profile delete"},
			{Name: "Yield", Command: "yield"},
			{Name: "Render", Command: "render"},
			{Name: "Preview eq", Command: "preview-eq"},
			{Name: "Live f", Command: "live-f"},
			{Name: "Unbuilt tail", Command: "tail"},
		},
	}, researchDir))

	bareReason := positionalFixtureSkipReason(`non-id positional "dimension" at depth 0`)
	saveReason := positionalFixtureSkipReason(`non-id positional "name" at depth 0`)
	searchReason := positionalFixtureSkipReason(`non-id positional "query" at depth 0`)
	report := &LiveDogfoodReport{
		Commands: []string{"bare", "save", "search", "which", "profile save", "profile delete", "yield", "render", "preview-eq", "live-f"},
		Tests: []LiveDogfoodTestResult{
			{Command: "bare", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusSkip, Reason: bareReason},
			{Command: "save", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusSkip, Reason: saveReason},
			{Command: "search", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusSkip, Reason: searchReason},
			{Command: "which", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusSkip, Reason: bareReason},
			{Command: "profile save", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusSkip, Reason: saveReason},
			{Command: "profile delete", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusSkip, Reason: saveReason},
			{Command: "yield", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusPass, Args: []string{"yield", "--type", "apartment"}},
			{Command: "render", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusPass, Args: []string{"render", "--dry-run"}},
			{Command: "preview-eq", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusPass, Args: []string{"preview-eq", "--dry-run=true"}},
			{Command: "live-f", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusPass, Args: []string{"live-f", "--dry-run=f"}},
		},
	}
	finalizeLiveDogfoodCoverage(report, researchDir, liveDogfoodProofContext{})

	assert.Equal(t, []string{"bare", "ghost", "preview-eq", "render", "save", "search", "tail"}, report.HollowFeatures)
	assert.Equal(t, bareReason, report.HollowReasons["bare"])
	assert.Equal(t, saveReason, report.HollowReasons["save"])
	assert.Equal(t, searchReason, report.HollowReasons["search"])
	assert.NotContains(t, report.HollowReasons, "ghost")
	assert.NotContains(t, report.HollowReasons, "render")
	assert.NotContains(t, report.HollowReasons, "preview-eq")
	assert.NotContains(t, report.HollowReasons, "tail")
	assert.NotContains(t, report.HollowReasons, "which")
	assert.NotContains(t, report.HollowFeatures, "which")
	assert.NotContains(t, report.HollowFeatures, "profile save")
	assert.NotContains(t, report.HollowFeatures, "profile delete")
	assert.NotContains(t, report.HollowFeatures, "yield")
	assert.NotContains(t, report.HollowFeatures, "live-f")
	assert.True(t, report.CoverageHollow)
}

func TestWriteLiveDogfoodAcceptanceCopiesHollowReasons(t *testing.T) {
	dir := t.TempDir()
	writeTestManifestForLiveDogfood(t, dir)
	markerPath := filepath.Join(t.TempDir(), Phase5AcceptanceFilename)
	reason := positionalFixtureSkipReason(`non-id positional "dimension" at depth 0`)
	report := &LiveDogfoodReport{
		Level:          "full",
		Verdict:        "PASS",
		MatrixSize:     1,
		Passed:         1,
		CoverageHollow: true,
		HollowFeatures: []string{"bare"},
		HollowReasons:  map[string]string{"bare": reason},
	}
	require.NoError(t, writeLiveDogfoodAcceptance(LiveDogfoodOptions{
		CLIDir:              dir,
		WriteAcceptancePath: markerPath,
	}, report, SourceFingerprint{Digest: "abc", Files: map[string]string{"go.mod": "x"}}))

	data, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, map[string]string{"bare": reason}, marker.HollowReasons)
	passed, detail := phase5AcceptancePassed(marker)
	assert.False(t, passed)
	assert.Contains(t, detail, "bare")
	assert.Contains(t, detail, happyArgsFixtureHint)
}

func TestLiveDogfoodPositionalHollowMatrix(t *testing.T) {
	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	researchDir := t.TempDir()
	require.NoError(t, writeResearchJSON(&ResearchResult{
		NovelFeatures: []NovelFeature{
			{Command: "triage"},
			{Command: "yield"},
			{Command: "dossier"},
			{Command: "ddl iter"},
			{Command: "legge cronologia"},
			{Command: "swap"},
			{Command: "bare"},
			{Command: "which"},
			{Command: "tail"},
			{Command: "api"},
			{Command: "import"},
			{Command: "profile save"},
			{Command: "profile show"},
			{Command: "profile use"},
			{Command: "learnings forget"},
			{Command: "learnings confirm"},
			{Command: "learnings reject"},
			{Command: "save"},
		},
	}, researchDir))
	writeStubBinary(t, dir, binaryName, positionalHollowStub(positionalHollowAgentContext()))

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:      dir,
		BinaryName:  binaryName,
		Level:       "full",
		Timeout:     5 * time.Second,
		ResearchDir: researchDir,
	})
	require.NoError(t, err)
	require.Zero(t, report.Failed, liveDogfoodFailureSummary(report))

	yield := findResultByCommandKind(report, "yield", LiveDogfoodTestHappy)
	require.NotNil(t, yield)
	assert.Equal(t, LiveDogfoodStatusPass, yield.Status, yield.Reason)
	assert.Equal(t, []string{"yield", "--commune", "ixelles", "--type", "apartment"}, yield.Args)

	triage := findResultByCommandKind(report, "triage", LiveDogfoodTestHappy)
	require.NotNil(t, triage)
	assert.Equal(t, LiveDogfoodStatusPass, triage.Status, triage.Reason)
	assert.Equal(t, []string{"triage", "--type", "apartment", "--deal", "rent", "--commune", "ixelles"}, triage.Args)

	dossier := findResultByCommandKind(report, "dossier", LiveDogfoodTestHappy)
	require.NotNil(t, dossier)
	assert.Equal(t, LiveDogfoodStatusPass, dossier.Status, dossier.Reason)
	assert.Equal(t, []string{"dossier", "roma"}, dossier.Args)

	iter := findResultByCommandKind(report, "ddl iter", LiveDogfoodTestHappy)
	require.NotNil(t, iter)
	assert.Equal(t, LiveDogfoodStatusPass, iter.Status, iter.Reason)
	assert.Equal(t, []string{"ddl", "iter", "18", "1153"}, iter.Args)

	swap := findResultByCommandKind(report, "swap", LiveDogfoodTestHappy)
	require.NotNil(t, swap)
	assert.Equal(t, LiveDogfoodStatusPass, swap.Status, swap.Reason)
	assert.Equal(t, []string{"swap", "mdi", "lucide", "--icons", "home,cog"}, swap.Args)

	api := findResultByCommandKind(report, "api", LiveDogfoodTestHappy)
	require.NotNil(t, api)
	assert.Equal(t, LiveDogfoodStatusPass, api.Status, api.Reason)
	assert.Equal(t, []string{"api"}, api.Args)

	which := findResultByCommandKind(report, "which", LiveDogfoodTestHappy)
	require.NotNil(t, which)
	assert.Equal(t, LiveDogfoodStatusPass, which.Status, which.Reason)
	assert.Equal(t, []string{"which"}, which.Args)

	tail := findResultByCommandKind(report, "tail", LiveDogfoodTestHappy)
	require.NotNil(t, tail)
	assert.Equal(t, LiveDogfoodStatusPass, tail.Status, tail.Reason)
	assert.Equal(t, []string{"tail", "items", "--follow=false"}, tail.Args)

	bare := findResultByCommandKind(report, "bare", LiveDogfoodTestHappy)
	require.NotNil(t, bare)
	assert.Equal(t, LiveDogfoodStatusSkip, bare.Status)
	assert.Contains(t, bare.Reason, `non-id positional "dimension"`)
	assert.Contains(t, bare.Reason, happyArgsFixtureHint)

	cronologia := findResultByCommandKind(report, "legge cronologia", LiveDogfoodTestHappy)
	require.NotNil(t, cronologia)
	assert.Equal(t, LiveDogfoodStatusSkip, cronologia.Status)
	assert.Contains(t, cronologia.Reason, `non-id positional "legisl"`)
	assert.NotContains(t, cronologia.Reason, "fewer segments than placeholders")
	assert.Contains(t, cronologia.Reason, happyArgsFixtureHint)

	confirm := findResultByCommandKind(report, "learnings confirm", LiveDogfoodTestHappy)
	require.NotNil(t, confirm)
	assert.Equal(t, LiveDogfoodStatusSkip, confirm.Status)
	assert.Contains(t, confirm.Reason, "no list companion")
	assert.Contains(t, confirm.Reason, happyArgsFixtureHint)

	assert.Equal(t, []string{"bare", "legge cronologia", "save"}, report.HollowFeatures)
	assert.Contains(t, report.HollowReasons["bare"], happyArgsFixtureHint)
	assert.Contains(t, report.HollowReasons["legge cronologia"], happyArgsFixtureHint)
	assert.Contains(t, report.HollowReasons["save"], happyArgsFixtureHint)
	for _, exempt := range []string{"which", "tail", "api", "import", "profile save", "profile show", "profile use", "learnings forget", "learnings confirm", "learnings reject", "yield", "dossier", "triage", "ddl iter", "swap"} {
		assert.NotContains(t, report.HollowFeatures, exempt)
	}
}

func TestLiveDogfoodTailOptionalPressArgs(t *testing.T) {
	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	researchDir := t.TempDir()
	require.NoError(t, writeResearchJSON(&ResearchResult{
		NovelFeatures: []NovelFeature{{Command: "tail"}, {Command: "bare"}},
	}, researchDir))
	context := `{"commands":[
  {"name":"list","annotations":{"mcp:read-only":"true","pp:method":"GET"}},
  {"name":"tail","annotations":{"mcp:read-only":"true","pp:no-error-path-probe":"true"}},
  {"name":"bare","annotations":{"mcp:read-only":"true"}}
]}`
	usages := map[string][2]string{
		"list": {"list", "list --json"},
		"tail": {"tail [resource]", "tail items"},
		"bare": {"bare <dimension>", "bare example-value"},
	}
	writeStubBinary(t, dir, binaryName, positionalHollowStubWith(context, usages))

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:      dir,
		BinaryName:  binaryName,
		Level:       "full",
		Timeout:     5 * time.Second,
		ResearchDir: researchDir,
	})
	require.NoError(t, err)
	require.Zero(t, report.Failed, liveDogfoodFailureSummary(report))

	tail := findResultByCommandKind(report, "tail", LiveDogfoodTestHappy)
	require.NotNil(t, tail)
	assert.Equal(t, LiveDogfoodStatusPass, tail.Status, tail.Reason)
	assert.Equal(t, []string{"tail", "--json"}, tail.Args)
	assert.Equal(t, []string{"bare"}, report.HollowFeatures)
	assert.Contains(t, report.HollowReasons["bare"], happyArgsFixtureHint)
}

func TestLiveDogfoodTailRequiredStaysSkippedAndNotHollow(t *testing.T) {
	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	researchDir := t.TempDir()
	require.NoError(t, writeResearchJSON(&ResearchResult{
		NovelFeatures: []NovelFeature{{Command: "tail"}},
	}, researchDir))
	context := `{"commands":[
  {"name":"list","annotations":{"mcp:read-only":"true","pp:method":"GET"}},
  {"name":"tail","annotations":{"mcp:read-only":"true","pp:no-error-path-probe":"true"}}
]}`
	usages := map[string][2]string{
		"list": {"list", "list --json"},
		"tail": {"tail <resource>", "tail items"},
	}
	writeStubBinary(t, dir, binaryName, positionalHollowStubWith(context, usages))

	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:      dir,
		BinaryName:  binaryName,
		Level:       "full",
		Timeout:     5 * time.Second,
		ResearchDir: researchDir,
	})
	require.NoError(t, err)
	require.Zero(t, report.Failed, liveDogfoodFailureSummary(report))

	tail := findResultByCommandKind(report, "tail", LiveDogfoodTestHappy)
	require.NotNil(t, tail)
	assert.Equal(t, LiveDogfoodStatusSkip, tail.Status)
	assert.Contains(t, tail.Reason, `non-id positional "resource"`)
	assert.Contains(t, tail.Reason, happyArgsFixtureHint)
	assert.Empty(t, report.HollowFeatures)
	assert.False(t, report.CoverageHollow)
}

func TestLiveDogfoodHappyArgsParsedBooleanStaysAttached(t *testing.T) {
	t.Parallel()

	yield := liveDogfoodCommand{
		Path: []string{"yield"},
		Help: `Usage:
  cli yield [id-or-url] [flags]

Examples:
  fixture-pp-cli yield listing-1

Flags:
      --json   Output JSON
`,
		Annotations: map[string]string{happyArgsAnnotation: "--json"},
	}
	args, ok, parsed := liveDogfoodHappyArgsParsed(yield)
	require.True(t, ok)
	assert.Empty(t, parsed.positionals)
	assert.Equal(t, []string{"yield", "listing-1", "--json"}, args)

	resolved, skipped, reason, _ := resolveLiveDogfoodHappyArgs(yield, args, len(parsed.positionals), emptyResolveCtx())
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"yield", "--json"}, resolved)

	yield.Annotations = map[string]string{happyArgsAnnotation: "--json=false"}
	args, ok, parsed = liveDogfoodHappyArgsParsed(yield)
	require.True(t, ok)
	assert.Equal(t, []string{"yield", "listing-1", "--json=false"}, args)
	resolved, skipped, reason, _ = resolveLiveDogfoodHappyArgs(yield, args, len(parsed.positionals), emptyResolveCtx())
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"yield", "--json=false"}, resolved)

	valued := liveDogfoodCommand{
		Path: []string{"yield"},
		Help: `Usage:
  cli yield [id-or-url] [flags]

Examples:
  fixture-pp-cli yield listing-1

Flags:
      --commune string   Commune
`,
		Annotations: map[string]string{happyArgsAnnotation: "--commune=ixelles"},
	}
	args, ok, parsed = liveDogfoodHappyArgsParsed(valued)
	require.True(t, ok)
	assert.Empty(t, parsed.positionals)
	assert.Equal(t, []string{"yield", "listing-1", "--commune", "ixelles"}, args)
	resolved, skipped, reason, _ = resolveLiveDogfoodHappyArgs(valued, args, len(parsed.positionals), emptyResolveCtx())
	assert.False(t, skipped, reason)
	assert.Equal(t, []string{"yield", "--commune", "ixelles"}, resolved)

	for _, arg := range []string{"--dry-run", "--dry-run=true", "--dry-run=TRUE", "--dry-run=t", "--dry-run=T", "--dry-run=1", "--dry-run=maybe"} {
		assert.True(t, liveDogfoodArgsContainDryRun([]string{"cmd", arg}), arg)
	}
	for _, arg := range []string{"--dry-run=false", "--dry-run=FALSE", "--dry-run=f", "--dry-run=F", "--dry-run=0", "--dry-run= False"} {
		assert.False(t, liveDogfoodArgsContainDryRun([]string{"cmd", arg}), arg)
	}
	assert.False(t, liveDogfoodArgsContainDryRun([]string{"cmd", "--", "--dry-run"}))
}

func TestPressFrameworkHappyArgsRefusesMissingOptionalShape(t *testing.T) {
	t.Parallel()

	_, ok := pressFrameworkHappyArgs(liveDogfoodCommand{Path: []string{"which"}, Help: joinHelp("which", "")})
	assert.False(t, ok)
	_, ok = pressFrameworkHappyArgs(liveDogfoodCommand{Path: []string{"dossier"}, Help: joinHelp("dossier [name]", "      --json   Output JSON\n")})
	assert.False(t, ok)

	assert.False(t, liveDogfoodPositionalsAllOptional(liveDogfoodCommand{Help: joinHelp("which", "")}))
	assert.False(t, liveDogfoodPositionalsAllOptional(liveDogfoodCommand{Help: joinHelp("tail <resource> [note]", "")}))
	assert.True(t, liveDogfoodPositionalsAllOptional(liveDogfoodCommand{Help: joinHelp("which [query] [limit]", "")}))

	assert.False(t, liveDogfoodHollowExempt(NovelFeature{Command: "which"}, map[string]bool{}))
	assert.False(t, liveDogfoodHollowExempt(NovelFeature{Command: "save"}, map[string]bool{"save": true}))
	assert.True(t, liveDogfoodHollowExempt(NovelFeature{Command: "profile delete"}, map[string]bool{"profile delete": true}))
	assert.Equal(t, "", liveDogfoodHappySkipReason(NovelFeature{Command: "bare"}, []LiveDogfoodTestResult{
		{Command: "bare", Kind: LiveDogfoodTestError, Status: LiveDogfoodStatusSkip, Reason: "no list companion"},
	}))
}

func emptyResolveCtx() resolveCtx {
	return resolveCtx{
		siblings: map[string][]liveDogfoodCommand{},
		cache:    newCompanionCache(),
		timeout:  time.Second,
	}
}

func joinHelp(usage, flags string) string {
	return "Usage:\n  cli " + usage + " [flags]\n\nFlags:\n" + flags
}

func liveDogfoodFailureSummary(report *LiveDogfoodReport) string {
	if report == nil {
		return ""
	}
	var out strings.Builder
	for _, result := range report.Tests {
		if result.Status != LiveDogfoodStatusFail {
			continue
		}
		out.WriteString(result.Command)
		out.WriteByte(' ')
		out.WriteString(string(result.Kind))
		out.WriteString(": ")
		out.WriteString(result.Reason)
		out.WriteByte('\n')
	}
	return out.String()
}

func positionalHollowAgentContext() string {
	return `{"commands":[
  {"name":"list","annotations":{"mcp:read-only":"true","pp:method":"GET"}},
  {"name":"triage","annotations":{"mcp:read-only":"true","pp:happy-args":"--type=apartment;--deal=rent;--commune=ixelles"}},
  {"name":"yield","annotations":{"mcp:read-only":"true","pp:happy-args":"--commune=ixelles;--type=apartment"}},
  {"name":"dossier","annotations":{"mcp:read-only":"true","pp:happy-args":"codcom-o-nome=roma"}},
  {"name":"ddl","subcommands":[
    {"name":"iter","annotations":{"mcp:read-only":"true","pp:happy-args":"legisl=18;numero=1153"}}
  ]},
  {"name":"legge","subcommands":[
    {"name":"cronologia","annotations":{"mcp:read-only":"true"}}
  ]},
  {"name":"swap","annotations":{"mcp:read-only":"true","pp:happy-args":"from-set=mdi;to-set=lucide"}},
  {"name":"bare","annotations":{"mcp:read-only":"true"}},
  {"name":"which","annotations":{"mcp:read-only":"true","pp:typed-exit-codes":"0,2"}},
  {"name":"tail","annotations":{"mcp:read-only":"true","pp:happy-args":"resource=items;--follow=false","pp:no-error-path-probe":"true"}},
  {"name":"api","annotations":{"mcp:read-only":"true"}},
  {"name":"import","annotations":{"pp:method":"POST"}},
  {"name":"profile","subcommands":[
    {"name":"save"},
    {"name":"show","annotations":{"mcp:read-only":"true"}},
    {"name":"use"}
  ]},
  {"name":"learnings","subcommands":[
    {"name":"forget"},
    {"name":"confirm","annotations":{"pp:typed-exit-codes":"0,2","mcp:local-write":"true"}},
    {"name":"reject","annotations":{"pp:typed-exit-codes":"0,2"}}
  ]},
  {"name":"save","annotations":{"mcp:read-only":"true"}}
]}`
}

func positionalHollowStub(agentContext string) string {
	usages := map[string][2]string{
		"list":              {"list", "list --json"},
		"triage":            {"triage [saved-search]", "triage --type apartment --deal rent --commune ixelles"},
		"yield":             {"yield [id-or-url]", "yield listing-1 --commune ixelles --type apartment"},
		"dossier":           {"dossier <codcom-o-nome>", "dossier example-value"},
		"ddl iter":          {"ddl iter <legisl> <numero>", "ddl iter 18 1153"},
		"legge cronologia":  {"legge cronologia <legisl> <numero>", "legge cronologia 18 1153"},
		"swap":              {"swap <from-set> <to-set>", "swap mdi lucide --icons home,cog"},
		"bare":              {"bare <dimension>", "bare example-value"},
		"which":             {"which [query]", `which "stale tickets"`},
		"tail":              {"tail [resource]", "tail items --follow=false"},
		"api":               {"api [interface]", "api"},
		"import":            {"import <resource>", "import <resource> --input data.jsonl --dry-run"},
		"profile save":      {"profile save <name>", "profile save my-defaults --json --compact"},
		"profile show":      {"profile show <name>", "profile show my-defaults"},
		"profile use":       {"profile use <name>", "profile use my-defaults"},
		"learnings forget":  {"learnings forget <query>", `learnings forget "what is due"`},
		"learnings confirm": {"learnings confirm <id>", "learnings confirm 3"},
		"learnings reject":  {"learnings reject <id>", "learnings reject 3"},
		"save":              {"save <name>", "save example-value"},
	}
	return positionalHollowStubWith(agentContext, usages)
}

func positionalHollowStubWith(agentContext string, usages map[string][2]string) string {
	var cases strings.Builder
	for name, pair := range usages {
		cases.WriteString("    ")
		cases.WriteString(shellQuote(name))
		cases.WriteString(") usage=")
		cases.WriteString(shellQuote(pair[0]))
		cases.WriteString("; example=")
		cases.WriteString(shellQuote(pair[1]))
		cases.WriteString(" ;;\n")
	}
	return `set -u
if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
` + agentContext + `
JSON
  exit 0
fi
cmd=""
help=0
for a in "$@"; do
  if [ "$a" = "--help" ]; then help=1; continue; fi
  case "$a" in
    --*) continue ;;
  esac
  if [ -z "$cmd" ]; then cmd="$a"; else cmd="$cmd $a"; fi
done
if [ "$help" = "1" ]; then
  usage="$cmd"
  example="$cmd"
  case "$cmd" in
` + cases.String() + `  esac
  cat <<HELP
Command.

Usage:
  fixture-pp-cli $usage [flags]

Examples:
  fixture-pp-cli $example

Flags:
      --type string       Type
      --deal string       Deal
      --commune string    Commune
      --follow            Follow
      --input string      Input
      --dry-run           Dry run
      --compact           Compact
      --icons string      Icons
      --json              Output JSON
HELP
  exit 0
fi
for a in "$@"; do
  if [ "$a" = "__printing_press_invalid__" ]; then
    echo "invalid" >&2
    exit 2
  fi
done
echo '{"ok":true}'
exit 0
`
}

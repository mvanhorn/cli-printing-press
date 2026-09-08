package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateRejectsGreedyLowercaseTickerPattern(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-greedy-ticker")
	apiSpec.Learn.Enabled = true
	apiSpec.Learn.TickerPatterns = []string{`^[a-z0-9]{2,12}$`}
	outputDir := filepath.Join(t.TempDir(), "learn-greedy-ticker-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}

	err := gen.Generate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "ticker_patterns[0]")
	require.Contains(t, err.Error(), `^[a-z0-9]{2,12}$`)
	require.Contains(t, err.Error(), "alpha example query")
	require.Contains(t, err.Error(), "QueryFamily would be empty")
	require.NoFileExists(t, filepath.Join(outputDir, "internal", "learn", "entities", "extract.go"),
		"generate must fail closed before emitting the learn package")
}

func TestGenerateAcceptsUppercaseTickerPattern(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-upper-ticker")
	apiSpec.Learn.Enabled = true
	apiSpec.Learn.TickerPatterns = []string{`^[A-Z][A-Z0-9]{2,11}$`}
	outputDir := filepath.Join(t.TempDir(), "learn-upper-ticker-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())
	require.FileExists(t, filepath.Join(outputDir, "internal", "learn", "entities", "extract.go"))
	require.FileExists(t, filepath.Join(outputDir, "internal", "cli", "learn_init.go"))
}

func TestGenerateRejectsAuthoredPlaybookSwallowedByTickerPattern(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learn-authored-ticker")
	apiSpec.Learn.Enabled = true
	apiSpec.Learn.TickerPatterns = []string{`^nccpl-[a-z]+$`}
	outputDir := filepath.Join(t.TempDir(), "learn-authored-ticker-pp-cli")
	playbookDir := filepath.Join(outputDir, "internal", "cli", "playbooks")
	require.NoError(t, os.MkdirAll(playbookDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(playbookDir, "nccpl.json"), []byte(`{
  "query_family_examples": ["nccpl-alpha nccpl-beta"],
  "steps": [{"cmd": "x"}]
}`), 0o644))

	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	err := gen.Generate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "ticker_patterns[0]")
	require.Contains(t, err.Error(), "nccpl-alpha nccpl-beta")
	require.Contains(t, err.Error(), "playbooks/nccpl.json")
}

func TestAuthoredLearnQueryFamilyExamplesTreatsOutputDirLiterally(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "out[put]")
	playbookDir := filepath.Join(outputDir, "internal", "cli", "playbooks")
	require.NoError(t, os.MkdirAll(playbookDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(playbookDir, "keep.json"), []byte(`{
  "query_family_examples": ["keep these tokens"],
  "steps": [{"cmd": "x"}]
}`), 0o644))

	got, err := authoredLearnQueryFamilyExamples(outputDir)
	require.NoError(t, err)
	require.Equal(t, []spec.LearnQueryFamilyExample{
		{Source: "playbooks/keep.json", Query: "keep these tokens"},
	}, got)
}

func TestLearnSeededQueryFamilyExamplesStayInTemplates(t *testing.T) {
	t.Parallel()

	files := []string{
		"templates/playbook_init_test.go.tmpl",
		"templates/teach_playbook_test.go.tmpl",
		"templates/learn/playbooks_test.go.tmpl",
		"templates/learn/recall_canonical_test.go.tmpl",
	}
	fromTemplates := map[string]struct{}{}
	for _, rel := range files {
		data, err := templateFS.ReadFile(rel)
		require.NoError(t, err, "read %s", rel)
		for _, query := range templateQueryFamilyExamples(string(data)) {
			fromTemplates[query] = struct{}{}
		}
	}
	fromSpec := spec.LearnSeededQueryFamilyQueries()
	require.NotEmpty(t, fromSpec)
	require.Len(t, fromTemplates, len(fromSpec),
		"template query_family_examples %v must match seeded list %v", keys(fromTemplates), fromSpec)
	for _, query := range fromSpec {
		_, ok := fromTemplates[query]
		require.True(t, ok, "seeded query_family_example %q missing from template query_family_examples arrays", query)
	}
}

var queryFamilyExamplesArray = regexp.MustCompile(`"query_family_examples"\s*:\s*\[([^\]]*)\]`)
var queryFamilyExampleString = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

func templateQueryFamilyExamples(src string) []string {
	var out []string
	for _, match := range queryFamilyExamplesArray.FindAllStringSubmatch(src, -1) {
		for _, inner := range queryFamilyExampleString.FindAllStringSubmatch(match[1], -1) {
			out = append(out, inner[1])
		}
	}
	return out
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

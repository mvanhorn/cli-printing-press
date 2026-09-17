package generator

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/stretchr/testify/require"
)

func assertTeachFamilyProbeSurface(t *testing.T, src, useMarker, happyArgs, exampleLine string) {
	t.Helper()
	idx := strings.Index(src, useMarker)
	require.GreaterOrEqual(t, idx, 0, "command marker %q not found", useMarker)
	block := src[idx:min(idx+4000, len(src))]
	require.Regexp(t, `"pp:happy-args":\s+`+regexp.QuoteMeta(`"`+happyArgs+`"`), block,
		"%s must ship semicolon-grammar pp:happy-args", useMarker)
	require.Contains(t, block, "Example: `  "+exampleLine+"`",
		"%s must ship a single-line probe-parseable Example", useMarker)

	example := extractCobraExample(t, block)
	require.NotContains(t, example, "$(", "%s Example must not use shell substitution", useMarker)
	require.NotContains(t, example, " &", "%s Example must not background with &", useMarker)
	require.NotContains(t, example, "<type>", "%s Example must not use placeholder values", useMarker)
	require.NotContains(t, example, "<id>", "%s Example must not use placeholder values", useMarker)
	require.Equal(t, 1, strings.Count(example, "\n")+1,
		"%s Example must be a single line, got %q", useMarker, example)

	args, err := shellargs.ArgsAfterBinary(example)
	require.NoError(t, err, "%s Example must parse as a command invocation: %q", useMarker, example)
	require.NotEmpty(t, args, "%s Example produced no subcommand args", useMarker)
}

func extractCobraExample(t *testing.T, block string) string {
	t.Helper()
	const marker = "Example: `"
	i := strings.Index(block, marker)
	require.GreaterOrEqual(t, i, 0, "Example field not found in command block")
	rest := block[i+len(marker):]
	j := strings.Index(rest, "`")
	require.GreaterOrEqual(t, j, 0, "unclosed Example string")
	return rest[:j]
}

// TestGeneratedTeachFamilyPassesLiveDogfoodContracts generates a CLI and
// runs the teach-family write commands with --json using the emitted
// happy-path fixtures. Live dogfood's json_fidelity probe requires
// non-empty parseable JSON on success.
func TestGeneratedTeachFamilyPassesLiveDogfoodContracts(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("generated CLI compile-and-run skipped in -short mode")
	}

	apiSpec := smallReadWriteSyncableOutputSpec("teach-dogfood")
	apiSpec.Learn.Enabled = true
	outputDir, binaryPath := buildGeneratedBinary(t, apiSpec)

	bin := naming.CLI(apiSpec.Name)
	teachSrc := readEmitted(t, outputDir, "internal", "cli", "teach.go")
	assertTeachFamilyProbeSurface(t, teachSrc, `Use:   "teach"`,
		`--query=find items in category;--resource-type=items;--resource=GROUP-category`,
		bin+` teach --query "find items in category" --resource-type items --resource GROUP-category`)
	assertTeachFamilyProbeSurface(t, teachSrc, `Use:   "teach-pattern"`,
		`--query-template=items in {entity};--resource-template=GROUP-{entity:category};--resource-type=items;--entity-kind=category;--strategy=substitute`,
		bin+` teach-pattern --query-template "items in {entity}" --resource-template "GROUP-{entity:category}" --resource-type items --entity-kind category --strategy substitute`)
	require.Contains(t, teachSrc, "func teachEmitsJSON(")
	require.Contains(t, teachSrc, "quietFlag.Changed && flags.quiet")

	playbookSrc := readEmitted(t, outputDir, "internal", "cli", "teach_playbook.go")
	assertTeachFamilyProbeSurface(t, playbookSrc, `Use:   "teach-playbook"`,
		`--query=find items in category;--notes=example playbook note`,
		bin+` teach-playbook --query "find items in category" --notes "example playbook note"`)
	assertTeachFamilyProbeSurface(t, playbookSrc, `Use:   "amend"`,
		`--query=find items in category;--add-note=example correction`,
		bin+` playbook amend --query "find items in category" --add-note "example correction"`)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "teach",
			args: []string{"teach", "--query", "find items in category", "--resource-type", "items", "--resource", "GROUP-category", "--json"},
			want: "recorded",
		},
		{
			name: "teach-pattern",
			args: []string{"teach-pattern", "--query-template", "items in {entity}", "--resource-template", "GROUP-{entity:category}", "--resource-type", "items", "--entity-kind", "category", "--strategy", "substitute", "--json"},
			want: "recorded",
		},
		{
			name: "teach-playbook",
			args: []string{"teach-playbook", "--query", "find items in category", "--notes", "example playbook note", "--json"},
			want: "recorded",
		},
		{
			name: "playbook amend",
			args: []string{"playbook", "amend", "--query", "find items in category", "--add-note", "example correction", "--json"},
			want: "amended",
		},
		{
			name: "teach-lookup",
			args: []string{"teach-lookup", "--kind", "country", "--canonical", "United States", "--value", "USA", "--json"},
			want: "recorded",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout := requireTeachFamilyJSON(t, binaryPath, tc.args)
			require.True(t, json.Valid([]byte(stdout)), "stdout: %q", stdout)
			var payload map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &payload), "stdout: %q", stdout)
			require.Contains(t, payload, tc.want, "stdout: %q", stdout)
		})
	}

	t.Run("teach --json ignores quiet default", func(t *testing.T) {
		stdout := requireTeachFamilyJSON(t, binaryPath, []string{
			"teach", "--query", "find items in category",
			"--resource-type", "items", "--resource", "GROUP-category", "--json",
		})
		require.NotEqual(t, "", stdout)
		require.True(t, json.Valid([]byte(stdout)), "stdout: %q", stdout)
	})

	t.Run("root --quiet=false teach --json emits JSON", func(t *testing.T) {
		stdout := requireTeachFamilyJSON(t, binaryPath, []string{
			"--quiet=false", "teach", "--query", "find items in category",
			"--resource-type", "items", "--resource", "GROUP-category", "--json",
		})
		require.True(t, json.Valid([]byte(stdout)), "stdout: %q", stdout)
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &payload), "stdout: %q", stdout)
		require.Contains(t, payload, "recorded", "stdout: %q", stdout)
	})
}

func requireTeachFamilyJSON(t *testing.T, binaryPath string, args []string) string {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = sandboxHomeEnv(t)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	out := strings.TrimSpace(stdout.String())
	require.NotEmpty(t, out, "expected non-empty JSON stdout; stderr=%q", stderr.String())
	return out
}

package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSampleCommandFiles_SkipsTestsAndNonCommands pins the description-sample
// filter: early *_test.go files, non-command helpers, and cobra.Command mentions
// that are not composite literals must not consume sample slots.
func TestSampleCommandFiles_SkipsTestsAndNonCommands(t *testing.T) {
	dir := t.TempDir()
	writeTerminalUXNoise(t, dir)
	writeTerminalUXCommand(t, dir, "cmd_alpha.go", "List alpha resources for the account", "ALPHA_COMMAND")
	writeTerminalUXCommand(t, dir, "cmd_bravo.go", "Show bravo resources for the account", "BRAVO_COMMAND")

	got := sampleCommandFiles(dir, 5)
	require.Len(t, got, 2)
	assert.Contains(t, got[0], "ALPHA_COMMAND")
	assert.Contains(t, got[1], "BRAVO_COMMAND")
	for _, content := range got {
		assert.NotContains(t, content, "NOT_A_COMMAND")
		assert.NotContains(t, content, "INFRA_HELPER")
	}

	all := sampleCommandFiles(dir, 0)
	require.Len(t, all, 2)
	assert.Nil(t, sampleCommandFiles(t.TempDir(), 5))
}

func TestScoreTerminalUX_DescriptionSampleUsesCommandFiles(t *testing.T) {
	// No NO_COLOR, TTY, no-color, or tabwriter signals, so the score is only
	// the description band (0 / +1 / +2) plus the example band (0 / +1 / +3).
	cases := []struct {
		name       string
		short      string
		commands   int
		wantScore  int
		wantSample int
	}{
		{
			name:       "five described commands score full description points",
			short:      "List alpha resources for the account",
			commands:   5,
			wantScore:  5,
			wantSample: 5,
		},
		{
			name:       "one-word shorts lose description points",
			short:      "List",
			commands:   5,
			wantScore:  3,
			wantSample: 5,
		},
		{
			name:       "three described commands stay on the partial band",
			short:      "List alpha resources for the account",
			commands:   3,
			wantScore:  2,
			wantSample: 3,
		},
		{
			name:       "one command file scores no description or example points",
			short:      "List alpha resources for the account",
			commands:   1,
			wantScore:  0,
			wantSample: 1,
		},
	}

	names := []string{"cmd_alpha.go", "cmd_bravo.go", "cmd_charlie.go", "cmd_delta.go", "cmd_echo.go"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTerminalUXNoise(t, dir)
			for i := 0; i < tc.commands; i++ {
				writeTerminalUXCommand(t, dir, names[i], tc.short, "COMMAND_FILE")
			}

			got := sampleCommandFiles(dir, 5)
			require.Len(t, got, tc.wantSample)
			for _, content := range got {
				assert.Contains(t, content, "COMMAND_FILE")
				assert.NotContains(t, content, "NOT_A_COMMAND")
				assert.NotContains(t, content, "INFRA_HELPER")
				if tc.short == "List" {
					assert.False(t, hasQualityDescription(content))
				} else {
					assert.True(t, hasQualityDescription(content))
				}
			}
			assert.Equal(t, tc.wantScore, scoreTerminalUX(dir))
		})
	}
}

func writeTerminalUXNoise(t *testing.T, dir string) {
	t.Helper()
	writeScorecardFixture(t, dir, "internal/cli/aaa_actions_test.go", `package cli

import "github.com/spf13/cobra"

func TestActions(t *testing.T) {
	_ = &cobra.Command{Use: "actions", Short: "List actions for the current account"}
}

const marker = "NOT_A_COMMAND"
`)
	writeScorecardFixture(t, dir, "internal/cli/aab_auto_refresh.go", `package cli

// commandPath must match cobra.CommandPath(). NOT_A_COMMAND
func refresh() error { return nil }
`)
	writeScorecardFixture(t, dir, "internal/cli/aac_comment.go", `package cli

// &cobra.Command{Use: "fake", Short: "this description is long enough to look real"}
func commentOnly() { const marker = "NOT_A_COMMAND" }
`)
	writeScorecardFixture(t, dir, "internal/cli/aad_signature.go", `package cli

func refresh(cmd *cobra.Command) error {
	const marker = "NOT_A_COMMAND"
	return nil
}
`)
	writeScorecardFixture(t, dir, "internal/cli/aae_string.go", `package cli

const doc = "cobra.Command{Short: \"describe the resource in detail\"}"
const marker = "NOT_A_COMMAND"
`)
	writeScorecardFixture(t, dir, "internal/cli/aaf_broken.go", `package cli

const marker = "NOT_A_COMMAND"
func (
`)
	writeScorecardFixture(t, dir, "internal/cli/aag_notcobra.go", `package cli

import notcobra "example.com/notcobra"

var _ = &notcobra.Command{Use: "x", Short: "List resources for another package"}

const marker = "NOT_A_COMMAND"
`)
	writeScorecardFixture(t, dir, "internal/cli/helpers.go", `package cli

import "github.com/spf13/cobra"

var _ = &cobra.Command{Use: "helpers", Short: "List helper resources for the account"}

const marker = "INFRA_HELPER"
`)
	writeScorecardFixture(t, dir, "internal/cli/notes.txt", "NOT_A_COMMAND\n")
}

func writeTerminalUXCommand(t *testing.T, dir, name, short, marker string) {
	t.Helper()
	writeScorecardFixture(t, dir, "internal/cli/"+name, `package cli

import "github.com/spf13/cobra"

func newCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "items",
		Short: "`+short+`",
	}
}

const marker = "`+marker+`"
`)
}

func TestFileHasCobraCommandLiteral(t *testing.T) {
	assert.True(t, fileHasCobraCommandLiteral(`package cli
import "github.com/spf13/cobra"
var _ = &cobra.Command{Use: "items"}
`))
	assert.True(t, fileHasCobraCommandLiteral(`package cli
import "github.com/spf13/cobra"
var _ = cobra.Command{Use: "items"}
`))
	assert.False(t, fileHasCobraCommandLiteral(`package cli
// &cobra.Command{Use: "items", Short: "long description here"}
func f() {}
`))
	assert.False(t, fileHasCobraCommandLiteral(`package cli
const doc = "cobra.Command{Use: \"items\"}"
`))
	assert.False(t, fileHasCobraCommandLiteral(`package cli
func f(cmd *cobra.Command) {}
`))
	assert.False(t, fileHasCobraCommandLiteral(`package cli
import other "example.com/other"
var _ = &other.Command{Use: "items"}
`))
	assert.False(t, fileHasCobraCommandLiteral("package cli\nfunc (\n"))
	assert.False(t, fileHasCobraCommandLiteral(""))
}

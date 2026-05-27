package patch

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseRootGo is a minimal root.go that mirrors the shape of every published
// printing-press CLI (rootFlags struct + Execute() with PersistentFlags,
// PersistentPreRunE, AddCommand sequence, err := rootCmd.Execute()).
const baseRootGo = `package cli

import (
	"strings"
	"time"

	"github.com/example/test/internal/client"
	"github.com/spf13/cobra"
)

var version = "1.0.0"

type rootFlags struct {
	asJSON   bool
	compact  bool
	timeout  time.Duration
}

func Execute() error {
	var flags rootFlags

	rootCmd := &cobra.Command{Use: "test-pp-cli"}

	rootCmd.PersistentFlags().BoolVar(&flags.asJSON, "json", false, "Output as JSON")
	rootCmd.PersistentFlags().BoolVar(&flags.compact, "compact", false, "Compact output")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if flags.asJSON {
			flags.compact = true
		}
		return nil
	}

	rootCmd.AddCommand(newFooCmd(&flags))
	rootCmd.AddCommand(newBarCmd(&flags))
	rootCmd.AddCommand(newNovelSQLCmd(&flags))
	rootCmd.AddCommand(newDoctorCmd(&flags))

	err := rootCmd.Execute()
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		return err
	}
	return err
}
`

func TestInjectRootAST_MutatesAllSixTargets(t *testing.T) {
	out, changed, err := injectRootAST([]byte(baseRootGo), injectOptions{})
	require.NoError(t, err)
	require.True(t, changed, "expected mutation")
	src := string(out)

	assert.Contains(t, src, "profileName", "rootFlags should gain profileName field")
	assert.Contains(t, src, "deliverSpec", "rootFlags should gain deliverSpec field")
	assert.Contains(t, src, "deliverBuf", "rootFlags should gain deliverBuf field")
	assert.Contains(t, src, "deliverSink", "rootFlags should gain deliverSink field")

	assert.Contains(t, src, `"bytes"`, "should import bytes")
	assert.Contains(t, src, `"io"`, "should import io")
	assert.Contains(t, src, `"os"`, "should import os")

	assert.Contains(t, src, `"profile"`, "should register --profile flag")
	assert.Contains(t, src, `"deliver"`, "should register --deliver flag")

	assert.Contains(t, src, "flags.deliverSpec != \"\"", "PersistentPreRunE should have deliver-setup block")
	assert.Contains(t, src, "ParseDeliverSink", "PersistentPreRunE should call ParseDeliverSink")
	assert.Contains(t, src, "flags.profileName != \"\"", "PersistentPreRunE should have profile-lookup block")
	assert.Contains(t, src, "GetProfile", "PersistentPreRunE should call GetProfile")

	assert.Contains(t, src, "newProfileCmd(&flags)", "Execute should register newProfileCmd")
	assert.Contains(t, src, "newFeedbackCmd(&flags)", "Execute should register newFeedbackCmd")

	assert.Contains(t, src, "Deliver(flags.deliverSink", "Execute should flush deliverBuf after rootCmd.Execute")
}

func TestInjectRootAST_PreservesNovelCommands(t *testing.T) {
	out, _, err := injectRootAST([]byte(baseRootGo), injectOptions{})
	require.NoError(t, err)
	src := string(out)

	// Every original AddCommand must still be present, in the same order,
	// before the new profile/feedback registrations.
	fooIdx := strings.Index(src, "newFooCmd(&flags)")
	barIdx := strings.Index(src, "newBarCmd(&flags)")
	sqlIdx := strings.Index(src, "newNovelSQLCmd(&flags)")
	doctorIdx := strings.Index(src, "newDoctorCmd(&flags)")
	profileIdx := strings.Index(src, "newProfileCmd(&flags)")
	feedbackIdx := strings.Index(src, "newFeedbackCmd(&flags)")

	require.Greater(t, fooIdx, 0, "newFooCmd must survive")
	require.Greater(t, barIdx, fooIdx, "newBarCmd order preserved")
	require.Greater(t, sqlIdx, barIdx, "newNovelSQLCmd order preserved")
	require.Greater(t, doctorIdx, sqlIdx, "newDoctorCmd order preserved")
	require.Greater(t, profileIdx, doctorIdx, "newProfileCmd inserted after last original")
	require.Greater(t, feedbackIdx, profileIdx, "newFeedbackCmd inserted after profile")
}

func TestInjectRootAST_Idempotent(t *testing.T) {
	first, changed, err := injectRootAST([]byte(baseRootGo), injectOptions{})
	require.NoError(t, err)
	require.True(t, changed)

	second, changedAgain, err := injectRootAST(first, injectOptions{})
	require.NoError(t, err)
	assert.False(t, changedAgain, "second run should be a no-op")
	assert.Equal(t, string(first), string(second), "second run must not alter bytes")
}

func TestInjectRootAST_PreservesVersionAndImports(t *testing.T) {
	out, _, err := injectRootAST([]byte(baseRootGo), injectOptions{})
	require.NoError(t, err)
	src := string(out)

	assert.Contains(t, src, `var version = "1.0.0"`, "version var must survive")
	assert.Contains(t, src, `"github.com/example/test/internal/client"`, "module path import must survive unchanged")
	assert.Contains(t, src, `"github.com/spf13/cobra"`, "cobra import must survive")
}

// TestInjectRootAST_SkipFeedback exercises the Pagliacci case: a spec has its
// own feedback resource so feedback.go is not dropped in, and the AST patch
// must skip `rootCmd.AddCommand(newFeedbackCmd(...))` to keep the build green.
func TestInjectRootAST_SkipFeedback(t *testing.T) {
	out, changed, err := injectRootAST([]byte(baseRootGo), injectOptions{
		Skip: map[string]bool{"feedback": true},
	})
	require.NoError(t, err)
	require.True(t, changed, "profile/deliver mutations should still fire")
	src := string(out)

	assert.NotContains(t, src, "newFeedbackCmd(&flags)",
		"newFeedbackCmd must NOT be registered when feedback is skipped")
	assert.Contains(t, src, "newProfileCmd(&flags)",
		"newProfileCmd must still be registered")
	assert.Contains(t, src, `"deliver"`, "--deliver flag must still be registered")
	assert.Contains(t, src, `flags.deliverSpec != ""`,
		"deliver-setup block must still be present")
}

func TestCheckRootShape_MatchesBase(t *testing.T) {
	assert.Empty(t, checkRootShape([]byte(baseRootGo)), "base fixture should match shape")
}

func TestCheckRootShape_RejectsInstacartShape(t *testing.T) {
	// Instacart / agent-capture use a package-global `var rootCmd`, no
	// rootFlags struct, and register PersistentFlags/AddCommand via a
	// different receiver.
	src := `package cli

import "github.com/spf13/cobra"

var jsonOutput bool

var rootCmd = &cobra.Command{Use: "thing"}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "")
	rootCmd.AddCommand(newThingCmd())
}

func Execute() error { return rootCmd.Execute() }
`
	msg := checkRootShape([]byte(src))
	require.NotEmpty(t, msg)
	assert.Contains(t, msg, "rootFlags struct",
		"must flag missing rootFlags struct")
}

// TestParseStmt_ReturnsErrorOnInvalidGo verifies that parseStmt propagates a
// parse error instead of panicking when given syntactically invalid Go source.
func TestParseStmt_ReturnsErrorOnInvalidGo(t *testing.T) {
	t.Parallel()
	_, err := parseStmt(`this is not valid Go !!!`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patch.parseStmt")
}

// TestParseStmt_EmptyStringReturnsError verifies that parseStmt returns an
// error (not a panic) when given an empty string. The wrapper parses as valid
// Go but yields an empty function body; the bounds-safe loop must catch this.
func TestParseStmt_EmptyStringReturnsError(t *testing.T) {
	t.Parallel()
	_, err := parseStmt("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patch.parseStmt")
}

// TestParseStmt_WhitespaceOnlyReturnsError exercises the same empty-body guard
// for whitespace/newline-only input.
func TestParseStmt_WhitespaceOnlyReturnsError(t *testing.T) {
	t.Parallel()
	_, err := parseStmt("   \n\t  ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "patch.parseStmt")
}

// TestInjectRootAST_NoPersistentFlagsBlock exercises the "refuse silently"
// path: if the target root.go doesn't have the expected shape, no mutation.
func TestInjectRootAST_NoPersistentFlagsBlock(t *testing.T) {
	src := `package cli

func Execute() error {
	return nil
}
`
	out, changed, err := injectRootAST([]byte(src), injectOptions{})
	require.NoError(t, err)
	// rootFlags struct is missing so no field mutation; no PersistentFlags
	// call so no flag mutation; no AddCommand so no command mutation.
	// No mutation should occur — returns unchanged.
	assert.False(t, changed, "no changes when target shape missing")
	assert.Equal(t, src, string(out))
}

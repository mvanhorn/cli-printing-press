package regenmerge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectValueDriftReturnsNilForIdenticalFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package x

const Greeting = "hello"

func add(a, b int) int { return a + b }
`
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(src), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(src), 0o644))

	assert.Nil(t, detectValueDrift(pub, fresh))
}

func TestDetectValueDriftCatchesConstLiteralChange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(`package x

const authPrefix = "Bearer "
`), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(`package x

const authPrefix = "Token "
`), 0o644))

	drift := detectValueDrift(pub, fresh)
	require.NotNil(t, drift, "literal value drift in const should be detected")
	require.NotNil(t, drift.Decls)
	_, ok := drift.Decls["const:authPrefix"]
	assert.True(t, ok, "expected drift entry for const authPrefix; got %v", drift.Decls)
}

func TestDetectValueDriftCatchesEmptyToNonEmptyConst(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(`package x

const graphqlEndpointPath = ""
`), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(`package x

const graphqlEndpointPath = "/graphql"
`), 0o644))

	drift := detectValueDrift(pub, fresh)
	require.NotNil(t, drift)
	_, ok := drift.Decls["const:graphqlEndpointPath"]
	assert.True(t, ok)
}

func TestDetectValueDriftCatchesSelectorIdentifierRename(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(`package x

type Config struct{ Bearer, Token string }

func authHeader(c Config) string {
	return c.Bearer
}
`), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(`package x

type Config struct{ Bearer, Token string }

func authHeader(c Config) string {
	return c.Token
}
`), 0o644))

	drift := detectValueDrift(pub, fresh)
	require.NotNil(t, drift, "selector identifier rename should be detected")
	_, ok := drift.Decls["authHeader"]
	assert.True(t, ok, "expected drift entry for authHeader; got %v", drift.Decls)
}

func TestDetectValueDriftCatchesTypeConversionRename(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(`package x

type MyType int
type MyOtherType int

func convert(x int) MyType { return MyType(x) }
`), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(`package x

type MyType int
type MyOtherType int

func convert(x int) MyType { return MyOtherType(x) }
`), 0o644))

	drift := detectValueDrift(pub, fresh)
	require.NotNil(t, drift)
	_, ok := drift.Decls["convert"]
	assert.True(t, ok)
}

func TestDetectValueDriftIgnoresDocCommentChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(`package x

// pubVersion of the doc comment.
const Greeting = "hello"
`), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(`package x

// freshVersion of the doc comment with completely different wording.
const Greeting = "hello"
`), 0o644))

	assert.Nil(t, detectValueDrift(pub, fresh), "doc-comment-only diff should not trigger drift")
}

func TestDetectValueDriftIgnoresAddCommandStmts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(`package x

func newCmd() *Cmd {
	cmd := &Cmd{}
	cmd.AddCommand(newA())
	cmd.AddCommand(newB())
	cmd.AddCommand(newHandAddedCmd())
	return cmd
}
`), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(`package x

func newCmd() *Cmd {
	cmd := &Cmd{}
	cmd.AddCommand(newA())
	cmd.AddCommand(newB())
	return cmd
}
`), 0o644))

	assert.Nil(t, detectValueDrift(pub, fresh),
		"AddCommand-only diff should defer to LostRegistrations re-injection, not trigger value drift")
}

func TestDetectValueDriftCatchesReorderedSliceLiterals(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(`package x

var defaults = []string{"a", "b"}
`), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(`package x

var defaults = []string{"b", "a"}
`), 0o644))

	drift := detectValueDrift(pub, fresh)
	require.NotNil(t, drift, "reordered slice literals should be detected")
	_, ok := drift.Decls["var:defaults"]
	assert.True(t, ok)
}

func TestDetectValueDriftReturnsNilOnParseError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(`this is not go code`), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(`package x

const x = "hello"
`), 0o644))

	assert.Nil(t, detectValueDrift(pub, fresh), "parse error on either side should defer to other branches")
}

func TestClassifyAssignsValueDriftWhenDeclSetMatchesAndLiteralChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pubDir := filepath.Join(dir, "pub")
	freshDir := filepath.Join(dir, "fresh")
	require.NoError(t, os.MkdirAll(filepath.Join(pubDir, "internal", "config"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(freshDir, "internal", "config"), 0o755))

	templated := func(literal string) []byte {
		return []byte(`// Copyright 2026 owner. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.
package config

const authPrefix = "` + literal + `"
`)
	}
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, "internal", "config", "config.go"), templated("Token "), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(freshDir, "internal", "config", "config.go"), templated("Bearer "), 0o644))

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)
	require.NotNil(t, report)

	var got *FileClassification
	for i := range report.Files {
		if report.Files[i].Path == "internal/config/config.go" {
			got = &report.Files[i]
			break
		}
	}
	require.NotNil(t, got, "config.go should appear in the report")
	assert.Equal(t, VerdictTemplatedValueDrift, got.Verdict,
		"literal-only drift in templated const should classify as TEMPLATED-VALUE-DRIFT")
	require.NotNil(t, got.ValueDrift, "ValueDrift field must be populated")
	_, ok := got.ValueDrift.Decls["const:authPrefix"]
	assert.True(t, ok, "drift entry for const:authPrefix expected; got %v", got.ValueDrift.Decls)
}

func TestClassifyAssignsTemplatedCleanWhenIdenticalDeclsAndAddCommandOnlyDiff(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pubDir := filepath.Join(dir, "pub")
	freshDir := filepath.Join(dir, "fresh")
	require.NoError(t, os.MkdirAll(filepath.Join(pubDir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(freshDir, "internal", "cli"), 0o755))

	pub := []byte(`// Copyright 2026 owner. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.
package cli

func newTransactionsCmd(flags *rootFlags) *Cmd {
	cmd := &Cmd{}
	cmd.AddCommand(newTransactionsListCmd(flags))
	cmd.AddCommand(newHandAddedCmd(flags))
	return cmd
}

`)
	fresh := []byte(`// Copyright 2026 owner. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.
package cli

func newTransactionsCmd(flags *rootFlags) *Cmd {
	cmd := &Cmd{}
	cmd.AddCommand(newTransactionsListCmd(flags))
	return cmd
}
`)
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, "internal", "cli", "transactions.go"), pub, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(freshDir, "internal", "cli", "transactions.go"), fresh, 0o644))

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)

	var got *FileClassification
	for i := range report.Files {
		if report.Files[i].Path == "internal/cli/transactions.go" {
			got = &report.Files[i]
			break
		}
	}
	require.NotNil(t, got)
	assert.Equal(t, VerdictTemplatedClean, got.Verdict,
		"AddCommand-only diff routes through LostRegistrations, not value drift")
	assert.NotEmpty(t, report.LostRegistrations, "lost AddCommand should be captured for re-injection")
}

func TestClassifyTreatsGeneratedNovelCommandHookAsTemplateEvolution(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pubDir := filepath.Join(dir, "pub")
	freshDir := filepath.Join(dir, "fresh")
	rel := filepath.Join("internal", "cli", "root.go")
	for _, root := range []string{pubDir, freshDir} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "cli"), 0o755))
	}
	pub := `// Generated by CLI Printing Press. DO NOT EDIT.
package cli

var novelCommands func(root *Cmd, flags *rootFlags)

func newRootCmd(flags *rootFlags) *Cmd {
	root := &Cmd{}
	if novelCommands != nil { novelCommands(root, flags) }
	return root
}

func newClient() *Client {
	c := &Client{}
	return c
}
`
	fresh := `// Generated by CLI Printing Press. DO NOT EDIT.
package cli

var novelCommandHooks []func(root *Cmd, flags *rootFlags)
var clientHooks []func(*Client) error

func registerNovelCommand(hook func(root *Cmd, flags *rootFlags)) {
	novelCommandHooks = append(novelCommandHooks, hook)
}

func registerClientHook(hook func(*Client) error) { clientHooks = append(clientHooks, hook) }

func newRootCmd(flags *rootFlags) *Cmd {
	root := &Cmd{}
	for _, hook := range novelCommandHooks { hook(root, flags) }
	return root
}

func newClient() *Client {
	c := &Client{}
	for _, hook := range clientHooks {
		if err := hook(c); err != nil { return nil }
	}
	return c
}
`
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, rel), []byte(pub), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(freshDir, rel), []byte(fresh), 0o644))

	report, err := Classify(pubDir, freshDir, Options{Force: true})
	require.NoError(t, err)
	got := verdictMap(report)[filepath.ToSlash(rel)]
	assert.Equal(t, VerdictTemplatedClean, got)
}

func TestDetectValueDriftIgnoresImportListChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(`package x

import "fmt"

func hello() { fmt.Println("hi") }
`), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(`package x

import (
	"fmt"
	"strings"
)

func hello() { fmt.Println("hi") }

func _strings() { _ = strings.ToUpper("") }
`), 0o644))

	// Imports differ, but `hello` is the same. The new function _strings is
	// in fresh-only, which decl-set comparison would catch upstream as
	// TEMPLATED-CLEAN-style movement; per-decl drift only fires for shared
	// decls that differ. Since `hello` matches, no value drift.
	assert.Nil(t, detectValueDrift(pub, fresh))
}

package regenmerge

import (
	"os"
	"path/filepath"
	"strconv"
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

func TestDetectValueDriftIgnoresRootHighlightsRewrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rootRel := filepath.Join("internal", "cli", "root.go")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pub", filepath.Dir(rootRel)), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "fresh", filepath.Dir(rootRel)), 0o755))
	rootSrc := func(highlight string) string {
		return `package cli

func newRootCmd() {
	_ = struct {
		Use   string
		Short string
		Long  string
	}{
		Use:   "demo-pp-cli",
		Short: "Manage demo resources",
		Long: ` + "`" + `Manage demo resources

Highlights (not in the official API docs):
  • ` + highlight + `

Agent mode: add --agent to any command.
` + "`" + `,
	}
}
`
	}
	pub := filepath.Join(dir, "pub", rootRel)
	fresh := filepath.Join(dir, "fresh", rootRel)
	require.NoError(t, os.WriteFile(pub, []byte(rootSrc("old cmd")), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(rootSrc("new cmd")), 0o644))

	assert.Nil(t, detectValueDrift(pub, fresh),
		"root.go Highlights-only rewrites are research input, not hand-edits")
}

func TestDetectValueDriftCatchesRootShortHandEdit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rootRel := filepath.Join("internal", "cli", "root.go")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pub", filepath.Dir(rootRel)), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "fresh", filepath.Dir(rootRel)), 0o755))
	rootSrc := func(short string) string {
		return `package cli

func newRootCmd() {
	_ = struct {
		Use   string
		Short string
		Long  string
	}{
		Use:   "demo-pp-cli",
		Short: "` + short + `",
		Long: ` + "`" + `Manage demo resources

Highlights (not in the official API docs):
  • items insight

Agent mode: add --agent to any command.
` + "`" + `,
	}
}
`
	}
	pub := filepath.Join(dir, "pub", rootRel)
	fresh := filepath.Join(dir, "fresh", rootRel)
	require.NoError(t, os.WriteFile(pub, []byte(rootSrc("hand-edited short")), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(rootSrc("Manage demo resources")), 0o644))

	drift := detectValueDrift(pub, fresh)
	require.NotNil(t, drift, "Short hand-edits must not be masked by Highlights normalization")
	_, ok := drift.Decls["newRootCmd"]
	assert.True(t, ok, "expected drift entry for newRootCmd; got %v", drift.Decls)
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

func TestDetectValueDriftIgnoresNovelHelperStmts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte(`package x

func newCmd() *Cmd {
	cmd := &Cmd{}
	cmd.AddCommand(newA())
	addNovelCommandIfAbsent(cmd, newHandAddedCmd())
	return cmd
}
`), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte(`package x

func newCmd() *Cmd {
	cmd := &Cmd{}
	cmd.AddCommand(newA())
	return cmd
}
`), 0o644))

	assert.Nil(t, detectValueDrift(pub, fresh),
		"addNovelCommandIfAbsent-only diff should defer to LostRegistrations, not trigger value drift")
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

func ApplyClientHooks(c *Client) error {
	for _, hook := range clientHooks {
		if err := hook(c); err != nil {
			return err
		}
	}
	return nil
}

func newRootCmd(flags *rootFlags) *Cmd {
	root := &Cmd{}
	for _, hook := range novelCommandHooks { hook(root, flags) }
	preferImplementedNovelCommands(root)
	return root
}

func newClient() *Client {
	c := &Client{}
	if err := ApplyClientHooks(c); err != nil {
		return nil
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

func TestClassifyConstVarKindChange(t *testing.T) {
	t.Parallel()

	const header = "// Code generated by CLI Printing Press. DO NOT EDIT.\npackage x\n\n"
	groupedConst := func(maxBytes int) string {
		return header + "const (\n\tMaxBytes = " + itoa(maxBytes) + "\n\tMaxItems = 50\n)\n"
	}
	groupedVar := func(maxBytes int) string {
		return header + "var (\n\tMaxBytes = " + itoa(maxBytes) + "\n\tMaxItems = 50\n)\n"
	}
	singleConst := func(maxBytes int) string {
		return header + "const MaxBytes = " + itoa(maxBytes) + "\n"
	}
	singleVar := func(maxBytes int) string {
		return header + "var MaxBytes = " + itoa(maxBytes) + "\n"
	}

	tests := []struct {
		name          string
		fresh         string
		pub           string
		want          Verdict
		driftKey      string
		exactKeys     bool
		pubContains   []string
		freshContains []string
	}{
		{
			name:          "A grouped const to var",
			fresh:         groupedConst(60000),
			pub:           header + "var MaxBytes = 32000\n\nconst (\n\tMaxItems = 50\n)\n",
			want:          VerdictTemplatedValueDrift,
			driftKey:      "var:MaxBytes",
			exactKeys:     true,
			pubContains:   []string{"var MaxBytes", "32000"},
			freshContains: []string{"const", "MaxBytes", "60000", "MaxItems"},
		},
		{
			name:          "B grouped const value edit",
			fresh:         groupedConst(60000),
			pub:           groupedConst(50000),
			want:          VerdictTemplatedValueDrift,
			driftKey:      "const:MaxBytes,MaxItems",
			exactKeys:     true,
			pubContains:   []string{"50000"},
			freshContains: []string{"60000"},
		},
		{
			name:          "C single const to var same value",
			fresh:         singleConst(60000),
			pub:           singleVar(60000),
			want:          VerdictTemplatedValueDrift,
			driftKey:      "var:MaxBytes",
			exactKeys:     true,
			pubContains:   []string{"var MaxBytes", "60000"},
			freshContains: []string{"const MaxBytes", "60000"},
		},
		{
			name:          "D single const to var different value",
			fresh:         singleConst(60000),
			pub:           singleVar(32000),
			want:          VerdictTemplatedValueDrift,
			driftKey:      "var:MaxBytes",
			exactKeys:     true,
			pubContains:   []string{"var MaxBytes", "32000"},
			freshContains: []string{"const MaxBytes", "60000"},
		},
		{
			name:          "E single const value edit",
			fresh:         singleConst(60000),
			pub:           singleConst(50000),
			want:          VerdictTemplatedValueDrift,
			driftKey:      "const:MaxBytes",
			exactKeys:     true,
			pubContains:   []string{"50000"},
			freshContains: []string{"60000"},
		},
		{
			name:          "F single var to const",
			fresh:         singleVar(60000),
			pub:           singleConst(32000),
			want:          VerdictTemplatedValueDrift,
			driftKey:      "const:MaxBytes",
			exactKeys:     true,
			pubContains:   []string{"const MaxBytes", "32000"},
			freshContains: []string{"var MaxBytes", "60000"},
		},
		{
			name:          "G grouped var to const",
			fresh:         groupedVar(60000),
			pub:           groupedConst(32000),
			want:          VerdictTemplatedValueDrift,
			driftKey:      "const:MaxBytes,MaxItems",
			exactKeys:     true,
			pubContains:   []string{"const", "32000"},
			freshContains: []string{"var", "60000"},
		},
		{
			name:          "grouped var to two consts",
			fresh:         header + "const MaxBytes = 60000\n\nconst MaxItems = 50\n",
			pub:           header + "var (\n\tMaxBytes = 32000\n\tMaxItems = 40\n)\n",
			want:          VerdictTemplatedValueDrift,
			driftKey:      "var:MaxBytes,MaxItems",
			exactKeys:     true,
			pubContains:   []string{"var", "32000", "40"},
			freshContains: []string{"const MaxBytes = 60000", "const MaxItems = 50"},
		},
		{
			name:          "grouped var split to const",
			fresh:         groupedVar(60000),
			pub:           header + "const MaxBytes = 32000\n\nvar (\n\tMaxItems = 50\n)\n",
			want:          VerdictTemplatedValueDrift,
			driftKey:      "const:MaxBytes",
			exactKeys:     true,
			pubContains:   []string{"const MaxBytes", "32000"},
			freshContains: []string{"var", "MaxBytes", "60000"},
		},
		{
			name:  "identical stays clean",
			fresh: singleConst(60000),
			pub:   singleConst(60000),
			want:  VerdictTemplatedClean,
		},
		{
			name:  "same-token split stays clean",
			fresh: groupedConst(60000),
			pub:   header + "const MaxBytes = 60000\n\nconst MaxItems = 50\n",
			want:  VerdictTemplatedClean,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fc := classifyKindFixture(t, tc.pub, tc.fresh)
			require.Equal(t, tc.want, fc.Verdict)
			if tc.want == VerdictTemplatedClean {
				assert.Nil(t, fc.ValueDrift)
				return
			}
			require.NotNil(t, fc.ValueDrift)
			if tc.exactKeys {
				require.Len(t, fc.ValueDrift.Decls, 1)
			}
			delta, ok := fc.ValueDrift.Decls[tc.driftKey]
			require.Truef(t, ok, "drift key %s missing; got %v", tc.driftKey, fc.ValueDrift.Decls)
			for _, needle := range tc.pubContains {
				assert.Contains(t, delta.Published, needle)
			}
			for _, needle := range tc.freshContains {
				assert.Contains(t, delta.Fresh, needle)
			}
		})
	}
}

func TestClassifyKindChangeAgainstBaseStaysDrift(t *testing.T) {
	t.Parallel()

	const header = "// Code generated by CLI Printing Press. DO NOT EDIT.\npackage x\n\n"
	base := header + "const MaxBytes = 60000\n"
	pub := header + "var MaxBytes = 32000\n"
	fresh := base

	fc := classifyKindFixtureWithBase(t, pub, fresh, base)
	require.Equal(t, VerdictTemplatedValueDrift, fc.Verdict)
	require.NotNil(t, fc.ValueDrift)
	delta, ok := fc.ValueDrift.Decls["var:MaxBytes"]
	require.True(t, ok, "got %v", fc.ValueDrift.Decls)
	assert.Contains(t, delta.Published, "var MaxBytes")
	assert.Contains(t, delta.Fresh, "const MaxBytes")
}

func TestClassifyTemplateKindChangeMatchingBaseStaysClean(t *testing.T) {
	t.Parallel()

	const header = "// Code generated by CLI Printing Press. DO NOT EDIT.\npackage x\n\n"
	base := header + "const MaxBytes = 60000\n"
	// Doc comment makes the file differ from base without changing the decl.
	pub := header + "// operator note\nconst MaxBytes = 60000\n"
	fresh := header + "var MaxBytes = 60000\n"

	fc := classifyKindFixtureWithBase(t, pub, fresh, base)
	assert.Equal(t, VerdictTemplatedClean, fc.Verdict)
}

func TestDetectValueDriftIgnoresUnmatchedFunc(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte("package x\n\nconst MaxBytes = 60000\n\nfunc Extra() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte("package x\n\nconst MaxBytes = 60000\n"), 0o644))
	assert.Nil(t, detectValueDrift(pub, fresh))
}

func TestSplitDeclKey(t *testing.T) {
	t.Parallel()

	_, _, ok := splitDeclKey("newRootCmd")
	assert.False(t, ok)
	_, _, ok = splitDeclKey("weird:Name")
	assert.False(t, ok)
	_, _, ok = splitDeclKey("const:")
	assert.False(t, ok)

	tok, names, ok := splitDeclKey("const:MaxBytes,MaxItems")
	require.True(t, ok)
	assert.Equal(t, "const", tok)
	assert.Equal(t, []string{"MaxBytes", "MaxItems"}, names)
}

func TestDeclKindConflict(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	broken := write("broken.go", "this is not go")
	assert.False(t, declKindConflict(broken, broken, ""))

	constVal := write("const.go", "package x\n\nconst MaxBytes = 60000\n")
	constOther := write("const_other.go", "package x\n\nconst MaxBytes = 32000\n")
	varVal := write("var.go", "package x\n\nvar MaxBytes = 32000\n")
	assert.False(t, declKindConflict(constVal, constOther, ""))
	assert.True(t, declKindConflict(varVal, constVal, ""))
	assert.True(t, declKindConflict(constVal, varVal, ""))
	assert.False(t, declKindConflict(constVal, varVal, constVal),
		"a token change whose published text still matches the original emission is template evolution")
	assert.True(t, declKindConflict(varVal, constVal, constVal),
		"a published token that differs from the original emission still conflicts")
	assert.True(t, declKindConflict(constOther, varVal, constVal),
		"a value edit on the same name as a template kind change must not overlay")
}

func TestDeclKindConflictIgnoresBlankIdentifier(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		return path
	}
	pub := write("pub.go", "package x\n\nconst _ = 0\n\nvar _ = 1\n\nconst MaxBytes = 60000\n")
	fresh := write("fresh.go", "package x\n\nvar _ = 1\n\nconst _ = 0\n\nconst MaxBytes = 32000\n")
	assert.False(t, declKindConflict(pub, fresh, ""))
	drift := detectValueDrift(pub, fresh)
	require.NotNil(t, drift)
	_, blank := drift.Decls["const:_"]
	assert.False(t, blank, "blank identifier must not be a kind-change drift key; got %v", drift.Decls)
	_, named := drift.Decls["const:MaxBytes"]
	assert.True(t, named)
}

func TestDetectValueDriftIgnoresBlankOnlyKindChange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pub := filepath.Join(dir, "pub.go")
	fresh := filepath.Join(dir, "fresh.go")
	require.NoError(t, os.WriteFile(pub, []byte("package x\n\nconst _ = 0\n"), 0o644))
	require.NoError(t, os.WriteFile(fresh, []byte("package x\n\nvar _ = 0\n"), 0o644))
	assert.Nil(t, detectValueDrift(pub, fresh))
}

func classifyKindFixture(t *testing.T, pubSrc, freshSrc string) FileClassification {
	t.Helper()
	return classifyKindFixtureWithBase(t, pubSrc, freshSrc, "")
}

func classifyKindFixtureWithBase(t *testing.T, pubSrc, freshSrc, baseSrc string) FileClassification {
	t.Helper()
	root := t.TempDir()
	pubDir := filepath.Join(root, "pub")
	freshDir := filepath.Join(root, "fresh")
	writeTree := func(dir, src string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "x"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.22\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "x", "a.go"), []byte(src), 0o644))
	}
	writeTree(pubDir, pubSrc)
	writeTree(freshDir, freshSrc)
	opts := Options{Force: true}
	if baseSrc != "" {
		baseDir := filepath.Join(root, "base")
		writeTree(baseDir, baseSrc)
		opts.BaseDir = baseDir
	}
	report, err := Classify(pubDir, freshDir, opts)
	require.NoError(t, err)
	assert.Equal(t, VerdictTemplatedClean, verdictMap(report)["go.mod"],
		"an unchanged go.mod must stay TEMPLATED-CLEAN")
	for i := range report.Files {
		if report.Files[i].Path == "internal/x/a.go" {
			return report.Files[i]
		}
	}
	t.Fatal("internal/x/a.go missing from report")
	return FileClassification{}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

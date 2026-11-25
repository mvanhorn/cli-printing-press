package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedLearnReadOnlyCommandsDoNotMutateStore(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("learnro")
	apiSpec.Learn.Enabled = true
	outputDir := filepath.Join(t.TempDir(), "learnro-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	teachSrc := readGeneratedFile(t, outputDir, "internal", "cli", "teach.go")
	statsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "learnings_stats.go")
	candidatesSrc := readGeneratedFile(t, outputDir, "internal", "cli", "learnings_candidates.go")
	playbookSrc := readGeneratedFile(t, outputDir, "internal", "cli", "teach_playbook.go")

	for _, body := range []struct {
		name string
		src  string
		fn   string
	}{
		{"stats", statsSrc, "newLearningsStatsCmd"},
		{"list", teachSrc, "newLearningsListCmd"},
		{"candidates", candidatesSrc, "newLearningsCandidatesCmd"},
		{"playbook list", playbookSrc, "newPlaybookListCmd"},
	} {
		fn := emittedFunc(t, body.src, body.fn)
		require.Contains(t, fn, "openLearnReadOnly(", body.name)
		require.NotContains(t, fn, "OpenWithContext(", body.name+" must not migrate the store")
		require.NotContains(t, fn, "PruneLearnEvents(", body.name+" must not delete rows")
	}

	listFn := emittedFunc(t, playbookSrc, "newPlaybookListCmd")
	require.Contains(t, listFn, "if !noLearnActive(flags)", "playbook list audit must honor the learn opt-out")
	require.Contains(t, listFn, `"action": "playbook-list"`)

	// recall records telemetry events on purpose. AGENTS.md keeps
	// mcp:read-only for that insert, so recall stays on the write opener.
	recall := emittedFunc(t, teachSrc, "newRecallCmd")
	require.Contains(t, recall, "store.OpenWithContext(")
	require.Contains(t, recall, "recordRecallEvents(")
	require.Contains(t, recall, `"mcp:read-only": "true"`)

	require.Contains(t, emittedFunc(t, teachSrc, "newTeachCmd"), "store.OpenWithContext(")
	amend := emittedFunc(t, playbookSrc, "newPlaybookAmendCmd")
	require.Contains(t, amend, "store.OpenWithContext(")
	require.Contains(t, amend, "appendLearningsAudit(")
	require.Contains(t, emittedFunc(t, candidatesSrc, "newLearningsConfirmCmd"), "store.OpenWithContext(")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "learn_readonly_runtime_test.go"), []byte(learnReadOnlyRuntimeTest), 0o644))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestLearn(ReadOnly|Writers|Schema)", "-count=1")
}

func emittedFunc(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "func "+name)
	require.GreaterOrEqual(t, start, 0, "func %s not found", name)
	rest := src[start+len("func "+name):]
	next := strings.Index(rest, "\nfunc ")
	if next < 0 {
		return src[start:]
	}
	return src[start : start+len("func "+name)+next]
}

const learnReadOnlyRuntimeTest = `package cli

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"learnro-pp-cli/internal/cliutil"
	"learnro-pp-cli/internal/store"
)

func TestLearnReadOnlyColdHomeCreatesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"stats", []string{"learnings", "stats", "--json"}, "stats"},
		{"list", []string{"learnings", "list", "--json"}, "[]"},
		{"candidates", []string{"learnings", "candidates", "--json"}, "[]"},
		{"playbook", []string{"playbook", "list", "--json", "--no-learn"}, "[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := isolateLearnHome(t)
			dbPath := filepath.Join(t.TempDir(), "missing-parent", "data.db")
			stdout, stderr, err := runLearn(t, append(tc.args, "--db", dbPath)...)
			if err != nil {
				t.Fatalf("cold %s: %v\nstderr=%s\nstdout=%s", tc.name, err, stderr, stdout)
			}
			assertLearnEmpty(t, tc.want, stdout)
			if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
				t.Fatalf("created %s: %v", dbPath, statErr)
			}
			if _, statErr := os.Stat(filepath.Dir(dbPath)); !os.IsNotExist(statErr) {
				t.Fatalf("created parent dir: %v", statErr)
			}
			assertNoSQLiteUnder(t, home)
			if _, statErr := os.Stat(learnAuditPath(t)); !os.IsNotExist(statErr) {
				t.Fatalf("cold opted-out command created an audit log: %v", statErr)
			}
		})
	}
}

func TestLearnReadOnlyDefaultPathCreatesNoDatabase(t *testing.T) {
	home := isolateLearnHome(t)
	for _, args := range [][]string{
		{"learnings", "stats", "--json"},
		{"learnings", "list", "--json"},
		{"learnings", "candidates", "--json"},
		{"playbook", "list", "--json", "--no-learn"},
	} {
		stdout, stderr, err := runLearn(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\nstderr=%s\nstdout=%s", args, err, stderr, stdout)
		}
	}
	assertNoSQLiteUnder(t, home)
	if _, err := os.Stat(learnAuditPath(t)); !os.IsNotExist(err) {
		t.Fatalf("opted-out playbook list created %s: %v", learnAuditPath(t), err)
	}
}

func TestLearnReadOnlyPlaybookListAuditsWhenLearningOn(t *testing.T) {
	isolateLearnHome(t)
	dbPath := filepath.Join(t.TempDir(), "missing", "data.db")
	stdout, stderr, err := runLearn(t, "playbook", "list", "--json", "--db", dbPath)
	if err != nil {
		t.Fatalf("playbook list: %v\nstderr=%s", err, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("stdout = %q, want []", stdout)
	}
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatalf("created database: %v", statErr)
	}
	data := readLearnAudit(t)
	if !strings.Contains(data, "playbook-list") {
		t.Fatalf("audit = %q, want a playbook-list line", data)
	}
}

func TestLearnReadOnlyOldStoreStaysUnmigrated(t *testing.T) {
	isolateLearnHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	seedReadableLearnDB(t, dbPath)
	settleLearnDB(t, dbPath, 1)
	readOnly := makeLearnDBReadOnly(t, dbPath)
	before := snapshotLearnDB(t, dbPath)
	if before.ver != 1 {
		t.Fatalf("user_version = %d, want 1", before.ver)
	}
	if !strings.EqualFold(before.journal, "delete") {
		t.Fatalf("journal_mode = %q, want delete", before.journal)
	}
	assertNoSidecars(t, dbPath)

	stdout, stderr, err := runLearn(t, "learnings", "list", "--json", "--db", dbPath)
	if err != nil {
		t.Fatalf("list: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	if !strings.Contains(stdout, "widget-42") {
		t.Fatalf("list = %s, want widget-42", stdout)
	}

	stdout, stderr, err = runLearn(t, "learnings", "stats", "--json", "--db", dbPath)
	if err != nil {
		t.Fatalf("stats: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	var stats map[string]any
	if err := json.Unmarshal([]byte(stdout), &stats); err != nil {
		t.Fatalf("stats json: %v\n%s", err, stdout)
	}
	if stats["recall_misses"] != float64(1) || stats["events_pruned"] != float64(0) {
		t.Fatalf("stats = %#v, want one miss and no prune", stats)
	}

	stdout, stderr, err = runLearn(t, "learnings", "candidates", "--json", "--db", dbPath)
	if err != nil {
		t.Fatalf("candidates: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	if !strings.Contains(stdout, "flag_alias") {
		t.Fatalf("candidates = %s, want flag_alias", stdout)
	}

	stdout, stderr, err = runLearn(t, "playbook", "list", "--json", "--no-learn", "--db", dbPath)
	if err != nil {
		t.Fatalf("playbook: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	if !strings.Contains(stdout, "check the id") {
		t.Fatalf("playbook = %s, want the seeded note", stdout)
	}

	after := snapshotLearnDB(t, dbPath)
	if before.sum != after.sum {
		t.Fatal("data.db bytes changed")
	}
	if before.ver != after.ver {
		t.Fatalf("user_version %d -> %d", before.ver, after.ver)
	}
	if !strings.EqualFold(before.journal, after.journal) {
		t.Fatalf("journal_mode %q -> %q", before.journal, after.journal)
	}
	if readOnly && before.perm != after.perm {
		t.Fatalf("mode %o -> %o", before.perm, after.perm)
	}
	assertNoSidecars(t, dbPath)
}

func TestLearnReadOnlySchemaTooOld(t *testing.T) {
	isolateLearnHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	writeRawLearnDB(t, dbPath, 1, nil)
	before := snapshotLearnDB(t, dbPath)

	for _, tc := range []struct {
		args  []string
		table string
	}{
		{[]string{"learnings", "stats", "--json"}, "learn_events"},
		{[]string{"learnings", "list", "--json"}, "search_learnings"},
		{[]string{"learnings", "candidates", "--json"}, "learn_candidates"},
		{[]string{"playbook", "list", "--json"}, "learning_playbooks"},
	} {
		stdout, stderr, err := runLearn(t, append(tc.args, "--db", dbPath)...)
		if err == nil {
			t.Fatalf("%v exited 0; stdout=%s stderr=%s", tc.args, stdout, stderr)
		}
		if ExitCode(err) == 2 {
			t.Fatalf("%v schema error must not be a usage error: %v\nstderr=%s", tc.args, err, stderr)
		}
		msg := err.Error()
		if !strings.Contains(msg, "too old to read "+tc.table) || !strings.Contains(msg, "does not migrate") {
			t.Fatalf("%v error = %q, want too old to read %s\nstderr=%s", tc.args, msg, tc.table, stderr)
		}
	}
	after := snapshotLearnDB(t, dbPath)
	if before.sum != after.sum || before.ver != after.ver || !strings.EqualFold(before.journal, after.journal) {
		t.Fatalf("too-old read changed store: before=%+v after=%+v", before, after)
	}
	assertNoSidecars(t, dbPath)
	if _, err := os.Stat(learnAuditPath(t)); !os.IsNotExist(err) {
		t.Fatalf("failed playbook list wrote an audit log: %v", err)
	}
}

func TestLearnReadOnlyIncompleteColumnsAreNotMigrated(t *testing.T) {
	isolateLearnHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	writeRawLearnDB(t, dbPath, 1, []string{"search_learnings", "learn_candidates", "learning_playbooks", "learn_events"})
	before := snapshotLearnDB(t, dbPath)

	stdout, stderr, err := runLearn(t, "learnings", "stats", "--json", "--db", dbPath)
	if err == nil {
		t.Fatalf("stub stats exited 0; stdout=%s", stdout)
	}
	if !strings.Contains(err.Error(), "too old to read learn_events") {
		t.Fatalf("stub stats error = %v\nstderr=%s", err, stderr)
	}
	if columnCount(t, dbPath, "learn_events") != 1 {
		t.Fatal("read-only stats added columns")
	}
	after := snapshotLearnDB(t, dbPath)
	if before.sum != after.sum || before.ver != after.ver {
		t.Fatalf("stub store changed: before ver %d after ver %d", before.ver, after.ver)
	}
}

func TestLearnReadOnlyCurrentSchemaMissingTable(t *testing.T) {
	isolateLearnHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	writeRawLearnDB(t, dbPath, store.StoreSchemaVersion, []string{"resources"})
	before := snapshotLearnDB(t, dbPath)

	stdout, stderr, err := runLearn(t, "learnings", "stats", "--json", "--db", dbPath)
	if err == nil {
		t.Fatalf("missing table exited 0; stdout=%s", stdout)
	}
	msg := err.Error()
	if !strings.Contains(msg, "missing table learn_events") {
		t.Fatalf("error = %q, want missing table\nstderr=%s", msg, stderr)
	}
	if strings.Contains(msg, "too old") || strings.Contains(msg, "newer than supported") {
		t.Fatalf("current schema reported a version error: %s", msg)
	}
	after := snapshotLearnDB(t, dbPath)
	if before.sum != after.sum || before.ver != after.ver {
		t.Fatal("missing-table read changed the store")
	}
}

func TestLearnReadOnlyCurrentSchemaBadColumnsKeepQueryError(t *testing.T) {
	isolateLearnHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	writeRawLearnDB(t, dbPath, store.StoreSchemaVersion, []string{"learn_events"})
	before := snapshotLearnDB(t, dbPath)

	stdout, stderr, err := runLearn(t, "learnings", "stats", "--json", "--db", dbPath)
	if err == nil {
		t.Fatalf("bad columns exited 0; stdout=%s", stdout)
	}
	msg := err.Error()
	if strings.Contains(msg, "too old") || strings.Contains(msg, "missing table") || strings.Contains(msg, "newer than supported") {
		t.Fatalf("current schema rewrote a query error: %s", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "no such column") {
		t.Fatalf("error = %q, want the sqlite column error\nstderr=%s", msg, stderr)
	}
	if columnCount(t, dbPath, "learn_events") != 1 {
		t.Fatal("query error rebuilt learn_events")
	}
	after := snapshotLearnDB(t, dbPath)
	if before.sum != after.sum || before.ver != after.ver {
		t.Fatal("bad-column read changed the store")
	}
}

func TestLearnReadOnlyNewerSchemaIsRejected(t *testing.T) {
	isolateLearnHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	writeRawLearnDB(t, dbPath, store.StoreSchemaVersion+50, nil)
	before := snapshotLearnDB(t, dbPath)

	stdout, stderr, err := runLearn(t, "learnings", "list", "--json", "--db", dbPath)
	if err == nil {
		t.Fatalf("newer schema exited 0; stdout=%s", stdout)
	}
	msg := err.Error()
	if !strings.Contains(msg, "newer than supported") {
		t.Fatalf("error = %q, want newer than supported\nstderr=%s", msg, stderr)
	}
	if strings.Contains(msg, "too old") {
		t.Fatalf("newer schema reported too old: %s", msg)
	}
	after := snapshotLearnDB(t, dbPath)
	if before.sum != after.sum || before.ver != after.ver {
		t.Fatal("newer-schema read changed the store")
	}
}

func TestLearnReadOnlyNewerSchemaWithTablesIsRejected(t *testing.T) {
	isolateLearnHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	seedReadableLearnDB(t, dbPath)
	settleLearnDB(t, dbPath, store.StoreSchemaVersion+50)
	before := snapshotLearnDB(t, dbPath)

	for _, args := range [][]string{
		{"learnings", "stats", "--json"},
		{"learnings", "list", "--json"},
		{"learnings", "candidates", "--json"},
		{"playbook", "list", "--json"},
	} {
		stdout, stderr, err := runLearn(t, append(args, "--db", dbPath)...)
		if err == nil {
			t.Fatalf("%v exited 0 on a newer schema; stdout=%s", args, stdout)
		}
		msg := err.Error()
		if !strings.Contains(msg, "newer than supported") || strings.Contains(msg, "too old") {
			t.Fatalf("%v error = %q, want newer than supported\nstderr=%s", args, msg, stderr)
		}
	}
	after := snapshotLearnDB(t, dbPath)
	if before.sum != after.sum || before.ver != after.ver || !strings.EqualFold(before.journal, after.journal) {
		t.Fatalf("newer readable schema changed: before=%+v after=%+v", before, after)
	}
	assertNoSidecars(t, dbPath)
	if _, err := os.Stat(learnAuditPath(t)); !os.IsNotExist(err) {
		t.Fatalf("rejected playbook list wrote an audit log: %v", err)
	}
}

func TestLearnReadOnlyCorruptAndUnreadablePaths(t *testing.T) {
	isolateLearnHome(t)

	t.Run("corrupt", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "data.db")
		payload := []byte("this is not a database")
		if err := os.WriteFile(dbPath, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := runLearn(t, "learnings", "stats", "--json", "--db", dbPath)
		if err == nil {
			t.Fatalf("corrupt file exited 0; stdout=%s", stdout)
		}
		if strings.Contains(err.Error(), "too old") || strings.Contains(err.Error(), "has not been synced") {
			t.Fatalf("corrupt file error = %v", err)
		}
		if !strings.Contains(err.Error(), "opening local database") {
			t.Fatalf("error = %v, want open failure\nstderr=%s", err, stderr)
		}
		got, readErr := os.ReadFile(dbPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != string(payload) {
			t.Fatal("corrupt file bytes changed")
		}
	})

	t.Run("empty", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "data.db")
		if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := runLearn(t, "learnings", "list", "--json", "--db", dbPath)
		if err == nil {
			t.Fatalf("empty file exited 0; stdout=%s", stdout)
		}
		// modernc treats a zero-length file as user_version 0 and does not
		// write a header on a read-only open. That is too old to read, and
		// the file must stay empty.
		if !strings.Contains(err.Error(), "too old to read search_learnings") || !strings.Contains(err.Error(), "does not migrate") {
			t.Fatalf("empty file error = %v\nstderr=%s", err, stderr)
		}
		info, statErr := os.Stat(dbPath)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Size() != 0 {
			t.Fatalf("empty file grew to %d", info.Size())
		}
	})

	t.Run("parent is a file", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		dbPath := filepath.Join(parent, "data.db")
		stdout, stderr, err := runLearn(t, "playbook", "list", "--json", "--db", dbPath)
		if err == nil {
			t.Fatalf("stat error exited 0; stdout=%s", stdout)
		}
		if strings.Contains(err.Error(), "too old") || !strings.Contains(err.Error(), "opening local database") {
			t.Fatalf("stat error = %v\nstderr=%s", err, stderr)
		}
		got, readErr := os.ReadFile(parent)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != "x" {
			t.Fatalf("parent changed: %q", got)
		}
	})

	t.Run("unicode missing path", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "donnees.db")
		// A non-ASCII sibling must not be created either.
		unicodePath := filepath.Join(filepath.Dir(dbPath), "données.db")
		stdout, stderr, err := runLearn(t, "learnings", "candidates", "--json", "--db", unicodePath)
		if err != nil {
			t.Fatalf("unicode path: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
		}
		if strings.TrimSpace(stdout) != "[]" {
			t.Fatalf("stdout = %q", stdout)
		}
		for _, path := range []string{dbPath, unicodePath} {
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("created %s: %v", path, statErr)
			}
		}
	})
}

func TestLearnReadOnlySymlinkDoesNotWriteTarget(t *testing.T) {
	isolateLearnHome(t)
	dir := t.TempDir()
	if !supportsLearnSymlink(t, dir) {
		t.Skip("symlinks are not available")
	}

	t.Run("dangling", func(t *testing.T) {
		target := filepath.Join(dir, "missing-target.db")
		link := filepath.Join(dir, "dangling.db")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := runLearn(t, "learnings", "list", "--json", "--db", link)
		if err != nil {
			t.Fatalf("dangling symlink: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
		}
		if strings.TrimSpace(stdout) != "[]" {
			t.Fatalf("stdout = %q", stdout)
		}
		if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
			t.Fatalf("dangling symlink materialized %s: %v", target, statErr)
		}
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("dangling path is no longer a symlink")
		}
	})

	t.Run("existing", func(t *testing.T) {
		target := filepath.Join(dir, "real.db")
		writeRawLearnDB(t, target, 1, nil)
		before := snapshotLearnDB(t, target)
		link := filepath.Join(dir, "alias.db")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := runLearn(t, "learnings", "stats", "--json", "--db", link)
		if err == nil || !strings.Contains(err.Error(), "too old to read learn_events") {
			t.Fatalf("symlink stats = %v stdout=%s stderr=%s", err, stdout, stderr)
		}
		after := snapshotLearnDB(t, target)
		if before.sum != after.sum || before.ver != after.ver || !strings.EqualFold(before.journal, after.journal) {
			t.Fatal("symlink open changed the target database")
		}
	})
}

func TestLearnReadOnlyCaseAliasDoesNotCreate(t *testing.T) {
	isolateLearnHome(t)
	dir := t.TempDir()
	if !caseSensitiveLearnFS(t, dir) {
		t.Skip("filesystem is not case-sensitive")
	}
	original := filepath.Join(dir, "data.db")
	if err := os.WriteFile(original, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "Data.DB")
	stdout, stderr, err := runLearn(t, "learnings", "list", "--json", "--db", alias)
	if err != nil {
		t.Fatalf("case alias: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
	}
	if _, statErr := os.Stat(alias); !os.IsNotExist(statErr) {
		t.Fatalf("created case alias: %v", statErr)
	}
	got, readErr := os.ReadFile(original)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "keep" {
		t.Fatalf("original changed: %q", got)
	}
}

func TestLearnReadOnlyPlaybookAuditOptOut(t *testing.T) {
	isolateLearnHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	seedReadableLearnDB(t, dbPath)
	settleLearnDB(t, dbPath, store.StoreSchemaVersion)

	beforeDB := snapshotLearnDB(t, dbPath)
	cases := []struct {
		name  string
		env   string
		extra []string
		write bool
	}{
		{name: "flag", extra: []string{"--no-learn"}, write: false},
		{name: "flag-false", extra: []string{"--no-learn=false"}, write: true},
		{name: "env-true", env: "true", write: false},
		{name: "env-1", env: "1", write: false},
		{name: "env-yes", env: "yes", write: false},
		{name: "env-TRUE", env: "TRUE", write: false},
		{name: "env-spaced", env: " yes ", write: false},
		{name: "env-false", env: "false", write: true},
		{name: "env-0", env: "0", write: true},
		{name: "env-no", env: "no", write: true},
		{name: "env-tru", env: "tru", write: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(noLearnEnvVar, tc.env)
			linesBefore := learnAuditLines(t)
			args := []string{"playbook", "list", "--json", "--db", dbPath}
			args = append(args, tc.extra...)
			stdout, stderr, err := runLearn(t, args...)
			if err != nil {
				t.Fatalf("playbook list: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
			}
			if !strings.Contains(stdout, "check the id") {
				t.Fatalf("stdout = %s, want the seeded note", stdout)
			}
			delta := learnAuditLines(t) - linesBefore
			if tc.write && delta != 1 {
				t.Fatalf("audit lines %+d, want 1; log=%q", delta, readLearnAudit(t))
			}
			if !tc.write && delta != 0 {
				t.Fatalf("audit lines %+d, want 0; log=%q", delta, readLearnAudit(t))
			}
		})
	}
	afterDB := snapshotLearnDB(t, dbPath)
	if beforeDB.sum != afterDB.sum || beforeDB.ver != afterDB.ver || !strings.EqualFold(beforeDB.journal, afterDB.journal) {
		t.Fatal("audit-opt-out reads changed the store")
	}
	assertNoSidecars(t, dbPath)
}

func TestLearnWritersStillMutate(t *testing.T) {
	isolateLearnHome(t)

	t.Run("teach", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "data.db")
		stdout, stderr, err := runLearn(t, "teach",
			"--query", "find widgets",
			"--resource-type", "items",
			"--resource", "widget-42",
			"--db", dbPath,
		)
		if err != nil {
			t.Fatalf("teach: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
		}
		if countLearnRows(t, dbPath, "SELECT COUNT(*) FROM search_learnings") != 1 {
			t.Fatal("teach did not write a learning")
		}
		if ver := learnUserVersion(t, dbPath); ver != store.StoreSchemaVersion {
			t.Fatalf("teach user_version = %d, want %d", ver, store.StoreSchemaVersion)
		}
	})

	t.Run("playbook amend", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "data.db")
		stdout, stderr, err := runLearn(t, "playbook", "amend",
			"--query", "find widgets",
			"--add-note", "ship the note",
			"--json",
			"--db", dbPath,
		)
		if err != nil {
			t.Fatalf("amend: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
		}
		if !strings.Contains(stdout, "amended") {
			t.Fatalf("amend stdout = %s", stdout)
		}
		if countLearnRows(t, dbPath, "SELECT COUNT(*) FROM learning_playbooks WHERE notes_text LIKE '%ship the note%'") != 1 {
			t.Fatal("amend did not write the note")
		}
		if !strings.Contains(readLearnAudit(t), "playbook-amend") {
			t.Fatalf("audit = %q, want playbook-amend", readLearnAudit(t))
		}
	})

	t.Run("playbook amend opt-out", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "absent.db")
		linesBefore := learnAuditLines(t)
		stdout, stderr, err := runLearn(t, "playbook", "amend",
			"--query", "find widgets",
			"--add-note", "should not land",
			"--no-learn",
			"--json",
			"--db", dbPath,
		)
		if err != nil {
			t.Fatalf("opt-out amend: %v\nstderr=%s\nstdout=%s", err, stderr, stdout)
		}
		if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
			t.Fatalf("opt-out amend created the store: %v", statErr)
		}
		if learnAuditLines(t) != linesBefore {
			t.Fatalf("opt-out amend wrote the audit log: %q", readLearnAudit(t))
		}
	})

	t.Run("sync", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "data.db")
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		}))
		t.Cleanup(srv.Close)
		t.Setenv("LEARNRO_BASE_URL", srv.URL)
		t.Setenv("MYAPI_TOKEN", "test-token")

		stdout, stderr, err := runLearn(t, "sync", "--db", dbPath, "--timeout", "1s")
		if _, statErr := os.Stat(dbPath); statErr != nil {
			t.Fatalf("sync did not create the store: %v\ncommand err=%v\nstderr=%s\nstdout=%s", statErr, err, stderr, stdout)
		}
		if ver := learnUserVersion(t, dbPath); ver != store.StoreSchemaVersion {
			t.Fatalf("sync user_version = %d, want %d (err=%v)", ver, store.StoreSchemaVersion, err)
		}
		if hits.Load() == 0 {
			t.Fatal("sync did not call the local server")
		}
	})
}

func TestLearnSchemaHelpers(t *testing.T) {
	if sqliteSchemaMismatch(nil) {
		t.Fatal("nil error classified as a schema mismatch")
	}
	if !sqliteSchemaMismatch(fmt.Errorf("SQL logic error: no such column: event")) {
		t.Fatal("column error not classified")
	}
	if !sqliteSchemaMismatch(fmt.Errorf("no such table: learn_events")) {
		t.Fatal("table error not classified")
	}
	if sqliteSchemaMismatch(fmt.Errorf("disk I/O error")) {
		t.Fatal("I/O error classified as a schema mismatch")
	}

	dbPath := filepath.Join(t.TempDir(), "data.db")
	s, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := learnTableReadable(s, "learn_events"); err == nil || !strings.Contains(err.Error(), "reading local store schema") {
		t.Fatalf("closed store table check = %v", err)
	}
	if err := learnSchemaVersionError(s, "learn_events", true); err == nil || !strings.Contains(err.Error(), "reading local store schema") {
		t.Fatalf("closed store version check = %v", err)
	}

	missing := filepath.Join(t.TempDir(), "nope.db")
	got, ok, err := openLearnReadOnly(context.Background(), missing)
	if err != nil || ok || got != nil {
		t.Fatalf("absent open = (%v, %v, %v), want nil, false, nil", got, ok, err)
	}
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, ok, err = openLearnReadOnly(context.Background(), filepath.Join(parent, "data.db"))
	if err == nil || ok || !strings.Contains(err.Error(), "opening local database") {
		t.Fatalf("stat open = (%v, %v)", ok, err)
	}
}

func isolateLearnHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv(noLearnEnvVar, "")
	return home
}

func runLearn(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := RootCmd()
	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func assertLearnEmpty(t *testing.T, kind, stdout string) {
	t.Helper()
	if kind == "[]" {
		if strings.TrimSpace(stdout) != "[]" {
			t.Fatalf("stdout = %q, want []", stdout)
		}
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("stats json: %v\n%s", err, stdout)
	}
	for _, key := range []string{"recall_hit_rate", "teach_to_reuse", "playbook_resolution_rate"} {
		if string(raw[key]) != "null" {
			t.Fatalf("%s = %s, want null", key, raw[key])
		}
	}
	for _, key := range []string{"recall_hits", "recall_misses", "events_pruned"} {
		if string(raw[key]) != "0" {
			t.Fatalf("%s = %s, want 0", key, raw[key])
		}
	}
}

func seedReadableLearnDB(t *testing.T, dbPath string) {
	t.Helper()
	s, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, _, err := s.UpsertLearning(context.Background(), store.UpsertLearningInput{
		Query:        "find widgets",
		ResourceID:   "widget-42",
		ResourceType: "items",
		Action:       store.LearningActionBoost,
		Source:       store.LearningSourceTaught,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UpsertPlaybook(store.UpsertPlaybookInput{
		QueryFamily: "find widgets",
		NotesText:   "check the id",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertLearnEvent(store.LearnEventRecallMiss, "fam", 0, false, store.LearnSurfaceCLI); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.DeriveCandidate(
		store.CandidateClassFlagAlias,
		"{\"failed_flag\":\"--kind\",\"corrected_flag\":\"--type\"}",
		"sig-ro",
		"find widgets",
		"items list",
	); err != nil {
		t.Fatal(err)
	}
}

func writeRawLearnDB(t *testing.T, dbPath string, version int, tables []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if _, err := raw.Exec("CREATE TABLE " + table + " (id INTEGER)"); err != nil {
			_ = raw.Close()
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	var mode string
	if err := raw.QueryRow("PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "delete") {
		t.Fatalf("journal_mode = %q", mode)
	}
	removeLearnSidecars(t, dbPath)
}

func settleLearnDB(t *testing.T, dbPath string, version int) {
	t.Helper()
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	var mode string
	if err := raw.QueryRow("PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if version >= 0 {
		if _, err := raw.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			_ = raw.Close()
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "delete") {
		t.Fatalf("journal_mode = %q", mode)
	}
	removeLearnSidecars(t, dbPath)
}

type learnDBSnap struct {
	sum     [32]byte
	journal string
	ver     int
	perm    os.FileMode
}

func snapshotLearnDB(t *testing.T, dbPath string) learnDBSnap {
	t.Helper()
	ro, err := store.OpenReadOnlyContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var journal string
	if err := ro.DB().QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		_ = ro.Close()
		t.Fatal(err)
	}
	ver, err := ro.SchemaVersion()
	_ = ro.Close()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return learnDBSnap{sum: sha256.Sum256(data), journal: journal, ver: ver, perm: info.Mode().Perm()}
}

func makeLearnDBReadOnly(t *testing.T, dbPath string) bool {
	t.Helper()
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm() &^ 0o222
	if perm == 0 {
		perm = 0o400
	}
	if err := os.Chmod(dbPath, perm); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()&0o222 == 0
}

func columnCount(t *testing.T, dbPath, table string) int {
	t.Helper()
	switch table {
	case "learn_events", "search_learnings", "learn_candidates", "learning_playbooks":
	default:
		t.Fatalf("unexpected table %q", table)
	}
	ro, err := store.OpenReadOnlyContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	rows, err := ro.DB().Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return n
}

func countLearnRows(t *testing.T, dbPath, query string) int {
	t.Helper()
	ro, err := store.OpenReadOnlyContext(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	var n int
	if err := ro.DB().QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func learnUserVersion(t *testing.T, dbPath string) int {
	t.Helper()
	return snapshotLearnDB(t, dbPath).ver
}

func removeLearnSidecars(t *testing.T, dbPath string) {
	t.Helper()
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(dbPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

func assertNoSidecars(t *testing.T, dbPath string) {
	t.Helper()
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(dbPath + suffix); !os.IsNotExist(err) {
			t.Fatalf("sidecar %s exists: %v", suffix, err)
		}
	}
}

func assertNoSQLiteUnder(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if strings.HasSuffix(name, ".db") || strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") || strings.HasSuffix(name, "-journal") {
			t.Errorf("sqlite file created: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func learnAuditPath(t *testing.T) string {
	t.Helper()
	dir, err := cliutil.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, learningsAuditFileName)
}

func readLearnAudit(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(learnAuditPath(t))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func learnAuditLines(t *testing.T) int {
	t.Helper()
	text := strings.TrimSpace(readLearnAudit(t))
	if text == "" {
		return 0
	}
	return len(strings.Split(text, "\n"))
}

func supportsLearnSymlink(t *testing.T, dir string) bool {
	t.Helper()
	target := filepath.Join(dir, "link-target")
	link := filepath.Join(dir, "link-probe")
	if err := os.WriteFile(target, []byte("t"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(target)
	if err := os.Symlink(target, link); err != nil {
		return false
	}
	os.Remove(link)
	return true
}

func caseSensitiveLearnFS(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbeRO")
	if err := os.WriteFile(probe, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(probe)
	_, err := os.Stat(filepath.Join(dir, "caseprobero"))
	return os.IsNotExist(err)
}
`

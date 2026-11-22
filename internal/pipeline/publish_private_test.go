package pipeline

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sqliteStoreHeader() []byte {
	return append([]byte("SQLite format 3\x00"), []byte("private-listing-row")...)
}

func walStoreHeader() []byte {
	return []byte{0x37, 0x7f, 0x06, 0x82, 0x00, 'w', 'a', 'l'}
}

func TestCopyPublishableManuscriptDirOmitsPrivateRunFiles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(src, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	writeBytes := func(rel string, body []byte) {
		t.Helper()
		path := filepath.Join(src, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, body, 0o644))
	}

	const marker = "private-listing-row"
	write("research/brief.md", "# brief\n")
	write("research/field-samples/notes.md", "authored field notes\n")
	write("research/novel-samples/notes.md", "research samples stay\n")
	write("proofs/shipcheck.md", "# shipcheck\n")
	write("proofs/sample-notes.md", "authored sample notes\n")
	write("proofs/"+Phase5AcceptanceFilename, "{\"status\":\"pass\"}\n")
	write("proofs/"+Phase5SkipFilename, "{\"status\":\"skip\"}\n")
	write("discovery/traffic-analysis.json", "{\"auth_stripped\":true}\n")
	write("discovery/probe-001.json", "{\"raw\":true}\n")
	write("review.md", "top-level review note\n")
	write("notes.db.md", "authored database notes\n")
	write("store.db/readme.md", "directory named like a store\n")
	write("reviews/summary.md", "plural reviews stay\n")
	write("preview/notes.md", "preview stays\n")

	write("review/session.md", marker+"\n")
	write("review/nested/state.json", marker+"\n")
	write("proofs/novel-samples/out.txt", marker+"\n")
	write("proofs/novel-sample/listing.txt", marker+"\n")
	write("proofs/nested/Samples/raw.txt", marker+"\n")
	write("proofs/samples/one.txt", marker+"\n")
	writeBytes("proofs/listings.db", sqliteStoreHeader())
	writeBytes("proofs/listings.db-wal", walStoreHeader())
	write("proofs/listings.db-shm", marker+"\n")
	write("proofs/listings.db-journal", marker+"\n")
	writeBytes("research/cache.sqlite", sqliteStoreHeader())
	write("research/cache.sqlite-wal", marker+"\n")
	writeBytes("research/cache.SQLITE3", sqliteStoreHeader())
	write("research/cache.sqlite3-journal", marker+"\n")
	write("schema.db", "plain text with a store suffix\n"+marker+"\n")
	writeBytes("proofs/payload", sqliteStoreHeader())
	writeBytes("research/renamed-wal.md", walStoreHeader())
	writeBytes("research/notes.db.md", sqliteStoreHeader())
	writeBytes("proofs/listings.db.bak", sqliteStoreHeader())

	kept := []string{
		"research/brief.md",
		"research/field-samples/notes.md",
		"research/novel-samples/notes.md",
		"proofs/shipcheck.md",
		"proofs/sample-notes.md",
		"proofs/" + Phase5AcceptanceFilename,
		"proofs/" + Phase5SkipFilename,
		"discovery/traffic-analysis.json",
		"review.md",
		"notes.db.md",
		"store.db/readme.md",
		"reviews/summary.md",
		"preview/notes.md",
	}
	dropped := []string{
		"review/session.md",
		"review/nested/state.json",
		"proofs/novel-samples/out.txt",
		"proofs/novel-sample/listing.txt",
		"proofs/nested/Samples/raw.txt",
		"proofs/samples/one.txt",
		"proofs/listings.db",
		"proofs/listings.db-wal",
		"proofs/listings.db-shm",
		"proofs/listings.db-journal",
		"research/cache.sqlite",
		"research/cache.sqlite-wal",
		"research/cache.SQLITE3",
		"research/cache.sqlite3-journal",
		"schema.db",
		"proofs/payload",
		"research/renamed-wal.md",
		"research/notes.db.md",
		"proofs/listings.db.bak",
	}

	assertTree := func(t *testing.T, dst string, excluded []string, rawIncluded bool) {
		t.Helper()
		for _, rel := range kept {
			assert.FileExists(t, filepath.Join(dst, filepath.FromSlash(rel)), rel)
		}
		for _, rel := range dropped {
			assert.NoFileExists(t, filepath.Join(dst, filepath.FromSlash(rel)), rel)
		}
		assert.NoDirExists(t, filepath.Join(dst, "review"))
		assert.NoDirExists(t, filepath.Join(dst, "proofs", "novel-samples"))
		assert.NoDirExists(t, filepath.Join(dst, "proofs", "novel-sample"))
		assert.NoDirExists(t, filepath.Join(dst, "proofs", "nested", "Samples"))
		if rawIncluded {
			assert.FileExists(t, filepath.Join(dst, "discovery", "probe-001.json"))
		} else {
			assert.NoFileExists(t, filepath.Join(dst, "discovery", "probe-001.json"))
		}
		for _, rel := range []string{
			"review",
			"proofs/novel-samples",
			"proofs/novel-sample",
			"proofs/nested/Samples",
			"proofs/listings.db",
			"proofs/listings.db-wal",
			"research/cache.sqlite",
			"research/cache.SQLITE3",
			"schema.db",
			"proofs/payload",
			"research/renamed-wal.md",
			"research/notes.db.md",
			"proofs/listings.db.bak",
		} {
			assert.Contains(t, excluded, rel)
		}
		for _, rel := range kept {
			assert.NotContains(t, excluded, rel)
		}
		assertTreeOmitsMarker(t, dst, marker)
	}

	var excluded []string
	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, CopyPublishableManuscriptDirWithOptions(src, dst, PublishableManuscriptCopyOptions{Excluded: &excluded}))
	assertTree(t, dst, excluded, false)
	ship, err := os.ReadFile(filepath.Join(dst, "proofs", "shipcheck.md"))
	require.NoError(t, err)
	assert.Equal(t, "# shipcheck\n", string(ship))
	acceptance, err := os.ReadFile(filepath.Join(dst, "proofs", Phase5AcceptanceFilename))
	require.NoError(t, err)
	assert.Equal(t, "{\"status\":\"pass\"}\n", string(acceptance))

	var includedExcluded []string
	included := filepath.Join(t.TempDir(), "included")
	require.NoError(t, CopyPublishableManuscriptDirWithOptions(src, included, PublishableManuscriptCopyOptions{
		IncludeRawCaptures: true,
		Excluded:           &includedExcluded,
	}))
	assertTree(t, included, includedExcluded, true)
	assert.NotContains(t, includedExcluded, "discovery/probe-001.json")
}

func TestCopyPublishableManuscriptDirOmitsPrivateCaseVariants(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "Review", "nested"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "Proofs", "Novel-Samples"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "Review", "nested", "state.json"), []byte("private-listing-row\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "Proofs", "Novel-Samples", "raw.txt"), []byte("private-listing-row\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "Proofs", "shipcheck.md"), []byte("# ship\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "review.md"), []byte("kept\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "Cache.DB"), sqliteStoreHeader(), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "Cache.DB-WAL"), []byte("private-listing-row\n"), 0o644))

	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, CopyPublishableManuscriptDirWithOptions(src, dst, PublishableManuscriptCopyOptions{IncludeRawCaptures: true}))
	assert.NoDirExists(t, filepath.Join(dst, "Review"))
	assert.NoDirExists(t, filepath.Join(dst, "Proofs", "Novel-Samples"))
	assert.FileExists(t, filepath.Join(dst, "Proofs", "shipcheck.md"))
	assert.FileExists(t, filepath.Join(dst, "review.md"))
	assert.NoFileExists(t, filepath.Join(dst, "Cache.DB"))
	assert.NoFileExists(t, filepath.Join(dst, "Cache.DB-WAL"))
	assertTreeOmitsMarker(t, dst, "private-listing-row")

	proofsDst := filepath.Join(t.TempDir(), "proofs-out")
	require.NoError(t, CopyPublishableManuscriptDir(filepath.Join(src, "Proofs"), proofsDst))
	assert.FileExists(t, filepath.Join(proofsDst, "shipcheck.md"))
	assert.NoDirExists(t, filepath.Join(proofsDst, "Novel-Samples"))

	plain := filepath.Join(t.TempDir(), "plain")
	require.NoError(t, os.MkdirAll(plain, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(plain, "review"), []byte("a file named review\n"), 0o644))
	plainDst := filepath.Join(t.TempDir(), "plain-dst")
	require.NoError(t, CopyPublishableManuscriptDir(plain, plainDst))
	assert.FileExists(t, filepath.Join(plainDst, "review"))

	linkRoot := filepath.Join(t.TempDir(), "link-root")
	require.NoError(t, os.MkdirAll(linkRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(linkRoot, "kept.md"), []byte("kept\n"), 0o644))
	if err := os.Symlink("kept.md", filepath.Join(linkRoot, "Review")); err != nil {
		t.Logf("symlink unavailable, skipping review-named link: %v", err)
		return
	}
	linkDst := filepath.Join(t.TempDir(), "link-dst")
	require.NoError(t, CopyPublishableManuscriptDir(linkRoot, linkDst))
	assert.FileExists(t, filepath.Join(linkDst, "kept.md"))
	assert.NoFileExists(t, filepath.Join(linkDst, "Review"))
}

func TestCopyPublishableManuscriptDirOmitsPrivateLinks(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "review"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "proofs", "novel-samples"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "proofs", "raw-output"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "review", "session.md"), []byte("private-listing-row\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "proofs", "novel-samples", "out.txt"), []byte("private-listing-row\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "proofs", "raw-output", "body.txt"), []byte("not a sample directory\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "proofs", "listings.db"), sqliteStoreHeader(), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "proofs", "shipcheck.md"), []byte("# ship\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "research", "brief.md"), []byte("# brief\n"), 0o644))

	link := func(oldname, newname string) {
		t.Helper()
		if err := os.Symlink(oldname, filepath.Join(src, filepath.FromSlash(newname))); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
	}
	link("brief.md", "research/brief-link.md")
	link("shipcheck.md", "proofs/sample-notes.md")
	link("../proofs/listings.db", "research/db-link.md")
	link("../review", "research/review-link")
	link("../review/session.md", "research/session-link.md")
	link("novel-samples/out.txt", "proofs/sample-file-link.txt")
	link("raw-output", "proofs/novel-samples-link")
	link("does-not-exist", "proofs/gone-samples")
	link("brief.md", "research/alias.db")

	sessionHard := true
	if err := os.Link(filepath.Join(src, "review", "session.md"), filepath.Join(src, "research", "session-hard.md")); err != nil {
		sessionHard = false
		t.Logf("hard link unavailable, skipping review alias: %v", err)
	}
	dbHard := true
	if err := os.Link(filepath.Join(src, "proofs", "listings.db"), filepath.Join(src, "research", "notes.md")); err != nil {
		dbHard = false
		t.Logf("hard link unavailable, skipping database alias: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, CopyPublishableManuscriptDirWithOptions(src, dst, PublishableManuscriptCopyOptions{IncludeRawCaptures: true}))
	assert.FileExists(t, filepath.Join(dst, "research", "brief.md"))
	assert.FileExists(t, filepath.Join(dst, "proofs", "shipcheck.md"))
	assert.FileExists(t, filepath.Join(dst, "proofs", "raw-output", "body.txt"))
	linkInfo, err := os.Lstat(filepath.Join(dst, "research", "brief-link.md"))
	require.NoError(t, err)
	assert.NotZero(t, linkInfo.Mode()&os.ModeSymlink)
	sampleLink, err := os.Lstat(filepath.Join(dst, "proofs", "sample-notes.md"))
	require.NoError(t, err)
	assert.NotZero(t, sampleLink.Mode()&os.ModeSymlink)
	assert.NoFileExists(t, filepath.Join(dst, "research", "db-link.md"))
	assert.NoFileExists(t, filepath.Join(dst, "research", "review-link"))
	assert.NoFileExists(t, filepath.Join(dst, "research", "session-link.md"))
	assert.NoFileExists(t, filepath.Join(dst, "proofs", "sample-file-link.txt"))
	assert.NoFileExists(t, filepath.Join(dst, "proofs", "novel-samples-link"))
	assert.NoFileExists(t, filepath.Join(dst, "proofs", "gone-samples"))
	assert.NoFileExists(t, filepath.Join(dst, "research", "alias.db"))
	if sessionHard {
		assert.NoFileExists(t, filepath.Join(dst, "research", "session-hard.md"))
	}
	if dbHard {
		assert.NoFileExists(t, filepath.Join(dst, "research", "notes.md"))
	}
	assert.NoDirExists(t, filepath.Join(dst, "review"))
	assert.NoDirExists(t, filepath.Join(dst, "proofs", "novel-samples"))
	assertTreeOmitsMarker(t, dst, "private-listing-row")
}

func assertTreeOmitsMarker(t *testing.T, root, marker string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return walkErr
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(body, []byte(marker)) {
			t.Errorf("shipped %s still contains %s", path, marker)
		}
		return nil
	})
	require.NoError(t, err)
}

package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyPublishableManuscriptDirSkipsLiveReportWhateverName(t *testing.T) {
	const leak = "/Users/operator/printing-press/library/example"
	report := []byte(`{"dir":"` + leak + `","binary":"` + leak + `/example-pp-cli","level":"full","verdict":"PASS","tests":[{"command":"items list","kind":"happy","status":"pass","output_sample":"balance 12.00"}]}` + "\n")
	device := []byte(`{"dir":"` + leak + `","binary":"` + leak + `/example-pp-cli","verdict":"unverified-device","tests":[{"command":"(device CLI)","status":"skip","reason":"manual"}]}` + "\n")

	src := filepath.Join(t.TempDir(), "src")
	proofs := filepath.Join(src, "proofs")
	research := filepath.Join(src, "research")
	require.NoError(t, os.MkdirAll(proofs, 0o755))
	require.NoError(t, os.MkdirAll(research, 0o755))

	skipped := []string{
		"2026-10-05-140047-dogfood-results-run1.json",
		"dogfood-live.json",
		"dogfood2.json",
		"live-dogfood-quick.json",
		"acme-run-publish-live-gate-rerun.json",
		"20260507T174200Z-dogfood-results-full-google-ads-token.json",
	}
	for _, name := range skipped {
		require.NoError(t, os.WriteFile(filepath.Join(proofs, name), report, 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(proofs, "dogfood-live-report.json"), device, 0o644))

	acceptance := []byte(`{"schema_version":1,"status":"pass","tests":[{"output_sample":"marker stays"}]}` + "\n")
	skipMarker := []byte(`{"schema_version":1,"status":"skip","skip_reason":"auth_required_no_credential"}` + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(proofs, Phase5AcceptanceFilename), acceptance, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(proofs, Phase5SkipFilename), skipMarker, 0o644))

	shipcheck := []byte("# Shipcheck\noutput_sample is a field name, not a transcript.\nsee https://example.com/home/docs\nroute /home/timeline/feed\n")
	notes := []byte(`{"verdict":"PASS","tests":["unit","integration"],"path":"/home/timeline/feed"}` + "\n")
	runNotes := []byte("cli " + leak + "\nstate /Users/operator/printing-press/.runstate/scope/runs/abc/proofs/out.json\nalso /opt/ci/.runstate/scope/state.json\n")
	require.NoError(t, os.WriteFile(filepath.Join(proofs, "shipcheck.md"), shipcheck, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(proofs, "notes.json"), notes, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(research, "run-notes.md"), runNotes, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(proofs, "blob.bin"), []byte{0xff, 0xfe, '/', 'U', 's', 'e', 'r', 's', '/', 'o'}, 0o644))

	assertShipped := func(t *testing.T, dst string) {
		t.Helper()
		for _, name := range skipped {
			assert.NoFileExists(t, filepath.Join(dst, "proofs", name))
		}
		assert.NoFileExists(t, filepath.Join(dst, "proofs", "dogfood-live-report.json"))

		gotAcceptance, err := os.ReadFile(filepath.Join(dst, "proofs", Phase5AcceptanceFilename))
		require.NoError(t, err)
		assert.Equal(t, string(acceptance), string(gotAcceptance))
		gotSkip, err := os.ReadFile(filepath.Join(dst, "proofs", Phase5SkipFilename))
		require.NoError(t, err)
		assert.Equal(t, string(skipMarker), string(gotSkip))

		gotShip, err := os.ReadFile(filepath.Join(dst, "proofs", "shipcheck.md"))
		require.NoError(t, err)
		assert.Equal(t, string(shipcheck), string(gotShip))
		gotNotes, err := os.ReadFile(filepath.Join(dst, "proofs", "notes.json"))
		require.NoError(t, err)
		assert.Equal(t, string(notes), string(gotNotes))
		assert.Contains(t, string(gotNotes), "/home/timeline/feed")

		gotRun, err := os.ReadFile(filepath.Join(dst, "research", "run-notes.md"))
		require.NoError(t, err)
		assert.NotContains(t, string(gotRun), "/Users/operator")
		assert.NotContains(t, string(gotRun), "/opt/ci/.runstate")
		assert.Contains(t, string(gotRun), artifacts.CLIDirPlaceholder+"/printing-press/library/example")
		assert.Contains(t, string(gotRun), artifacts.RunStatePlaceholder+"/scope/runs/abc/proofs/out.json")
		assert.Contains(t, string(gotRun), artifacts.RunStatePlaceholder+"/scope/state.json")

		blob, err := os.ReadFile(filepath.Join(dst, "proofs", "blob.bin"))
		require.NoError(t, err)
		assert.Equal(t, []byte{0xff, 0xfe, '/', 'U', 's', 'e', 'r', 's', '/', 'o'}, blob)
	}

	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, CopyPublishableManuscriptDir(src, dst))
	assertShipped(t, dst)

	included := filepath.Join(t.TempDir(), "included")
	require.NoError(t, CopyPublishableManuscriptDirWithOptions(src, included, PublishableManuscriptCopyOptions{IncludeRawCaptures: true}))
	assertShipped(t, included)
}

func TestCopyPublishableManuscriptDirOmitsWriterSamples(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	research := filepath.Join(src, "research")
	canonical := filepath.Join(research, "sniff-spec-samples")
	custom := filepath.Join(research, "raw-evidence")
	historical := filepath.Join(research, "asoview-sniff-samples")
	require.NoError(t, os.MkdirAll(canonical, 0o755))
	require.NoError(t, os.MkdirAll(custom, 0o755))
	require.NoError(t, os.MkdirAll(historical, 0o755))

	capture := &browsersniff.EnrichedCapture{
		TargetURL: "https://api.example.com",
		Entries: []browsersniff.EnrichedEntry{{
			Method:              "GET",
			URL:                 "https://api.example.com/v1/items?limit=1",
			ResponseStatus:      200,
			ResponseContentType: "application/json",
			ResponseBody:        `{"id":1}`,
		}},
	}
	n, err := browsersniff.WriteSamples(capture, canonical)
	require.NoError(t, err)
	require.Positive(t, n)
	n, err = browsersniff.WriteSamples(capture, custom)
	require.NoError(t, err)
	require.Positive(t, n)
	require.NoError(t, os.WriteFile(filepath.Join(historical, "get__items.json"), []byte(`{"raw_url":"https://api.example.com/v1/items","response_body":{},"response_body_known":true}`+"\n"), 0o644))
	notesDir := filepath.Join(research, "field-samples")
	require.NoError(t, os.MkdirAll(notesDir, 0o755))
	fieldNotes := []byte("authored sample notes\n")
	require.NoError(t, os.WriteFile(filepath.Join(notesDir, "notes.md"), fieldNotes, 0o644))
	stemDir := filepath.Join(research, "api-spec-samples")
	require.NoError(t, os.MkdirAll(stemDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stemDir, "get__config.json"), []byte(`{"raw_url":"https://api.example.com/v1/config?key=AIzaSyFAKEKEY123456","response_body":{"ok":true},"response_body_known":true}`+"\n"), 0o644))
	wideDir := filepath.Join(research, "wide-samples")
	require.NoError(t, os.MkdirAll(wideDir, 0o755))
	// The flag sits past 64 KiB and more than 4 KiB before EOF, so neither a
	// head window nor a short tail read can see it.
	wideBody := `{"raw_url":"https://api.example.com/v1/blob?key=wide-SECRET","response_body":"` + strings.Repeat("x", 70*1024) + `","response_body_known":true,"notes":"` + strings.Repeat("y", 8*1024) + `"}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(wideDir, "get__blob.json"), []byte(wideBody), 0o644))
	authoredDir := filepath.Join(research, "notes-samples")
	require.NoError(t, os.MkdirAll(authoredDir, 0o755))
	authoredJSON := []byte(`{"title":"field notes","raw_url":"https://api.example.com/v1/items","items":["one"]}` + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(authoredDir, "summary.json"), authoredJSON, 0o644))

	brief := []byte("# brief\n")
	analysis := []byte(`{"raw_url":"https://api.example.com/v1/items?limit=1","note":"synthesis"}` + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(research, "brief.md"), brief, 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(research, "field-notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(research, "field-notes", "readme.md"), []byte("notes\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(research, "analysis.json"), analysis, 0o644))

	assert.FileExists(t, filepath.Join(canonical, browsersniff.SamplesDirMarker))
	assert.FileExists(t, filepath.Join(custom, browsersniff.SamplesDirMarker))

	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, CopyPublishableManuscriptDir(src, dst))
	assert.NoDirExists(t, filepath.Join(dst, "research", "sniff-spec-samples"))
	assert.NoDirExists(t, filepath.Join(dst, "research", "raw-evidence"))
	assert.NoDirExists(t, filepath.Join(dst, "research", "asoview-sniff-samples"))
	assert.NoDirExists(t, filepath.Join(dst, "research", "api-spec-samples"))
	assert.NoDirExists(t, filepath.Join(dst, "research", "wide-samples"))
	gotField, err := os.ReadFile(filepath.Join(dst, "research", "field-samples", "notes.md"))
	require.NoError(t, err)
	assert.Equal(t, string(fieldNotes), string(gotField))
	gotAuthored, err := os.ReadFile(filepath.Join(dst, "research", "notes-samples", "summary.json"))
	require.NoError(t, err)
	assert.Equal(t, string(authoredJSON), string(gotAuthored))
	gotBrief, err := os.ReadFile(filepath.Join(dst, "research", "brief.md"))
	require.NoError(t, err)
	assert.Equal(t, string(brief), string(gotBrief))
	assert.FileExists(t, filepath.Join(dst, "research", "field-notes", "readme.md"))
	gotAnalysis, err := os.ReadFile(filepath.Join(dst, "research", "analysis.json"))
	require.NoError(t, err)
	assert.Equal(t, string(analysis), string(gotAnalysis))

	included := filepath.Join(t.TempDir(), "included")
	require.NoError(t, CopyPublishableManuscriptDirWithOptions(src, included, PublishableManuscriptCopyOptions{IncludeRawCaptures: true}))
	assert.FileExists(t, filepath.Join(included, "research", "sniff-spec-samples", browsersniff.SamplesDirMarker))
	assert.FileExists(t, filepath.Join(included, "research", "raw-evidence", browsersniff.SamplesDirMarker))
	assert.FileExists(t, filepath.Join(included, "research", "asoview-sniff-samples", "get__items.json"))
	assert.FileExists(t, filepath.Join(included, "research", "api-spec-samples", "get__config.json"))
	assert.FileExists(t, filepath.Join(included, "research", "wide-samples", "get__blob.json"))
}

func TestCopyPublishableManuscriptDirSkipsSampleScanPastSizeLimit(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	oversized := filepath.Join(src, "research", "huge-samples")
	shippable := filepath.Join(src, "research", "near-limit-samples")
	require.NoError(t, os.MkdirAll(oversized, 0o755))
	require.NoError(t, os.MkdirAll(shippable, 0o755))

	prefix := []byte(`{"raw_url":"https://api.example.com/v1/blob?key=SECRET","response_body_known":true}` + "\n")
	notes := []byte("authored notes beside an oversized capture\n")

	huge, err := os.Create(filepath.Join(oversized, "get__blob.json"))
	require.NoError(t, err)
	_, err = huge.Write(prefix)
	require.NoError(t, err)
	require.NoError(t, huge.Truncate(publishableManuscriptMaxCaptureBytes))
	require.NoError(t, huge.Close())
	require.NoError(t, os.WriteFile(filepath.Join(oversized, "notes.md"), notes, 0o644))

	near, err := os.Create(filepath.Join(shippable, "get__blob.json"))
	require.NoError(t, err)
	_, err = near.Write(prefix)
	require.NoError(t, err)
	require.NoError(t, near.Truncate(publishableManuscriptMaxCaptureBytes-1))
	require.NoError(t, near.Close())
	require.NoError(t, os.WriteFile(filepath.Join(shippable, "notes.md"), notes, 0o644))

	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, CopyPublishableManuscriptDir(src, dst))

	gotNotes, err := os.ReadFile(filepath.Join(dst, "research", "huge-samples", "notes.md"))
	require.NoError(t, err)
	assert.Equal(t, string(notes), string(gotNotes))
	assert.NoFileExists(t, filepath.Join(dst, "research", "huge-samples", "get__blob.json"))
	assert.NoDirExists(t, filepath.Join(dst, "research", "near-limit-samples"))
}

func TestRedactAbsoluteHostPaths(t *testing.T) {
	home := "/Users/operator/printing-press/library/example"
	linux := "/home/operator/printing-press/library/example"
	dotfile := "/home/operator/.config/printing-press/credentials"
	keyed := "dir=" + home
	runstate := "/Users/operator/printing-press/.runstate/scope/runs/abc/proofs/out.json"
	windows := `C:\\Users\\operator\\printing-press\\.runstate\\scope\\a.json`
	windowsHome := `C:\\Users\\operator\\printing-press\\library`
	in := "cli " + home + "\nlinux " + linux + "\ndot " + dotfile + "\n" + keyed + "\nstate " + runstate + "\nwin " + windows + "\nhome " + windowsHome + "\nurl https://example.com/home/docs\nroute /home/timeline/feed\n"
	got := redactAbsoluteHostPaths(in)
	assert.NotContains(t, got, "/Users/operator")
	assert.NotContains(t, got, "/home/operator")
	assert.NotContains(t, got, `C:\\Users\\operator`)
	assert.Contains(t, got, "dir="+artifacts.CLIDirPlaceholder+"/printing-press/library/example")
	assert.Contains(t, got, artifacts.CLIDirPlaceholder+"/printing-press/library/example")
	assert.Contains(t, got, artifacts.CLIDirPlaceholder+"/.config/printing-press/credentials")
	assert.Contains(t, got, artifacts.RunStatePlaceholder+"/scope/runs/abc/proofs/out.json")
	assert.Contains(t, got, artifacts.RunStatePlaceholder+`\\scope\\a.json`)
	assert.Contains(t, got, artifacts.CLIDirPlaceholder+`\\printing-press\\library`)
	assert.Contains(t, got, "https://example.com/home/docs")
	assert.Contains(t, got, "/home/timeline/feed")
	assert.Equal(t, got, redactAbsoluteHostPaths(got))
	if strings.Contains(got, "/Users/") || strings.Contains(got, "/home/operator") {
		t.Fatalf("redacted text still has a home path:\n%s", got)
	}
}

func TestCopyPublishableManuscriptDirRedactsReadOnlyAndEscapedWindowsHome(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	require.NoError(t, os.MkdirAll(src, 0o755))
	note := []byte("see /Users/operator/printing-press/library/example\n")
	require.NoError(t, os.WriteFile(filepath.Join(src, "note.md"), note, 0o444))
	windows := []byte("{\"dir\":\"C:\\\\Users\\\\operator\\\\printing-press\\\\library\"}\n")
	require.NoError(t, os.WriteFile(filepath.Join(src, "win.json"), windows, 0o644))

	dst := filepath.Join(t.TempDir(), "dst")
	require.NoError(t, CopyPublishableManuscriptDir(src, dst))

	gotNote, err := os.ReadFile(filepath.Join(dst, "note.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(gotNote), "/Users/operator")
	assert.Contains(t, string(gotNote), artifacts.CLIDirPlaceholder+"/printing-press/library/example")
	info, err := os.Stat(filepath.Join(dst, "note.md"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), info.Mode().Perm())

	gotWin, err := os.ReadFile(filepath.Join(dst, "win.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(gotWin), `C:\\Users\\operator`)
	assert.Contains(t, string(gotWin), artifacts.CLIDirPlaceholder)
}

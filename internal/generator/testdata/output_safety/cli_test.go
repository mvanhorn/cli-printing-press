package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

type ttyBuf struct {
	bytes.Buffer
	tty bool
}

func (t *ttyBuf) TerminalOverride() bool { return t.tty }

func TestOutputSafetyHumanFormatsEscapeControlsOnTTY(t *testing.T) {
	payload := dangerousRowPayload(t)
	for _, mode := range []string{"csv", "plain", "quiet"} {
		var tty ttyBuf
		tty.tty = true
		if err := renderSafety(mode, &tty, payload); err != nil {
			t.Fatal(err)
		}
		if hasRawTerminalControl(tty.String()) {
			t.Fatalf("%s tty leaked controls: %q", mode, tty.String())
		}
		if !strings.Contains(tty.String(), `\x1b`) {
			t.Fatalf("%s tty did not visibly escape ESC: %q", mode, tty.String())
		}
		if !strings.Contains(tty.String(), "café") || !strings.Contains(tty.String(), "東京") || !strings.Contains(tty.String(), "😀") {
			t.Fatalf("%s tty mangled unicode: %q", mode, tty.String())
		}
	}
}

func TestOutputSafetyPipedCSVAndPlainStayByteIdentical(t *testing.T) {
	payload := dangerousRowPayload(t)
	for _, mode := range []string{"csv", "plain"} {
		var pipe bytes.Buffer
		if err := renderSafety(mode, &pipe, payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(pipe.String(), "\x1b]52;") || !strings.Contains(pipe.String(), "\u0085") || !strings.Contains(pipe.String(), "\u2028") {
			t.Fatalf("%s pipe stripped controls: %q", mode, pipe.String())
		}
		if !strings.Contains(pipe.String(), "café") || !strings.Contains(pipe.String(), "😀") {
			t.Fatalf("%s pipe mangled unicode: %q", mode, pipe.String())
		}
	}

	plainPayload, err := json.Marshal([]map[string]any{{"name": "café-東京", "note": "ok"}})
	if err != nil {
		t.Fatal(err)
	}
	var pipe, tty ttyBuf
	tty.tty = true
	if err := renderSafety("csv", &pipe, plainPayload); err != nil {
		t.Fatal(err)
	}
	if err := renderSafety("csv", &tty, plainPayload); err != nil {
		t.Fatal(err)
	}
	if pipe.String() != tty.String() {
		t.Fatalf("control-free csv changed on tty\npipe %q\ntty  %q", pipe.String(), tty.String())
	}
	var plainPipe, plainTTY ttyBuf
	plainTTY.tty = true
	if err := renderSafety("plain", &plainPipe, plainPayload); err != nil {
		t.Fatal(err)
	}
	if err := renderSafety("plain", &plainTTY, plainPayload); err != nil {
		t.Fatal(err)
	}
	if plainPipe.String() != plainTTY.String() {
		t.Fatalf("control-free plain changed on tty\npipe %q\ntty  %q", plainPipe.String(), plainTTY.String())
	}
}

func TestOutputSafetyCSVFormulaPrefixSurvivesTTYEscape(t *testing.T) {
	payload, err := json.Marshal([]map[string]any{
		{"expr": "=1+1", "n": "-5", "lead": "\x1b=1"},
		{"expr": "ok", "n": "1", "lead": "zzzz\x07"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var tty ttyBuf
	tty.tty = true
	if err := printCSV(&tty, payload); err != nil {
		t.Fatal(err)
	}
	got := tty.String()
	if hasRawTerminalControl(got) {
		t.Fatalf("formula tty leaked controls: %q", got)
	}
	if !strings.Contains(got, "'=1+1") || !strings.Contains(got, "-5") {
		t.Fatalf("formula/number changed: %q", got)
	}
	if !strings.Contains(got, `\x1b=1`) || !strings.Contains(got, `\x07`) {
		t.Fatalf("split controls were not escaped: %q", got)
	}
}

func TestOutputSafetyJSONEscapesC1AndRoundTrips(t *testing.T) {
	raw := []byte("{\"name\":\"café\u0085\u009b\u2028\u2029東京😀\",\"note\":\"a&b<c>\"}")
	var flagged ttyBuf
	flagged.tty = true
	if err := printOutput(&flagged, raw, true); err != nil {
		t.Fatal(err)
	}
	assertJSONControlsEscaped(t, "json flag", flagged.Bytes())

	var pipe bytes.Buffer
	if err := printOutput(&pipe, raw, false); err != nil {
		t.Fatal(err)
	}
	assertJSONControlsEscaped(t, "piped default", pipe.Bytes())

	var object ttyBuf
	object.tty = true
	if err := printOutput(&object, raw, false); err != nil {
		t.Fatal(err)
	}
	assertJSONControlsEscaped(t, "tty object", object.Bytes())
}

func TestOutputSafetyJSONLeavesOrdinaryUnicodeAlone(t *testing.T) {
	raw := bytes.TrimSpace([]byte("{\"name\":\"café-東京😀\"}\n"))
	var want bytes.Buffer
	enc := json.NewEncoder(&want)
	enc.SetIndent("", "  ")
	if err := enc.Encode(json.RawMessage(raw)); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := printOutput(&out, raw, true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), want.Bytes()) {
		t.Fatalf("ordinary json changed\n got %q\nwant %q", out.Bytes(), want.Bytes())
	}
}

func TestOutputSafetyJSONInvalidBytesAndWriteError(t *testing.T) {
	raw := []byte("{\"name\":\"bad\xc0\x9b\"}")
	var tty ttyBuf
	tty.tty = true
	if err := printOutput(&tty, raw, true); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(tty.Bytes(), []byte{0x1b}) || bytes.Contains(tty.Bytes(), []byte{0xc0, 0x9b}) {
		t.Fatalf("overlong ESC survived json: %q", tty.Bytes())
	}
	if !json.Valid(bytes.TrimSpace(tty.Bytes())) {
		t.Fatalf("overlong json not valid: %q", tty.Bytes())
	}

	plain := []byte("not-json \x1b]52;c;x\x07")
	var text ttyBuf
	text.tty = true
	if err := printOutput(&text, plain, false); err != nil {
		t.Fatal(err)
	}
	if hasRawTerminalControl(text.String()) {
		t.Fatalf("non-json tty leaked controls: %q", text.String())
	}

	err := printOutput(errWriter{}, json.RawMessage(`{"name":"x"}`), true)
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error = %v", err)
	}
}

func TestOutputSafetyErrorEnvelopeEscapesC1(t *testing.T) {
	var buf bytes.Buffer
	writeAPIErrorEnvelope(&buf, &rootFlags{asJSON: true}, errors.New("boom \u0085\u2028"), 17)
	assertJSONControlsEscaped(t, "error envelope", buf.Bytes())
	if !strings.Contains(buf.String(), `"code":17`) && !strings.Contains(buf.String(), `"code": 17`) {
		t.Fatalf("envelope dropped code: %s", buf.String())
	}
}

func TestOutputSafetyTerminalStreamFilter(t *testing.T) {
	var buf bytes.Buffer
	filter := &terminalStreamFilter{w: &buf}
	n, err := filter.Write(nil)
	if n != 0 || err != nil {
		t.Fatalf("empty write = %d, %v", n, err)
	}
	payload := []byte("progress\r café\x1b\u0085\tkeep")
	n, err = filter.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("write = %d, %v", n, err)
	}
	got := buf.String()
	if !strings.Contains(got, "\r") || !strings.Contains(got, "\t") || !strings.Contains(got, "café") {
		t.Fatalf("stream layout or unicode changed: %q", got)
	}
	if hasRawTerminalControl(strings.ReplaceAll(strings.ReplaceAll(got, "\r", ""), "\t", "")) {
		t.Fatalf("stream leaked controls: %q", got)
	}
	if !strings.Contains(got, `\x1b`) || !strings.Contains(got, `\x85`) {
		t.Fatalf("stream did not escape controls: %q", got)
	}
	if _, err := (&terminalStreamFilter{w: errWriter{}}).Write([]byte("x")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("filter write error = %v", err)
	}
}

func TestOutputSafetyPrintTableEscapesOnTTY(t *testing.T) {
	var tty ttyBuf
	tty.tty = true
	cmd := &cobra.Command{}
	cmd.SetOut(&tty)
	cell := "café\x1b\u0085"
	if err := (&rootFlags{}).printTable(cmd, []string{"na\x1bme"}, [][]string{{cell}}); err != nil {
		t.Fatal(err)
	}
	if hasRawTerminalControl(tty.String()) || !strings.Contains(tty.String(), "café") || !strings.Contains(tty.String(), `\x1b`) {
		t.Fatalf("table tty = %q", tty.String())
	}

	var pipe bytes.Buffer
	piped := &cobra.Command{}
	piped.SetOut(&pipe)
	if err := (&rootFlags{}).printTable(piped, []string{"name"}, [][]string{{cell}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pipe.String(), "\x1b") || !strings.Contains(pipe.String(), "café") {
		t.Fatalf("table pipe stripped controls: %q", pipe.String())
	}

	if err := (&rootFlags{asJSON: true}).printTable(cmd, nil, nil); err == nil {
		t.Fatal("json table mode returned nil error")
	}
}

func TestOutputSafetyNonObjectJSONEscapesC1(t *testing.T) {
	raw := []byte("[\"café\u0085\u2028\"]")
	var tty ttyBuf
	tty.tty = true
	if err := printOutput(&tty, raw, false); err != nil {
		t.Fatal(err)
	}
	if hasRawTerminalControl(tty.String()) || bytes.Contains(tty.Bytes(), []byte("\u2028")) {
		t.Fatalf("non-object json leaked controls: %q", tty.Bytes())
	}
	if !json.Valid(bytes.TrimSpace(tty.Bytes())) {
		t.Fatalf("non-object json invalid: %q", tty.Bytes())
	}
	var arr []string
	if err := json.Unmarshal(tty.Bytes(), &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0] != "café\u0085\u2028" {
		t.Fatalf("non-object round-trip %#v", arr)
	}
}

func TestOutputSafetySyncEventsEscapeC1(t *testing.T) {
	got := syncErrorJSON("items", "", errors.New("boom \u0085\u2028"))
	if hasRawTerminalControl(got) || strings.Contains(got, "\u2028") {
		t.Fatalf("sync error leaked controls: %q", got)
	}
	var ev struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(got), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Error != "boom \u0085\u2028" {
		t.Fatalf("sync error round-trip %q", ev.Error)
	}
	warn := syncWarningJSON("items", "", 429, "rate", "slow \u0085\u2029")
	if hasRawTerminalControl(warn) || strings.Contains(warn, "\u2029") {
		t.Fatalf("sync warning leaked controls: %q", warn)
	}
	var w struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(warn), &w); err != nil {
		t.Fatal(err)
	}
	if w.Message != "slow \u0085\u2029" {
		t.Fatalf("sync warning round-trip %q", w.Message)
	}
}

func TestOutputSafetyTableDeletesNestedControls(t *testing.T) {
	got := formatCellValue([]any{map[string]any{"zzz": "x\x1b\u0085"}})
	if hasRawTerminalControl(got) {
		t.Fatalf("table cell leaked controls: %q", got)
	}
	if !strings.Contains(formatCellValue("café 😀"), "café") {
		t.Fatal("table scrub mangled unicode")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type shortWriter struct{ n int }

func (s shortWriter) Write(p []byte) (int, error) {
	if s.n < len(p) {
		return s.n, nil
	}
	return len(p), nil
}

func TestOutputSafetyDeliverMirrorPreservesTerminal(t *testing.T) {
	payload := dangerousRowPayload(t)

	tty := &ttyBuf{tty: true}
	var sink bytes.Buffer
	out := &mirrorWriter{primary: tty, copy: &sink}
	if isTerminal(out) || !out.EscapesTerminalControls() {
		t.Fatal("deliver mirror of a TTY must keep machine format and still escape")
	}
	if err := renderSafety("csv", out, payload); err != nil {
		t.Fatal(err)
	}
	if tty.String() != sink.String() {
		t.Fatal("deliver sink diverged from the terminal")
	}
	if hasRawTerminalControl(sink.String()) || !strings.Contains(sink.String(), `\x1b`) || !strings.Contains(sink.String(), "café") {
		t.Fatalf("tty deliver mirror leaked or skipped escape: %q", sink.String())
	}

	pipe := &ttyBuf{}
	sink.Reset()
	out = &mirrorWriter{primary: pipe, copy: &sink}
	if isTerminal(out) || out.EscapesTerminalControls() {
		t.Fatal("deliver mirror of a pipe must stay a pipe")
	}
	if err := renderSafety("csv", out, payload); err != nil {
		t.Fatal(err)
	}
	if pipe.String() != sink.String() || !strings.Contains(pipe.String(), "\x1b]52;") {
		t.Fatalf("piped deliver mirror changed csv: %q", pipe.String())
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { devNull.Close() })
	sink.Reset()
	out = &mirrorWriter{primary: devNull, copy: &sink}
	if isTerminal(out) || !out.EscapesTerminalControls() {
		t.Fatal("char device behind a deliver mirror must escape without selecting a terminal format")
	}
	if err := renderSafety("plain", out, payload); err != nil {
		t.Fatal(err)
	}
	if hasRawTerminalControl(sink.String()) || !strings.Contains(sink.String(), `\x1b`) {
		t.Fatalf("char-device deliver sink leaked or skipped escape: %q", sink.String())
	}

	regular, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { regular.Close() })
	sink.Reset()
	out = &mirrorWriter{primary: regular, copy: &sink}
	if isTerminal(out) || out.EscapesTerminalControls() {
		t.Fatal("regular file behind a deliver mirror must not be a terminal")
	}
	if err := renderSafety("plain", out, payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.String(), "\x1b]52;") {
		t.Fatalf("regular-file deliver mirror escaped plain output: %q", sink.String())
	}

	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	closedMirror := &mirrorWriter{primary: closed, copy: &bytes.Buffer{}}
	if isTerminal(closedMirror) || !closedMirror.EscapesTerminalControls() {
		t.Fatal("mirror of an unstatable file must escape without selecting a terminal format")
	}
}

func TestOutputSafetyDeliverMirrorKeepsMachineJSON(t *testing.T) {
	row := map[string]any{
		"id": "1", "name": "café\x1b]52;c;abc\x07\u0085東京😀", "status": "open",
		"owner": "a", "region": "b", "count": "3", "note": "kept-field",
	}
	payload, err := json.Marshal([]map[string]any{row})
	if err != nil {
		t.Fatal(err)
	}

	tty := &ttyBuf{tty: true}
	var sink bytes.Buffer
	out := &mirrorWriter{primary: tty, copy: &sink}
	if wantsHumanTable(out, &rootFlags{}) {
		t.Fatal("deliver mirror selected a human table")
	}
	if err := printOutputWithFlags(out, payload, &rootFlags{}); err != nil {
		t.Fatal(err)
	}
	if tty.String() != sink.String() {
		t.Fatal("deliver sink diverged from stdout")
	}
	assertDeliveredJSON(t, sink.Bytes())

	var bare ttyBuf
	bare.tty = true
	if !wantsHumanTable(&bare, &rootFlags{}) {
		t.Fatal("bare tty should select a human table")
	}
	if err := printOutput(&bare, payload, false); err != nil {
		t.Fatal(err)
	}
	if json.Valid(bytes.TrimSpace(bare.Bytes())) {
		t.Fatalf("bare tty rendered JSON, want a table: %q", bare.String())
	}
}

func assertDeliveredJSON(t *testing.T, out []byte) {
	t.Helper()
	if hasRawTerminalControl(string(out)) {
		t.Fatalf("deliver body leaked controls: %q", out)
	}
	if !json.Valid(bytes.TrimSpace(out)) {
		t.Fatalf("deliver body is not JSON: %q", out)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("deliver body unmarshal: %v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0]["note"] != "kept-field" {
		t.Fatalf("deliver body dropped fields: %#v", rows)
	}
	name, _ := rows[0]["name"].(string)
	if !strings.Contains(name, "café") || !strings.Contains(name, "東京") || !strings.Contains(name, "😀") || !strings.Contains(name, "\x1b") {
		t.Fatalf("deliver body changed name %q", name)
	}
}

func TestOutputSafetyDeliverMirrorWriteErrors(t *testing.T) {
	payload := []byte("abcdef")
	out := &mirrorWriter{primary: errWriter{}, copy: &bytes.Buffer{}}
	if _, err := out.Write(payload); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("primary error: %v", err)
	}

	out = &mirrorWriter{primary: shortWriter{n: 2}, copy: &bytes.Buffer{}}
	n, err := out.Write(payload)
	if n != 2 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("primary short write: n=%d err=%v", n, err)
	}

	var sink bytes.Buffer
	out = &mirrorWriter{primary: &sink, copy: errWriter{}}
	if _, err := out.Write(payload); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("copy error: %v", err)
	}
	if sink.String() != "abcdef" {
		t.Fatalf("primary was not written before copy failed: %q", sink.String())
	}

	out = &mirrorWriter{primary: &bytes.Buffer{}, copy: shortWriter{n: 1}}
	n, err = out.Write(payload)
	if n != 1 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("copy short write: n=%d err=%v", n, err)
	}
}

func dangerousRowPayload(t *testing.T) json.RawMessage {
	t.Helper()
	cell := "café\x1b]52;c;abc\x07\u0085\u009b\u2028東京😀"
	payload, err := json.Marshal([]map[string]any{{"id": cell, "name": cell}})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func renderSafety(mode string, w io.Writer, payload json.RawMessage) error {
	flags := &rootFlags{}
	switch mode {
	case "csv":
		flags.csv = true
	case "plain":
		flags.plain = true
	case "quiet":
		flags.quiet = true
	default:
		return errors.New(mode)
	}
	return printOutputWithFlags(w, payload, flags)
}

func assertJSONControlsEscaped(t *testing.T, label string, out []byte) {
	t.Helper()
	if hasRawTerminalControl(string(out)) || bytes.Contains(out, []byte("\u2028")) || bytes.Contains(out, []byte("\u2029")) {
		t.Fatalf("%s leaked controls: %q", label, out)
	}
	if !json.Valid(bytes.TrimSpace(out)) {
		t.Fatalf("%s invalid json: %q", label, out)
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("%s unmarshal: %v", label, err)
	}
	name, _ := obj["name"].(string)
	if name == "" {
		if errObj, ok := obj["error"].(string); ok {
			if !strings.Contains(errObj, "boom") || !strings.Contains(errObj, "\u0085") || !strings.Contains(errObj, "\u2028") {
				t.Fatalf("%s error round-trip %q", label, errObj)
			}
			return
		}
		t.Fatalf("%s missing name: %#v", label, obj)
	}
	if !strings.Contains(name, "café") || !strings.Contains(name, "東京") || !strings.Contains(name, "😀") {
		t.Fatalf("%s unicode round-trip %q", label, name)
	}
	if !strings.Contains(name, "\u0085") || !strings.Contains(name, "\u009b") || !strings.Contains(name, "\u2028") || !strings.Contains(name, "\u2029") {
		t.Fatalf("%s decoded value changed %q", label, name)
	}
	if note, ok := obj["note"].(string); ok && note != "a&b<c>" {
		t.Fatalf("%s note round-trip %q", label, note)
	}
}

func hasRawTerminalControl(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			return true
		}
		if r == '\u2028' || r == '\u2029' {
			return true
		}
		if r != '\t' && r != '\n' && r != '\r' && (r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)) {
			return true
		}
		i += size
	}
	return false
}

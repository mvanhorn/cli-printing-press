// Copyright 2026 mvanhorn. Licensed under Apache-2.0. See LICENSE.

package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func emittedExtractViaPycookiecheat(authGo string) (string, bool) {
	_, rest, found := strings.Cut(authGo, "func extractViaPycookiecheat(")
	if !found {
		return "", false
	}
	if i := strings.Index(rest, "\nfunc "); i >= 0 {
		rest = rest[:i]
	}
	return rest, true
}

func buildPycookiecheatArgvStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "stub.go")
	binName := "python3"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	bin := filepath.Join(dir, binName)
	const content = `package main
import (
	"encoding/json"
	"os"
)
func main() {
	path := os.Getenv("PP_ARGV_LOG")
	args := os.Args[1:]
	rec := map[string]any{"args": args}
	// runPythonFile deletes the helper after the process exits, so capture
	// the program text and mode while the file still exists.
	if len(args) > 0 {
		if info, err := os.Stat(args[0]); err == nil && !info.IsDir() {
			rec["mode"] = int(info.Mode().Perm())
			if b, err := os.ReadFile(args[0]); err == nil {
				rec["script"] = string(b)
			}
		}
	}
	data, _ := json.Marshal(rec)
	_ = os.WriteFile(path, data, 0o600)
	_, _ = os.Stdout.Write([]byte("{\"session_id\":\"ok\"}\n"))
}
`
	require.NoError(t, os.WriteFile(src, []byte(content), 0o600))
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return dir
}

// Generated cookie-auth CLIs must pass the Chrome cookie DB path to
// pycookiecheat via argv, not by interpolating it into a python -c program.
// A hostile Profile * directory name is otherwise code execution.
func TestGeneratedPycookiecheatPassesCookiePathViaArgv(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "pycookieargv-pp-cli")
	require.NoError(t, New(chromeChannelSpec("pycookieargv"), outputDir).Generate())

	authGo := readGeneratedFile(t, outputDir, "internal", "cli", "auth.go")
	fn, found := emittedExtractViaPycookiecheat(authGo)
	require.True(t, found, "expected extractViaPycookiecheat in generated auth.go")

	assert.Contains(t, fn, "sys.argv")
	assert.NotContains(t, fn, `cookie_file="%s"`)
	assert.NotContains(t, fn, `chrome_cookies("https://%s"`)
	assert.NotContains(t, fn, "fmt.Sprintf")
	assert.NotContains(t, fn, "safePath")
	assert.NotContains(t, fn, "filepath.ToSlash")
	assert.NotContains(t, fn, `"-c"`)
	assert.Contains(t, fn, "runPythonFile")
	assert.Contains(t, fn, `"https://" + cleanDomain`)
	assert.Contains(t, fn, "cookiePath")

	requireGeneratedCompiles(t, outputDir)

	stubDir := buildPycookiecheatArgvStub(t)
	runtimeTest := fmt.Sprintf(`package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const argvStubDir = %q

func usePythonStub(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", argvStubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

type argvCapture struct {
	Args   []string
	Script string
	Mode   int
}

func readArgvCapture(t *testing.T, path string) argvCapture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cap argvCapture
	if err := json.Unmarshal(data, &cap); err != nil {
		t.Fatalf("argv log: %%v\n%%s", err, data)
	}
	return cap
}

func argvHasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func TestExtractViaPycookiecheatProfile1ExtractsViaArgv(t *testing.T) {
	usePythonStub(t)
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.json")
	t.Setenv("PP_ARGV_LOG", argvLog)

	dataDir := filepath.Join(dir, "Chrome")
	got, err := extractViaPycookiecheat(cookieTool{name: "pycookiecheat", pyBin: "python3"}, ".example.com", chromeProfile{Dir: "Profile 1", DataDir: dataDir})
	if err != nil {
		t.Fatalf("extract Profile 1: %%v", err)
	}
	if got != "session_id=ok" {
		t.Fatalf("extract Profile 1 = %%q, want session_id=ok", got)
	}

	cap := readArgvCapture(t, argvLog)
	args := cap.Args
	if len(args) < 3 || argvHasFlag(args, "-c") || !strings.HasSuffix(args[0], "helper.py") {
		t.Fatalf("argv = %%#v, want [script url path]", args)
	}
	url, cookiePath := args[len(args)-2], args[len(args)-1]
	wantPath := filepath.Join(dataDir, "Profile 1", "Cookies")
	if cookiePath != wantPath {
		t.Fatalf("cookie path argv = %%q, want %%q\nargv=%%#v", cookiePath, wantPath, args)
	}
	if url != "https://example.com" {
		t.Fatalf("url argv = %%q, want https://example.com", url)
	}
	if strings.Contains(cap.Script, wantPath) || strings.Contains(cap.Script, "Profile 1") {
		t.Fatalf("python program embedded the cookie path:\n%%s", cap.Script)
	}
	if !strings.Contains(cap.Script, "sys.argv") {
		t.Fatalf("python program does not read argv:\n%%s", cap.Script)
	}
	if runtime.GOOS != "windows" && cap.Mode&0o077 != 0 {
		t.Fatalf("helper script mode = %%o, want 0600", cap.Mode)
	}
}

func TestExtractViaPycookiecheatHostileProfileHasNoSideEffect(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "pwned")
	// Working injection against the old fmt.Sprintf python -c program:
	// the trailing # comments out "/Cookies"))) so the os.system runs.
	hostile := "x\") ) ) or __import__(\"os\").system(\"touch " + sentinel + "\")#"

	argvLog := filepath.Join(dir, "argv.json")
	t.Setenv("PP_ARGV_LOG", argvLog)
	usePythonStub(t)

	dataDir := filepath.Join(dir, "Chrome")
	if _, err := extractViaPycookiecheat(cookieTool{name: "pycookiecheat", pyBin: "python3"}, ".example.com", chromeProfile{Dir: hostile, DataDir: dataDir}); err != nil {
		t.Fatalf("hostile extract (stub): %%v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("hostile profile created sentinel via stub: %%v", err)
	}
	cap := readArgvCapture(t, argvLog)
	if strings.Contains(cap.Script, hostile) || strings.Contains(cap.Script, sentinel) {
		t.Fatalf("python program embedded hostile profile:\n%%s", cap.Script)
	}
	joined := strings.Join(cap.Args, "\n")
	if !strings.Contains(joined, hostile) {
		t.Fatalf("hostile path missing from argv: %%#v", cap.Args)
	}
	if argvHasFlag(cap.Args, "-c") {
		t.Fatalf("argv still uses -c: %%#v", cap.Args)
	}

	t.Setenv("PATH", os.Getenv("PATH"))
	// Drop the stub directory so a real interpreter runs the helper file.
	parts := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	kept := parts[:0]
	for _, p := range parts {
		if p != argvStubDir {
			kept = append(kept, p)
		}
	}
	t.Setenv("PATH", strings.Join(kept, string(os.PathListSeparator)))
	pyName := "python3"
	if _, err := exec.LookPath(pyName); err != nil {
		pyName = "python"
		if _, err := exec.LookPath(pyName); err != nil {
			return
		}
	}
	if _, err := extractViaPycookiecheat(cookieTool{name: "pycookiecheat", pyBin: pyName}, ".example.com", chromeProfile{Dir: hostile, DataDir: dataDir}); err == nil {
		t.Fatal("real python extract unexpectedly succeeded")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("hostile profile executed code via real python")
	}
}

func TestExtractViaPycookiecheatOmitsPathArgWhenNoProfile(t *testing.T) {
	usePythonStub(t)
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.json")
	t.Setenv("PP_ARGV_LOG", argvLog)

	got, err := extractViaPycookiecheat(cookieTool{name: "pycookiecheat", pyBin: "python3"}, ".example.com", chromeProfile{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "session_id=ok" {
		t.Fatalf("got %%q", got)
	}
	cap := readArgvCapture(t, argvLog)
	args := cap.Args
	if len(args) < 2 || args[len(args)-1] != "https://example.com" || argvHasFlag(args, "-c") || !strings.HasSuffix(args[0], "helper.py") {
		t.Fatalf("argv = %%#v, want [script url]", args)
	}
}
`, stubDir)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "pycookiecheat_argv_test.go"), []byte(runtimeTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestExtractViaPycookiecheat")
}

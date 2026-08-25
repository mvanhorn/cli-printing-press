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
	bin := filepath.Join(dir, "py-stub")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	const content = `package main
import (
	"encoding/json"
	"os"
)
func main() {
	path := os.Getenv("PP_ARGV_LOG")
	data, _ := json.Marshal(os.Args[1:])
	_ = os.WriteFile(path, data, 0o600)
	_, _ = os.Stdout.Write([]byte("{\"session_id\":\"ok\"}\n"))
}
`
	require.NoError(t, os.WriteFile(src, []byte(content), 0o600))
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return bin
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
	assert.Contains(t, fn, `"-c", script`)
	assert.Contains(t, fn, `"https://"+cleanDomain`)
	assert.Contains(t, fn, "cookiePath")

	requireGeneratedCompiles(t, outputDir)

	stub := buildPycookiecheatArgvStub(t)
	runtimeTest := fmt.Sprintf(`package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const argvStub = %q

func readArgvLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatalf("argv log: %%v\n%%s", err, data)
	}
	return args
}

func TestExtractViaPycookiecheatProfile1ExtractsViaArgv(t *testing.T) {
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.json")
	t.Setenv("PP_ARGV_LOG", argvLog)

	dataDir := filepath.Join(dir, "Chrome")
	got, err := extractViaPycookiecheat(cookieTool{name: "pycookiecheat", pyBin: argvStub}, ".example.com", chromeProfile{Dir: "Profile 1", DataDir: dataDir})
	if err != nil {
		t.Fatalf("extract Profile 1: %%v", err)
	}
	if got != "session_id=ok" {
		t.Fatalf("extract Profile 1 = %%q, want session_id=ok", got)
	}

	args := readArgvLog(t, argvLog)
	if len(args) < 4 || args[0] != "-c" {
		t.Fatalf("argv = %%#v, want [-c script url path]", args)
	}
	script, url, cookiePath := args[1], args[2], args[3]
	wantPath := filepath.Join(dataDir, "Profile 1", "Cookies")
	if cookiePath != wantPath {
		t.Fatalf("cookie path argv = %%q, want %%q", cookiePath, wantPath)
	}
	if url != "https://example.com" {
		t.Fatalf("url argv = %%q, want https://example.com", url)
	}
	if strings.Contains(script, wantPath) || strings.Contains(script, "Profile 1") {
		t.Fatalf("python -c program embedded the cookie path:\n%%s", script)
	}
	if !strings.Contains(script, "sys.argv") {
		t.Fatalf("python -c program does not read argv:\n%%s", script)
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

	dataDir := filepath.Join(dir, "Chrome")
	if _, err := extractViaPycookiecheat(cookieTool{name: "pycookiecheat", pyBin: argvStub}, ".example.com", chromeProfile{Dir: hostile, DataDir: dataDir}); err != nil {
		t.Fatalf("hostile extract (stub): %%v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("hostile profile created sentinel via stub: %%v", err)
	}
	args := readArgvLog(t, argvLog)
	if len(args) < 2 {
		t.Fatalf("argv = %%#v", args)
	}
	if strings.Contains(args[1], hostile) || strings.Contains(args[1], sentinel) {
		t.Fatalf("python -c program embedded hostile profile:\n%%s", args[1])
	}
	if len(args) < 4 || !strings.Contains(args[3], hostile) {
		t.Fatalf("hostile path missing from argv: %%#v", args)
	}

	py, err := exec.LookPath("python3")
	if err != nil {
		if py, err = exec.LookPath("python"); err != nil {
			return
		}
	}
	if _, err := extractViaPycookiecheat(cookieTool{name: "pycookiecheat", pyBin: py}, ".example.com", chromeProfile{Dir: hostile, DataDir: dataDir}); err == nil {
		t.Fatal("real python extract unexpectedly succeeded")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("hostile profile executed code via real python")
	}
}

func TestExtractViaPycookiecheatOmitsPathArgWhenNoProfile(t *testing.T) {
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.json")
	t.Setenv("PP_ARGV_LOG", argvLog)

	got, err := extractViaPycookiecheat(cookieTool{name: "pycookiecheat", pyBin: argvStub}, ".example.com", chromeProfile{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "session_id=ok" {
		t.Fatalf("got %%q", got)
	}
	args := readArgvLog(t, argvLog)
	if len(args) != 3 || args[0] != "-c" || args[2] != "https://example.com" {
		t.Fatalf("argv = %%#v, want [-c script url]", args)
	}
}
`, stub)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "pycookiecheat_argv_test.go"), []byte(runtimeTest), 0o600))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestExtractViaPycookiecheat")
}

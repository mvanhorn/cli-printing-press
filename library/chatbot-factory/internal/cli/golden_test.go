// internal/cli/golden_test.go
package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"chatbot-factory-pp-cli/internal/pipeline"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite golden files from current output")

type goldenCase struct {
	name  string
	args  []string
	stdin string
	setup func(t *testing.T) string // returns tmpDir path where the CLI should be invoked from
}

func runCLI(t *testing.T, args []string, stdin string, workDir string) []byte {
	t.Helper()
	// Build a temporary binary in t.TempDir for testing.
	// This avoids issues with go run flag parsing and Windows path handling.
	tempDir := t.TempDir()
	binName := "chatbot-factory-pp-cli"
	if os.Getenv("GOOS") == "windows" || os.Getenv("OS") == "Windows_NT" {
		binName += ".exe"
	}
	binPath := filepath.Join(tempDir, binName)

	// Get repo root (two levels up from internal/cli)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Join(wd, "..", "..")

	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/chatbot-factory-pp-cli")
	buildCmd.Dir = repoRoot
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\noutput: %s", err, buildOut)
	}

	// Run the binary
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = bytes.NewBufferString(stdin)
	if workDir != "" {
		cmd.Dir = workDir
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli failed: %v\noutput: %s", err, out)
	}
	return out
}

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name, "golden.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update-golden to create)", err)
	}
	if !jsonEqual(t, want, got) {
		t.Fatalf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv interface{}
	if err := json.Unmarshal(a, &av); err != nil {
		return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	aa, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return bytes.Equal(aa, bb)
}

func TestGolden(t *testing.T) {
	cases := []goldenCase{
		{name: "version", args: []string{"version", "--json"}},
		{
			name: "pipeline-status",
			args: []string{"pipeline", "status", "--json", "--state", "state.json"},
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				s := pipeline.NewState("fst", "telegram")
				s.Phases["preflight"] = pipeline.PhaseState{Status: "completed", Plan: "completed"}
				s.Phases["init"] = pipeline.PhaseState{Status: "completed", Plan: "completed"}
				_ = s.Save(filepath.Join(dir, "state.json"))
				return dir
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var workDir string
			if c.setup != nil {
				workDir = c.setup(t)
			}
			out := runCLI(t, c.args, c.stdin, workDir)
			assertGolden(t, c.name, out)
		})
	}
}

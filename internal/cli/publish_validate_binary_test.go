package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildValidationBinaryRelativeDirDoesNotLeak(t *testing.T) {
	const cliName = "fixture-pp-cli"
	tests := []struct {
		name    string
		dir     func(work string) string
		sources validationFixtureSources
		wantErr bool
	}{
		{
			name:    "relative nested",
			dir:     func(string) string { return filepath.Join("library", "commerce", cliName) },
			sources: validationFixtureSources{commandPackage: true},
		},
		{
			name:    "absolute",
			dir:     func(work string) string { return filepath.Join(work, "library", "commerce", cliName) },
			sources: validationFixtureSources{commandPackage: true},
		},
		{
			name:    "dot",
			dir:     func(string) string { return "." },
			sources: validationFixtureSources{commandPackage: true},
		},
		{
			name:    "spaces",
			dir:     func(string) string { return filepath.Join("library", "commerce", "space name") },
			sources: validationFixtureSources{commandPackage: true},
		},
		{
			name:    "root package fallback",
			dir:     func(string) string { return filepath.Join("library", "commerce", cliName) },
			sources: validationFixtureSources{rootPackage: true},
		},
		{
			name:    "failed build",
			dir:     func(string) string { return filepath.Join("library", "commerce", cliName) },
			sources: validationFixtureSources{commandPackage: true, rootPackage: true, broken: true},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			work := t.TempDir()
			t.Chdir(work)
			t.Setenv("GOWORK", "off")
			t.Setenv("GOTOOLCHAIN", "local")
			t.Setenv("CGO_ENABLED", "0")
			t.Setenv("GOFLAGS", "-mod=readonly")

			dir := tt.dir(work)
			writeValidationFixture(t, dir, cliName, tt.sources)
			before := snapshotTreeFiles(t, work)

			bin, cleanup, err := buildValidationBinary(dir, cliName)
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, cleanup)
				require.Empty(t, bin)
				require.Equal(t, before, snapshotTreeFiles(t, work))
				require.Empty(t, validationTempDirs(t, work))
				require.NoError(t, assertNoDuplicatedCLITree(work, dir))
				return
			}

			require.NoError(t, err)
			t.Cleanup(cleanup)
			require.Truef(t, filepath.IsAbs(bin), "returned path %q", bin)
			info, statErr := os.Stat(bin)
			require.NoError(t, statErr)
			require.False(t, info.IsDir())

			absDir, absErr := filepath.Abs(dir)
			require.NoError(t, absErr)
			relBin, relErr := filepath.Rel(absDir, bin)
			require.NoError(t, relErr)
			require.Falsef(t, strings.HasPrefix(relBin, ".."), "binary escaped CLI dir: %s", relBin)
			tempName := strings.Split(filepath.ToSlash(relBin), "/")[0]
			require.Truef(t, strings.HasPrefix(tempName, ".publish-validate-"), "binary not in owned temp dir: %s", relBin)

			help := exec.Command(bin, "--help")
			help.Dir = dir
			out, helpErr := help.CombinedOutput()
			require.NoError(t, helpErr, string(out))
			require.Contains(t, string(out), "help-ok")

			during := snapshotTreeFiles(t, work)
			for rel, content := range before {
				got, ok := during[rel]
				require.Truef(t, ok, "source file removed: %s", rel)
				require.Equalf(t, content, got, "source file changed: %s", rel)
			}
			extra := extraTreeFiles(before, during)
			require.Equal(t, []string{filepath.ToSlash(relFromWork(t, work, bin))}, extra)
			require.NoError(t, assertNoDuplicatedCLITree(work, dir))
			require.Equal(t, []string{filepath.Dir(bin)}, validationTempDirs(t, work))

			cleanup()
			_, goneErr := os.Stat(bin)
			require.Error(t, goneErr)
			require.Equal(t, before, snapshotTreeFiles(t, work))
			require.Empty(t, validationTempDirs(t, work))
			require.NoError(t, assertNoDuplicatedCLITree(work, dir))
		})
	}
}

type validationFixtureSources struct {
	commandPackage bool
	rootPackage    bool
	broken         bool
}

func writeValidationFixture(t *testing.T, dir, cliName string, sources validationFixtureSources) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fixture\n\ngo 1.26.9\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("keep-this-source\n"), 0o644))

	body := validationFixtureMain(sources.broken)
	if sources.commandPackage {
		cmdDir := filepath.Join(dir, "cmd", cliName)
		require.NoError(t, os.MkdirAll(cmdDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(body), 0o644))
	}
	if sources.rootPackage {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(body), 0o644))
	}
}

func validationFixtureMain(broken bool) string {
	if broken {
		return "package main\n\nfunc main() {\n\tvar x int = \"bad\"\n}\n"
	}
	return `package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--help" {
		fmt.Println("help-ok")
		return
	}
	os.Exit(2)
}
`
}

func snapshotTreeFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(content)
		return nil
	})
	require.NoError(t, err)
	return out
}

func extraTreeFiles(before, during map[string]string) []string {
	var extra []string
	for rel := range during {
		if _, ok := before[rel]; !ok {
			extra = append(extra, rel)
		}
	}
	sort.Strings(extra)
	return extra
}

func relFromWork(t *testing.T, work, path string) string {
	t.Helper()
	rel, err := filepath.Rel(work, path)
	require.NoError(t, err)
	return rel
}

func validationTempDirs(t *testing.T, root string) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".publish-validate-") {
			dirs = append(dirs, path)
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(dirs)
	return dirs
}

func assertNoDuplicatedCLITree(work, dir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	relDir, err := filepath.Rel(work, absDir)
	if err != nil {
		return err
	}
	relDir = filepath.ToSlash(relDir)
	if relDir == "." {
		return nil
	}
	nested := relDir + "/" + relDir
	return filepath.WalkDir(work, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(work, path)
		if relErr != nil {
			return relErr
		}
		if strings.Contains(filepath.ToSlash(rel), nested) {
			return fmt.Errorf("duplicated CLI tree at %s", path)
		}
		return nil
	})
}

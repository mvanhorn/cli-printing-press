package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveEmbossTarget(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	t.Setenv("PRINTING_PRESS_HOME", home)

	libraryDir := filepath.Join(home, "library")
	require.NoError(t, os.MkdirAll(filepath.Join(libraryDir, "notion-pp-cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(libraryDir, "discord-pp-cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(libraryDir, "dub-2"), 0o755))

	tests := []struct {
		name    string
		flagDir string
		args    []string
		want    string
		wantErr string
	}{
		{
			name:    "flag only",
			flagDir: "/some/path",
			args:    nil,
			want:    "/some/path",
		},
		{
			name:    "positional path with separator",
			flagDir: "",
			args:    []string{"~/printing-press/library/notion-pp-cli"},
			want:    "~/printing-press/library/notion-pp-cli",
		},
		{
			name:    "exact name match",
			flagDir: "",
			args:    []string{"notion-pp-cli"},
			want:    filepath.Join(libraryDir, "notion-pp-cli"),
		},
		{
			name:    "bare name with suffix added",
			flagDir: "",
			args:    []string{"discord"},
			want:    filepath.Join(libraryDir, "discord-pp-cli"),
		},
		{
			name:    "bare name prefers library over local path",
			flagDir: "",
			args:    []string{"notion"},
			want:    filepath.Join(libraryDir, "notion-pp-cli"),
		},
		{
			name:    "bare name does not resolve local cwd entry",
			flagDir: "",
			args:    []string{"local-only"},
			wantErr: `no CLI named "local-only" found`,
		},
		{
			name:    "legacy rerun name resolves migrated slug-keyed dir",
			flagDir: "",
			args:    []string{"dub-pp-cli-2"},
			want:    filepath.Join(libraryDir, "dub-2"),
		},
		{
			name:    "no match",
			flagDir: "",
			args:    []string{"nonexistent"},
			wantErr: `no CLI named "nonexistent" found`,
		},
		{
			name:    "both flag and arg errors",
			flagDir: "/some/path",
			args:    []string{"notion-pp-cli"},
			wantErr: "specify either a positional argument or --dir, not both",
		},
		{
			name:    "neither flag nor arg errors",
			flagDir: "",
			args:    nil,
			wantErr: "specify a CLI name or path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "bare name prefers library over local path" {
				require.NoError(t, os.MkdirAll("notion", 0o755))
			}
			if tt.name == "bare name does not resolve local cwd entry" {
				require.NoError(t, os.MkdirAll("local-only", 0o755))
			}

			got, err := resolveEmbossTarget(tt.flagDir, tt.args)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestWriteEmbossDeltaReportWritesToScopedProofsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", home)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", filepath.Join(home, "repo"))

	dir := filepath.Join(t.TempDir(), "sample-pp-cli-2")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	state := pipeline.NewStateWithRun("sample", dir, "run-123", "test-scope")
	require.NoError(t, state.Save())
	before := EmbossSnapshot{ScorecardTotal: 60, ScorecardGrade: "B", VerifyPassRate: 80, VerifyPassed: 8, VerifyTotal: 10, CommandCount: 10}
	after := EmbossSnapshot{ScorecardTotal: 66, ScorecardGrade: "B", VerifyPassRate: 90, VerifyPassed: 9, VerifyTotal: 10, CommandCount: 11}
	delta := &EmbossDelta{ScorecardDelta: 6, VerifyDelta: 10, CommandDelta: 1}

	path, err := writeEmbossDeltaReport(dir, before, after, delta)
	require.NoError(t, err)

	assert.Contains(t, path, filepath.Join(home, ".runstate", "test-scope", "runs", "run-123", "proofs"))
	assert.Contains(t, filepath.Base(path), "sample-pp-cli-2")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "# Emboss Delta Report: sample-pp-cli-2"))
}

func TestResolveEmbossWorkspaceCreatesFreshRunForPublishedCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PRINTING_PRESS_HOME", home)
	t.Setenv("PRINTING_PRESS_SCOPE", "test-scope")
	t.Setenv("PRINTING_PRESS_REPO_ROOT", filepath.Join(home, "repo"))

	current := pipeline.NewStateWithRun("sample", filepath.Join(home, ".runstate", "test-scope", "runs", "run-current", "working", "sample-pp-cli"), "run-current", "test-scope")
	require.NoError(t, current.Save())

	publishedDir := filepath.Join(home, "library", "sample-pp-cli")
	require.NoError(t, os.MkdirAll(publishedDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(publishedDir, "README.md"), []byte("published"), 0o644))

	existing := pipeline.NewStateWithRun("sample", filepath.Join(home, ".runstate", "test-scope", "runs", "run-old", "working", "sample-pp-cli"), "run-old", "test-scope")
	existing.PublishedDir = publishedDir
	require.NoError(t, os.MkdirAll(existing.EffectiveWorkingDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(existing.EffectiveWorkingDir(), "README.md"), []byte("stale working copy"), 0o644))
	require.NoError(t, existing.SaveWithoutCurrentPointer())

	workingDir, baselinePath, state, err := resolveEmbossWorkspace(publishedDir)
	require.NoError(t, err)
	require.NotNil(t, state)

	assert.NotEqual(t, existing.RunID, state.RunID)
	assert.NotEqual(t, existing.EffectiveWorkingDir(), workingDir)
	assert.Equal(t, filepath.Join(state.ProofsDir(), ".emboss-baseline.json"), baselinePath)

	data, err := os.ReadFile(filepath.Join(workingDir, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, "published", string(data))

	currentState, err := pipeline.LoadCurrentState("sample")
	require.NoError(t, err)
	assert.Equal(t, current.RunID, currentState.RunID)
}

package llm

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAvailable(t *testing.T) {
	// Just verify it doesn't panic - result depends on what's installed
	_ = Available()
}

func TestRunReturnsErrorWhenNoLLM(t *testing.T) {
	// Only test this if neither CLI is installed
	_, err1 := exec.LookPath("claude")
	_, err2 := exec.LookPath("codex")
	if err1 == nil || err2 == nil {
		t.Skip("skipping: LLM CLI is installed")
	}

	_, err := Run("test prompt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no LLM CLI found")
}

func TestRunLongPromptUsesPrivateTempFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not portable to Windows")
	}

	binDir := t.TempDir()
	claudePath := filepath.Join(binDir, "claude")
	script := `#!/bin/sh
prompt="$2"
path=${prompt#Read the file at }
path=${path% and follow the instructions inside it exactly.}
stat -c %a "$path" 2>/dev/null || stat -f "%OLp" "$path"
`
	require.NoError(t, os.WriteFile(claudePath, []byte(script), 0700))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	response, err := Run(strings.Repeat("x", 100001))
	require.NoError(t, err)
	assert.Equal(t, "600", response)
}

func TestRunShortPromptDoesNotCreateTempFile(t *testing.T) {
	binDir := t.TempDir()
	tmpDir := t.TempDir()
	claudePath := filepath.Join(binDir, "claude")
	script := `#!/bin/sh
for f in "$TMPDIR"/llm-prompt-*.md; do
	if [ -e "$f" ]; then
		echo "unexpected temp file"
		exit 1
	fi
done
printf ok
`
	require.NoError(t, os.WriteFile(claudePath, []byte(script), 0700))
	t.Setenv("PATH", binDir)
	t.Setenv("TMPDIR", tmpDir)

	response, err := Run("short prompt")
	require.NoError(t, err)
	assert.Equal(t, "ok", response)
}

func TestRunCodexFallbackDoesNotCreateTempFile(t *testing.T) {
	binDir := t.TempDir()
	tmpDir := t.TempDir()
	codexPath := filepath.Join(binDir, "codex")
	script := `#!/bin/sh
for f in "$TMPDIR"/llm-prompt-*.md; do
	if [ -e "$f" ]; then
		echo "unexpected temp file"
		exit 1
	fi
done
printf ok
`
	require.NoError(t, os.WriteFile(codexPath, []byte(script), 0700))
	t.Setenv("PATH", binDir)
	t.Setenv("TMPDIR", tmpDir)

	response, err := Run(strings.Repeat("x", 100001))
	require.NoError(t, err)
	assert.Equal(t, "ok", response)
}

package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimOutputDir_Fresh(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "notion-pp-cli")

	claimed, err := ClaimOutputDir(base)
	require.NoError(t, err)
	assert.Equal(t, base, claimed)

	// Directory should exist
	info, err := os.Stat(claimed)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestClaimOutputDir_Increments(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "notion-pp-cli")

	// Pre-create base and -2
	require.NoError(t, os.Mkdir(base, 0o755))
	require.NoError(t, os.Mkdir(base+"-2", 0o755))

	claimed, err := ClaimOutputDir(base)
	require.NoError(t, err)
	assert.Equal(t, base+"-3", claimed)
}

func TestClaimOutputDir_MaxRetries(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "notion-pp-cli")

	// Pre-create base + -2 through -99
	require.NoError(t, os.Mkdir(base, 0o755))
	for i := 2; i <= 99; i++ {
		require.NoError(t, os.Mkdir(base+"-"+fmt.Sprintf("%d", i), 0o755))
	}

	_, err := ClaimOutputDir(base)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "could not claim")
}

func TestClaimOutputDir_PermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test requires non-root")
	}
	// Parent dir that doesn't exist and can't be created — on macOS /proc doesn't exist,
	// so use a path that will fail os.MkdirAll
	base := "/nonexistent-root-dir/fakedir/notion-pp-cli"

	_, err := ClaimOutputDir(base)
	assert.Error(t, err)
	// Should NOT contain "could not claim" — should be the underlying OS error
	assert.NotContains(t, err.Error(), "could not claim")
}

func TestClaimOutputDir_ConcurrentClaims(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "notion-pp-cli")

	const goroutines = 20
	var wg sync.WaitGroup
	results := make(chan string, goroutines)
	errs := make(chan error, goroutines)

	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			claimed, err := ClaimOutputDir(base)
			if err != nil {
				errs <- err
				return
			}
			results <- claimed
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	// No errors
	for err := range errs {
		t.Errorf("unexpected error: %v", err)
	}

	// All claimed paths must be unique
	seen := map[string]bool{}
	for path := range results {
		assert.False(t, seen[path], "duplicate claim: %s", path)
		seen[path] = true
	}
	assert.Len(t, seen, goroutines)
}

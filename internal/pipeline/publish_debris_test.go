package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasExecutableMagic(t *testing.T) {
	t.Parallel()

	assert.True(t, hasExecutableMagic([]byte{0x7f, 'E', 'L', 'F', 0x02}))
	assert.True(t, hasExecutableMagic([]byte{'M', 'Z', 0x90, 0x00}))
	assert.True(t, hasExecutableMagic([]byte{0xFE, 0xED, 0xFA, 0xCF}))
	assert.True(t, hasExecutableMagic([]byte{0xCF, 0xFA, 0xED, 0xFE}))
	assert.False(t, hasExecutableMagic([]byte("#!/bin/sh\n")))
	assert.False(t, hasExecutableMagic([]byte("package main\n")))
	assert.False(t, hasExecutableMagic([]byte{'M'}))
}

func TestIsLiveCheckStagingDirName(t *testing.T) {
	t.Parallel()

	assert.True(t, isLiveCheckStagingDirName(".printing-press-live-check-2895911864"))
	assert.True(t, isLiveCheckStagingDirName("printing-press-live-check-3501810456"))
	assert.False(t, isLiveCheckStagingDirName(".printing-press.json"))
	assert.False(t, isLiveCheckStagingDirName("printing-press-live-check"))
	assert.False(t, isLiveCheckStagingDirName("cmd"))
}

func TestRemoveUnshippablePackageFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	plant := func(path string, data []byte, mode os.FileMode) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, data, mode))
	}
	plant(filepath.Join(root, ".printing-press-live-check-2895911864", "foo-pp-cli.exe"), []byte("MZ leftover"), 0o755)
	plant(filepath.Join(root, "nested", "deep", ".printing-press-live-check-3501810456", "foo-pp-cli.exe"), []byte("still here"), 0o644)
	plant(filepath.Join(root, "nested", "stray.exe"), []byte("not a real image"), 0o644)
	plant(filepath.Join(root, "internal", "cli", "stray-bin"), []byte{0x7f, 'E', 'L', 'F', 0x02}, 0o755)
	plant(filepath.Join(root, "scripts", "helper.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755)
	plant(filepath.Join(root, "cmd", "foo-pp-cli", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644)
	plant(filepath.Join(root, "vendor", "Foo.class"), []byte{0xCA, 0xFE, 0xBA, 0xBE, 0x00}, 0o644)

	require.NoError(t, RemoveUnshippablePackageFiles(root))

	assert.NoDirExists(t, filepath.Join(root, ".printing-press-live-check-2895911864"))
	assert.NoDirExists(t, filepath.Join(root, "nested", "deep", ".printing-press-live-check-3501810456"))
	assert.NoFileExists(t, filepath.Join(root, "nested", "stray.exe"))
	assert.NoFileExists(t, filepath.Join(root, "internal", "cli", "stray-bin"))
	assert.FileExists(t, filepath.Join(root, "scripts", "helper.sh"))
	assert.FileExists(t, filepath.Join(root, "cmd", "foo-pp-cli", "main.go"))
	assert.FileExists(t, filepath.Join(root, "vendor", "Foo.class"))
}

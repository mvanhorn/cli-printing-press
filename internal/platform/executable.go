package platform

import (
	"path/filepath"
	"runtime"
	"strings"
)

func ExecutablePath(path string) string {
	return ExecutablePathForGOOS(path, runtime.GOOS)
}

func ExecutablePathForGOOS(path, goos string) string {
	if goos == "windows" && !strings.EqualFold(filepath.Ext(path), ".exe") {
		return path + ".exe"
	}
	return path
}

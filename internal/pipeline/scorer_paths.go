package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

// researchParentWalkDepth bounds fallback discovery to the generated CLI's
// nearby run state instead of scanning unrelated ancestors.
const researchParentWalkDepth = 3

// ResolveTargetDir returns the canonical absolute path used by scorer
// commands for a generated CLI directory. Keeping the target absolute makes
// parent discovery and subprocess working directories independent of the
// caller's current directory.
func ResolveTargetDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("target directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving target directory %q: %w", dir, err)
	}
	return filepath.Clean(abs), nil
}

// FindResearchDir returns the nearest directory containing research.json,
// starting at cliDir and walking at most the shared scorer discovery bound.
// When no file is found, it returns the canonical CLI directory so callers
// preserve their existing missing-research behavior and diagnostics.
func FindResearchDir(cliDir string) string {
	if cliDir == "" {
		return cliDir
	}
	canonical, err := ResolveTargetDir(cliDir)
	if err != nil {
		return cliDir
	}

	dir := canonical
	for steps := 0; steps <= researchParentWalkDepth; steps++ {
		if researchBelongsToTarget(dir, canonical) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return canonical
}

// researchBelongsToTarget prevents implicit ancestor discovery from adopting
// a research artifact for another CLI. Explicit research directories remain
// caller-owned; this guard applies only to the blank-directory fallback.
func researchBelongsToTarget(researchDir, cliDir string) bool {
	if _, err := os.Stat(filepath.Join(researchDir, "research.json")); err != nil {
		return false
	}
	research, err := LoadResearch(researchDir)
	if err != nil || strings.TrimSpace(research.APIName) == "" {
		return false
	}

	expectedAPI := naming.TrimCLISuffix(filepath.Base(cliDir))
	if manifest, err := ReadCLIManifest(cliDir); err == nil && strings.TrimSpace(manifest.APIName) != "" {
		expectedAPI = strings.TrimSpace(manifest.APIName)
	}
	return strings.TrimSpace(research.APIName) == expectedAPI
}

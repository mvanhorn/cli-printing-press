package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

func PressHome() string {
	if root := os.Getenv("PRINTING_PRESS_HOME"); root != "" {
		return root
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "printing-press")
	}
	return filepath.Join(home, "printing-press")
}

func WorkspaceScope() string {
	if scope := os.Getenv("PRINTING_PRESS_SCOPE"); scope != "" {
		return scope
	}

	root := repoRoot()
	base := sanitizeScopeToken(filepath.Base(root))
	if base == "" || base == "." {
		base = "workspace"
	}

	sum := sha256.Sum256([]byte(root))
	return base + "-" + hex.EncodeToString(sum[:4])
}

func WorkspaceRoot() string {
	return filepath.Join(PressHome(), "workspaces", WorkspaceScope())
}

func RunstateRoot() string {
	return filepath.Join(PressHome(), ".runstate")
}

func ScopedRunstateRoot() string {
	return filepath.Join(RunstateRoot(), WorkspaceScope())
}

func CurrentRunDir() string {
	return filepath.Join(ScopedRunstateRoot(), "current")
}

func CurrentRunPointerPath(apiName string) string {
	return filepath.Join(CurrentRunDir(), apiName+".json")
}

func RunRoot(runID string) string {
	return filepath.Join(ScopedRunstateRoot(), "runs", runID)
}

func RunStatePath(runID string) string {
	return filepath.Join(RunRoot(runID), "state.json")
}

func RunSpecPath(runID string) string {
	return filepath.Join(RunRoot(runID), "spec.json")
}

func RunManifestPath(runID string) string {
	return filepath.Join(RunRoot(runID), "manifest.json")
}

func WorkingRoot(runID string) string {
	return filepath.Join(RunRoot(runID), "working")
}

func WorkingCLIDir(apiName, runID string) string {
	return filepath.Join(WorkingRoot(runID), naming.CLI(apiName))
}

func RunResearchDir(runID string) string {
	return filepath.Join(RunRoot(runID), "research")
}

func RunProofsDir(runID string) string {
	return filepath.Join(RunRoot(runID), "proofs")
}

func RunPipelineDir(runID string) string {
	return filepath.Join(RunRoot(runID), "pipeline")
}

func RunDiscoveryDir(runID string) string {
	return filepath.Join(RunRoot(runID), "discovery")
}

func PublishedLibraryRoot() string {
	return filepath.Join(PressHome(), "library")
}

func PublishedManuscriptsRoot() string {
	return filepath.Join(PressHome(), "manuscripts")
}

// ArchivedManuscriptDir returns the path to a manuscript archive.
// The key is typically the CLI name (e.g., "steam-web-pp-cli") but may be
// an API slug (e.g., "steam") for backwards compatibility with older archives.
func ArchivedManuscriptDir(key, runID string) string {
	return filepath.Join(PublishedManuscriptsRoot(), key, runID)
}

func ArchivedResearchDir(apiName, runID string) string {
	return filepath.Join(ArchivedManuscriptDir(apiName, runID), "research")
}

func ArchivedProofsDir(apiName, runID string) string {
	return filepath.Join(ArchivedManuscriptDir(apiName, runID), "proofs")
}

func ArchivedPipelineDir(apiName, runID string) string {
	return filepath.Join(ArchivedManuscriptDir(apiName, runID), "pipeline")
}

func ArchivedDiscoveryDir(apiName, runID string) string {
	return filepath.Join(ArchivedManuscriptDir(apiName, runID), "discovery")
}

func ArchivedManifestPath(apiName, runID string) string {
	return filepath.Join(ArchivedManuscriptDir(apiName, runID), "manifest.json")
}

func LegacyWorkspaceRoot() string {
	return filepath.Join(PressHome(), "workspaces", WorkspaceScope())
}

func LegacyWorkspaceLibraryRoot() string {
	return filepath.Join(LegacyWorkspaceRoot(), "library")
}

func LegacyWorkspaceManuscriptsRoot() string {
	return filepath.Join(LegacyWorkspaceRoot(), "manuscripts")
}

func LibraryRoot() string {
	return PublishedLibraryRoot()
}

func ManuscriptsRoot() string {
	return PublishedManuscriptsRoot()
}

func ResearchDir(apiName string) string {
	return filepath.Join(LegacyWorkspaceManuscriptsRoot(), apiName, "research")
}

func ProofsDir(apiName string) string {
	return filepath.Join(LegacyWorkspaceManuscriptsRoot(), apiName, "proofs")
}

func repoRoot() string {
	if root := os.Getenv("PRINTING_PRESS_REPO_ROOT"); root != "" {
		return root
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	cur := cwd
	for {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return cwd
		}
		cur = parent
	}
}

func sanitizeScopeToken(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

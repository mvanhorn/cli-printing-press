package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/version"
	"golang.org/x/mod/semver"
)

func ensureNotOlderThanCLIManifest(cliDir, commandName string) error {
	return ensureVersionNotOlderThanCLIManifest(cliDir, commandName, version.Get())
}

func ensureVersionNotOlderThanCLIManifest(cliDir, commandName, currentVersion string) error {
	manifest, err := pipeline.ReadCLIManifest(cliDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", pipeline.CLIManifestFilename, err)
	}
	required := strings.TrimSpace(manifest.PrintingPressVersion)
	if required == "" {
		return nil
	}
	current := normalizeSemver(currentVersion)
	required = normalizeSemver(required)
	if !semver.IsValid(current) || !semver.IsValid(required) {
		return fmt.Errorf("cannot compare cli-printing-press version %q with %s printing_press_version %q", currentVersion, pipeline.CLIManifestFilename, manifest.PrintingPressVersion)
	}
	if semver.Compare(current, required) < 0 {
		return fmt.Errorf("%s refused: cli-printing-press %s is older than this CLI's generating version %s from %s; upgrade or rebuild the binary before running generator-owned sync paths", commandName, strings.TrimPrefix(current, "v"), strings.TrimPrefix(required, "v"), pipeline.CLIManifestFilename)
	}
	return nil
}

func normalizeSemver(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

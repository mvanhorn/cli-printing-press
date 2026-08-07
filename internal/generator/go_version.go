package generator

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

var goRuntimeVersionRE = regexp.MustCompile(`go([0-9]+\.[0-9]+)(?:\.([0-9]+))?`)

func currentGoDirectiveVersion() string {
	version, _ := resolveCurrentGoDirectiveVersion()
	return version
}

func currentGoToolchainVersion() string {
	version, _ := resolveCurrentGoToolchainVersion()
	return version
}

func resolveCurrentGoDirectiveVersion() (string, error) {
	if version := goDirectiveVersionFromRuntime(runtime.Version()); version != "" {
		return version, nil
	}
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err == nil {
		if version := goDirectiveVersionFromRuntime(strings.TrimSpace(string(out))); version != "" {
			return version, nil
		}
	}
	return "", fmt.Errorf("could not determine Go toolchain version from runtime %q or go env GOVERSION", runtime.Version())
}

func resolveCurrentGoToolchainVersion() (string, error) {
	version, err := resolveCurrentGoDirectiveVersion()
	if err != nil {
		return "", err
	}
	return "go" + version, nil
}

func goDirectiveVersionFromRuntime(version string) string {
	match := goRuntimeVersionRE.FindStringSubmatch(version)
	if match == nil {
		return ""
	}
	if match[2] == "" {
		return match[1] + ".0"
	}
	return match[1] + "." + match[2]
}

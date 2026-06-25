package generator

import (
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

var goRuntimeVersionRE = regexp.MustCompile(`go([0-9]+\.[0-9]+)(?:\.([0-9]+))?`)

func currentGoDirectiveVersion() string {
	if version := goDirectiveVersionFromRuntime(runtime.Version()); version != "" {
		return version
	}
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err == nil {
		if version := goDirectiveVersionFromRuntime(strings.TrimSpace(string(out))); version != "" {
			return version
		}
	}
	panic("could not determine Go toolchain version")
}

func currentGoToolchainVersion() string {
	return "go" + currentGoDirectiveVersion()
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

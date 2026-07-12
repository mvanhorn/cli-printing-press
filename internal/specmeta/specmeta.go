package specmeta

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func RebaseAuthEnvPrefix(auth *spec.AuthConfig, oldName, newName string) {
	if auth == nil || oldName == "" || newName == "" || oldName == newName {
		return
	}
	oldPrefix := naming.EnvPrefix(oldName) + "_"
	newPrefix := naming.EnvPrefix(newName) + "_"
	for i, envVar := range auth.EnvVars {
		if suffix, ok := strings.CutPrefix(envVar, oldPrefix); ok {
			auth.EnvVars[i] = newPrefix + suffix
		}
	}
	for i := range auth.EnvVarSpecs {
		if suffix, ok := strings.CutPrefix(auth.EnvVarSpecs[i].Name, oldPrefix); ok {
			auth.EnvVarSpecs[i].Name = newPrefix + suffix
		}
	}
}

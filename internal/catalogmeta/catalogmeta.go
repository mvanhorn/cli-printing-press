package catalogmeta

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
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

func IsReplaceableBaseURL(baseURL string, placeholder bool) bool {
	switch strings.TrimRight(strings.TrimSpace(baseURL), "/") {
	case "", strings.TrimRight(spec.PlaceholderBaseURL, "/"), "https://api.example.com":
		return true
	default:
		return placeholder
	}
}

func ApplyRuntimeMetadata(apiSpec *spec.APISpec, entry *catalog.Entry) {
	if apiSpec == nil || entry == nil {
		return
	}
	if entry.BaseURL != "" && IsReplaceableBaseURL(apiSpec.BaseURL, apiSpec.BaseURLIsPlaceholder) {
		apiSpec.BaseURL = strings.TrimRight(entry.BaseURL, "/")
		apiSpec.BaseURLIsPlaceholder = false
	}
	if entry.DisplayName != "" {
		apiSpec.DisplayName = entry.DisplayName
		apiSpec.DisplayNameDerivedFromTitle = false
	}
	if entry.Description != "" {
		apiSpec.CLIDescription = entry.Description
	}
	if entry.AuthKeyURL != "" {
		apiSpec.Auth.KeyURL = entry.AuthKeyURL
	}
	if entry.AuthInstructions != "" {
		apiSpec.Auth.Instructions = entry.AuthInstructions
	}
	if entry.ClientPattern != "" {
		apiSpec.ClientPattern = entry.ClientPattern
	}
	if entry.HTTPTransport != "" {
		apiSpec.HTTPTransport = entry.HTTPTransport
	}
	if entry.SpecSource != "" {
		apiSpec.SpecSource = entry.SpecSource
	}
}

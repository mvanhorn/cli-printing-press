package catalogmeta

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// ApplyCatalogAuthEnvVars overrides the parser-derived auth env var list with
// curator-declared canonical names from a catalog entry. The catalog names
// take precedence; previously declared names (the parser's default
// <API>_BEARER_AUTH / <API>_TOKEN, or any x-auth-env-vars values) are
// appended as trailing fallbacks so operators on existing setups don't need
// a rename to keep auth working. EnvVarSpecs are rebuilt as a non-required
// OR-case (any matching env var satisfies auth) so the generator's
// AuthHeader() code reads each in declared order and returns the first
// non-empty value.
//
// The override is a no-op when catalogEnvVars is empty, the spec carries no
// auth surface, or the spec declares HTTP Basic auth (auth.Format contains
// "Basic "). Basic auth treats env vars as a credential pair (username:
// password), not as alternatives, so layering an OR-case list on top would
// generate code that returns an empty Authorization header. Curators who need
// to override basic-auth env var names should declare them via the spec's
// x-auth-env-vars extension instead.
func ApplyCatalogAuthEnvVars(auth *spec.AuthConfig, catalogEnvVars []string) {
	if auth == nil || len(catalogEnvVars) == 0 || auth.Type == "" || auth.Type == "none" {
		return
	}
	if strings.Contains(strings.ToLower(auth.Format), "basic ") {
		return
	}
	merged := mergeAuthEnvVars(catalogEnvVars, auth.EnvVars)
	if len(merged) == 0 {
		return
	}
	auth.EnvVars = merged
	auth.EnvVarSpecs = spec.NewORCaseEnvVarSpecs(merged)
}

func mergeAuthEnvVars(catalog, existing []string) []string {
	seen := make(map[string]struct{}, len(catalog)+len(existing))
	merged := make([]string, 0, len(catalog)+len(existing))
	for _, source := range [][]string{catalog, existing} {
		for _, name := range source {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			merged = append(merged, name)
		}
	}
	return merged
}

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
	ApplyCatalogAuthEnvVars(&apiSpec.Auth, entry.AuthEnvVars)
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

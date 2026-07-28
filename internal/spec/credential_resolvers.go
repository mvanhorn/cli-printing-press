package spec

import (
	"fmt"
	"sort"
	"strings"
)

// Credential resolver catalog names, selectable via auth.credential_resolvers.
const (
	CredentialResolverFile        = "file"
	CredentialResolverBitwarden   = "bitwarden"
	CredentialResolverOnePassword = "onepassword"
)

// credentialResolverTemplates maps a catalog name to the template that emits it.
// Adding a secret manager is an entry here plus one template file — the
// validator, the registry and the dispatcher are vendor-agnostic and do not
// change.
var credentialResolverTemplates = map[string]string{
	CredentialResolverFile:        "platform_resolver_file.go.tmpl",
	CredentialResolverBitwarden:   "platform_resolver_bitwarden.go.tmpl",
	CredentialResolverOnePassword: "platform_resolver_onepassword.go.tmpl",
}

// defaultCredentialResolvers is what a spec that says nothing gets.
//
// Deliberately the one resolver that depends on nothing installed. A vendor
// default is what this field exists to remove: it makes every printed CLI carry
// a dependency its operator never chose, and leaves a subprocess call to a
// password manager in trees that will never use it.
var defaultCredentialResolvers = []string{CredentialResolverFile}

// KnownCredentialResolvers returns the catalog, sorted.
func KnownCredentialResolvers() []string {
	names := make([]string, 0, len(credentialResolverTemplates))
	for name := range credentialResolverTemplates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// EffectiveCredentialResolvers returns the resolvers to compile in, normalized
// and de-duplicated.
func (a AuthConfig) EffectiveCredentialResolvers() []string {
	if len(a.CredentialResolvers) == 0 {
		return append([]string(nil), defaultCredentialResolvers...)
	}
	seen := map[string]struct{}{}
	names := make([]string, 0, len(a.CredentialResolvers))
	for _, raw := range a.CredentialResolvers {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return append([]string(nil), defaultCredentialResolvers...)
	}
	return names
}

// CredentialResolverTemplates returns the template files for the selected
// resolvers, plus the shared subprocess helper when any resolver needs it.
func (a AuthConfig) CredentialResolverTemplates() []string {
	templates := []string{}
	needsExec := false
	for _, name := range a.EffectiveCredentialResolvers() {
		tmpl, ok := credentialResolverTemplates[name]
		if !ok {
			continue
		}
		templates = append(templates, tmpl)
		if name != CredentialResolverFile {
			needsExec = true
		}
	}
	if needsExec {
		templates = append(templates, "platform_resolver_exec.go.tmpl")
	}
	sort.Strings(templates)
	return templates
}

// ValidateCredentialResolvers rejects unknown catalog names. An unknown name is
// an error rather than a silent skip: a spec asking for "vault" and getting a
// CLI that cannot resolve vault:// references would fail at credential-read
// time, which is the worst moment to discover a typo.
func ValidateCredentialResolvers(names []string) error {
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if _, ok := credentialResolverTemplates[name]; !ok {
			return fmt.Errorf("auth.credential_resolvers contains unknown resolver %q (known: %s)",
				raw, strings.Join(KnownCredentialResolvers(), ", "))
		}
	}
	return nil
}

// SyntheticCredentialReference builds a reference valid under the first
// resolver a spec selected, for use as a test fixture.
//
// Generated conformance tests need a reference that PASSES validation, and what
// passes now depends on which resolvers the spec asked for. Hardcoding one
// vendor's syntax in a test template is how the whole 1Password assumption got
// baked in originally.
func SyntheticCredentialReference(auth AuthConfig, name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "synthetic"
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)
	resolvers := auth.EffectiveCredentialResolvers()
	if len(resolvers) == 0 {
		return "file:///tmp/printing-press-synthetic/" + name
	}
	switch resolvers[0] {
	case CredentialResolverBitwarden:
		return "bws://synthetic-project/" + name
	case CredentialResolverOnePassword:
		return "op://Synthetic/Printing Press/" + name
	default:
		return "file:///tmp/printing-press-synthetic/" + name
	}
}

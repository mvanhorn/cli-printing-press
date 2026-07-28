package spec

import (
	"strings"
	"testing"
)

// The default must depend on nothing installed and must name no vendor. This is
// the whole point of the field: before it, every printed CLI shipped a
// 1Password resolver and an op://-only reference validator whether or not its
// operator used 1Password.
func TestDefaultCredentialResolversNameNoVendor(t *testing.T) {
	got := AuthConfig{}.EffectiveCredentialResolvers()
	if len(got) != 1 || got[0] != CredentialResolverFile {
		t.Fatalf("default resolvers = %v, want [%s]", got, CredentialResolverFile)
	}
	for _, tmpl := range (AuthConfig{}).CredentialResolverTemplates() {
		if strings.Contains(tmpl, "onepassword") || strings.Contains(tmpl, "bitwarden") {
			t.Errorf("a default print emits vendor resolver %s", tmpl)
		}
	}
}

// The file resolver needs no subprocess, so a default print must not carry the
// exec helper either.
func TestDefaultPrintOmitsExecHelper(t *testing.T) {
	for _, tmpl := range (AuthConfig{}).CredentialResolverTemplates() {
		if strings.Contains(tmpl, "resolver_exec") {
			t.Errorf("default print emits %s, which only subprocess-backed resolvers need", tmpl)
		}
	}
}

func TestSelectedResolversEmitTheirTemplates(t *testing.T) {
	cases := []struct {
		name     string
		selected []string
		want     []string
	}{
		{
			name:     "bitwarden pulls in the exec helper",
			selected: []string{CredentialResolverBitwarden},
			want:     []string{"platform_resolver_bitwarden.go.tmpl", "platform_resolver_exec.go.tmpl"},
		},
		{
			name:     "file alone stays subprocess-free",
			selected: []string{CredentialResolverFile},
			want:     []string{"platform_resolver_file.go.tmpl"},
		},
		{
			name:     "several resolvers coexist",
			selected: []string{CredentialResolverFile, CredentialResolverBitwarden},
			want: []string{
				"platform_resolver_bitwarden.go.tmpl",
				"platform_resolver_exec.go.tmpl",
				"platform_resolver_file.go.tmpl",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AuthConfig{CredentialResolvers: c.selected}.CredentialResolverTemplates()
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("templates = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCredentialResolversNormalizeAndDeduplicate(t *testing.T) {
	got := AuthConfig{CredentialResolvers: []string{" File ", "FILE", "bitwarden", ""}}.EffectiveCredentialResolvers()
	if len(got) != 2 || got[0] != CredentialResolverFile || got[1] != CredentialResolverBitwarden {
		t.Fatalf("resolvers = %v, want [file bitwarden]", got)
	}
}

// An unknown name must be an error rather than a silent skip: a spec asking for
// a resolver it does not get produces a CLI that fails at credential-read time,
// which is the worst moment to discover a typo.
func TestUnknownResolverIsRejectedAndNamesTheCatalog(t *testing.T) {
	err := ValidateCredentialResolvers([]string{"vault"})
	if err == nil {
		t.Fatal("unknown resolver accepted")
	}
	for _, known := range KnownCredentialResolvers() {
		if !strings.Contains(err.Error(), known) {
			t.Errorf("error does not name known resolver %q: %v", known, err)
		}
	}
}

func TestKnownResolversAreAccepted(t *testing.T) {
	if err := ValidateCredentialResolvers(KnownCredentialResolvers()); err != nil {
		t.Fatalf("catalog rejected its own names: %v", err)
	}
}

// Generated conformance tests need a fixture that PASSES validation, and what
// passes depends on which resolvers were selected. Hardcoding one vendor's
// syntax in a test template is how the 1Password assumption got baked in.
func TestSyntheticReferenceMatchesSelectedResolver(t *testing.T) {
	cases := []struct {
		selected []string
		prefix   string
	}{
		{nil, "file://"},
		{[]string{CredentialResolverFile}, "file://"},
		{[]string{CredentialResolverBitwarden}, "bws://"},
		{[]string{CredentialResolverOnePassword}, "op://"},
	}
	for _, c := range cases {
		got := SyntheticCredentialReference(AuthConfig{CredentialResolvers: c.selected}, "token")
		if !strings.HasPrefix(got, c.prefix) {
			t.Errorf("fixture for %v = %q, want prefix %q", c.selected, got, c.prefix)
		}
	}
}

// Distinct names must stay distinct, or two sources in a conformance fixture
// collapse onto one credential.
func TestSyntheticReferencesAreDistinctPerName(t *testing.T) {
	auth := AuthConfig{CredentialResolvers: []string{CredentialResolverBitwarden}}
	a := SyntheticCredentialReference(auth, "client-id")
	b := SyntheticCredentialReference(auth, "client-secret")
	if a == b {
		t.Fatalf("distinct names produced the same fixture %q", a)
	}
}

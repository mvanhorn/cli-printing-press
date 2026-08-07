package specmeta

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestRebaseAuthEnvPrefix(t *testing.T) {
	auth := spec.AuthConfig{
		EnvVars: []string{"OLD_API_KEY", "OTHER_TOKEN"},
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "OLD_TOKEN"},
			{Name: "OTHER_SECRET"},
		},
	}

	RebaseAuthEnvPrefix(&auth, "old", "new")

	if got, want := auth.EnvVars[0], "NEW_API_KEY"; got != want {
		t.Fatalf("EnvVars[0] = %q, want %q", got, want)
	}
	if got, want := auth.EnvVars[1], "OTHER_TOKEN"; got != want {
		t.Fatalf("EnvVars[1] = %q, want %q", got, want)
	}
	if got, want := auth.EnvVarSpecs[0].Name, "NEW_TOKEN"; got != want {
		t.Fatalf("EnvVarSpecs[0].Name = %q, want %q", got, want)
	}
	if got, want := auth.EnvVarSpecs[1].Name, "OTHER_SECRET"; got != want {
		t.Fatalf("EnvVarSpecs[1].Name = %q, want %q", got, want)
	}
}

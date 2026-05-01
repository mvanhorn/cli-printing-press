package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestValidateNarrativeCmd_RequiresFlags confirms cobra returns a
// usage error rather than nil-derefing or running silently when the
// caller forgets --research / --binary.
func TestValidateNarrativeCmd_RequiresFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no flags", nil, "--research is required"},
		{"only research", []string{"--research", "/dev/null"}, "--binary is required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := newValidateNarrativeCmd()
			cmd.SetArgs(tc.args)
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error mentioning %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should contain %q", err.Error(), tc.want)
			}
		})
	}
}

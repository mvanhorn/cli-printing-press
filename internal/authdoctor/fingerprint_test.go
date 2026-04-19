package authdoctor

import "testing"

func TestFingerprint(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"short value no ellipsis", "abc", "abc"},
		{"exact threshold no ellipsis", "abcd", "abcd"},
		{"longer than threshold gets ellipsis", "pat-abcdef123456", "pat-..."},
		{"github-style pat", "ghp_abcdef1234567890", "ghp_..."},
		{"slack bot token", "xoxb-1234-5678-abcdef", "xoxb..."},
		{"control chars replaced", "\x1b[31mred", "?[31..."},
		{"tab and newline replaced", "a\tb\ncde", "a?b?..."},
		{"unicode printable kept", "résumé-token-value", "résu..."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Fingerprint(tc.input)
			if got != tc.want {
				t.Errorf("Fingerprint(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

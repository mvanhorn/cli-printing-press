package artifacts

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactCLIDirRoot(t *testing.T) {
	cases := []struct {
		name     string
		cliDir   string
		want     string
		wantBase string
	}{
		{
			name:     "absolute home path collapses to placeholder + slug",
			cliDir:   "/Users/operator/printing-press/library/amazon-orders",
			want:     filepath.Join(CLIDirPlaceholder, "amazon-orders"),
			wantBase: "amazon-orders",
		},
		{
			name:     "linux home path collapses to placeholder + slug",
			cliDir:   "/home/operator/printing-press/library/recipe-goat",
			want:     filepath.Join(CLIDirPlaceholder, "recipe-goat"),
			wantBase: "recipe-goat",
		},
		{
			name:   "empty stays empty",
			cliDir: "",
			want:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactCLIDirRoot(tc.cliDir)
			if got != tc.want {
				t.Fatalf("RedactCLIDirRoot(%q) = %q, want %q", tc.cliDir, got, tc.want)
			}
			if tc.wantBase != "" && filepath.Base(got) != tc.wantBase {
				t.Fatalf("filepath.Base(%q) = %q, want %q", got, filepath.Base(got), tc.wantBase)
			}
			if got != "" && strings.HasPrefix(got, "/") {
				t.Fatalf("redacted path %q must not start with /", got)
			}
		})
	}
}

func TestRedactPathUnderCLI(t *testing.T) {
	cli := "/Users/operator/printing-press/library/amazon-orders"
	cases := []struct {
		name string
		p    string
		want string
	}{
		{
			name: "spec inside CLI dir rebases under <cli-dir>",
			p:    filepath.Join(cli, "spec.yaml"),
			want: filepath.Join(CLIDirPlaceholder, "spec.yaml"),
		},
		{
			name: "nested spec inside CLI dir rebases under <cli-dir>",
			p:    filepath.Join(cli, ".manuscripts", "run1", "spec.yaml"),
			want: filepath.Join(CLIDirPlaceholder, ".manuscripts", "run1", "spec.yaml"),
		},
		{
			name: "spec outside CLI dir falls back to <runstate>/basename",
			p:    "/Users/operator/printing-press/.runstate/run1/spec.yaml",
			want: filepath.Join(RunStatePlaceholder, "spec.yaml"),
		},
		{
			name: "empty input stays empty",
			p:    "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactPathUnderCLI(cli, tc.p)
			if got != tc.want {
				t.Fatalf("RedactPathUnderCLI(_, %q) = %q, want %q", tc.p, got, tc.want)
			}
			if strings.Contains(got, "/Users/") || strings.Contains(got, "/home/") {
				t.Fatalf("redacted path %q still contains $HOME-style prefix", got)
			}
		})
	}
}

func TestRedactPathUnderCLI_EmptyCLIDir(t *testing.T) {
	got := RedactPathUnderCLI("", "/Users/operator/specs/amazon.yaml")
	want := filepath.Join(RunStatePlaceholder, "amazon.yaml")
	if got != want {
		t.Fatalf("RedactPathUnderCLI(empty, _) = %q, want %q", got, want)
	}
}

package naming

import "testing"

func TestTrimCLISuffix(t *testing.T) {
	tests := map[string]string{
		"notion-pp-cli":   "notion",
		"notion-pp-cli-2": "notion",
		"legacy-cli":      "legacy",
		"legacy-cli-4":    "legacy",
		"plain":           "plain",
	}

	for input, want := range tests {
		if got := TrimCLISuffix(input); got != want {
			t.Fatalf("TrimCLISuffix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLibraryDirName(t *testing.T) {
	tests := map[string]string{
		"notion-pp-cli":   "notion",
		"notion-pp-cli-2": "notion-2",
		"notion-2-pp-cli": "notion-2",
		"legacy-cli":      "legacy",
		"legacy-cli-4":    "legacy-4",
		"plain":           "plain",
	}

	for input, want := range tests {
		if got := LibraryDirName(input); got != want {
			t.Fatalf("LibraryDirName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMCP(t *testing.T) {
	tests := map[string]string{
		"stripe":  "stripe-pp-mcp",
		"cal-com": "cal-com-pp-mcp",
		"notion":  "notion-pp-mcp",
	}
	for input, want := range tests {
		if got := MCP(input); got != want {
			t.Fatalf("MCP(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsCLIDirName(t *testing.T) {
	if !IsCLIDirName("stripe-pp-cli-3") {
		t.Fatal("expected suffixed pp-cli directory to be recognized")
	}
	if IsCLIDirName("stripe-pp-mcp") {
		t.Fatal("mcp directories must not be treated as cli directories")
	}
}

func TestIsValidLibraryDirName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Slug-keyed names
		{"dub", true},
		{"cal-com", true},
		{"dub-2", true},
		{"steam-web", true},
		{"a", true},
		{"a1", true},
		{"1password", true},

		// Legacy CLI directory names
		{"dub-pp-cli", true},
		{"dub-pp-cli-2", true},
		{"notion-pp-cli", true},
		{"legacy-cli", true},

		// Invalid names
		{"", false},
		{"../etc", false},
		{".DS_Store", false},
		{".hidden", false},
		{"foo/bar", false},
		{"-leading-hyphen", false},
		{"UPPERCASE", false},
		{"has space", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidLibraryDirName(tt.name)
			if got != tt.want {
				t.Errorf("IsValidLibraryDirName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestTrimCLISuffixBareSlug(t *testing.T) {
	// Lock in that TrimCLISuffix returns bare slugs unchanged.
	if got := TrimCLISuffix("dub"); got != "dub" {
		t.Fatalf("TrimCLISuffix(%q) = %q, want %q", "dub", got, "dub")
	}
}

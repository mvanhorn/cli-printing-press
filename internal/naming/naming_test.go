package naming

import (
	"strings"
	"testing"
)

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

func TestEnvPrefix(t *testing.T) {
	tests := map[string]string{
		"pokeapi":       "POKEAPI",
		"pokéapi":       "POKEAPI",
		"PokéAPI":       "POKEAPI",
		"cal-com":       "CAL_COM",
		"Cal.com":       "CAL_COM",
		"food & dining": "FOOD_DINING",
		"1password":     "API_1PASSWORD",
		"!!!":           "API",
		"Großhandel":    "GROSSHANDEL",
		"Łódź":          "LODZ",
		"Ørsted":        "ORSTED",
		"東京":            "DONG_JING",
		"русский":       "RUSSKII",
	}

	for input, want := range tests {
		if got := EnvPrefix(input); got != want {
			t.Fatalf("EnvPrefix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestASCIIFold(t *testing.T) {
	tests := map[string]string{
		"":              "",
		"already-ascii": "already-ascii",
		// Precomposed accents:
		"Pokémon": "Pokemon",
		"naïve":   "naive",
		"café":    "cafe",
		// Fused-diacritic Latin:
		"Großhandel":   "Grosshandel",
		"Łódź":         "Lodz",
		"Encyclopædia": "Encyclopaedia",
		"Ørsted":       "Orsted",
		"Þingvellir":   "Thingvellir",
		// Non-Latin scripts:
		"東京":      "Dong Jing ",
		"русский": "russkii",
		"Δelta":   "Delta",
	}

	for input, want := range tests {
		if got := ASCIIFold(input); got != want {
			t.Fatalf("ASCIIFold(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSnakeIdentifier(t *testing.T) {
	tests := map[string]string{
		"funding --who":     "funding_who",
		"FUNDING-TREND":     "funding_trend",
		"already_snake":     "already_snake",
		"Pokémon list":      "pokemon_list",
		"Großhandel--query": "grosshandel_query",
		"русский_kpi":       "russkii_kpi",
	}

	for input, want := range tests {
		if got := SnakeIdentifier(input); got != want {
			t.Fatalf("SnakeIdentifier(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSnake(t *testing.T) {
	tests := map[string]string{
		"Pets":          "pets",
		"GetInventory":  "get_inventory",
		"List":          "list",
		"PublicList":    "public_list",
		"APIKeys":       "a_p_i_keys",
		"simple":        "simple",
		"already_snake": "already_snake",
		"with-hyphen":   "with-hyphen",
	}

	for input, want := range tests {
		if got := Snake(input); got != want {
			t.Fatalf("Snake(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEnvVarPlaceholder(t *testing.T) {
	tests := map[string]string{
		"DUB_TOKEN":         "token",
		"STYTCH_PROJECT_ID": "project_id",
		"STEAM_WEB_API_KEY": "web_api_key",
		"TOKEN":             "token",
		"GITHUB_TOKEN":      "token",
	}

	for input, want := range tests {
		if got := EnvVarPlaceholder(input); got != want {
			t.Fatalf("EnvVarPlaceholder(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestOneLine(t *testing.T) {
	tests := map[string]string{
		"line one\nline two": "line one line two",
		"too  many   spaces": "too many spaces",
		`say "hello"`:        "say 'hello'",
		"  spaces  ":         "spaces",
		"# Introduction\nAeroAPI delivers flight data.": "AeroAPI delivers flight data.",
		"## Overview": "Overview",
	}

	for input, want := range tests {
		if got := OneLine(input); got != want {
			t.Fatalf("OneLine(%q) = %q, want %q", input, got, want)
		}
	}

	if got := OneLine(string(make([]byte, 200))); len(got) > 120 {
		t.Fatalf("OneLine(long string) length = %d, want <= 120", len(got))
	}
}

func TestCompactDescriptionPreservesHumanText(t *testing.T) {
	input := "# Introduction\nAn \"agent-native\" CLI with C:\\tmp paths."
	want := `An "agent-native" CLI with C:\tmp paths.`
	if got := CompactDescription(input); got != want {
		t.Fatalf("CompactDescription(%q) = %q, want %q", input, got, want)
	}
}

func TestMCPDescription(t *testing.T) {
	tests := []struct {
		name        string
		desc        string
		noAuth      bool
		authType    string
		publicCount int
		totalCount  int
		wantSuffix  string
	}{
		{"public minority gets annotated", "List items", true, "api_key", 2, 10, "(public)"},
		{"auth minority gets api key annotation", "Create item", false, "api_key", 8, 10, "(requires API key)"},
		{"auth minority gets cookie annotation", "Create item", false, "cookie", 8, 10, "(requires browser login)"},
		{"all auth has no annotation", "Create item", false, "api_key", 0, 10, ""},
		{"all public has no annotation", "List items", true, "api_key", 10, 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MCPDescription(tt.desc, tt.noAuth, tt.authType, tt.publicCount, tt.totalCount)
			if tt.wantSuffix != "" && !strings.Contains(got, tt.wantSuffix) {
				t.Fatalf("MCPDescription() = %q, want suffix %q", got, tt.wantSuffix)
			}
			if tt.wantSuffix == "" && (strings.Contains(got, "(public)") || strings.Contains(got, "(requires")) {
				t.Fatalf("MCPDescription() = %q, want no auth annotation", got)
			}
		})
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

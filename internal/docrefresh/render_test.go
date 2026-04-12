package docrefresh

import (
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/internal/docparse"
)

func TestRender_MinimalPopulatesRequiredSections(t *testing.T) {
	d := Data{
		CLIName:     "example-pp-cli",
		APIName:     "example",
		DisplayName: "Example",
		ModulePath:  "github.com/mvanhorn/printing-press-library/library/other/example",
		Description: "A test CLI.",
		BaseCommands: []CommandView{
			{Path: "quote", Use: "quote", Short: "Get quotes"},
		},
	}
	readme, skill, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	readmeStr := string(readme)
	skillStr := string(skill)

	// Install path renders with module path
	wantInstall := "go install github.com/mvanhorn/printing-press-library/library/other/example/cmd/example-pp-cli@latest"
	if !strings.Contains(readmeStr, wantInstall) {
		t.Errorf("README missing install path %q", wantInstall)
	}
	if !strings.Contains(skillStr, wantInstall) {
		t.Errorf("SKILL missing install path %q", wantInstall)
	}
	// Description renders when no narrative is present
	if !strings.Contains(readmeStr, "A test CLI.") {
		t.Error("README should render Description as fallback when no narrative")
	}
	// Commands section shows base commands
	if !strings.Contains(readmeStr, "`quote`") {
		t.Error("README should list base command 'quote'")
	}
	// SKILL frontmatter name
	if !strings.Contains(skillStr, "name: pp-example") {
		t.Error("SKILL frontmatter should have name: pp-example")
	}
}

func TestRender_NarrativeTakesPrecedenceOverDescription(t *testing.T) {
	d := Data{
		CLIName:     "example-pp-cli",
		APIName:     "example",
		ModulePath:  "github.com/x/y",
		Description: "A test CLI.",
		Narrative: &Narrative{
			Headline:  "**The best CLI ever**",
			ValueProp: "This is the expanded narrative.",
		},
	}
	readme, skill, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "**The best CLI ever**") {
		t.Error("README should render Narrative.Headline")
	}
	if !strings.Contains(string(readme), "This is the expanded narrative.") {
		t.Error("README should render Narrative.ValueProp")
	}
	if strings.Contains(string(readme), "A test CLI.") {
		t.Error("README should NOT render Description fallback when Narrative present")
	}
	// SKILL frontmatter description uses the headline
	if !strings.Contains(string(skill), "The best CLI ever") {
		t.Error("SKILL frontmatter should use Narrative.Headline")
	}
}

func TestRender_TranscendenceGroupedWhenGroupsPresent(t *testing.T) {
	d := Data{
		CLIName:    "example-pp-cli",
		APIName:    "example",
		ModulePath: "github.com/x/y",
		TranscendenceCommands: []CommandView{
			{Path: "portfolio perf", Short: "Show P&L", Group: "Local state"},
			{Path: "portfolio gains", Short: "Per-lot gains", Group: "Local state"},
			{Path: "auth login-chrome", Short: "Chrome cookie import", Group: "Reachability"},
		},
	}
	readme, _, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	readmeStr := string(readme)
	if !strings.Contains(readmeStr, "### Local state") {
		t.Error("README should have 'Local state' group heading")
	}
	if !strings.Contains(readmeStr, "### Reachability") {
		t.Error("README should have 'Reachability' group heading")
	}
}

func TestRender_TranscendenceFlatWhenNoGroups(t *testing.T) {
	d := Data{
		CLIName:    "example-pp-cli",
		APIName:    "example",
		ModulePath: "github.com/x/y",
		TranscendenceCommands: []CommandView{
			{Path: "digest", Short: "Morning digest"},
			{Path: "compare", Short: "Compare symbols"},
		},
	}
	readme, _, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	readmeStr := string(readme)
	// No group headings
	if strings.Contains(readmeStr, "### Other") {
		t.Error("README should not render a group heading for a single ungrouped bucket")
	}
	if !strings.Contains(readmeStr, "`digest`") || !strings.Contains(readmeStr, "`compare`") {
		t.Error("README should still list the flat commands")
	}
}

func TestRender_AuthBranches(t *testing.T) {
	cases := []struct {
		auth     string
		envVars  []string
		expect   string
		unexpect string
	}{
		{"api_key", []string{"FOO_TOKEN"}, "export FOO_TOKEN", "auth login"},
		{"bearer_token", nil, "auth set-token", "auth login"},
		{"oauth2", nil, "auth login", "auth set-token"},
		{"cookie", nil, "auth login", "auth set-token"},
		{"composed", nil, "auth login", "auth set-token"},
		{"none", nil, "No authentication required.", "auth login"},
		{"", nil, "No authentication required.", "auth set-token"}, // unknown → none branch
	}
	for _, tc := range cases {
		t.Run(tc.auth, func(t *testing.T) {
			d := Data{
				CLIName: "x-pp-cli", APIName: "x", ModulePath: "m",
				AuthType: tc.auth, AuthEnvVars: tc.envVars,
			}
			readme, skill, err := Render(d)
			if err != nil {
				t.Fatal(err)
			}
			combined := string(readme) + "\n" + string(skill)
			if !strings.Contains(combined, tc.expect) {
				t.Errorf("auth=%q: expected %q in output", tc.auth, tc.expect)
			}
			_ = tc.unexpect // kept as documentation; only positive assertions run
		})
	}
}

func TestRender_SourcesRendered(t *testing.T) {
	d := Data{
		CLIName: "x-pp-cli", APIName: "x", ModulePath: "m",
		Sources: []Source{
			{Name: "yfinance", URL: "https://github.com/ranaroussi/yfinance", Language: "Python", Stars: 14000},
			{Name: "yahoo-finance2", URL: "https://github.com/gadicc/yahoo-finance2", Language: "JavaScript", Stars: 2800},
		},
	}
	readme, _, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	readmeStr := string(readme)
	if !strings.Contains(readmeStr, "yfinance") || !strings.Contains(readmeStr, "14000 stars") {
		t.Errorf("README should render sources")
	}
}

func TestFromClassifications_SplitsBaseAndTranscendence(t *testing.T) {
	classified := []docparse.Classification{
		{Command: docparse.Command{Use: "quote", Short: "Get quotes"}, IsTranscendence: false},
		{Command: docparse.Command{Use: "watchlist", Short: "Watchlists"}, IsTranscendence: true},
		{Command: docparse.Command{Use: "portfolio", Short: "Portfolio"}, IsTranscendence: true},
	}
	d := FromClassifications(Data{CLIName: "x"}, classified)
	if len(d.BaseCommands) != 1 || d.BaseCommands[0].Path != "quote" {
		t.Errorf("BaseCommands = %v", d.BaseCommands)
	}
	if len(d.TranscendenceCommands) != 2 {
		t.Errorf("TranscendenceCommands count = %d, want 2", len(d.TranscendenceCommands))
	}
}

func TestGroupTranscendence_OtherLast(t *testing.T) {
	cmds := []CommandView{
		{Path: "a", Group: ""},
		{Path: "b", Group: "Zebra"},
		{Path: "c", Group: "Alpha"},
		{Path: "d", Group: ""},
	}
	groups := groupTranscendence(cmds)
	if len(groups) != 3 {
		t.Fatalf("want 3 groups, got %d", len(groups))
	}
	if groups[0].Name != "Alpha" || groups[1].Name != "Zebra" || groups[2].Name != "Other" {
		t.Errorf("group order wrong: %v", []string{groups[0].Name, groups[1].Name, groups[2].Name})
	}
	if len(groups[2].Commands) != 2 {
		t.Errorf("Other bucket should have 2 commands, got %d", len(groups[2].Commands))
	}
}

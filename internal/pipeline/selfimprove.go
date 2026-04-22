package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// templateMapping maps Steinberger dimensions to the template files responsible.
var templateMapping = map[string][]string{
	"output_modes":          {"root.go.tmpl", "helpers.go.tmpl"},
	"auth":                  {"config.go.tmpl", "auth.go.tmpl"},
	"error_handling":        {"helpers.go.tmpl"},
	"terminal_ux":           {"helpers.go.tmpl"},
	"readme":                {"readme.md.tmpl"},
	"doctor":                {"doctor.go.tmpl"},
	"agent_native":          {"root.go.tmpl", "helpers.go.tmpl"},
	"local_cache":           {},
	"live_api_verification": {},
}

// dimensionAdvice maps each dimension to concrete improvement guidance.
var dimensionAdvice = map[string]string{
	"output_modes": `Add support for all five output modes in the root command:
- --format=json (structured machine output)
- --format=plain (human-readable default)
- --format=table (columnar alignment)
- --format=csv (spreadsheet-friendly)
- --select=FIELD (jq-style field extraction)

Templates to modify: root.go.tmpl (add format flag), helpers.go.tmpl (add formatting functions).`,

	"auth": `Ensure the config template reads at least two env vars (API key + optional base URL).
Add an auth.go.tmpl that validates credentials on startup and provides clear error
messages when credentials are missing.

Templates to modify: config.go.tmpl (add env var reads), auth.go.tmpl (add validation).`,

	"error_handling": `Add actionable hint messages for common failure modes:
- "hint: set ENV_VAR to authenticate" when 401 received
- "hint: check your network connection" on connection errors
- "hint: run 'doctor' to diagnose" as a catch-all
Use distinct exit codes: 1=general, 2=auth, 3=network, 4=not-found, 5=rate-limited.

Template to modify: helpers.go.tmpl (add hintedError function and exit code constants).`,

	"terminal_ux": `Add color support with NO_COLOR and isatty detection:
- Check NO_COLOR env var to disable colors
- Use isatty to detect pipe vs terminal
- Wrap colorEnabled flag around all ANSI output

Template to modify: helpers.go.tmpl (add color detection and formatting functions).`,

	"readme": `Ensure README template includes all five scored sections:
- Quick Start (install + first command)
- Output Formats (examples of each format)
- Agent Usage (how to use in scripts/agents)
- Troubleshooting (common errors and fixes)
- Doctor (what the doctor command checks)

Template to modify: readme.md.tmpl (add missing sections).`,

	"doctor": `Add health check HTTP calls for each API dependency:
- Base API URL reachability
- Auth endpoint validation
- Rate limit status check
- Each check should use http.Get and report pass/fail

Template to modify: doctor.go.tmpl (add http.Get health check functions).`,

	"agent_native": `Ensure all agent-friendly features are present:
- --format=json for structured output
- --select=FIELD for field extraction
- --dry-run to preview API calls without executing
- --non-interactive to suppress prompts

Templates to modify: root.go.tmpl (add flags), helpers.go.tmpl (add dry-run logic).`,

	"local_cache": `This requires a new template. Create cache.go.tmpl that:
- Uses a local SQLite or bolt database in ~/.cache/<cli-name>/
- Caches GET responses with configurable TTL
- Provides --no-cache flag to bypass
- Provides --clear-cache flag to purge

Note: No existing template covers this - a new cache.go.tmpl is needed.`,

	"live_api_verification": `Re-run verify against the real API instead of the mock server.
A low score here means the CLI has never been exercised end-to-end against
its real backend, so nothing catches wire-level breakage - wrong base URL,
auth-header mismatch, pagination shape, content-type quirks.

Steps to improve:
1. Set the CLI's auth env var (e.g., GITHUB_TOKEN, HUBSPOT_API_KEY) to a
   real read-only credential.
2. Re-run verify with the credential set so VerifyConfig.APIKey is non-empty.
3. Investigate any command that fails live but passes mock - the mock is
   lying about one of: response shape, pagination cursor, error envelope.
4. Fix the generator or the printed CLI depending on which side is wrong,
   then re-run verify until PassRate is at or above 95%.

Template surface: no template change usually needed; this dimension
measures runtime behavior against a real endpoint, not generated-code
structure. If the fix requires a template change, use the template that
owns the failing surface (client.go.tmpl for HTTP details, root.go.tmpl
for flag wiring, etc.).`,
}

// GenerateFixPlans creates a fix plan markdown file for each Steinberger
// dimension scoring below 5/10.
func GenerateFixPlans(scorecard *Scorecard, pipelineDir string) ([]string, error) {
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating pipeline dir: %w", err)
	}

	dimensions := []struct {
		name  string
		score int
	}{
		{"output_modes", scorecard.Steinberger.OutputModes},
		{"auth", scorecard.Steinberger.Auth},
		{"error_handling", scorecard.Steinberger.ErrorHandling},
		{"terminal_ux", scorecard.Steinberger.TerminalUX},
		{"readme", scorecard.Steinberger.README},
		{"doctor", scorecard.Steinberger.Doctor},
		{"agent_native", scorecard.Steinberger.AgentNative},
		{"local_cache", scorecard.Steinberger.LocalCache},
		{"live_api_verification", scorecard.Steinberger.LiveAPIVerification},
	}

	// live_api_verification is Tier 2 and opt-in: when verify didn't run
	// live, the dimension is unscored (score 0 but also flagged in
	// UnscoredDimensions). We don't write a fix plan for an unscored dim
	// because the operator already knows verify didn't run - that's the
	// prerequisite, not a scoring problem.
	liveUnscored := scorecard.IsDimensionUnscored("live_api_verification")

	var plans []string
	for _, d := range dimensions {
		if d.score >= 5 {
			continue
		}
		if d.name == "live_api_verification" && liveUnscored {
			continue
		}

		planPath := filepath.Join(pipelineDir, fmt.Sprintf("fix-%s-plan.md", d.name))
		content := buildFixPlan(scorecard.APIName, d.name, d.score)
		if err := os.WriteFile(planPath, []byte(content), 0o644); err != nil {
			return plans, fmt.Errorf("writing fix plan for %s: %w", d.name, err)
		}
		plans = append(plans, planPath)
	}

	return plans, nil
}

func buildFixPlan(apiName, dimension string, currentScore int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Fix Plan: %s\n\n", dimension)
	fmt.Fprintf(&b, "**API:** %s\n", apiName)
	fmt.Fprintf(&b, "**Current Score:** %d/10\n", currentScore)
	b.WriteString("**Target Score:** 8/10\n\n")

	// Templates involved
	templates := templateMapping[dimension]
	if len(templates) > 0 {
		b.WriteString("## Templates to Modify\n\n")
		for _, t := range templates {
			fmt.Fprintf(&b, "- `templates/%s`\n", t)
		}
	} else {
		b.WriteString("## Templates to Create\n\n")
		b.WriteString("- `templates/cache.go.tmpl` (new)\n")
	}
	b.WriteString("\n")

	// Improvement advice
	b.WriteString("## What to Change\n\n")
	if advice, ok := dimensionAdvice[dimension]; ok {
		b.WriteString(advice)
	} else {
		b.WriteString("No specific guidance available for this dimension.\n")
	}
	b.WriteString("\n\n")

	// Verification
	b.WriteString("## Verification\n\n")
	b.WriteString("After applying changes, re-run the scorecard:\n\n")
	b.WriteString("```bash\n")
	fmt.Fprintf(&b, "printing-press scorecard --api %s\n", apiName)
	b.WriteString("```\n\n")
	fmt.Fprintf(&b, "The %s dimension should score at least 8/10.\n", dimension)

	return b.String()
}

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

const readinessSchemaVersion = 1

type readinessReportOptions struct {
	SpecFiles                   []string
	OutputDir                   string
	TrafficAnalysis             *browsersniff.TrafficAnalysis
	PlaceholderBaseURLSpecFiles []string
	AsJSON                      bool
}

type readinessReport struct {
	SchemaVersion int                 `json:"schema_version"`
	Verdict       string              `json:"verdict"`
	Inputs        readinessInputs     `json:"inputs"`
	Summary       readinessSummary    `json:"summary"`
	Auth          readinessAuth       `json:"auth"`
	DataLayer     readinessDataLayer  `json:"data_layer"`
	MCP           readinessMCP        `json:"mcp"`
	Findings      []readinessFinding  `json:"findings"`
	NextSteps     []readinessNextStep `json:"next_steps"`
}

type readinessInputs struct {
	SpecFiles  []string `json:"spec_files"`
	OutputDir  string   `json:"output_dir"`
	SpecSource string   `json:"spec_source,omitempty"`
}

type readinessSummary struct {
	APIName            string `json:"api_name"`
	DisplayName        string `json:"display_name,omitempty"`
	BaseURL            string `json:"base_url,omitempty"`
	ResourceCount      int    `json:"resource_count"`
	EndpointCount      int    `json:"endpoint_count"`
	ReadEndpointCount  int    `json:"read_endpoint_count"`
	WriteEndpointCount int    `json:"write_endpoint_count"`
}

type readinessAuth struct {
	Type                   string   `json:"type"`
	Required               bool     `json:"required"`
	Optional               bool     `json:"optional"`
	EnvVars                []string `json:"env_vars,omitempty"`
	RequestEnvVars         []string `json:"request_env_vars,omitempty"`
	KeyURL                 string   `json:"key_url,omitempty"`
	VerifyPath             string   `json:"verify_path,omitempty"`
	VerifyQuery            bool     `json:"verify_query,omitempty"`
	RequiresBrowserSession bool     `json:"requires_browser_session,omitempty"`
	BrowserSessionReason   string   `json:"browser_session_reason,omitempty"`
}

type readinessDataLayer struct {
	Store                  bool     `json:"store"`
	Sync                   bool     `json:"sync"`
	Search                 bool     `json:"search"`
	Analytics              bool     `json:"analytics"`
	SyncableResources      []string `json:"syncable_resources,omitempty"`
	DependentSyncResources []string `json:"dependent_sync_resources,omitempty"`
	DefaultSyncConcurrency int      `json:"default_sync_concurrency"`
}

type readinessMCP struct {
	EffectiveTransports []string `json:"effective_transports"`
	TypedEndpointCount  int      `json:"typed_endpoint_count"`
	LargeSurface        bool     `json:"large_surface"`
}

type readinessFinding struct {
	Severity       string `json:"severity"`
	Category       string `json:"category"`
	Code           string `json:"code"`
	Message        string `json:"message"`
	Recommendation string `json:"recommendation,omitempty"`
}

type readinessNextStep struct {
	Label   string `json:"label"`
	Command string `json:"command,omitempty"`
}

func printReadinessReport(w io.Writer, apiSpec *spec.APISpec, opts readinessReportOptions) error {
	report := buildReadinessReport(apiSpec, opts)
	if opts.AsJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	_, err := fmt.Fprint(w, renderReadinessMarkdown(report))
	return err
}

func buildReadinessReport(apiSpec *spec.APISpec, opts readinessReportOptions) readinessReport {
	profile := profiler.Profile(apiSpec)
	visionSet := generator.SelectVisionTemplates(profile.ToVisionaryPlan(apiSpec.Name))
	if apiSpec.Streaming.Enabled() {
		visionSet.Store = true
		visionSet.Sync = true
	}

	resourceCount, endpointCount, readCount, writeCount := readinessCounts(apiSpec)
	auth := readinessAuthSummary(apiSpec.Auth)
	syncable := profile.SyncableResourceNames()
	dependent := readinessDependentResourceNames(profile.DependentSyncResources)
	findings := readinessFindings(apiSpec, opts, auth, endpointCount, readCount, writeCount, syncable, dependent)

	report := readinessReport{
		SchemaVersion: readinessSchemaVersion,
		Inputs: readinessInputs{
			SpecFiles:  append([]string(nil), opts.SpecFiles...),
			OutputDir:  opts.OutputDir,
			SpecSource: strings.TrimSpace(apiSpec.SpecSource),
		},
		Summary: readinessSummary{
			APIName:            apiSpec.Name,
			DisplayName:        apiSpec.EffectiveDisplayName(),
			BaseURL:            apiSpec.BaseURL,
			ResourceCount:      resourceCount,
			EndpointCount:      endpointCount,
			ReadEndpointCount:  readCount,
			WriteEndpointCount: writeCount,
		},
		Auth: auth,
		DataLayer: readinessDataLayer{
			Store:                  visionSet.Store,
			Sync:                   visionSet.Sync,
			Search:                 visionSet.Search,
			Analytics:              visionSet.Analytics,
			SyncableResources:      syncable,
			DependentSyncResources: dependent,
			DefaultSyncConcurrency: apiSpec.SyncDefaultConcurrency(),
		},
		MCP: readinessMCP{
			EffectiveTransports: apiSpec.EffectiveMCPTransports(),
			TypedEndpointCount:  apiSpec.TypedEndpointCount(),
			LargeSurface:        apiSpec.TypedEndpointCount() > spec.DefaultRemoteTransportEndpointThreshold,
		},
		Findings:  findings,
		NextSteps: readinessNextSteps(apiSpec, opts, findings),
	}
	report.Verdict = readinessVerdict(findings)
	return report
}

func readinessCounts(apiSpec *spec.APISpec) (resourceCount, endpointCount, readCount, writeCount int) {
	if apiSpec == nil {
		return 0, 0, 0, 0
	}
	for _, name := range sortedResourceNames(apiSpec.Resources) {
		resourceCount++
		r := apiSpec.Resources[name]
		e, read, write := readinessEndpointCounts(r.Endpoints)
		endpointCount += e
		readCount += read
		writeCount += write
		for _, subName := range sortedResourceNames(r.SubResources) {
			resourceCount++
			sub := r.SubResources[subName]
			e, read, write = readinessEndpointCounts(sub.Endpoints)
			endpointCount += e
			readCount += read
			writeCount += write
		}
	}
	return resourceCount, endpointCount, readCount, writeCount
}

func readinessEndpointCounts(endpoints map[string]spec.Endpoint) (endpointCount, readCount, writeCount int) {
	for _, name := range sortedEndpointNames(endpoints) {
		endpointCount++
		if generator.EndpointIsWriteCommand(endpoints[name], name) {
			writeCount++
		} else {
			readCount++
		}
	}
	return endpointCount, readCount, writeCount
}

func readinessAuthSummary(auth spec.AuthConfig) readinessAuth {
	authType := strings.TrimSpace(auth.Type)
	if authType == "" {
		authType = "none"
	}
	normalized := auth
	normalized.NormalizeEnvVarSpecs("")
	var envVars []string
	var requestEnvVars []string
	for _, envVar := range normalized.EnvVarSpecs {
		name := strings.TrimSpace(envVar.Name)
		if name == "" {
			continue
		}
		envVars = append(envVars, name)
		if envVar.IsRequestCredential() {
			requestEnvVars = append(requestEnvVars, name)
		}
	}
	sort.Strings(envVars)
	sort.Strings(requestEnvVars)
	required := authType != "none" && !auth.Optional
	return readinessAuth{
		Type:                   authType,
		Required:               required,
		Optional:               auth.Optional,
		EnvVars:                envVars,
		RequestEnvVars:         requestEnvVars,
		KeyURL:                 strings.TrimSpace(auth.KeyURL),
		VerifyPath:             strings.TrimSpace(auth.VerifyPath),
		VerifyQuery:            strings.TrimSpace(auth.VerifyQuery) != "",
		RequiresBrowserSession: auth.RequiresBrowserSession,
		BrowserSessionReason:   strings.TrimSpace(auth.BrowserSessionReason),
	}
}

func readinessFindings(apiSpec *spec.APISpec, opts readinessReportOptions, auth readinessAuth, endpointCount, readCount, writeCount int, syncable, dependent []string) []readinessFinding {
	var findings []readinessFinding
	add := func(severity, category, code, message, recommendation string) {
		findings = append(findings, readinessFinding{
			Severity:       severity,
			Category:       category,
			Code:           code,
			Message:        message,
			Recommendation: recommendation,
		})
	}

	if endpointCount == 0 {
		add("blocker", "surface", "zero_endpoints", "No typed endpoints were found in the parsed spec.", "Use a richer spec or add endpoint definitions before generating.")
	}
	if apiSpec.BaseURLIsPlaceholder || apiSpec.BaseURL == spec.PlaceholderBaseURL || len(opts.PlaceholderBaseURLSpecFiles) > 0 {
		message := "The spec does not declare a usable API base URL."
		if len(opts.PlaceholderBaseURLSpecFiles) > 0 {
			message = fmt.Sprintf("The spec does not declare a usable API base URL: %s.", strings.Join(opts.PlaceholderBaseURLSpecFiles, ", "))
		}
		add("blocker", "spec", "missing_base_url", message, "Add a real servers block/base_url or supply a base URL through discovery before generating.")
	}
	if trafficAnalysisRequiresUnshippablePageContext(opts.TrafficAnalysis) {
		add("blocker", "reachability", "browser_page_context_required", "Traffic analysis says this target requires live browser page-context execution.", "Re-run discovery for a direct, browser-clearance, or browser-http replayable surface.")
	}
	if auth.Required && len(auth.EnvVars) == 0 && auth.KeyURL == "" {
		add("warning", "auth", "auth_guidance_missing", "Auth is required, but no credential env vars or key URL are declared.", "Add auth env vars and a key URL so generated setup and doctor output are actionable.")
	}
	if auth.Required && auth.VerifyPath == "" && !auth.VerifyQuery {
		add("warning", "auth", "auth_verify_missing", "Auth is required, but no verify path/query is configured.", "Add auth.verify_path or auth.verify_query so doctor can validate credentials precisely.")
	}
	if readCount > 0 && len(syncable) == 0 && len(dependent) == 0 {
		add("warning", "data_layer", "no_syncable_resources", "No syncable resources were detected for the local data layer.", "Confirm list endpoints return arrays or envelopes with stable item IDs.")
	}
	if apiSpec.TypedEndpointCount() > spec.DefaultRemoteTransportEndpointThreshold && len(apiSpec.MCP.Transport) == 0 && !apiSpec.HasMCPTransport("http") {
		add("warning", "mcp", "large_mcp_surface_stdio_only", "The typed endpoint surface is large and defaults to stdio-only MCP transport.", "Add explicit mcp.transport or MCP intents if remote agent access is important.")
	}
	if apiSpec.EffectiveHTTPTransport() != spec.HTTPTransportStandard || apiSpec.SpecSource == "sniffed" {
		add("warning", "transport", "browser_transport_risk", "This spec uses browser-facing or sniffed transport assumptions.", "Review reachability/auth evidence before publishing the generated CLI.")
	}
	if writeCount > 0 {
		add("info", "surface", "mutating_commands_present", fmt.Sprintf("%d mutating endpoint(s) will be generated.", writeCount), "Use --dry-run/--agent and generated confirmation safeguards when testing writes.")
	}
	if auth.Optional {
		add("info", "auth", "auth_optional", "Auth is optional for this spec.", "Generated doctor output should treat missing optional credentials as informational.")
	}
	return findings
}

func readinessVerdict(findings []readinessFinding) string {
	verdict := "ready"
	for _, finding := range findings {
		switch finding.Severity {
		case "blocker":
			return "blocked"
		case "warning":
			verdict = "warn"
		}
	}
	return verdict
}

func readinessNextSteps(apiSpec *spec.APISpec, opts readinessReportOptions, findings []readinessFinding) []readinessNextStep {
	var steps []readinessNextStep
	for _, finding := range findings {
		switch finding.Code {
		case "missing_base_url":
			steps = append(steps, readinessNextStep{Label: "Add a real API base URL to the spec"})
		case "auth_guidance_missing":
			steps = append(steps, readinessNextStep{Label: "Declare credential env vars and an auth key URL"})
		case "auth_verify_missing":
			steps = append(steps, readinessNextStep{Label: "Add an auth verify path or query"})
		case "browser_page_context_required":
			steps = append(steps, readinessNextStep{Label: "Repeat discovery for a replayable HTTP surface"})
		}
	}
	if apiSpec != nil && apiSpec.Name != "" {
		steps = append(steps, readinessNextStep{
			Label:   "Generate after reviewing readiness findings",
			Command: readinessGenerateCommand(apiSpec, opts),
		})
	}
	return dedupeReadinessNextSteps(steps)
}

func readinessGenerateCommand(apiSpec *spec.APISpec, opts readinessReportOptions) string {
	parts := []string{"cli-printing-press", "generate"}
	if len(opts.SpecFiles) == 0 {
		parts = append(parts, "--spec", "<spec>")
	} else {
		for _, specFile := range opts.SpecFiles {
			parts = append(parts, "--spec", readinessShellArg(specFile))
		}
	}
	if apiSpec != nil && apiSpec.Name != "" {
		parts = append(parts, "--name", readinessShellArg(apiSpec.Name))
	}
	return strings.Join(parts, " ")
}

func readinessShellArg(value string) string {
	if value == "" {
		return "''"
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '-', '_', '.', '/', ':', '@', '+', '=':
			continue
		}
		return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
	}
	return value
}

func renderReadinessMarkdown(report readinessReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Print Readiness Report\n\n")
	fmt.Fprintf(&b, "Verdict: %s\n\n", strings.ToUpper(report.Verdict))

	fmt.Fprintf(&b, "## Generated Surface\n")
	fmt.Fprintf(&b, "- API: %s\n", report.Summary.APIName)
	if report.Summary.DisplayName != "" && report.Summary.DisplayName != report.Summary.APIName {
		fmt.Fprintf(&b, "- Display name: %s\n", report.Summary.DisplayName)
	}
	if report.Summary.BaseURL != "" {
		fmt.Fprintf(&b, "- Base URL: %s\n", report.Summary.BaseURL)
	}
	fmt.Fprintf(&b, "- Output dir: %s\n", report.Inputs.OutputDir)
	fmt.Fprintf(&b, "- Resources: %d\n", report.Summary.ResourceCount)
	fmt.Fprintf(&b, "- Endpoints: %d (%d read, %d write)\n\n", report.Summary.EndpointCount, report.Summary.ReadEndpointCount, report.Summary.WriteEndpointCount)

	fmt.Fprintf(&b, "## Auth Readiness\n")
	fmt.Fprintf(&b, "- Type: %s\n", report.Auth.Type)
	fmt.Fprintf(&b, "- Required: %t\n", report.Auth.Required)
	if len(report.Auth.EnvVars) > 0 {
		fmt.Fprintf(&b, "- Env vars: %s\n", strings.Join(report.Auth.EnvVars, ", "))
	}
	if report.Auth.KeyURL != "" {
		fmt.Fprintf(&b, "- Key URL: %s\n", report.Auth.KeyURL)
	}
	if report.Auth.VerifyPath != "" {
		fmt.Fprintf(&b, "- Verify path: %s\n", report.Auth.VerifyPath)
	} else if report.Auth.VerifyQuery {
		fmt.Fprintf(&b, "- Verify query: configured\n")
	}
	if report.Auth.RequiresBrowserSession {
		fmt.Fprintf(&b, "- Browser session required: %t\n", report.Auth.RequiresBrowserSession)
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "## Local Data Layer\n")
	fmt.Fprintf(&b, "- Store: %t\n", report.DataLayer.Store)
	fmt.Fprintf(&b, "- Sync: %t\n", report.DataLayer.Sync)
	fmt.Fprintf(&b, "- Search: %t\n", report.DataLayer.Search)
	fmt.Fprintf(&b, "- Analytics: %t\n", report.DataLayer.Analytics)
	if len(report.DataLayer.SyncableResources) > 0 {
		fmt.Fprintf(&b, "- Syncable resources: %s\n", strings.Join(report.DataLayer.SyncableResources, ", "))
	}
	if len(report.DataLayer.DependentSyncResources) > 0 {
		fmt.Fprintf(&b, "- Dependent sync resources: %s\n", strings.Join(report.DataLayer.DependentSyncResources, ", "))
	}
	fmt.Fprintf(&b, "- Default sync concurrency: %d\n\n", report.DataLayer.DefaultSyncConcurrency)

	fmt.Fprintf(&b, "## MCP Surface\n")
	fmt.Fprintf(&b, "- Transports: %s\n", strings.Join(report.MCP.EffectiveTransports, ", "))
	fmt.Fprintf(&b, "- Typed endpoint count: %d\n", report.MCP.TypedEndpointCount)
	fmt.Fprintf(&b, "- Large surface: %t\n\n", report.MCP.LargeSurface)

	fmt.Fprintf(&b, "## Findings\n")
	if len(report.Findings) == 0 {
		fmt.Fprintf(&b, "- none\n")
	} else {
		for _, finding := range report.Findings {
			fmt.Fprintf(&b, "- %s [%s/%s]: %s", strings.ToUpper(finding.Severity), finding.Category, finding.Code, finding.Message)
			if finding.Recommendation != "" {
				fmt.Fprintf(&b, " Recommendation: %s", finding.Recommendation)
			}
			fmt.Fprintf(&b, "\n")
		}
	}
	fmt.Fprintf(&b, "\n")

	if len(report.NextSteps) > 0 {
		fmt.Fprintf(&b, "## Recommended Next Steps\n")
		for _, step := range report.NextSteps {
			if step.Command != "" {
				fmt.Fprintf(&b, "- %s: `%s`\n", step.Label, step.Command)
			} else {
				fmt.Fprintf(&b, "- %s\n", step.Label)
			}
		}
		fmt.Fprintf(&b, "\n")
	}
	return b.String()
}

func readinessDependentResourceNames(resources []profiler.DependentResource) []string {
	names := make([]string, 0, len(resources))
	for _, resource := range resources {
		if name := strings.TrimSpace(resource.Name); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func sortedResourceNames(resources map[string]spec.Resource) []string {
	names := make([]string, 0, len(resources))
	for name := range resources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedEndpointNames(endpoints map[string]spec.Endpoint) []string {
	names := make([]string, 0, len(endpoints))
	for name := range endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func dedupeReadinessNextSteps(steps []readinessNextStep) []readinessNextStep {
	seen := map[string]struct{}{}
	out := make([]readinessNextStep, 0, len(steps))
	for _, step := range steps {
		key := step.Label + "\x00" + step.Command
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, step)
	}
	return out
}

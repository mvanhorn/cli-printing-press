package llmpolish

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/llm"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// VisionCustomization holds LLM-generated customizations for vision templates.
type VisionCustomization struct {
	// ResourcePriority is the suggested sync order (most important first)
	ResourcePriority []string `json:"resource_priority"`
	// FTSFields maps resource name to fields that should be indexed in FTS5
	FTSFields map[string][]string `json:"fts_fields"`
	// WorkflowNames maps generic workflow names to domain-specific names
	WorkflowNames map[string]string `json:"workflow_names"`
	// ExampleOverrides maps command names to better domain-specific examples
	ExampleOverrides map[string]string `json:"example_overrides"`
	// DescOverrides maps command names to better domain-specific descriptions
	DescOverrides map[string]string `json:"desc_overrides"`
	// SyncHints maps resource name to sync strategy hints
	SyncHints map[string]SyncHint `json:"sync_hints"`
}

// SyncHint describes how to sync a specific resource.
type SyncHint struct {
	Direction string `json:"direction"` // "newest_first" or "oldest_first"
	BatchSize int    `json:"batch_size"`
	Priority  int    `json:"priority"` // 1 = highest
}

func SynthesizeVision(profile *profiler.APIProfile, apiSpec *spec.APISpec) (*VisionCustomization, error) {
	if !llm.Available() {
		return nil, nil
	}

	prompt := buildVisionPrompt(profile, apiSpec)
	response, err := llm.Run(prompt)
	if err != nil {
		return nil, nil
	}

	var customization VisionCustomization
	if err := json.Unmarshal([]byte(strings.TrimSpace(response)), &customization); err != nil {
		return nil, nil
	}

	return &customization, nil
}

func buildVisionPrompt(profile *profiler.APIProfile, apiSpec *spec.APISpec) string {
	if profile == nil {
		profile = &profiler.APIProfile{}
	}
	if apiSpec == nil {
		apiSpec = &spec.APISpec{}
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "You are analyzing the %s API to customize a CLI's power-user features.\n\n", defaultString(apiSpec.Name, "unknown"))
	builder.WriteString("The API profiler detected these signals:\n")
	fmt.Fprintf(&builder, "- High volume: %t\n", profile.HighVolume)
	fmt.Fprintf(&builder, "- Needs local search: %t\n", profile.NeedsSearch)
	fmt.Fprintf(&builder, "- Has real-time events: %t\n", profile.HasRealtime)
	fmt.Fprintf(&builder, "- Has chronological data: %t\n", profile.HasChronological)
	syncNames := make([]string, len(profile.SyncableResources))
	for i, sr := range profile.SyncableResources {
		syncNames[i] = sr.Name
	}
	fmt.Fprintf(&builder, "- Syncable resources: %s\n", formatStringSlice(syncNames))
	fmt.Fprintf(&builder, "- Searchable fields per resource: %s\n", formatSearchableFields(profile.SearchableFields))
	if apiSpec.Description != "" {
		fmt.Fprintf(&builder, "\nAPI description: %s\n", apiSpec.Description)
	}

	fmt.Fprintf(&builder, "\nProfiler recommended features: %s\n", formatStringSlice(profile.RecommendedFeatures()))
	builder.WriteString("\nResources and their endpoints:\n")
	builder.WriteString(formatResourceSummaries(apiSpec.Resources, ""))
	builder.WriteString(`
Based on this API's domain, answer these questions as a JSON object:

1. resource_priority: What order should resources be synced? Put the most valuable/frequently-accessed resources first.

2. fts_fields: For each syncable resource, which string fields contain actual searchable content (not IDs, not timestamps, not enum values)? Be selective - index content users would search for.

3. workflow_names: Map these generic workflow names to domain-specific names that make sense for this API:
   - "archive" -> what would a power user call this? (e.g., "archive" for Discord, "backup" for a CMS)
   - "audit" -> what would they call this? (e.g., "audit" for Discord, "activity" for GitHub)

4. example_overrides: For the sync, search, export, and tail commands, write realistic example command lines using actual resource names and plausible IDs.

5. desc_overrides: For the sync, search, export, and tail commands, write developer-friendly one-line descriptions specific to this API's domain.

6. sync_hints: For the top 3 syncable resources, specify:
   - direction: "newest_first" or "oldest_first" (which makes more sense for this data?)
   - batch_size: optimal page size (usually 100, but some APIs differ)
   - priority: 1-3 ranking

Return ONLY valid JSON, no markdown fences, no explanation.
`)

	return builder.String()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func formatStringSlice(values []string) string {
	if len(values) == 0 {
		return "[]"
	}

	sorted := append([]string(nil), values...)
	sort.Strings(sorted)

	return "[" + strings.Join(sorted, ", ") + "]"
}

func formatSearchableFields(fields map[string][]string) string {
	if len(fields) == 0 {
		return "{}"
	}

	resources := make([]string, 0, len(fields))
	for resource := range fields {
		resources = append(resources, resource)
	}
	sort.Strings(resources)

	parts := make([]string, 0, len(resources))
	for _, resource := range resources {
		parts = append(parts, fmt.Sprintf("%s: %s", resource, formatStringSlice(fields[resource])))
	}

	return "{" + strings.Join(parts, "; ") + "}"
}

func formatResourceSummaries(resources map[string]spec.Resource, prefix string) string {
	if len(resources) == 0 {
		return "- none\n"
	}

	names := make([]string, 0, len(resources))
	for name := range resources {
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	for _, name := range names {
		resource := resources[name]
		fullName := name
		if prefix != "" {
			fullName = prefix + "." + name
		}

		endpoints := make([]string, 0, len(resource.Endpoints))
		for endpointName, endpoint := range resource.Endpoints {
			endpoints = append(endpoints, fmt.Sprintf("%s (%s)", endpointName, strings.ToUpper(endpoint.Method)))
		}
		sort.Strings(endpoints)

		line := fmt.Sprintf("- %s", fullName)
		if resource.Description != "" {
			line += ": " + resource.Description
		}
		if len(endpoints) > 0 {
			line += " | endpoints: " + strings.Join(endpoints, ", ")
		}
		builder.WriteString(line + "\n")

		if len(resource.SubResources) > 0 {
			builder.WriteString(formatResourceSummaries(resource.SubResources, fullName))
		}
	}

	return builder.String()
}

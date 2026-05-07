package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/vision"
	"github.com/spf13/cobra"
)

func newVisionCmd() *cobra.Command {
	var apiName string
	var outputDir string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "vision",
		Short: "Generate a visionary research template for an API",
		Long: `Creates a visionary-research.md skeleton for an API. This template
is designed to be filled in by the SKILL.md Phase 0 research process,
which uses LLM + web search to discover usage patterns, non-wrapper tools,
workflows, and architecture decisions.

The vision command produces the structure; Phase 0 fills it with intelligence.`,
		Example: `  # Generate visionary research for an API
  printing-press vision --api stripe --output ./research`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if apiName == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--api is required")}
			}
			if outputDir == "" {
				state, err := pipeline.LoadCurrentState(apiName)
				if err != nil {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("no current run for %s; run `printing-press print %s` first or pass --output", apiName, apiName)}
				}
				outputDir = state.ResearchDir()
			}

			absOut, err := filepath.Abs(outputDir)
			if err != nil {
				return fmt.Errorf("resolving output path: %w", err)
			}

			plan := &vision.VisionaryPlan{
				APIName: apiName,
				Identity: vision.APIIdentity{
					DomainCategory: "TODO: Determine from API docs",
					PrimaryUsers:   []string{"TODO: Identify from research"},
					CoreEntities:   []string{"TODO: Extract from spec"},
					DataProfile: vision.DataProfile{
						WritePattern: "TODO: append-only, mutable, or event-sourced",
						Volume:       "TODO: high, medium, or low",
						Realtime:     false,
						SearchNeed:   "TODO: high or low",
					},
				},
				UsagePatterns: []vision.UsagePattern{
					{Name: "TODO: Top usage pattern", EvidenceScore: 0, Description: "Discovered via Phase 0b community research"},
				},
				ToolLandscape: []vision.ToolClassification{
					{Name: "TODO: Discover via Phase 0c", ToolType: "unknown"},
				},
				Workflows: []vision.Workflow{
					{Name: "TODO: Identify via Phase 0d", Steps: []vision.WorkflowStep{{Description: "step 1"}}, ProposedCLIFeature: "TODO"},
				},
				Architecture: []vision.ArchitectureDecision{
					{Area: "persistence", NeedLevel: "TODO", Decision: "TODO", Rationale: "TODO"},
					{Area: "search", NeedLevel: "TODO", Decision: "TODO", Rationale: "TODO"},
					{Area: "realtime", NeedLevel: "TODO", Decision: "TODO", Rationale: "TODO"},
					{Area: "bulk", NeedLevel: "TODO", Decision: "TODO", Rationale: "TODO"},
					{Area: "caching", NeedLevel: "TODO", Decision: "TODO", Rationale: "TODO"},
				},
				Features: []vision.FeatureIdea{
					{Name: "TODO: Top feature", Description: "Discovered via Phase 0f ideation", TemplateNames: []string{"export.go.tmpl"}},
				},
			}

			if err := vision.WriteReport(plan, absOut); err != nil {
				return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("writing visionary research: %w", err)}
			}

			fmt.Fprintf(os.Stderr, "Visionary research template written to %s/visionary-research.md\n", absOut)
			fmt.Fprintf(os.Stderr, "Run Phase 0 (SKILL.md) to fill it with real research.\n")
			if asJSON {
				if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
					"api_name":    apiName,
					"output_file": filepath.Join(absOut, "visionary-research.md"),
				}); err != nil {
					return fmt.Errorf("encoding JSON: %w", err)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&apiName, "api", "", "API name to research")
	cmd.Flags().StringVar(&outputDir, "output", "", "Output directory (default: current runstate research dir for the API)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

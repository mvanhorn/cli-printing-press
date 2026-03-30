package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/internal/docspec"
	"github.com/mvanhorn/cli-printing-press/internal/generator"
	"github.com/mvanhorn/cli-printing-press/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/internal/llm"
	"github.com/mvanhorn/cli-printing-press/internal/llmpolish"
	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/mvanhorn/cli-printing-press/internal/version"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func Execute() error {
	rootCmd := &cobra.Command{
		Use:           "printing-press",
		Short:         "Describe your API. Get a production CLI.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Version,
	}
	rootCmd.SetVersionTemplate("printing-press {{.Version}}\n")

	rootCmd.AddCommand(newGenerateCmd())
	rootCmd.AddCommand(newScorecardCmd())
	rootCmd.AddCommand(newDogfoodCmd())
	rootCmd.AddCommand(newVerifyCmd())
	rootCmd.AddCommand(newEmbossCmd())
	rootCmd.AddCommand(newVisionCmd())
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newPrintCmd())
	rootCmd.AddCommand(newSniffCmd())
	rootCmd.AddCommand(newCatalogCmd())
	rootCmd.AddCommand(newLibraryCmd())
	rootCmd.AddCommand(newPublishCmd())

	return rootCmd.Execute()
}

func newGenerateCmd() *cobra.Command {
	var specFiles []string
	var cliName string
	var outputDir string
	var validate bool
	var refresh bool
	var force bool
	var lenient bool
	var docsURL string
	var polish bool
	var asJSON bool
	var dryRun bool
	var specSource string
	var clientPattern string
	var researchDir string

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a Go CLI project from an API spec",
		Example: `  # Generate from a local OpenAPI spec
  printing-press generate --spec ./openapi.yaml

  # Generate from a URL with force overwrite
  printing-press generate --spec https://api.example.com/openapi.json --force

  # Generate from API documentation
  printing-press generate --docs https://docs.stripe.com/api --name stripe

  # Multiple specs merged into one CLI
  printing-press generate --spec api-v1.yaml --spec api-v2.yaml --name myapi`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRun && docsURL != "" {
				return fmt.Errorf("--dry-run cannot be used with --docs (doc scraping has unavoidable side effects)")
			}
			if docsURL != "" {
				apiName := cliName
				if apiName == "" {
					apiName = "myapi"
				}

				var docSpec *spec.APISpec
				var err error

				if llm.Available() {
					fmt.Fprintln(os.Stderr, "Using LLM to understand API docs...")
					docSpec, err = docspec.GenerateFromDocsLLM(docsURL, apiName)
					if err != nil {
						fmt.Fprintf(os.Stderr, "warning: LLM doc-to-spec failed, falling back to regex: %v\n", err)
						docSpec, err = docspec.GenerateFromDocs(docsURL, apiName)
					}
				} else {
					docSpec, err = docspec.GenerateFromDocs(docsURL, apiName)
				}
				if err != nil {
					return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("generating spec from docs: %w", err)}
				}
				docYAML, err := yaml.Marshal(docSpec)
				if err != nil {
					return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("marshaling doc spec: %w", err)}
				}
				// Re-parse through the standard path so validation is consistent
				parsed, err := spec.ParseBytes(docYAML)
				if err != nil {
					return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("parsing generated spec: %w", err)}
				}
				if specSource != "" {
					switch specSource {
					case "official", "community", "sniffed", "docs":
						parsed.SpecSource = specSource
					default:
						return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--spec-source must be one of: official, community, sniffed, docs (got %q)", specSource)}
					}
				} else {
					parsed.SpecSource = "docs"
				}
				if clientPattern != "" {
					switch clientPattern {
					case "rest", "proxy-envelope":
						parsed.ClientPattern = clientPattern
					default:
						return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--client-pattern must be one of: rest, proxy-envelope (got %q)", clientPattern)}
					}
				}

				explicitOutput := outputDir != ""
				if outputDir == "" {
					outputDir = pipeline.DefaultOutputDir(parsed.Name)
				}
				absOut, err := filepath.Abs(outputDir)
				if err != nil {
					return fmt.Errorf("resolving output path: %w", err)
				}
				absOut, err = claimOrForce(absOut, force, explicitOutput)
				if err != nil {
					return &ExitError{Code: ExitInputError, Err: err}
				}

				gen := generator.New(parsed, absOut)
				loadResearchSources(gen, researchDir)
				if err := gen.Generate(); err != nil {
					return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("generating project: %w", err)}
				}
				if validate {
					if err := gen.Validate(); err != nil {
						return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("validating generated project: %w", err)}
					}
				}

				polished := false
				if polish {
					fmt.Fprintln(os.Stderr, "Running LLM polish pass...")
					polishResult, polishErr := llmpolish.Polish(llmpolish.PolishRequest{
						APIName:   parsed.Name,
						OutputDir: absOut,
					})
					if polishErr != nil {
						fmt.Fprintf(os.Stderr, "warning: polish failed: %v\n", polishErr)
					} else if polishResult.Skipped {
						fmt.Fprintf(os.Stderr, "polish skipped: %s\n", polishResult.SkipReason)
					} else {
						polished = true
						fmt.Fprintf(os.Stderr, "Polish: %d help texts improved, %d examples added, README %v\n",
							polishResult.HelpTextsImproved, polishResult.ExamplesAdded, polishResult.READMERewritten)
					}
				}

				if err := pipeline.WriteManifestForGenerate(pipeline.GenerateManifestParams{
					APIName:   parsed.Name,
					DocsURL:   docsURL,
					OutputDir: absOut,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not write manifest: %v\n", err)
				}

				fmt.Fprintf(os.Stderr, "Generated %s at %s (from docs)\n", naming.CLI(parsed.Name), absOut)
				if asJSON {
					if err := json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"name":       parsed.Name,
						"output_dir": absOut,
						"spec_files": specFiles,
						"validated":  validate,
						"polished":   polished,
					}); err != nil {
						return fmt.Errorf("encoding JSON: %w", err)
					}
				}
				return nil
			}

			if len(specFiles) == 0 {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--spec is required")}
			}

			var specs []*spec.APISpec
			for _, specFile := range specFiles {
				data, err := readSpec(specFile, refresh, dryRun)
				if err != nil {
					return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("reading spec %s: %w", specFile, err)}
				}

				var apiSpec *spec.APISpec
				if openapi.IsOpenAPI(data) {
					if lenient {
						apiSpec, err = openapi.ParseLenient(data)
					} else {
						apiSpec, err = openapi.Parse(data)
					}
				} else if graphql.IsGraphQLSDL(data) {
					apiSpec, err = graphql.ParseSDLBytes(specFile, data)
				} else {
					apiSpec, err = spec.ParseBytes(data)
				}
				if err != nil {
					return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("parsing spec %s: %w", specFile, err)}
				}

				specs = append(specs, apiSpec)
			}

			var apiSpec *spec.APISpec
			if len(specs) == 1 {
				apiSpec = specs[0]
			} else {
				if cliName == "" {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--name is required when using multiple specs")}
				}
				apiSpec = mergeSpecs(specs, cliName)
			}

			if specSource != "" {
				switch specSource {
				case "official", "community", "sniffed", "docs":
					apiSpec.SpecSource = specSource
				default:
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--spec-source must be one of: official, community, sniffed, docs (got %q)", specSource)}
				}
			}
			if clientPattern != "" {
				switch clientPattern {
				case "rest", "proxy-envelope":
					apiSpec.ClientPattern = clientPattern
				default:
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--client-pattern must be one of: rest, proxy-envelope (got %q)", clientPattern)}
				}
			}

			explicitOutput := outputDir != ""
			if outputDir == "" {
				outputDir = pipeline.DefaultOutputDir(apiSpec.Name)
			}

			absOut, err := filepath.Abs(outputDir)
			if err != nil {
				return fmt.Errorf("resolving output path: %w", err)
			}
			if dryRun {
				return printDryRun(apiSpec, absOut, specFiles)
			}
			absOut, err = claimOrForce(absOut, force, explicitOutput)
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: err}
			}

			gen := generator.New(apiSpec, absOut)
			loadResearchSources(gen, researchDir)
			if err := gen.Generate(); err != nil {
				return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("generating project: %w", err)}
			}
			if validate {
				if err := gen.Validate(); err != nil {
					return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("validating generated project: %w", err)}
				}
			}

			polished := false
			if polish {
				fmt.Fprintln(os.Stderr, "Running LLM polish pass...")
				polishResult, polishErr := llmpolish.Polish(llmpolish.PolishRequest{
					APIName:   apiSpec.Name,
					OutputDir: absOut,
				})
				if polishErr != nil {
					fmt.Fprintf(os.Stderr, "warning: polish failed: %v\n", polishErr)
				} else if polishResult.Skipped {
					fmt.Fprintf(os.Stderr, "polish skipped: %s\n", polishResult.SkipReason)
				} else {
					polished = true
					fmt.Fprintf(os.Stderr, "Polish: %d help texts improved, %d examples added, README %v\n",
						polishResult.HelpTextsImproved, polishResult.ExamplesAdded, polishResult.READMERewritten)
				}
			}

			// Rename output directory to match the derived CLI name if they differ.
			// This prevents mismatches when the caller passes a directory name
			// that doesn't match what the generator derives from the spec title
			// (e.g., --output .../calcom-pp-cli but spec title "Cal.com" derives "cal-com-pp-cli").
			derivedDir := naming.CLI(apiSpec.Name)
			currentBase := filepath.Base(absOut)
			if currentBase != derivedDir {
				finalPath := filepath.Join(filepath.Dir(absOut), derivedDir)
				if err := os.Rename(absOut, finalPath); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not rename output dir from %s to %s: %v\n", currentBase, derivedDir, err)
				} else {
					absOut = finalPath
				}
			}

			if err := pipeline.WriteManifestForGenerate(pipeline.GenerateManifestParams{
				APIName:   apiSpec.Name,
				SpecSrcs:  specFiles,
				OutputDir: absOut,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write manifest: %v\n", err)
			}

			fmt.Fprintf(os.Stderr, "Generated %s at %s\n", naming.CLI(apiSpec.Name), absOut)
			if asJSON {
				if err := json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"name":       apiSpec.Name,
					"output_dir": absOut,
					"spec_files": specFiles,
					"validated":  validate,
					"polished":   polished,
				}); err != nil {
					return fmt.Errorf("encoding JSON: %w", err)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&specFiles, "spec", nil, "Path or URL to API spec (can be repeated)")
	cmd.Flags().StringVar(&cliName, "name", "", "CLI name (required when using multiple specs)")
	cmd.Flags().StringVar(&outputDir, "output", "", "Output directory (default: ~/printing-press/library/<name>-pp-cli)")
	cmd.Flags().BoolVar(&validate, "validate", true, "Run quality gates on the generated project")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Refresh cached remote spec before generating")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite the base output directory (e.g. ~/printing-press/library/notion-pp-cli) instead of auto-incrementing")
	cmd.Flags().BoolVar(&lenient, "lenient", false, "Skip validation errors from broken $refs in OpenAPI specs")
	cmd.Flags().StringVar(&docsURL, "docs", "", "API documentation URL to generate spec from")
	cmd.Flags().BoolVar(&polish, "polish", false, "Run LLM polish pass on generated CLI (requires claude or codex CLI)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Parse spec and show what would be generated without writing files (remote specs are still fetched)")
	cmd.Flags().StringVar(&specSource, "spec-source", "", "Spec provenance: official, community, sniffed, docs (affects generated client defaults like rate limiting)")
	cmd.Flags().StringVar(&clientPattern, "client-pattern", "", "HTTP client pattern: rest (default), proxy-envelope (wraps requests in POST envelope)")
	cmd.Flags().StringVar(&researchDir, "research-dir", "", "Pipeline directory containing research.json and discovery/ for README source credits")

	return cmd
}

func readSpec(specFile string, refresh bool, skipCache bool) ([]byte, error) {
	if strings.HasPrefix(specFile, "http://") || strings.HasPrefix(specFile, "https://") {
		return fetchOrCacheSpec(specFile, refresh, skipCache)
	}

	return os.ReadFile(specFile)
}

func mergeSpecs(specs []*spec.APISpec, name string) *spec.APISpec {
	if len(specs) == 1 {
		return specs[0]
	}

	merged := &spec.APISpec{
		Name:        name,
		Description: "Combined CLI for multiple API services",
		Version:     specs[0].Version,
		BaseURL:     specs[0].BaseURL,
		BasePath:    specs[0].BasePath,
		Auth:        specs[0].Auth,
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   fmt.Sprintf("~/.config/%s-pp-cli/config.toml", name),
		},
		Resources: map[string]spec.Resource{},
		Types:     map[string]spec.TypeDef{},
	}

	for _, s := range specs {
		for resourceName, resource := range s.Resources {
			key := resourceName
			if _, exists := merged.Resources[key]; exists {
				key = s.Name + "-" + resourceName
			}
			merged.Resources[key] = resource
		}

		for typeName, typeDef := range s.Types {
			key := typeName
			if _, exists := merged.Types[key]; exists {
				key = s.Name + "-" + typeName
			}
			merged.Types[key] = typeDef
		}

		if s.Auth.AuthorizationURL != "" && merged.Auth.AuthorizationURL == "" {
			merged.Auth = s.Auth
		}
	}

	return merged
}

// claimOrForce resolves the output directory based on --force and --output flags.
//
//   - force=true:  RemoveAll the target, then create it fresh (claims exact slot)
//   - explicit output (--output set) without force: error if exists and non-empty
//   - default (no --output, no --force): auto-increment via ClaimOutputDir
func claimOrForce(absOut string, force bool, explicitOutput bool) (string, error) {
	if force {
		if err := os.RemoveAll(absOut); err != nil {
			return "", fmt.Errorf("removing existing output dir: %w", err)
		}
		if err := os.MkdirAll(absOut, 0o755); err != nil {
			return "", fmt.Errorf("creating output dir: %w", err)
		}
		return absOut, nil
	}

	if explicitOutput {
		if info, err := os.Stat(absOut); err == nil && info.IsDir() {
			entries, readErr := os.ReadDir(absOut)
			if readErr != nil {
				return "", fmt.Errorf("reading output directory: %w", readErr)
			}
			if len(entries) > 0 {
				return "", fmt.Errorf("output directory %s already exists (use --force to overwrite)", absOut)
			}
		}
		return absOut, nil
	}

	return pipeline.ClaimOutputDir(absOut)
}

func fetchOrCacheSpec(specURL string, refresh bool, skipCache bool) ([]byte, error) {
	sum := sha256.Sum256([]byte(specURL))
	cacheKey := hex.EncodeToString(sum[:])

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding user home directory: %w", err)
	}

	cacheDir := filepath.Join(homeDir, ".cache", "printing-press", "specs")
	cachePath := filepath.Join(cacheDir, cacheKey+".json")

	// Read from existing cache even in dry-run mode (no writes needed)
	if !refresh {
		info, err := os.Stat(cachePath)
		switch {
		case err == nil && time.Since(info.ModTime()) < 24*time.Hour:
			fmt.Fprintf(os.Stderr, "Using cached spec for %s\n", specURL)
			data, readErr := os.ReadFile(cachePath)
			if readErr != nil {
				return nil, fmt.Errorf("reading cached spec: %w", readErr)
			}
			return data, nil
		case err != nil && !os.IsNotExist(err):
			return nil, fmt.Errorf("checking cached spec: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "Fetching spec from %s...\n", specURL)
	resp, err := http.Get(specURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected response status: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if !skipCache {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating cache directory: %w", err)
		}
		if err := os.WriteFile(cachePath, data, 0o644); err != nil {
			return nil, fmt.Errorf("writing cached spec: %w", err)
		}
	}

	return data, nil
}

func newVersionCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:     "version",
		Short:   "Print version",
		Example: `  printing-press version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(map[string]string{
					"version": version.Version,
					"go":      runtime.Version(),
				})
			}
			fmt.Printf("printing-press %s\n", version.Version)
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newPrintCmd() *cobra.Command {
	var outputDir string
	var force bool
	var resume bool
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "print <api-name>",
		Short: "Create an autonomous CLI generation pipeline",
		Long:  "Creates a pipeline directory with plan seeds for each phase. Use /ce:work on each plan to execute.",
		Example: `  # Run full pipeline for a catalog API
  printing-press print stripe

  # Force overwrite existing pipeline
  printing-press print stripe --force

  # Resume an interrupted pipeline
  printing-press print stripe --resume`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apiName := args[0]

			state, err := pipeline.Init(apiName, pipeline.Options{
				OutputDir: outputDir,
				Force:     force,
				Resume:    resume,
			})
			if err != nil {
				msg := err.Error()
				switch {
				case strings.Contains(msg, "already exists"):
					return &ExitError{Code: ExitInputError, Err: err}
				case strings.Contains(msg, "discovering spec"):
					return &ExitError{Code: ExitSpecError, Err: err}
				default:
					return &ExitError{Code: ExitGenerationError, Err: err}
				}
			}

			fmt.Fprintf(os.Stderr, "Pipeline created for %s\n", apiName)
			fmt.Fprintf(os.Stderr, "  Spec: %s\n", state.SpecURL)
			fmt.Fprintf(os.Stderr, "  Output: %s\n", state.EffectiveWorkingDir())
			fmt.Fprintf(os.Stderr, "  Plans:\n")
			for i, phase := range pipeline.PhaseOrder {
				fmt.Fprintf(os.Stderr, "    %d. %s\n", i, state.PlanPath(phase))
			}
			fmt.Fprintf(os.Stderr, "\nStart with: /ce:work %s\n", state.PlanPath(pipeline.PhasePreflight))

			if asJSON {
				if err := json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"api_name":         apiName,
					"pipeline_dir":     state.PipelineDir(),
					"phases_completed": countCompletedPhases(state),
					"state_file":       state.StatePath(),
					"working_dir":      state.EffectiveWorkingDir(),
					"run_id":           state.RunID,
				}); err != nil {
					return fmt.Errorf("encoding JSON: %w", err)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&outputDir, "output", "", "Working directory (default: ~/printing-press/.runstate/<scope>/runs/<run-id>/working/<api-name>-pp-cli)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing pipeline")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume from existing checkpoint")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func countCompletedPhases(state *pipeline.PipelineState) int {
	n := 0
	for _, p := range state.Phases {
		if p.Status == pipeline.StatusCompleted {
			n++
		}
	}
	return n
}

func printDryRun(apiSpec *spec.APISpec, absOut string, specFiles []string) error {
	resourceCount := 0
	endpointCount := 0
	for _, r := range apiSpec.Resources {
		resourceCount++
		endpointCount += len(r.Endpoints)
		for _, sub := range r.SubResources {
			resourceCount++
			endpointCount += len(sub.Endpoints)
		}
	}

	fmt.Fprintf(os.Stderr, "Dry run — spec parsed, no files will be generated\n")
	fmt.Fprintf(os.Stderr, "  Spec files: %s\n", strings.Join(specFiles, ", "))
	fmt.Fprintf(os.Stderr, "  API name:   %s\n", apiSpec.Name)
	fmt.Fprintf(os.Stderr, "  Output dir: %s\n", absOut)
	fmt.Fprintf(os.Stderr, "  Resources:  %d\n", resourceCount)
	fmt.Fprintf(os.Stderr, "  Endpoints:  %d\n", endpointCount)

	summary := map[string]any{
		"dry_run":        true,
		"name":           apiSpec.Name,
		"output_dir":     absOut,
		"spec_files":     specFiles,
		"resource_count": resourceCount,
		"endpoint_count": endpointCount,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(summary)
}

// loadResearchSources populates the generator's Sources and DiscoveryPages
// from a pipeline research directory. Silently skips if researchDir is empty
// or data is unavailable.
func loadResearchSources(gen *generator.Generator, researchDir string) {
	if researchDir == "" {
		return
	}
	research, err := pipeline.LoadResearch(researchDir)
	if err == nil {
		for _, s := range pipeline.SourcesForREADME(research) {
			gen.Sources = append(gen.Sources, generator.ReadmeSource{
				Name:     s.Name,
				URL:      s.URL,
				Language: s.Language,
				Stars:    s.Stars,
			})
		}
	}
	discoveryDir := filepath.Join(researchDir, "discovery")
	gen.DiscoveryPages = pipeline.ParseDiscoveryPages(discoveryDir)
}

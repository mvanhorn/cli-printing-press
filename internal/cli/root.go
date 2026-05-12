package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	catalogfs "github.com/mvanhorn/cli-printing-press/v4/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/docspec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/llm"
	"github.com/mvanhorn/cli-printing-press/v4/internal/llmpolish"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline/regenmerge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/version"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func Execute() error {
	rootCmd := &cobra.Command{
		Use:          "printing-press",
		Short:        "Describe your API. Get a production CLI.",
		SilenceUsage: true,
		Version:      version.Version,
	}
	rootCmd.SetVersionTemplate("printing-press {{.Version}}\n")

	rootCmd.AddCommand(newGenerateCmd())
	rootCmd.AddCommand(newScorecardCmd())
	rootCmd.AddCommand(newDogfoodCmd())
	rootCmd.AddCommand(newRegenMergeCmd())
	rootCmd.AddCommand(newValidateNarrativeCmd())
	rootCmd.AddCommand(newVerifyCmd())
	rootCmd.AddCommand(newVerifySkillCmd())
	rootCmd.AddCommand(newEmbossCmd())
	rootCmd.AddCommand(newPatchCmd())
	rootCmd.AddCommand(newVisionCmd())
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newPrintCmd())
	rootCmd.AddCommand(newBrowserSniffCmd())
	rootCmd.AddCommand(newCrowdSniffCmd())
	rootCmd.AddCommand(newCatalogCmd())
	rootCmd.AddCommand(newLibraryCmd())
	rootCmd.AddCommand(newAuthCmd())
	rootCmd.AddCommand(newPublishCmd())
	rootCmd.AddCommand(newPolishCmd())
	rootCmd.AddCommand(newWorkflowVerifyCmd())
	rootCmd.AddCommand(newShipcheckCmd())
	rootCmd.AddCommand(newLockCmd())
	rootCmd.AddCommand(newMCPAuditCmd())
	rootCmd.AddCommand(newToolsAuditCmd())
	rootCmd.AddCommand(newPublicParamAuditCmd())
	rootCmd.AddCommand(newPIIAuditCmd())
	rootCmd.AddCommand(newProbeReachabilityCmd())
	rootCmd.AddCommand(newSchemaCmd())
	rootCmd.AddCommand(newBundleCmd())
	rootCmd.AddCommand(newMCPSyncCmd())

	return rootCmd.Execute()
}

func newGenerateCmd() *cobra.Command {
	var specFiles []string
	var cliName string
	var owner string
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
	var httpTransport string
	var researchDir string
	var maxEndpointsPerResource int
	var maxResources int
	var specURL string
	var planFile string
	var trafficAnalysisPath string

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a Go CLI project from an API spec",
		Example: `  # Generate from a local OpenAPI spec
  printing-press generate --spec ./openapi.yaml

  # Generate from a URL and recreate output while preserving hand-authored CLI files
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
				if docSpec.BaseURLIsPlaceholder {
					return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("doc scrape of %s found no API base URL; the generator refuses to ship a CLI whose `doctor` would DNS-fail on every call. Re-run with docs that include the API host, or supply a real --base-url via crowd-sniff", docsURL)}
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
				if err := applyGenerateSpecFlags(parsed, specSource, "docs", clientPattern, httpTransport, owner); err != nil {
					return err
				}

				absOut, _, snapshotDir, err := resolveGenerateOutputDir(outputDir, parsed.Name, force, true)
				if err != nil {
					return err
				}

				novelFeatures, polished, err := runGenerateProject(parsed, absOut, generateProjectOptions{validate: validate, polish: polish, researchDir: researchDir, trafficAnalysisPath: trafficAnalysisPath})
				if err != nil {
					return err
				}

				if snapshotDir != "" {
					if err := finalizeForceMerge(snapshotDir, absOut, docYAML); err != nil {
						return err
					}
				}

				runID := pipeline.DeriveRunIDFromResearchDir(researchDir)
				if runID == "" {
					fmt.Fprintln(os.Stderr, "warning: could not derive run_id from --research-dir; phase5 dogfood acceptance will refuse to write without it")
				}
				if err := pipeline.WriteManifestForGenerate(pipeline.GenerateManifestParams{
					APIName:       parsed.Name,
					DocsURL:       docsURL,
					OutputDir:     absOut,
					Owner:         parsed.Owner,
					Printer:       parsed.Printer,
					PrinterName:   parsed.PrinterName,
					RunID:         runID,
					Spec:          parsed,
					NovelFeatures: novelFeatures,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not write manifest: %v\n", err)
				}

				fmt.Fprintf(os.Stderr, "Generated %s at %s (from docs)\n", parsed.Name, absOut)
				autoBundleForHost(absOut, os.Stderr)
				if asJSON {
					if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
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

			if planFile != "" {
				if trafficAnalysisPath != "" {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--traffic-analysis cannot be used with --plan")}
				}
				planData, err := os.ReadFile(planFile)
				if err != nil {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("reading plan file: %w", err)}
				}
				planSpec := generator.ParsePlan(string(planData))
				if planSpec.CLIName == "" {
					if cliName != "" {
						planSpec.CLIName = cliName
					} else {
						return &ExitError{Code: ExitInputError, Err: fmt.Errorf("plan has no CLI name and --name was not provided")}
					}
				}
				if cliName != "" {
					planSpec.CLIName = cliName
				}
				if len(planSpec.Commands) == 0 {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("plan contains no command definitions")}
				}

				absOut, _, snapshotDir, err := resolveGenerateOutputDir(outputDir, planSpec.CLIName, force, true)
				if err != nil {
					return err
				}

				if err := generator.GenerateFromPlan(planSpec, absOut); err != nil {
					return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("generating from plan: %w", err)}
				}

				if snapshotDir != "" {
					// Plan-driven generation does not write a manifest with
					// SpecChecksum, so the cross-spec guard naturally lands
					// on the defensive full-merge path. Pass nil so any
					// manifest hash that does exist still gates merge mode.
					if err := finalizeForceMerge(snapshotDir, absOut, nil); err != nil {
						return err
					}
				}

				fmt.Fprintf(os.Stderr, "Generated %s at %s (from plan)\n", naming.CLI(planSpec.CLIName), absOut)
				if asJSON {
					if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
						"name":       planSpec.CLIName,
						"output_dir": absOut,
						"plan_file":  planFile,
						"commands":   len(planSpec.Commands),
					}); err != nil {
						return fmt.Errorf("encoding JSON: %w", err)
					}
				}
				return nil
			}

			if len(specFiles) == 0 {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--spec is required (or use --plan for plan-driven generation)")}
			}

			if maxResources > 0 {
				openapi.SetMaxResources(maxResources)
			}
			if maxEndpointsPerResource > 0 {
				openapi.SetMaxEndpointsPerResource(maxEndpointsPerResource)
			}

			var specs []*spec.APISpec
			var specRawBytes [][]byte // raw spec data for archiving
			for _, specFile := range specFiles {
				data, err := readSpec(specFile, refresh, dryRun)
				if err != nil {
					return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("reading spec %s: %w", specFile, err)}
				}
				specRawBytes = append(specRawBytes, data)

				var apiSpec *spec.APISpec
				if openapi.IsOpenAPI(data) {
					apiSpec, err = parseOpenAPISpec(specFile, data, lenient)
				} else if graphql.IsGraphQLSDL(data) {
					apiSpec, err = graphql.ParseSDLBytes(specFile, data)
				} else {
					apiSpec, err = spec.ParseBytes(data)
				}
				if err != nil {
					return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("parsing spec %s: %w", specFile, err)}
				}

				if apiSpec.BaseURLIsPlaceholder {
					return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("spec %s declares no `servers:` block and no per-operation servers; the generator cannot resolve a real base URL and refuses to ship a CLI whose `doctor` would DNS-fail on every call. Add a `servers:` block with the real API host, or run via crowd-sniff with `--base-url` to supply one", specFile)}
				}

				specs = append(specs, apiSpec)
			}

			var apiSpec *spec.APISpec
			if len(specs) == 1 {
				apiSpec = specs[0]
				// Override spec-derived name when --name is explicitly provided
				if cliName != "" {
					apiSpec.Name = cliName
				}
			} else {
				if cliName == "" {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--name is required when using multiple specs")}
				}
				apiSpec = mergeSpecs(specs, cliName)
			}

			if err := applyGenerateSpecFlags(apiSpec, specSource, "", clientPattern, httpTransport, owner); err != nil {
				return err
			}

			absOut, explicitOutput, snapshotDir, err := resolveGenerateOutputDir(outputDir, apiSpec.Name, force, !dryRun)
			if err != nil {
				return err
			}
			if dryRun {
				return printDryRun(apiSpec, absOut, specFiles)
			}

			novelFeatures, polished, err := runGenerateProject(apiSpec, absOut, generateProjectOptions{validate: validate, polish: polish, researchDir: researchDir, trafficAnalysisPath: trafficAnalysisPath, specFiles: specFiles, specURL: specURL, rejectUnshippablePageContextTraffic: true})
			if err != nil {
				return err
			}

			// Merge any preserved hand-edits from the snapshot into the freshly
			// emitted tree. snapshotDir is non-empty only when --force ran and
			// the prior absOut had content. The cross-spec guard inside
			// mergeForceSnapshot falls back to NOVEL-only preservation when
			// the snapshot's spec hash differs from the current spec.
			if snapshotDir != "" {
				var primarySpec []byte
				if len(specRawBytes) > 0 {
					primarySpec = specRawBytes[0]
				}
				if err := finalizeForceMerge(snapshotDir, absOut, primarySpec); err != nil {
					return err
				}
			}

			// When --output was not explicitly supplied, normalize the output
			// directory to the spec-derived name so default-path runs land in the
			// expected slot (e.g., spec title "Cal.com" derives "cal-com-pp-cli").
			// When --output is explicit, the caller's chosen path is authoritative.
			if !explicitOutput {
				derivedDir := apiSpec.Name
				currentBase := filepath.Base(absOut)
				if currentBase != derivedDir {
					finalPath := filepath.Join(filepath.Dir(absOut), derivedDir)
					if err := os.Rename(absOut, finalPath); err != nil {
						fmt.Fprintf(os.Stderr, "warning: could not rename output dir from %s to %s: %v\n", currentBase, derivedDir, err)
					} else {
						absOut = finalPath
					}
				}
			}

			runID := pipeline.DeriveRunIDFromResearchDir(researchDir)
			if runID == "" {
				fmt.Fprintln(os.Stderr, "warning: could not derive run_id from --research-dir; phase5 dogfood acceptance will refuse to write without it")
			}
			if err := pipeline.WriteManifestForGenerate(pipeline.GenerateManifestParams{
				APIName:       apiSpec.Name,
				SpecSrcs:      specFiles,
				SpecURL:       specURL,
				OutputDir:     absOut,
				Owner:         apiSpec.Owner,
				Printer:       apiSpec.Printer,
				PrinterName:   apiSpec.PrinterName,
				RunID:         runID,
				Spec:          apiSpec,
				NovelFeatures: novelFeatures,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write manifest: %v\n", err)
			}

			// Archive the input spec alongside the CLI for reproducibility.
			// The spec_url may change or disappear; this local copy is the
			// only guaranteed way to regenerate from the exact same input.
			if len(specRawBytes) > 0 {
				archiveName := "spec.yaml"
				if json.Valid(specRawBytes[0]) {
					archiveName = "spec.json"
				}
				data := artifacts.RedactArchivedSpecSecrets(specRawBytes[0])
				if err := os.WriteFile(filepath.Join(absOut, archiveName), data, 0o644); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not archive spec: %v\n", err)
				}
			}

			fmt.Fprintf(os.Stderr, "Generated %s at %s\n", apiSpec.Name, absOut)
			autoBundleForHost(absOut, os.Stderr)
			if asJSON {
				if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
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
	cmd.Flags().StringVar(&owner, "owner", "", "Override owner attribution in generated copyright headers (highest priority; otherwise resolved from existing .printing-press.json, copyright header, or git config)")
	cmd.Flags().StringVar(&outputDir, "output", "", "Output directory (default: ~/printing-press/library/<name>)")
	cmd.Flags().BoolVar(&validate, "validate", true, "Run quality gates on the generated project")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Refresh cached remote spec before generating")
	cmd.Flags().BoolVar(&force, "force", false, "Recreate the base output directory while preserving hand-edits to generated files via AST-based merge")
	cmd.Flags().BoolVar(&lenient, "lenient", false, "Skip validation errors from broken $refs in OpenAPI specs")
	cmd.Flags().StringVar(&docsURL, "docs", "", "API documentation URL to generate spec from")
	cmd.Flags().BoolVar(&polish, "polish", false, "Run LLM polish pass on generated CLI (requires claude or codex CLI)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Parse spec and show what would be generated without writing files (remote specs are still fetched)")
	cmd.Flags().StringVar(&specSource, "spec-source", "", "Spec provenance: official, community, sniffed/browser-sniffed, docs (affects generated client defaults like rate limiting)")
	cmd.Flags().StringVar(&clientPattern, "client-pattern", "", "HTTP client pattern: rest (default), proxy-envelope (wraps requests in POST envelope)")
	cmd.Flags().StringVar(&httpTransport, "transport", "", "HTTP transport: standard, browser-http, browser-chrome, or browser-chrome-h3 (defaults based on spec provenance and reachability)")
	cmd.Flags().StringVar(&researchDir, "research-dir", "", "Pipeline directory containing research.json and discovery/ for README source credits")
	cmd.Flags().IntVar(&maxResources, "max-resources", 0, "Maximum resource groups to generate (default 500, raise for enormous APIs)")
	cmd.Flags().IntVar(&maxEndpointsPerResource, "max-endpoints-per-resource", 0, "Maximum endpoints per resource (default 50, raise for large APIs)")
	cmd.Flags().StringVar(&specURL, "spec-url", "", "Original spec URL for provenance (use when --spec is a local file downloaded from a URL)")
	cmd.Flags().StringVar(&planFile, "plan", "", "Path to a markdown plan document for plan-driven generation (instead of --spec)")
	cmd.Flags().StringVar(&trafficAnalysisPath, "traffic-analysis", "", "Path to browser-sniff traffic-analysis.json for advisory generation context")

	return cmd
}

func runGeneratePolishPass(enabled bool, apiName, outputDir string) bool {
	if !enabled {
		return false
	}

	fmt.Fprintln(os.Stderr, "Running LLM polish pass...")
	polishResult, polishErr := llmpolish.Polish(llmpolish.PolishRequest{
		APIName:   apiName,
		OutputDir: outputDir,
	})
	if polishErr != nil {
		fmt.Fprintf(os.Stderr, "warning: polish failed: %v\n", polishErr)
		return false
	}
	if polishResult.Skipped {
		fmt.Fprintf(os.Stderr, "polish skipped: %s\n", polishResult.SkipReason)
		return false
	}

	fmt.Fprintf(os.Stderr, "Polish: %d help texts improved, %d examples added, README %v\n",
		polishResult.HelpTextsImproved, polishResult.ExamplesAdded, polishResult.READMERewritten)
	return true
}

type generateProjectOptions struct {
	validate                            bool
	polish                              bool
	researchDir                         string
	trafficAnalysisPath                 string
	specFiles                           []string
	specURL                             string
	rejectUnshippablePageContextTraffic bool
}

func runGenerateProject(apiSpec *spec.APISpec, absOut string, opts generateProjectOptions) ([]pipeline.NovelFeatureManifest, bool, error) {
	enrichSpecFromCatalog(apiSpec, catalogSpecLookupRefs(opts.specFiles, opts.specURL)...)
	gen := generator.New(apiSpec, absOut)
	novelFeatures := loadResearchSources(gen, opts.researchDir)
	trafficAnalysis, err := loadTrafficAnalysisForGenerate(opts.trafficAnalysisPath, opts.specFiles, apiSpec.SpecSource)
	if err != nil {
		return nil, false, &ExitError{Code: ExitInputError, Err: err}
	}
	if opts.rejectUnshippablePageContextTraffic && trafficAnalysisRequiresUnshippablePageContext(trafficAnalysis) {
		return nil, false, &ExitError{Code: ExitInputError, Err: fmt.Errorf("traffic analysis says this target requires live browser page-context execution; persistent browser transport is not a shippable printed CLI runtime. Re-run discovery for a Surf/direct/browser-clearance replayable surface instead")}
	}
	applyHTTPTransportDefault(apiSpec, trafficAnalysis)
	browsersniff.ApplyReachabilityDefaults(apiSpec, trafficAnalysis)
	gen.TrafficAnalysis = trafficAnalysis
	if err := gen.Generate(); err != nil {
		return nil, false, &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("generating project: %w", err)}
	}
	if opts.validate {
		if err := gen.Validate(); err != nil {
			return nil, false, &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("validating generated project: %w", err)}
		}
	}
	return novelFeatures, runGeneratePolishPass(opts.polish, apiSpec.Name, absOut), nil
}

func applyGenerateSpecFlags(apiSpec *spec.APISpec, specSource, defaultSpecSource, clientPattern, httpTransport, owner string) error {
	if specSource != "" {
		normalized, err := normalizeSpecSource(specSource)
		if err != nil {
			return &ExitError{Code: ExitInputError, Err: err}
		}
		apiSpec.SpecSource = normalized
	} else if defaultSpecSource != "" {
		apiSpec.SpecSource = defaultSpecSource
	}
	if clientPattern != "" {
		normalized, err := normalizeClientPattern(clientPattern)
		if err != nil {
			return &ExitError{Code: ExitInputError, Err: err}
		}
		apiSpec.ClientPattern = normalized
	}
	if httpTransport != "" {
		normalized, err := normalizeHTTPTransport(httpTransport)
		if err != nil {
			return &ExitError{Code: ExitInputError, Err: err}
		}
		apiSpec.HTTPTransport = normalized
	}
	if owner != "" {
		apiSpec.Owner = owner
	}
	return nil
}

func normalizeSpecSource(value string) (string, error) {
	switch value {
	case "", "official", "community", "sniffed", "docs":
		return value, nil
	case "browser-sniffed":
		return "sniffed", nil
	default:
		return "", fmt.Errorf("--spec-source must be one of: official, community, sniffed, browser-sniffed, docs (got %q)", value)
	}
}

func normalizeClientPattern(value string) (string, error) {
	switch value {
	case "", "rest", "proxy-envelope":
		return value, nil
	default:
		return "", fmt.Errorf("--client-pattern must be one of: rest, proxy-envelope (got %q)", value)
	}
}

func normalizeHTTPTransport(value string) (string, error) {
	switch value {
	case "", spec.HTTPTransportStandard, spec.HTTPTransportBrowserHTTP, spec.HTTPTransportBrowserChrome, spec.HTTPTransportBrowserChromeH3:
		return value, nil
	default:
		return "", fmt.Errorf("--transport must be one of: standard, browser-http, browser-chrome, browser-chrome-h3 (got %q)", value)
	}
}

func resolveGenerateOutputDir(outputDir, cliName string, force bool, claim bool) (resolvedAbsOut string, explicitOutput bool, snapshotDir string, err error) {
	explicitOutput = outputDir != ""
	if outputDir == "" {
		outputDir = pipeline.DefaultOutputDir(cliName)
	}
	absOut, err := filepath.Abs(outputDir)
	if err != nil {
		return "", false, "", fmt.Errorf("resolving output path: %w", err)
	}
	if !claim {
		return absOut, explicitOutput, "", nil
	}
	absOut, snapshotDir, err = claimOrForce(absOut, force, explicitOutput)
	if err != nil {
		return "", false, "", &ExitError{Code: ExitInputError, Err: err}
	}
	return absOut, explicitOutput, snapshotDir, nil
}

func applyHTTPTransportDefault(apiSpec *spec.APISpec, analysis *browsersniff.TrafficAnalysis) {
	if apiSpec == nil || apiSpec.HTTPTransport != "" {
		return
	}
	if trafficAnalysisExplicitlyRecommendsBrowserHTTP3Transport(analysis) {
		apiSpec.HTTPTransport = spec.HTTPTransportBrowserChromeH3
		return
	}
	if trafficAnalysisRecommendsBrowserTransport(analysis) {
		apiSpec.HTTPTransport = spec.HTTPTransportBrowserChrome
	}
}

func trafficAnalysisRequiresUnshippablePageContext(analysis *browsersniff.TrafficAnalysis) bool {
	if analysis == nil {
		return false
	}
	if analysis.Reachability != nil {
		switch analysis.Reachability.Mode {
		case "browser_required":
			return true
		}
	}
	for _, hint := range analysis.GenerationHints {
		hint = strings.ToLower(hint)
		if hint == "requires_page_context" || hint == "page_context_required" {
			return true
		}
		// Backward compatibility for older traffic-analysis artifacts generated
		// before resident browser runtime transport was removed.
		if hint == "browser_runtime_required" || strings.Contains(hint, "browser_runtime") {
			return true
		}
	}
	return false
}

func trafficAnalysisRecommendsBrowserTransport(analysis *browsersniff.TrafficAnalysis) bool {
	if analysis == nil {
		return false
	}
	if analysis.Reachability != nil {
		switch analysis.Reachability.Mode {
		case "browser_http", "browser_clearance_http":
			return true
		}
	}
	for _, protocol := range analysis.Protocols {
		if protocol.Label == "html_scrape" {
			return true
		}
	}
	for _, protection := range analysis.Protections {
		switch strings.ToLower(protection.Label) {
		case "cloudflare", "datadome", "akamai", "perimeterx", "captcha", "protected_web", "aws_waf", "bot_challenge":
			return true
		}
	}
	for _, hint := range analysis.GenerationHints {
		hint = strings.ToLower(hint)
		if strings.Contains(hint, "browser") || strings.Contains(hint, "scrape") {
			return true
		}
	}
	return false
}

func trafficAnalysisExplicitlyRecommendsBrowserHTTP3Transport(analysis *browsersniff.TrafficAnalysis) bool {
	if analysis == nil {
		return false
	}
	for _, hint := range analysis.GenerationHints {
		hint = strings.ToLower(hint)
		if strings.Contains(hint, "http3") || strings.Contains(hint, "http_3") || strings.Contains(hint, "h3") {
			return true
		}
	}
	return false
}

func loadTrafficAnalysisForGenerate(inputPath string, specFiles []string, specSource string) (*browsersniff.TrafficAnalysis, error) {
	if strings.TrimSpace(inputPath) == "" {
		inputPath = inferTrafficAnalysisPath(specFiles, specSource)
	}
	if strings.TrimSpace(inputPath) == "" {
		return nil, nil
	}

	analysis, err := browsersniff.ReadTrafficAnalysis(inputPath)
	if err != nil {
		return nil, fmt.Errorf("loading traffic analysis %s: %w", inputPath, err)
	}
	return analysis, nil
}

func inferTrafficAnalysisPath(specFiles []string, specSource string) string {
	if specSource != "sniffed" || len(specFiles) != 1 {
		return ""
	}
	specPath := specFiles[0]
	if openapi.IsRemoteSpecSource(specPath) {
		return ""
	}
	candidate := browsersniff.DefaultTrafficAnalysisPath(specPath)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func readSpec(specFile string, refresh bool, skipCache bool) ([]byte, error) {
	data, err := openapi.LoadSpecBytes(specFile, refresh, skipCache)
	if err != nil {
		return nil, err
	}
	if rejectErr := rejectIfNotSpec(data); rejectErr != nil {
		return nil, rejectErr
	}
	return data, nil
}

func parseOpenAPISpec(specFile string, data []byte, lenient bool) (*spec.APISpec, error) {
	if openapi.IsRemoteSpecSource(specFile) {
		if lenient {
			return openapi.ParseLenient(data)
		}
		return openapi.Parse(data)
	}
	if lenient {
		return openapi.ParseWithPathLenient(data, specFile)
	}
	return openapi.ParseWithPath(data, specFile)
}

func mergeSpecs(specs []*spec.APISpec, name string) *spec.APISpec {
	if len(specs) == 1 {
		return specs[0]
	}

	mergedBaseURL, perSpecPathPrefix := planMultiSpecBaseURL(specs)

	merged := &spec.APISpec{
		Name:        name,
		Description: "Combined CLI for multiple API services",
		Version:     specs[0].Version,
		BaseURL:     mergedBaseURL,
		BasePath:    specs[0].BasePath,
		Auth:        specs[0].Auth,
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   fmt.Sprintf("~/.config/%s-pp-cli/config.toml", name),
		},
		Resources: map[string]spec.Resource{},
		Types:     map[string]spec.TypeDef{},
	}

	for i, s := range specs {
		if merged.SpecSource == "" || merged.SpecSource == "official" {
			switch s.SpecSource {
			case "sniffed":
				merged.SpecSource = "sniffed"
			case "community":
				merged.SpecSource = "community"
			}
		}
		if s.SpecSource == "sniffed" {
			merged.SpecSource = "sniffed"
		}
		candidateTransport := s.EffectiveHTTPTransport()
		if s.HTTPTransport != "" || candidateTransport != spec.HTTPTransportStandard || merged.HTTPTransport != "" {
			merged.HTTPTransport = strongerHTTPTransport(merged.HTTPTransport, candidateTransport)
		}

		prefix := perSpecPathPrefix[i]
		for resourceName, resource := range s.Resources {
			if prefix != "" {
				// Same-host/different-path specs are normalized by folding each
				// spec's path prefix into endpoint paths. Do not also preserve
				// the source BaseURL path as a resource override, or generated
				// commands double-prefix nested endpoints.
				resource = prefixResourceEndpointPaths(resource, prefix, s.BaseURL)
			} else {
				resource = resourceWithMergedSpecBaseURL(resource, s.BaseURL, merged.BaseURL)
			}
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

		if mcpConfigured(s.MCP) && !mcpConfigured(merged.MCP) {
			merged.MCP = s.MCP
		}
	}

	return merged
}

func resourceWithMergedSpecBaseURL(resource spec.Resource, sourceBaseURL, mergedBaseURL string) spec.Resource {
	sourceBaseURL = strings.TrimRight(strings.TrimSpace(sourceBaseURL), "/")
	mergedBaseURL = strings.TrimRight(strings.TrimSpace(mergedBaseURL), "/")
	if sourceBaseURL != "" && sourceBaseURL != mergedBaseURL && strings.TrimSpace(resource.BaseURL) == "" {
		resource.BaseURL = sourceBaseURL
	}
	if len(resource.SubResources) > 0 {
		subResources := make(map[string]spec.Resource, len(resource.SubResources))
		for name, sub := range resource.SubResources {
			subResources[name] = resourceWithMergedSpecBaseURL(sub, sourceBaseURL, mergedBaseURL)
		}
		resource.SubResources = subResources
	}
	return resource
}

// planMultiSpecBaseURL decides how to reconcile the BaseURL field across
// multiple input specs. The returned perSpecPathPrefix slice has one entry per
// spec; a non-empty entry tells the caller to prepend that prefix to every
// endpoint path in that spec. When every spec lives on the same scheme+host
// but their path components diverge, the merged BaseURL collapses to the bare
// host and each spec's path component is returned for folding into its
// endpoints — this rescues the "spec A at https://x.com, spec B at
// https://x.com/api/v2" case where the old collapse silently dropped spec B's
// /api/v2 prefix and 404'd every B command. When hosts disagree (a separate,
// out-of-scope multi-host problem) or every spec shares the same BaseURL, the
// merged BaseURL stays specs[0].BaseURL and every prefix is empty.
func planMultiSpecBaseURL(specs []*spec.APISpec) (mergedBaseURL string, perSpecPathPrefix []string) {
	perSpecPathPrefix = make([]string, len(specs))

	hosts := make([]string, len(specs))
	paths := make([]string, len(specs))
	for i, s := range specs {
		hosts[i], paths[i] = splitBaseURL(s.BaseURL)
	}

	commonHost := hosts[0]
	if commonHost == "" {
		return specs[0].BaseURL, perSpecPathPrefix
	}
	for _, h := range hosts[1:] {
		if h != commonHost {
			return specs[0].BaseURL, perSpecPathPrefix
		}
	}

	// All specs share a host. If every spec also shares the same path, no
	// rewriting is needed — the merged BaseURL keeps the shared prefix.
	allSamePath := true
	for _, p := range paths[1:] {
		if p != paths[0] {
			allSamePath = false
			break
		}
	}
	if allSamePath {
		return specs[0].BaseURL, perSpecPathPrefix
	}

	copy(perSpecPathPrefix, paths)
	fmt.Fprintf(os.Stderr, "[multi-spec] base URL host %q shared; folding per-spec path prefixes into endpoint paths\n", commonHost)
	return commonHost, perSpecPathPrefix
}

// splitBaseURL splits an absolute http(s) URL into its scheme+host root and
// its path component. Returns ("", "") for empty or non-absolute inputs so
// callers fall through to the existing "specs[0] wins" behavior. The path
// component is trimmed of its trailing slash so the caller can prepend it to
// an endpoint Path (which already starts with "/") without double slashes.
func splitBaseURL(raw string) (host, path string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", ""
	}
	host = parsed.Scheme + "://" + parsed.Host
	path = strings.TrimRight(parsed.Path, "/")
	return host, path
}

// prefixResourceEndpointPaths returns a copy of resource with prefix prepended
// to every endpoint Path (including sub-resources). Endpoints that already
// declare an absolute BaseURL override are left alone — their path is
// resolved against that override at runtime, not the spec-level BaseURL, so
// folding the prefix in would double-resolve.
func prefixResourceEndpointPaths(resource spec.Resource, prefix, sourceBaseURL string) spec.Resource {
	out := resource
	sourceBaseURL = strings.TrimRight(strings.TrimSpace(sourceBaseURL), "/")
	// The path prefix is being folded into every endpoint path, so any inherited
	// BaseURL for the same spec must be cleared. Keeping both causes generated
	// absolute paths to include the prefix twice. Independent endpoint-level
	// server overrides are preserved.
	if strings.TrimRight(strings.TrimSpace(out.BaseURL), "/") == sourceBaseURL {
		out.BaseURL = ""
	}
	if len(resource.Endpoints) > 0 {
		out.Endpoints = make(map[string]spec.Endpoint, len(resource.Endpoints))
		for name, ep := range resource.Endpoints {
			epBaseURL := strings.TrimRight(strings.TrimSpace(ep.BaseURL), "/")
			if epBaseURL == "" || epBaseURL == sourceBaseURL {
				ep.BaseURL = ""
				ep.Path = prefix + ep.Path
			}
			out.Endpoints[name] = ep
		}
	}
	if len(resource.SubResources) > 0 {
		out.SubResources = make(map[string]spec.Resource, len(resource.SubResources))
		for name, sub := range resource.SubResources {
			out.SubResources[name] = prefixResourceEndpointPaths(sub, prefix, sourceBaseURL)
		}
	}
	return out
}

func strongerHTTPTransport(current, candidate string) string {
	if httpTransportPriority(candidate) > httpTransportPriority(current) {
		return candidate
	}
	return current
}

func httpTransportPriority(value string) int {
	switch value {
	case spec.HTTPTransportBrowserChromeH3:
		return 4
	case spec.HTTPTransportBrowserChrome:
		return 3
	case spec.HTTPTransportBrowserHTTP:
		return 2
	case spec.HTTPTransportStandard:
		return 1
	default:
		return 0
	}
}

// claimOrForce resolves the output directory based on --force and --output flags.
//
//   - force=true:  rename the existing dir to a sibling snapshot (when present), recreate absOut empty for Generate(); the caller is responsible for merging the snapshot back in via regenmerge.MergeIntoFreshTree once Generate() finishes. Returns the snapshot path so the caller can drive the merge.
//   - explicit output (--output set) without force: error if exists and non-empty
//   - default (no --output, no --force): auto-increment via ClaimOutputDir
//
// snapshotDir is non-empty only on the force=true path AND when the prior absOut had content. When non-empty it points to a sibling tempdir holding the pre-regen tree.
func claimOrForce(absOut string, force bool, explicitOutput bool) (resolvedAbsOut, snapshotDir string, err error) {
	if force {
		snapshotDir, err = snapshotForceRegen(absOut)
		if err != nil {
			return "", "", err
		}
		if mkErr := os.MkdirAll(absOut, 0o755); mkErr != nil {
			if snapshotDir == "" {
				return "", "", fmt.Errorf("creating output dir: %w", mkErr)
			}
			if rollbackErr := os.Rename(snapshotDir, absOut); rollbackErr != nil {
				return "", "", fmt.Errorf("creating output dir: %w; snapshot rollback also failed (%v); user must manually move %s back to %s",
					mkErr, rollbackErr, snapshotDir, absOut)
			}
			return "", "", fmt.Errorf("creating output dir: %w", mkErr)
		}
		return absOut, snapshotDir, nil
	}

	if explicitOutput {
		if info, err := os.Stat(absOut); err == nil && info.IsDir() {
			entries, readErr := os.ReadDir(absOut)
			if readErr != nil {
				return "", "", fmt.Errorf("reading output directory: %w", readErr)
			}
			if len(entries) > 0 {
				return "", "", fmt.Errorf("output directory %s already exists (use --force to overwrite)", absOut)
			}
		}
		return absOut, "", nil
	}

	resolved, err := pipeline.ClaimOutputDir(absOut)
	if err != nil {
		return "", "", err
	}
	return resolved, "", nil
}

// finalizeForceMerge runs the post-Generate merge for any --force codepath:
// classifies snapshotDir against freshDir, merges preserved hand-edits back,
// re-runs `go mod tidy` when go.mod was merged (so go.sum keeps up with
// preserved requires), and removes the snapshot on success. On merge
// failure the snapshot is left in place and the error surfaces a recovery
// command.
//
// Wired from the three --force codepaths (--spec, --docs, --plan) so each
// one preserves hand-edits consistently — discarding snapshotDir after
// generation would silently lose user work and leave an orphan that blocks
// future --force runs.
func finalizeForceMerge(snapshotDir, freshDir string, currentSpecBytes []byte) error {
	gomodMerged, err := mergeForceSnapshot(snapshotDir, freshDir, currentSpecBytes)
	if err != nil {
		return &ExitError{Code: ExitGenerationError, Err: err}
	}
	if gomodMerged {
		retidyAfterMerge(freshDir)
	}
	if removeErr := os.RemoveAll(snapshotDir); removeErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove snapshot dir %s: %v\n", snapshotDir, removeErr)
	}
	return nil
}

// mergeForceSnapshot drives the snapshot→fresh merge after Generate() has
// populated absOut. Computes the cross-spec guard by comparing the
// snapshot's recorded spec checksum to a sha256 over the current spec
// bytes. Same checksum (or no recorded checksum) → run the full AST-aware
// merge; mismatch or unreadable manifest → fall back to NOVEL-only
// preservation.
//
// When the merge updates go.mod (snapshot had hand-added requires), the
// caller must re-run `go mod tidy` against freshDir to refresh go.sum —
// validation's tidy ran before merge against fresh's smaller go.mod, so
// hashes for the preserved requires are missing from go.sum until the
// post-merge tidy fills them in. The boolean return reports whether that
// re-tidy is needed.
//
// On failure the snapshot is intentionally left in place; the returned
// error includes the snapshot path so the user can recover manually with
// `rm -rf <freshDir> && mv <snapshotDir> <freshDir>`.
func mergeForceSnapshot(snapshotDir, freshDir string, currentSpecBytes []byte) (gomodMerged bool, err error) {
	report, err := regenmerge.Classify(snapshotDir, freshDir, regenmerge.Options{Force: true})
	if err != nil {
		return false, fmt.Errorf("classifying snapshot vs fresh: %w; snapshot preserved at %s", err, snapshotDir)
	}

	novelOnly := !forceRegenSpecHashMatches(snapshotDir, currentSpecBytes)

	mergeOpts := regenmerge.Options{Force: true, NovelOnly: novelOnly}
	if err := regenmerge.MergeIntoFreshTree(snapshotDir, freshDir, report, mergeOpts); err != nil {
		return false, fmt.Errorf("merging snapshot into fresh tree: %w; snapshot preserved at %s — recover with `rm -rf %s && mv %s %s`",
			err, snapshotDir, freshDir, snapshotDir, freshDir)
	}

	preserved := 0
	for _, fc := range report.Files {
		if fc.Applied {
			preserved++
		}
	}
	injected := 0
	for _, lr := range report.LostRegistrations {
		if lr.Applied {
			injected += len(lr.Calls)
		}
	}
	mode := ""
	if novelOnly {
		mode = " (cross-spec: novel-only preservation)"
	}
	fmt.Fprintf(os.Stderr, "Force regen merged %d preserved files / %d AddCommand calls%s\n", preserved, injected, mode)
	return report.GoMod != nil && report.GoMod.Merged, nil
}

// retidyAfterMerge re-runs `go mod tidy` against dir so go.sum picks up
// hashes for any requires the merge added. Generation's prior tidy ran
// against fresh's go.mod before merge, so any preserved require from the
// snapshot is in go.mod but missing from go.sum until this step fills it
// in. Failure here surfaces as a warning rather than a hard error: the
// merged tree still ships valid sources, and `go mod tidy` is something
// the user can run manually.
func retidyAfterMerge(dir string) {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: post-merge `go mod tidy` failed: %v\n%s", err, out)
	}
}

// forceRegenSpecHashMatches reports whether the snapshot's recorded spec
// checksum matches a sha256 over the current spec bytes. Returns true when:
//   - the snapshot manifest is missing (defensive — old binary or partial
//     state from a CLI generated before SpecChecksum was populated),
//   - the snapshot manifest has an empty SpecChecksum (plan-generated, old
//     format, or docs source without a stored hash),
//   - or the snapshot checksum equals the current spec hash.
//
// Returns false when:
//   - the manifest exists but cannot be decoded (corrupt JSON — treat as
//     unknown lineage and fall back to NOVEL-only preservation),
//   - the snapshot has a checksum but the caller has no current bytes to
//     compare (e.g., a --plan --force run over a spec-generated tree;
//     lineage differs by construction so NOVEL-only is the safe fallback),
//   - or both sides have a checksum and they differ.
//
// The hash matches climanifest.go's storage convention (sha256 over the
// raw input spec bytes, "sha256:" + hex), so a same-spec regen produces a
// byte-identical hash and the full-merge path runs.
func forceRegenSpecHashMatches(snapshotDir string, currentSpecBytes []byte) bool {
	manifestPath := filepath.Join(snapshotDir, pipeline.CLIManifestFilename)
	if _, err := os.Stat(manifestPath); errors.Is(err, os.ErrNotExist) {
		return true
	}
	manifest, err := pipeline.ReadCLIManifest(snapshotDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not decode snapshot manifest at %s: %v; falling back to novel-only preservation\n", manifestPath, err)
		return false
	}
	if manifest.SpecChecksum == "" {
		return true
	}
	if len(currentSpecBytes) == 0 {
		return false
	}
	return manifest.SpecChecksum == currentSpecChecksum(currentSpecBytes)
}

func currentSpecChecksum(specBytes []byte) string {
	sum := sha256.Sum256(specBytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// snapshotForceRegen renames absOut to a sibling tempdir for use as a regen
// recovery path. Returns "" when absOut is missing or empty (nothing to
// snapshot — fresh generation has nothing to preserve).
//
// Symlink-refusal happens BEFORE the rename so a refused regen exits without
// mutating the user's tree — fail before mutating is the load-bearing
// guarantee here.
func snapshotForceRegen(absOut string) (string, error) {
	info, err := os.Lstat(absOut)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("statting output dir for force regen: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to snapshot symlinked output dir: %s", absOut)
	}

	entries, err := os.ReadDir(absOut)
	if err != nil {
		return "", fmt.Errorf("reading output dir for force regen: %w", err)
	}
	if len(entries) == 0 {
		return "", nil
	}

	if err := refuseSymlinksUnderForceRegenTree(absOut); err != nil {
		return "", err
	}

	parent := filepath.Dir(absOut)
	base := filepath.Base(absOut)
	if orphans, err := findExistingPreserveSiblings(parent, base); err != nil {
		return "", err
	} else if len(orphans) > 0 {
		return "", fmt.Errorf("found %d unrecovered snapshot(s) from prior --force run(s) at: %s; recover hand-edits or remove the directories before retrying",
			len(orphans), strings.Join(orphans, ", "))
	}
	snapshot := filepath.Join(parent, base+".preserve-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if _, err := os.Lstat(snapshot); err == nil {
		return "", fmt.Errorf("snapshot path collision: %s already exists", snapshot)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("checking snapshot path %s: %w", snapshot, err)
	}
	if err := os.Rename(absOut, snapshot); err != nil {
		return "", fmt.Errorf("snapshotting output dir to %s: %w", snapshot, err)
	}
	return snapshot, nil
}

// findExistingPreserveSiblings returns absolute paths to any directories of
// the form `<base>.preserve-*` already in parent. These represent
// unrecovered snapshots from previous --force runs that crashed before
// merge cleanup. Continuing past one would orphan the user's hand-edits
// (the new snapshot would be taken from the partial-fresh content of the
// crashed run, not the original source-of-truth).
func findExistingPreserveSiblings(parent, base string) ([]string, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s for prior snapshots: %w", parent, err)
	}
	var orphans []string
	prefix := base + ".preserve-"
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), prefix) {
			orphans = append(orphans, filepath.Join(parent, entry.Name()))
		}
	}
	return orphans, nil
}

// refuseSymlinksUnderForceRegenTree walks the parts of absOut that the
// regenmerge pipeline subsequently reads through (internal/, internal/cli,
// internal/cli/*.go, sibling-package directories) and returns an error if
// any of them are symlinks. The rename in snapshotForceRegen is the
// destructive boundary, so all symlink checks must pass before it.
func refuseSymlinksUnderForceRegenTree(absOut string) error {
	for _, rel := range []string{"internal", filepath.Join("internal", "cli")} {
		path := filepath.Join(absOut, rel)
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("statting %s for force regen symlink check: %w", rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to snapshot symlinked %s: %s", rel, path)
		}
	}

	if err := refuseSymlinkedEntries(filepath.Join(absOut, "internal", "cli"), "internal/cli file"); err != nil {
		return err
	}
	return refuseSymlinkedEntries(filepath.Join(absOut, "internal"), "internal sibling package")
}

// refuseSymlinkedEntries reads dir and returns an error if any direct entry
// is a symlink. A missing dir is not an error (caller may scan paths that
// don't exist on every CLI). label is interpolated into the error message
// to identify which surface refused the symlink.
func refuseSymlinkedEntries(dir, label string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s for symlink check: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to snapshot symlinked %s: %s", label, filepath.Join(dir, entry.Name()))
		}
	}
	return nil
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
				if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
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

// loadResearchSources populates the generator's Sources, DiscoveryPages, and
// NovelFeatures from a pipeline research directory. It returns only dogfood-
// verified novel features in manifest form so publish validation cannot be
// satisfied by planned-but-unbuilt absorb ideas. Silently skips if researchDir
// is empty or data is unavailable.
func loadResearchSources(gen *generator.Generator, researchDir string) []pipeline.NovelFeatureManifest {
	if researchDir == "" {
		return nil
	}
	var manifestNovel []pipeline.NovelFeatureManifest
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
		// Prefer verified (built) novel features over the aspirational list.
		// novel_features_built is written by dogfood after validating which
		// planned features actually survived the build. A nil pointer means
		// dogfood hasn't run yet (fall back to planned). A non-nil pointer
		// to an empty slice means dogfood ran and nothing survived (show nothing).
		var novelSrc []pipeline.NovelFeature
		if research.NovelFeaturesBuilt != nil {
			novelSrc = *research.NovelFeaturesBuilt
		} else {
			novelSrc = research.NovelFeatures
		}
		for _, nf := range novelSrc {
			gen.NovelFeatures = append(gen.NovelFeatures, generator.NovelFeature{
				Name:         nf.Name,
				Command:      nf.Command,
				Description:  nf.Description,
				Rationale:    nf.Rationale,
				Example:      nf.Example,
				WhyItMatters: nf.WhyItMatters,
				Group:        nf.Group,
			})
		}
		if research.NovelFeaturesBuilt != nil {
			for _, nf := range *research.NovelFeaturesBuilt {
				manifestNovel = append(manifestNovel, pipeline.NovelFeatureManifest{
					Name:        nf.Name,
					Command:     nf.Command,
					Description: nf.Description,
				})
			}
		}
		if research.Narrative != nil {
			gen.Narrative = translateNarrative(research.Narrative)
		}
	}
	discoveryDir := filepath.Join(researchDir, "discovery")
	gen.DiscoveryPages = pipeline.ParseDiscoveryPages(discoveryDir)
	return manifestNovel
}

// translateNarrative copies an absorb-phase pipeline.ReadmeNarrative into
// the generator's template-facing struct. Kept as a thin adapter so the
// pipeline package doesn't leak into template data shapes.
func translateNarrative(n *pipeline.ReadmeNarrative) *generator.ReadmeNarrative {
	if n == nil {
		return nil
	}
	out := &generator.ReadmeNarrative{
		DisplayName:    n.DisplayName,
		Headline:       n.Headline,
		ValueProp:      n.ValueProp,
		AuthNarrative:  n.AuthNarrative,
		WhenToUse:      n.WhenToUse,
		TriggerPhrases: append([]string(nil), n.TriggerPhrases...),
	}
	for _, qs := range n.QuickStart {
		out.QuickStart = append(out.QuickStart, generator.QuickStartStep{
			Command: qs.Command,
			Comment: qs.Comment,
		})
	}
	for _, tt := range n.Troubleshoots {
		out.Troubleshoots = append(out.Troubleshoots, generator.TroubleshootTip{
			Symptom: tt.Symptom,
			Fix:     tt.Fix,
		})
	}
	for _, r := range n.Recipes {
		out.Recipes = append(out.Recipes, generator.Recipe{
			Title:       r.Title,
			Command:     r.Command,
			Explanation: r.Explanation,
		})
	}
	return out
}

// enrichSpecFromCatalog looks up the API in the embedded catalog and copies
// generation metadata into the spec if present.
func enrichSpecFromCatalog(apiSpec *spec.APISpec, specRefs ...string) {
	if apiSpec == nil || apiSpec.Name == "" {
		return
	}
	entry := lookupCatalogEntryForGenerateSpec(apiSpec.Name, specRefs)
	if entry == nil {
		return
	}
	enrichSpecFromCatalogEntry(apiSpec, entry)
}

func catalogSpecLookupRefs(specFiles []string, specURL string) []string {
	refs := make([]string, 0, len(specFiles)+1)
	if specURL != "" {
		refs = append(refs, specURL)
	}
	refs = append(refs, specFiles...)
	return refs
}

func lookupCatalogEntryForGenerateSpec(apiName string, specRefs []string) *catalog.Entry {
	if entry, err := catalog.LookupFS(catalogfs.FS, apiName); err == nil {
		return entry
	}
	specURLs := make(map[string]struct{}, len(specRefs))
	for _, ref := range specRefs {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
			specURLs[ref] = struct{}{}
		}
	}
	if len(specURLs) == 0 {
		return nil
	}
	entries, err := catalog.ParseFS(catalogfs.FS)
	if err != nil {
		return nil
	}
	for i := range entries {
		if _, ok := specURLs[entries[i].SpecURL]; ok {
			return &entries[i]
		}
	}
	return nil
}

func enrichSpecFromCatalogEntry(apiSpec *spec.APISpec, entry *catalog.Entry) {
	if apiSpec == nil || entry == nil {
		return
	}
	if len(entry.ProxyRoutes) > 0 && len(apiSpec.ProxyRoutes) == 0 {
		apiSpec.ProxyRoutes = entry.ProxyRoutes
	}
	if entry.Homepage != "" && apiSpec.WebsiteURL == "" {
		apiSpec.WebsiteURL = entry.Homepage
	}
	if entry.Category != "" && apiSpec.Category == "" {
		apiSpec.Category = entry.Category
	}
	if entry.Owner != "" && apiSpec.Owner == "" {
		apiSpec.Owner = entry.Owner
	}
	if entry.OwnerName != "" && apiSpec.OwnerName == "" {
		apiSpec.OwnerName = entry.OwnerName
	}
	if entry.DisplayName != "" && (apiSpec.DisplayName == "" || apiSpec.DisplayNameDerivedFromTitle) {
		apiSpec.DisplayName = entry.DisplayName
		apiSpec.DisplayNameDerivedFromTitle = false
	}
	if entry.HTTPTransport != "" && apiSpec.HTTPTransport == "" {
		apiSpec.HTTPTransport = entry.HTTPTransport
	}
	if mcpConfigured(entry.MCP) && !mcpConfigured(apiSpec.MCP) {
		apiSpec.MCP = entry.MCP
	}
	if entry.BearerRefresh.BundleURL != "" && apiSpec.BearerRefresh.BundleURL == "" {
		apiSpec.BearerRefresh.BundleURL = entry.BearerRefresh.BundleURL
	}
	if entry.BearerRefresh.Pattern != "" && apiSpec.BearerRefresh.Pattern == "" {
		apiSpec.BearerRefresh.Pattern = entry.BearerRefresh.Pattern
	}
	if entry.AuthKeyURL != "" && apiSpec.Auth.Type != "none" {
		apiSpec.Auth.KeyURL = entry.AuthKeyURL
	}
	if entry.AuthInstructions != "" && apiSpec.Auth.Type != "none" {
		apiSpec.Auth.Instructions = entry.AuthInstructions
	}
}

func mcpConfigured(m spec.MCPConfig) bool {
	return len(m.Transport) > 0 ||
		m.Addr != "" ||
		len(m.Intents) > 0 ||
		m.EndpointTools != "" ||
		m.Orchestration != "" ||
		m.OrchestrationThreshold != 0
}

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/categories"
	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/docspec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/googlediscovery"
	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/llm"
	"github.com/mvanhorn/cli-printing-press/v4/internal/llmpolish"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline/regenmerge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/specmeta"
	"github.com/mvanhorn/cli-printing-press/v4/internal/version"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	CanonicalBinaryName = "cli-printing-press"
	LegacyBinaryName    = "printing-press"
)

func Execute() error {
	return ExecuteWithName(CanonicalBinaryName)
}

func ExecuteWithName(commandName string) error {
	// Cancel the command context on interrupt so long-running and hardware-backed
	// subcommands (device-sniff --live, generate, dogfood) shut down gracefully
	// rather than relying on the runtime's default kill.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rootCmd := NewRootCommand(commandName)
	return rootCmd.ExecuteContext(ctx)
}

func NewRootCommand(commandName string) *cobra.Command {
	if commandName == "" {
		commandName = CanonicalBinaryName
	}
	rootCmd := &cobra.Command{
		Use:          commandName,
		Short:        "Describe your API. Get a production CLI.",
		SilenceUsage: true,
		Version:      version.Version,
	}
	rootCmd.SetVersionTemplate(commandName + " {{.Version}}\n")

	rootCmd.AddCommand(newGenerateCmd())
	rootCmd.AddCommand(newScorecardCmd())
	rootCmd.AddCommand(newDogfoodCmd())
	rootCmd.AddCommand(newRegenMergeCmd())
	rootCmd.AddCommand(newValidateNarrativeCmd())
	rootCmd.AddCommand(newVerifyCmd())
	rootCmd.AddCommand(newVerifySkillCmd())
	rootCmd.AddCommand(newVerifyInternalSkillCmd())
	rootCmd.AddCommand(newEmbossCmd())
	rootCmd.AddCommand(newPatchCmd())
	rootCmd.AddCommand(newContributorsCmd())
	rootCmd.AddCommand(newVisionCmd())
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newPrintCmd())
	rootCmd.AddCommand(newBrowserSniffCmd())
	rootCmd.AddCommand(newCrowdSniffCmd())
	rootCmd.AddCommand(newDeviceSniffCmd())
	rootCmd.AddCommand(newBluetoothSniffCmd())
	rootCmd.AddCommand(newLibraryCmd())
	rootCmd.AddCommand(newAuthCmd())
	rootCmd.AddCommand(newPublishCmd())
	rootCmd.AddCommand(newPolishCmd())
	rootCmd.AddCommand(newWorkflowVerifyCmd())
	rootCmd.AddCommand(newApifyActorAuditCmd())
	rootCmd.AddCommand(newShipcheckCmd())
	rootCmd.AddCommand(newLockCmd())
	rootCmd.AddCommand(newMCPAuditCmd())
	rootCmd.AddCommand(newToolsAuditCmd())
	rootCmd.AddCommand(newPublicParamAuditCmd())
	rootCmd.AddCommand(newSyncParamDropCmd())
	rootCmd.AddCommand(newPIIAuditCmd())
	rootCmd.AddCommand(newProbeReachabilityCmd())
	rootCmd.AddCommand(newSchemaCmd())
	rootCmd.AddCommand(newBundleCmd())
	rootCmd.AddCommand(newMCPSyncCmd())

	return rootCmd
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
	var strictRefs bool
	var docsURL string
	var polish bool
	var asJSON bool
	var dryRun bool
	var specSource string
	var category string
	var clientPattern string
	var httpTransport string
	var researchDir string
	var maxEndpointsPerResource int
	var maxResources int
	var specURL string
	var planFile string
	var trafficAnalysisPath string
	var authPreference string
	var namePrefix bool
	var mcpOrchestration string
	var mcpTransport []string
	var mcpEndpointTools string
	var mcpIntentsPath string

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a Go CLI project from an API spec",
		Example: `  # Generate from a local OpenAPI spec
  cli-printing-press generate --spec ./openapi.yaml

  # Generate from a URL and recreate output while preserving hand-authored CLI files
  cli-printing-press generate --spec https://api.example.com/openapi.json --force

  # Generate from API documentation
  cli-printing-press generate --docs https://docs.stripe.com/api --name stripe

  # Multiple specs merged into one CLI
  cli-printing-press generate --spec api-v1.yaml --spec api-v2.yaml --name myapi`,
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
				if err := applyGenerateSpecFlags(parsed, specSource, "docs", category, clientPattern, httpTransport, owner, generateMCPFlagOverrides{
					Orchestration: mcpOrchestration,
					Transport:     mcpTransport,
					EndpointTools: mcpEndpointTools,
					IntentsPath:   mcpIntentsPath,
				}); err != nil {
					return err
				}

				absOut, _, snapshotDir, err := resolveGenerateOutputDir(outputDir, parsed.Name, force, true)
				if err != nil {
					return err
				}

				generateResult, err := runGenerateProject(parsed, absOut, generateProjectOptions{validate: validateBeforeForceMerge(validate, snapshotDir), polish: polish, researchDir: researchDir, trafficAnalysisPath: trafficAnalysisPath})
				if err != nil {
					return err
				}

				if snapshotDir != "" {
					if err := finalizeForceMerge(snapshotDir, absOut, docYAML, validate, generateResult.Validate); err != nil {
						return err
					}
				}

				runID := pipeline.ResolveRunIDFromResearchDir(researchDir)
				if runID == "" {
					fmt.Fprintln(os.Stderr, "warning: could not derive run_id from --research-dir; phase5 dogfood acceptance will refuse to write without it")
				}
				if err := pipeline.WriteManifestForGenerate(pipeline.GenerateManifestParams{
					APIName:       parsed.Name,
					DocsURL:       docsURL,
					OutputDir:     absOut,
					Description:   generateResult.ManifestDescription,
					DisplayName:   generateResult.DisplayName,
					Creator:       parsed.Creator,
					Contributors:  parsed.Contributors,
					Owner:         parsed.Owner,
					Printer:       parsed.Printer,
					PrinterName:   parsed.PrinterName,
					RunID:         runID,
					Spec:          parsed,
					NovelFeatures: generateResult.NovelFeatures,
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
						"polished":   generateResult.Polished,
					}); err != nil {
						return fmt.Errorf("encoding JSON: %w", err)
					}
				}
				return nil
			}

			if planFile != "" {
				if (generateMCPFlagOverrides{Orchestration: mcpOrchestration, Transport: mcpTransport, EndpointTools: mcpEndpointTools, IntentsPath: mcpIntentsPath}).hasAny() {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--mcp-* flags cannot be used with --plan")}
				}
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
				planCommandCount := generator.GeneratedPlanCommandCount(planSpec.Commands)

				absOut, _, snapshotDir, err := resolveGenerateOutputDir(outputDir, planSpec.CLIName, force, !dryRun)
				if err != nil {
					return err
				}
				if dryRun {
					return printPlanDryRun(planSpec, absOut, planFile, planCommandCount)
				}

				if err := generator.GenerateFromPlan(planSpec, absOut); err != nil {
					return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("generating from plan: %w", err)}
				}

				if snapshotDir != "" {
					// Plan-driven generation does not write a manifest with
					// SpecChecksum, so the cross-spec guard naturally lands
					// on the defensive full-merge path. Pass nil so any
					// manifest hash that does exist still gates merge mode.
					// Plan mode has no non-force validation suite to mirror,
					// so force merge should not invent a build-only subset.
					if err := finalizeForceMerge(snapshotDir, absOut, nil, false, nil); err != nil {
						return err
					}
				}

				fmt.Fprintf(os.Stderr, "Generated %s at %s (from plan)\n", naming.CLI(planSpec.CLIName), absOut)
				fmt.Fprintln(os.Stderr, "Notice: plan mode emits a lightweight scaffold, not a full Printing Press CLI. Use spec generation for store, MCP, manifest, and framework commands.")
				if asJSON {
					if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
						"name":       planSpec.CLIName,
						"output_dir": absOut,
						"plan_file":  planFile,
						"commands":   planCommandCount,
					}); err != nil {
						return fmt.Errorf("encoding JSON: %w", err)
					}
				}
				return nil
			}

			if len(specFiles) == 0 {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--spec is required (or use --plan for plan-driven generation)")}
			}

			var singleSpecData []byte
			if len(specFiles) == 1 {
				data, err := readSpec(specFiles[0], refresh, dryRun)
				if err != nil {
					return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("reading spec %s: %w", specFiles[0], err)}
				}
				singleSpecData = data
				if devicespec.LooksLikeDeviceSpec(data) {
					deviceSpec, err := devicespec.ParseBytes(data)
					if err != nil {
						return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("parsing device spec %s: %w", specFiles[0], err)}
					}
					if cliName != "" {
						deviceSpec.Name = cliName
					}
					archivedDeviceSpec, err := archivedDeviceSpecBytes(data, deviceSpec, cliName)
					if err != nil {
						return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("serializing device spec %s: %w", specFiles[0], err)}
					}
					absOut, explicitOutput, snapshotDir, err := resolveGenerateOutputDir(outputDir, deviceSpec.Name, force, !dryRun)
					if err != nil {
						return err
					}
					if dryRun {
						fmt.Fprintf(os.Stdout, "Would generate %s at %s from BLE device spec %s\n", naming.CLI(deviceSpec.Name), absOut, specFiles[0])
						return nil
					}
					generateResult, err := runGenerateDeviceProject(deviceSpec, absOut, generateProjectOptions{validate: validateBeforeForceMerge(validate, snapshotDir), polish: polish})
					if err != nil {
						return err
					}
					if snapshotDir != "" {
						if err := finalizeForceMerge(snapshotDir, absOut, archivedDeviceSpec, validate, generateResult.Validate); err != nil {
							return err
						}
					}
					if !explicitOutput {
						derivedDir := deviceSpec.Name
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
					if err := os.WriteFile(filepath.Join(absOut, "device-spec.yaml"), artifacts.RedactArchivedSpecSecrets(archivedDeviceSpec), 0o644); err != nil {
						fmt.Fprintf(os.Stderr, "warning: could not archive device spec: %v\n", err)
					}
					fmt.Fprintf(os.Stderr, "Generated %s at %s (from BLE device spec)\n", deviceSpec.Name, absOut)
					autoBundleForHost(absOut, os.Stderr)
					if asJSON {
						if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
							"name":       deviceSpec.Name,
							"output_dir": absOut,
							"spec_files": specFiles,
							"validated":  validate,
							"polished":   generateResult.Polished,
							"protocol":   deviceSpec.Protocol,
						}); err != nil {
							return fmt.Errorf("encoding JSON: %w", err)
						}
					}
					return nil
				}
			}

			if maxResources > 0 {
				openapi.SetMaxResources(maxResources)
			}
			if maxEndpointsPerResource > 0 {
				openapi.SetMaxEndpointsPerResource(maxEndpointsPerResource)
			}

			authPreferenceManifestDir := openAPIAuthPreferenceManifestDir(outputDir, cliName, specFiles, researchDir, singleSpecData)
			openAPIParseAuthPref := openAPIAuthPreferenceForGenerate(authPreference, authPreferenceManifestDir)

			var specs []*spec.APISpec
			var specRawBytes [][]byte // raw spec data for archiving
			for i, specFile := range specFiles {
				var data []byte
				var err error
				if i == 0 && len(specFiles) == 1 && singleSpecData != nil {
					data = singleSpecData
				} else {
					data, err = readSpec(specFile, refresh, dryRun)
					if err != nil {
						return &ExitError{Code: ExitSpecError, Err: fmt.Errorf("reading spec %s: %w", specFile, err)}
					}
				}
				specRawBytes = append(specRawBytes, data)

				var apiSpec *spec.APISpec
				if openapi.IsOpenAPI(data) {
					apiSpec, err = parseOpenAPISpec(specFile, data, openapi.ParseOptions{
						Lenient:        lenient,
						StrictRefs:     strictRefs,
						AuthPreference: openAPIParseAuthPref,
					})
				} else if graphql.IsGraphQLSDL(data) {
					apiSpec, err = graphql.ParseSDLBytes(specFile, data)
				} else if googlediscovery.IsDiscovery(data) {
					apiSpec, err = googlediscovery.Parse(specFile, data)
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
				// Override spec-derived name when --name is explicitly provided.
				// When --name is empty but --research-dir points at a state.json
				// whose api_name slug differs from the title-derived name (e.g.
				// "Canvas LMS API" → `canvas-lms` vs the user's intended
				// `canvas`), prefer the state.json slug so the generated
				// cmd/<slug>-pp-cli matches what manifest/publish-validate look
				// for. Explicit --name still wins.
				if cliName != "" {
					specmeta.RebaseAuthEnvPrefix(&apiSpec.Auth, apiSpec.Name, cliName)
					apiSpec.Name = cliName
				} else if researchName := pipeline.LoadAPINameFromResearchDir(researchDir); researchName != "" {
					apiSpec.Name = researchName
				}
			} else {
				if cliName == "" {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--name is required when using multiple specs")}
				}
				apiSpec = mergeSpecsWithOptions(specs, cliName, mergeSpecOptions{NamePrefix: namePrefix})
			}

			if err := applyGenerateSpecFlags(apiSpec, specSource, "", category, clientPattern, httpTransport, owner, generateMCPFlagOverrides{
				Orchestration: mcpOrchestration,
				Transport:     mcpTransport,
				EndpointTools: mcpEndpointTools,
				IntentsPath:   mcpIntentsPath,
			}); err != nil {
				return err
			}
			var reprintContributor spec.Person
			if researchDir != "" {
				reprintContributor = currentGitPerson()
			}
			applyLibraryAttributionForGenerate(apiSpec, reprintContributor)

			absOut, explicitOutput, snapshotDir, err := resolveGenerateOutputDir(outputDir, apiSpec.Name, force, !dryRun)
			if err != nil {
				return err
			}
			if dryRun {
				return printDryRun(apiSpec, absOut, specFiles)
			}

			generateResult, err := runGenerateProject(apiSpec, absOut, generateProjectOptions{validate: validateBeforeForceMerge(validate, snapshotDir), polish: polish, researchDir: researchDir, trafficAnalysisPath: trafficAnalysisPath, specFiles: specFiles, rejectUnshippablePageContextTraffic: true})
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
				if err := finalizeForceMerge(snapshotDir, absOut, primarySpec, validate, generateResult.Validate); err != nil {
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

			archiveBytes, archiveName, archiveOK := archiveSpecBytes(apiSpec, specs, specRawBytes)
			runID := pipeline.ResolveRunIDFromResearchDir(researchDir)
			if runID == "" {
				fmt.Fprintln(os.Stderr, "warning: could not derive run_id from --research-dir; phase5 dogfood acceptance will refuse to write without it")
			}
			if err := pipeline.WriteManifestForGenerate(pipeline.GenerateManifestParams{
				APIName:         apiSpec.Name,
				SpecSrcs:        specFiles,
				SpecArchiveName: archiveName,
				SpecURL:         specURL,
				OutputDir:       absOut,
				Description:     generateResult.ManifestDescription,
				DisplayName:     generateResult.DisplayName,
				Creator:         apiSpec.Creator,
				Contributors:    apiSpec.Contributors,
				Owner:           apiSpec.Owner,
				Printer:         apiSpec.Printer,
				PrinterName:     apiSpec.PrinterName,
				RunID:           runID,
				Spec:            apiSpec,
				AuthPreference:  openAPIParseAuthPref,
				NovelFeatures:   generateResult.NovelFeatures,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write manifest: %v\n", err)
			}

			// Archive a snapshot of the spec alongside the CLI; multi-spec
			// runs use the merged form (see archiveSpecBytes for why).
			if archiveOK {
				data := artifacts.RedactArchivedSpecSecrets(archiveBytes)
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
					"polished":   generateResult.Polished,
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
	cmd.Flags().Bool("allow-novel-wipe", false, "Deprecated compatibility no-op; --force now preserves hand-authored files via regen-merge")
	_ = cmd.Flags().MarkHidden("allow-novel-wipe")
	cmd.Flags().BoolVar(&lenient, "lenient", false, "Skip validation errors from broken $refs in OpenAPI specs")
	cmd.Flags().BoolVar(&strictRefs, "strict-refs", false, "Disable lenient stubbing for missing local schema refs (only meaningful with --lenient)")
	cmd.Flags().StringVar(&docsURL, "docs", "", "API documentation URL to generate spec from")
	cmd.Flags().BoolVar(&polish, "polish", false, "Run LLM polish pass on generated CLI (requires claude or codex CLI)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Parse spec and show what would be generated without writing files (remote specs are still fetched)")
	cmd.Flags().StringVar(&specSource, "spec-source", "", "Spec provenance: official, community, sniffed/browser-sniffed, docs (affects generated client defaults like rate limiting)")
	cmd.Flags().StringVar(&category, "category", "", "Public-library category for generated CLI metadata")
	cmd.Flags().StringVar(&clientPattern, "client-pattern", "", "HTTP client pattern: rest (default), proxy-envelope (wraps requests in POST envelope)")
	cmd.Flags().StringVar(&httpTransport, "transport", "", "HTTP transport: standard, browser-http, browser-chrome, or browser-chrome-h3 (defaults based on spec provenance and reachability)")
	cmd.Flags().StringVar(&mcpOrchestration, "mcp-orchestration", "", "MCP orchestration mode: endpoint-mirror or code")
	cmd.Flags().StringSliceVar(&mcpTransport, "mcp-transport", nil, "MCP transports to compile: stdio, http, or a comma-separated list")
	cmd.Flags().StringVar(&mcpEndpointTools, "mcp-endpoint-tools", "", "MCP endpoint mirror visibility: visible or hidden")
	cmd.Flags().StringVar(&mcpIntentsPath, "mcp-intents", "", "Path to a YAML or JSON file containing MCP intents")
	cmd.Flags().StringVar(&researchDir, "research-dir", "", "Pipeline directory containing research.json and discovery/ for README source credits")
	cmd.Flags().IntVar(&maxResources, "max-resources", 0, "Maximum resource groups to generate (default 500, raise for enormous APIs)")
	cmd.Flags().IntVar(&maxEndpointsPerResource, "max-endpoints-per-resource", 0, "Maximum endpoints per resource (default 50, raise for large APIs)")
	cmd.Flags().StringVar(&specURL, "spec-url", "", "Original spec URL for provenance (use when --spec is a local file downloaded from a URL)")
	cmd.Flags().StringVar(&planFile, "plan", "", "Path to a markdown plan document for plan-driven generation (instead of --spec)")
	cmd.Flags().StringVar(&trafficAnalysisPath, "traffic-analysis", "", "Path to browser-sniff traffic-analysis.json for advisory generation context")
	cmd.Flags().StringVar(&authPreference, "auth-preference", "", "Preferred securityScheme name from the spec (overrides default selection; useful when a spec advertises multiple schemes such as OAuth2 + HTTP Basic and you want the simpler one).")
	cmd.Flags().BoolVar(&namePrefix, "name-prefix", false, "Prefix resource command names with their source spec name when merging multiple specs")

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

func validateBeforeForceMerge(validate bool, snapshotDir string) bool {
	return validate && snapshotDir == ""
}

type generateProjectOptions struct {
	validate                            bool
	polish                              bool
	researchDir                         string
	trafficAnalysisPath                 string
	specFiles                           []string
	rejectUnshippablePageContextTraffic bool
}

type generateProjectResult struct {
	NovelFeatures       []pipeline.NovelFeatureManifest
	ManifestDescription string
	DisplayName         string
	Polished            bool
	Validate            func() error
}

func runGenerateProject(apiSpec *spec.APISpec, absOut string, opts generateProjectOptions) (generateProjectResult, error) {
	if apiSpec != nil {
		applyResearchAuthMetadata(apiSpec, opts.researchDir)
	}
	gen := generator.New(apiSpec, absOut)
	novelFeatures := loadResearchSources(gen, opts.researchDir)
	trafficAnalysis, err := loadTrafficAnalysisForGenerate(opts.trafficAnalysisPath, opts.specFiles, apiSpec.SpecSource)
	if err != nil {
		return generateProjectResult{}, &ExitError{Code: ExitInputError, Err: err}
	}
	if opts.rejectUnshippablePageContextTraffic {
		if err := validateTrafficAnalysisPageContextGate(trafficAnalysis, apiSpec.HTTPTransport); err != nil {
			return generateProjectResult{}, &ExitError{Code: ExitInputError, Err: err}
		}
	}
	// ApplyReachabilityDefaults runs first so its HAR-driven HTTP-version
	// mapping wins for browser_http / browser_clearance_http modes.
	// applyHTTPTransportDefault then fills the cases reachability does
	// not cover (no reachability section, hint-only signals, browser_required)
	// because its own no-op-when-set guard short-circuits in the populated
	// case. The two functions cover disjoint reachability modes, so the
	// short-circuit is the only thing keeping a write-write conflict
	// impossible today; preserve that invariant if either function's
	// mode coverage widens.
	browsersniff.ApplyReachabilityDefaults(apiSpec, trafficAnalysis)
	applyHTTPTransportDefault(apiSpec, trafficAnalysis)
	gen.TrafficAnalysis = trafficAnalysis
	if err := gen.Generate(); err != nil {
		return generateProjectResult{}, &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("generating project: %w", err)}
	}
	manifestDescription := gen.ManifestDescription()
	// Emit tools-manifest.json from the parsed spec so a fresh generate
	// run produces the agent-facing tool description alongside the Go
	// runtime surface. Without this, tools-manifest stays untouched until
	// the first mcp-sync or publish — and any pre-existing tools-manifest
	// (left over from a prior generation under a different spec / parser)
	// silently misrepresents the current MCP tool set. Non-blocking: a
	// warning is the same posture publish takes when this fails.
	if err := pipeline.WriteToolsManifestWithDescription(absOut, apiSpec, manifestDescription); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write tools manifest: %v\n", err)
	}
	validateGeneratedProject := func() error {
		return gen.Validate()
	}
	if opts.validate {
		if err := validateGeneratedProject(); err != nil {
			return generateProjectResult{}, &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("validating generated project: %w", err)}
		}
	}
	return generateProjectResult{
		NovelFeatures:       novelFeatures,
		ManifestDescription: manifestDescription,
		DisplayName:         gen.ManifestDisplayName(),
		Polished:            runGeneratePolishPass(opts.polish, apiSpec.Name, absOut),
		Validate:            validateGeneratedProject,
	}, nil
}

func runGenerateDeviceProject(deviceSpec *devicespec.DeviceSpec, absOut string, opts generateProjectOptions) (generateProjectResult, error) {
	gen := generator.NewDevice(deviceSpec, absOut)
	if err := gen.Generate(); err != nil {
		return generateProjectResult{}, &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("generating device project: %w", err)}
	}
	validateGeneratedProject := func() error {
		return gen.Validate()
	}
	if opts.validate {
		if err := validateGeneratedProject(); err != nil {
			return generateProjectResult{}, &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("validating generated device project: %w", err)}
		}
	}
	return generateProjectResult{
		DisplayName: deviceSpec.DisplayName,
		Polished:    runGeneratePolishPass(opts.polish, deviceSpec.Name, absOut),
		Validate:    validateGeneratedProject,
	}, nil
}

func archivedDeviceSpecBytes(source []byte, deviceSpec *devicespec.DeviceSpec, cliName string) ([]byte, error) {
	if strings.TrimSpace(cliName) == "" {
		return source, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(source, &doc); err != nil {
		return nil, err
	}
	if err := rewriteTopLevelYAMLScalarLine(&source, &doc, "name", deviceSpec.Name); err != nil {
		return nil, err
	}
	return source, nil
}

func rewriteTopLevelYAMLScalarLine(source *[]byte, doc *yaml.Node, key, value string) error {
	if doc == nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("device spec archive must be a YAML mapping")
	}
	mapping := doc.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		valueNode := mapping.Content[i+1]
		if keyNode.Value != key {
			continue
		}
		lines := strings.SplitAfter(string(*source), "\n")
		lineIndex := valueNode.Line - 1
		if lineIndex < 0 || lineIndex >= len(lines) {
			return fmt.Errorf("could not locate YAML field %q line", key)
		}
		line := lines[lineIndex]
		prefixEnd := strings.Index(line, ":")
		if prefixEnd < 0 || strings.TrimSpace(line[:prefixEnd]) != key {
			return fmt.Errorf("could not rewrite YAML field %q without reformatting", key)
		}
		lineEnding := ""
		if strings.HasSuffix(line, "\n") {
			lineEnding = "\n"
			line = strings.TrimSuffix(line, "\n")
		}
		lines[lineIndex] = line[:prefixEnd+1] + " " + value + lineEnding
		*source = []byte(strings.Join(lines, ""))
		return nil
	}
	return fmt.Errorf("device spec archive missing YAML field %q", key)
}

type generateMCPFlagOverrides struct {
	Orchestration string
	Transport     []string
	EndpointTools string
	IntentsPath   string
}

func (o generateMCPFlagOverrides) hasAny() bool {
	return o.Orchestration != "" || len(o.Transport) > 0 || o.EndpointTools != "" || o.IntentsPath != ""
}

func applyGenerateSpecFlags(apiSpec *spec.APISpec, specSource, defaultSpecSource, category, clientPattern, httpTransport, owner string, mcpOverrides generateMCPFlagOverrides) error {
	if specSource != "" {
		normalized, err := normalizeSpecSource(specSource)
		if err != nil {
			return &ExitError{Code: ExitInputError, Err: err}
		}
		apiSpec.SpecSource = normalized
	} else if defaultSpecSource != "" {
		apiSpec.SpecSource = defaultSpecSource
	}
	if category != "" {
		if !categories.IsPublic(category) {
			return &ExitError{
				Code: ExitInputError,
				Err:  fmt.Errorf("--category must be one of: %s", strings.Join(categories.Public(), ", ")),
			}
		}
		apiSpec.Category = category
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
	if err := applyGenerateMCPOverrides(apiSpec, mcpOverrides); err != nil {
		return &ExitError{Code: ExitInputError, Err: err}
	}
	return nil
}

func applyGenerateMCPOverrides(apiSpec *spec.APISpec, overrides generateMCPFlagOverrides) error {
	if apiSpec == nil || !overrides.hasAny() {
		return nil
	}
	if overrides.Orchestration != "" {
		normalized, err := normalizeMCPOrchestration(overrides.Orchestration)
		if err != nil {
			return err
		}
		apiSpec.MCP.Orchestration = normalized
	}
	if len(overrides.Transport) > 0 {
		normalized, err := normalizeMCPTransports(overrides.Transport)
		if err != nil {
			return err
		}
		apiSpec.MCP.Transport = normalized
	}
	if overrides.EndpointTools != "" {
		normalized, err := normalizeMCPEndpointTools(overrides.EndpointTools)
		if err != nil {
			return err
		}
		apiSpec.MCP.EndpointTools = normalized
	}
	if overrides.IntentsPath != "" {
		intents, err := readMCPIntentsFile(overrides.IntentsPath)
		if err != nil {
			return err
		}
		apiSpec.MCP.Intents = intents
	}
	if err := apiSpec.Validate(); err != nil {
		return fmt.Errorf("applying MCP generation flags: %w", err)
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
	case "", spec.HTTPTransportStandard, spec.HTTPTransportBrowserHTTP, spec.HTTPTransportBrowserChrome, spec.HTTPTransportBrowserChromeH2, spec.HTTPTransportBrowserChromeH3:
		return value, nil
	default:
		return "", fmt.Errorf("--transport must be one of: standard, browser-http, browser-chrome, browser-chrome-h2, browser-chrome-h3 (got %q)", value)
	}
}

func normalizeMCPOrchestration(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "endpoint-mirror", "code":
		return strings.TrimSpace(value), nil
	default:
		return "", fmt.Errorf("--mcp-orchestration must be one of: endpoint-mirror, code (got %q)", value)
	}
}

func normalizeMCPEndpointTools(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "visible", "hidden":
		return strings.TrimSpace(value), nil
	default:
		return "", fmt.Errorf("--mcp-endpoint-tools must be one of: visible, hidden (got %q)", value)
	}
}

func normalizeMCPTransports(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		transport := strings.ToLower(strings.TrimSpace(value))
		switch transport {
		case "stdio", "http":
		case "":
			return nil, fmt.Errorf("--mcp-transport values must not be empty")
		default:
			return nil, fmt.Errorf("--mcp-transport must contain only stdio or http (got %q)", value)
		}
		if _, ok := seen[transport]; ok {
			return nil, fmt.Errorf("--mcp-transport contains duplicate value %q", transport)
		}
		seen[transport] = struct{}{}
		normalized = append(normalized, transport)
	}
	return normalized, nil
}

func readMCPIntentsFile(path string) ([]spec.Intent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading --mcp-intents file: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing --mcp-intents file: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("--mcp-intents file must contain either a list of intents or an intents: list")
	}

	root := doc.Content[0]
	switch root.Kind {
	case yaml.SequenceNode:
		var intents []spec.Intent
		if err := root.Decode(&intents); err != nil {
			return nil, fmt.Errorf("parsing --mcp-intents file: %w", err)
		}
		return intents, nil
	case yaml.MappingNode:
		var wrapped struct {
			Intents []spec.Intent `yaml:"intents"`
		}
		if err := root.Decode(&wrapped); err != nil {
			return nil, fmt.Errorf("parsing --mcp-intents file: %w", err)
		}
		if wrapped.Intents != nil {
			return wrapped.Intents, nil
		}
		return nil, fmt.Errorf("--mcp-intents file must contain either a list of intents or an intents: list")
	default:
		return nil, fmt.Errorf("--mcp-intents file must contain either a list of intents or an intents: list")
	}
}

func resolveGenerateOutputDir(outputDir, cliName string, force, claim bool) (resolvedAbsOut string, explicitOutput bool, snapshotDir string, err error) {
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
	if trafficAnalysisReachabilityOverrideMode(analysis) == "standard_http" {
		return
	}
	if trafficAnalysisExplicitlyRecommendsBrowserHTTP3Transport(analysis) {
		apiSpec.HTTPTransport = spec.HTTPTransportBrowserChromeH3
		return
	}
	if trafficAnalysisRecommendsBrowserTransport(analysis) {
		// Surface the implicit H/2 force the pre-template-change else-branch
		// provided. ApplyReachabilityDefaults handles the browser_http /
		// browser_clearance_http modes with HAR-driven precision; everything
		// this branch covers (Cloudflare/DataDome/Akamai protections, html_scrape
		// protocol, generic browser/scrape hints) lacks HAR HTTP-version data,
		// so default to -h2 instead of bare browser-chrome (no force) to keep
		// shipped CLIs on origins these heuristics flag behaving identically.
		apiSpec.HTTPTransport = spec.HTTPTransportBrowserChromeH2
	}
}

func validateTrafficAnalysisPageContextGate(analysis *browsersniff.TrafficAnalysis, httpTransport string) error {
	if !trafficAnalysisRequiresUnshippablePageContext(analysis) {
		return nil
	}
	overrideMode := trafficAnalysisReachabilityOverrideMode(analysis)
	if overrideMode == "" {
		return fmt.Errorf("traffic analysis says this target requires live browser page-context execution; persistent browser transport is not a shippable printed CLI runtime. Re-run discovery for a Surf/direct/browser-clearance replayable surface instead")
	}
	if !trafficAnalysisReachabilityOverrideMatchesTransport(overrideMode, httpTransport) {
		return fmt.Errorf("traffic analysis reachability override %q conflicts with --transport/http_transport %q", overrideMode, httpTransport)
	}
	return nil
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

func trafficAnalysisReachabilityOverrideMode(analysis *browsersniff.TrafficAnalysis) string {
	if analysis == nil {
		return ""
	}
	const prefix = "reachability_override_browser_required_to_"
	for _, hint := range analysis.GenerationHints {
		hint = strings.ToLower(strings.TrimSpace(hint))
		if !strings.HasPrefix(hint, prefix) {
			continue
		}
		mode := strings.TrimSpace(strings.TrimPrefix(hint, prefix))
		switch mode {
		case "browser_http", "browser_clearance_http", "standard_http":
			return mode
		}
	}
	return ""
}

func trafficAnalysisReachabilityOverrideMatchesTransport(mode string, httpTransport string) bool {
	httpTransport = strings.TrimSpace(httpTransport)
	if httpTransport == "" {
		return true
	}
	switch mode {
	case "standard_http":
		return httpTransport == spec.HTTPTransportStandard
	case "browser_http", "browser_clearance_http":
		switch httpTransport {
		case spec.HTTPTransportBrowserHTTP, spec.HTTPTransportBrowserChrome, spec.HTTPTransportBrowserChromeH2, spec.HTTPTransportBrowserChromeH3:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func trafficAnalysisRecommendsBrowserTransport(analysis *browsersniff.TrafficAnalysis) bool {
	if analysis == nil {
		return false
	}
	if trafficAnalysisReachabilityOverrideMode(analysis) == "standard_http" {
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

func parseOpenAPISpec(specFile string, data []byte, opts openapi.ParseOptions) (*spec.APISpec, error) {
	if openapi.IsRemoteSpecSource(specFile) {
		// Remote source: record the URL so the parser can derive an absolute
		// BaseURL when the spec's servers: block is relative-only.
		opts.SourceURL = specFile
	} else {
		opts.Path = specFile
	}
	return openapi.ParseWithOptions(data, opts)
}

// openAPIAuthPreferenceForGenerate resolves AuthPreference for openapi.ParseWithOptions.
// Explicit --auth-preference wins; existing same-directory manifests are the
// durable fallback for reprints.
func openAPIAuthPreferenceForGenerate(cliAuthPref, outputDir string) string {
	if s := strings.TrimSpace(cliAuthPref); s != "" {
		return s
	}
	if strings.TrimSpace(outputDir) == "" {
		return ""
	}
	if manifest, err := pipeline.ReadCLIManifest(outputDir); err == nil {
		return strings.TrimSpace(manifest.AuthPreference)
	}
	return ""
}

func openAPIAuthPreferenceManifestDir(outputDir, cliName string, specFiles []string, researchDir string, singleSpecData []byte) string {
	if strings.TrimSpace(outputDir) != "" {
		return outputDir
	}
	name := strings.TrimSpace(cliName)
	if name == "" {
		name = pipeline.LoadAPINameFromResearchDir(researchDir)
	}
	if name == "" && len(specFiles) == 1 && len(singleSpecData) > 0 && openapi.IsOpenAPI(singleSpecData) {
		if parsed, err := parseOpenAPISpec(specFiles[0], singleSpecData, openapi.ParseOptions{Lenient: true}); err == nil {
			name = parsed.Name
		}
	}
	if name == "" {
		return ""
	}
	return pipeline.DefaultOutputDir(name)
}

// archiveSpecBytes picks the bytes and filename for the spec snapshot that
// generate writes alongside the CLI. Single-spec runs preserve the user's
// original input (post-redaction at the call site) so audit/replay round-trip
// against the same bytes the parser saw. Multi-spec runs serialize the merged
// APISpec — its union of paths, merged title, and merged x-mcp config — so
// downstream consumers that re-read this snapshot operate on the surface the
// generator actually emitted rather than on whichever input happened to be
// passed first.
//
// Returns ok=false when there is nothing to archive (no inputs) or when
// marshalling the merged spec failed; the call site logs and continues so a
// transient archive failure does not abort generation.
func archiveSpecBytes(apiSpec *spec.APISpec, specs []*spec.APISpec, specRawBytes [][]byte) ([]byte, string, bool) {
	if len(specs) > 1 {
		// json.MarshalIndent on a nil pointer succeeds with the literal
		// "null" bytes, which would write a syntactically-valid but
		// useless snapshot. Surface the precondition explicitly.
		if apiSpec == nil {
			return nil, "", false
		}
		data, err := json.MarshalIndent(apiSpec, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not marshal merged spec for archive: %v\n", err)
			return nil, "", false
		}
		return data, "spec.json", true
	}
	if len(specRawBytes) == 0 {
		return nil, "", false
	}
	raw := specRawBytes[0]
	if json.Valid(raw) {
		return raw, "spec.json", true
	}
	return raw, "spec.yaml", true
}

func mergeSpecs(specs []*spec.APISpec, name string) *spec.APISpec {
	return mergeSpecsWithOptions(specs, name, mergeSpecOptions{})
}

type mergeSpecOptions struct {
	NamePrefix bool
}

func mergeSpecsWithOptions(specs []*spec.APISpec, name string, opts mergeSpecOptions) *spec.APISpec {
	if len(specs) == 1 {
		return specs[0]
	}

	mergedBaseURL, perSpecPathPrefix := planMultiSpecBaseURL(specs)
	sharedPathPrefix := sharedMultiSpecEndpointPathPrefix(specs)

	merged := &spec.APISpec{
		Name:            name,
		Description:     "Combined CLI for multiple API services",
		Version:         specs[0].Version,
		BaseURL:         mergedBaseURL,
		BasePath:        specs[0].BasePath,
		Auth:            mergeMultiSpecAuth(specs),
		RequiredHeaders: mergeMultiSpecRequiredHeaders(specs),
		Learn:           mergeMultiSpecLearn(specs),
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   fmt.Sprintf("~/.config/%s-pp-cli/config.toml", name),
		},
		Resources: map[string]spec.Resource{},
		Types:     map[string]spec.TypeDef{},
	}

	seenEndpointRefs := map[string]string{}
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
		resourceRenames := map[string]string{}
		duplicateEndpointRefs := map[string]string{}
		conflictingRequiredHeaders := conflictingMultiSpecRequiredHeaderOverrides(merged.RequiredHeaders, s.RequiredHeaders)
		acceptedResourceKeys := make([]string, 0, len(s.Resources))
		resourceNames := make([]string, 0, len(s.Resources))
		for resourceName := range s.Resources {
			resourceNames = append(resourceNames, resourceName)
		}
		sort.Strings(resourceNames)
		for _, resourceName := range resourceNames {
			resource := s.Resources[resourceName]
			if prefix != "" {
				// Same-host/different-path specs are normalized by folding each
				// spec's path prefix into endpoint paths. Do not also preserve
				// the source BaseURL path as a resource override, or generated
				// commands double-prefix nested endpoints.
				resource = prefixResourceEndpointPaths(resource, prefix, s.BaseURL)
			} else {
				resource = resourceWithMergedSpecBaseURL(resource, s.BaseURL, merged.BaseURL)
			}
			resource = applyRequiredHeaderOverrides(resource, conflictingRequiredHeaders)
			if !opts.NamePrefix && i > 0 {
				var keep bool
				resource, keep = filterDuplicateResourceEndpoints(resource, resourceName, seenEndpointRefs, duplicateEndpointRefs)
				if !keep {
					continue
				}
			}
			key := multiSpecResourceName(s, resourceName, sharedPathPrefix)
			if opts.NamePrefix {
				key = prefixedMultiSpecResourceName(s, resourceName)
			}
			if existing, exists := merged.Resources[key]; exists {
				if !opts.NamePrefix && resourceEndpointsCoveredBy(existing, resource) {
					continue
				}
				key = prefixedMultiSpecResourceName(s, resourceName)
				key = uniqueMultiSpecResourceName(merged.Resources, key)
			}
			resource = rewriteDefaultResourceDescription(resource, resourceName, key)
			if key != resourceName {
				resourceRenames[resourceName] = key
				endpointRefRenames := rekeyResourceEndpointReferences(seenEndpointRefs, resource, resourceName, key)
				for duplicateRef, canonicalRef := range duplicateEndpointRefs {
					if renamed, ok := endpointRefRenames[canonicalRef]; ok {
						duplicateEndpointRefs[duplicateRef] = renamed
					}
				}
			}
			merged.Resources[key] = resource
			acceptedResourceKeys = append(acceptedResourceKeys, key)
		}
		if !opts.NamePrefix {
			for _, key := range acceptedResourceKeys {
				resource := merged.Resources[key]
				addResourceEndpointReferences(seenEndpointRefs, resource, key)
			}
		}

		for typeName, typeDef := range s.Types {
			key := typeName
			if _, exists := merged.Types[key]; exists {
				key = s.Name + "-" + typeName
			}
			merged.Types[key] = typeDef
		}

		if mcpConfigured(s.MCP) && !mcpConfigured(merged.MCP) {
			merged.MCP = rewriteMCPIntentEndpointRefs(s.MCP, resourceRenames, duplicateEndpointRefs)
		}
	}

	return merged
}

func prefixedMultiSpecResourceName(s *spec.APISpec, resourceName string) string {
	specName := strings.Trim(strings.TrimSpace(s.Name), "-")
	resourceName = strings.Trim(strings.TrimSpace(resourceName), "-")
	if specName == "" || resourceName == "" || specName == resourceName || strings.HasPrefix(resourceName, specName+"-") {
		return resourceName
	}
	return specName + "-" + resourceName
}

func uniqueMultiSpecResourceName(resources map[string]spec.Resource, preferred string) string {
	if _, exists := resources[preferred]; !exists {
		return preferred
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", preferred, i)
		if _, exists := resources[candidate]; !exists {
			return candidate
		}
	}
}

func mergeMultiSpecAuth(specs []*spec.APISpec) spec.AuthConfig {
	if len(specs) == 1 {
		return specs[0].Auth
	}

	auth := specs[0].Auth
	authSpecIndex := 0
	if !authConfigHasModel(auth) {
		for i, s := range specs[1:] {
			if s.Auth.AuthorizationURL != "" {
				auth = s.Auth
				authSpecIndex = i + 1
				break
			}
		}
	} else {
		for _, s := range specs[1:] {
			if s.Auth.AuthorizationURL != "" && compatibleMultiSpecAuthModels(auth, s.Auth) {
				fillMissingAuthFields(&auth, s.Auth)
				break
			}
		}
	}
	authOrigin := baseURLOrigin(specs[authSpecIndex].BaseURL)

	scopeSet := make(map[string]struct{}, len(auth.Scopes))
	for _, scope := range auth.Scopes {
		if scope = strings.TrimSpace(scope); scope != "" {
			scopeSet[scope] = struct{}{}
		}
	}
	headers := mergeableAdditionalAuthHeaders(auth.AdditionalHeaders)
	seenHeaders := make(map[string]struct{}, len(headers)+1)
	seenEnvVars := make(map[string]struct{}, len(headers)+len(auth.EnvVarSpecs)+len(auth.EnvVars))
	seedAuthHeaderDedupe(seenHeaders, seenEnvVars, auth, headers)

	for _, s := range specs {
		if compatibleOAuthScopeAuth(auth, s.Auth) {
			for _, scope := range s.Auth.Scopes {
				if scope = strings.TrimSpace(scope); scope != "" {
					scopeSet[scope] = struct{}{}
				}
			}
		}
		headers = appendUniqueAdditionalAuthHeaders(headers, seenHeaders, seenEnvVars, s.Auth, baseURLOrigin(s.BaseURL) == authOrigin)
	}

	auth.Scopes = sortedScopes(scopeSet)
	auth.AdditionalHeaders = headers
	return auth
}

func authConfigHasModel(auth spec.AuthConfig) bool {
	typeName := strings.TrimSpace(auth.Type)
	if typeName != "" && !strings.EqualFold(typeName, "none") {
		return true
	}
	return strings.TrimSpace(auth.Header) != "" ||
		strings.TrimSpace(auth.Format) != "" ||
		len(auth.EnvVars) > 0 ||
		len(auth.EnvVarSpecs) > 0 ||
		auth.HasCookies() ||
		len(auth.AdditionalHeaders) > 0 ||
		strings.TrimSpace(auth.AuthorizationURL) != ""
}

func compatibleMultiSpecAuthModels(base, incoming spec.AuthConfig) bool {
	baseType := strings.ToLower(strings.TrimSpace(base.Type))
	incomingType := strings.ToLower(strings.TrimSpace(incoming.Type))
	if baseType == "" || incomingType == "" ||
		baseType == spec.TierAuthTypeNone || incomingType == spec.TierAuthTypeNone ||
		baseType != incomingType {
		return false
	}
	if base.Header != "" && incoming.Header != "" && !strings.EqualFold(base.Header, incoming.Header) {
		return false
	}
	if base.In != "" && incoming.In != "" && !strings.EqualFold(base.In, incoming.In) {
		return false
	}
	if !compatibleMultiSpecAuthField(base.Subtype, incoming.Subtype) ||
		!compatibleMultiSpecAuthField(base.Scheme, incoming.Scheme) ||
		!compatibleMultiSpecAuthURL(base.AuthorizationURL, incoming.AuthorizationURL) ||
		!compatibleMultiSpecAuthURL(base.DeviceAuthorizationURL, incoming.DeviceAuthorizationURL) ||
		!compatibleMultiSpecAuthURL(base.TokenURL, incoming.TokenURL) ||
		!compatibleMultiSpecAuthField(base.DefaultClientID, incoming.DefaultClientID) ||
		!compatibleMultiSpecAuthField(base.RefreshTokenMechanism, incoming.RefreshTokenMechanism) {
		return false
	}
	if baseType == "oauth2" || strings.TrimSpace(base.OAuth2Grant) != "" || strings.TrimSpace(incoming.OAuth2Grant) != "" {
		if base.EffectiveOAuth2Grant() != incoming.EffectiveOAuth2Grant() {
			return false
		}
	}
	return true
}

func compatibleMultiSpecAuthField(base, incoming string) bool {
	base = strings.TrimSpace(base)
	incoming = strings.TrimSpace(incoming)
	return base == "" || incoming == "" || base == incoming
}

func compatibleMultiSpecAuthURL(base, incoming string) bool {
	base = normalizeAuthURL(base)
	incoming = normalizeAuthURL(incoming)
	return base == "" || incoming == "" || base == incoming
}

func fillMissingAuthFields(dst *spec.AuthConfig, src spec.AuthConfig) {
	if dst == nil {
		return
	}
	if dst.Subtype == "" {
		dst.Subtype = src.Subtype
	}
	if dst.Header == "" {
		dst.Header = src.Header
	}
	if dst.Prefix == "" {
		dst.Prefix = src.Prefix
	}
	if dst.Format == "" {
		dst.Format = src.Format
	}
	if dst.In == "" {
		dst.In = src.In
	}
	if dst.Scheme == "" {
		dst.Scheme = src.Scheme
	}
	if dst.AuthorizationURL == "" {
		dst.AuthorizationURL = src.AuthorizationURL
	}
	if dst.DeviceAuthorizationURL == "" {
		dst.DeviceAuthorizationURL = src.DeviceAuthorizationURL
	}
	if dst.TokenURL == "" {
		dst.TokenURL = src.TokenURL
	}
	if dst.DefaultClientID == "" {
		dst.DefaultClientID = src.DefaultClientID
	}
	if strings.EqualFold(dst.Type, "oauth2") {
		if dst.OAuth2Grant == "" {
			dst.OAuth2Grant = src.OAuth2Grant
		}
		if dst.RefreshTokenMechanism == "" {
			dst.RefreshTokenMechanism = src.RefreshTokenMechanism
		}
	}
}

func mergeMultiSpecRequiredHeaders(specs []*spec.APISpec) []spec.RequiredHeader {
	var merged []spec.RequiredHeader
	seen := map[string]struct{}{}
	for _, s := range specs {
		for _, header := range s.RequiredHeaders {
			key := strings.ToLower(strings.TrimSpace(header.Name))
			if key != "" {
				// Required headers are global on the merged client, so the first
				// value wins when specs declare the same name differently.
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
			}
			merged = append(merged, header)
		}
	}
	return merged
}

func conflictingMultiSpecRequiredHeaderOverrides(existing, incoming []spec.RequiredHeader) map[string]string {
	existingByName := make(map[string]string, len(existing))
	for _, header := range existing {
		name := strings.ToLower(strings.TrimSpace(header.Name))
		if name != "" {
			existingByName[name] = header.Value
		}
	}

	overrides := make(map[string]string)
	for _, header := range incoming {
		name := strings.ToLower(strings.TrimSpace(header.Name))
		if name == "" {
			continue
		}
		if value, exists := existingByName[name]; exists && value != header.Value {
			overrides[header.Name] = header.Value
		}
	}
	return overrides
}

func applyRequiredHeaderOverrides(resource spec.Resource, overrides map[string]string) spec.Resource {
	if len(overrides) == 0 {
		return resource
	}

	headerNames := make([]string, 0, len(overrides))
	for name := range overrides {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)

	for name, endpoint := range resource.Endpoints {
		for _, headerName := range headerNames {
			if hasHeaderOverride(endpoint.HeaderOverrides, headerName) {
				continue
			}
			endpoint.HeaderOverrides = append(endpoint.HeaderOverrides, spec.RequiredHeader{
				Name:  headerName,
				Value: overrides[headerName],
			})
		}
		resource.Endpoints[name] = endpoint
	}
	for name, subResource := range resource.SubResources {
		resource.SubResources[name] = applyRequiredHeaderOverrides(subResource, overrides)
	}
	return resource
}

func hasHeaderOverride(overrides []spec.RequiredHeader, name string) bool {
	for _, override := range overrides {
		if strings.EqualFold(strings.TrimSpace(override.Name), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func mergeMultiSpecLearn(specs []*spec.APISpec) spec.LearnConfig {
	var merged spec.LearnConfig
	controlConfigured := false
	for _, s := range specs {
		learn := s.Learn
		if !learnConfigConfigured(learn) {
			continue
		}
		if !controlConfigured && (learn.Enabled || learn.Disabled || learn.EnabledSet) {
			merged.Enabled = learn.Enabled
			merged.Disabled = learn.Disabled
			merged.EnabledSet = learn.EnabledSet
			controlConfigured = true
		}
		merged.TickerPatterns = appendUniqueStrings(merged.TickerPatterns, learn.TickerPatterns)
		merged.Stopwords = appendUniqueStrings(merged.Stopwords, learn.Stopwords)
		if merged.Synonyms == nil && len(learn.Synonyms) > 0 {
			merged.Synonyms = map[string]string{}
		}
		for key, value := range learn.Synonyms {
			if _, exists := merged.Synonyms[key]; !exists {
				merged.Synonyms[key] = value
			}
		}
		if merged.EntityLookupSeeds == nil && len(learn.EntityLookupSeeds) > 0 {
			merged.EntityLookupSeeds = map[string][]spec.LookupSeed{}
		}
		for kind, seeds := range learn.EntityLookupSeeds {
			for _, seed := range seeds {
				merged.EntityLookupSeeds[kind] = appendUniqueLookupSeed(merged.EntityLookupSeeds[kind], seed)
			}
		}
	}
	return merged
}

func learnConfigConfigured(learn spec.LearnConfig) bool {
	return learn.Enabled || learn.Disabled || learn.EnabledSet ||
		len(learn.TickerPatterns) > 0 || len(learn.Stopwords) > 0 ||
		len(learn.Synonyms) > 0 || len(learn.EntityLookupSeeds) > 0
}

func appendUniqueStrings(dst, src []string) []string {
	seen := make(map[string]struct{}, len(dst)+len(src))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range src {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func appendUniqueLookupSeed(dst []spec.LookupSeed, seed spec.LookupSeed) []spec.LookupSeed {
	for i := range dst {
		if dst[i].Canonical != seed.Canonical {
			continue
		}
		dst[i].Aliases = appendUniqueStrings(dst[i].Aliases, seed.Aliases)
		return dst
	}
	seed.Aliases = appendUniqueStrings(nil, seed.Aliases)
	return append(dst, seed)
}

func seedAuthHeaderDedupe(seenHeaders, seenEnvVars map[string]struct{}, auth spec.AuthConfig, headers []spec.AdditionalAuthHeader) {
	if header := strings.TrimSpace(auth.Header); header != "" {
		seenHeaders[header] = struct{}{}
	}
	for _, envVar := range auth.EnvVarSpecs {
		if name := strings.TrimSpace(envVar.Name); name != "" {
			seenEnvVars[name] = struct{}{}
		}
	}
	for _, name := range auth.EnvVars {
		if name = strings.TrimSpace(name); name != "" {
			seenEnvVars[name] = struct{}{}
		}
	}
	for _, header := range headers {
		if name := strings.TrimSpace(header.Header); name != "" {
			seenHeaders[name] = struct{}{}
		}
		if name := strings.TrimSpace(header.EnvVar.Name); name != "" {
			seenEnvVars[name] = struct{}{}
		}
	}
}

func compatibleOAuthScopeAuth(base, incoming spec.AuthConfig) bool {
	if len(incoming.Scopes) == 0 {
		return false
	}
	if base.Subtype == spec.AuthSubtypeGoogleServiceAccount && incoming.Subtype == spec.AuthSubtypeGoogleServiceAccount {
		if base.Type != incoming.Type || base.EffectiveOAuth2Grant() != incoming.EffectiveOAuth2Grant() {
			return false
		}
		return normalizeAuthURL(base.TokenURL) == normalizeAuthURL(incoming.TokenURL)
	}
	if strings.TrimSpace(base.AuthorizationURL) == "" {
		return false
	}
	if base.Type != incoming.Type || base.EffectiveOAuth2Grant() != incoming.EffectiveOAuth2Grant() {
		return false
	}
	if normalizeAuthURL(base.AuthorizationURL) != normalizeAuthURL(incoming.AuthorizationURL) {
		return false
	}
	if normalizeAuthURL(base.TokenURL) != normalizeAuthURL(incoming.TokenURL) {
		return false
	}
	return strings.TrimSpace(base.RefreshTokenMechanism) == strings.TrimSpace(incoming.RefreshTokenMechanism)
}

func appendUniqueAdditionalAuthHeaders(headers []spec.AdditionalAuthHeader, seenHeaders, seenEnvVars map[string]struct{}, auth spec.AuthConfig, sameOrigin bool) []spec.AdditionalAuthHeader {
	var candidates []spec.AdditionalAuthHeader
	if sameOrigin {
		candidates = append(candidates, auth.AdditionalHeaders...)
		if promoted, ok := additionalHeaderFromAPIKeyAuth(auth); ok {
			candidates = append(candidates, promoted)
		}
	}
	for _, candidate := range candidates {
		if !isMergeableAdditionalAuthHeader(candidate) {
			continue
		}
		header := strings.TrimSpace(candidate.Header)
		envVarName := strings.TrimSpace(candidate.EnvVar.Name)
		if header == "" || envVarName == "" {
			continue
		}
		if _, exists := seenHeaders[header]; exists {
			continue
		}
		if _, exists := seenEnvVars[envVarName]; exists {
			continue
		}
		seenHeaders[header] = struct{}{}
		seenEnvVars[envVarName] = struct{}{}
		headers = append(headers, candidate)
	}
	return headers
}

func mergeableAdditionalAuthHeaders(headers []spec.AdditionalAuthHeader) []spec.AdditionalAuthHeader {
	mergeable := make([]spec.AdditionalAuthHeader, 0, len(headers))
	for _, header := range headers {
		if isMergeableAdditionalAuthHeader(header) {
			mergeable = append(mergeable, header)
		}
	}
	return mergeable
}

func isMergeableAdditionalAuthHeader(header spec.AdditionalAuthHeader) bool {
	return !strings.EqualFold(strings.TrimSpace(header.In), "query")
}

func sortedScopes(scopeSet map[string]struct{}) []string {
	if len(scopeSet) == 0 {
		return nil
	}
	scopes := make([]string, 0, len(scopeSet))
	for scope := range scopeSet {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

func additionalHeaderFromAPIKeyAuth(auth spec.AuthConfig) (spec.AdditionalAuthHeader, bool) {
	if auth.Type != "api_key" || strings.TrimSpace(auth.Header) == "" || !strings.EqualFold(strings.TrimSpace(auth.In), "header") {
		return spec.AdditionalAuthHeader{}, false
	}
	if strings.Contains(strings.ToLower(auth.Format), "basic ") {
		return spec.AdditionalAuthHeader{}, false
	}
	if auth.IsAuthEnvVarORCase() {
		return spec.AdditionalAuthHeader{}, false
	}
	auth.EnvVarSpecs = append([]spec.AuthEnvVar(nil), auth.EnvVarSpecs...)
	auth.NormalizeEnvVarSpecs("")
	var requestCredential spec.AuthEnvVar
	for _, envVar := range auth.EnvVarSpecs {
		if envVar.IsRequestCredential() && strings.TrimSpace(envVar.Name) != "" {
			if strings.TrimSpace(requestCredential.Name) != "" {
				return spec.AdditionalAuthHeader{}, false
			}
			requestCredential = envVar
		}
	}
	if strings.TrimSpace(requestCredential.Name) == "" {
		return spec.AdditionalAuthHeader{}, false
	}
	return spec.AdditionalAuthHeader{
		Header: strings.TrimSpace(auth.Header),
		In:     "header",
		Scheme: auth.Scheme,
		EnvVar: requestCredential,
	}, true
}

func baseURLOrigin(raw string) string {
	host, _ := splitBaseURL(raw)
	return host
}

func normalizeAuthURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String()
}

func rewriteMCPIntentEndpointRefs(mcp spec.MCPConfig, resourceRenames, duplicateEndpointRefs map[string]string) spec.MCPConfig {
	if len(resourceRenames) == 0 && len(duplicateEndpointRefs) == 0 || len(mcp.Intents) == 0 {
		return mcp
	}
	mcp.Intents = append([]spec.Intent(nil), mcp.Intents...)
	for intentIndex := range mcp.Intents {
		intent := mcp.Intents[intentIndex]
		if len(intent.Steps) == 0 {
			continue
		}
		intent.Steps = append([]spec.IntentStep(nil), intent.Steps...)
		for stepIndex := range intent.Steps {
			endpoint := intent.Steps[stepIndex].Endpoint
			if canonical, ok := duplicateEndpointRefs[endpoint]; ok {
				endpoint = canonical
			} else {
				endpoint = rewriteEndpointResourceRef(endpoint, resourceRenames)
			}
			intent.Steps[stepIndex].Endpoint = endpoint
		}
		mcp.Intents[intentIndex] = intent
	}
	return mcp
}

func rewriteEndpointResourceRef(ref string, resourceRenames map[string]string) string {
	resourceName, rest, ok := strings.Cut(ref, ".")
	if !ok {
		return ref
	}
	if renamed, ok := resourceRenames[resourceName]; ok {
		return renamed + "." + rest
	}
	return ref
}

func resourceEndpointsCoveredBy(existing, incoming spec.Resource) bool {
	existingSignatures := resourceEndpointSignatures(existing)
	incomingSignatures := resourceEndpointSignatures(incoming)
	if len(existingSignatures) == 0 || len(incomingSignatures) == 0 {
		return false
	}
	for signature := range incomingSignatures {
		if _, ok := existingSignatures[signature]; !ok {
			return false
		}
	}
	return true
}

func resourceEndpointSignatures(resource spec.Resource) map[string]struct{} {
	signatures := map[string]struct{}{}
	addResourceEndpointSignatures(signatures, resource)
	return signatures
}

func addResourceEndpointSignatures(signatures map[string]struct{}, resource spec.Resource) {
	for _, endpoint := range resource.Endpoints {
		signatures[endpointSignature(resource, endpoint)] = struct{}{}
	}
	for _, sub := range resource.SubResources {
		if sub.BaseURL == "" {
			sub.BaseURL = resource.BaseURL
		}
		addResourceEndpointSignatures(signatures, sub)
	}
}

func addResourceEndpointReferences(references map[string]string, resource spec.Resource, resourceRef string) {
	endpointNames := make([]string, 0, len(resource.Endpoints))
	for name := range resource.Endpoints {
		endpointNames = append(endpointNames, name)
	}
	sort.Strings(endpointNames)
	for _, name := range endpointNames {
		endpoint := resource.Endpoints[name]
		signature := endpointSignature(resource, endpoint)
		if _, exists := references[signature]; !exists {
			references[signature] = resourceRef + "." + name
		}
	}
	subResourceNames := make([]string, 0, len(resource.SubResources))
	for name := range resource.SubResources {
		subResourceNames = append(subResourceNames, name)
	}
	sort.Strings(subResourceNames)
	for _, name := range subResourceNames {
		sub := resource.SubResources[name]
		if sub.BaseURL == "" {
			sub.BaseURL = resource.BaseURL
		}
		addResourceEndpointReferences(references, sub, resourceRef+"."+name)
	}
}

func rekeyResourceEndpointReferences(references map[string]string, resource spec.Resource, oldRef, newRef string) map[string]string {
	if oldRef == newRef {
		return nil
	}
	renamed := make(map[string]string)
	rekeyResourceEndpointReferencesWithBase(references, resource, oldRef, newRef, "", renamed)
	return renamed
}

func rekeyResourceEndpointReferencesWithBase(references map[string]string, resource spec.Resource, oldRef, newRef, inheritedBaseURL string, renamed map[string]string) {
	baseURL := resource.BaseURL
	if baseURL == "" {
		baseURL = inheritedBaseURL
	}
	signatureResource := resource
	signatureResource.BaseURL = baseURL

	endpointNames := make([]string, 0, len(resource.Endpoints))
	for name := range resource.Endpoints {
		endpointNames = append(endpointNames, name)
	}
	sort.Strings(endpointNames)
	for _, name := range endpointNames {
		endpoint := resource.Endpoints[name]
		signature := endpointSignature(signatureResource, endpoint)
		oldEndpointRef := oldRef + "." + name
		if references[signature] == oldEndpointRef {
			newEndpointRef := newRef + "." + name
			references[signature] = newEndpointRef
			renamed[oldEndpointRef] = newEndpointRef
		}
	}

	subResourceNames := make([]string, 0, len(resource.SubResources))
	for name := range resource.SubResources {
		subResourceNames = append(subResourceNames, name)
	}
	sort.Strings(subResourceNames)
	for _, name := range subResourceNames {
		sub := resource.SubResources[name]
		rekeyResourceEndpointReferencesWithBase(references, sub, oldRef+"."+name, newRef+"."+name, baseURL, renamed)
	}
}

func filterDuplicateResourceEndpoints(resource spec.Resource, resourceName string, seen map[string]string, duplicateRefs map[string]string) (spec.Resource, bool) {
	return filterDuplicateResourceEndpointsWithBase(resource, resourceName, "", seen, duplicateRefs)
}

func filterDuplicateResourceEndpointsWithBase(resource spec.Resource, refPrefix, inheritedBaseURL string, seen map[string]string, duplicateRefs map[string]string) (spec.Resource, bool) {
	originalHasEndpointOrOperation := resourceHasEndpointOrOperation(resource)
	baseURL := resource.BaseURL
	if baseURL == "" {
		baseURL = inheritedBaseURL
	}
	signatureResource := resource
	signatureResource.BaseURL = baseURL

	filteredEndpoints := make(map[string]spec.Endpoint, len(resource.Endpoints))
	endpointNames := make([]string, 0, len(resource.Endpoints))
	for name := range resource.Endpoints {
		endpointNames = append(endpointNames, name)
	}
	sort.Strings(endpointNames)
	for _, name := range endpointNames {
		endpoint := resource.Endpoints[name]
		signature := endpointSignature(signatureResource, endpoint)
		if canonical, exists := seen[signature]; exists {
			duplicateRefs[refPrefix+"."+name] = canonical
			continue
		}
		filteredEndpoints[name] = endpoint
		seen[signature] = refPrefix + "." + name
	}
	if len(resource.Endpoints) > 0 {
		resource.Endpoints = filteredEndpoints
	}

	filteredSubResources := make(map[string]spec.Resource, len(resource.SubResources))
	subResourceNames := make([]string, 0, len(resource.SubResources))
	for name := range resource.SubResources {
		subResourceNames = append(subResourceNames, name)
	}
	sort.Strings(subResourceNames)
	for _, name := range subResourceNames {
		subResource := resource.SubResources[name]
		filtered, keep := filterDuplicateResourceEndpointsWithBase(subResource, refPrefix+"."+name, baseURL, seen, duplicateRefs)
		if keep {
			filteredSubResources[name] = filtered
		}
	}
	if len(resource.SubResources) > 0 {
		resource.SubResources = filteredSubResources
	}

	if !originalHasEndpointOrOperation || resourceHasEndpointOrOperation(resource) {
		return resource, true
	}
	return resource, false
}

func resourceHasEndpointOrOperation(resource spec.Resource) bool {
	if len(resource.Endpoints) > 0 || len(resource.Operations) > 0 {
		return true
	}
	for _, subResource := range resource.SubResources {
		if resourceHasEndpointOrOperation(subResource) {
			return true
		}
	}
	return false
}

type endpointParameterSignature struct {
	Name                 string   `json:"name,omitempty"`
	In                   string   `json:"in,omitempty"`
	FlagName             string   `json:"flag_name,omitempty"`
	URLName              string   `json:"url_name,omitempty"`
	BodyName             string   `json:"body_name,omitempty"`
	Type                 string   `json:"type,omitempty"`
	Required             bool     `json:"required,omitempty"`
	Positional           bool     `json:"positional,omitempty"`
	PathParam            bool     `json:"path_param,omitempty"`
	GlobalScope          bool     `json:"global_scope,omitempty"`
	Default              any      `json:"default,omitempty"`
	Enum                 []string `json:"enum,omitempty"`
	Format               string   `json:"format,omitempty"`
	QueryStyle           string   `json:"query_style,omitempty"`
	QueryExplode         *bool    `json:"query_explode,omitempty"`
	ItemType             string   `json:"item_type,omitempty"`
	FieldSelectorDefault string   `json:"field_selector_default,omitempty"`
	Fields               []endpointParameterSignature
}

func endpointParameterShape(param spec.Param) endpointParameterSignature {
	fields := make([]endpointParameterSignature, 0, len(param.Fields))
	for _, field := range param.Fields {
		fields = append(fields, endpointParameterShape(field))
	}
	return endpointParameterSignature{
		Name:                 param.Name,
		In:                   param.In,
		FlagName:             param.FlagName,
		URLName:              param.URLName,
		BodyName:             param.BodyName,
		Type:                 param.Type,
		Required:             param.Required,
		Positional:           param.Positional,
		PathParam:            param.PathParam,
		GlobalScope:          param.GlobalScope,
		Default:              param.Default,
		Enum:                 param.Enum,
		Format:               param.Format,
		QueryStyle:           param.QueryStyle,
		QueryExplode:         param.QueryExplode,
		ItemType:             param.ItemType,
		FieldSelectorDefault: param.FieldSelectorDefault,
		Fields:               fields,
	}
}

func endpointParameterSignatures(params []spec.Param) []string {
	shapes := make([]string, 0, len(params))
	for _, param := range params {
		encoded, err := json.Marshal(endpointParameterShape(param))
		if err != nil {
			encoded = []byte(fmt.Sprintf("%#v", endpointParameterShape(param)))
		}
		shapes = append(shapes, string(encoded))
	}
	sort.Strings(shapes)
	return shapes
}

func endpointSignature(resource spec.Resource, endpoint spec.Endpoint) string {
	baseURL := strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(resource.BaseURL), "/")
	}
	method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
	path := strings.TrimRight(strings.TrimSpace(endpoint.Path), "/")
	shape := struct {
		Mutation           *bool                 `json:"mutation,omitempty"`
		RequestContentType string                `json:"request_content_type,omitempty"`
		BodyJSONFallback   bool                  `json:"body_json_fallback,omitempty"`
		BodyRequired       bool                  `json:"body_required,omitempty"`
		BodyIsArray        bool                  `json:"body_is_array,omitempty"`
		Response           spec.ResponseDef      `json:"response"`
		ResponseFormat     string                `json:"response_format,omitempty"`
		ResponsePath       string                `json:"response_path,omitempty"`
		DataSourceStrategy string                `json:"data_source_strategy,omitempty"`
		Pagination         *spec.Pagination      `json:"pagination,omitempty"`
		HeaderOverrides    []spec.RequiredHeader `json:"header_overrides,omitempty"`
		NoAuth             bool                  `json:"no_auth,omitempty"`
		ObservedAuth       []string              `json:"observed_auth,omitempty"`
		Tier               string                `json:"tier,omitempty"`
		RequiresRole       string                `json:"requires_role,omitempty"`
		Critical           bool                  `json:"critical,omitempty"`
		Syncable           bool                  `json:"syncable,omitempty"`
		Walker             *spec.WalkerConfig    `json:"walker,omitempty"`
		Params             []string              `json:"params,omitempty"`
		Body               []string              `json:"body,omitempty"`
	}{
		Mutation:           endpoint.Mutation,
		RequestContentType: endpoint.RequestContentType,
		BodyJSONFallback:   endpoint.BodyJSONFallback,
		BodyRequired:       endpoint.BodyRequired,
		BodyIsArray:        endpoint.BodyIsArray,
		Response:           endpoint.Response,
		ResponseFormat:     endpoint.ResponseFormat,
		ResponsePath:       endpoint.ResponsePath,
		DataSourceStrategy: endpoint.DataSourceStrategy,
		Pagination:         endpoint.Pagination,
		HeaderOverrides:    endpoint.HeaderOverrides,
		NoAuth:             endpoint.NoAuth,
		ObservedAuth:       endpoint.ObservedAuth,
		Tier:               endpoint.Tier,
		RequiresRole:       endpoint.RequiresRole,
		Critical:           endpoint.Critical,
		Syncable:           endpoint.Syncable,
		Walker:             endpoint.Walker,
		Params:             endpointParameterSignatures(endpoint.Params),
		Body:               endpointParameterSignatures(endpoint.Body),
	}
	encoded, err := json.Marshal(shape)
	if err != nil {
		encoded = []byte(fmt.Sprintf("%#v", shape))
	}
	return method + " " + baseURL + " " + path + " " + string(encoded)
}

func multiSpecResourceName(s *spec.APISpec, resourceName string, sharedPathPrefix []string) string {
	if s == nil || len(s.Resources) != 1 || len(sharedPathPrefix) < 2 {
		return resourceName
	}
	specName := strings.TrimSpace(s.Name)
	if specName == "" || specName == resourceName {
		return resourceName
	}
	if !sharedPrefixContainsResourceName(sharedPathPrefix, resourceName) {
		return resourceName
	}
	return specName
}

func rewriteDefaultResourceDescription(resource spec.Resource, oldName, newName string) spec.Resource {
	if oldName == newName {
		return resource
	}
	if resource.DescriptionDerived {
		resource.Description = spec.DefaultResourceDescription(newName)
	}
	return resource
}

func sharedMultiSpecEndpointPathPrefix(specs []*spec.APISpec) []string {
	if !allSpecsHaveSingleResource(specs) {
		return nil
	}
	var prefix []string
	for _, s := range specs {
		specPathCount := 0
		var stopped bool
		prefix, stopped = foldSharedEndpointPathPrefix(prefix, s.Resources, &specPathCount)
		if stopped {
			return nil
		}
		if specPathCount == 0 {
			return nil
		}
	}
	if len(prefix) < 2 {
		return nil
	}
	return prefix
}

func sharedPrefixContainsResourceName(prefix []string, resourceName string) bool {
	resourceName = normalizePathResourceSegment(resourceName)
	for _, segment := range prefix {
		if normalizePathResourceSegment(segment) == resourceName {
			return true
		}
	}
	return false
}

func allSpecsHaveSingleResource(specs []*spec.APISpec) bool {
	if len(specs) < 2 {
		return false
	}
	for _, s := range specs {
		if s == nil || len(s.Resources) != 1 {
			return false
		}
	}
	return true
}

func foldSharedEndpointPathPrefix(prefix []string, resources map[string]spec.Resource, pathCount *int) ([]string, bool) {
	for _, resource := range resources {
		for _, endpoint := range resource.Endpoints {
			segments := splitEndpointPath(endpoint.Path)
			if len(segments) == 0 {
				continue
			}
			(*pathCount)++
			if prefix == nil {
				prefix = segments
				continue
			}
			prefix = commonEndpointPathPrefix(prefix, segments)
			if len(prefix) < 2 {
				return nil, true
			}
		}
		if len(resource.SubResources) > 0 {
			var stopped bool
			prefix, stopped = foldSharedEndpointPathPrefix(prefix, resource.SubResources, pathCount)
			if stopped {
				return nil, true
			}
		}
	}
	return prefix, false
}

func splitEndpointPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	rawSegments := strings.Split(path, "/")
	segments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

func commonEndpointPathPrefix(a, b []string) []string {
	limit := min(len(a), len(b))
	for i := range limit {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:limit]
}

func normalizePathResourceSegment(value string) string {
	return strings.ReplaceAll(strings.Trim(strings.ToLower(value), "/"), "_", "-")
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
// preserved requires), drops preserved files that reintroduce a
// fresh-generation build break, and removes the snapshot on success. On merge
// failure the snapshot is left in place and the error surfaces a recovery
// command.
//
// Wired from the three --force codepaths (--spec, --docs, --plan) so each
// one preserves hand-edits consistently — discarding snapshotDir after
// generation would silently lose user work and leave an orphan that blocks
// future --force runs.
func finalizeForceMerge(snapshotDir, freshDir string, currentSpecBytes []byte, validate bool, validateMerged func() error) error {
	freshBackup, cleanupFresh, err := backupFreshTree(freshDir)
	if err != nil {
		return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("backing up fresh tree before merge: %w; snapshot preserved at %s", err, snapshotDir)}
	}
	defer cleanupFresh()

	gomodMerged, err := mergeForceSnapshot(snapshotDir, freshDir, currentSpecBytes, false)
	if err != nil {
		return &ExitError{Code: ExitGenerationError, Err: err}
	}
	if gomodMerged {
		retidyAfterMerge(freshDir)
	}
	if err := repairPreserveBuildBreak(snapshotDir, freshDir, freshBackup, currentSpecBytes, validate); err != nil {
		return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("%w; snapshot preserved at %s", err, snapshotDir)}
	}
	if validate {
		if validateMerged == nil {
			return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("validating post-merge generated project: validator unavailable; snapshot preserved at %s", snapshotDir)}
		}
		if err := validateMerged(); err != nil {
			return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("validating post-merge generated project: %w; snapshot preserved at %s", err, snapshotDir)}
		}
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
// preservation and print every skipped TEMPLATED-* hand-edit. Same-spec
// regen still carries those edits; the drop list is the cross-spec
// honesty so the operator sees the loss instead of a mode suffix alone.
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
func mergeForceSnapshot(snapshotDir, freshDir string, currentSpecBytes []byte, forceNovelOnly bool) (gomodMerged bool, err error) {
	novelOnly := forceNovelOnly || !forceRegenSpecHashMatches(snapshotDir, currentSpecBytes)
	baseDir, cleanupBase := synthesizeForceRegenBase(snapshotDir, currentSpecBytes, novelOnly)
	if cleanupBase != nil {
		defer cleanupBase()
	}

	classifyOpts := regenmerge.Options{Force: true, BaseDir: baseDir}
	report, err := regenmerge.Classify(snapshotDir, freshDir, classifyOpts)
	if err != nil {
		return false, fmt.Errorf("classifying snapshot vs fresh: %w; snapshot preserved at %s", err, snapshotDir)
	}

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
	switch {
	case forceNovelOnly:
		mode = " (dropped templated preserves that reintroduced a fresh-generation build break)"
	case novelOnly:
		mode = " (cross-spec: novel-only preservation)"
	}
	fmt.Fprintf(os.Stderr, "Force regen merged %d preserved files / %d AddCommand calls%s\n", preserved, injected, mode)
	if novelOnly {
		if warning := regenmerge.FormatSkippedTemplatedHandEdits(report); warning != "" {
			fmt.Fprint(os.Stderr, warning)
		}
	}
	return report.GoMod != nil && report.GoMod.Merged, nil
}

func synthesizeForceRegenBase(snapshotDir string, currentSpecBytes []byte, novelOnly bool) (string, func()) {
	if novelOnly || len(currentSpecBytes) == 0 {
		return "", nil
	}
	manifest, err := pipeline.ReadCLIManifest(snapshotDir)
	if err != nil {
		return "", nil
	}
	priorVersion := strings.TrimSpace(manifest.PrintingPressVersion)
	if priorVersion == "" || sameSemver(priorVersion, version.Version) {
		return "", nil
	}
	if !validPrintingPressVersion(priorVersion) {
		fmt.Fprintf(os.Stderr, "warning: cannot synthesize force-regen base from invalid printing_press_version %q\n", priorVersion)
		return "", nil
	}

	tmp, err := os.MkdirTemp("", "printing-press-force-base-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot create force-regen base tempdir: %v\n", err)
		return "", nil
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	specPath := filepath.Join(tmp, "spec.yaml")
	baseDir := filepath.Join(tmp, "base")
	if err := os.WriteFile(specPath, currentSpecBytes, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot write force-regen base spec: %v\n", err)
		cleanup()
		return "", nil
	}
	moduleVersion := strings.TrimPrefix(priorVersion, "v")
	moduleVersion = "v" + moduleVersion
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", forceRegenCommandModulePath(moduleVersion)+"@"+moduleVersion,
		"generate", "--spec", specPath, "--output", baseDir, "--validate=false")
	fmt.Fprintf(os.Stderr, "Synthesizing force-regen base with cli-printing-press %s (this may take a moment)...\n", moduleVersion)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		fmt.Fprintf(os.Stderr, "warning: force-regen base synthesis with cli-printing-press %s timed out; falling back to two-way merge\n", moduleVersion)
		cleanup()
		return "", nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: force-regen base synthesis with cli-printing-press %s failed: %v\n%s", moduleVersion, err, out)
		cleanup()
		return "", nil
	}
	return baseDir, cleanup
}

func sameSemver(a, b string) bool {
	return strings.TrimPrefix(strings.TrimSpace(a), "v") == strings.TrimPrefix(strings.TrimSpace(b), "v")
}

var printingPressVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([-.+][0-9A-Za-z.-]+)?$`)

func validPrintingPressVersion(v string) bool {
	return printingPressVersionPattern.MatchString(strings.TrimSpace(v))
}

func forceRegenCommandModulePath(moduleVersion string) string {
	major := printingPressMajor(moduleVersion)
	base := "github.com/mvanhorn/cli-printing-press"
	if major >= 2 {
		base += "/v" + strconv.Itoa(major)
	}
	return base + "/cmd/cli-printing-press"
}

func printingPressMajor(moduleVersion string) int {
	v := strings.TrimPrefix(strings.TrimSpace(moduleVersion), "v")
	majorText, _, _ := strings.Cut(v, ".")
	major, err := strconv.Atoi(majorText)
	if err != nil {
		return 0
	}
	return major
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
// checksum matches the canonical or legacy raw checksum for the current spec
// bytes. Returns true when:
//   - the snapshot manifest is missing (defensive — old binary or partial
//     state from a CLI generated before SpecChecksum was populated),
//   - the snapshot manifest has an empty SpecChecksum (plan-generated, old
//     format, or docs source without a stored hash),
//   - or the snapshot checksum equals the current canonical hash or either
//     line-ending form accepted during the transition window.
//
// Returns false when:
//   - the manifest exists but cannot be decoded (corrupt JSON — treat as
//     unknown lineage and fall back to NOVEL-only preservation),
//   - the snapshot has a checksum but the caller has no current bytes to
//     compare (e.g., a --plan --force run over a spec-generated tree;
//     lineage differs by construction so NOVEL-only is the safe fallback),
//   - or both sides have a checksum and they differ.
//
// The hash matches pipeline.ComputeSpecChecksum's storage convention. Legacy
// all-LF and all-CRLF raw hashes remain accepted for known text spec formats so
// a same-spec cross-platform regen still takes the full-merge path.
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
	return pipeline.SpecChecksumMatches(manifest.SpecChecksum, currentSpecBytes, manifest.SpecFormat)
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
		Example: `  cli-printing-press version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{
					"version": version.Version,
					"go":      runtime.Version(),
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", cmd.Root().Use, version.Version)
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
		Example: `  # Run full pipeline for an API by name
  cli-printing-press print stripe

  # Force overwrite existing pipeline
  cli-printing-press print stripe --force

  # Resume an interrupted pipeline
  cli-printing-press print stripe --resume`,
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

func printPlanDryRun(planSpec *generator.PlanSpec, absOut, planFile string, commandCount int) error {
	fmt.Fprintf(os.Stderr, "Dry run — plan parsed, no files will be generated\n")
	fmt.Fprintf(os.Stderr, "  Plan file:  %s\n", planFile)
	fmt.Fprintf(os.Stderr, "  CLI name:   %s\n", planSpec.CLIName)
	fmt.Fprintf(os.Stderr, "  Output dir: %s\n", absOut)
	fmt.Fprintf(os.Stderr, "  Commands:   %d\n", commandCount)
	fmt.Fprintln(os.Stderr, "  Contract:   lightweight scaffold, not a full Printing Press CLI")

	summary := map[string]any{
		"dry_run":    true,
		"name":       planSpec.CLIName,
		"output_dir": absOut,
		"plan_file":  planFile,
		"commands":   commandCount,
		"contract":   "lightweight scaffold, not a full Printing Press CLI",
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
			manifestNovel = []pipeline.NovelFeatureManifest{}
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
		AntiTriggers:   append([]string(nil), n.AntiTriggers...),
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

func applyResearchAuthMetadata(apiSpec *spec.APISpec, researchDir string) {
	if apiSpec == nil || strings.TrimSpace(researchDir) == "" {
		return
	}
	research, err := pipeline.LoadResearch(researchDir)
	if err != nil {
		return
	}
	envVar := research.CanonicalAuthEnvVar()
	if !isResearchCanonicalEnvVar(envVar) {
		return
	}
	applyCanonicalAuthEnvVar(&apiSpec.Auth, envVar)
}

func isResearchCanonicalEnvVar(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 0 {
			if c < 'A' || c > 'Z' {
				return false
			}
			continue
		}
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

func applyCanonicalAuthEnvVar(auth *spec.AuthConfig, canonical string) {
	if auth == nil || canonical == "" || auth.Type == "" || auth.Type == "none" {
		return
	}
	if strings.Contains(strings.ToLower(auth.Format), "basic ") {
		return
	}
	if len(auth.EnvVars) > 1 {
		return
	}
	if len(auth.EnvVars) > 0 && strings.TrimSpace(auth.EnvVars[0]) == canonical {
		return
	}
	oldEnvVars := append([]string(nil), auth.EnvVars...)
	merged := mergeAuthEnvVarNames([]string{canonical}, auth.EnvVars)
	if len(merged) == 0 {
		return
	}
	normalizeSingleTokenAuthFormatForAliases(auth, oldEnvVars)
	auth.EnvVars = merged
	if len(merged) == 1 {
		auth.EnvVarSpecs = []spec.AuthEnvVar{{
			Name:      merged[0],
			Kind:      spec.AuthEnvVarKindPerCall,
			Required:  true,
			Sensitive: true,
			Inferred:  true,
		}}
		return
	}
	auth.EnvVarSpecs = spec.NewORCaseEnvVarSpecs(merged)
}

func normalizeSingleTokenAuthFormatForAliases(auth *spec.AuthConfig, oldEnvVars []string) {
	if auth == nil || auth.Format == "" || len(oldEnvVars) != 1 {
		return
	}
	oldName := strings.TrimSpace(oldEnvVars[0])
	if oldName == "" {
		return
	}
	oldPlaceholder := naming.EnvVarPlaceholder(oldName)
	if oldPlaceholder != "" {
		auth.Format = strings.ReplaceAll(auth.Format, "{"+oldPlaceholder+"}", "{token}")
	}
	auth.Format = strings.ReplaceAll(auth.Format, "{"+oldName+"}", "{token}")
}

func mergeAuthEnvVarNames(canonical, existing []string) []string {
	seen := make(map[string]struct{}, len(canonical)+len(existing))
	merged := make([]string, 0, len(canonical)+len(existing))
	for _, source := range [][]string{canonical, existing} {
		for _, name := range source {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			merged = append(merged, name)
		}
	}
	return merged
}

func applyLibraryAttributionForGenerate(apiSpec *spec.APISpec, reprintContributor spec.Person) {
	if apiSpec == nil || strings.TrimSpace(apiSpec.Name) == "" {
		return
	}
	manifest, err := pipeline.ReadCLIManifest(filepath.Join(pipeline.PublishedLibraryRoot(), apiSpec.Name))
	if err != nil {
		return
	}
	if manifest.APIName != "" && manifest.APIName != apiSpec.Name {
		return
	}
	if manifest.Creator == nil || manifest.Creator.IsZero() {
		return
	}

	creator := manifest.Creator.Clean()
	apiSpec.Creator = creator
	apiSpec.Owner = manifest.Owner
	if apiSpec.Owner == "" {
		apiSpec.Owner = creator.Handle
	}
	apiSpec.OwnerName = creator.Name
	apiSpec.Printer = manifest.Printer
	if apiSpec.Printer == "" {
		apiSpec.Printer = creator.Handle
	}
	apiSpec.PrinterName = manifest.PrinterName
	if apiSpec.PrinterName == "" {
		apiSpec.PrinterName = creator.Name
	}
	if spec.SamePerson(reprintContributor, creator) {
		apiSpec.Contributors = append([]spec.Person(nil), manifest.Contributors...)
	} else {
		apiSpec.Contributors = spec.PrependContributor(manifest.Contributors, reprintContributor)
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

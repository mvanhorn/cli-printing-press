package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/spf13/cobra"
)

func newPublicParamAuditCmd() *cobra.Command {
	var specFiles []string
	var cliName string
	var lenient bool
	var ledgerPath string
	var asJSON bool
	var strict bool

	cmd := &cobra.Command{
		Use:   "public-param-audit",
		Short: "Inventory cryptic wire parameter names that need public flag names",
		Long: `Parses one or more specs and inventories flag-backed parameters whose
wire names are suspicious public CLI/MCP names, such as one-letter or
punctuation-heavy names. The deterministic inventory is reconciled with an
optional agent-edited ledger. Strict mode fails until every finding has either
a real flag_name in the spec or an evidence-backed skip decision in the ledger.`,
		Example: `  printing-press public-param-audit --spec ./spec.yaml
  printing-press public-param-audit --spec ./spec.yaml --ledger ./public-param-audit.json --strict`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(specFiles) == 0 {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--spec is required")}
			}

			apiSpec, err := parsePublicParamAuditSpec(specFiles, cliName, lenient)
			if err != nil {
				return err
			}

			findings := pipeline.AuditPublicParamNames(apiSpec)
			if ledgerPath != "" {
				previous, err := readPublicParamLedger(ledgerPath)
				if err != nil {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("reading public parameter ledger: %w", err)}
				}
				findings = pipeline.ReconcilePublicParamAuditFindings(findings, previous.Findings)
			}
			ledger := pipeline.NewPublicParamAuditLedger(findings)

			if ledgerPath != "" {
				if err := writePublicParamLedger(ledgerPath, ledger); err != nil {
					return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("writing public parameter ledger: %w", err)}
				}
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(ledger); err != nil {
					return fmt.Errorf("encoding JSON: %w", err)
				}
			} else {
				renderPublicParamAudit(cmd.OutOrStdout(), ledger)
			}

			if strict && ledger.Summary.Pending > 0 {
				return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("public parameter audit has %d pending finding(s)", ledger.Summary.Pending)}
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&specFiles, "spec", nil, "Path or URL to API spec (can be repeated)")
	cmd.Flags().StringVar(&cliName, "name", "", "CLI name (required when using multiple specs)")
	cmd.Flags().BoolVar(&lenient, "lenient", false, "Skip validation errors from broken $refs in OpenAPI specs")
	cmd.Flags().StringVar(&ledgerPath, "ledger", "", "Path to an agent-edited public parameter audit ledger")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a human-readable summary")
	cmd.Flags().BoolVar(&strict, "strict", false, "fail when unresolved findings remain")
	return cmd
}

func parsePublicParamAuditSpec(specFiles []string, cliName string, lenient bool) (*spec.APISpec, error) {
	var specs []*spec.APISpec
	for _, specFile := range specFiles {
		data, err := readSpec(specFile, false, true)
		if err != nil {
			return nil, &ExitError{Code: ExitSpecError, Err: fmt.Errorf("reading spec %s: %w", specFile, err)}
		}

		var apiSpec *spec.APISpec
		if openapi.IsOpenAPI(data) {
			apiSpec, err = parseOpenAPISpec(specFile, data, lenient)
		} else if graphql.IsGraphQLSDL(data) {
			apiSpec, err = graphql.ParseSDLBytes(specFile, data)
		} else {
			apiSpec, err = spec.ParseBytes(data)
		}
		if err != nil {
			return nil, &ExitError{Code: ExitSpecError, Err: fmt.Errorf("parsing spec %s: %w", specFile, err)}
		}
		specs = append(specs, apiSpec)
	}

	if len(specs) == 1 {
		if cliName != "" {
			specs[0].Name = cliName
		}
		return specs[0], nil
	}
	if cliName == "" {
		return nil, &ExitError{Code: ExitInputError, Err: fmt.Errorf("--name is required when using multiple specs")}
	}
	return mergeSpecs(specs, cliName), nil
}

func readPublicParamLedger(path string) (pipeline.PublicParamAuditLedger, error) {
	var ledger pipeline.PublicParamAuditLedger
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ledger, nil
		}
		return ledger, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return ledger, nil
	}
	if err := json.Unmarshal(data, &ledger); err != nil {
		return ledger, err
	}
	return ledger, nil
}

func writePublicParamLedger(path string, ledger pipeline.PublicParamAuditLedger) error {
	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func renderPublicParamAudit(out interface{ Write([]byte) (int, error) }, ledger pipeline.PublicParamAuditLedger) {
	fmt.Fprintf(out, "Public parameter audit: %d pending, %d resolved, %d accepted skip(s), %d total\n",
		ledger.Summary.Pending,
		ledger.Summary.Resolved,
		ledger.Summary.Accepted,
		ledger.Summary.Total,
	)
	for _, finding := range ledger.Findings {
		status := "pending"
		if finding.CurrentPublicName != "" {
			status = "resolved"
		} else if finding.HasAcceptedPublicParamSkip() {
			status = "accepted-skip"
		}
		fmt.Fprintf(out, "- %s %s.%s %s %q", status, finding.Resource, finding.Endpoint, finding.Location, finding.WireName)
		if finding.CurrentPublicName != "" {
			fmt.Fprintf(out, " -> --%s", finding.CurrentPublicName)
		}
		if len(finding.Reasons) > 0 {
			fmt.Fprintf(out, " (%s)", strings.Join(finding.Reasons, ", "))
		}
		if finding.Description != "" {
			fmt.Fprintf(out, ": %s", finding.Description)
		}
		fmt.Fprintln(out)
	}
}

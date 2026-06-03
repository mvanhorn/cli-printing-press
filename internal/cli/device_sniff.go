package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	blesniff "github.com/mvanhorn/cli-printing-press/v4/internal/devicesniff/ble"
	"github.com/mvanhorn/cli-printing-press/v4/internal/devicesniff/bleprobe"
	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type bleSniffCLIOptions struct {
	inputPath          string
	outputPath         string
	analysisOutputPath string
	evidenceOutputPath string
	redactionTerms     []string
	asJSON             bool
}

type deviceSniffSummary struct {
	DeviceSpecPath            string   `json:"device_spec_path"`
	AnalysisPath              string   `json:"analysis_path"`
	EvidencePath              string   `json:"evidence_path"`
	Commands                  int      `json:"commands"`
	Telemetry                 int      `json:"telemetry"`
	RequiresOperatorSelection bool     `json:"requires_operator_selection"`
	Warnings                  []string `json:"warnings,omitempty"`
}

func newDeviceSniffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device-sniff",
		Short: "Analyze captured device traffic to discover controllable capabilities",
	}
	cmd.AddCommand(newDeviceSniffBLECmd("ble", "cli-printing-press device-sniff ble"))
	return cmd
}

func newBluetoothSniffCmd() *cobra.Command {
	return newDeviceSniffBLECmd("bluetooth-sniff", "cli-printing-press bluetooth-sniff")
}

func newDeviceSniffBLECmd(use, commandPath string) *cobra.Command {
	opts := bleSniffCLIOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: "Analyze captured BLE evidence to discover GATT commands and telemetry",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceSniffBLE(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.inputPath, "input", "", "Path to captured BLE evidence JSON")
	cmd.Flags().StringVar(&opts.outputPath, "output", "", "Output path for generated DeviceSpec YAML")
	cmd.Flags().StringVar(&opts.analysisOutputPath, "analysis-output", "", "Output path for BLE analysis report JSON (defaults beside the spec)")
	cmd.Flags().StringVar(&opts.evidenceOutputPath, "evidence-output", "", "Output path for archived BLE evidence JSON; device addresses are pseudonymized and any --redact-term names removed, but captured values are preserved (defaults beside the spec)")
	cmd.Flags().StringArrayVar(&opts.redactionTerms, "redact-term", nil, "Sensitive device-name term to redact from archived BLE evidence; repeat as needed")
	cmd.Flags().BoolVar(&opts.asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("input")
	bleprobe.AddProbeCommands(cmd, commandPath)

	return cmd
}

func runDeviceSniffBLE(cmd *cobra.Command, opts bleSniffCLIOptions) error {
	input, err := loadBLESniffEvidence(opts.inputPath)
	if err != nil {
		return fmt.Errorf("loading BLE evidence: %w", err)
	}
	input.RedactionTerms = append(input.RedactionTerms, opts.redactionTerms...)
	redactedNameTerms := blesniff.HasEffectiveRedactionTerms(input.RedactionTerms)
	input = blesniff.RedactEvidence(input)

	analysis, err := blesniff.AnalyzeEvidence(input)
	if err != nil {
		return fmt.Errorf("analyzing BLE evidence: %w", err)
	}

	outputPath := opts.outputPath
	if outputPath == "" {
		outputPath = defaultDeviceSniffCachePath(analysis.Spec.Name)
	}
	analysisPath := opts.analysisOutputPath
	if analysisPath == "" {
		analysisPath = defaultDeviceSniffAnalysisPath(outputPath)
	}
	evidencePath := opts.evidenceOutputPath
	if evidencePath == "" {
		evidencePath = defaultDeviceSniffEvidencePath(outputPath)
	}
	if err := validateDeviceSniffOutputPaths(outputPath, analysisPath, evidencePath); err != nil {
		return err
	}

	if err := writeDeviceSniffBLEOutputs(analysis.Spec, analysis.Report, input, outputPath, analysisPath, evidencePath); err != nil {
		return err
	}

	summary := deviceSniffSummary{
		DeviceSpecPath:            outputPath,
		AnalysisPath:              analysisPath,
		EvidencePath:              evidencePath,
		Commands:                  len(analysis.Spec.Capabilities.Commands),
		Telemetry:                 len(analysis.Spec.Capabilities.Telemetry),
		RequiresOperatorSelection: analysis.Report.RequiresOperatorSelection,
		Warnings:                  analysis.Report.Warnings,
	}
	if opts.asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(summary)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Device spec written to %s (%d command%s, %d telemetry field%s)\n", outputPath, summary.Commands, plural(summary.Commands), summary.Telemetry, plural(summary.Telemetry))
	fmt.Fprintf(cmd.OutOrStdout(), "BLE analysis written to %s\n", analysisPath)
	redactionNote := "device addresses pseudonymized"
	if redactedNameTerms {
		redactionNote = "device addresses pseudonymized, supplied name terms redacted"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Archived evidence written to %s (%s; captured values preserved)\n", evidencePath, redactionNote)
	if summary.RequiresOperatorSelection {
		fmt.Fprintln(cmd.OutOrStdout(), "Operator selection required before replay: multiple BLE devices matched the capture")
	}
	return nil
}

func loadBLESniffEvidence(path string) (blesniff.EvidenceInput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return blesniff.EvidenceInput{}, err
	}
	return blesniff.ParseEvidence(data)
}

func writeDeviceSniffBLEOutputs(spec *devicespec.DeviceSpec, report blesniff.Report, evidence blesniff.EvidenceInput, specPath string, analysisPath string, evidencePath string) error {
	specBytes, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshaling device spec: %w", err)
	}
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling BLE analysis: %w", err)
	}
	reportBytes = append(reportBytes, '\n')
	evidenceBytes, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling redacted evidence: %w", err)
	}
	evidenceBytes = append(evidenceBytes, '\n')

	writes := []deviceSniffWrite{
		{path: specPath, tempSuffix: "device-spec", data: specBytes, perm: 0o600, label: "device spec"},
		{path: analysisPath, tempSuffix: "ble-analysis", data: reportBytes, perm: 0o600, label: "BLE analysis"},
		{path: evidencePath, tempSuffix: "redacted-evidence", data: evidenceBytes, perm: 0o600, label: "redacted evidence"},
	}
	for i := range writes {
		if err := ensureParentDir(writes[i].path); err != nil {
			return fmt.Errorf("preparing %s directory: %w", writes[i].label, err)
		}
		writes[i].tmpPath = siblingTempPath(writes[i].path, writes[i].tempSuffix)
		defer func(path string) { _ = os.Remove(path) }(writes[i].tmpPath)
		if err := os.WriteFile(writes[i].tmpPath, writes[i].data, writes[i].perm); err != nil {
			return fmt.Errorf("writing %s temp file: %w", writes[i].label, err)
		}
	}

	for i := range writes {
		backup, hadBackup, err := backupFileForReplace(writes[i].path)
		if err != nil {
			for j := i - 1; j >= 0; j-- {
				restoreFileBackup(writes[j].path, writes[j].backupPath, writes[j].hadBackup)
			}
			return fmt.Errorf("preparing %s publish: %w", writes[i].label, err)
		}
		writes[i].backupPath = backup
		writes[i].hadBackup = hadBackup
	}

	defer func() {
		for _, write := range writes {
			_ = os.Remove(write.backupPath)
		}
	}()

	for i := range writes {
		if err := os.Rename(writes[i].tmpPath, writes[i].path); err != nil {
			for j := i - 1; j >= 0; j-- {
				_ = os.Remove(writes[j].path)
			}
			for j := range writes {
				restoreFileBackup(writes[j].path, writes[j].backupPath, writes[j].hadBackup)
			}
			return fmt.Errorf("publishing %s: %w", writes[i].label, err)
		}
	}

	return nil
}

type deviceSniffWrite struct {
	path       string
	tempSuffix string
	tmpPath    string
	backupPath string
	hadBackup  bool
	data       []byte
	perm       os.FileMode
	label      string
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func validateDeviceSniffOutputPaths(paths ...string) error {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolving output path %q: %w", path, err)
		}
		if _, ok := seen[abs]; ok {
			return fmt.Errorf("device-sniff output paths must be distinct: %s", path)
		}
		seen[abs] = struct{}{}
	}
	return nil
}

func defaultDeviceSniffCachePath(name string) string {
	if name == "" {
		name = "device"
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".cache", "printing-press", "device-sniff", name+"-device.yaml")
	}
	return filepath.Join(home, ".cache", "printing-press", "device-sniff", name+"-device.yaml")
}

func defaultDeviceSniffAnalysisPath(specPath string) string {
	return sidecarPath(specPath, "-ble-analysis.json")
}

func defaultDeviceSniffEvidencePath(specPath string) string {
	return sidecarPath(specPath, "-redacted-evidence.json")
}

func sidecarPath(path string, suffix string) string {
	ext := filepath.Ext(path)
	stem := path[:len(path)-len(ext)]
	return stem + suffix
}

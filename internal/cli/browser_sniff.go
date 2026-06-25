package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/spf13/cobra"
)

func newBrowserSniffCmd() *cobra.Command {
	var harPath string
	var outputPath string
	var analysisOutputPath string
	var samplesOutputPath string
	var name string
	var blocklist string
	var include string
	var minSamples int
	var authFrom string
	var preserveHosts bool

	cmd := &cobra.Command{
		Use:   "browser-sniff",
		Short: "Analyze captured web traffic to discover API endpoints and generate a spec",
		RunE: func(cmd *cobra.Command, args []string) error {
			browsersniff.SetAdditionalBlocklist(splitCSV(blocklist))
			browsersniff.SetAdditionalIncludeList(splitCSV(include))

			capture, err := browsersniff.LoadCapture(harPath)
			if err != nil {
				return fmt.Errorf("loading capture: %w", err)
			}
			if len(capture.Entries) == 0 {
				return fmt.Errorf("capture %s contains no entries; capture traffic again or choose a docs-only/spec path", harPath)
			}

			if authFrom != "" {
				authCapture, err := browsersniff.ParseEnriched(authFrom)
				if err != nil {
					return fmt.Errorf("reading auth capture: %w", err)
				}
				if err := validateAuthDomainBinding(authCapture, capture); err != nil {
					return err
				}
				capture.Auth = authCapture.Auth
			}

			analyzeOptions := browsersniff.AnalyzeOptions{PreserveHosts: preserveHosts}
			apiSpec, err := browsersniff.AnalyzeCaptureWithOptions(capture, analyzeOptions)
			if err != nil {
				return fmt.Errorf("analyzing capture: %w", err)
			}

			if name != "" {
				apiSpec.Name = name
				apiSpec.Config.Path = fmt.Sprintf("~/.config/%s-pp-cli/config.toml", name)
			}

			if outputPath == "" {
				outputPath = browsersniff.DefaultCachePath(apiSpec.Name)
			}
			if analysisOutputPath == "" {
				analysisOutputPath = browsersniff.DefaultTrafficAnalysisPath(outputPath)
			}
			if samplesOutputPath == "" {
				samplesOutputPath = browsersniff.DefaultSamplesPath(outputPath)
			}

			trafficAnalysis, err := browsersniff.AnalyzeTraffic(capture)
			if err != nil {
				return fmt.Errorf("analyzing traffic: %w", err)
			}
			browsersniff.ApplyReachabilityDefaults(apiSpec, trafficAnalysis)

			droppedEndpoints := browsersniff.FilterEndpointsByMinSamplesWithOptions(apiSpec, capture, minSamples, analyzeOptions)

			samplesWritten, err := writeBrowserSniffOutputs(apiSpec, trafficAnalysis, capture, outputPath, analysisOutputPath, samplesOutputPath, analyzeOptions)
			if err != nil {
				return err
			}

			endpoints := 0
			for _, resource := range apiSpec.Resources {
				endpoints += len(resource.Endpoints)
			}

			fmt.Printf("Spec written to %s (%d endpoints across %d resources)\n", outputPath, endpoints, len(apiSpec.Resources))
			fmt.Printf("Traffic analysis written to %s\n", analysisOutputPath)
			if samplesOutputPath != "" && samplesWritten > 0 {
				fmt.Printf("Samples written to %s (%d endpoint%s)\n", samplesOutputPath, samplesWritten, plural(samplesWritten))
			}
			if droppedEndpoints > 0 {
				fmt.Printf("Dropped %d endpoint%s below --min-samples=%d (still visible in %s)\n", droppedEndpoints, plural(droppedEndpoints), minSamples, analysisOutputPath)
			}
			fmt.Printf("Run 'cli-printing-press generate --spec %s' to build the CLI\n", outputPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&harPath, "har", "", "Path to HAR or enriched capture file")
	cmd.Flags().StringVar(&outputPath, "output", "", "Output path for generated spec YAML")
	cmd.Flags().StringVar(&analysisOutputPath, "analysis-output", "", "Output path for traffic analysis JSON (defaults beside the spec)")
	cmd.Flags().StringVar(&samplesOutputPath, "samples-output", "", "Output directory for per-endpoint redacted samples (defaults to <spec-stem>-samples beside the spec; pass empty string to disable via --samples-output=\"\")")
	cmd.Flags().StringVar(&name, "name", "", "Override the auto-detected API name")
	cmd.Flags().StringVar(&blocklist, "blocklist", "", "Comma-separated additional hostnames to filter (extends the default analytics/telemetry blocklist)")
	cmd.Flags().StringVar(&include, "include", "", "Comma-separated host or path substrings to rescue from default filtering; matches win over --blocklist and the static-asset suffix demotion")
	cmd.Flags().BoolVar(&preserveHosts, "preserve-hosts", false, "Keep secondary API hosts in the generated spec with per-endpoint base_url overrides instead of selecting only the dominant host")
	cmd.Flags().IntVar(&minSamples, "min-samples", 1, "Drop endpoints with fewer than N paired samples from the emitted spec; the dropped endpoints remain in the traffic-analysis sidecar for audit. Default 1 leaves behavior unchanged; 2+ is recommended for production capture")
	cmd.Flags().StringVar(&authFrom, "auth-from", "", "Path to an enriched capture file to import auth from")
	_ = cmd.MarkFlagRequired("har")

	return cmd
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func writeBrowserSniffOutputs(apiSpec *spec.APISpec, trafficAnalysis *browsersniff.TrafficAnalysis, capture *browsersniff.EnrichedCapture, outputPath string, analysisOutputPath string, samplesOutputPath string, analyzeOptions browsersniff.AnalyzeOptions) (int, error) {
	specTmp := siblingTempPath(outputPath, "spec")
	analysisTmp := siblingTempPath(analysisOutputPath, "traffic-analysis")
	defer func() { _ = os.Remove(specTmp) }()
	defer func() { _ = os.Remove(analysisTmp) }()

	if err := browsersniff.WriteSpec(apiSpec, specTmp); err != nil {
		return 0, fmt.Errorf("writing spec: %w", err)
	}
	if err := browsersniff.WriteTrafficAnalysis(trafficAnalysis, analysisTmp); err != nil {
		return 0, fmt.Errorf("writing traffic analysis: %w", err)
	}

	samplesWritten := 0
	samplesTmp := ""
	if samplesOutputPath != "" && capture != nil {
		samplesTmp = samplesOutputPath + ".tmp"
		_ = os.RemoveAll(samplesTmp)
		if err := os.MkdirAll(samplesTmp, 0o755); err != nil {
			return 0, fmt.Errorf("preparing samples temp dir: %w", err)
		}
		written, err := browsersniff.WriteSamplesWithOptions(capture, samplesTmp, analyzeOptions)
		if err != nil {
			_ = os.RemoveAll(samplesTmp)
			return 0, fmt.Errorf("writing samples: %w", err)
		}
		samplesWritten = written
	}
	defer func() {
		if samplesTmp != "" {
			_ = os.RemoveAll(samplesTmp)
		}
	}()

	analysisBackup, analysisHadBackup, err := backupFileForReplace(analysisOutputPath)
	if err != nil {
		return 0, fmt.Errorf("preparing traffic analysis publish: %w", err)
	}
	specBackup, specHadBackup, err := backupFileForReplace(outputPath)
	if err != nil {
		restoreFileBackup(analysisOutputPath, analysisBackup, analysisHadBackup)
		return 0, fmt.Errorf("preparing spec publish: %w", err)
	}
	samplesBackup := ""
	samplesHadBackup := false
	if samplesTmp != "" {
		samplesBackup, samplesHadBackup, err = backupDirForReplace(samplesOutputPath)
		if err != nil {
			restoreFileBackup(analysisOutputPath, analysisBackup, analysisHadBackup)
			restoreFileBackup(outputPath, specBackup, specHadBackup)
			return 0, fmt.Errorf("preparing samples publish: %w", err)
		}
	}
	cleanupBackups := true
	defer func() {
		if cleanupBackups {
			_ = os.Remove(analysisBackup)
			_ = os.Remove(specBackup)
			if samplesBackup != "" {
				_ = os.RemoveAll(samplesBackup)
			}
		}
	}()

	if err := os.Rename(analysisTmp, analysisOutputPath); err != nil {
		restoreFileBackup(analysisOutputPath, analysisBackup, analysisHadBackup)
		restoreFileBackup(outputPath, specBackup, specHadBackup)
		restoreDirBackup(samplesOutputPath, samplesBackup, samplesHadBackup)
		return 0, fmt.Errorf("publishing traffic analysis: %w", err)
	}
	if err := os.Rename(specTmp, outputPath); err != nil {
		_ = os.Remove(analysisOutputPath)
		restoreFileBackup(analysisOutputPath, analysisBackup, analysisHadBackup)
		restoreFileBackup(outputPath, specBackup, specHadBackup)
		restoreDirBackup(samplesOutputPath, samplesBackup, samplesHadBackup)
		return 0, fmt.Errorf("publishing spec: %w", err)
	}
	if samplesTmp != "" {
		if err := os.Rename(samplesTmp, samplesOutputPath); err != nil {
			_ = os.Remove(analysisOutputPath)
			_ = os.Remove(outputPath)
			restoreFileBackup(analysisOutputPath, analysisBackup, analysisHadBackup)
			restoreFileBackup(outputPath, specBackup, specHadBackup)
			restoreDirBackup(samplesOutputPath, samplesBackup, samplesHadBackup)
			return 0, fmt.Errorf("publishing samples: %w", err)
		}
	}

	return samplesWritten, nil
}

func backupDirForReplace(path string) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	if err == nil && !info.IsDir() {
		return "", false, fmt.Errorf("%s is not a directory", path)
	}

	backup := path + ".backup"
	_ = os.RemoveAll(backup)
	if err := os.Rename(path, backup); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return backup, false, nil
		}
		return backup, false, err
	}
	return backup, true, nil
}

func restoreDirBackup(path string, backup string, hadBackup bool) {
	if backup == "" {
		return
	}
	_ = os.RemoveAll(path)
	if hadBackup {
		_ = os.Rename(backup, path)
		return
	}
	_ = os.RemoveAll(backup)
}

func siblingTempPath(path string, suffix string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+suffix+".tmp")
}

func backupFileForReplace(path string) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	if err == nil && info.IsDir() {
		return "", false, fmt.Errorf("%s is a directory", path)
	}

	backup := siblingTempPath(path, "backup")
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return backup, false, nil
		}
		return backup, false, err
	}
	return backup, true, nil
}

func restoreFileBackup(path string, backup string, hadBackup bool) {
	_ = os.Remove(path)
	if hadBackup {
		_ = os.Rename(backup, path)
		return
	}
	_ = os.Remove(backup)
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}

	return out
}

func validateAuthDomainBinding(authCapture *browsersniff.EnrichedCapture, targetCapture *browsersniff.EnrichedCapture) error {
	if authCapture == nil || authCapture.Auth == nil || strings.TrimSpace(authCapture.Auth.BoundDomain) == "" {
		return nil
	}

	targetDomain := captureDomain(targetCapture)
	boundDomain := normalizeDomain(authCapture.Auth.BoundDomain)
	if targetDomain == "" || boundDomain == "" {
		return nil
	}
	if targetDomain == boundDomain || strings.HasSuffix(targetDomain, "."+boundDomain) {
		return nil
	}

	return fmt.Errorf("auth captured for %s cannot be used with %s (domain mismatch)", authCapture.Auth.BoundDomain, targetDomain)
}

func captureDomain(capture *browsersniff.EnrichedCapture) string {
	if capture == nil {
		return ""
	}

	if capture.TargetURL != "" {
		parsed, err := url.Parse(capture.TargetURL)
		if err == nil && parsed.Hostname() != "" {
			return normalizeDomain(parsed.Hostname())
		}
	}

	baseURL := commonCaptureBaseURL(capture)
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}

	return normalizeDomain(parsed.Hostname())
}

func commonCaptureBaseURL(capture *browsersniff.EnrichedCapture) string {
	counts := make(map[string]int)
	best := ""
	bestCount := 0

	for _, entry := range capture.Entries {
		parsed, err := url.Parse(entry.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}

		base := parsed.Scheme + "://" + parsed.Host
		counts[base]++
		if counts[base] > bestCount {
			best = base
			bestCount = counts[base]
		}
	}

	return best
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

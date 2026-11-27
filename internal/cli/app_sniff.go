package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/appsniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/spf13/cobra"
)

type appSniffOptions struct {
	runner appsniff.CommandRunner
	stdout io.Writer
	stderr io.Writer
}

func newAppSniffCmd() *cobra.Command {
	return newAppSniffCmdWithOptions(appSniffOptions{})
}

func newAppSniffCmdWithOptions(opts appSniffOptions) *cobra.Command {
	var (
		appPath            string
		cachePath          string
		noCache            bool
		androidPath        string
		apkeepPkg          string
		adbPkg             string
		iosPath            string
		simulatorBundleID  string
		harPath            string
		name               string
		baseURL            string
		hostFilter         string
		top                int
		includeGRPC        bool
		auditCLIDir        string
		outputPath         string
		analysisOutputPath string
		manifestOutputPath string
		asJSON             bool
	)

	cmd := &cobra.Command{
		Use:   "app-sniff",
		Short: "Discover API endpoints from desktop (.app/.asar/CacheStorage) and mobile (.apk/.xapk/.ipa) binaries",
		Long: `Discover undocumented API endpoints statically from local desktop and mobile
application binaries without live browser automation:

  - macOS / Electron apps (.app, app.asar, unpacked JS) + Chromium CacheStorage
  - Android packages (.apk, .xapk split bundles, .aab, or via --apkeep / --adb)
    with pure-Go DEX runtime Retrofit annotation parsing, resources.arsc base-URL
    extraction, Hermes bytecode strings, Flutter AOT, and TLS pinning detection
  - iOS bundles (.ipa, simulator .app via --simulator) with FairPlay DRM detection

Produces a Printing Press spec YAML and schema-valid traffic-analysis.json
compatible with 'cli-printing-press generate'. Optionally reconciles against a
HAR capture (--har) or audits an existing printed CLI for drift (--audit-cli).`,
		Example: `  cli-printing-press app-sniff --app /Applications/MyApp.app --output my-app-spec.yaml
  cli-printing-press app-sniff --android ./app.xapk --output mobile-spec.yaml
  cli-printing-press app-sniff --apkeep com.example.app --output mobile-spec.yaml
  cli-printing-press app-sniff --app /Applications/MyApp.app --audit-cli ~/printing-press/library/my-app --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppSniff(cmd.Context(), appSniffRunArgs{
				appPath:            appPath,
				cachePath:          cachePath,
				noCache:            noCache,
				androidPath:        androidPath,
				apkeepPkg:          apkeepPkg,
				adbPkg:             adbPkg,
				iosPath:            iosPath,
				simulatorBundleID:  simulatorBundleID,
				harPath:            harPath,
				name:               name,
				baseURL:            baseURL,
				hostFilter:         hostFilter,
				top:                top,
				includeGRPC:        includeGRPC,
				auditCLIDir:        auditCLIDir,
				outputPath:         outputPath,
				analysisOutputPath: analysisOutputPath,
				manifestOutputPath: manifestOutputPath,
				asJSON:             asJSON,
			}, opts)
		},
	}

	cmd.Flags().StringVar(&appPath, "app", "", "Path to a macOS .app bundle, app.asar archive, or unpacked JS/source directory")
	cmd.Flags().StringVar(&cachePath, "cache", "", "Path to a Chromium/Electron Service Worker CacheStorage directory")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Skip automatic macOS CacheStorage scanning alongside --app")
	cmd.Flags().StringVar(&androidPath, "android", "", "Path to an Android .apk, .xapk, .aab, or unpacked directory")
	cmd.Flags().StringVar(&apkeepPkg, "apkeep", "", "Download an Android .xapk/.apk via apkeep (reverse-DNS package ID) into a temp directory and analyze it")
	cmd.Flags().StringVar(&adbPkg, "adb", "", "Pull an installed Android package's APK splits from a connected ADB device/emulator and analyze them")
	cmd.Flags().StringVar(&iosPath, "ios", "", "Path to an iOS .ipa archive or unpacked .app bundle")
	cmd.Flags().StringVar(&simulatorBundleID, "simulator", "", "Inspect an unencrypted .app bundle from the booted iOS Simulator via xcrun simctl")
	cmd.Flags().StringVar(&harPath, "har", "", "Optional HAR or enriched capture file to reconcile static endpoints against observed traffic")
	cmd.Flags().StringVar(&name, "name", "", "Override the auto-detected API name")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Override the auto-detected primary HTTPS base URL")
	cmd.Flags().StringVar(&hostFilter, "host", "", "Comma-separated host substrings to retain")
	cmd.Flags().IntVar(&top, "top", 0, "Cap emitted REST endpoints to top N ranked routes (0 keeps all)")
	cmd.Flags().BoolVar(&includeGRPC, "include-grpc", false, "Include Protobuf/gRPC service paths in the generated spec (default excludes them from REST specs)")
	cmd.Flags().StringVar(&auditCLIDir, "audit-cli", "", "Audit an existing printed CLI directory against discovered endpoints, version, shards, and MCP parity")
	cmd.Flags().StringVar(&outputPath, "output", "", "Output path for generated spec YAML")
	cmd.Flags().StringVar(&analysisOutputPath, "analysis-output", "", "Output path for traffic-analysis.json (defaults beside the spec)")
	cmd.Flags().StringVar(&manifestOutputPath, "manifest-output", "", "Optional output path for full redacted app-sniff JSON manifest")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output structured JSON summary to stdout")

	return cmd
}

type appSniffRunArgs struct {
	appPath            string
	cachePath          string
	noCache            bool
	androidPath        string
	apkeepPkg          string
	adbPkg             string
	iosPath            string
	simulatorBundleID  string
	harPath            string
	name               string
	baseURL            string
	hostFilter         string
	top                int
	includeGRPC        bool
	auditCLIDir        string
	outputPath         string
	analysisOutputPath string
	manifestOutputPath string
	asJSON             bool
}

func runAppSniff(ctx context.Context, a appSniffRunArgs, opts appSniffOptions) error {
	stdout := opts.stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	sourceCount := countNonEmpty(a.appPath, a.androidPath, a.apkeepPkg, a.adbPkg, a.iosPath, a.simulatorBundleID)
	if sourceCount == 0 && strings.TrimSpace(a.cachePath) == "" {
		return fmt.Errorf("specify one input source: --app, --cache, --android, --apkeep, --adb, --ios, or --simulator")
	}
	if sourceCount > 1 {
		return fmt.Errorf("specify only one primary source among --app, --android, --apkeep, --adb, --ios, --simulator")
	}

	sniffOpts := appsniff.Options{
		Name:        a.name,
		BaseURL:     a.baseURL,
		HostFilters: splitCSV(a.hostFilter),
		Top:         a.top,
		IncludeGRPC: a.includeGRPC,
		ScanCache:   !a.noCache,
		CacheDir:    a.cachePath,
		HARPath:     a.harPath,
	}

	var result *appsniff.DiscoveryResult
	var err error
	var tempDirs []string
	defer func() {
		for _, td := range tempDirs {
			_ = os.RemoveAll(td)
		}
	}()

	switch {
	case a.appPath != "":
		result, err = appsniff.DiscoverDesktopApp(a.appPath, sniffOpts)
	case a.androidPath != "":
		result, err = appsniff.DiscoverAndroid(a.androidPath, sniffOpts)
	case a.apkeepPkg != "":
		td, terr := os.MkdirTemp("", "pp-app-sniff-apkeep-*")
		if terr != nil {
			return fmt.Errorf("creating temp dir for apkeep: %w", terr)
		}
		tempDirs = append(tempDirs, td)
		pkgPath, aerr := appsniff.AcquireApkeep(ctx, a.apkeepPkg, td, opts.runner)
		if aerr != nil {
			return aerr
		}
		result, err = appsniff.DiscoverAndroid(pkgPath, sniffOpts)
		if err == nil && result.Meta.PackageID == "" {
			result.Meta.PackageID = a.apkeepPkg
		}
	case a.adbPkg != "":
		td, terr := os.MkdirTemp("", "pp-app-sniff-adb-*")
		if terr != nil {
			return fmt.Errorf("creating temp dir for adb: %w", terr)
		}
		tempDirs = append(tempDirs, td)
		pullDir, aerr := appsniff.AcquireADB(ctx, a.adbPkg, td, opts.runner)
		if aerr != nil {
			return aerr
		}
		result, err = appsniff.DiscoverAndroid(pullDir, sniffOpts)
		if err == nil && result.Meta.PackageID == "" {
			result.Meta.PackageID = a.adbPkg
		}
	case a.iosPath != "":
		result, err = appsniff.DiscoverIOS(a.iosPath, sniffOpts)
	case a.simulatorBundleID != "":
		simAppPath, aerr := appsniff.AcquireIOSSimulator(ctx, a.simulatorBundleID, opts.runner)
		if aerr != nil {
			return aerr
		}
		result, err = appsniff.DiscoverIOS(simAppPath, sniffOpts)
		if err == nil && result.Meta.BundleID == "" {
			result.Meta.BundleID = a.simulatorBundleID
		}
	case a.cachePath != "":
		result, err = appsniff.DiscoverCacheDir(a.cachePath, sniffOpts)
	}
	if err != nil {
		return err
	}

	if a.harPath != "" {
		if err := appsniff.ReconcileWithHAR(result, a.harPath); err != nil {
			return err
		}
	}

	// Audit mode: compare against an existing printed CLI and optionally also emit spec.
	var auditReport *appsniff.AuditReport
	if a.auditCLIDir != "" {
		auditReport, err = appsniff.AuditCLI(a.auditCLIDir, result)
		if err != nil {
			return err
		}
		if a.outputPath == "" && a.manifestOutputPath == "" {
			return writeAppSniffAuditOutput(stdout, result, auditReport, a.asJSON)
		}
	}

	apiSpec, err := appsniff.BuildSpec(result, sniffOpts)
	if err != nil {
		return err
	}
	trafficAnalysis := appsniff.BuildTrafficAnalysis(result, apiSpec)

	outputPath := a.outputPath
	if outputPath == "" {
		outputPath = appsniff.DefaultCachePath(apiSpec.Name)
	}
	analysisOutputPath := a.analysisOutputPath
	if analysisOutputPath == "" {
		analysisOutputPath = browsersniff.DefaultTrafficAnalysisPath(outputPath)
	}

	if _, err := writeBrowserSniffOutputs(apiSpec, trafficAnalysis, nil, outputPath, analysisOutputPath, "", browsersniff.AnalyzeOptions{}); err != nil {
		return err
	}

	if a.manifestOutputPath != "" {
		if err := appsniff.WriteManifest(result, a.manifestOutputPath); err != nil {
			return err
		}
	}

	endpointCount := 0
	for _, r := range apiSpec.Resources {
		endpointCount += len(r.Endpoints)
	}

	if a.asJSON {
		payload := map[string]any{
			"spec_path":        outputPath,
			"analysis_path":    analysisOutputPath,
			"manifest_path":    a.manifestOutputPath,
			"target_kind":      result.TargetKind,
			"primary_base_url": result.PrimaryBaseURL,
			"app_version":      result.Meta.Version,
			"framework":        result.Meta.Framework,
			"encrypted":        result.Meta.Encrypted,
			"endpoints":        endpointCount,
			"resources":        len(apiSpec.Resources),
			"grpc_endpoints":   len(result.GRPCEndpoints),
			"shards":           result.Meta.Shards,
			"auth_scheme":      result.Auth.Scheme,
			"pinning":          result.Pinning,
			"reconciliation":   result.Reconciliation,
			"stats":            result.Stats,
			"warnings":         result.Warnings,
		}
		if auditReport != nil {
			payload["audit"] = auditReport
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	fmt.Fprintf(stdout, "Spec written to %s (%d endpoint%s across %d resource%s)\n",
		outputPath, endpointCount, plural(endpointCount), len(apiSpec.Resources), plural(len(apiSpec.Resources)))
	fmt.Fprintf(stdout, "Traffic analysis written to %s\n", analysisOutputPath)
	if a.manifestOutputPath != "" {
		fmt.Fprintf(stdout, "Manifest written to %s\n", a.manifestOutputPath)
	}
	if result.PrimaryBaseURL != "" {
		fmt.Fprintf(stdout, "Base URL: %s (auth: %s, framework: %s)\n", result.PrimaryBaseURL, result.Auth.Scheme, result.Meta.Framework)
	}
	if len(result.GRPCEndpoints) > 0 && !a.includeGRPC {
		fmt.Fprintf(stdout, "Excluded %d gRPC/Protobuf endpoint%s from REST spec (pass --include-grpc to include)\n",
			len(result.GRPCEndpoints), plural(len(result.GRPCEndpoints)))
	}
	if result.Reconciliation != nil {
		fmt.Fprintf(stdout, "HAR reconciliation: %d confirmed, %d observed-only, %d static-only\n",
			result.Reconciliation.Confirmed, result.Reconciliation.ObservedOnly, result.Reconciliation.StaticOnly)
	}
	for _, p := range result.Pinning {
		fmt.Fprintf(stderr, "warning: %s: %s\n", p.Kind, p.Detail)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if auditReport != nil {
		writeAppSniffAuditText(stdout, auditReport)
	}
	fmt.Fprintf(stdout, "Run 'cli-printing-press generate --spec %s --traffic-analysis %s' to build the CLI\n",
		outputPath, analysisOutputPath)
	return nil
}

func writeAppSniffAuditOutput(w io.Writer, res *appsniff.DiscoveryResult, report *appsniff.AuditReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"target":      res.Target,
			"target_kind": res.TargetKind,
			"app_version": res.Meta.Version,
			"audit":       report,
		})
	}
	writeAppSniffAuditText(w, report)
	return nil
}

func writeAppSniffAuditText(w io.Writer, report *appsniff.AuditReport) {
	fmt.Fprintf(w, "CLI audit for %s: %d covered, %d missing endpoint%s\n",
		filepath.Base(report.CLIDir), report.CoveredCount, report.MissingCount, plural(report.MissingCount))
	if report.VersionDrift {
		fmt.Fprintf(w, "  version drift: CLI=%s app=%s\n", report.CLIVersion, report.AppVersion)
	}
	if len(report.MissingShards) > 0 {
		hosts := make([]string, 0, len(report.MissingShards))
		for _, s := range report.MissingShards {
			hosts = append(hosts, s.Host)
		}
		fmt.Fprintf(w, "  unwired shards: %s\n", strings.Join(hosts, ", "))
	}
	if len(report.MCPParityGaps) > 0 {
		fmt.Fprintf(w, "  MCP codeOrchEndpoints parity gaps: %d\n", len(report.MCPParityGaps))
	}
}

func countNonEmpty(vals ...string) int {
	n := 0
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}

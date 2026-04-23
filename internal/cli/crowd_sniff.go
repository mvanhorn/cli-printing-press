package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mvanhorn/cli-printing-press/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/internal/crowdsniff"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

// crowdSniffSource is the interface for discovery sources, enabling test injection.
type crowdSniffSource interface {
	Discover(ctx context.Context, apiName string) (crowdsniff.SourceResult, error)
}

// crowdSniffOptions holds injectable dependencies for testing.
type crowdSniffOptions struct {
	sources []crowdSniffSource
	stdout  io.Writer
	stderr  io.Writer
}

func newCrowdSniffCmd() *cobra.Command {
	return newCrowdSniffCmdWithOptions(crowdSniffOptions{})
}

func newCrowdSniffCmdWithOptions(opts crowdSniffOptions) *cobra.Command {
	var apiName string
	var outputPath string
	var baseURL string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "crowd-sniff",
		Short: "Discover API endpoints from npm SDKs and GitHub code search",
		Long: `Discover API endpoints by mining community signals: npm SDK packages
and GitHub code search. Produces a spec YAML compatible with 'printing-press generate'.

Complements 'browser-sniff' (which discovers from live web traffic) by finding
what developers have already mapped in published packages and code.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCrowdSniff(cmd.Context(), apiName, baseURL, outputPath, asJSON, opts)
		},
	}

	cmd.Flags().StringVar(&apiName, "api", "", "API name or domain (e.g., 'notion', 'api.stripe.com')")
	cmd.Flags().StringVar(&outputPath, "output", "", "Output path for generated spec YAML")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Override auto-detected base URL (must be HTTPS)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	_ = cmd.MarkFlagRequired("api")

	return cmd
}

func runCrowdSniff(ctx context.Context, apiName, baseURL, outputPath string, asJSON bool, opts crowdSniffOptions) error {
	stdout := opts.stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if err := validateCrowdSniffAPIName(apiName); err != nil {
		return err
	}

	sources := opts.sources
	if len(sources) == 0 {
		sources = []crowdSniffSource{
			crowdsniff.NewNPMSource(crowdsniff.NPMOptions{}),
			crowdsniff.NewGitHubSource(crowdsniff.GitHubOptions{}),
		}
	}

	results := make([]crowdsniff.SourceResult, len(sources))
	g := new(errgroup.Group)
	var warningMu sync.Mutex

	for i, src := range sources {
		i, src := i, src
		g.Go(func() error {
			result, err := src.Discover(ctx, apiName)
			if err != nil {
				warningMu.Lock()
				fmt.Fprintf(stderr, "warning: source %d: %v\n", i, err)
				warningMu.Unlock()
				return nil
			}
			results[i] = result
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("running sources: %w", err)
	}

	aggregated, baseURLCandidates := crowdsniff.Aggregate(results)
	mergedAuth := crowdsniff.AggregateAuth(results)

	if len(aggregated) == 0 {
		return fmt.Errorf("no endpoints discovered for %q", apiName)
	}

	resolvedBaseURL := crowdsniff.ResolveBaseURL(baseURL, baseURLCandidates)
	if resolvedBaseURL == "" {
		return fmt.Errorf("could not determine base URL for %q; use --base-url to specify", apiName)
	}

	if !isHTTPS(resolvedBaseURL) {
		return fmt.Errorf("base URL must use HTTPS: %s", resolvedBaseURL)
	}

	// Filter out endpoints whose origin doesn't match the resolved base URL.
	// Community wrappers that glue together multiple APIs (common for financial,
	// weather, and aggregator domains) would otherwise contaminate the spec
	// with endpoints from Polygon, FRED, Tiingo, etc. when targeting Yahoo
	// Finance — observed in retro #174.
	targetHost := hostFromBaseURL(resolvedBaseURL)
	if targetHost != "" {
		kept, dropped := crowdsniff.FilterByHost(aggregated, targetHost)
		if len(dropped) > 0 {
			fmt.Fprintf(stderr, "filtered %d endpoint(s) from other origins (target host: %s)\n", len(dropped), targetHost)
			for _, d := range dropped {
				fmt.Fprintf(stderr, "  dropped: %s %s (origin(s): %s)\n", d.Method, d.Path, strings.Join(d.OriginBaseURLs, ", "))
			}
		}
		aggregated = kept
	}

	if len(aggregated) == 0 {
		return fmt.Errorf("no endpoints remained after host filtering for %q (target: %s)", apiName, targetHost)
	}

	apiSpec, err := crowdsniff.BuildSpec(apiName, resolvedBaseURL, aggregated, mergedAuth)
	if err != nil {
		return fmt.Errorf("building spec: %w", err)
	}
	apiSpec.SpecSource = "community"

	if outputPath == "" {
		outputPath = defaultCrowdSniffCachePath(apiName)
	}

	if err := browsersniff.WriteSpec(apiSpec, outputPath); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}

	endpointCount := 0
	paramCount := 0
	for _, resource := range apiSpec.Resources {
		endpointCount += len(resource.Endpoints)
		for _, ep := range resource.Endpoints {
			paramCount += len(ep.Params)
		}
	}

	tierCounts := make(map[string]int)
	for _, ep := range aggregated {
		tierCounts[ep.SourceTier]++
	}

	if asJSON {
		return json.NewEncoder(stdout).Encode(map[string]interface{}{
			"spec_path":      outputPath,
			"endpoints":      endpointCount,
			"resources":      len(apiSpec.Resources),
			"param_count":    paramCount,
			"tier_breakdown": tierCounts,
		})
	}

	fmt.Fprintf(stdout, "Spec written to %s (%d endpoints across %d resources)\n", outputPath, endpointCount, len(apiSpec.Resources))
	if len(tierCounts) > 0 {
		parts := make([]string, 0, len(tierCounts))
		for tier, count := range tierCounts {
			parts = append(parts, fmt.Sprintf("%s: %d", tier, count))
		}
		fmt.Fprintf(stdout, "Tiers: %s\n", strings.Join(parts, ", "))
	}
	fmt.Fprintf(stdout, "Run 'printing-press generate --spec %s' to build the CLI\n", outputPath)
	return nil
}

// validateCrowdSniffAPIName rejects dangerous --api values.
func validateCrowdSniffAPIName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--api value is required")
	}
	for _, ch := range name {
		if ch == '\n' || ch == '\r' || ch == 0 {
			return fmt.Errorf("--api value contains invalid characters")
		}
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		// If it looks like a URL (contains ://), allow slashes in the URL path.
		if !strings.Contains(name, "://") {
			return fmt.Errorf("--api value contains path traversal characters")
		}
	}
	return nil
}

func defaultCrowdSniffCachePath(name string) string {
	// Sanitize name for use in file path.
	safeName := url.PathEscape(name)

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".cache", "printing-press", "crowd-sniff", safeName+"-spec.yaml")
	}
	return filepath.Join(home, ".cache", "printing-press", "crowd-sniff", safeName+"-spec.yaml")
}

func isHTTPS(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "https")
}

// hostFromBaseURL returns the lowercase host of a URL (no scheme, no port).
// Used by host-based endpoint filtering.
func hostFromBaseURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	return strings.ToLower(host)
}

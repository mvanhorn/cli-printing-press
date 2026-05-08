// manifest-gen downloads an OpenAPI/internal spec and generates a tools-manifest.json.
//
// Usage:
//
//	manifest-gen -spec <url-or-path> -output <dir>
//	manifest-gen -spec https://api.dub.co/openapi.yaml -output ./out
//	manifest-gen -spec ./local-spec.yaml -output ./out
//
// For sniffed/internal specs (not OpenAPI), use -format internal:
//
//	manifest-gen -spec ./espn-spec.yaml -format internal -output ./out
//
// The tool writes tools-manifest.json to the output directory.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func main() {
	specFlag := flag.String("spec", "", "URL or local path to the API spec")
	outputFlag := flag.String("output", ".", "Output directory for tools-manifest.json")
	formatFlag := flag.String("format", "auto", "Spec format: auto, openapi, internal, graphql")
	flag.Parse()

	if *specFlag == "" {
		fmt.Fprintln(os.Stderr, "usage: manifest-gen -spec <url-or-path> [-output <dir>] [-format auto|openapi|internal|graphql]")
		os.Exit(1)
	}

	// Load spec data.
	data, err := loadSpec(*specFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading spec: %v\n", err)
		os.Exit(1)
	}

	// Detect format if auto.
	format := *formatFlag
	if format == "auto" {
		format = detectFormat(data, *specFlag)
	}

	// Parse spec.
	var parsed *spec.APISpec
	switch format {
	case "openapi":
		if openapi.IsRemoteSpecSource(*specFlag) {
			parsed, err = openapi.ParseLenient(data)
		} else {
			parsed, err = openapi.ParseWithPathLenient(data, *specFlag)
		}
	case "internal":
		parsed, err = spec.ParseBytes(data)
	case "graphql":
		parsed, err = graphql.ParseSDLBytes(*specFlag, data)
	default:
		fmt.Fprintf(os.Stderr, "unknown format: %q\n", format)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing spec: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Parsed: %s (%s)\n", parsed.Name, format)
	fmt.Fprintf(os.Stderr, "  Base URL: %s\n", parsed.BaseURL)
	fmt.Fprintf(os.Stderr, "  Auth: %s\n", parsed.Auth.Type)

	total := 0
	for _, r := range parsed.Resources {
		total += len(r.Endpoints)
		for _, sr := range r.SubResources {
			total += len(sr.Endpoints)
		}
	}
	fmt.Fprintf(os.Stderr, "  Endpoints: %d\n", total)

	// Generate tools manifest.
	if err := os.MkdirAll(*outputFlag, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating output dir: %v\n", err)
		os.Exit(1)
	}

	if err := pipeline.WriteToolsManifest(*outputFlag, parsed); err != nil {
		fmt.Fprintf(os.Stderr, "error writing tools manifest: %v\n", err)
		os.Exit(1)
	}

	// Compute and print checksum.
	manifestPath := filepath.Join(*outputFlag, "tools-manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading manifest: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\nWrote %s (%d bytes)\n", manifestPath, len(manifestData))
	fmt.Fprintf(os.Stderr, "Spec format: %s\n", format)
}

func loadSpec(source string) ([]byte, error) {
	if openapi.IsRemoteSpecSource(source) {
		resp, err := http.Get(source)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", source, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("fetching %s: HTTP %d", source, resp.StatusCode)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
	}
	return os.ReadFile(source)
}

func detectFormat(data []byte, path string) string {
	s := string(data)
	lowerPath := strings.ToLower(path)

	// GraphQL SDL detection.
	if strings.HasSuffix(lowerPath, ".graphql") || strings.HasSuffix(lowerPath, ".gql") {
		return "graphql"
	}
	if strings.Contains(s, "type Query") || strings.Contains(s, "type Mutation") {
		return "graphql"
	}

	// OpenAPI detection.
	if strings.Contains(s, "openapi:") || strings.Contains(s, "\"openapi\"") ||
		strings.Contains(s, "swagger:") || strings.Contains(s, "\"swagger\"") {
		return "openapi"
	}

	// Internal spec detection.
	if strings.Contains(s, "base_url:") || strings.Contains(s, "resources:") {
		return "internal"
	}

	// Default to OpenAPI.
	return "openapi"
}

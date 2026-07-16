package pipeline

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var generatedResourcePathEntryRE = regexp.MustCompile(`(?m)^\s*"([^"]+)":\s*"([^"]+)",\s*$`)
var generatedResourceNameRE = regexp.MustCompile(`"([a-zA-Z0-9][a-zA-Z0-9_-]*)"`)

func runResourcePathContractChecks(dir string) []CommandResult {
	manifest, err := ReadToolsManifest(dir)
	if err != nil {
		if !hasGeneratedResourceCommand(dir) {
			return nil
		}
		return []CommandResult{{
			Command: "resource-path:manifest", Kind: "static",
			Error: fmt.Sprintf("cannot validate generated resource paths: %v", err),
		}}
	}
	manifestMethods := make(map[string]map[string]bool)
	for _, tool := range manifest.Tools {
		path := strings.TrimSpace(tool.Path)
		if path == "" {
			continue
		}
		if manifestMethods[path] == nil {
			manifestMethods[path] = map[string]bool{}
		}
		manifestMethods[path][strings.ToUpper(tool.Method)] = true
	}

	shared := readRuntimeFile(filepath.Join(dir, "internal", "cli", "resource_paths.go"))
	readPaths := generatedResourcePathMap(shared, "resourceReadPaths", "resourceDetailPaths")
	detailPaths := generatedResourcePathMap(shared, "resourceDetailPaths", "resourceWritePaths")
	writePaths := generatedResourcePathMap(shared, "resourceWritePaths", "resourceReadConfigs")
	exportPaths := make(map[string]string, len(readPaths)+len(detailPaths))
	maps.Copy(exportPaths, readPaths)
	for resource, path := range detailPaths {
		exportPaths[resource+" (detail)"] = path
	}

	var results []CommandResult
	for _, check := range []struct {
		command  string
		resolver string
		paths    map[string]string
		methods  map[string]bool
	}{
		{command: "tail", resolver: "resourceReadPath(", paths: readPaths, methods: map[string]bool{"GET": true}},
		{command: "export", resolver: "resourceReadPath(", paths: exportPaths, methods: map[string]bool{"GET": true}},
		{command: "import", resolver: "resourceWritePath(", paths: writePaths, methods: map[string]bool{"POST": true, "PUT": true, "PATCH": true}},
	} {
		sourcePath := filepath.Join(dir, "internal", "cli", check.command+".go")
		sourceBytes, readErr := os.ReadFile(sourcePath)
		if readErr != nil {
			continue
		}
		source := string(sourceBytes)
		result := CommandResult{Command: "resource-path:" + check.command, Kind: "static", Help: true, DryRun: true, Execute: true, Score: 3}
		var mismatches []string
		if strings.Contains(source, check.resolver) {
			if len(check.paths) == 0 {
				mismatches = append(mismatches, "resolver has no emitted resource paths")
			} else {
				for resource, path := range check.paths {
					if !manifestHasResourcePath(manifestMethods[path], check.methods) {
						if manifest.Auth.Type == "cookie" || manifest.Auth.Type == "composed" {
							continue
						}
						mismatches = append(mismatches, fmt.Sprintf("%q resolves to %s, which is absent from tools-manifest.json", resource, path))
					}
				}
			}
		} else if strings.Contains(source, `path := "/" + resource`) {
			resources := legacyAdvertisedResources(source, check.command)
			if check.command == "import" && len(resources) == 0 {
				mismatches = append(mismatches, "import derives paths from arbitrary resource names")
			}
			for _, resource := range resources {
				path := "/" + resource
				if !manifestHasResourcePath(manifestMethods[path], check.methods) {
					mismatches = append(mismatches, fmt.Sprintf("%q builds %s, which is absent from tools-manifest.json", resource, path))
				}
			}
		} else {
			mismatches = append(mismatches, "command does not use the emitted resource path resolver")
		}
		if len(mismatches) > 0 {
			sort.Strings(mismatches)
			result.Help, result.DryRun, result.Execute, result.Score = false, false, false, 0
			result.Error = strings.Join(mismatches, "; ") + "; use the emitted resource path map"
		}
		results = append(results, result)
	}
	return results
}

func hasGeneratedResourceCommand(dir string) bool {
	for _, command := range []string{"tail.go", "export.go", "import.go"} {
		if _, err := os.Stat(filepath.Join(dir, "internal", "cli", command)); err == nil {
			return true
		}
	}
	return false
}

func generatedResourcePathMap(source, startMarker, endMarker string) map[string]string {
	start := strings.Index(source, "var "+startMarker)
	if start < 0 {
		return nil
	}
	block := source[start:]
	if end := strings.Index(block, "var "+endMarker); end >= 0 {
		block = block[:end]
	}
	paths := map[string]string{}
	for _, match := range generatedResourcePathEntryRE.FindAllStringSubmatch(block, -1) {
		paths[match[1]] = match[2]
	}
	return paths
}

func manifestHasResourcePath(actual, allowed map[string]bool) bool {
	for method := range allowed {
		if actual[method] {
			return true
		}
	}
	return false
}

func legacyAdvertisedResources(source, command string) []string {
	var block string
	switch command {
	case "tail":
		start := strings.Index(source, "func tailKnownResources")
		if start >= 0 {
			block = source[start:]
			if end := strings.Index(block, "\n}"); end >= 0 {
				block = block[:end+2]
			}
		}
	case "export":
		start := strings.Index(source, "validResources := map[string]bool")
		if start >= 0 {
			block = source[start:]
			if end := strings.Index(block, "validResourceList :="); end >= 0 {
				block = block[:end]
			}
		}
	default:
		return nil
	}
	if block == "" {
		return nil
	}
	seen := map[string]bool{}
	var resources []string
	for _, match := range generatedResourceNameRE.FindAllStringSubmatch(block, -1) {
		if !seen[match[1]] {
			seen[match[1]] = true
			resources = append(resources, match[1])
		}
	}
	return resources
}

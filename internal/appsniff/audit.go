package appsniff

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var (
	reCLIVersionLit     = regexp.MustCompile(`(?i)(?:ClientVersion|AppVersion|UserAgent|x-client-version|x-app-version)[^"\n]{0,40}"[^"\n]*?(\d+\.\d+\.\d+(?:-[A-Za-z0-9.]+)?)"`)
	reCodeOrchEntry     = regexp.MustCompile(`Path:\s*"([^"]+)"`)
	reEndpointPathInGo  = regexp.MustCompile(`"(/(?:api|v\d+(?:\.\d+)?|graphql|trpc|rpc|oauth2?|auth)/[^"\s]{1,180})"`)
	reClientMethodInCLI = regexp.MustCompile(`func\s*\([^)]*\*Client\)\s*([A-Z][A-Za-z0-9_]+)\s*\(`)
)

// AuditCLI compares an existing printed CLI directory against discovered app
// endpoints, version numbers, backend shards, and MCP code-orchestration
// endpoint registration. All reads are strictly read-only.
func AuditCLI(cliDir string, res *DiscoveryResult) (*AuditReport, error) {
	if res == nil {
		return nil, fmt.Errorf("discovery result is required")
	}
	absDir, err := filepath.Abs(cliDir)
	if err != nil {
		return nil, fmt.Errorf("resolving cli dir: %w", err)
	}
	st, err := os.Stat(absDir)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("cli directory %s does not exist", absDir)
	}

	report := &AuditReport{
		CLIDir:     absDir,
		AppVersion: res.Meta.Version,
	}

	coveredPaths := make(map[string]bool)
	allSourceText := strings.Builder{}
	mcpPaths := make(map[string]bool)
	clientGoPaths := make(map[string]bool)

	_ = filepath.WalkDir(absDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if IsForbiddenSecretPath(path) {
			return nil
		}
		lower := strings.ToLower(path)
		if !strings.HasSuffix(lower, ".go") && !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".json") {
			return nil
		}
		if strings.HasSuffix(lower, "_test.go") {
			return nil
		}
		data, rerr := readBoundedFile(path, 2*1024*1024)
		if rerr != nil {
			return nil
		}
		text := string(data)
		allSourceText.WriteString(text)
		allSourceText.WriteByte('\n')

		if report.CLIVersion == "" {
			if m := reCLIVersionLit.FindStringSubmatch(text); len(m) == 2 {
				report.CLIVersion = m[1]
			}
		}

		for _, m := range reEndpointPathInGo.FindAllStringSubmatch(text, 512) {
			canon := canonicalizePathForMatch(m[1])
			coveredPaths[canon] = true
			relSlash := filepath.ToSlash(path)
			if strings.Contains(relSlash, "/internal/client/") {
				clientGoPaths[canon] = true
			}
		}
		if strings.Contains(filepath.ToSlash(path), "/internal/mcp/") && strings.Contains(text, "codeOrchEndpoints") {
			for _, m := range reCodeOrchEntry.FindAllStringSubmatch(text, 512) {
				mcpPaths[canonicalizePathForMatch(m[1])] = true
			}
		}
		return nil
	})

	sourceCorpus := strings.ToLower(allSourceText.String())

	// 1. Version drift check.
	if report.AppVersion != "" && report.CLIVersion != "" && report.AppVersion != report.CLIVersion {
		report.VersionDrift = true
		report.Notes = append(report.Notes, fmt.Sprintf("client version drift: CLI advertises %s while app bundle is %s", report.CLIVersion, report.AppVersion))
	}

	// 2. Unwired backend shards check.
	for _, shard := range res.Meta.Shards {
		if !strings.Contains(sourceCorpus, strings.ToLower(shard.Host)) {
			report.MissingShards = append(report.MissingShards, shard)
		}
	}

	// 3. Endpoint coverage diff.
	for _, ep := range res.Endpoints {
		canon := canonicalizePathForMatch(ep.Path)
		if coveredPaths[canon] || strings.Contains(sourceCorpus, strings.ToLower(ep.Path)) {
			report.CoveredCount++
		} else {
			report.MissingEndpoints = append(report.MissingEndpoints, ep)
		}
	}
	report.MissingCount = len(report.MissingEndpoints)

	// 4. MCP codeOrchEndpoints parity check (if CLI uses code orchestration).
	if len(mcpPaths) > 0 && len(clientGoPaths) > 0 {
		var gaps []string
		for cp := range clientGoPaths {
			if !mcpPaths[cp] && !slices.Contains(gaps, cp) {
				gaps = append(gaps, cp)
			}
		}
		sort.Strings(gaps)
		report.MCPParityGaps = gaps
		if len(gaps) > 0 {
			report.Notes = append(report.Notes, fmt.Sprintf("MCP codeOrchEndpoints is missing %d path(s) present in internal/client/", len(gaps)))
		}
	}
	_ = reClientMethodInCLI
	return report, nil
}

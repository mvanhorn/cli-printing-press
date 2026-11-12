package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	mockDataPipelineSyncTimeout = 30 * time.Second
	// One --max-pages 1 sync of the chosen resource. A scoped sync of a
	// single slow resource exceeded the old 30s cap, and that timeout then
	// fell through to a wider sync.
	liveDataPipelineSyncTimeout = 120 * time.Second
)

func dataPipelineSyncTimeout(mode string) time.Duration {
	if mode == "live" {
		return liveDataPipelineSyncTimeout
	}
	return mockDataPipelineSyncTimeout
}

// dataPipelineNoSyncableResourcesDetail is a passing probe result. The
// CLI's defaultSyncResources() list parsed as empty and no verify sync
// hint was set, so there is no scoped target and sync must not run.
const dataPipelineNoSyncableResourcesDetail = "SKIP (no syncable resources declared)"

// chooseDataPipelineProbeResource returns the one resource a data-pipeline
// sync probe may pass to --resources. An empty resource means sync must
// not run. Callers treat dataPipelineNoSyncableResourcesDetail as a pass
// and every other empty result as a refusal. Neither path may fall back
// to an unscoped sync.
func chooseDataPipelineProbeResource(cliDir string) (string, string) {
	declared, found, parsed := declaredSyncResourceNames(cliDir)
	hint, exclude, hintErr := dataPipelineProbeHints(cliDir)
	if hintErr != nil {
		return "", fmt.Sprintf("FAIL: could not read verify sync hints (%v); refusing unscoped sync", hintErr)
	}
	excluded := make(map[string]struct{}, len(exclude))
	for _, name := range exclude {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		excluded[name] = struct{}{}
	}
	children := dependentChildrenByParent(cliDir)

	var safe []string
	seen := make(map[string]struct{})
	for _, name := range declared {
		if _, drop := excluded[name]; drop || !isSafeSyncProbeResource(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		safe = append(safe, name)
	}

	if hint != "" {
		if !isSafeSyncProbeResource(hint) {
			return "", fmt.Sprintf("FAIL: verify sync resource %q is not a safe data-pipeline probe target; refusing unscoped sync", hint)
		}
		if _, drop := excluded[hint]; drop || !slices.Contains(safe, hint) {
			return "", fmt.Sprintf("FAIL: verify sync resource %q is not an allowed declared sync resource; refusing unscoped sync", hint)
		}
		if cascadesToExcluded(hint, children, excluded) {
			return "", fmt.Sprintf("FAIL: verify sync resource %q also syncs an excluded resource; refusing unscoped sync", hint)
		}
		return hint, ""
	}
	safe = withoutExcludedChildren(safe, children, excluded)
	if len(safe) == 0 {
		// An explicit empty list has nothing to scope. A missing or
		// unparsed declaration, or a list emptied by safety and exclude
		// filters, stays a refusal so sync is never unscoped.
		if found && parsed && len(declared) == 0 {
			return "", dataPipelineNoSyncableResourcesDetail
		}
		return "", "FAIL: no safe sync resource for the data-pipeline probe; refusing unscoped sync"
	}
	// repos stays the probe when the CLI declares it, so GitHub-shaped
	// sync trees keep that scoped target. Any other CLI uses the first
	// remaining safe declared name.
	if slices.Contains(safe, "repos") {
		return "repos", ""
	}
	return safe[0], ""
}

// isSafeSyncProbeResource rejects values that would widen --resources
// (commas), look like flags, or otherwise escape a single argv token.
func isSafeSyncProbeResource(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") {
		return false
	}
	if strings.ContainsAny(name, " \t\r\n,=\\\"'`") {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && (r >= '0' && r <= '9' || r == '_' || r == '-'):
		default:
			return false
		}
	}
	return true
}

// Naming a parent also syncs its dependents. Skip that parent when one of
// those dependents is excluded; --resources has no switch that syncs the
// parent alone.
func cascadesToExcluded(name string, children map[string][]string, excluded map[string]struct{}) bool {
	for _, child := range children[name] {
		if _, drop := excluded[child]; drop {
			return true
		}
	}
	return false
}

func withoutExcludedChildren(names []string, children map[string][]string, excluded map[string]struct{}) []string {
	if len(children) == 0 || len(excluded) == 0 {
		return names
	}
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if cascadesToExcluded(name, children, excluded) {
			continue
		}
		kept = append(kept, name)
	}
	return kept
}

var dependentChildRe = regexp.MustCompile(`Name:\s*"([^"]+)"\s*,\s*ParentTable:\s*"([^"]+)"`)

func dependentChildrenByParent(cliDir string) map[string][]string {
	children := map[string][]string{}
	if strings.TrimSpace(cliDir) == "" {
		return children
	}
	for _, path := range listGoFiles(filepath.Join(cliDir, "internal", "cli")) {
		content := stripGoComments(readFileContent(path))
		for _, match := range dependentChildRe.FindAllStringSubmatch(content, -1) {
			child, parent := match[1], match[2]
			if child == "" || parent == "" {
				continue
			}
			children[parent] = append(children[parent], child)
		}
	}
	return children
}

func dataPipelineProbeHints(cliDir string) (string, []string, error) {
	if strings.TrimSpace(cliDir) == "" {
		return "", nil, nil
	}
	var resource string
	var exclude []string
	manifest, err := ReadCLIManifest(cliDir)
	if err != nil && !os.IsNotExist(err) {
		return "", nil, err
	}
	if err == nil {
		resource = strings.TrimSpace(manifest.VerifySyncResource)
		exclude = append(exclude, manifest.VerifySyncExclude...)
	}
	verify, err := researchVerifyHint(cliDir)
	if err != nil {
		return "", nil, err
	}
	if verify != nil {
		if resource == "" {
			resource = strings.TrimSpace(verify.SyncResource)
		}
		exclude = append(exclude, verify.SyncExclude...)
	}
	return resource, exclude, nil
}

func researchVerifyHint(cliDir string) (*ResearchVerify, error) {
	for _, path := range []string{
		filepath.Join(cliDir, "research.json"),
		filepath.Join(cliDir, "research", "research.json"),
	} {
		verify, err := researchVerifyFromFile(path)
		if err != nil || verify != nil {
			return verify, err
		}
	}
	canonical, err := ResolveTargetDir(cliDir)
	if err != nil {
		return nil, nil
	}
	dir := FindResearchDir(cliDir)
	if dir == "" || dir == canonical {
		return nil, nil
	}
	return researchVerifyFromFile(filepath.Join(dir, "research.json"))
}

func researchVerifyFromFile(path string) (*ResearchVerify, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var envelope struct {
		Verify json.RawMessage `json:"verify"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", filepath.Base(path), err)
	}
	if trimmed := strings.TrimSpace(string(envelope.Verify)); trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var verify ResearchVerify
	if err := json.Unmarshal(envelope.Verify, &verify); err != nil {
		return nil, fmt.Errorf("decoding %s verify: %w", filepath.Base(path), err)
	}
	return &verify, nil
}

func declaredSyncResourceNames(cliDir string) (names []string, found, parsed bool) {
	if strings.TrimSpace(cliDir) == "" {
		return nil, false, false
	}
	for _, path := range defaultSyncResourceFiles(cliDir) {
		names, found, parsed = inspectDefaultSyncResources(readFileContent(path))
		if found {
			return names, true, parsed
		}
	}
	return nil, false, false
}

func defaultSyncResourceFiles(cliDir string) []string {
	var paths []string
	syncGo := filepath.Join(cliDir, "internal", "cli", "sync.go")
	if fileExists(syncGo) {
		paths = append(paths, syncGo)
	}
	for _, path := range listGoFiles(filepath.Join(cliDir, "internal", "cli")) {
		if filepath.Base(path) == "sync.go" {
			continue
		}
		paths = append(paths, path)
	}
	paths = append(paths, listGoFiles(filepath.Join(cliDir, "internal", "store"))...)
	return paths
}

func parseDefaultSyncResources(content string) ([]string, bool) {
	names, found, _ := inspectDefaultSyncResources(content)
	return names, found
}

// inspectDefaultSyncResources reads the first defaultSyncResources
// declaration. found is false when it is absent. parsed is true only when
// a []string composite was read, including an empty one. A declaration that
// does not parse stays found and not parsed so callers do not treat it as
// an empty list or fall through to a later file.
func inspectDefaultSyncResources(content string) (names []string, found, parsed bool) {
	content = stripGoComments(content)
	idx := indexDeclAtLineStart(content, "func defaultSyncResources")
	if idx < 0 {
		idx = indexDeclAtLineStart(content, "var defaultSyncResources")
	}
	if idx < 0 {
		return nil, false, false
	}
	return parsedSyncResourceLiteral(truncateAtNextDecl(content[idx:]))
}

func parsedSyncResourceLiteral(segment string) (names []string, found, parsed bool) {
	const marker = "[]string{"
	lit := strings.Index(segment, marker)
	if lit < 0 {
		return nil, true, false
	}
	names, parsed = compositeStringLiterals(segment[lit+len(marker)-1:])
	if !parsed {
		return nil, true, false
	}
	return names, true, true
}

func truncateAtNextDecl(segment string) string {
	if len(segment) == 0 {
		return segment
	}
	cut := len(segment)
	for _, needle := range []string{"\nfunc ", "\nvar "} {
		if next := strings.Index(segment[1:], needle); next >= 0 && next+1 < cut {
			cut = next + 1
		}
	}
	return segment[:cut]
}

// stripGoComments drops // and /* */ comments so a commented resource list
// cannot become the probe target. Quoted strings are left intact.
func stripGoComments(content string) string {
	var b strings.Builder
	b.Grow(len(content))
	i := 0
	for i < len(content) {
		switch content[i] {
		case '"', '\'', '`':
			next, ok := endOfGoLiteral(content, i)
			if !ok {
				b.WriteString(content[i:])
				return b.String()
			}
			b.WriteString(content[i:next])
			i = next
		case '/':
			if strings.HasPrefix(content[i:], "//") {
				nl := strings.IndexByte(content[i:], '\n')
				if nl < 0 {
					return b.String()
				}
				b.WriteByte('\n')
				i += nl + 1
				continue
			}
			if strings.HasPrefix(content[i:], "/*") {
				end := strings.Index(content[i+2:], "*/")
				if end < 0 {
					return b.String()
				}
				b.WriteByte('\n')
				i += end + 4
				continue
			}
			b.WriteByte(content[i])
			i++
		default:
			b.WriteByte(content[i])
			i++
		}
	}
	return b.String()
}

func endOfGoLiteral(content string, start int) (int, bool) {
	quote := content[start]
	if quote == '`' {
		next := strings.IndexByte(content[start+1:], '`')
		if next < 0 {
			return 0, false
		}
		return start + 1 + next + 1, true
	}
	i := start + 1
	for i < len(content) {
		if content[i] == '\\' {
			if i+1 >= len(content) {
				return 0, false
			}
			i += 2
			continue
		}
		if content[i] == quote {
			return i + 1, true
		}
		i++
	}
	return 0, false
}

func indexDeclAtLineStart(content, needle string) int {
	from := 0
	for from < len(content) {
		i := strings.Index(content[from:], needle)
		if i < 0 {
			return -1
		}
		abs := from + i
		lineStart := strings.LastIndex(content[:abs], "\n") + 1
		after := abs + len(needle)
		if strings.TrimSpace(content[lineStart:abs]) == "" && !identByteAt(content, after) {
			return abs
		}
		from = abs + len(needle)
	}
	return -1
}

func identByteAt(content string, i int) bool {
	if i < 0 || i >= len(content) {
		return false
	}
	r := content[i]
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// compositeStringLiterals reads a Go composite literal that starts at '{'.
// A value that is not a string ends the parse as unusable (found, no names
// is the caller's fail-closed path).
func compositeStringLiterals(content string) ([]string, bool) {
	if content == "" || content[0] != '{' {
		return nil, false
	}
	var names []string
	i := 1
	for i < len(content) {
		switch content[i] {
		case ' ', '\t', '\n', '\r', ',':
			i++
		case '}':
			return names, true
		case '"':
			value, next, ok := parseGoString(content, i)
			if !ok {
				return nil, false
			}
			names = append(names, value)
			i = next
		default:
			return nil, false
		}
	}
	return nil, false
}

func parseGoString(content string, start int) (string, int, bool) {
	i := start + 1
	for i < len(content) {
		if content[i] == '\\' {
			if i+1 >= len(content) {
				return "", 0, false
			}
			i += 2
			continue
		}
		if content[i] == '"' {
			value, err := strconv.Unquote(content[start : i+1])
			if err != nil {
				return "", 0, false
			}
			return value, i + 1, true
		}
		i++
	}
	return "", 0, false
}

func scopedSyncProbeAttempts(dbPath, resource string) [][]string {
	return [][]string{
		{"sync", "--db", dbPath, "--resources", resource, "--full"},
		{"sync", "--db", dbPath, "--resources", resource},
		{"sync", "--resources", resource, "--full"},
		{"sync", "--resources", resource},
	}
}

func canDropSyncProbeFlag(flag string) bool {
	switch flag {
	case "--full", "--db":
		return true
	default:
		return false
	}
}

func syncProbeAttemptHasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func shouldSkipSyncProbeAttempt(args []string, errs []error) bool {
	for _, err := range errs {
		flag, ok := unknownSyncFlag(err)
		if !ok || !canDropSyncProbeFlag(flag) {
			continue
		}
		if syncProbeAttemptHasFlag(args, flag) {
			return true
		}
	}
	return false
}

func annotateDataPipelineProbe(pass bool, detail, resource string) (bool, string) {
	if detail == "" || resource == "" {
		return pass, detail
	}
	return pass, detail + " [sync --resources " + resource + "]"
}

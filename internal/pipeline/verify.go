package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mvanhorn/cli-printing-press/internal/artifacts"
)

type Verifier struct {
	Dir      string
	SpecPath string
	spec     *openAPISpec
}

type PathProofResult struct {
	File         string `json:"file"`
	Command      string `json:"command"`
	Path         string `json:"path"`
	ExtractedURL string `json:"extracted_url"`
	Method       string `json:"method"`
	Valid        bool   `json:"valid"`
	InSpec       bool   `json:"in_spec"`
	SpecPath     string `json:"spec_path,omitempty"`
}

type FlagProofResult struct {
	Flag       string `json:"flag"`
	FlagName   string `json:"flag_name"`
	CLIName    string `json:"cli_name"`
	References int    `json:"references"`
	RefCount   int    `json:"ref_count"`
	DeadFlag   bool   `json:"dead_flag"`
}

type PipelineProofResult struct {
	Table      string `json:"table"`
	TableName  string `json:"table_name"`
	Columns    int    `json:"columns"`
	HasWrite   bool   `json:"has_write"`
	WritePath  string `json:"write_path,omitempty"`
	HasRead    bool   `json:"has_read"`
	ReadPath   string `json:"read_path,omitempty"`
	HasSearch  bool   `json:"has_search"`
	SearchPath string `json:"search_path,omitempty"`
	HasFTS     bool   `json:"has_fts"`
	FTSExists  bool   `json:"fts_exists"`
	GhostTable bool   `json:"ghost_table"`
	OrphanFTS  bool   `json:"orphan_fts"`
}

type AuthProofResult struct {
	SpecScheme      string `json:"spec_scheme"`
	SpecFormat      string `json:"spec_format"`
	GeneratedScheme string `json:"generated_scheme"`
	GeneratedFormat string `json:"generated_format"`
	Match           bool   `json:"match"`
	Mismatch        bool   `json:"mismatch"`
	EnvVarCorrect   bool   `json:"env_var_correct"`
	Detail          string `json:"detail"`
}

func NewVerifier(dir, specPath string) (*Verifier, error) {
	v := &Verifier{
		Dir:      dir,
		SpecPath: specPath,
	}
	if specPath != "" {
		loaded, err := loadDogfoodOpenAPISpec(specPath)
		if err != nil {
			return nil, fmt.Errorf("loading spec: %w", err)
		}
		v.spec = loaded
	}
	return v, nil
}

func (v *Verifier) CompileGate() error {
	if v == nil {
		return fmt.Errorf("nil verifier")
	}
	if err := artifacts.CleanupGeneratedCLI(v.Dir, artifacts.CleanupOptions{
		RemoveValidationBinaries: true,
		RemoveDogfoodBinaries:    true,
		RemoveRecursiveCopies:    true,
		RemoveFinderMetadata:     true,
	}); err != nil {
		return fmt.Errorf("pre-compile cleanup: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	build := exec.CommandContext(ctx, "go", "build", "./...")
	build.Dir = v.Dir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %w\n%s", err, out)
	}

	vet := exec.CommandContext(ctx, "go", "vet", "./...")
	vet.Dir = v.Dir
	if out, err := vet.CombinedOutput(); err != nil {
		return fmt.Errorf("go vet failed: %w\n%s", err, out)
	}

	return nil
}

var (
	verifyPathAssignRe = regexp.MustCompile(`(?m)\bpath\s*(?::=|=|:)\s*"([^"]+)"`)
	verifyClientCallRe = regexp.MustCompile(`c\.(Get|Post|Patch|Delete|Put)\s*\(\s*"([^"]+)"`)
)

var verifyInfraFiles = map[string]struct{}{
	"root.go":      {},
	"helpers.go":   {},
	"doctor.go":    {},
	"auth.go":      {},
	"version.go":   {},
	"export.go":    {},
	"import.go":    {},
	"search.go":    {},
	"sync.go":      {},
	"tail.go":      {},
	"analytics.go": {},
	"workflow.go":  {},
}

func (v *Verifier) PathProof() []PathProofResult {
	if v == nil {
		return nil
	}

	cliDir := filepath.Join(v.Dir, "internal", "cli")
	files := listGoFiles(cliDir)
	if len(files) == 0 {
		return nil
	}

	var specPatterns []*regexp.Regexp
	var specKeys []string
	if v.spec != nil && len(v.spec.Paths) > 0 {
		specPatterns = compileSpecPathPatterns(v.spec.Paths)
		specKeys = append(specKeys, v.spec.Paths...)
	}

	var results []PathProofResult

	for _, file := range files {
		base := filepath.Base(file)
		if _, infra := verifyInfraFiles[base]; infra {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		src := string(content)
		command := strings.TrimSuffix(base, ".go")

		pathMatches := verifyPathAssignRe.FindAllStringSubmatch(src, -1)
		clientMatches := verifyClientCallRe.FindAllStringSubmatch(src, -1)

		if len(pathMatches) == 0 && len(clientMatches) == 0 {
			continue
		}

		for _, m := range pathMatches {
			url := m[1]
			r := PathProofResult{
				File:         base,
				Command:      command,
				Path:         url,
				ExtractedURL: url,
			}
			if len(specPatterns) > 0 {
				r.InSpec = pathMatchesSpec(url, specPatterns)
				r.Valid = r.InSpec
				if r.InSpec {
					r.SpecPath = findMatchingSpecPath(url, specKeys)
				}
			} else {
				r.Valid = true
			}
			results = append(results, r)
		}

		for _, m := range clientMatches {
			method := strings.ToUpper(m[1])
			url := m[2]
			r := PathProofResult{
				File:         base,
				Command:      command,
				Path:         url,
				ExtractedURL: url,
				Method:       method,
			}
			if len(specPatterns) > 0 {
				r.InSpec = pathMatchesSpec(url, specPatterns)
				r.Valid = r.InSpec
				if r.InSpec {
					r.SpecPath = findMatchingSpecPath(url, specKeys)
				}
			} else {
				r.Valid = true
			}
			results = append(results, r)
		}
	}

	return results
}

func (v *Verifier) FlagProof() []FlagProofResult {
	if v == nil {
		return nil
	}

	rootData, err := os.ReadFile(filepath.Join(v.Dir, "internal", "cli", "root.go"))
	if err != nil {
		return nil
	}

	fieldRe := regexp.MustCompile(`&flags\.(\w+)`)
	matches := fieldRe.FindAllStringSubmatch(string(rootData), -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var fields []string
	for _, m := range matches {
		if _, ok := seen[m[1]]; !ok {
			seen[m[1]] = struct{}{}
			fields = append(fields, m[1])
		}
	}

	cliFiles := listGoFiles(filepath.Join(v.Dir, "internal", "cli"))
	var cliSources []string
	for _, file := range cliFiles {
		if filepath.Base(file) == "root.go" {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		cliSources = append(cliSources, string(data))
	}

	var clientSource string
	clientData, err := os.ReadFile(filepath.Join(v.Dir, "internal", "client", "client.go"))
	if err == nil {
		clientSource = string(clientData)
	}

	var results []FlagProofResult
	for _, field := range fields {
		refCount := 0
		needle := "flags." + field
		for _, src := range cliSources {
			refCount += strings.Count(src, needle)
		}
		if clientSource != "" {
			refCount += strings.Count(clientSource, "f."+field)
			refCount += strings.Count(clientSource, field)
		}

		results = append(results, FlagProofResult{
			Flag:       field,
			FlagName:   field,
			CLIName:    camelToKebab(field),
			References: refCount,
			RefCount:   refCount,
			DeadFlag:   refCount == 0,
		})
	}

	return results
}

var (
	createTableRe        = regexp.MustCompile(`CREATE TABLE\s+(?:IF NOT EXISTS\s+)?(\w+)\s*\(`)
	createVirtualTableRe = regexp.MustCompile(`CREATE VIRTUAL TABLE\s+(?:IF NOT EXISTS\s+)?(\w+)\s+USING\s+fts5`)
)

var exemptTables = map[string]struct{}{
	"sync_state": {},
	"resources":  {},
}

func (v *Verifier) PipelineProof() []PipelineProofResult {
	if v == nil {
		return nil
	}

	storeData, err := os.ReadFile(filepath.Join(v.Dir, "internal", "store", "store.go"))
	if err != nil {
		return nil
	}
	storeSrc := string(storeData)

	tableMatches := createTableRe.FindAllStringSubmatch(storeSrc, -1)
	ftsMatches := createVirtualTableRe.FindAllStringSubmatch(storeSrc, -1)

	ftsSet := make(map[string]struct{})
	for _, m := range ftsMatches {
		ftsSet[m[1]] = struct{}{}
	}

	cliFiles := listGoFiles(filepath.Join(v.Dir, "internal", "cli"))
	var cliSources []string
	for _, file := range cliFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		cliSources = append(cliSources, string(data))
	}
	allCLI := strings.Join(cliSources, "\n")
	allCLILower := strings.ToLower(allCLI)

	var results []PipelineProofResult

	for _, m := range tableMatches {
		tableName := m[1]

		if _, exempt := exemptTables[tableName]; exempt {
			continue
		}
		if strings.HasSuffix(tableName, "_fts") {
			continue
		}

		pascal := pascalCase(tableName)
		ftsName := tableName + "_fts"
		_, ftsExists := ftsSet[ftsName]

		columnCount := countTableColumns(storeSrc, tableName)

		r := PipelineProofResult{
			Table:     tableName,
			TableName: tableName,
			Columns:   columnCount,
			FTSExists: ftsExists,
			HasFTS:    ftsExists,
		}

		upsertNeedle := "Upsert" + pascal
		insertNeedle := strings.ToLower("INSERT INTO " + tableName)
		insertReplaceNeedle := strings.ToLower("INSERT OR REPLACE INTO " + tableName)
		if strings.Contains(allCLI, upsertNeedle) {
			r.HasWrite = true
			r.WritePath = upsertNeedle
		} else if strings.Contains(allCLILower, insertNeedle) || strings.Contains(allCLILower, insertReplaceNeedle) {
			r.HasWrite = true
			r.WritePath = "INSERT INTO " + tableName
		}

		fromNeedle := strings.ToLower("FROM " + tableName)
		joinNeedle := strings.ToLower("JOIN " + tableName)
		getNeedle := "Get" + pascal
		queryNeedle := "Query" + pascal
		if strings.Contains(allCLILower, fromNeedle) || strings.Contains(allCLILower, joinNeedle) {
			r.HasRead = true
			r.ReadPath = "FROM/JOIN " + tableName
		} else if strings.Contains(allCLI, getNeedle) || strings.Contains(allCLI, queryNeedle) {
			r.HasRead = true
			r.ReadPath = getNeedle
		}

		if ftsExists {
			searchNeedle := "Search" + pascal
			ftsMatchNeedle := strings.ToLower(ftsName + " MATCH")
			if strings.Contains(allCLI, searchNeedle) || strings.Contains(allCLILower, ftsMatchNeedle) {
				r.HasSearch = true
				r.SearchPath = searchNeedle
			}
		}

		r.GhostTable = !r.HasWrite
		r.OrphanFTS = ftsExists && !r.HasSearch

		results = append(results, r)
	}

	return results
}

func (v *Verifier) AuthProof() AuthProofResult {
	if v == nil {
		return AuthProofResult{Match: true, Detail: "nil verifier"}
	}

	result := AuthProofResult{
		Match:  true,
		Detail: "no recognized auth scheme in spec",
	}

	if v.spec == nil {
		result.Detail = "spec not provided; auth check skipped"
		return result
	}

	expectedPrefix := ""
	switch {
	case strings.Contains(strings.ToLower(v.spec.Auth.Format), "bot "):
		schemeName := `bot token format (expects "Bot " prefix)`
		result.SpecFormat = schemeName
		result.SpecScheme = schemeName
		expectedPrefix = "Bot "
	case strings.EqualFold(v.spec.Auth.Type, "bearer_token"):
		schemeName := `bearer token format (expects "Bearer " prefix)`
		result.SpecFormat = schemeName
		result.SpecScheme = schemeName
		expectedPrefix = "Bearer "
	case strings.Contains(strings.ToLower(v.spec.Auth.Format), "basic "):
		schemeName := `basic auth format (expects "Basic " prefix)`
		result.SpecFormat = schemeName
		result.SpecScheme = schemeName
		expectedPrefix = "Basic "
	}

	clientData, err := os.ReadFile(filepath.Join(v.Dir, "internal", "client", "client.go"))
	if err != nil {
		result.Match = false
		result.Mismatch = true
		result.Detail = fmt.Sprintf("failed to read client.go: %v", err)
		return result
	}

	clientSource := string(clientData)
	switch {
	case strings.Contains(clientSource, `"Bot "`):
		result.GeneratedFormat = "Bot "
		result.GeneratedScheme = "Bot "
	case strings.Contains(clientSource, `"Bearer "`):
		result.GeneratedFormat = "Bearer "
		result.GeneratedScheme = "Bearer "
	default:
		result.GeneratedFormat = "unknown"
		result.GeneratedScheme = "unknown"
	}

	envVarRe := regexp.MustCompile(`os\.Getenv\("([^"]+)"\)`)
	envMatches := envVarRe.FindAllStringSubmatch(clientSource, -1)
	if len(envMatches) > 0 {
		for _, m := range envMatches {
			envName := m[1]
			if strings.HasSuffix(envName, "_TOKEN") || strings.HasSuffix(envName, "_API_KEY") || strings.HasSuffix(envName, "_KEY") {
				result.EnvVarCorrect = true
				break
			}
		}
	}

	if expectedPrefix == "" {
		result.Detail = "no bot/bearer/basic auth format detected in spec"
		return result
	}

	result.Match = result.GeneratedFormat == expectedPrefix
	result.Mismatch = !result.Match
	if result.Match {
		result.Detail = fmt.Sprintf("spec and generated client both use %q", strings.TrimSpace(expectedPrefix))
	} else {
		result.Detail = fmt.Sprintf("spec expects %q but generated client uses %q", strings.TrimSpace(expectedPrefix), strings.TrimSpace(result.GeneratedFormat))
	}

	return result
}

func findMatchingSpecPath(path string, specKeys []string) string {
	paramRe := regexp.MustCompile(`\\\{[^/]+\\\}`)
	for _, key := range specKeys {
		quoted := regexp.QuoteMeta(key)
		regex := "^" + paramRe.ReplaceAllString(quoted, `[^/]+`) + "$"
		re, err := regexp.Compile(regex)
		if err != nil {
			continue
		}
		if re.MatchString(path) {
			return key
		}
	}
	return ""
}

func pascalCase(tableName string) string {
	parts := strings.Split(tableName, "_")
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		singular := part
		if len(singular) > 1 && strings.HasSuffix(singular, "s") {
			singular = singular[:len(singular)-1]
		}
		runes := []rune(singular)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	return b.String()
}

func countTableColumns(storeSrc, tableName string) int {
	re := regexp.MustCompile(`(?is)CREATE TABLE\s+(?:IF NOT EXISTS\s+)?` + regexp.QuoteMeta(tableName) + `\s*\((.*?)\);`)
	match := re.FindStringSubmatch(storeSrc)
	if match == nil {
		return 0
	}
	columns := 0
	for line := range strings.SplitSeq(match[1], "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, ","))
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "PRIMARY KEY") || strings.HasPrefix(upper, "FOREIGN KEY") || strings.HasPrefix(upper, "UNIQUE") || strings.HasPrefix(upper, "CONSTRAINT") || strings.HasPrefix(upper, "CHECK") {
			continue
		}
		columns++
	}
	return columns
}

func camelToKebab(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			// Don't insert hyphen if:
			// - first character
			// - previous char was also uppercase AND next char is uppercase or end (acronym interior)
			if i > 0 {
				prevUpper := unicode.IsUpper(runes[i-1])
				nextUpper := i+1 < len(runes) && unicode.IsUpper(runes[i+1])
				nextEnd := i+1 >= len(runes)
				if prevUpper && (nextUpper || nextEnd) {
					// Inside or at end of acronym - no hyphen
				} else if !prevUpper {
					// Start of new word after lowercase
					b.WriteByte('-')
				} else {
					// prevUpper && next is lowercase = start of new word after acronym (e.g., PRTriage -> pr-triage)
					b.WriteByte('-')
				}
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

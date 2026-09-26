package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
	"gopkg.in/yaml.v3"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	openapiparser "github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	apispec "github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// Novel hosts declared by a novel feature must appear in research artifacts.
// Dry-run dogfood prints URLs without contacting them, so an invented host
// would otherwise ship. DNS is a second check and is skipped when Resolve is nil.

const novelHostLookupTimeout = 2 * time.Second

var errNovelHostNXDOMAIN = errors.New("novel host does not resolve")

// NovelHostResolver looks up a host. Return errNovelHostNXDOMAIN when the
// name does not exist. Other errors are treated as offline and do not fail
// the gate.
type NovelHostResolver func(ctx context.Context, host string) error

// NovelHostInput is the artifact set the novel-host gate compares against.
type NovelHostInput struct {
	CLIDir          string
	ResearchDir     string
	Spec            *apispec.APISpec
	SpecPaths       []string
	Traffic         *browsersniff.TrafficAnalysis
	DiscoveryPages  []string
	Resolve         NovelHostResolver
	FeatureOverride []NovelFeature
}

type novelHostDecl struct {
	feature string
	file    string
	line    int
	host    string
}

var (
	absoluteURLRe = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)
	proseHostRe   = regexp.MustCompile(`\b(?:[a-zA-Z0-9-]+\.)+[a-zA-Z]{2,}\b`)
	bareHostRe    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
)

var bareHostFileExt = map[string]bool{
	"css": true, "csv": true, "gif": true, "go": true, "html": true, "jpeg": true,
	"jpg": true, "js": true, "json": true, "lock": true, "m3u8": true, "m4s": true,
	"md": true, "mod": true, "mp3": true, "mp4": true, "mpd": true, "pdf": true,
	"png": true, "proto": true, "sum": true, "svg": true, "toml": true, "ts": true,
	"tsv": true, "txt": true, "wav": true, "webp": true, "xml": true, "yaml": true,
	"yml": true,
}

var documentedURLListNames = map[string]bool{
	"observed-urls.txt":             true,
	"documented-urls.txt":           true,
	"urls.txt":                      true,
	"observed-hosts.txt":            true,
	"browser-sniff-report.md":       true,
	"crowd-browser-sniff-report.md": true,
}

func dnsNovelHostResolver(ctx context.Context, host string) error {
	ctx, cancel := context.WithTimeout(ctx, novelHostLookupTimeout)
	defer cancel()
	_, err := net.DefaultResolver.LookupHost(ctx, host)
	if err == nil {
		return nil
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return errNovelHostNXDOMAIN
	}
	return nil
}

// Dry-run dogfood prints novel-feature URLs and never contacts them, so an
// invented host must fail generation or it ships in the printed CLI.
func RejectUnverifiedNovelHosts(in NovelHostInput) error {
	findings := unverifiedNovelHosts(in)
	if len(findings) == 0 {
		return nil
	}
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		where := f.File
		if f.Line > 0 {
			where = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		parts = append(parts, fmt.Sprintf("%s (%s) — %s", f.Command, where, f.Reason))
	}
	return fmt.Errorf("novel feature host is not in research artifacts: %s", strings.Join(parts, "; "))
}

func unverifiedNovelHosts(in NovelHostInput) []ReimplementationFinding {
	features := in.FeatureOverride
	if features == nil && in.ResearchDir != "" {
		research, err := LoadResearch(in.ResearchDir)
		if err != nil || len(research.NovelFeatures) == 0 {
			return nil
		}
		features = research.NovelFeatures
	}
	if len(features) == 0 {
		return nil
	}

	declared := declaredNovelHosts(in, features)
	if len(declared) == 0 {
		return nil
	}
	observed := observedNovelHosts(in)
	var findings []ReimplementationFinding
	seen := map[string]bool{}
	dnsErr := map[string]error{}
	for _, decl := range declared {
		if _, ok := observed[decl.host]; !ok {
			key := decl.feature + "\x00" + decl.file + "\x00" + strconv.Itoa(decl.line) + "\x00" + decl.host
			if !seen[key] {
				seen[key] = true
				findings = append(findings, ReimplementationFinding{
					Command: decl.feature,
					File:    decl.file,
					Line:    decl.line,
					Host:    decl.host,
					Reason:  fmt.Sprintf("unverified host %q is not in research artifacts", decl.host),
				})
			}
			continue
		}
		if in.Resolve == nil {
			continue
		}
		key := decl.feature + "\x00" + decl.file + "\x00" + strconv.Itoa(decl.line) + "\x00" + decl.host + "\x00dns"
		if seen[key] {
			continue
		}
		seen[key] = true
		err, ok := dnsErr[decl.host]
		if !ok {
			err = in.Resolve(context.Background(), decl.host)
			dnsErr[decl.host] = err
		}
		if !errors.Is(err, errNovelHostNXDOMAIN) {
			continue
		}
		findings = append(findings, ReimplementationFinding{
			Command: decl.feature,
			File:    decl.file,
			Line:    decl.line,
			Host:    decl.host,
			Reason:  fmt.Sprintf("unverified host %q does not resolve in DNS", decl.host),
		})
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Host < findings[j].Host
	})
	return findings
}

func declaredNovelHosts(in NovelHostInput, features []NovelFeature) []novelHostDecl {
	var declared []novelHostDecl
	researchPath := ""
	var researchRaw []byte
	if in.ResearchDir != "" {
		researchPath = filepath.Join(in.ResearchDir, "research.json")
		researchRaw, _ = os.ReadFile(researchPath)
	}
	spans := novelFeatureSpans(researchRaw)
	usedSpans := map[int]bool{}
	for _, feature := range features {
		text := strings.Join([]string{
			feature.Name, feature.Command, feature.Description, feature.Rationale,
			feature.Example, feature.WhyItMatters,
		}, "\n")
		label := feature.Command
		if label == "" {
			label = feature.Name
		}
		span := takeFeatureSpan(spans, usedSpans, feature)
		for _, host := range hostsInProse(text) {
			declared = append(declared, novelHostDecl{
				feature: label,
				file:    "research.json",
				line:    hostLineInSpan(researchRaw, span, host),
				host:    host,
			})
		}
	}
	declared = append(declared, hostsInNovelCommandSources(in.CLIDir, features)...)
	return declared
}

func hostsInNovelCommandSources(cliDir string, features []NovelFeature) []novelHostDecl {
	if cliDir == "" {
		return nil
	}
	cliFilesDir := filepath.Join(cliDir, "internal", "cli")
	entries, err := os.ReadDir(cliFilesDir)
	if err != nil {
		return nil
	}
	leaves := map[string]string{}
	for _, feature := range features {
		leaf := lastPathSegment(commandPath(feature.Command))
		if leaf == "" {
			continue
		}
		label := feature.Command
		if label == "" {
			label = feature.Name
		}
		leaves[leaf] = label
	}
	var declared []novelHostDecl
	contents := make(map[string]string, len(entries))
	var sources []novelSourceFile
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cliFilesDir, name))
		if err != nil {
			continue
		}
		content := string(data)
		contents[name] = content
		if !novelCommandSource(content, name, leaves) {
			continue
		}
		label := novelSourceFeature(content, leaves)
		declared = append(declared, hostsInGoSource(name, content, label)...)
		sources = append(sources, novelSourceFile{name: name, content: content, label: label})
	}
	declared = append(declared, hostsFromNovelHelpers(cliDir, cliFilesDir, contents, sources, leaves)...)
	return declared
}

func novelCommandSource(content, name string, leaves map[string]string) bool {
	// root.go defines the scaffold marker constant and is not a novel command.
	if name == "root.go" {
		return false
	}
	if commandMatchesNovelLeaf(content, leaves) {
		return true
	}
	// Hand-written commands and generated scaffolds carry this marker.
	return strings.Contains(content, "pp:novel-scaffold") || strings.Contains(content, "pp:novel")
}

func commandMatchesNovelLeaf(content string, leaves map[string]string) bool {
	for _, match := range cobraUseLeafRe.FindAllStringSubmatch(content, -1) {
		if _, ok := leaves[match[1]]; ok {
			return true
		}
	}
	return false
}

func novelSourceFeature(content string, leaves map[string]string) string {
	for _, match := range cobraUseLeafRe.FindAllStringSubmatch(content, -1) {
		if label, ok := leaves[match[1]]; ok {
			return label
		}
	}
	return "novel feature"
}

func hostsInGoSource(filename, content, feature string) []novelHostDecl {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, content, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	return hostsInAST(fset, filename, file, feature)
}

func hostsInAST(fset *token.FileSet, filename string, node ast.Node, feature string) []novelHostDecl {
	if node == nil {
		return nil
	}
	var declared []novelHostDecl
	ast.Inspect(node, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val, unquoteErr := strconv.Unquote(lit.Value)
		if unquoteErr != nil {
			return true
		}
		line := fset.Position(lit.Pos()).Line
		for _, host := range hostsInLiteral(val) {
			declared = append(declared, novelHostDecl{
				feature: feature,
				file:    filename,
				line:    line,
				host:    host,
			})
		}
		return true
	})
	return declared
}

func hostsInLiteral(val string) []string {
	var hosts []string
	if host := bareHostname(val); host != "" {
		hosts = append(hosts, host)
	}
	hosts = append(hosts, urlHosts(val)...)
	return uniqueHosts(hosts)
}

func hostsInProse(text string) []string {
	// Absolute URLs are host context. Bare dotted identifiers count only when
	// they are real ICANN hostnames, so prose like settings.production does not.
	return uniqueHosts(append(urlHosts(text), proseHosts(text)...))
}

func urlHosts(text string) []string {
	var hosts []string
	for _, match := range absoluteURLRe.FindAllString(text, -1) {
		match = strings.TrimRight(match, ".,;")
		if host := hostFromURL(match); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func proseHosts(text string) []string {
	var hosts []string
	for _, match := range proseHostRe.FindAllString(text, -1) {
		if !icannHostname(match) {
			continue
		}
		if host := bareHostname(match); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func icannHostname(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	suffix, icann := publicsuffix.PublicSuffix(host)
	return icann && suffix != "" && suffix != host
}

func hostFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return normalizeNovelHost(parsed.Hostname())
}

func bareHostname(raw string) string {
	host := strings.ToLower(strings.TrimSpace(raw))
	if !bareHostRe.MatchString(host) {
		return ""
	}
	last := host[strings.LastIndex(host, ".")+1:]
	if bareHostFileExt[last] {
		return ""
	}
	return normalizeNovelHost(host)
}

func normalizeNovelHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if host == "" || strings.Contains(host, "{") || strings.Contains(host, "}") {
		return ""
	}
	switch host {
	case "localhost", "::1":
		return ""
	}
	if strings.HasPrefix(host, "127.") {
		return ""
	}
	return host
}

func uniqueHosts(hosts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, host := range hosts {
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
	}
	return out
}

type novelFeatureSpan struct {
	start int
	body  []byte
}

func novelFeatureSpans(raw []byte) []novelFeatureSpan {
	key := []byte(`"novel_features"`)
	keyAt := bytes.Index(raw, key)
	if keyAt < 0 {
		return nil
	}
	rel := bytes.IndexByte(raw[keyAt+len(key):], '[')
	if rel < 0 {
		return nil
	}
	start := keyAt + len(key) + rel
	dec := json.NewDecoder(bytes.NewReader(raw[start:]))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('[') {
		return nil
	}
	var spans []novelFeatureSpan
	for dec.More() {
		off := int(dec.InputOffset())
		var msg json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			break
		}
		abs := start + off
		for abs < len(raw) && (raw[abs] == ' ' || raw[abs] == '\n' || raw[abs] == '\r' || raw[abs] == '\t') {
			abs++
		}
		spans = append(spans, novelFeatureSpan{start: abs, body: append([]byte(nil), msg...)})
	}
	return spans
}

func takeFeatureSpan(spans []novelFeatureSpan, used map[int]bool, feature NovelFeature) novelFeatureSpan {
	for i, span := range spans {
		if used[i] {
			continue
		}
		var got NovelFeature
		if err := json.Unmarshal(span.body, &got); err != nil {
			continue
		}
		if got.Name != feature.Name || got.Command != feature.Command {
			continue
		}
		used[i] = true
		return span
	}
	return novelFeatureSpan{}
}

func hostLineInSpan(raw []byte, span novelFeatureSpan, host string) int {
	if len(span.body) == 0 || host == "" {
		return 0
	}
	idx := strings.Index(strings.ToLower(string(span.body)), strings.ToLower(host))
	if idx < 0 {
		return 0
	}
	abs := span.start + idx
	if abs < 0 || abs > len(raw) {
		return 0
	}
	return 1 + bytes.Count(raw[:abs], []byte("\n"))
}

func observedNovelHosts(in NovelHostInput) map[string]struct{} {
	observed := map[string]struct{}{}
	addHost := func(host string) {
		if host = normalizeNovelHost(host); host != "" {
			observed[host] = struct{}{}
		}
	}
	addSpec := func(spec *apispec.APISpec) {
		if spec == nil {
			return
		}
		for _, host := range urlHosts(spec.BaseURL) {
			addHost(host)
		}
		for _, host := range specBaseHosts(spec) {
			addHost(host)
		}
	}
	addSpec(in.Spec)
	for _, specPath := range in.SpecPaths {
		collectSpecNeighborhood(specPath, addHost)
	}
	addTrafficHosts(in.Traffic, addHost)
	for _, page := range in.DiscoveryPages {
		for _, host := range urlHosts(page) {
			addHost(host)
		}
	}
	for _, root := range novelHostArtifactRoots(in) {
		collectObservedHostFiles(root, addHost)
	}
	return observed
}

func addTrafficHosts(analysis *browsersniff.TrafficAnalysis, addHost func(string)) {
	if analysis == nil {
		return
	}
	for _, host := range urlHosts(analysis.Summary.TargetURL) {
		addHost(host)
	}
	for host := range analysis.Summary.HostDistribution {
		addHost(host)
	}
	for _, secondary := range analysis.SecondaryHosts {
		addHost(secondary.Host)
	}
	for _, cluster := range analysis.EndpointClusters {
		addHost(cluster.Host)
		for _, evidence := range cluster.Evidence {
			addHost(evidence.Host)
		}
	}
}

func specBaseHosts(spec *apispec.APISpec) []string {
	var hosts []string
	var walk func(resource apispec.Resource)
	walk = func(resource apispec.Resource) {
		hosts = append(hosts, urlHosts(resource.BaseURL)...)
		for _, endpoint := range resource.Endpoints {
			hosts = append(hosts, urlHosts(endpoint.BaseURL)...)
			hosts = append(hosts, urlHosts(endpoint.Path)...)
		}
		for _, sub := range resource.SubResources {
			walk(sub)
		}
	}
	for _, resource := range spec.Resources {
		walk(resource)
	}
	for _, tier := range spec.TierRouting.Tiers {
		hosts = append(hosts, urlHosts(tier.BaseURL)...)
	}
	return hosts
}

func novelHostArtifactRoots(in NovelHostInput) []string {
	var roots []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		if !info.IsDir() {
			path = filepath.Dir(path)
		}
		if slices.Contains(roots, path) {
			return
		}
		roots = append(roots, path)
	}
	add(in.ResearchDir)
	if in.ResearchDir != "" {
		add(filepath.Join(in.ResearchDir, "discovery"))
		add(filepath.Join(filepath.Dir(in.ResearchDir), "discovery"))
		add(filepath.Join(filepath.Dir(in.ResearchDir), "research"))
	}
	if in.CLIDir != "" {
		for _, name := range []string{"spec.yaml", "spec.yml", "spec.json"} {
			add(filepath.Join(in.CLIDir, name))
		}
	}
	return roots
}

func collectSpecNeighborhood(specPath string, addHost func(string)) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return
	}
	collectHostArtifact(filepath.Base(specPath), specPath, data, addHost)
	if samples := samplesDirForSpec(specPath); samples != "" {
		collectObservedHostFiles(samples, addHost)
	}
	dir := filepath.Dir(specPath)
	base := filepath.Base(specPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" || stem == "." {
		stem = "traffic"
	}
	for _, name := range []string{
		stem + "-traffic-analysis.json",
		"traffic-analysis.json",
		"observed-urls.txt",
		"documented-urls.txt",
		"urls.txt",
		"observed-hosts.txt",
		"browser-sniff-report.md",
		"crowd-browser-sniff-report.md",
	} {
		sidecarPath := filepath.Join(dir, name)
		sidecar, readErr := os.ReadFile(sidecarPath)
		if readErr != nil {
			continue
		}
		collectHostArtifact(name, sidecarPath, sidecar, addHost)
	}
}

func samplesDirForSpec(specPath string) string {
	dir := filepath.Dir(specPath)
	base := filepath.Base(specPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" || stem == "." {
		return ""
	}
	return filepath.Join(dir, stem+"-samples")
}

func collectObservedHostFiles(root string, addHost func(string)) {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			switch name {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !observedHostArtifact(name, path) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		collectHostArtifact(name, path, data, addHost)
		return nil
	})
}

func collectHostArtifact(name, path string, data []byte, addHost func(string)) {
	slash := filepath.ToSlash(path)
	if strings.Contains(slash, "-samples/") {
		for _, host := range hostsFromSampleRawURL(data) {
			addHost(host)
		}
		return
	}
	if documentedURLListNames[name] {
		for _, host := range urlHosts(string(data)) {
			addHost(host)
		}
		return
	}
	if strings.HasSuffix(name, "-traffic-analysis.json") || name == "traffic-analysis.json" {
		var analysis browsersniff.TrafficAnalysis
		if err := json.Unmarshal(data, &analysis); err != nil {
			return
		}
		addTrafficHosts(&analysis, addHost)
		return
	}
	for _, host := range hostsFromSpecBytes(data, path) {
		addHost(host)
	}
	for _, host := range hostsFromStructuredFields(data) {
		addHost(host)
	}
}

func hostsFromSampleRawURL(data []byte) []string {
	var sample struct {
		RawURL string `json:"raw_url"`
	}
	if err := json.Unmarshal(data, &sample); err != nil {
		return nil
	}
	return urlHosts(sample.RawURL)
}

func observedHostArtifact(name, path string) bool {
	if documentedURLListNames[name] {
		return true
	}
	if strings.HasSuffix(name, "-traffic-analysis.json") || name == "traffic-analysis.json" {
		return true
	}
	if strings.Contains(filepath.ToSlash(path), "-samples/") {
		return strings.HasSuffix(name, ".json")
	}
	switch name {
	case "spec.yaml", "spec.yml", "spec.json":
		return true
	}
	return strings.Contains(name, "browser-sniff-spec")
}

func hostsFromSpecBytes(data []byte, path string) []string {
	var parsed *apispec.APISpec
	var err error
	if isInternalYAMLSpec(data) {
		parsed, err = apispec.ParseBytes(data)
	} else if looksLikeOpenAPI(data) {
		parsed, err = openapiparser.ParseWithOptions(data, openapiparser.ParseOptions{Path: path, Lenient: true})
	}
	if err != nil || parsed == nil {
		return nil
	}
	hosts := urlHosts(parsed.BaseURL)
	return append(hosts, specBaseHosts(parsed)...)
}

func looksLikeOpenAPI(data []byte) bool {
	text := string(data)
	return strings.Contains(text, "openapi:") || strings.Contains(text, `"openapi"`) || strings.Contains(text, "swagger:") || strings.Contains(text, `"swagger"`)
}

// structuredHostSkipKeys are prose and payload containers. URLs inside them
// are not servers, base URLs, or captured request URLs.
var structuredHostSkipKeys = map[string]bool{
	"contact": true, "default": true, "description": true, "enum": true,
	"example": true, "examples": true, "externalDocs": true, "external_docs": true,
	"license": true, "pattern": true, "request_body": true, "request_headers": true,
	"response_body": true, "response_headers": true, "summary": true,
	"termsOfService": true, "title": true,
}

func hostsFromStructuredFields(data []byte) []string {
	node, err := decodeLooseDocument(data)
	if err != nil || node == nil {
		return nil
	}
	var hosts []string
	walkStructuredHosts(node, func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if host := hostFromURL(raw); host != "" {
			hosts = append(hosts, host)
			return
		}
		if host := bareHostname(raw); host != "" {
			hosts = append(hosts, host)
		}
	})
	return uniqueHosts(hosts)
}

func decodeLooseDocument(data []byte) (any, error) {
	var node any
	if json.Valid(data) {
		if err := json.Unmarshal(data, &node); err != nil {
			return nil, err
		}
		return node, nil
	}
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	return node, nil
}

func walkStructuredHosts(node any, add func(string)) {
	switch n := node.(type) {
	case map[string]any:
		walkStringMapHosts(n, add)
	case map[any]any:
		converted := make(map[string]any, len(n))
		for key, child := range n {
			name, ok := key.(string)
			if !ok {
				continue
			}
			converted[name] = child
		}
		walkStringMapHosts(converted, add)
	case []any:
		for _, child := range n {
			walkStructuredHosts(child, add)
		}
	}
}

func walkStringMapHosts(node map[string]any, add func(string)) {
	if servers, ok := node["servers"]; ok {
		addServerURLs(servers, add)
	}
	if base, ok := novelHostString(node["base_url"]); ok {
		add(base)
	}
	if _, swagger := node["swagger"]; swagger {
		if host, ok := novelHostString(node["host"]); ok {
			addSwaggerHost(host, node, add)
		}
	}
	for key, child := range node {
		if key == "servers" || structuredHostSkipKeys[key] {
			continue
		}
		walkStructuredHosts(child, add)
	}
}

func addServerURLs(servers any, add func(string)) {
	list, ok := servers.([]any)
	if !ok {
		return
	}
	for _, item := range list {
		fields, ok := novelHostStringMap(item)
		if !ok {
			continue
		}
		if raw, ok := novelHostString(fields["url"]); ok {
			add(raw)
		}
	}
}

func addSwaggerHost(host string, node map[string]any, add func(string)) {
	host = strings.TrimSpace(host)
	if host == "" {
		return
	}
	schemes, _ := node["schemes"].([]any)
	if len(schemes) == 0 {
		add("https://" + host)
		return
	}
	for _, scheme := range schemes {
		name, ok := novelHostString(scheme)
		if !ok || (name != "http" && name != "https") {
			continue
		}
		add(name + "://" + host)
	}
}

func novelHostStringMap(node any) (map[string]any, bool) {
	switch n := node.(type) {
	case map[string]any:
		return n, true
	case map[any]any:
		converted := make(map[string]any, len(n))
		for key, child := range n {
			name, ok := key.(string)
			if !ok {
				return nil, false
			}
			converted[name] = child
		}
		return converted, true
	default:
		return nil, false
	}
}

func novelHostString(node any) (string, bool) {
	text, ok := node.(string)
	return text, ok
}

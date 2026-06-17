package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
)

type novelFeatureCommandRender struct {
	Owner          string
	Ident          string
	Use            string
	Short          string
	CommandPath    string
	ReadOnlyString string
	HasPositional  bool
	Feature        bool
	Flags          []novelFeatureFlagRender
	Children       []novelFeatureChildRender
}

type novelFeatureFlagRender struct {
	Name        string
	VarName     string
	Kind        string
	Description string
}

type novelFeatureChildRender struct {
	Ident string
}

type novelFeatureTestRender struct {
	Owner       string
	Ident       string
	SkipMessage string
}

type novelFeatureStubNode struct {
	segment  string
	path     []string
	feature  *NovelFeature
	children map[string]*novelFeatureStubNode
}

// renderNovelFeatureStubs emits verify-friendly Cobra scaffolds for planned
// transcendence commands. The stubs make the advertised command paths resolvable
// before the Phase 3 worker fills in API/store-backed behavior.
func (g *Generator) renderNovelFeatureStubs() ([]novelFeatureCommandRender, error) {
	root := g.buildNovelFeatureStubTree()
	if len(root.children) == 0 {
		return nil, nil
	}

	generatedPaths := g.generatedCommandPaths()
	var roots []novelFeatureCommandRender
	for _, child := range sortedNovelChildren(root) {
		rendered, err := g.renderNovelFeatureNode(child, generatedPaths)
		if err != nil {
			return nil, err
		}
		if rendered != nil {
			roots = append(roots, *rendered)
		}
	}
	return roots, nil
}

func (g *Generator) buildNovelFeatureStubTree() *novelFeatureStubNode {
	root := &novelFeatureStubNode{children: map[string]*novelFeatureStubNode{}}
	for i := range g.NovelFeatures {
		feature := &g.NovelFeatures[i]
		parts := novelFeatureCommandParts(feature.Command)
		if len(parts) == 0 {
			continue
		}
		node := root
		for _, part := range parts {
			if node.children == nil {
				node.children = map[string]*novelFeatureStubNode{}
			}
			child := node.children[part]
			if child == nil {
				child = &novelFeatureStubNode{
					segment:  part,
					path:     append(append([]string(nil), node.path...), part),
					children: map[string]*novelFeatureStubNode{},
				}
				node.children[part] = child
			}
			node = child
		}
		if node.feature == nil {
			node.feature = feature
		}
	}
	return root
}

func (g *Generator) novelFeatureChildrenByParent() map[string][]novelFeatureChildRender {
	root := g.buildNovelFeatureStubTree()
	generatedPaths := g.generatedCommandPaths()
	out := map[string][]novelFeatureChildRender{}
	var walk func(*novelFeatureStubNode)
	walk = func(node *novelFeatureStubNode) {
		children := sortedNovelChildren(node)
		if len(children) > 0 && len(node.path) > 0 {
			parentPath := strings.Join(node.path, " ")
			for _, child := range children {
				if g.novelFeatureStubShouldSkip(child.path, generatedPaths) {
					continue
				}
				out[parentPath] = append(out[parentPath], novelFeatureChildRender{Ident: novelFeatureStubIdent(child.path)})
			}
		}
		for _, child := range children {
			walk(child)
		}
	}
	for _, child := range sortedNovelChildren(root) {
		walk(child)
	}
	return out
}

func (g *Generator) renderNovelFeatureNode(node *novelFeatureStubNode, generatedPaths map[string]struct{}) (*novelFeatureCommandRender, error) {
	var renderedChildren []novelFeatureChildRender
	for _, child := range sortedNovelChildren(node) {
		rendered, err := g.renderNovelFeatureNode(child, generatedPaths)
		if err != nil {
			return nil, err
		}
		if rendered != nil {
			renderedChildren = append(renderedChildren, novelFeatureChildRender{Ident: rendered.Ident})
		}
	}

	data := g.novelFeatureCommandData(node)
	data.Children = renderedChildren
	outPath := filepath.Join("internal", "cli", novelFeatureStubFileName(node.path))
	if g.novelFeatureStubShouldSkipGenerated(node.path, generatedPaths) {
		fmt.Fprintf(os.Stderr, "warning: novel feature command %q maps to generated command path; skipping novel stub\n", data.CommandPath)
		return nil, nil
	}
	if exists, hasConstructor := g.novelFeatureStubExistingFileConstructorStatus(node.path); exists {
		if !hasConstructor {
			fmt.Fprintf(os.Stderr, "warning: novel feature command %q maps to existing %s without expected constructor %s; skipping novel stub\n", data.CommandPath, outPath, novelFeatureStubConstructorName(node.path))
			return nil, nil
		}
		fmt.Fprintf(os.Stderr, "warning: novel feature command %q maps to existing %s; leaving existing file unchanged\n", data.CommandPath, outPath)
		return &data, nil
	}
	if err := g.renderTemplate("novel_feature_command.go.tmpl", outPath, data); err != nil {
		return nil, fmt.Errorf("rendering novel feature command %s: %w", data.CommandPath, err)
	}
	if node.feature != nil {
		testPath := filepath.Join("internal", "cli", strings.TrimSuffix(novelFeatureStubFileName(node.path), ".go")+"_test.go")
		testData := novelFeatureTestRender{
			Owner:       g.Spec.Owner,
			Ident:       data.Ident,
			SkipMessage: "TODO: implement table-driven tests for " + data.CommandPath,
		}
		if err := g.renderTemplate("novel_feature_command_test.go.tmpl", testPath, testData); err != nil {
			return nil, fmt.Errorf("rendering novel feature command test %s: %w", data.CommandPath, err)
		}
	}

	return &data, nil
}

func (g *Generator) novelFeatureStubShouldSkip(parts []string, generatedPaths map[string]struct{}) bool {
	return g.novelFeatureStubShouldSkipGenerated(parts, generatedPaths) || g.novelFeatureStubExistingFileMissingConstructor(parts)
}

func (g *Generator) novelFeatureStubShouldSkipGenerated(parts []string, generatedPaths map[string]struct{}) bool {
	return novelFeatureStubCollidesWithGeneratedCommand(parts, generatedPaths)
}

func (g *Generator) novelFeatureStubExistingFileMissingConstructor(parts []string) bool {
	exists, hasConstructor := g.novelFeatureStubExistingFileConstructorStatus(parts)
	return exists && !hasConstructor
}

func (g *Generator) novelFeatureStubExistingFileConstructorStatus(parts []string) (bool, bool) {
	outPath := filepath.Join("internal", "cli", novelFeatureStubFileName(parts))
	data, err := os.ReadFile(filepath.Join(g.OutputDir, outPath))
	if err != nil {
		return false, false
	}
	constructor := "func " + novelFeatureStubConstructorName(parts) + "("
	return true, strings.Contains(string(data), constructor)
}

func novelFeatureStubConstructorName(parts []string) string {
	return "newNovel" + novelFeatureStubIdent(parts) + "Cmd"
}

func (g *Generator) novelFeatureCommandData(node *novelFeatureStubNode) novelFeatureCommandRender {
	commandPath := strings.Join(node.path, " ")
	short := "TODO: implement " + commandPath
	use := node.segment
	hasPositional := false
	readOnly := true
	var flags []novelFeatureFlagRender
	if node.feature != nil {
		short = naming.OneLine(node.feature.Description)
		if short == "" {
			short = naming.OneLine(node.feature.Name)
		}
		if short == "" {
			short = "TODO: implement " + commandPath
		}
		readOnly = novelFeatureReadOnly(*node.feature)
		flags = novelFeatureFlags(*node.feature, node.path, g.Spec.Name)
		hasPositional = novelFeatureHasPositional(node.feature.Command)
		use = novelFeatureUse(node.segment, node.feature.Command)
	} else if len(node.children) > 0 {
		short = novelFeatureParentShort(node)
	}
	readOnlyString := "true"
	if !readOnly {
		readOnlyString = "false"
	}
	return novelFeatureCommandRender{
		Owner:          g.Spec.Owner,
		Ident:          novelFeatureStubIdent(node.path),
		Use:            use,
		Short:          short,
		CommandPath:    commandPath,
		ReadOnlyString: readOnlyString,
		HasPositional:  hasPositional,
		Feature:        node.feature != nil,
		Flags:          flags,
	}
}

func novelFeatureStubCollidesWithGeneratedCommand(parts []string, generatedPaths map[string]struct{}) bool {
	_, ok := generatedPaths[novelFeatureCommandKey(parts)]
	return ok
}

func (g *Generator) generatedCommandPaths() map[string]struct{} {
	paths := map[string]struct{}{}
	add := func(parts ...string) {
		paths[novelFeatureCommandKey(parts)] = struct{}{}
	}
	for _, promoted := range g.PromotedCommands {
		add(promoted.PromotedName)
	}
	for name, resource := range g.Spec.Resources {
		if !g.PromotedResourceNames[name] {
			add(name)
		}
		for eName := range resource.Endpoints {
			if g.PromotedEndpointNames[name] == eName {
				continue
			}
			add(name, eName)
		}
		for subName, subResource := range resource.SubResources {
			add(name, subName)
			for eName := range subResource.Endpoints {
				add(name, subName, eName)
			}
		}
	}
	return paths
}

func novelFeatureCommandKey(parts []string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		normalized = append(normalized, toKebab(part))
	}
	return strings.Join(normalized, " ")
}

func sortedNovelChildren(node *novelFeatureStubNode) []*novelFeatureStubNode {
	if len(node.children) == 0 {
		return nil
	}
	keys := make([]string, 0, len(node.children))
	for key := range node.children {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*novelFeatureStubNode, 0, len(keys))
	for _, key := range keys {
		out = append(out, node.children[key])
	}
	return out
}

func novelFeatureCommandParts(command string) []string {
	parts := make([]string, 0)
	for token := range strings.FieldsSeq(strings.ToLower(command)) {
		token = strings.Trim(token, `"'`)
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "-") || novelFeatureTokenIsPositional(token) {
			break
		}
		parts = append(parts, toKebab(token))
	}
	return parts
}

func novelFeatureHasPositional(command string) bool {
	// Only tokens before the first flag are positional arguments; a `<`/`[`
	// inside a flag-value hint (e.g. "search --filter [active|inactive]") is
	// not a positional and must not re-arm the args-based Help guard (#2592).
	for token := range strings.FieldsSeq(command) {
		token = strings.Trim(token, `"'`)
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "-") {
			break
		}
		if novelFeatureTokenIsPositional(token) {
			return true
		}
	}
	return false
}

func novelFeatureTokenIsPositional(token string) bool {
	return strings.Contains(token, "<") || strings.Contains(token, "[")
}

func novelFeatureUse(segment, command string) string {
	var positional []string
	for token := range strings.FieldsSeq(command) {
		token = strings.Trim(token, `"'`)
		if token == "" {
			continue
		}
		// Stop at the first flag: placeholders after a flag are value hints,
		// not positional args, and must not leak into the cobra Use string.
		if strings.HasPrefix(token, "-") {
			break
		}
		if novelFeatureTokenIsPositional(token) {
			positional = append(positional, token)
		}
	}
	if len(positional) == 0 {
		return segment
	}
	return strings.Join(append([]string{segment}, positional...), " ")
}

func novelFeatureParentShort(node *novelFeatureStubNode) string {
	// Only called from the else-if len(node.children) > 0 branch, so children
	// is always non-empty here.
	children := sortedNovelChildren(node)
	leafNames := make([]string, 0, len(children))
	for _, child := range children {
		leafNames = append(leafNames, child.segment)
	}
	return fmt.Sprintf("%s subcommands: %s", node.segment, strings.Join(leafNames, ", "))
}

func novelFeatureStubIdent(parts []string) string {
	return commandIdent(parts...)
}

func novelFeatureStubFileName(parts []string) string {
	safeParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		safeParts = append(safeParts, strings.ReplaceAll(toKebab(part), "-", "_"))
	}
	if len(safeParts) == 0 {
		return "novel_feature.go"
	}
	return strings.Join(safeParts, "_") + ".go"
}

func novelFeatureReadOnly(feature NovelFeature) bool {
	text := strings.ToLower(strings.Join([]string{
		feature.Description,
		feature.WhyItMatters,
	}, " "))
	words := strings.FieldsFunc(text, func(r rune) bool {
		return r < 'a' || r > 'z'
	})
	for _, verb := range []string{"create", "call", "run", "delete", "replay", "define", "batch"} {
		if slices.Contains(words, verb) {
			return false
		}
	}
	return true
}

func novelFeatureFlags(feature NovelFeature, commandPath []string, apiName string) []novelFeatureFlagRender {
	if strings.TrimSpace(feature.Example) == "" {
		return nil
	}
	tokens, err := shellargs.Split(feature.Example)
	if err != nil {
		return nil
	}
	tokens = dropNovelFeatureExamplePrefix(tokens, commandPath, apiName)

	type flagInfo struct {
		name      string
		hasValue  bool
		repeated  bool
		firstSeen int
	}
	infos := map[string]*flagInfo{}
	order := 0
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if !strings.HasPrefix(token, "--") || token == "--" {
			continue
		}
		raw := strings.TrimPrefix(token, "--")
		name, value, hasInlineValue := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		if name == "" || isNovelFeatureFrameworkFlag(name) {
			if !hasInlineValue && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				i++
			}
			continue
		}
		hasValue := hasInlineValue || value != ""
		if !hasValue && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
			hasValue = true
			i++
		}
		info := infos[name]
		if info == nil {
			info = &flagInfo{name: name, firstSeen: order}
			infos[name] = info
			order++
		} else {
			info.repeated = true
		}
		info.hasValue = info.hasValue || hasValue
	}

	ordered := make([]*flagInfo, 0, len(infos))
	for _, info := range infos {
		ordered = append(ordered, info)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].firstSeen < ordered[j].firstSeen
	})
	flags := make([]novelFeatureFlagRender, 0, len(ordered))
	seenVars := map[string]int{}
	for _, info := range ordered {
		kind := "bool"
		if info.hasValue {
			kind = "string"
		}
		if info.repeated || isLikelyStringSliceFlag(info.name) {
			kind = "stringSlice"
		}
		varName := lowerFirst(commandIdent("flag", info.name))
		if count := seenVars[varName]; count > 0 {
			seenVars[varName] = count + 1
			varName = fmt.Sprintf("%s%d", varName, count+1)
		} else {
			seenVars[varName] = 1
		}
		flags = append(flags, novelFeatureFlagRender{
			Name:        info.name,
			VarName:     varName,
			Kind:        kind,
			Description: "TODO: describe --" + info.name,
		})
	}
	return flags
}

func dropNovelFeatureExamplePrefix(tokens []string, commandPath []string, apiName string) []string {
	if len(tokens) == 0 {
		return tokens
	}
	if looksLikeNovelFeatureBinary(tokens[0], apiName) {
		tokens = tokens[1:]
	}
	if len(tokens) >= len(commandPath) {
		matches := true
		for i, part := range commandPath {
			if toKebab(strings.ToLower(tokens[i])) != part {
				matches = false
				break
			}
		}
		if matches {
			return tokens[len(commandPath):]
		}
	}
	return tokens
}

func looksLikeNovelFeatureBinary(token, apiName string) bool {
	base := filepath.Base(strings.Trim(token, `"'`))
	if strings.HasSuffix(base, "-pp-cli") {
		return true
	}
	return base == naming.CLI(apiName)
}

func isNovelFeatureFrameworkFlag(name string) bool {
	_, ok := map[string]struct{}{
		"agent":                 {},
		"allow-partial-failure": {},
		"compact":               {},
		"config":                {},
		"csv":                   {},
		"data-source":           {},
		"deliver":               {},
		"dry-run":               {},
		"human-friendly":        {},
		"idempotent":            {},
		"ignore-missing":        {},
		"json":                  {},
		"max-age":               {},
		"no-cache":              {},
		"no-color":              {},
		"no-input":              {},
		"no-learn":              {},
		"plain":                 {},
		"profile":               {},
		"quiet":                 {},
		"rate-limit":            {},
		"select":                {},
		"throttle-mode":         {},
		"timeout":               {},
		"yes":                   {},
	}[name]
	return ok
}

func isLikelyStringSliceFlag(name string) bool {
	switch name {
	case "tag", "tags", "label", "labels", "filter", "filters", "include", "exclude":
		return true
	default:
		return false
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

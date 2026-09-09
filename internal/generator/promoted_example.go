package generator

import (
	"fmt"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func (g *Generator) validatePromotedExamples() error {
	if g == nil {
		return nil
	}
	var errs []string
	for _, pc := range g.PromotedCommands {
		if _, err := g.resolvePromotedExample(pc.PromotedName, pc.EndpointName, pc.Endpoint); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("promoted command Example does not match the registered command tree:\n  %s", strings.Join(errs, "\n  "))
}

func (g *Generator) resolvePromotedExample(promotedName, endpointName string, endpoint spec.Endpoint) (string, error) {
	promotedName = toKebab(promotedName)
	raw := strings.TrimSpace(endpoint.Example)
	if raw == "" {
		return g.synthesizedPromotedExample(promotedName, endpoint), nil
	}
	if g.promotedExampleMatchesRegistered(promotedName, endpoint, raw) {
		return endpoint.Example, nil
	}
	rewritten, ok := g.rewritePromotedExampleToRegistered(promotedName, endpointName, endpoint, raw)
	if ok && g.promotedExampleMatchesRegistered(promotedName, endpoint, rewritten) {
		return rewritten, nil
	}
	return "", fmt.Errorf("resource %q: example %q names a command or flag that is not registered; use the collapsed leaf form",
		promotedName, raw)
}

func (g *Generator) synthesizedPromotedExample(promotedName string, endpoint spec.Endpoint) string {
	if line, ok := g.narrativeExampleLine([]string{promotedName}, endpoint); ok {
		return line
	}
	parts := []string{naming.CLI(g.Spec.Name), promotedName}
	parts = append(parts, commandExampleArgParts(endpoint)...)
	return "  " + strings.Join(parts, " ")
}

func (g *Generator) synthesizedRunnablePromotedExample(promotedName string, endpoint spec.Endpoint) string {
	if line, ok := g.narrativeExampleLine([]string{promotedName}, endpoint); ok {
		return runnableExampleLine(line)
	}
	if !requiredInputsAreDerivable(endpoint) {
		return ""
	}
	parts := []string{naming.CLI(g.Spec.Name), promotedName}
	parts = append(parts, commandExampleArgParts(endpoint)...)
	return runnableExampleLine("  " + strings.Join(parts, " "))
}

func (g *Generator) promotedExampleMatchesRegistered(promotedName string, endpoint spec.Endpoint, example string) bool {
	cliName := ""
	if g != nil && g.Spec != nil {
		cliName = naming.CLI(g.Spec.Name)
	}
	path, tail, ok := promotedExampleCommandTail(cliName, example)
	if !ok {
		return false
	}
	if len(path) == 0 || path[0] != promotedName {
		return false
	}
	return narrativeTailMatches(append(path[1:], tail...), endpoint)
}

func (g *Generator) rewritePromotedExampleToRegistered(promotedName, endpointName string, endpoint spec.Endpoint, example string) (string, bool) {
	cliName := ""
	if g != nil && g.Spec != nil {
		cliName = naming.CLI(g.Spec.Name)
	}
	tokens, err := shellargs.Split(strings.TrimSpace(example))
	if err != nil || len(tokens) == 0 {
		return "", false
	}
	start := 0
	if tokens[0] == cliName {
		start = 1
	}
	if start >= len(tokens) {
		return "", false
	}
	cmdStartRel, ok := narrativeCommandStart(tokens[start:])
	if !ok {
		return "", false
	}
	cmdStart := start + cmdStartRel
	if cmdStart >= len(tokens) || tokens[cmdStart] != promotedName {
		return "", false
	}
	rewritten := append([]string{}, tokens...)
	collapsed := toKebab(endpointName)
	alias := strings.TrimSpace(endpoint.Alias)
	next := cmdStart + 1
	if next < len(rewritten) && !strings.HasPrefix(rewritten[next], "-") {
		extra := rewritten[next]
		if extra == collapsed || (alias != "" && extra == alias) {
			rewritten = append(rewritten[:next], rewritten[next+1:]...)
		}
	}
	flagMap := promotedExampleFlagRewrites(endpoint)
	for i := cmdStart + 1; i < len(rewritten); i++ {
		tok := rewritten[i]
		if !strings.HasPrefix(tok, "--") || tok == "--" {
			continue
		}
		name, inline, hasInline := strings.Cut(strings.TrimPrefix(tok, "--"), "=")
		if public, mapped := flagMap[name]; mapped && public != name {
			if hasInline {
				rewritten[i] = "--" + public + "=" + inline
			} else {
				rewritten[i] = "--" + public
			}
		}
	}
	return "  " + shellargs.Join(rewritten), true
}

func promotedExampleCommandTail(cliName, example string) (path []string, tail []string, ok bool) {
	tokens, err := shellargs.Split(strings.TrimSpace(example))
	if err != nil || len(tokens) == 0 {
		return nil, nil, false
	}
	start := 0
	if tokens[0] == cliName {
		start = 1
	}
	if start >= len(tokens) {
		return nil, nil, false
	}
	cmdStart, found := narrativeCommandStart(tokens[start:])
	if !found {
		return nil, nil, false
	}
	rest := tokens[start+cmdStart:]
	for i, tok := range rest {
		if strings.HasPrefix(tok, "-") {
			return rest[:i], rest[i:], true
		}
	}
	return rest, nil, true
}

func promotedExampleFlagRewrites(endpoint spec.Endpoint) map[string]string {
	out := map[string]string{}
	add := func(p spec.Param) {
		if p.Positional {
			return
		}
		public := publicFlagName(p)
		if public == "" {
			return
		}
		candidates := []string{p.Name, naming.FlagName(p.Name), strings.ReplaceAll(p.Name, "_", "-")}
		if ident := strings.TrimSpace(p.IdentName); ident != "" {
			candidates = append(candidates, ident, naming.FlagName(ident))
		}
		for _, cand := range candidates {
			cand = strings.TrimSpace(cand)
			if cand != "" && cand != public {
				out[cand] = public
			}
		}
	}
	for _, p := range endpoint.Params {
		add(p)
	}
	for _, p := range endpoint.Body {
		add(p)
	}
	return out
}

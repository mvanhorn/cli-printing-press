package pipeline

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// pathParamProbe identifies a cobra leaf command (by its full path from
// root) whose Usage line carries one or more <placeholder> positional
// tokens. The existing verify matrix only probes top-level commands, so
// nested leaves never get their positionals exercised; a generator-
// emitted args[]-indexing bug therefore ships silently with verify
// reporting 100% pass.
type pathParamProbe struct {
	path         []string
	placeholders []string
	flags        pathParamFlagSet
}

// pathParamFlagSet is the command's registered flags, taken from its
// `--help` Flags and Global Flags blocks. Long names are stored without
// leading dashes.
type pathParamFlagSet struct {
	long  map[string]struct{}
	short map[string]struct{}
}

// pathParamRequiredSentinelRe matches the usage-error sentinel that
// generator-emitted positional validators print when their (potentially
// mis-indexed) `len(args) < N` guard fires: e.g. `Error: tagId is
// required`. Case-insensitive so generator wording drift does not blind
// the probe. A hit is not itself a failure: the subject token is checked
// against the command's registered flags below.
var pathParamRequiredSentinelRe = regexp.MustCompile(`(?i)\bis required\b`)

// pathParamRequiredTokenRe finds one subject token immediately before
// "is required". A flag may carry an `=` value, including a quoted value.
// The literal space in the sentinel regex keeps this scan on one line.
var pathParamRequiredTokenRe = regexp.MustCompile(`(?i)(?:` +
	`--[A-Za-z0-9][\w-]*(?:=(?:"[^"]*"|'[^']*'|` + "`[^`]*`" + `|[^\s,;]+)?)?` +
	`|-[A-Za-z0-9](?:=[^\s,;]+)?` +
	`|<[A-Za-z_][\w-]*>` +
	`|[A-Za-z_][\w-]*` +
	`)`)

// pathParamUsageRe captures the first content line under `Usage:`.
// Tolerant of multi-level command paths: it does not anchor on the
// number of leading words, so it works equally well on
// `cli sub <id>` and `cli a b c <id>` shapes.
var pathParamUsageRe = regexp.MustCompile(`(?m)^Usage:\s*\n\s+(.+)$`)

// pathParamProbeMaxDepth caps recursion when walking the cobra tree.
// Six levels covers every printed CLI in the library today by a wide
// margin and prevents runaway walks if a buggy --help renderer ever
// produces a cycle.
const pathParamProbeMaxDepth = 6

const pathParamProbeTimeout = 10 * time.Second

// PathParamProbeResult records one positional-binding probe.
// Positionals lists only the synthetic values injected for each
// placeholder; Command already carries the human-readable command path,
// so the two fields together describe the full invocation without
// duplicating the path in both places.
type PathParamProbeResult struct {
	Command     string   `json:"command"`
	Positionals []string `json:"positionals,omitempty"`
	Passed      bool     `json:"passed"`
	Reason      string   `json:"reason,omitempty"`
}

// discoverPathParamProbes walks the binary's cobra tree by recursively
// reading --help output and returns nested leaves (depth >= 2) whose
// Usage line has at least one <placeholder> token. Top-level (depth 1)
// commands are already exercised by inferPositionalArgs through the
// existing matrix, so the probe deliberately starts at depth 2 to avoid
// double-counting them in Total / Critical. Nodes whose --help fails
// are skipped rather than counted as failures (a non-responsive --help
// is its own bug, surfaced elsewhere).
//
// Only leaves (nodes whose --help advertises no children) are probed.
// A parent/group whose Usage happened to carry a <placeholder> token
// (e.g. `cli tags <filter> [command]`) would otherwise be invoked with
// the synthetic value as a subcommand, producing an "unknown command"
// exit that silently passes the sentinel check and inflates Total /
// Passed without testing anything meaningful.
func discoverPathParamProbes(binary string, env []string) []pathParamProbe {
	if binary == "" {
		return nil
	}
	var probes []pathParamProbe
	var walk func(path []string)
	walk = func(path []string) {
		if len(path) > pathParamProbeMaxDepth {
			return
		}

		helpOut, ok := readCommandHelp(binary, path, env)
		if !ok {
			return
		}

		children := parseHelpCommands(helpOut)

		if len(path) >= 2 && len(children) == 0 {
			if placeholders := extractPositionalsFromUsage(helpOut); len(placeholders) > 0 {
				probes = append(probes, pathParamProbe{
					path:         append([]string(nil), path...),
					placeholders: placeholders,
					flags:        pathParamFlagsFromHelp(helpOut),
				})
			}
		}

		for _, child := range children {
			childPath := append(append([]string(nil), path...), child.Name)
			walk(childPath)
		}
	}
	walk(nil)
	return probes
}

func readCommandHelp(binary string, path []string, env []string) (string, bool) {
	args := append(append([]string{}, path...), "--help")
	out, err := runCLIWithOutput(binary, args, env, pathParamProbeTimeout)
	if err != nil {
		return "", false
	}
	return string(out), true
}

// extractPositionalsFromUsage finds the Usage: line in a --help output
// and returns the positional placeholder names. Returns nil when no
// Usage line is found or no placeholders are present.
func extractPositionalsFromUsage(helpOut string) []string {
	m := pathParamUsageRe.FindStringSubmatch(helpOut)
	if m == nil {
		return nil
	}
	return extractPositionalPlaceholders(m[1])
}

// runPathParamProbes invokes each discovered placeholder-bearing command
// with synthetic positionals + `--dry-run` and records whether the
// binary rejected the call with an "is required" usage error. Only that
// failure mode counts: unrelated transient errors (auth, mock-server
// quirks, exit codes documented as success) are not failures so the
// probe does not introduce false positives on already-correct CLIs.
//
// `paramDefaults` is the spec author's Param.Default map; when set, a
// matching entry wins over the cross-domain canonicalargs registry and
// the per-name fallback. The resolution chain matches inferPositionalArgs
// so the probe uses the same synthetic value the top-level matrix uses.
func runPathParamProbes(binary string, env []string, paramDefaults map[string]string) []PathParamProbeResult {
	probes := discoverPathParamProbes(binary, env)
	if len(probes) == 0 {
		return nil
	}
	results := make([]PathParamProbeResult, 0, len(probes))
	for _, probe := range probes {
		positionals := make([]string, 0, len(probe.placeholders))
		for _, name := range probe.placeholders {
			positionals = append(positionals, resolvePositionalValue(name, paramDefaults))
		}
		invokeArgs := append([]string{}, probe.path...)
		invokeArgs = append(invokeArgs, positionals...)
		invokeArgs = append(invokeArgs, "--dry-run")

		out, err := runCLIWithOutput(binary, invokeArgs, env, pathParamProbeTimeout)
		result := PathParamProbeResult{
			Command:     strings.Join(probe.path, " "),
			Positionals: positionals,
			Passed:      true,
		}
		// Only the "missing positional" sentinel counts as a probe failure.
		// "is required" naming a flag this command registers (for example
		// "Error: --email is required") is a flag check, not a mis-indexed
		// positional guard. Other errors (auth, transient, intentional
		// non-zero) belong to other checks. Timeouts are inconclusive:
		// leave Passed=true so a hung --dry-run does not flip the verdict;
		// the existing matrix's 10s timeout flags the same command independently.
		if err != nil && pathParamRejectionIsPositionalBinding(string(out), probe.placeholders, probe.flags) && !isContextDeadlineErr(err) {
			result.Passed = false
			result.Reason = `command rejected the call with "is required" despite the supplied positional args; regenerate with the current generator (path-param positional binding fix)`
		}
		results = append(results, result)
	}
	return results
}

func isContextDeadlineErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// pathParamFlagsFromHelp reads the Flags and Global Flags blocks.
// Names in examples or usage prose are not registered.
func pathParamFlagsFromHelp(help string) pathParamFlagSet {
	set := pathParamFlagSet{
		long:  map[string]struct{}{},
		short: map[string]struct{}{},
	}
	for line := range strings.SplitSeq(extractFlagsSection(help), "\n") {
		decl, ok := parseLiveDogfoodFlagDecl(line)
		if !ok {
			continue
		}
		if decl.long != "" {
			set.long[strings.ToLower(decl.long)] = struct{}{}
		}
		if decl.shorthand != 0 {
			set.short[strings.ToLower(string(decl.shorthand))] = struct{}{}
		}
	}
	return set
}

// pathParamRejectionIsPositionalBinding reports whether output is the
// mis-indexed positional guard. A subject of "is required" that names a
// registered flag does not count. A "--" prefix is not enough: "user-id"
// and an unregistered "--user-id" still fail, and a bare name that is also
// a positional fails even when a flag shares that name.
func pathParamRejectionIsPositionalBinding(output string, placeholders []string, flags pathParamFlagSet) bool {
	if !pathParamRequiredSentinelRe.MatchString(output) {
		return false
	}
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimRight(line, "\r")
		rest := line
		for {
			loc := pathParamRequiredSentinelRe.FindStringIndex(rest)
			if loc == nil {
				break
			}
			for _, token := range pathParamRequiredSubjects(rest[:loc[0]]) {
				if !pathParamTokenNamesRegisteredFlag(token, placeholders, flags) {
					return true
				}
			}
			rest = rest[loc[1]:]
		}
	}
	return false
}

// pathParamRequiredSubjects returns the name tokens "is required" applies
// to: the last token, plus a trailing and/or list ("--email or user-id").
// Words before that list, including a flag mentioned only in passing, are
// not subjects. No parseable subject yields one empty token so the caller
// keeps the failure.
func pathParamRequiredSubjects(before string) []string {
	raw := pathParamRequiredTokenRe.FindAllStringIndex(before, -1)
	matches := make([][2]int, 0, len(raw))
	for _, match := range raw {
		// List words stay in the gap so "--email or user-id" is one subject list.
		word := strings.ToLower(before[match[0]:match[1]])
		if word == "and" || word == "or" {
			continue
		}
		matches = append(matches, [2]int{match[0], match[1]})
	}
	if len(matches) == 0 {
		return []string{""}
	}
	last := len(matches) - 1
	tail := before[matches[last][1]:]
	if strings.Trim(tail, " \t\r\n\"'`()[]{}.,:;!") != "" && !isPathParamListGap(tail) {
		return []string{""}
	}
	subjects := []string{before[matches[last][0]:matches[last][1]]}
	for i := last - 1; i >= 0; i-- {
		gap := before[matches[i][1]:matches[i+1][0]]
		if !isPathParamListGap(gap) {
			break
		}
		subjects = append(subjects, before[matches[i][0]:matches[i][1]])
	}
	return subjects
}

func isPathParamListGap(gap string) bool {
	// Whitespace alone is not a list. "Error:" keeps its colon, so the label
	// is not pulled in as another subject of "is required".
	gap = strings.Trim(gap, " \t\r\n\"'`()[]{}")
	if gap == "" {
		return false
	}
	fields := strings.FieldsFunc(gap, func(r rune) bool {
		return r == ',' || r == '/' || r == '|'
	})
	if len(fields) == 0 {
		return true
	}
	for _, field := range fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if field != "and" && field != "or" {
			return false
		}
	}
	return true
}

func pathParamTokenNamesRegisteredFlag(token string, placeholders []string, flags pathParamFlagSet) bool {
	if token == "" {
		return false
	}
	if strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">") {
		return false
	}
	if name, ok := pathParamLongFlagName(token); ok {
		_, registered := flags.long[strings.ToLower(name)]
		return registered
	}
	if name, ok := pathParamShortFlagName(token); ok {
		_, registered := flags.short[strings.ToLower(name)]
		return registered
	}
	bare := strings.ToLower(strings.Trim(token, "<>"))
	if bare == "" || pathParamNameIsPositional(bare, placeholders) {
		return false
	}
	_, registered := flags.long[bare]
	return registered
}

func pathParamLongFlagName(token string) (string, bool) {
	rest, ok := strings.CutPrefix(token, "--")
	if !ok || rest == "" || strings.HasPrefix(rest, "-") {
		return "", false
	}
	name, _, _ := strings.Cut(rest, "=")
	name = strings.Trim(name, `"'`+"`")
	if name == "" || strings.ContainsAny(name, " \t") {
		return "", false
	}
	return name, true
}

func pathParamShortFlagName(token string) (string, bool) {
	if len(token) < 2 || token[0] != '-' || token[1] == '-' {
		return "", false
	}
	if len(token) == 2 || token[2] == '=' {
		return token[1:2], true
	}
	return "", false
}

func pathParamNameIsPositional(name string, placeholders []string) bool {
	name = strings.ToLower(strings.Trim(name, "<>"))
	if name == "" {
		return false
	}
	for _, placeholder := range placeholders {
		if strings.EqualFold(strings.Trim(placeholder, "<>"), name) {
			return true
		}
	}
	return false
}

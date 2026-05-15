package pipeline

import (
	"context"
	"errors"
	"os/exec"
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
}

// pathParamRequiredSentinelRe matches the usage-error sentinel that
// generator-emitted positional validators print when their (potentially
// mis-indexed) `len(args) < N` guard fires: e.g. `Error: tagId is
// required`. Case-insensitive so generator wording drift does not blind
// the probe.
var pathParamRequiredSentinelRe = regexp.MustCompile(`(?i)\bis required\b`)

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
func discoverPathParamProbes(binary string) []pathParamProbe {
	if binary == "" {
		return nil
	}
	var probes []pathParamProbe
	var walk func(path []string)
	walk = func(path []string) {
		if len(path) > pathParamProbeMaxDepth {
			return
		}

		helpOut, ok := readCommandHelp(binary, path)
		if !ok {
			return
		}

		children := parseHelpCommands(helpOut)

		if len(path) >= 2 && len(children) == 0 {
			if placeholders := extractPositionalsFromUsage(helpOut); len(placeholders) > 0 {
				probes = append(probes, pathParamProbe{
					path:         append([]string(nil), path...),
					placeholders: placeholders,
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

func readCommandHelp(binary string, path []string) (string, bool) {
	args := append(append([]string{}, path...), "--help")
	ctx, cancel := context.WithTimeout(context.Background(), pathParamProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	applyDefaultSubprocessEnv(cmd)
	out, err := cmd.CombinedOutput()
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
	probes := discoverPathParamProbes(binary)
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
		// Other errors (auth, transient, intentional non-zero) belong to
		// other checks. Timeouts are inconclusive: leave Passed=true so a
		// hung --dry-run does not flip the verdict; the existing matrix's
		// 10s timeout flags the same command independently.
		if err != nil && pathParamRequiredSentinelRe.MatchString(string(out)) && !isContextDeadlineErr(err) {
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

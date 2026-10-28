package pipeline

import "strings"

// pflag ignores a bool value it cannot parse, and the last recognized
// --dry-run wins. --dry-run=maybe therefore does not preview, and
// --dry-run=false is a real run.
func liveDogfoodDryRunValue(value string) (enabled, recognized bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true":
		return true, true
	case "0", "f", "false":
		return false, true
	default:
		return false, false
	}
}

type dryRunFlagInfo struct {
	shorthand  byte
	valueFlags map[string]struct{}
	shorthands map[byte]bool
}

func liveDogfoodDryRunInfo(help string) dryRunFlagInfo {
	info := dryRunFlagInfo{
		valueFlags: liveDogfoodFlagValueNames(help),
		shorthands: liveDogfoodShorthandTypes(help),
	}
	for line := range strings.SplitSeq(extractFlagsSection(help), "\n") {
		decl, ok := parseLiveDogfoodFlagDecl(line)
		if ok && decl.long == "dry-run" {
			info.shorthand = decl.shorthand
			break
		}
	}
	return info
}

func (info dryRunFlagInfo) longTakesValue(name string) bool {
	_, ok := info.valueFlags[strings.ToLower(name)]
	return ok
}

func (info dryRunFlagInfo) isDryRunShort(letter byte) bool {
	return info.shorthand != 0 && letter == info.shorthand
}

func splitLiveDogfoodLongFlag(arg string) (name, value string, hasValue, ok bool) {
	body, found := strings.CutPrefix(arg, "--")
	if !found || body == "" || body[0] == '-' || body[0] == '=' {
		return "", "", false, false
	}
	name, value, hasValue = strings.Cut(body, "=")
	return name, value, hasValue, name != ""
}

func liveDogfoodShortCluster(arg string) (string, bool) {
	if arg == "-" || isNegativeNumericArg(arg) || !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return "", false
	}
	return arg[1:], true
}

// liveDogfoodEffectiveDryRun reports whether argv previews, using the same
// last-wins bool rules as the printed CLI. A token consumed as another
// flag's value is not a flag, and tokens after "--" are positionals.
func liveDogfoodEffectiveDryRun(args []string, help string) bool {
	info := liveDogfoodDryRunInfo(help)
	enabled := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if name, value, hasValue, ok := splitLiveDogfoodLongFlag(arg); ok {
			if name == "dry-run" {
				if !hasValue && info.longTakesValue("dry-run") && i+1 < len(args) {
					if v, known := liveDogfoodDryRunValue(args[i+1]); known {
						enabled = v
					}
					i++
					continue
				}
				if !hasValue {
					enabled = true
					continue
				}
				if v, known := liveDogfoodDryRunValue(value); known {
					enabled = v
				}
				continue
			}
			if !hasValue && info.longTakesValue(name) && i+1 < len(args) {
				i++
			}
			continue
		}
		if cluster, ok := liveDogfoodShortCluster(arg); ok {
			var next int
			enabled, next = applyLiveDogfoodDryRunShort(cluster, args, i, enabled, info)
			i = next
		}
	}
	return enabled
}

// applyLiveDogfoodDryRunShort returns the index of the last token the cluster
// consumed. The caller stores that index and the loop increments past it.
func applyLiveDogfoodDryRunShort(cluster string, args []string, i int, enabled bool, info dryRunFlagInfo) (bool, int) {
	for len(cluster) > 0 {
		letter := cluster[0]
		rest := cluster[1:]
		takesValue := info.shorthands[letter]
		if strings.HasPrefix(rest, "=") {
			if info.isDryRunShort(letter) {
				if v, known := liveDogfoodDryRunValue(rest[1:]); known {
					enabled = v
				}
			}
			return enabled, i
		}
		if !takesValue {
			if info.isDryRunShort(letter) {
				enabled = true
			}
			cluster = rest
			continue
		}
		if rest != "" {
			return enabled, i
		}
		if i+1 < len(args) {
			return enabled, i + 1
		}
		return enabled, i
	}
	return enabled, i
}

// stripLiveDogfoodDryRunFlags removes dry-run flags. Values of other flags
// and tokens after "--" stay, including when that value is the string "--dry-run".
func stripLiveDogfoodDryRunFlags(args []string, help string) []string {
	stripped, _ := walkLiveDogfoodDryRunArgv(args, help)
	return stripped
}

// walkLiveDogfoodDryRunArgv strips dry-run flags and reports where a rebuilt
// --dry-run is still parsed as that flag. insertAt points at a real "--"
// separator, or at a trailing value-taking flag that would consume an
// appended token. A "--" taken as another flag's value is not the separator.
func walkLiveDogfoodDryRunArgv(args []string, help string) (out []string, insertAt int) {
	info := liveDogfoodDryRunInfo(help)
	out = make([]string, 0, len(args)+1)
	insertAt = -1
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			insertAt = len(out)
			out = append(out, args[i:]...)
			return out, insertAt
		}
		if name, _, hasValue, ok := splitLiveDogfoodLongFlag(arg); ok {
			if name == "dry-run" {
				if !hasValue && info.longTakesValue("dry-run") && i+1 < len(args) {
					i++
				}
				continue
			}
			if !hasValue && info.longTakesValue(name) && i+1 >= len(args) {
				insertAt = len(out)
			}
			out = append(out, arg)
			if !hasValue && info.longTakesValue(name) && i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
			continue
		}
		if cluster, ok := liveDogfoodShortCluster(arg); ok {
			rewritten, takeNext, keepNext := rewriteDryRunCluster(cluster, info)
			if takeNext && i+1 >= len(args) {
				insertAt = len(out)
			}
			if rewritten != "" {
				out = append(out, rewritten)
			}
			if takeNext && i+1 < len(args) {
				i++
				if keepNext {
					out = append(out, args[i])
				}
			}
			continue
		}
		out = append(out, arg)
	}
	if insertAt < 0 {
		insertAt = len(out)
	}
	return out, insertAt
}

// rewriteDryRunCluster drops a dry-run shorthand from a single-dash cluster.
// takeNext means the following argv token belongs to the cluster; keepNext
// is false when that token is the dry-run flag's own value and must be dropped.
func rewriteDryRunCluster(cluster string, info dryRunFlagInfo) (rewritten string, takeNext, keepNext bool) {
	var kept strings.Builder
	for len(cluster) > 0 {
		letter := cluster[0]
		rest := cluster[1:]
		takesValue := info.shorthands[letter]
		isDry := info.isDryRunShort(letter)
		if strings.HasPrefix(rest, "=") {
			if !isDry {
				kept.WriteByte(letter)
				kept.WriteString(rest)
			}
			break
		}
		if takesValue {
			if isDry {
				if rest == "" {
					takeNext = true
				}
				break
			}
			kept.WriteByte(letter)
			if rest != "" {
				kept.WriteString(rest)
				break
			}
			takeNext = true
			keepNext = true
			break
		}
		if !isDry {
			kept.WriteByte(letter)
		}
		cluster = rest
	}
	if kept.Len() == 0 {
		return "", takeNext, false
	}
	return "-" + kept.String(), takeNext, keepNext
}

// ensureLiveDogfoodDryRunArg makes a mutating dry-run argv actually preview.
// Rebuilding the flag (instead of trusting a pre-existing token) keeps a
// --dry-run=false, an unrecognized value, a shorthand, or a lookalike value
// of another flag from running the mutation.
func ensureLiveDogfoodDryRunArg(args []string, help string) []string {
	stripped, insertAt := walkLiveDogfoodDryRunArgv(args, help)
	out := make([]string, 0, len(stripped)+1)
	out = append(out, stripped[:insertAt]...)
	out = append(out, "--dry-run")
	out = append(out, stripped[insertAt:]...)
	return out
}

func happyArgsMentionDryRun(parsed happyArgs) bool {
	for i := 0; i+1 < len(parsed.flags); i += 2 {
		if parsed.flags[i] == "--dry-run" {
			return true
		}
	}
	return false
}

func happyArgsEnableDryRun(parsed happyArgs) bool {
	enabled := false
	for i := 0; i+1 < len(parsed.flags); i += 2 {
		if parsed.flags[i] != "--dry-run" {
			continue
		}
		if v, known := liveDogfoodDryRunValue(parsed.flags[i+1]); known {
			enabled = v
		}
	}
	return enabled
}

func liveDogfoodDryRunTokenSource(command liveDogfoodCommand, parsed happyArgs, matrixDryRun bool) string {
	if happyArgsEnableDryRun(parsed) {
		return "pp:happy-args"
	}
	exampleArgs, ok := liveDogfoodExampleArgs(command)
	exampleDry := ok && liveDogfoodEffectiveDryRun(exampleArgs, command.Help)
	if matrixDryRun {
		if exampleDry {
			return "mutating dry-run (Example also includes --dry-run)"
		}
		return "mutating dry-run"
	}
	if exampleDry {
		return "Example"
	}
	return ""
}

func formatDryRunHollowReason(args []string, source string) string {
	argv := strings.Join(args, " ")
	if source == "" {
		return "happy-path argv contains --dry-run: " + argv
	}
	return "happy-path argv contains --dry-run from " + source + ": " + argv
}

func annotateLiveDogfoodDryRun(result LiveDogfoodTestResult, help string) LiveDogfoodTestResult {
	result.dryRunChecked = true
	result.requestsDryRun = liveDogfoodEffectiveDryRun(result.Args, help)
	return result
}

func liveDogfoodResultRequestsDryRun(result LiveDogfoodTestResult) bool {
	if result.dryRunChecked {
		return result.requestsDryRun
	}
	return liveDogfoodArgsContainDryRun(result.Args)
}

func liveDogfoodDryRunPassHollowReason(feature NovelFeature, tests []LiveDogfoodTestResult) string {
	for _, result := range tests {
		if result.Kind != LiveDogfoodTestHappy || result.Status != LiveDogfoodStatusPass || !liveDogfoodResultRequestsDryRun(result) {
			continue
		}
		if !matchNovelFeature(feature, map[string]bool{commandPath(result.Command): true}, nil) {
			continue
		}
		if result.dryRunHollowReason != "" {
			return result.dryRunHollowReason
		}
		return formatDryRunHollowReason(result.Args, "")
	}
	return ""
}

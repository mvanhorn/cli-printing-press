package pipeline

import (
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const typedExitCodesAnnotation = "pp:typed-exit-codes"

var (
	exitCodeLineRe   = regexp.MustCompile(`^\s+(\d+)\s+.+$`)
	exitStatusCodeRe = regexp.MustCompile(`exit status (\d+)`)
	cobraHelpHeaders = map[string]struct{}{
		"Usage:":                  {},
		"Aliases:":                {},
		"Available Commands:":     {},
		"Examples:":               {},
		"Flags:":                  {},
		"Global Flags:":           {},
		"Additional help topics:": {},
	}
)

func typedSuccessCodes(cmd discoveredCommand, helpOutput string) map[int]bool {
	if cmd.Annotations != nil {
		if raw := strings.TrimSpace(cmd.Annotations[typedExitCodesAnnotation]); raw != "" {
			if codes, ok := parseTypedExitCodesAnnotation(raw); ok {
				return codes
			}
		}
	}
	if codes, ok := parseExitCodesFromHelp(helpOutput); ok {
		return codes
	}
	return map[int]bool{0: true}
}

func parseTypedExitCodesAnnotation(raw string) (map[int]bool, bool) {
	codes := map[int]bool{}
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		code, err := strconv.Atoi(part)
		if err != nil || code < 0 {
			return nil, false
		}
		codes[code] = true
	}
	return codes, len(codes) > 0
}

func parseExitCodesFromHelp(helpOutput string) (map[int]bool, bool) {
	codes := map[int]bool{}
	inBlock := false

	for line := range strings.SplitSeq(helpOutput, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if strings.EqualFold(trimmed, "Exit codes:") {
				inBlock = true
			}
			continue
		}

		if trimmed == "" || isCobraHelpSectionHeader(trimmed) || trimmed == "Use:" {
			break
		}

		match := exitCodeLineRe.FindStringSubmatch(line)
		if match == nil {
			if len(codes) > 0 {
				break
			}
			continue
		}
		code, err := strconv.Atoi(match[1])
		if err == nil {
			codes[code] = true
		}
	}

	return codes, len(codes) > 0
}

func isCobraHelpSectionHeader(line string) bool {
	_, ok := cobraHelpHeaders[line]
	return ok
}

func isDocumentedSuccessExit(err error, codes map[int]bool) bool {
	code, ok := extractExitCode(err)
	return ok && codes[code]
}

func extractExitCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	match := exitStatusCodeRe.FindStringSubmatch(err.Error())
	if match == nil {
		return 0, false
	}
	code, convErr := strconv.Atoi(match[1])
	return code, convErr == nil
}

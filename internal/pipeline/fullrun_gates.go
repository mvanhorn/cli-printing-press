package pipeline

import (
	"fmt"
	"strings"
)

func countQualityGateVerdicts(output string, expectedGates []string) (passed, failed int) {
	verdicts := make(map[string]string, len(expectedGates))
	for _, gate := range expectedGates {
		verdicts[gate] = ""
	}
	for line := range strings.SplitSeq(output, "\n") {
		verdict, gate, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || (verdict != "PASS" && verdict != "FAIL") {
			continue
		}
		// A failure takes precedence when a gate has repeated verdicts.
		if previous, expected := verdicts[gate]; expected && previous != "FAIL" {
			verdicts[gate] = verdict
		}
	}
	for _, verdict := range verdicts {
		switch verdict {
		case "PASS":
			passed++
		case "FAIL":
			failed++
		}
	}
	return passed, failed
}

func writeFullRunGateFailures(b *strings.Builder, results []*FullRunResult, qualityGateCount int) {
	b.WriteString("## Gate Failures\n\n")
	allPassed := len(results) > 0
	for _, result := range results {
		if result.GatesFailed == 0 && result.GatesPassed == qualityGateCount {
			continue
		}
		allPassed = false
		if result.GatesFailed > 0 {
			fmt.Fprintf(b, "- **%s** - %d gates failed\n", result.APIName, result.GatesFailed)
		}
		if result.GatesPassed+result.GatesFailed < qualityGateCount {
			fmt.Fprintf(b, "- **%s** - validation incomplete (%d/%d gates passed)\n", result.APIName, result.GatesPassed, qualityGateCount)
		}
	}
	if allPassed {
		b.WriteString("All gates passed across all runs.\n")
	}
}

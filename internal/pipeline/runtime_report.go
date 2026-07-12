package pipeline

func finalizeVerifyReport(report *VerifyReport, threshold int, requireDataPipeline bool) {
	for _, result := range report.Results {
		report.Total++
		if result.Score >= 2 {
			report.Passed++
			continue
		}
		report.Failed++
		if result.Score == 0 {
			report.Critical++
		}
	}
	// Path-param probes catch the failure mode where a nested leaf command
	// exits with a "<positional> is required" usage error even though the
	// caller supplied the positionals. The existing per-command matrix
	// only probes top-level commands, so this gap let a generator codegen
	// bug (mis-indexed args[] in path-param emit) ship silently with
	// verify reporting 100% pass. Each failing probe is a critical
	// failure: the command is unusable as shipped.
	for _, probe := range report.PathParamProbes {
		report.Total++
		if probe.Passed {
			report.Passed++
			continue
		}
		report.Failed++
		report.Critical++
	}
	if report.Total > 0 {
		report.PassRate = float64(report.Passed) / float64(report.Total) * 100
	}

	passGate := report.PassRate >= float64(threshold) && report.Critical == 0
	if requireDataPipeline {
		passGate = passGate && report.DataPipeline
	}
	switch {
	case requireDataPipeline && !report.DataPipeline:
		report.Verdict = "FAIL"
	case passGate:
		report.Verdict = "PASS"
	case report.PassRate >= 60 && report.Critical <= 3:
		report.Verdict = "WARN"
	default:
		report.Verdict = "FAIL"
	}
	if report.BrowserSessionRequired && report.BrowserSessionProof != "valid" {
		report.Verdict = "FAIL"
	}
}

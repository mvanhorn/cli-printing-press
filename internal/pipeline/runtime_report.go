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
	if report.Total > 0 {
		report.PassRate = float64(report.Passed) / float64(report.Total) * 100
	}

	passGate := report.PassRate >= float64(threshold) && report.Critical == 0
	if requireDataPipeline {
		passGate = passGate && report.DataPipeline
	}
	switch {
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

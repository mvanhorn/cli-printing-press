package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// RunWorkflowVerification builds the CLI and runs all workflows from the manifest.
func RunWorkflowVerification(dir string) (*WorkflowVerifyReport, error) {
	manifest, err := LoadWorkflowManifest(dir)
	if err != nil {
		return nil, fmt.Errorf("loading workflow manifest: %w", err)
	}

	if manifest == nil {
		report := &WorkflowVerifyReport{
			Dir:     dir,
			Verdict: WorkflowVerdictPass,
			Issues:  []string{"no workflow manifest found, skipping"},
		}
		if err := writeWorkflowVerifyReport(dir, report); err != nil {
			return nil, err
		}
		return report, nil
	}

	// Build the CLI binary.
	cliName := findCLIName(dir)
	if cliName == "" {
		report := &WorkflowVerifyReport{
			Dir:     dir,
			Verdict: WorkflowVerdictFail,
			Issues:  []string{"no CLI command directory found, cannot build binary"},
		}
		if err := writeWorkflowVerifyReport(dir, report); err != nil {
			return nil, err
		}
		return report, nil
	}

	binary, err := buildDogfoodBinary(dir, cliName)
	if err != nil {
		return nil, fmt.Errorf("building CLI binary: %w", err)
	}
	defer func() { _ = os.Remove(binary) }()

	report := &WorkflowVerifyReport{
		Dir: dir,
	}

	for _, wf := range manifest.Workflows {
		result := runWorkflow(binary, wf, dir)
		report.Workflows = append(report.Workflows, result)
	}

	report.Verdict = deriveOverallVerdict(manifest, report.Workflows)

	if err := writeWorkflowVerifyReport(dir, report); err != nil {
		return nil, err
	}
	return report, nil
}

// runWorkflow executes all steps in a workflow sequentially.
func runWorkflow(binary string, wf Workflow, dir string) WorkflowResult {
	result := WorkflowResult{
		Name:    wf.Name,
		Primary: wf.Primary,
	}

	vars := make(map[string]string)
	authBlocked := false

	for _, step := range wf.Steps {
		var sr StepResult
		sr.Command = step.Command

		// Check if a prior step's failure should skip this one.
		if authBlocked {
			sr.Status = StepStatusSkippedAuth
			result.Steps = append(result.Steps, sr)
			continue
		}

		// Check if we need variables from a prior step that aren't available.
		cmdExpanded := substituteVars(step.Command, vars)
		if strings.Contains(cmdExpanded, "${") {
			sr.Status = StepStatusSkippedNoInput
			sr.Error = "missing variable from prior step"
			result.Steps = append(result.Steps, sr)
			continue
		}

		sr = executeStep(binary, step, cmdExpanded, dir)

		// Extract values on success.
		if sr.Status == StepStatusPass && len(step.Extract) > 0 {
			sr.Extracted = make(map[string]string)
			for varName, jsonPath := range step.Extract {
				val, err := extractJSONField([]byte(sr.Output), jsonPath)
				if err == nil {
					vars[varName] = val
					sr.Extracted[varName] = val
				}
			}
		}

		if sr.Status == StepStatusBlockedAuth {
			authBlocked = true
		}

		result.Steps = append(result.Steps, sr)
	}

	result.Verdict = deriveWorkflowVerdict(result.Steps)
	return result
}

// executeStep runs a single workflow step and classifies the result.
func executeStep(binary string, step WorkflowStep, cmdExpanded string, dir string) StepResult {
	sr := StepResult{
		Command: step.Command,
	}

	args := strings.Fields(cmdExpanded)
	args = append(args, "--json")

	maxAttempts := 1
	if step.Mode == StepModeLive {
		maxAttempts = 3
	}

	var output string
	var cmdErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		cancel()

		output = string(out)
		cmdErr = err

		if err != nil && ctx.Err() == context.DeadlineExceeded {
			if attempt < maxAttempts-1 {
				continue
			}
			sr.Status = StepStatusBlockedTransient
			sr.Error = "timed out after 30s"
			sr.Output = output
			return sr
		}

		if err == nil {
			break
		}

		// On transient errors for live mode, retry.
		if step.Mode == StepModeLive && isTransientError(output) && attempt < maxAttempts-1 {
			continue
		}
		break
	}

	sr.Output = output

	if cmdErr != nil {
		return classifyError(sr, output)
	}

	// Validate JSON output.
	var parsed any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		sr.Status = StepStatusFailCLIBug
		sr.Error = "output is not valid JSON"
		return sr
	}

	// Check expect_fields if specified.
	if len(step.ExpectFields) > 0 {
		if obj, ok := parsed.(map[string]any); ok {
			for _, field := range step.ExpectFields {
				if _, exists := obj[field]; !exists {
					sr.Status = StepStatusFailCLIBug
					sr.Error = fmt.Sprintf("expected field %q not found in output", field)
					return sr
				}
			}
		} else {
			sr.Status = StepStatusFailCLIBug
			sr.Error = "output is not a JSON object, cannot check expect_fields"
			return sr
		}
	}

	sr.Status = StepStatusPass
	return sr
}

// classifyError determines the step status from a command failure.
func classifyError(sr StepResult, output string) StepResult {
	lower := strings.ToLower(output)

	if strings.Contains(lower, "unknown command") || strings.Contains(lower, "unknown flag") {
		sr.Status = StepStatusFailCLIBug
		sr.Error = "unknown command or flag"
		return sr
	}

	if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "authentication required") || strings.Contains(lower, "403") {
		sr.Status = StepStatusBlockedAuth
		sr.Error = "authentication required"
		return sr
	}

	if isTransientError(output) {
		sr.Status = StepStatusBlockedTransient
		sr.Error = "transient error"
		return sr
	}

	sr.Status = StepStatusFailCLIBug
	sr.Error = "command failed with non-zero exit"
	return sr
}

// isTransientError checks if error output suggests a transient failure.
func isTransientError(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "500") ||
		strings.Contains(lower, "502") ||
		strings.Contains(lower, "503") ||
		strings.Contains(lower, "504") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "timeout")
}

// substituteVars replaces ${var_name} placeholders with values from vars.
func substituteVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	return s
}

// extractJSONField extracts a value from JSON data using a simplified jq-style path.
// Supports paths like $.name, $.data.id, $.items[0].code.
func extractJSONField(jsonData []byte, path string) (string, error) {
	// Strip $. prefix
	path = strings.TrimPrefix(path, "$.")
	if path == "" {
		return "", fmt.Errorf("empty path")
	}

	var raw any
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return "", fmt.Errorf("parsing JSON: %w", err)
	}

	// Split by . but handle array notation
	segments := splitJSONPath(path)

	current := raw
	for _, seg := range segments {
		name, idx, hasIdx := parseSegment(seg)

		// Navigate into map
		if name != "" {
			obj, ok := current.(map[string]any)
			if !ok {
				return "", fmt.Errorf("expected object at %q, got %T", seg, current)
			}
			val, exists := obj[name]
			if !exists {
				return "", fmt.Errorf("field %q not found", name)
			}
			current = val
		}

		// Navigate into array if index present
		if hasIdx {
			arr, ok := current.([]any)
			if !ok {
				return "", fmt.Errorf("expected array at %q, got %T", seg, current)
			}
			if idx < 0 || idx >= len(arr) {
				return "", fmt.Errorf("index %d out of range for array of length %d", idx, len(arr))
			}
			current = arr[idx]
		}
	}

	// Convert to string
	switch v := current.(type) {
	case string:
		return v, nil
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v)), nil
		}
		return fmt.Sprintf("%g", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	case nil:
		return "", fmt.Errorf("value is null")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("marshaling value: %w", err)
		}
		return string(b), nil
	}
}

// arrayIndexRe matches segments like "items[0]".
var arrayIndexRe = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)\[(\d+)\]$`)

// splitJSONPath splits a dot-notation path into segments.
func splitJSONPath(path string) []string {
	return strings.Split(path, ".")
}

// parseSegment extracts a field name and optional array index from a path segment.
func parseSegment(seg string) (name string, idx int, hasIdx bool) {
	m := arrayIndexRe.FindStringSubmatch(seg)
	if m != nil {
		idx := 0
		_, _ = fmt.Sscanf(m[2], "%d", &idx)
		return m[1], idx, true
	}
	return seg, 0, false
}

// deriveWorkflowVerdict determines the verdict for a single workflow from its step results.
func deriveWorkflowVerdict(steps []StepResult) WorkflowVerdict {
	hasPass := false
	for _, s := range steps {
		switch s.Status {
		case StepStatusFailCLIBug:
			return WorkflowVerdictFail
		case StepStatusPass:
			hasPass = true
		}
	}

	// Check if auth blocked before any substantive pass.
	if !hasPass {
		for _, s := range steps {
			if s.Status == StepStatusBlockedAuth || s.Status == StepStatusSkippedAuth {
				return WorkflowVerdictUnverified
			}
		}
	}

	return WorkflowVerdictPass
}

// deriveOverallVerdict determines the report verdict from workflow results.
func deriveOverallVerdict(manifest *WorkflowManifest, results []WorkflowResult) WorkflowVerdict {
	// Use primary workflow if available.
	primary := manifest.PrimaryWorkflow()
	if primary != nil {
		for _, r := range results {
			if r.Name == primary.Name {
				return r.Verdict
			}
		}
	}

	// Fall back to worst-case across all workflows.
	worst := WorkflowVerdictPass
	for _, r := range results {
		if r.Verdict == WorkflowVerdictFail {
			return WorkflowVerdictFail
		}
		if r.Verdict == WorkflowVerdictUnverified && worst == WorkflowVerdictPass {
			worst = WorkflowVerdictUnverified
		}
	}
	return worst
}

// writeWorkflowVerifyReport writes the report as JSON to the given directory.
func writeWorkflowVerifyReport(dir string, report *WorkflowVerifyReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling workflow verify report: %w", err)
	}
	path := filepath.Join(dir, "workflow-verify-report.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing workflow verify report: %w", err)
	}
	return nil
}

// LoadWorkflowVerifyReport reads a previously written workflow verify report.
func LoadWorkflowVerifyReport(dir string) (*WorkflowVerifyReport, error) {
	path := filepath.Join(dir, "workflow-verify-report.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report WorkflowVerifyReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

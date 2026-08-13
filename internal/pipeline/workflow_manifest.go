package pipeline

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// WorkflowManifest is the top-level structure for workflow_verify.yaml
type WorkflowManifest struct {
	Workflows []Workflow `yaml:"workflows" json:"workflows"`
}

// Workflow represents a single testable workflow
type Workflow struct {
	Name    string         `yaml:"name" json:"name"`
	Primary bool           `yaml:"primary" json:"primary"`
	Steps   []WorkflowStep `yaml:"steps" json:"steps"`
}

// WorkflowStep represents one step in a workflow
type WorkflowStep struct {
	Command      string            `yaml:"command" json:"command"`
	Args         map[string]string `yaml:"args,omitempty" json:"args,omitempty"`
	ArgsStdin    bool              `yaml:"args_stdin,omitempty" json:"args_stdin,omitempty"`
	StdinJSON    map[string]any    `yaml:"stdin_json,omitempty" json:"stdin_json,omitempty"`
	Extract      map[string]string `yaml:"extract,omitempty" json:"extract,omitempty"`
	Mode         StepMode          `yaml:"mode" json:"mode"`
	ExpectFields []string          `yaml:"expect_fields,omitempty" json:"expect_fields,omitempty"`
	AuthRequired bool              `yaml:"auth_required,omitempty" json:"auth_required,omitempty"`
}

// StepMode controls how a step is executed during verification
type StepMode string

const (
	StepModeLive  StepMode = "live"
	StepModeMock  StepMode = "mock"
	StepModeLocal StepMode = "local"
)

// StepResult holds the outcome of executing a single workflow step
type StepResult struct {
	Command   string            `json:"command"`
	Status    StepStatus        `json:"status"`
	Output    string            `json:"output,omitempty"`
	Error     string            `json:"error,omitempty"`
	Extracted map[string]string `json:"extracted,omitempty"`
}

// StepStatus classifies the result of a step execution
type StepStatus string

const (
	StepStatusPass             StepStatus = "pass"
	StepStatusFailCLIBug       StepStatus = "fail-cli-bug"
	StepStatusBlockedAuth      StepStatus = "blocked-auth"
	StepStatusBlockedTransient StepStatus = "blocked-transient"
	StepStatusSkippedNoInput   StepStatus = "skipped-no-input"
	StepStatusSkippedAuth      StepStatus = "skipped-auth-required"
)

// WorkflowResult holds the overall result of running a workflow
type WorkflowResult struct {
	Name    string          `json:"name"`
	Primary bool            `json:"primary"`
	Steps   []StepResult    `json:"steps"`
	Verdict WorkflowVerdict `json:"verdict"`
}

// WorkflowVerdict is the overall outcome of a workflow verification
type WorkflowVerdict string

const (
	WorkflowVerdictPass       WorkflowVerdict = "workflow-pass"
	WorkflowVerdictFail       WorkflowVerdict = "workflow-fail"
	WorkflowVerdictUnverified WorkflowVerdict = "unverified-needs-auth"
)

// WorkflowVerifyReport holds the complete verification results
type WorkflowVerifyReport struct {
	Dir       string           `json:"dir"`
	Workflows []WorkflowResult `json:"workflows"`
	Verdict   WorkflowVerdict  `json:"verdict"`
	Issues    []string         `json:"issues,omitempty"`
}

// LoadWorkflowManifest reads and parses a workflow_verify.yaml from the given directory.
// Returns nil, nil if the file does not exist (not an error).
func LoadWorkflowManifest(dir string) (*WorkflowManifest, error) {
	path := filepath.Join(dir, "workflow_verify.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading workflow manifest: %w", err)
	}

	var manifest WorkflowManifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parsing workflow manifest: %w", err)
	}

	return &manifest, nil
}

// PrimaryWorkflow returns the first workflow marked as primary, or nil if none.
func (m *WorkflowManifest) PrimaryWorkflow() *Workflow {
	if m == nil {
		return nil
	}
	for i := range m.Workflows {
		if m.Workflows[i].Primary {
			return &m.Workflows[i]
		}
	}
	return nil
}

// CommandNames returns all unique command names referenced across all workflow steps.
func (m *WorkflowManifest) CommandNames() []string {
	if m == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var names []string
	for _, w := range m.Workflows {
		for _, s := range w.Steps {
			cmd := s.BaseCommand()
			if _, ok := seen[cmd]; !ok {
				seen[cmd] = struct{}{}
				names = append(names, cmd)
			}
		}
	}
	sort.Strings(names)
	return names
}

// BaseCommand returns the command name without variable substitutions.
// e.g., "cart add ${item_code}" -> "cart add"
func (s *WorkflowStep) BaseCommand() string {
	parts := strings.Fields(s.Command)
	var clean []string
	for _, p := range parts {
		if strings.HasPrefix(p, "${") {
			break
		}
		clean = append(clean, p)
	}
	return strings.Join(clean, " ")
}

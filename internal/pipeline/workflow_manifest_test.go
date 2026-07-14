package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testManifestYAML = `workflows:
  - name: "Order a pizza for delivery"
    primary: true
    steps:
      - command: "stores find_stores"
        args:
          s: "1600 Pennsylvania Ave"
          c: "Washington, DC 20500"
        extract:
          store_id: "$.Stores[0].StoreID"
        mode: live
      - command: "menu get_menu"
        args:
          store: "${store_id}"
        extract:
          item_code: "$.MenuCategories[0].Products[0].Code"
        mode: live
      - command: "cart new"
        args:
          store: "${store_id}"
          service: "delivery"
        mode: local
      - command: "cart add ${item_code}"
        mode: local
      - command: "orders price_order"
        args_stdin: true
        stdin_json:
          input:
            patient_ref: "patient_synthetic"
        mode: mock
        expect_fields:
          - "Order.Amounts.Customer"
          - "Order.Products"
`

func TestLoadWorkflowManifest_Valid(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "workflow_verify.yaml"), testManifestYAML)

	m, err := LoadWorkflowManifest(dir)
	require.NoError(t, err)
	require.NotNil(t, m)

	require.Len(t, m.Workflows, 1)
	wf := m.Workflows[0]
	assert.Equal(t, "Order a pizza for delivery", wf.Name)
	assert.True(t, wf.Primary)
	require.Len(t, wf.Steps, 5)

	// Step 0: stores find_stores
	s0 := wf.Steps[0]
	assert.Equal(t, "stores find_stores", s0.Command)
	assert.Equal(t, StepModeLive, s0.Mode)
	assert.Equal(t, "1600 Pennsylvania Ave", s0.Args["s"])
	assert.Equal(t, "Washington, DC 20500", s0.Args["c"])
	assert.Equal(t, "$.Stores[0].StoreID", s0.Extract["store_id"])
	assert.False(t, s0.ArgsStdin)

	// Step 1: menu get_menu
	s1 := wf.Steps[1]
	assert.Equal(t, "menu get_menu", s1.Command)
	assert.Equal(t, StepModeLive, s1.Mode)
	assert.Equal(t, "${store_id}", s1.Args["store"])
	assert.Equal(t, "$.MenuCategories[0].Products[0].Code", s1.Extract["item_code"])

	// Step 2: cart new
	s2 := wf.Steps[2]
	assert.Equal(t, "cart new", s2.Command)
	assert.Equal(t, StepModeLocal, s2.Mode)
	assert.Equal(t, "${store_id}", s2.Args["store"])
	assert.Equal(t, "delivery", s2.Args["service"])

	// Step 3: cart add ${item_code}
	s3 := wf.Steps[3]
	assert.Equal(t, "cart add ${item_code}", s3.Command)
	assert.Equal(t, StepModeLocal, s3.Mode)

	// Step 4: orders price_order
	s4 := wf.Steps[4]
	assert.Equal(t, "orders price_order", s4.Command)
	assert.Equal(t, StepModeMock, s4.Mode)
	assert.True(t, s4.ArgsStdin)
	assert.Equal(t, "patient_synthetic", s4.StdinJSON["input"].(map[string]any)["patient_ref"])
	assert.Equal(t, []string{"Order.Amounts.Customer", "Order.Products"}, s4.ExpectFields)
}

func TestLoadWorkflowManifest_NotFound(t *testing.T) {
	dir := t.TempDir()

	m, err := LoadWorkflowManifest(dir)
	assert.NoError(t, err)
	assert.Nil(t, m)
}

func TestLoadWorkflowManifest_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "workflow_verify.yaml"), "{{not valid yaml]]]")

	m, err := LoadWorkflowManifest(dir)
	assert.Error(t, err)
	assert.Nil(t, m)
	assert.Contains(t, err.Error(), "parsing workflow manifest")
}

func TestPrimaryWorkflow(t *testing.T) {
	m := &WorkflowManifest{
		Workflows: []Workflow{
			{Name: "secondary", Primary: false},
			{Name: "primary-one", Primary: true},
			{Name: "another", Primary: false},
		},
	}

	pw := m.PrimaryWorkflow()
	require.NotNil(t, pw)
	assert.Equal(t, "primary-one", pw.Name)
	assert.True(t, pw.Primary)
}

func TestPrimaryWorkflow_None(t *testing.T) {
	m := &WorkflowManifest{
		Workflows: []Workflow{
			{Name: "a", Primary: false},
			{Name: "b", Primary: false},
		},
	}

	assert.Nil(t, m.PrimaryWorkflow())

	// Also test nil receiver
	var nilManifest *WorkflowManifest
	assert.Nil(t, nilManifest.PrimaryWorkflow())
}

func TestCommandNames(t *testing.T) {
	m := &WorkflowManifest{
		Workflows: []Workflow{
			{
				Name: "workflow-a",
				Steps: []WorkflowStep{
					{Command: "stores find_stores"},
					{Command: "cart add ${item_code}"},
					{Command: "menu get_menu"},
				},
			},
			{
				Name: "workflow-b",
				Steps: []WorkflowStep{
					{Command: "stores find_stores"}, // duplicate
					{Command: "orders price_order"},
					{Command: "cart add ${other}"}, // same base as "cart add" above
				},
			},
		},
	}

	names := m.CommandNames()
	assert.Equal(t, []string{"cart add", "menu get_menu", "orders price_order", "stores find_stores"}, names)

	// Nil receiver
	var nilManifest *WorkflowManifest
	assert.Nil(t, nilManifest.CommandNames())
}

func TestBaseCommand(t *testing.T) {
	tests := []struct {
		command  string
		expected string
	}{
		{"stores find_stores", "stores find_stores"},
		{"cart add ${item_code}", "cart add"},
		{"${var}", ""},
		{"menu get_menu ${store_id} ${extra}", "menu get_menu"},
		{"simple", "simple"},
	}

	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			s := &WorkflowStep{Command: tc.command}
			assert.Equal(t, tc.expected, s.BaseCommand())
		})
	}
}

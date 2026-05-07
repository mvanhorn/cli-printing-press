package pipeline

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditPublicParamNamesFindsDecisionRequiredCrypticParams(t *testing.T) {
	api := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"stores": {
				Endpoints: map[string]spec.Endpoint{
					"find": {
						Path:        "/stores",
						Description: "Find nearby stores",
						Params: []spec.Param{
							{Name: "s", Type: "string", Required: true, Description: "Street address"},
							{Name: "c", Type: "string", Required: true, Description: "City name"},
							{Name: "q", Type: "string", Required: false},
							{Name: "store_code", Type: "string", Required: true, Description: "Store code"},
							{Name: "id", Type: "string", Required: true, Positional: true, Description: "Store ID"},
						},
					},
				},
			},
		},
	}

	findings := AuditPublicParamNames(api)

	require.Len(t, findings, 3)
	city := requirePublicParamFinding(t, findings, "stores.find.params.c")
	assert.Equal(t, "c", city.WireName)
	assert.Equal(t, []string{"one-letter-wire-name"}, city.Reasons)
	assert.Equal(t, "q", requirePublicParamFinding(t, findings, "stores.find.params.q").WireName)
	assert.Equal(t, "s", requirePublicParamFinding(t, findings, "stores.find.params.s").WireName)
}

func TestAuditPublicParamNamesMarksExistingFlagNamesResolved(t *testing.T) {
	api := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"stores": {
				Endpoints: map[string]spec.Endpoint{
					"find": {
						Params: []spec.Param{
							{Name: "s", FlagName: "street", Aliases: []string{"s"}, Type: "string", Required: true, Description: "Street address"},
							{Name: "c", Type: "string", Required: true, Description: "City name"},
						},
					},
				},
			},
		},
	}

	ledger := NewPublicParamAuditLedger(AuditPublicParamNames(api))

	assert.Equal(t, PublicParamAuditSummary{Total: 2, Pending: 1, Resolved: 1}, ledger.Summary)
	street := requirePublicParamFinding(t, ledger.Findings, "stores.find.params.s")
	assert.Equal(t, "street", street.CurrentPublicName)
	assert.Equal(t, []string{"s"}, street.Aliases)
}

func TestPublicParamAuditSkipRequiresEvidence(t *testing.T) {
	findings := []PublicParamAuditFinding{
		{ID: "stores.find.params.s", WireName: "s", Decision: PublicParamDecisionSkip, SkipReason: "This is a public API field."},
		{ID: "stores.find.params.c", WireName: "c", Decision: PublicParamDecisionSkip, SourceEvidence: "Docs say c is a literal vendor field.", SkipReason: "The vendor documents c as the public term."},
	}

	summary := SummarizePublicParamAudit(findings)

	assert.Equal(t, 1, summary.Pending)
	assert.Equal(t, 1, summary.Accepted)
}

func TestPublicParamAuditFlagNameDecisionDoesNotResolveUntilSpecChanges(t *testing.T) {
	findings := []PublicParamAuditFinding{
		{
			ID:               "stores.find.params.s",
			WireName:         "s",
			Decision:         PublicParamDecisionFlagName,
			ProposedFlagName: "street",
			SourceEvidence:   "Docs call this street address.",
		},
	}

	summary := SummarizePublicParamAudit(findings)

	assert.Equal(t, 1, summary.Pending)
	assert.Equal(t, 0, summary.Resolved)
}

func TestReconcilePublicParamAuditFindingsPreservesAgentDecisionFields(t *testing.T) {
	current := []PublicParamAuditFinding{{ID: "stores.find.params.s", WireName: "s", Description: "Street address"}}
	previous := []PublicParamAuditFinding{{
		ID:               "stores.find.params.s",
		Decision:         PublicParamDecisionFlagName,
		ProposedFlagName: "street",
		ProposedAliases:  []string{"s"},
		SourceEvidence:   "Docs call this street address.",
		SkipReason:       "old skip",
		Note:             "agent reviewed",
	}}

	got := ReconcilePublicParamAuditFindings(current, previous)

	require.Len(t, got, 1)
	assert.Equal(t, "Street address", got[0].Description)
	assert.Equal(t, PublicParamDecisionFlagName, got[0].Decision)
	assert.Equal(t, "street", got[0].ProposedFlagName)
	assert.Equal(t, []string{"s"}, got[0].ProposedAliases)
	assert.Equal(t, "Docs call this street address.", got[0].SourceEvidence)
	assert.Equal(t, "old skip", got[0].SkipReason)
	assert.Equal(t, "agent reviewed", got[0].Note)
}

func requirePublicParamFinding(t *testing.T, findings []PublicParamAuditFinding, id string) PublicParamAuditFinding {
	t.Helper()
	for _, finding := range findings {
		if finding.ID == id {
			return finding
		}
	}
	require.Failf(t, "missing finding", "finding %q not found in %#v", id, findings)
	return PublicParamAuditFinding{}
}

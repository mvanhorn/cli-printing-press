// Package vision defines types for the visionary research system.
package vision

import "slices"

type DataProfile struct {
	WritePattern string
	Volume       string
	Realtime     bool
	SearchNeed   string
}

type APIIdentity struct {
	DomainCategory string
	PrimaryUsers   []string
	CoreEntities   []string
	DataProfile    DataProfile
}

type UsagePattern struct {
	Name            string
	Description     string
	EvidenceScore   int
	EvidenceSources []string
	Requirements    []string
}

type ToolClassification struct {
	Name       string
	URL        string
	Stars      int
	Language   string
	ToolType   string
	Features   []string
	Maintained bool
}

type WorkflowStep struct {
	Description string
}

type Workflow struct {
	Name               string
	Steps              []WorkflowStep
	Frequency          string
	PainPoint          string
	ProposedCLIFeature string
}

type ArchitectureDecision struct {
	Area               string
	NeedLevel          string
	Decision           string
	Rationale          string
	ImplementationHint string
}

type FeatureIdea struct {
	Name                      string
	Description               string
	EvidenceStrength          int
	UserImpact                int
	ImplementationFeasibility int
	Uniqueness                int
	Composability             int
	DataProfileFit            int
	Maintainability           int
	CompetitiveMoat           int
	TotalScore                int
	TemplateNames             []string
}

func (f *FeatureIdea) ComputeScore() int {
	return f.EvidenceStrength +
		f.UserImpact +
		f.ImplementationFeasibility +
		f.Uniqueness +
		f.Composability +
		f.DataProfileFit +
		f.Maintainability +
		f.CompetitiveMoat
}

type DomainInfo struct {
	Archetype    string
	HasAssignees bool
	HasDueDates  bool
	HasPriority  bool
	HasTeams     bool
	HasLabels    bool
	HasEstimates bool
}

type VisionaryPlan struct {
	APIName       string
	Identity      APIIdentity
	Insight       NonObviousInsight
	Domain        DomainInfo
	UsagePatterns []UsagePattern
	ToolLandscape []ToolClassification
	Workflows     []Workflow
	Architecture  []ArchitectureDecision
	Features      []FeatureIdea
}

func (v *VisionaryPlan) ShouldIncludeTemplate(templateName string) bool {
	for i := range v.Features {
		if v.Features[i].TotalScore < 8 {
			continue
		}
		if slices.Contains(v.Features[i].TemplateNames, templateName) {
			return true
		}
	}
	return false
}

func (v *VisionaryPlan) DataProfileRequires(capability string) bool {
	for _, ad := range v.Architecture {
		if ad.Area == capability {
			return true
		}
	}
	return false
}

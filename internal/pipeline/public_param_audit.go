package pipeline

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
)

const (
	PublicParamAuditLedgerVersion = 1

	PublicParamDecisionSkip     = "skip"
	PublicParamDecisionFlagName = "flag_name"
)

// PublicParamAuditLedger is the persisted agent-reviewed audit state.
// The audit refreshes deterministic fields on every run while preserving
// agent-owned decision fields for stable finding IDs.
type PublicParamAuditLedger struct {
	Version  int                       `json:"version"`
	Summary  PublicParamAuditSummary   `json:"summary"`
	Findings []PublicParamAuditFinding `json:"findings"`
}

type PublicParamAuditSummary struct {
	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Resolved int `json:"resolved"`
	Accepted int `json:"accepted"`
}

// PublicParamAuditFinding records one flag-backed spec parameter whose
// wire name is suspiciously unlike a public command flag.
type PublicParamAuditFinding struct {
	ID                  string   `json:"id"`
	Resource            string   `json:"resource"`
	Endpoint            string   `json:"endpoint"`
	Location            string   `json:"location"`
	WireName            string   `json:"wire_name"`
	CurrentPublicName   string   `json:"current_public_name,omitempty"`
	Aliases             []string `json:"aliases,omitempty"`
	Type                string   `json:"type,omitempty"`
	Required            bool     `json:"required,omitempty"`
	Description         string   `json:"description,omitempty"`
	EndpointPath        string   `json:"endpoint_path,omitempty"`
	EndpointDescription string   `json:"endpoint_description,omitempty"`
	Reasons             []string `json:"reasons"`

	Decision         string   `json:"decision,omitempty"`
	ProposedFlagName string   `json:"proposed_flag_name,omitempty"`
	ProposedAliases  []string `json:"proposed_aliases,omitempty"`
	SourceEvidence   string   `json:"source_evidence,omitempty"`
	SkipReason       string   `json:"skip_reason,omitempty"`
	Note             string   `json:"note,omitempty"`
}

// AuditPublicParamNames inventories parameters where the wire name is a
// poor public flag candidate and an agent may need to author flag_name.
func AuditPublicParamNames(api *spec.APISpec) []PublicParamAuditFinding {
	if api == nil {
		return nil
	}

	var findings []PublicParamAuditFinding
	resourceNames := sortedResourceKeys(api.Resources)
	for _, resourceName := range resourceNames {
		findings = append(findings, auditPublicParamResource(resourceName, api.Resources[resourceName])...)
	}
	return findings
}

func auditPublicParamResource(resourceName string, resource spec.Resource) []PublicParamAuditFinding {
	var findings []PublicParamAuditFinding
	endpointNames := sortedEndpointKeys(resource.Endpoints)
	for _, endpointName := range endpointNames {
		endpoint := resource.Endpoints[endpointName]
		findings = append(findings, auditPublicParams(resourceName, endpointName, "params", endpoint, endpoint.Params)...)
		findings = append(findings, auditPublicParams(resourceName, endpointName, "body", endpoint, endpoint.Body)...)
	}
	subResourceNames := sortedResourceKeys(resource.SubResources)
	for _, subName := range subResourceNames {
		findings = append(findings, auditPublicParamResource(resourceName+"."+subName, resource.SubResources[subName])...)
	}
	return findings
}

func auditPublicParams(resourceName, endpointName, location string, endpoint spec.Endpoint, params []spec.Param) []PublicParamAuditFinding {
	var findings []PublicParamAuditFinding
	for _, param := range params {
		if param.Positional {
			continue
		}
		reasons := publicParamAuditReasons(param)
		if len(reasons) == 0 {
			continue
		}
		findings = append(findings, PublicParamAuditFinding{
			ID:                  publicParamAuditID(resourceName, endpointName, location, param.Name),
			Resource:            resourceName,
			Endpoint:            endpointName,
			Location:            location,
			WireName:            param.Name,
			CurrentPublicName:   param.FlagName,
			Aliases:             append([]string(nil), param.Aliases...),
			Type:                param.Type,
			Required:            param.Required,
			Description:         strings.TrimSpace(param.Description),
			EndpointPath:        endpoint.Path,
			EndpointDescription: strings.TrimSpace(endpoint.Description),
			Reasons:             reasons,
		})
	}
	return findings
}

func publicParamAuditReasons(param spec.Param) []string {
	wire := strings.TrimSpace(param.Name)
	if wire == "" {
		return nil
	}

	var reasons []string
	if utf8.RuneCountInString(wire) == 1 {
		reasons = append(reasons, "one-letter-wire-name")
	}
	if containsOperatorLikePunctuation(wire) {
		reasons = append(reasons, "operator-like-wire-name")
	}
	return reasons
}

func containsOperatorLikePunctuation(name string) bool {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			continue
		case r >= 'A' && r <= 'Z':
			continue
		case r >= '0' && r <= '9':
			continue
		case r == '_' || r == '-':
			continue
		default:
			return true
		}
	}
	return false
}

func publicParamAuditID(resourceName, endpointName, location, wireName string) string {
	return fmt.Sprintf("%s.%s.%s.%s", resourceName, endpointName, location, wireName)
}

// ReconcilePublicParamAuditFindings preserves agent-owned ledger fields
// for findings still present in the current deterministic inventory.
func ReconcilePublicParamAuditFindings(current, previous []PublicParamAuditFinding) []PublicParamAuditFinding {
	previousByID := make(map[string]PublicParamAuditFinding, len(previous))
	for _, finding := range previous {
		previousByID[finding.ID] = finding
	}
	out := make([]PublicParamAuditFinding, 0, len(current))
	for _, finding := range current {
		if previous, ok := previousByID[finding.ID]; ok {
			finding.Decision = previous.Decision
			finding.ProposedFlagName = previous.ProposedFlagName
			finding.ProposedAliases = append([]string(nil), previous.ProposedAliases...)
			finding.SourceEvidence = previous.SourceEvidence
			finding.SkipReason = previous.SkipReason
			finding.Note = previous.Note
		}
		out = append(out, finding)
	}
	return out
}

func SummarizePublicParamAudit(findings []PublicParamAuditFinding) PublicParamAuditSummary {
	summary := PublicParamAuditSummary{Total: len(findings)}
	for _, finding := range findings {
		switch {
		case finding.CurrentPublicName != "":
			summary.Resolved++
		case finding.HasAcceptedPublicParamSkip():
			summary.Accepted++
		default:
			summary.Pending++
		}
	}
	return summary
}

func (f PublicParamAuditFinding) HasAcceptedPublicParamSkip() bool {
	return f.Decision == PublicParamDecisionSkip &&
		strings.TrimSpace(f.SourceEvidence) != "" &&
		strings.TrimSpace(f.SkipReason) != ""
}

func NewPublicParamAuditLedger(findings []PublicParamAuditFinding) PublicParamAuditLedger {
	return PublicParamAuditLedger{
		Version:  PublicParamAuditLedgerVersion,
		Summary:  SummarizePublicParamAudit(findings),
		Findings: findings,
	}
}

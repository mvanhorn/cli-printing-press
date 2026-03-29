package generator

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/internal/spec"
)

type WorkflowTemplateContext struct {
	CLIName       string
	APIName       string
	Domain        string
	PrimaryEntity EntityMapping
	TeamEntity    EntityMapping
	UserEntity    EntityMapping
	StateEntity   EntityMapping
	HasAssignees  bool
	HasDueDates   bool
	HasPriority   bool
	HasTeams      bool
	HasLabels     bool
	HasEstimates  bool
}

type EntityMapping struct {
	TableName       string
	HumanSingular   string
	HumanPlural     string
	IdentifierField string
	TitleField      string
	UpdatedAtField  string
	AssigneeField   string
	PriorityField   string
	DueDateField    string
	TeamField       string
	LabelField      string
	EstimateField   string
	StateField      string
}

func MapEntities(s *spec.APISpec, domain profiler.DomainSignals) WorkflowTemplateContext {
	ctx := WorkflowTemplateContext{
		Domain:       string(domain.Archetype),
		HasAssignees: domain.HasAssignees,
		HasDueDates:  domain.HasDueDates,
		HasPriority:  domain.HasPriority,
		HasTeams:     domain.HasTeams,
		HasLabels:    domain.HasLabels,
		HasEstimates: domain.HasEstimates,
	}

	if s == nil {
		return ctx
	}

	ctx.APIName = s.Name

	primaryKeywords := primaryKeywordsForArchetype(domain.Archetype)
	teamKeywords := []string{"team", "group", "organization", "workspace"}
	userKeywords := []string{"user", "member", "person", "contact", "account"}
	stateKeywords := []string{"status", "state", "stage", "phase"}

	for name, resource := range s.Resources {
		nameLower := strings.ToLower(name)

		if ctx.PrimaryEntity.TableName == "" && matchesKeywords(nameLower, primaryKeywords) {
			ctx.PrimaryEntity = mapResource(name, resource)
		}
		if ctx.TeamEntity.TableName == "" && matchesKeywords(nameLower, teamKeywords) {
			ctx.TeamEntity = mapResource(name, resource)
		}
		if ctx.UserEntity.TableName == "" && matchesKeywords(nameLower, userKeywords) {
			ctx.UserEntity = mapResource(name, resource)
		}
		if ctx.StateEntity.TableName == "" && matchesKeywords(nameLower, stateKeywords) {
			ctx.StateEntity = mapResource(name, resource)
		}

		for subName, sub := range resource.SubResources {
			subLower := strings.ToLower(subName)
			if ctx.PrimaryEntity.TableName == "" && matchesKeywords(subLower, primaryKeywords) {
				ctx.PrimaryEntity = mapResource(subName, sub)
			}
		}
	}

	if ctx.PrimaryEntity.TableName != "" {
		scanEntityFields(s, ctx.PrimaryEntity.TableName, &ctx.PrimaryEntity)
	}

	return ctx
}

func primaryKeywordsForArchetype(archetype profiler.DomainArchetype) []string {
	switch archetype {
	case profiler.ArchetypeProjectMgmt:
		return []string{"issue", "task", "ticket", "work_item", "story"}
	case profiler.ArchetypeCommunication:
		return []string{"message", "chat", "conversation"}
	case profiler.ArchetypePayments:
		return []string{"charge", "payment", "invoice", "transaction"}
	case profiler.ArchetypeInfrastructure:
		return []string{"instance", "server", "deployment", "container"}
	case profiler.ArchetypeContent:
		return []string{"article", "post", "page", "document"}
	case profiler.ArchetypeCRM:
		return []string{"contact", "deal", "lead", "opportunity"}
	case profiler.ArchetypeDeveloperPlatform:
		return []string{"repository", "pull_request", "merge_request", "commit"}
	default:
		return []string{}
	}
}

func matchesKeywords(name string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(name, kw) {
			return true
		}
	}
	return false
}

func mapResource(name string, r spec.Resource) EntityMapping {
	singular := strings.ToLower(name)
	singular = strings.TrimSuffix(singular, "s")

	plural := singular + "s"
	if strings.HasSuffix(singular, "y") && !strings.HasSuffix(singular, "ey") {
		plural = singular[:len(singular)-1] + "ies"
	}

	em := EntityMapping{
		TableName:     strings.ReplaceAll(strings.ToLower(name), "-", "_"),
		HumanSingular: singular,
		HumanPlural:   plural,
	}

	for _, endpoint := range r.Endpoints {
		scanMappingFields(endpoint.Body, &em)
		scanMappingFields(endpoint.Params, &em)
	}

	if em.IdentifierField == "" {
		em.IdentifierField = "id"
	}

	return em
}

func scanMappingFields(params []spec.Param, em *EntityMapping) {
	for _, p := range params {
		name := strings.ToLower(p.Name)

		if em.IdentifierField == "" && (name == "id" || strings.HasSuffix(name, "_id")) {
			em.IdentifierField = p.Name
		}
		if em.TitleField == "" && (name == "title" || name == "name" || name == "subject" || name == "summary") {
			em.TitleField = p.Name
		}
		if em.UpdatedAtField == "" && (name == "updated_at" || name == "modified_at" || name == "last_modified") {
			em.UpdatedAtField = p.Name
		}
		if em.AssigneeField == "" && (strings.Contains(name, "assignee") || name == "assigned_to") {
			em.AssigneeField = p.Name
		}
		if em.PriorityField == "" && strings.Contains(name, "priority") {
			em.PriorityField = p.Name
		}
		if em.DueDateField == "" && (strings.Contains(name, "due_date") || strings.Contains(name, "due_at") || strings.Contains(name, "deadline")) {
			em.DueDateField = p.Name
		}
		if em.TeamField == "" && (name == "team_id" || name == "team") {
			em.TeamField = p.Name
		}
		if em.LabelField == "" && (strings.Contains(name, "label") || name == "tags") {
			em.LabelField = p.Name
		}
		if em.EstimateField == "" && (strings.Contains(name, "estimate") || strings.Contains(name, "story_points") || name == "points") {
			em.EstimateField = p.Name
		}
		if em.StateField == "" && (name == "status" || name == "state") {
			em.StateField = p.Name
		}

		if len(p.Fields) > 0 {
			scanMappingFields(p.Fields, em)
		}
	}
}

func scanEntityFields(s *spec.APISpec, tableName string, em *EntityMapping) {
	for name, resource := range s.Resources {
		if strings.EqualFold(strings.ReplaceAll(strings.ToLower(name), "-", "_"), tableName) {
			for _, endpoint := range resource.Endpoints {
				scanMappingFields(endpoint.Body, em)
				scanMappingFields(endpoint.Params, em)
			}
			return
		}
	}
}

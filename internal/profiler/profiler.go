package profiler

import (
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/mvanhorn/cli-printing-press/internal/vision"
)

type DomainArchetype string

const (
	ArchetypeCommunication     DomainArchetype = "communication"
	ArchetypeProjectMgmt       DomainArchetype = "project-management"
	ArchetypePayments          DomainArchetype = "payments"
	ArchetypeInfrastructure    DomainArchetype = "infrastructure"
	ArchetypeContent           DomainArchetype = "content"
	ArchetypeCRM               DomainArchetype = "crm"
	ArchetypeDeveloperPlatform DomainArchetype = "developer-platform"
	ArchetypeGeneric           DomainArchetype = "generic"
)

type DomainSignals struct {
	Archetype        DomainArchetype
	HasAssignees     bool
	HasDueDates      bool
	HasPriority      bool
	HasThreading     bool
	HasTransactions  bool
	HasSubscriptions bool
	HasMedia         bool
	HasTeams         bool
	HasLabels        bool
	HasEstimates     bool
}

// PaginationProfile describes the detected pagination patterns across the API.
type PaginationProfile struct {
	CursorParam     string `json:"cursor_param"`      // most common cursor param name (after, cursor, page_token, offset)
	PageSizeParam   string `json:"page_size_param"`   // most common page size param (limit, per_page, page_size, first)
	SinceParam      string `json:"since_param"`       // temporal filter param (since, updated_after, modified_since)
	ItemsKey        string `json:"items_key"`         // response array key (data, results, items, or "" for root array)
	DefaultPageSize int    `json:"default_page_size"` // detected or default 100
}

// SyncableResource describes a resource that supports paginated list sync.
type SyncableResource struct {
	Name string
	Path string
}

// APIProfile describes the shape of an API and what power-user features it warrants.
type APIProfile struct {
	HighVolume       bool
	NeedsSearch      bool
	HasRealtime      bool
	OfflineValuable  bool
	ComplexResources bool
	HasLifecycles    bool
	HasDependencies  bool
	HasChronological bool
	HasFileOps       bool
	CRUDResources    int
	ListEndpoints    int
	TotalEndpoints   int
	ReadRatio        float64

	SyncableResources []SyncableResource
	SearchableFields  map[string][]string

	Domain     DomainSignals
	Pagination PaginationProfile
}

func Profile(s *spec.APISpec) *APIProfile {
	if s == nil {
		return &APIProfile{
			SearchableFields: make(map[string][]string),
		}
	}

	p := &APIProfile{
		SearchableFields: make(map[string][]string),
	}

	resourceNames := collectResourceNames(s.Resources)
	syncable := make(map[string]string) // resource name -> list endpoint path
	searchable := make(map[string]map[string]struct{})
	listResources := make(map[string]struct{})

	var getEndpoints int
	var listCapableGETs int
	var hasSearchEndpoint bool

	cursorParams := make(map[string]int)
	pageSizeParams := make(map[string]int)
	sinceParams := make(map[string]int)
	responsePaths := make(map[string]int)

	var walk func(name string, r spec.Resource)
	walk = func(name string, r spec.Resource) {
		resourceName := strings.ToLower(name)
		resourceHasGet := false
		resourceHasPost := false
		resourceHasMutating := false

		if containsAny(resourceName, []string{"webhook", "event", "callback", "notification"}) {
			p.HasRealtime = true
		}
		if containsAny(resourceName, []string{"audit", "log", "event", "history", "activity"}) {
			p.HasChronological = true
		}

		for endpointName, endpoint := range r.Endpoints {
			p.TotalEndpoints++

			method := strings.ToUpper(endpoint.Method)
			switch method {
			case "GET":
				getEndpoints++
				resourceHasGet = true
			case "POST":
				resourceHasPost = true
			case "PUT", "PATCH", "DELETE":
				resourceHasMutating = true
			}

			endpointNameLower := strings.ToLower(endpointName)
			pathLower := strings.ToLower(endpoint.Path)

			if containsAny(endpointNameLower, []string{"search"}) || containsAny(pathLower, []string{"search"}) {
				hasSearchEndpoint = true
			}
			if containsAny(pathLower, []string{"webhook", "event", "callback", "notification"}) {
				p.HasRealtime = true
			}
			if containsAny(pathLower, []string{"audit", "log", "event", "history", "activity"}) || hasChronologicalParams(endpoint.Params) {
				p.HasChronological = true
			}

			if isListEndpoint(endpointName, endpoint) {
				listCapableGETs++
				listResources[resourceName] = struct{}{}
				if endpoint.Pagination != nil {
					p.ListEndpoints++

					// Check for enum-parameterized list endpoints: when a required
					// query param has enum values, each value represents a distinct
					// entity type that should sync independently. Example:
					// GET /v1/api/networkentity?entityType=collection|workspace|api|flow
					// → sync resources: collection, workspace, api, flow
					if enumParam := findEntityTypeEnum(endpoint); enumParam != nil && len(enumParam.Enum) >= 2 {
						for _, val := range enumParam.Enum {
							expandedName := strings.ToLower(val)
							expandedPath := endpoint.Path + "?" + enumParam.Name + "=" + val
							// Enum-expanded paths are more specific than generic resource
							// paths, so they always win on name collision. This ensures
							// deterministic output regardless of Go map iteration order.
							syncable[expandedName] = expandedPath
						}
					} else {
						// Standard: pick the shortest path for determinism when
						// multiple paginated list endpoints exist for the same resource.
						if existing, ok := syncable[resourceName]; !ok || len(endpoint.Path) < len(existing) {
							syncable[resourceName] = endpoint.Path
						}
					}
				}
			}

			if endpoint.Pagination != nil {
				if endpoint.Pagination.CursorParam != "" {
					cursorParams[endpoint.Pagination.CursorParam]++
				}
				if endpoint.Pagination.LimitParam != "" {
					pageSizeParams[endpoint.Pagination.LimitParam]++
				}
			}
			if endpoint.ResponsePath != "" {
				responsePaths[endpoint.ResponsePath]++
			}
			for _, param := range endpoint.Params {
				name := strings.ToLower(param.Name)
				if strings.Contains(name, "since") || strings.Contains(name, "updated_after") || strings.Contains(name, "modified_since") || strings.Contains(name, "updated_at") {
					sinceParams[param.Name]++
				}
			}

			if len(endpoint.Body) > 10 {
				p.ComplexResources = true
			}
			if hasLifecycleField(endpoint.Body) || hasLifecycleField(endpoint.Params) {
				p.HasLifecycles = true
			}
			if hasFileBody(endpoint.Body) {
				p.HasFileOps = true
			}
			if !p.HasDependencies && hasDependency(endpoint.Body, resourceNames) {
				p.HasDependencies = true
			}

			// Collect searchable string fields from both request body and query
			// params. GET endpoints don't have bodies, but their query params
			// often name the same fields that responses contain (e.g., "name",
			// "query", "search"). This enables FTS5 indexing for those entities.
			allFields := collectStringFields(endpoint.Body)
			if endpoint.Method == "GET" || endpoint.Method == "" {
				allFields = append(allFields, collectStringFields(endpoint.Params)...)
			}
			for _, field := range allFields {
				if searchable[resourceName] == nil {
					searchable[resourceName] = make(map[string]struct{})
				}
				searchable[resourceName][field] = struct{}{}
			}
		}

		if resourceHasGet && resourceHasPost && resourceHasMutating {
			p.CRUDResources++
		}

		for subName, sub := range r.SubResources {
			walk(subName, sub)
		}
	}

	for name, resource := range s.Resources {
		walk(name, resource)
	}

	if p.TotalEndpoints > 0 {
		p.ReadRatio = float64(getEndpoints) / float64(p.TotalEndpoints)
		p.OfflineValuable = p.ReadRatio > 0.6
	}
	if listCapableGETs > 0 {
		paginationRatio := float64(p.ListEndpoints) / float64(listCapableGETs)
		// HighVolume: either >50% of list endpoints are paginated, or 5+ paginated endpoints exist
		p.HighVolume = paginationRatio > 0.5 || p.ListEndpoints >= 5
	}
	// NeedsSearch: 3+ list resources exist and fewer than half have dedicated search endpoints
	searchEndpointCount := 0
	if hasSearchEndpoint {
		searchEndpointCount = 1 // conservative: count as 1 even if multiple search endpoints exist
	}
	p.NeedsSearch = len(listResources) >= 3 && float64(searchEndpointCount)/float64(len(listResources)) < 0.5

	p.SyncableResources = sortedSyncableResources(syncable)
	for resource, fields := range searchable {
		p.SearchableFields[resource] = sortedKeys(fields)
	}

	p.Domain = detectDomainSignals(s)

	p.Pagination = PaginationProfile{
		CursorParam:     mostCommon(cursorParams, "after"),
		PageSizeParam:   mostCommon(pageSizeParams, "limit"),
		SinceParam:      mostCommon(sinceParams, ""),
		ItemsKey:        mostCommon(responsePaths, ""),
		DefaultPageSize: 100,
	}

	return p
}

func (p *APIProfile) ToVisionaryPlan(apiName string) *vision.VisionaryPlan {
	if p == nil {
		p = &APIProfile{}
	}

	plan := &vision.VisionaryPlan{
		APIName: apiName,
		Identity: vision.APIIdentity{
			CoreEntities: syncableResourceNames(p.SyncableResources),
			DataProfile: vision.DataProfile{
				Volume:     lowHigh(p.HighVolume),
				SearchNeed: lowHigh(p.NeedsSearch),
				Realtime:   p.HasRealtime,
			},
		},
	}

	plan.Domain = vision.DomainInfo{
		Archetype:    string(p.Domain.Archetype),
		HasAssignees: p.Domain.HasAssignees,
		HasDueDates:  p.Domain.HasDueDates,
		HasPriority:  p.Domain.HasPriority,
		HasTeams:     p.Domain.HasTeams,
		HasLabels:    p.Domain.HasLabels,
		HasEstimates: p.Domain.HasEstimates,
	}

	plan.Architecture = append(plan.Architecture,
		vision.ArchitectureDecision{
			Area:               "persistence",
			NeedLevel:          lowHigh(p.HighVolume || p.OfflineValuable),
			Decision:           "local store",
			Rationale:          "Read-heavy or high-volume APIs benefit from local persistence for repeat access and offline workflows.",
			ImplementationHint: "Use SQLite-backed storage and cache frequently accessed resources.",
		},
		vision.ArchitectureDecision{
			Area:               "search",
			NeedLevel:          lowHigh(p.NeedsSearch),
			Decision:           "full-text indexing",
			Rationale:          "Multi-resource list-heavy APIs need a fast local search surface when no dedicated endpoint exists.",
			ImplementationHint: "Index string fields in FTS5 tables keyed by resource type.",
		},
		vision.ArchitectureDecision{
			Area:               "realtime",
			NeedLevel:          lowHigh(p.HasRealtime),
			Decision:           "streaming event tail",
			Rationale:          "Webhook and event-heavy APIs warrant live inspection workflows.",
			ImplementationHint: "Offer tail-style commands that poll or stream event resources.",
		},
	)

	for _, featureName := range p.RecommendedFeatures() {
		feature := featureIdeaFor(featureName, p)
		feature.TotalScore = feature.ComputeScore()
		plan.Features = append(plan.Features, feature)
	}

	return plan
}

func (p *APIProfile) RecommendedFeatures() []string {
	if p == nil {
		return []string{"export", "import"}
	}

	var features []string
	if p.HighVolume {
		features = append(features, "sync")
	}
	if p.NeedsSearch {
		features = append(features, "search")
	}
	if p.HighVolume || p.NeedsSearch || p.HasDependencies {
		features = append(features, "store")
	}

	features = append(features, "export", "import")

	if p.HasRealtime || p.HasChronological {
		features = append(features, "tail")
	}
	if p.HighVolume || p.HasChronological {
		features = append(features, "analytics")
	}

	return features
}

// SyncableResourceNames returns the names of the syncable resources.
func (p *APIProfile) SyncableResourceNames() []string {
	return syncableResourceNames(p.SyncableResources)
}

func featureIdeaFor(name string, p *APIProfile) vision.FeatureIdea {
	switch name {
	case "sync":
		return scoredFeature(
			"sync",
			"Continuously mirror paginated resources into a local cache for fast bulk access.",
			[]string{"sync.go.tmpl"},
			2, 3, 2, 1, 2, 3, 2, 1,
		)
	case "search":
		return scoredFeature(
			"search",
			"Search across locally indexed records when the upstream API lacks a dedicated search endpoint.",
			[]string{"search.go.tmpl"},
			2, 3, 2, 1, 2, 3, 2, 1,
		)
	case "store":
		return scoredFeature(
			"store",
			"Persist fetched records locally to support repeat access, joins, and offline work.",
			[]string{"store.go.tmpl"},
			2, 2, 3, 1, 2, 2, 2, 1,
		)
	case "export":
		return scoredFeature(
			"export",
			"Export API records into shell-friendly formats for scripting and archival.",
			[]string{"export.go.tmpl"},
			1, 2, 3, 1, 2, 1, 3, 1,
		)
	case "import":
		return scoredFeature(
			"import",
			"Import records from files or stdin to support bootstrap and migration workflows.",
			[]string{"import.go.tmpl"},
			1, 2, 3, 1, 2, 1, 3, 1,
		)
	case "tail":
		return scoredFeature(
			"tail",
			"Tail event-like resources to inspect API activity as it happens.",
			[]string{"tail.go.tmpl"},
			2, 3, 2, 1, 1, dataFit(p.HasRealtime || p.HasChronological), 2, 1,
		)
	case "analytics":
		return scoredFeature(
			"analytics",
			"Run local analytics over synced records to summarize high-volume or historical activity.",
			[]string{"analytics.go.tmpl"},
			2, 2, 2, 1, 2, dataFit(p.HighVolume || p.HasChronological), 2, 1,
		)
	default:
		return vision.FeatureIdea{Name: name}
	}
}

func scoredFeature(name, description string, templates []string, evidence, impact, feasibility, uniqueness, composability, fit, maintainability, moat int) vision.FeatureIdea {
	return vision.FeatureIdea{
		Name:                      name,
		Description:               description,
		EvidenceStrength:          evidence,
		UserImpact:                impact,
		ImplementationFeasibility: feasibility,
		Uniqueness:                uniqueness,
		Composability:             composability,
		DataProfileFit:            fit,
		Maintainability:           maintainability,
		CompetitiveMoat:           moat,
		TemplateNames:             templates,
	}
}

func lowHigh(v bool) string {
	if v {
		return "high"
	}
	return "low"
}

func dataFit(v bool) int {
	if v {
		return 3
	}
	return 1
}

func isListEndpoint(name string, endpoint spec.Endpoint) bool {
	if strings.ToUpper(endpoint.Method) != "GET" {
		return false
	}
	if endpoint.Pagination != nil {
		return true
	}
	if endpoint.Response.Type == "array" {
		return true
	}

	name = strings.ToLower(name)
	return containsAny(name, []string{"list", "all"})
}

// findEntityTypeEnum returns the first required enum query param on a list endpoint
// that looks like an entity type selector. Heuristics:
// 1. Param is required with 2+ enum values
// 2. Param name contains "type", "kind", "entity", "resource", or "category"
// Returns nil if no qualifying param is found. Does NOT fall back to arbitrary
// enum params — filters like status=open|closed should not trigger expansion.
func findEntityTypeEnum(endpoint spec.Endpoint) *spec.Param {
	for i := range endpoint.Params {
		p := &endpoint.Params[i]
		if len(p.Enum) < 2 || p.PathParam || !p.Required {
			continue
		}
		nameLower := strings.ToLower(p.Name)
		if containsAny(nameLower, []string{"type", "kind", "entity", "resource", "category"}) {
			return p
		}
	}
	return nil
}

func hasLifecycleField(params []spec.Param) bool {
	for _, param := range params {
		if isLifecycleParam(param) {
			return true
		}
		if hasLifecycleField(param.Fields) {
			return true
		}
	}
	return false
}

func isLifecycleParam(param spec.Param) bool {
	name := strings.ToLower(param.Name)
	return (name == "status" || name == "state") && len(param.Enum) >= 3
}

func hasFileBody(params []spec.Param) bool {
	for _, param := range params {
		if strings.EqualFold(param.Type, "file") || strings.EqualFold(param.Format, "binary") {
			return true
		}
		if hasFileBody(param.Fields) {
			return true
		}
	}
	return false
}

func hasDependency(params []spec.Param, resourceNames map[string]struct{}) bool {
	for _, param := range params {
		name := strings.ToLower(param.Name)
		if strings.HasSuffix(name, "_id") && strings.EqualFold(param.Type, "string") {
			prefix := strings.TrimSuffix(name, "_id")
			if matchesResource(prefix, resourceNames) {
				return true
			}
		}
		if hasDependency(param.Fields, resourceNames) {
			return true
		}
	}
	return false
}

func matchesResource(name string, resourceNames map[string]struct{}) bool {
	for _, variant := range nameVariants(name) {
		if _, ok := resourceNames[variant]; ok {
			return true
		}
	}
	return false
}

func collectResourceNames(resources map[string]spec.Resource) map[string]struct{} {
	names := make(map[string]struct{})

	var walk func(name string, r spec.Resource)
	walk = func(name string, r spec.Resource) {
		for _, variant := range nameVariants(name) {
			names[variant] = struct{}{}
		}
		for subName, sub := range r.SubResources {
			walk(subName, sub)
		}
	}

	for name, resource := range resources {
		walk(name, resource)
	}

	return names
}

func nameVariants(name string) []string {
	normalized := normalizeName(name)
	if normalized == "" {
		return nil
	}

	seen := map[string]struct{}{normalized: {}}
	var variants []string
	variants = append(variants, normalized)

	if strings.HasSuffix(normalized, "ies") {
		addVariant(normalized[:len(normalized)-3]+"y", seen, &variants)
	}
	if strings.HasSuffix(normalized, "es") {
		addVariant(strings.TrimSuffix(normalized, "es"), seen, &variants)
	}
	if strings.HasSuffix(normalized, "s") {
		addVariant(strings.TrimSuffix(normalized, "s"), seen, &variants)
	}

	return variants
}

func addVariant(variant string, seen map[string]struct{}, variants *[]string) {
	if variant == "" {
		return
	}
	if _, ok := seen[variant]; ok {
		return
	}
	seen[variant] = struct{}{}
	*variants = append(*variants, variant)
}

func normalizeName(name string) string {
	replacer := strings.NewReplacer("-", "_", " ", "_")
	return strings.Trim(replacer.Replace(strings.ToLower(name)), "_")
}

func collectStringFields(params []spec.Param) []string {
	fields := make(map[string]struct{})
	var walk func(items []spec.Param)
	walk = func(items []spec.Param) {
		for _, param := range items {
			if strings.EqualFold(param.Type, "string") {
				fields[param.Name] = struct{}{}
			}
			if len(param.Fields) > 0 {
				walk(param.Fields)
			}
		}
	}
	walk(params)
	return sortedKeys(fields)
}

func hasChronologicalParams(params []spec.Param) bool {
	for _, param := range params {
		name := strings.ToLower(param.Name)
		desc := strings.ToLower(param.Description)

		if name == "since" || name == "until" || name == "before" || name == "after" {
			return true
		}
		if strings.Contains(name, "timestamp") || strings.Contains(name, "created_at") || strings.Contains(name, "updated_at") {
			return true
		}
		if (strings.Contains(name, "sort") || strings.Contains(name, "order")) &&
			(strings.Contains(desc, "time") || strings.Contains(desc, "date") || strings.Contains(desc, "timestamp") || strings.Contains(desc, "created")) {
			return true
		}
		if hasChronologicalParams(param.Fields) {
			return true
		}
	}
	return false
}

func containsAny(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedSyncableResources converts a name->path map into a sorted slice of SyncableResource.
func sortedSyncableResources(m map[string]string) []SyncableResource {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	resources := make([]SyncableResource, len(names))
	for i, name := range names {
		resources[i] = SyncableResource{Name: name, Path: m[name]}
	}
	return resources
}

// syncableResourceNames extracts just the names from a slice of SyncableResource.
func syncableResourceNames(resources []SyncableResource) []string {
	names := make([]string, len(resources))
	for i, r := range resources {
		names[i] = r.Name
	}
	return names
}

func detectDomainSignals(s *spec.APISpec) DomainSignals {
	if s == nil {
		return DomainSignals{Archetype: ArchetypeGeneric}
	}

	scores := map[DomainArchetype]int{
		ArchetypeCommunication:     0,
		ArchetypeProjectMgmt:       0,
		ArchetypePayments:          0,
		ArchetypeInfrastructure:    0,
		ArchetypeContent:           0,
		ArchetypeCRM:               0,
		ArchetypeDeveloperPlatform: 0,
	}

	resourceKeywords := map[DomainArchetype][]string{
		ArchetypeCommunication:     {"message", "channel", "chat", "thread", "conversation", "dm", "reaction"},
		ArchetypeProjectMgmt:       {"issue", "task", "ticket", "project", "sprint", "milestone", "board", "epic", "backlog"},
		ArchetypePayments:          {"charge", "payment", "invoice", "subscription", "refund", "payout", "transaction", "balance", "transfer"},
		ArchetypeInfrastructure:    {"server", "instance", "cluster", "deployment", "container", "node", "pod", "volume", "network"},
		ArchetypeContent:           {"article", "post", "page", "blog", "content", "document", "media", "asset", "collection"},
		ArchetypeCRM:               {"contact", "deal", "lead", "opportunity", "account", "pipeline", "company", "person"},
		ArchetypeDeveloperPlatform: {"repository", "commit", "branch", "pull_request", "merge_request", "pipeline", "build", "release", "package"},
	}

	ds := DomainSignals{}

	var walkResources func(name string, r spec.Resource)
	walkResources = func(name string, r spec.Resource) {
		nameLower := strings.ToLower(name)
		for archetype, keywords := range resourceKeywords {
			for _, kw := range keywords {
				if strings.Contains(nameLower, kw) {
					scores[archetype] += 2
				}
			}
		}

		for _, endpoint := range r.Endpoints {
			scanFieldSignals(endpoint.Params, &ds)
			scanFieldSignals(endpoint.Body, &ds)
		}

		for subName, sub := range r.SubResources {
			walkResources(subName, sub)
		}
	}

	for name, resource := range s.Resources {
		walkResources(name, resource)
	}

	// Pick the archetype with the highest score
	bestArchetype := ArchetypeGeneric
	bestScore := 0
	for archetype, score := range scores {
		if score > bestScore {
			bestScore = score
			bestArchetype = archetype
		}
	}
	ds.Archetype = bestArchetype

	return ds
}

func scanFieldSignals(params []spec.Param, ds *DomainSignals) {
	for _, param := range params {
		name := strings.ToLower(param.Name)

		if strings.Contains(name, "assignee") || name == "assignee_id" || name == "assigned_to" {
			ds.HasAssignees = true
		}
		if strings.Contains(name, "priority") {
			ds.HasPriority = true
		}
		if strings.Contains(name, "due_date") || strings.Contains(name, "due_at") || strings.Contains(name, "deadline") {
			ds.HasDueDates = true
		}
		if strings.Contains(name, "team") || name == "team_id" {
			ds.HasTeams = true
		}
		if strings.Contains(name, "label") || strings.Contains(name, "tag") {
			ds.HasLabels = true
		}
		if strings.Contains(name, "estimate") || strings.Contains(name, "story_points") || strings.Contains(name, "points") {
			ds.HasEstimates = true
		}
		if strings.Contains(name, "thread") || strings.Contains(name, "reply_to") || strings.Contains(name, "parent_id") {
			ds.HasThreading = true
		}
		if strings.Contains(name, "amount") || strings.Contains(name, "currency") || strings.Contains(name, "price") {
			ds.HasTransactions = true
		}
		if strings.Contains(name, "subscription") || strings.Contains(name, "recurring") || strings.Contains(name, "interval") {
			ds.HasSubscriptions = true
		}
		if strings.Contains(name, "media") || strings.Contains(name, "attachment") || strings.Contains(name, "image") || strings.Contains(name, "file") {
			ds.HasMedia = true
		}

		if len(param.Fields) > 0 {
			scanFieldSignals(param.Fields, ds)
		}
	}
}

func mostCommon(counts map[string]int, fallback string) string {
	if len(counts) == 0 {
		return fallback
	}
	best := fallback
	bestCount := 0
	for k, v := range counts {
		if v > bestCount {
			best = k
			bestCount = v
		}
	}
	return best
}

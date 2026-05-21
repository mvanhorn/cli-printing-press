package profiler

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/vision"
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
	CursorType      string `json:"cursor_type"`       // most common paginator class (cursor, page_token, offset, page); drives runtime iteration strategy
	PageSizeParam   string `json:"page_size_param"`   // most common page size param (limit, per_page, page_size, first)
	SinceParam      string `json:"since_param"`       // temporal filter param (since, updated_after, modified_since)
	DateRangeParam  string `json:"date_range_param"`  // date-range filter param (dates, date_range, dateRange)
	ItemsKey        string `json:"items_key"`         // response array key (data, results, items, or "" for root array)
	DefaultPageSize int    `json:"default_page_size"` // detected or default 100
}

// SearchBodyField describes an additional body field needed for POST search endpoints.
type SearchBodyField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`    // string, integer, boolean, array
	Default  any    `json:"default"` // default value from spec, or synthesized from enum
	Required bool   `json:"required"`
}

// SyncBodyField describes a request-body field on a syncable POST list endpoint.
type SyncBodyField struct {
	Name       string
	Type       string
	Default    any
	HasDefault bool
}

// DiscriminatorMapping routes one discriminator value to the concrete resource
// whose typed table should receive the item.
type DiscriminatorMapping struct {
	Value    string
	Resource string
}

// DiscriminatorDispatch describes a mixed response payload whose items carry a
// discriminator field such as type/kind/__typename/objectType.
type DiscriminatorDispatch struct {
	Field    string
	Mappings []DiscriminatorMapping
}

// SyncableResource describes a resource that supports list sync (paginated or single-page).
type SyncableResource struct {
	Name   string
	Path   string
	Method string
	Tier   string
	// IDField is the resolved primary-key field name for items returned by the
	// list endpoint, populated from the chosen endpoint's resolved value (in
	// turn populated by the OpenAPI parser's `x-resource-id` extension or the
	// response-schema fallback chain). Empty when no key could be resolved;
	// templates fall back to runtime list scanning.
	IDField string
	// Critical flags this resource as essential — its failure during sync
	// should fail the whole run even under the new (non-strict) exit-code
	// policy. Defaults to false.
	Critical bool

	// SinceParam is the actual query parameter name this resource's list
	// endpoint declares for incremental temporal filtering (since,
	// updated_after, modified_since, …). Empty when the endpoint declares
	// no such parameter; the sync template skips temporal filtering for
	// those resources and emits one resource_not_incremental warning per
	// run when --since/incremental sync was requested.
	SinceParam string

	// SupportsPagination is true when the chosen list endpoint declares a
	// cursor or page-size parameter. The sync template uses this to avoid
	// sending synthetic limit/offset params to strict non-paginated list
	// endpoints.
	SupportsPagination bool

	// BodyFields names request-body fields on POST list endpoints. Sync uses
	// this to send pagination and user-supplied params in the body for
	// RPC-style list calls.
	BodyFields []SyncBodyField

	// Discriminator routes heterogeneous response items to concrete typed
	// resources before storage. Empty when the endpoint returns a homogeneous
	// resource.
	Discriminator DiscriminatorDispatch
}

// DependentResource describes a child resource that requires iterating a parent
// to sync (e.g., /channels/{channelId}/messages depends on channels).
type DependentResource struct {
	Name           string // child resource name, e.g. "messages"
	ParentResource string // parent resource name, e.g. "channels"
	ParentIDParam  string // path param name, e.g. "channel_id"
	Path           string // full path template, e.g. "/channels/{channel_id}/messages"
	Method         string
	Tier           string

	// IDField is the primary-key field name resolved from the spec
	// (x-resource-id extension or the four-tier fallback chain). Empty when
	// no override applies; templates fall back to a generic runtime list.
	// Mirrors SyncableResource.IDField — annotations on a child path-item
	// flow into this field so the override map covers dependent resources
	// too, not just flat resources.
	IDField string

	// Critical signals that a failure of this dependent resource should
	// fail the whole sync run regardless of --strict. Mirrors
	// SyncableResource.Critical so spec authors can mark child paths as
	// load-bearing.
	Critical bool

	// SinceParam mirrors SyncableResource.SinceParam for child paths so
	// the same per-resource temporal-filter gating applies to dependent
	// syncs.
	SinceParam string

	// SupportsPagination mirrors SyncableResource.SupportsPagination for child
	// paths so dependent syncs skip synthetic limit/offset params on endpoints
	// that do not declare page-size pagination.
	SupportsPagination bool

	// BodyFields mirrors SyncableResource.BodyFields for child sync paths.
	BodyFields []SyncBodyField

	// Discriminator routes heterogeneous dependent-resource response items to
	// concrete typed resources before storage.
	Discriminator DiscriminatorDispatch

	// KeyField, when non-empty, names the field to extract from each parent
	// record for substitution into the child path — overriding the default of
	// using the parent's primary key (IDField on the parent's SyncableResource
	// entry). Populated from a spec-declared walker (Endpoint.Walker.KeyField
	// in internal YAML, or the `key_field` key under `x-pp-sync-walker` in
	// OpenAPI). When empty, the existing parent-primary-key flow runs.
	KeyField string
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

	SyncableResources      []SyncableResource
	DependentSyncResources []DependentResource
	SearchableFields       map[string][]string

	// SearchEndpointPath is the API path for live search (e.g., "/search", "/users/search").
	// Empty if the API has no search endpoint.
	SearchEndpointPath string
	// SearchQueryParam is the query parameter name for the search endpoint (e.g., "q", "query").
	// Defaults to "q" if a search endpoint exists but no recognized param is found.
	SearchQueryParam string
	// SearchEndpointMethod is the HTTP method for the search endpoint (GET or POST).
	SearchEndpointMethod string
	// SearchBodyFields holds additional body fields (beyond the query param) needed for POST
	// search endpoints. Each entry has name, default value, and type. The search template
	// uses these to construct the full POST body at generation time.
	SearchBodyFields []SearchBodyField

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

	resourceNames, resourceNameIndex := collectResourceNameMetadata(s.Resources)
	syncable := make(map[string]syncableMeta) // resource name -> chosen list endpoint metadata
	syncCandidates := make(map[string][]syncableCandidate)
	addSyncCandidate := func(resourceName string, meta syncableMeta) {
		for _, candidate := range syncCandidates[resourceName] {
			if candidate.meta.Path == meta.Path {
				return
			}
		}
		syncCandidates[resourceName] = append(syncCandidates[resourceName], syncableCandidate{
			meta: meta,
		})
	}
	// Keyed by "<parent>/<leaf>" so the same leaf under multiple parents
	// survives instead of first-seen-wins.
	parameterized := make(map[string]parameterizedEntry)
	// Mirrors the schema builder's table-naming so DependentResource.Name
	// lines up byte-for-byte.
	shardedSubResources := spec.SubResourceShardedNames(s)
	searchable := make(map[string]map[string]struct{})
	listResources := make(map[string]struct{})

	var getEndpoints int
	var listCapableEndpoints int
	var hasSearchEndpoint bool

	cursorParams := make(map[string]int)
	cursorTypes := make(map[string]int)
	pageSizeParams := make(map[string]int)
	sinceParams := make(map[string]int)
	dateRangeParams := make(map[string]int)
	responsePaths := make(map[string]int)

	var walk func(name string, r spec.Resource, inheritedTier string, parentName string)
	walk = func(name string, r spec.Resource, inheritedTier string, parentName string) {
		if r.Tier == "" {
			r.Tier = inheritedTier
		}
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
				// Prefer shorter/more general search paths (e.g., /search over /users/search)
				if p.SearchEndpointPath == "" || len(endpoint.Path) < len(p.SearchEndpointPath) {
					method := strings.ToUpper(endpoint.Method)
					p.SearchEndpointPath = endpoint.Path
					p.SearchEndpointMethod = method
					p.SearchQueryParam = "q" // default
					p.SearchBodyFields = nil

					// Find the query parameter
					searchParamNames := []string{"q", "query", "search", "keyword", "term", "querytext", "searchterm", "searchtext", "text"}
					isSearchParam := func(name string) bool {
						lower := strings.ToLower(name)
						return slices.Contains(searchParamNames, lower)
					}

					for _, param := range endpoint.Params {
						if isSearchParam(param.Name) {
							p.SearchQueryParam = param.Name
							break
						}
					}

					// For POST endpoints, check body params for query param and
					// capture additional required fields with their defaults
					if method == "POST" {
						for _, param := range endpoint.Body {
							if isSearchParam(param.Name) {
								p.SearchQueryParam = param.Name
								continue
							}
							// Capture non-query body fields so the template can
							// construct the full POST body at generation time
							field := SearchBodyField{
								Name:     param.Name,
								Type:     param.Type,
								Required: param.Required,
							}
							// Use spec default if available
							if param.Default != nil {
								field.Default = param.Default
							} else if len(param.Enum) > 0 {
								// For arrays with enum values, use all enum values as default
								if param.Type == "array" {
									field.Default = param.Enum
								} else {
									field.Default = param.Enum[0]
								}
							} else if param.Type == "array" && len(param.Fields) > 0 && len(param.Fields[0].Enum) > 0 {
								// Array items have enum — use all enum values (e.g., search all entity types)
								field.Default = param.Fields[0].Enum
							} else {
								// Synthesize reasonable defaults by type
								switch param.Type {
								case "integer", "number":
									field.Default = 10
								case "boolean":
									field.Default = true
								case "string":
									field.Default = ""
								case "object":
									field.Default = map[string]any{}
								case "array":
									field.Default = []any{}
								}
							}
							p.SearchBodyFields = append(p.SearchBodyFields, field)
						}
					}
				}
			}
			if containsAny(pathLower, []string{"webhook", "event", "callback", "notification"}) {
				p.HasRealtime = true
			}
			if containsAny(pathLower, []string{"audit", "log", "event", "history", "activity"}) || hasChronologicalParams(endpoint.Params) {
				p.HasChronological = true
			}

			if isListEndpoint(endpointName, endpoint, s.Types) {
				listCapableEndpoints++
				listResources[resourceName] = struct{}{}

				// pathParamsAllTemplateVars treats paths whose only
				// {placeholder}s are spec-declared EndpointTemplateVars
				// (e.g. /tenant/{tenant}/<resource> when "tenant" is the
				// tenant-scoping path-positional template) as standalone.
				// buildURL substitutes those from env-backed
				// Config.TemplateVars at request time, so they don't need
				// parent-context iteration like /channels/{channelId}/messages
				// does.
				resolvable := pathParamsAllTemplateVars(endpoint.Path, s)
				standaloneList := (!strings.Contains(endpoint.Path, "{") || resolvable) && !hasRequiredScopeParams(endpoint)

				if endpoint.Pagination != nil {
					p.ListEndpoints++

					// Check for enum-parameterized list endpoints: when a required
					// query param has enum values, each value represents a distinct
					// entity type that should sync independently. Example:
					// GET /v1/api/networkentity?entityType=collection|workspace|api|flow
					// → sync resources: collection, workspace, api, flow
					if enumParam := findEntityTypeEnum(endpoint); enumParam != nil && len(enumParam.Enum) >= 2 {
						addSyncCandidate(resourceName, metaFromEndpoint(s, r, endpoint, s.Types, resourceNameIndex))
						for _, val := range enumParam.Enum {
							expandedName := strings.ToLower(val)
							expandedPath := endpoint.Path + "?" + enumParam.Name + "=" + val
							// Enum-expanded paths are more specific than generic resource
							// paths, so they always win on name collision. This ensures
							// deterministic output regardless of Go map iteration order.
							meta := metaFromEndpoint(s, r, endpoint, s.Types, resourceNameIndex)
							meta.Path = expandedPath
							syncable[expandedName] = meta
						}
					} else if strings.Contains(endpoint.Path, "{") && !resolvable {
						// Parameterized paginated paths can't sync standalone — track
						// them for dependent-resource detection below. Carry the
						// endpoint's metadata so x-resource-id and x-critical
						// annotations on a child path-item flow into the override
						// and critical-resource maps. Store raw names so
						// detectDependentResources can snake-case downstream.
						key := parentName + "/" + name
						if _, ok := parameterized[key]; !ok {
							parameterized[key] = parameterizedEntry{
								name:       name,
								parentName: parentName,
								meta:       metaFromEndpoint(s, r, endpoint, s.Types, resourceNameIndex),
							}
						}
					} else if standaloneList {
						addSyncCandidate(resourceName, metaFromEndpoint(s, r, endpoint, s.Types, resourceNameIndex))
					}
				} else if standaloneList {
					addSyncCandidate(resourceName, metaFromEndpoint(s, r, endpoint, s.Types, resourceNameIndex))
				}
			} else if method == "GET" && (!strings.Contains(endpoint.Path, "{") || pathParamsAllTemplateVars(endpoint.Path, s)) && !hasRequiredScopeParams(endpoint) && looksLikeCollectionEndpoint(endpointNameLower) {
				// Catch-all for simple GET collection endpoints that isListEndpoint
				// didn't recognise (e.g., response is an untyped object with no
				// wrapper field defined in the spec's types map).
				// Only include endpoints whose name suggests a collection (list, all,
				// index, etc.) — exclude singular getters like "get" or "show".
				addSyncCandidate(resourceName, metaFromEndpoint(s, r, endpoint, s.Types, resourceNameIndex))
			}

			if endpoint.Pagination != nil {
				if endpoint.Pagination.CursorParam != "" {
					cursorParams[endpoint.Pagination.CursorParam]++
				}
				if endpoint.Pagination.Type != "" {
					cursorTypes[endpoint.Pagination.Type]++
				}
				if endpoint.Pagination.LimitParam != "" {
					pageSizeParams[endpoint.Pagination.LimitParam]++
				}
			} else {
				// Fallback for specs that expose pagination via plain params
				// instead of a structured pagination: block.
				for _, param := range endpoint.Params {
					if param.PathParam || param.Positional {
						continue
					}
					lower := strings.ToLower(param.Name)
					if cursorParamCandidates[lower] {
						cursorParams[param.Name]++
					}
					if pageSizeParamCandidates[lower] {
						pageSizeParams[param.Name]++
					}
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
				if name == "dates" || name == "date_range" || name == "daterange" {
					dateRangeParams[param.Name]++
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

		subNames := sortedKeys(r.SubResources)
		for _, subName := range subNames {
			sub := r.SubResources[subName]
			walk(subName, sub, r.Tier, name)
		}
	}

	for name, resource := range s.Resources {
		walk(name, resource, "", "")
	}
	applySyncCandidates(syncable, syncCandidates)

	if p.TotalEndpoints > 0 {
		p.ReadRatio = float64(getEndpoints) / float64(p.TotalEndpoints)
		p.OfflineValuable = p.ReadRatio > 0.6
	}
	if listCapableEndpoints > 0 {
		paginationRatio := float64(p.ListEndpoints) / float64(listCapableEndpoints)
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
	p.DependentSyncResources = detectDependentResources(parameterized, syncable, shardedSubResources)
	p.DependentSyncResources = applySpecWalkers(s, p.DependentSyncResources, syncable, s.Types, resourceNameIndex)
	for resource, fields := range searchable {
		p.SearchableFields[resource] = sortedKeys(fields)
	}

	p.Domain = detectDomainSignals(s)

	p.Pagination = PaginationProfile{
		CursorParam:     mostCommon(cursorParams, "after"),
		CursorType:      mostCommon(cursorTypes, ""),
		PageSizeParam:   mostCommon(pageSizeParams, "limit"),
		SinceParam:      mostCommon(sinceParams, ""),
		DateRangeParam:  mostCommon(dateRangeParams, ""),
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

// Lowercase-keyed candidate sets shared by the profiler's pagination
// inference path and hasRequiredScopeParams.
var (
	pageSizeParamCandidates = map[string]bool{
		"limit": true, "per_page": true, "page_size": true, "pagesize": true,
		"first": true, "count": true, "max_results": true, "page[size]": true,
	}
	cursorParamCandidates = map[string]bool{
		"after": true, "cursor": true, "page_token": true, "offset": true,
		"page": true, "before": true, "starting_after": true, "page[cursor]": true,
	}
)

// pathTemplatePlaceholderRE matches {placeholder} tokens in a path. Identifier
// shape mirrors templateVarPattern in the emitted url.go.tmpl so client-side
// resolution sees the same set of names this helper accepts.
var pathTemplatePlaceholderRE = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// pathParamsAllTemplateVars reports whether every {placeholder} in path is
// declared in s.EndpointTemplateVars — i.e. fully resolvable via the printed
// CLI's runtime buildURL substitution without parent-context iteration. Paths
// with no {placeholder}s return false; the standaloneList gate handles those
// separately.
func pathParamsAllTemplateVars(path string, s *spec.APISpec) bool {
	if s == nil || len(s.EndpointTemplateVars) == 0 || !strings.Contains(path, "{") {
		return false
	}
	matches := pathTemplatePlaceholderRE.FindAllStringSubmatch(path, -1)
	if len(matches) == 0 {
		return false
	}
	for _, m := range matches {
		if !s.IsEndpointTemplateVar(m[1]) {
			return false
		}
	}
	return true
}

// hasRequiredScopeParams flags "scoped list" endpoints (e.g., GetFriendList
// requires steamid) that can't be synced without runtime context.
func hasRequiredScopeParams(endpoint spec.Endpoint) bool {
	temporalOrFormatParams := map[string]bool{
		"since": true, "updated_after": true, "modified_since": true, "since_id": true,
		"key": true, "format": true,
	}
	for _, param := range endpoint.Params {
		if param.Required && !param.Positional && !param.PathParam {
			lower := strings.ToLower(param.Name)
			if pageSizeParamCandidates[lower] || cursorParamCandidates[lower] || temporalOrFormatParams[lower] {
				continue
			}
			// Enum params with 2+ values are handled by enum expansion, not scope
			if len(param.Enum) >= 2 {
				continue
			}
			return true
		}
	}
	return false
}

func isListEndpoint(name string, endpoint spec.Endpoint, types map[string]spec.TypeDef) bool {
	method := strings.ToUpper(endpoint.Method)

	if method == "POST" {
		return endpoint.Pagination != nil &&
			looksLikeCollectionEndpoint(strings.ToLower(name)) &&
			hasListShapedResponse(endpoint, types)
	}

	if method != "GET" {
		return false
	}
	if endpoint.Pagination != nil {
		return true
	}
	if hasListShapedResponse(endpoint, types) {
		return true
	}

	return looksLikeBasicGetListEndpoint(strings.ToLower(name))
}

func hasListShapedResponse(endpoint spec.Endpoint, types map[string]spec.TypeDef) bool {
	if endpoint.Response.Type == "array" {
		return true
	}

	// Check for wrapper-object responses: the endpoint returns type "object"
	// and the referenced type has a field matching a known wrapper key. These
	// are list endpoints that wrap their arrays (e.g., {events: [...]}).
	// The key list matches extractPageItems in sync.go.tmpl plus "events".
	return endpoint.Response.Type == "object" &&
		endpoint.Response.Item != "" &&
		hasWrapperArrayField(endpoint.Response.Item, types)
}

// wrapperArrayKeys are response object field names that indicate the object
// wraps a list of items. Kept in sync with extractPageItems in sync.go.tmpl.
// hasWrapperArrayField lowercases each field name before lookup, so
// PascalCase variants ("Items", "Data") match the lowercase entries here.
var wrapperArrayKeys = map[string]bool{
	"data":    true,
	"results": true,
	"items":   true,
	"events":  true,
	"entries": true,
	"records": true,
	"nodes":   true,
}

// hasWrapperArrayField checks whether a named type in the spec's types map
// has any field whose name matches a known wrapper key, or whether the type
// name itself suggests a list wrapper (contains "Response", "List", "Result",
// or "Collection"). The type-name heuristic is a fallback for specs where the
// types map is empty or incomplete.
func hasWrapperArrayField(typeName string, types map[string]spec.TypeDef) bool {
	if typeDef, ok := types[typeName]; ok {
		for _, field := range typeDef.Fields {
			if wrapperArrayKeys[strings.ToLower(field.Name)] {
				return true
			}
		}
	}

	// Fallback: if the type name itself suggests a list wrapper, treat it
	// as a wrapper even when the types map lacks field definitions.
	nameUpper := strings.ToUpper(typeName)
	return strings.Contains(nameUpper, "RESPONSE") ||
		strings.Contains(nameUpper, "LIST") ||
		strings.Contains(nameUpper, "RESULT") ||
		strings.Contains(nameUpper, "COLLECTION")
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

// looksLikeCollectionEndpoint returns true when the endpoint name suggests it
// returns a list of items rather than a single resource. Used as a guard for
// the catch-all syncable-resource heuristic so that singleton getters like
// "get" or "show" are excluded.
func looksLikeCollectionEndpoint(nameLower string) bool {
	return containsAny(nameLower, collectionEndpointTerms)
}

var collectionEndpointTerms = []string{"list", "all", "index", "search", "query", "browse", "find"}

func looksLikeBasicGetListEndpoint(nameLower string) bool {
	return containsAny(nameLower, basicGetListEndpointTerms)
}

var basicGetListEndpointTerms = []string{"list", "all"}

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

func collectResourceNameMetadata(resources map[string]spec.Resource) (map[string]struct{}, map[string]string) {
	names := make(map[string]struct{})
	index := make(map[string]string)

	walkResources(resources, func(name string, _ spec.Resource) {
		resourceName := strings.ToLower(name)
		for _, variant := range nameVariants(name) {
			names[variant] = struct{}{}
			if _, ok := index[variant]; !ok {
				index[variant] = resourceName
			}
		}
	})

	return names, index
}

func walkResources(resources map[string]spec.Resource, visit func(name string, resource spec.Resource)) {
	for _, name := range sortedKeys(resources) {
		resource := resources[name]
		visit(name, resource)
		walkResources(resource.SubResources, visit)
	}
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
	if before, ok := strings.CutSuffix(normalized, "es"); ok {
		addVariant(before, seen, &variants)
	}
	if before, ok := strings.CutSuffix(normalized, "s"); ok {
		addVariant(before, seen, &variants)
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

// detectDependentResources examines parameterized paths and identifies
// parent-child relationships. For example, /channels/{channel_id}/messages
// becomes a dependent resource of "channels" (only one level of nesting).
// When the same leaf appears under multiple parents (or collides with a
// top-level resource), each parent emits a sharded Name so its shard syncs
// to its own table.
func detectDependentResources(parameterized map[string]parameterizedEntry, syncable map[string]syncableMeta, shardedSubResources spec.SubResourceShards) []DependentResource {
	var deps []DependentResource
	for _, entry := range parameterized {
		paramName, ok := firstPathParam(entry.meta.Path)
		if !ok {
			continue
		}
		parentResource := resolveParentResource(entry.parentName, paramName, syncable)
		if parentResource == "" {
			continue
		}

		emittedName := spec.ToSnakeCase(entry.name)
		if shardedSubResources.IsSharded(entry.name) {
			// Prefer the spec-walk parent so multi-param paths
			// (e.g. /repos/{owner}/{repo}/commits) pick the right shard
			// prefix; the path-param fallback would derive "owner".
			shardParent := parentResource
			if entry.parentName != "" {
				shardParent = entry.parentName
			}
			emittedName = spec.ShardedSubResourceTableName(shardParent, entry.name)
		}

		deps = append(deps, DependentResource{
			Name:               emittedName,
			ParentResource:     parentResource,
			ParentIDParam:      paramName,
			Path:               entry.meta.Path,
			Method:             entry.meta.Method,
			Tier:               entry.meta.Tier,
			IDField:            entry.meta.IDField,
			Critical:           entry.meta.Critical,
			SinceParam:         entry.meta.SinceParam,
			SupportsPagination: entry.meta.SupportsPagination,
			BodyFields:         entry.meta.BodyFields,
			Discriminator:      entry.meta.Discriminator,
		})
	}
	// Sort for deterministic output
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Name < deps[j].Name
	})
	return deps
}

// applySpecWalkers merges spec-declared walker configs (Endpoint.Walker,
// populated from the `walker:` internal-YAML field or the `x-pp-sync-walker`
// OpenAPI operation extension) into the dependent-sync set. For each endpoint
// with a non-nil walker, the function either augments the matching
// auto-detected DependentResource (carrying ParentResource, ParentIDParam,
// and KeyField overrides through) or synthesizes a new entry when
// auto-detection missed the link — covering paths where the placeholder name
// does not match a parent resource, or paths with the placeholder in a
// matrix or query parameter that resolveParentResource cannot map.
//
// Walker configs that fail validation are dropped with a stderr warning
// rather than silently. Three checks fail:
//
//   - parent is not a syncable resource: typo or stale spec; without a flat-
//     list parent endpoint there is nothing to iterate.
//   - the child path has 2+ {placeholders} and key_param is not declared
//     explicitly: firstPathParam returns the first placeholder, which on a
//     2-deep path is the parent slot, almost always wrong.
//   - the child path has 0 placeholders and key_param is not declared (the
//     walker would bind via matrix/query but has no slot named).
//
// Existing dependent entries are matched by ("GET "+path) tuple — walker is
// sync-only and GET-only, and a path-only key would collide if two endpoints
// share a path across resources or methods.
//
// Synthesized entries derive Name from spec.ToSnakeCase(resourceName), not
// from the endpoint-map key, so a walker that re-declares an already-auto-
// detected path doesn't create a parallel entry under a different Name.
// All other per-endpoint fields (Tier, IDField, Critical, SinceParam,
// Discriminator) flow through metaFromEndpoint so the synthesized entry
// matches what detectDependentResources would have produced — incremental
// sync, tier routing, and discriminator dispatch all work the same.
//
// Entries without a walker pass through unchanged.
func applySpecWalkers(s *spec.APISpec, deps []DependentResource, syncable map[string]syncableMeta, types map[string]spec.TypeDef, resourceNameIndex map[string]string) []DependentResource {
	if s == nil {
		return deps
	}
	byPath := make(map[string]int, len(deps))
	for i, d := range deps {
		byPath["GET "+d.Path] = i
	}
	var walk func(name string, r spec.Resource)
	walk = func(resourceName string, r spec.Resource) {
		for endpointName, e := range r.Endpoints {
			if e.Walker == nil {
				continue
			}
			parent := strings.ToLower(strings.TrimSpace(e.Walker.Parent))
			if _, ok := syncable[parent]; !ok {
				fmt.Fprintf(os.Stderr,
					"warning: walker on %s.%s: parent %q is not a syncable resource; ignoring\n",
					resourceName, endpointName, e.Walker.Parent)
				continue
			}
			keyParam := strings.TrimSpace(e.Walker.KeyParam)
			if keyParam == "" {
				placeholders := countPathPlaceholders(e.Path)
				switch placeholders {
				case 1:
					if p, ok := firstPathParam(e.Path); ok {
						keyParam = p
					}
				case 0:
					fmt.Fprintf(os.Stderr,
						"warning: walker on %s.%s: path %q has no {placeholder}; declare key_param explicitly\n",
						resourceName, endpointName, e.Path)
					continue
				default:
					fmt.Fprintf(os.Stderr,
						"warning: walker on %s.%s: path %q has %d placeholders; declare key_param explicitly\n",
						resourceName, endpointName, e.Path, placeholders)
					continue
				}
			}
			keyField := strings.TrimSpace(e.Walker.KeyField)
			lookupKey := "GET " + e.Path
			if idx, ok := byPath[lookupKey]; ok {
				deps[idx].ParentResource = parent
				if keyParam != "" {
					deps[idx].ParentIDParam = keyParam
				}
				deps[idx].KeyField = keyField
				continue
			}
			meta := metaFromEndpoint(s, r, e, types, resourceNameIndex)
			deps = append(deps, DependentResource{
				Name:               spec.ToSnakeCase(resourceName),
				ParentResource:     parent,
				ParentIDParam:      keyParam,
				Path:               e.Path,
				Method:             meta.Method,
				Tier:               meta.Tier,
				IDField:            meta.IDField,
				Critical:           meta.Critical,
				SinceParam:         meta.SinceParam,
				SupportsPagination: meta.SupportsPagination,
				BodyFields:         meta.BodyFields,
				Discriminator:      meta.Discriminator,
				KeyField:           keyField,
			})
			byPath[lookupKey] = len(deps) - 1
		}
		for subName, sub := range r.SubResources {
			walk(subName, sub)
		}
	}
	for name, r := range s.Resources {
		walk(name, r)
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Name < deps[j].Name })
	return deps
}

// countPathPlaceholders counts the number of `{name}` substitution slots in
// a path template. Used by applySpecWalkers to decide whether
// firstPathParam's default is safe (single-placeholder path) or ambiguous
// (zero or 2+).
func countPathPlaceholders(path string) int {
	n := 0
	for i := 0; i < len(path); i++ {
		if path[i] != '{' {
			continue
		}
		j := strings.IndexByte(path[i:], '}')
		if j < 0 {
			break
		}
		n++
		i += j
	}
	return n
}

// firstPathParam returns the name of the first {param} in a path template.
func firstPathParam(path string) (string, bool) {
	start := strings.Index(path, "{")
	end := strings.Index(path, "}")
	if start < 0 || end < 0 || end <= start {
		return "", false
	}
	return path[start+1 : end], true
}

// resolveParentResource picks a parent name that matches a syncable resource.
// Prefers the spec-walk parent (correct for multi-param paths like
// /repos/{owner}/{repo}/commits) and falls back to stripping Id/_id from the
// path param. Returns "" when no candidate matches.
func resolveParentResource(walkParent, paramName string, syncable map[string]syncableMeta) string {
	if walkParent != "" {
		candidate := strings.ToLower(walkParent)
		if _, ok := syncable[candidate]; ok {
			return candidate
		}
	}
	stem := paramName
	stem = strings.TrimSuffix(stem, "_id")
	stem = strings.TrimSuffix(stem, "Id")
	stem = strings.TrimSuffix(stem, "ID")
	stem = strings.ToLower(stem)
	for _, candidate := range []string{stem, stem + "s", stem + "es"} {
		if _, ok := syncable[candidate]; ok {
			return candidate
		}
	}
	return ""
}

// syncableMeta carries the chosen list endpoint's metadata while the profiler
// is still selecting between candidates (e.g., flat vs. paginated). It is
// converted into a SyncableResource at the end of Profile().
type syncableMeta struct {
	Path               string
	Method             string
	Tier               string
	IDField            string
	Critical           bool
	SinceParam         string
	SupportsPagination bool
	BodyFields         []SyncBodyField
	Discriminator      DiscriminatorDispatch
}

type syncableCandidate struct {
	meta syncableMeta
}

// parameterizedEntry pairs a parameterized list endpoint with the parent
// resource it was discovered under during the spec walk. parentName is
// empty for top-level resources whose paths happen to be parameterized;
// detectDependentResources then falls back to the path-param heuristic.
type parameterizedEntry struct {
	name       string
	parentName string
	meta       syncableMeta
}

// metaFromEndpoint extracts the IDField and Critical fields a parser populated
// from path-item-level extensions (or, for IDField, from response-schema
// inference). Keeps the per-endpoint plumbing in one place so future profiler
// fields propagate uniformly.
func metaFromEndpoint(s *spec.APISpec, resource spec.Resource, e spec.Endpoint, types map[string]spec.TypeDef, resourceNameIndex map[string]string) syncableMeta {
	return syncableMeta{
		Path:               e.Path,
		Method:             strings.ToUpper(e.Method),
		Tier:               s.EffectiveTier(resource, e),
		IDField:            e.IDField,
		Critical:           e.Critical,
		SinceParam:         detectEndpointSinceParam(e.Params),
		SupportsPagination: endpointSupportsPagination(e),
		BodyFields:         syncBodyFieldsFromEndpoint(e),
		Discriminator:      discriminatorDispatchForEndpoint(e, types, resourceNameIndex),
	}
}

func syncBodyFieldsFromEndpoint(endpoint spec.Endpoint) []SyncBodyField {
	if !strings.EqualFold(endpoint.Method, "POST") || len(endpoint.Body) == 0 {
		return nil
	}
	fields := make([]SyncBodyField, 0, len(endpoint.Body))
	for _, param := range endpoint.Body {
		field := SyncBodyField{Name: param.Name, Type: param.Type}
		if defaultValue, ok := syncBodyDefault(param); ok {
			field.Default = defaultValue
			field.HasDefault = true
		}
		fields = append(fields, field)
	}
	return fields
}

func syncBodyDefault(param spec.Param) (any, bool) {
	if param.Default != nil {
		return param.Default, true
	}
	if len(param.Enum) == 1 {
		return param.Enum[0], true
	}
	return nil, false
}

// detectEndpointSinceParam returns the actual query parameter name this
// endpoint declares for incremental temporal filtering, or "" when none is
// declared. The match list mirrors the profile-level aggregation in
// Profile() so per-endpoint detection stays consistent with the
// PaginationProfile.SinceParam summary.
func detectEndpointSinceParam(params []spec.Param) string {
	for _, p := range params {
		name := strings.ToLower(p.Name)
		if strings.Contains(name, "since") || strings.Contains(name, "updated_after") || strings.Contains(name, "modified_since") || strings.Contains(name, "updated_at") {
			return p.Name
		}
	}
	return ""
}

func endpointSupportsPagination(endpoint spec.Endpoint) bool {
	if endpoint.Pagination != nil &&
		(strings.TrimSpace(endpoint.Pagination.LimitParam) != "" ||
			strings.TrimSpace(endpoint.Pagination.CursorParam) != "") {
		return true
	}
	for _, param := range endpoint.Params {
		if param.PathParam || param.Positional {
			continue
		}
		if pageSizeParamCandidates[strings.ToLower(param.Name)] {
			return true
		}
	}
	return false
}

func applySyncCandidates(syncable map[string]syncableMeta, candidates map[string][]syncableCandidate) {
	resourceNames := sortedKeys(candidates)
	for _, resourceName := range resourceNames {
		entries := candidates[resourceName]
		sort.SliceStable(entries, func(i, j int) bool {
			if len(entries[i].meta.Path) != len(entries[j].meta.Path) {
				return len(entries[i].meta.Path) < len(entries[j].meta.Path)
			}
			return entries[i].meta.Path < entries[j].meta.Path
		})
		if len(entries) == 0 {
			continue
		}

		if _, ok := syncable[resourceName]; !ok {
			syncable[resourceName] = entries[0].meta
		}
		canonicalPath := syncable[resourceName].Path
		for _, entry := range entries {
			if entry.meta.Path == canonicalPath {
				continue
			}
			name := siblingSyncResourceName(resourceName, entry)
			if name == "" || name == resourceName {
				continue
			}
			addSyncableIfUnique(syncable, name, entry.meta)
		}
	}
}

func siblingSyncResourceName(resourceName string, candidate syncableCandidate) string {
	suffix := siblingSyncResourceSuffix(resourceName, candidate.meta.Path)
	if len(suffix) == 0 || isGenericCollectionSegment(suffix[len(suffix)-1]) {
		return resourceName
	}
	return resourceName + "-" + strings.Join(suffix, "-")
}

func isGenericCollectionSegment(segment string) bool {
	return slices.Contains(collectionEndpointTerms, segment)
}

func siblingSyncResourceSuffix(resourceName, path string) []string {
	segments := staticPathSegments(path)
	for i, segment := range segments {
		if segment == resourceName {
			return segments[i+1:]
		}
	}
	if len(segments) == 0 {
		return nil
	}
	return segments[len(segments)-1:]
}

func staticPathSegments(path string) []string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment := strings.TrimSpace(segment)
		if segment == "" || strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			continue
		}
		if normalized := normalizeSyncResourceSegment(segment); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func normalizeSyncResourceSegment(value string) string {
	segment := strings.ReplaceAll(spec.ToSnakeCase(value), "_", "-")
	return strings.Trim(segment, "-")
}

func addSyncableIfUnique(syncable map[string]syncableMeta, name string, meta syncableMeta) {
	if existing, ok := syncable[name]; !ok || existing.Path == meta.Path {
		syncable[name] = meta
	}
}

func discriminatorDispatchForEndpoint(endpoint spec.Endpoint, types map[string]spec.TypeDef, resourceNameIndex map[string]string) DiscriminatorDispatch {
	if endpoint.Response.Discriminator != nil {
		dispatch := buildDiscriminatorDispatch(endpoint.Response.Discriminator.Field, endpoint.Response.Discriminator.Mapping, resourceNameIndex)
		if len(dispatch.Mappings) >= 2 {
			return dispatch
		}
	}

	typeDef, ok := lookupTypeDef(endpoint.Response.Item, types)
	if !ok {
		return DiscriminatorDispatch{}
	}
	for _, field := range typeDef.Fields {
		if !isDiscriminatorField(field.Name) || len(field.Enum) < 2 {
			continue
		}
		mapping := make(map[string]string, len(field.Enum))
		for _, value := range field.Enum {
			mapping[value] = value
		}
		dispatch := buildDiscriminatorDispatch(field.Name, mapping, resourceNameIndex)
		if len(dispatch.Mappings) >= 2 {
			return dispatch
		}
	}
	return DiscriminatorDispatch{}
}

func lookupTypeDef(name string, types map[string]spec.TypeDef) (spec.TypeDef, bool) {
	if name == "" || len(types) == 0 {
		return spec.TypeDef{}, false
	}
	if typeDef, ok := types[name]; ok {
		return typeDef, true
	}
	normalized := normalizeName(name)
	for typeName, typeDef := range types {
		if normalizeName(typeName) == normalized {
			return typeDef, true
		}
	}
	return spec.TypeDef{}, false
}

func isDiscriminatorField(name string) bool {
	switch strings.ToLower(strings.ReplaceAll(name, "_", "")) {
	case "type", "kind", "typename", "objecttype":
		return true
	default:
		return false
	}
}

func buildDiscriminatorDispatch(field string, rawMapping map[string]string, resourceNameIndex map[string]string) DiscriminatorDispatch {
	if strings.TrimSpace(field) == "" || len(rawMapping) == 0 || len(resourceNameIndex) == 0 {
		return DiscriminatorDispatch{}
	}
	values := make([]string, 0, len(rawMapping))
	for value := range rawMapping {
		values = append(values, value)
	}
	sort.Strings(values)

	seenResources := make(map[string]struct{})
	dispatch := DiscriminatorDispatch{Field: field}
	for _, value := range values {
		target := rawMapping[value]
		resource, ok := resourceNameForDiscriminatorTarget(target, resourceNameIndex)
		if !ok {
			resource, ok = resourceNameForDiscriminatorTarget(value, resourceNameIndex)
		}
		if !ok {
			continue
		}
		dispatch.Mappings = append(dispatch.Mappings, DiscriminatorMapping{
			Value:    value,
			Resource: resource,
		})
		seenResources[resource] = struct{}{}
	}
	if len(seenResources) < 2 {
		return DiscriminatorDispatch{}
	}
	return dispatch
}

func resourceNameForDiscriminatorTarget(target string, resourceNameIndex map[string]string) (string, bool) {
	for _, variant := range nameVariants(target) {
		if resource, ok := resourceNameIndex[variant]; ok {
			return resource, true
		}
	}
	return "", false
}

// sortedSyncableResources converts the per-resource metadata map into a sorted
// slice of SyncableResource so generated output is deterministic.
func sortedSyncableResources(m map[string]syncableMeta) []SyncableResource {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	resources := make([]SyncableResource, len(names))
	for i, name := range names {
		meta := m[name]
		resources[i] = SyncableResource{
			Name:               name,
			Path:               meta.Path,
			Method:             meta.Method,
			Tier:               meta.Tier,
			IDField:            meta.IDField,
			Critical:           meta.Critical,
			SinceParam:         meta.SinceParam,
			SupportsPagination: meta.SupportsPagination,
			BodyFields:         meta.BodyFields,
			Discriminator:      meta.Discriminator,
		}
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

package profiler

import (
	"fmt"
	"maps"
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
	CursorType      string `json:"cursor_type"`       // most common paginator class (cursor, page_token, offset, page, id_walk); drives runtime iteration strategy
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
	WireName   string
	Type       string
	Default    any
	HasDefault bool
}

func (f SyncBodyField) BodyWireName() string {
	if f.WireName != "" {
		return f.WireName
	}
	return f.Name
}

// FieldSelector describes a query param that asks sparse-response APIs to
// include a richer set of response fields during sync.
type FieldSelector struct {
	Name    string
	Default string
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
	// SkipDefaultSync keeps resources callable via --resources while excluding
	// auth-flow endpoints from generated "sync all" defaults.
	SkipDefaultSync bool
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
	// SinceParamFormat mirrors the OpenAPI format hint for SinceParam. The
	// sync template uses "date" to send YYYY-MM-DD values instead of RFC3339
	// timestamps to date-only endpoints.
	SinceParamFormat string

	// SupportsPagination is true when the chosen list endpoint declares a
	// cursor or page-size parameter. The sync template uses this to avoid
	// sending synthetic limit/offset params to strict non-paginated list
	// endpoints.
	SupportsPagination bool
	// Pagination* fields preserve the chosen list endpoint's concrete paging
	// contract so generated sync does not reuse a different resource's default.
	PaginationCursorParam string
	PaginationCursorType  string
	PaginationLimitParam  string
	PaginationPageSize    int

	// UsesHTMLResponse and HTMLExtract mirror the chosen list endpoint's
	// response_format/html_extract contract so sync can normalize HTML into
	// JSON before passing the body into the JSON page extractor.
	UsesHTMLResponse bool
	HTMLExtract      *spec.HTMLExtract

	// BodyFields names request-body fields on POST list endpoints. Sync uses
	// this to send pagination and user-supplied params in the body for
	// RPC-style list calls.
	BodyFields []SyncBodyField

	// IDWalkFilterParam names the array body field that accepts filter
	// predicates for id-walk POST query pagination.
	IDWalkFilterParam string
	IDWalkLimitParam  string
	IDWalkPageSize    int

	FieldSelector FieldSelector

	// Discriminator routes heterogeneous response items to concrete typed
	// resources before storage. Empty when the endpoint returns a homogeneous
	// resource.
	Discriminator DiscriminatorDispatch

	// QueryEntity is the SQL-query entity name (e.g. "Customer") this resource
	// reads through the shared query endpoint, set only when the API declares a
	// query_sync hint (APISpec.QuerySync) and this resource's list endpoint
	// matches it. Empty for every normal REST resource; the sync template emits
	// the query injection / envelope unwrap / offset paging only for resources
	// that carry it.
	QueryEntity string

	// ReconcileMode mirrors DependentResource.ReconcileMode for flat resources.
	// Always "none" this round — forward-looking metadata reserved for a
	// follow-up flat/tenant reconcile pass; no sync logic consumes it yet.
	ReconcileMode string
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
	PathParams     []DependentPathParam

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
	SinceParam       string
	SinceParamFormat string

	// SupportsPagination mirrors SyncableResource.SupportsPagination for child
	// paths so dependent syncs skip synthetic limit/offset params on endpoints
	// that do not declare page-size pagination.
	SupportsPagination bool
	// Pagination* mirrors SyncableResource for child sync paths.
	PaginationCursorParam string
	PaginationCursorType  string
	PaginationLimitParam  string
	PaginationPageSize    int

	// UsesHTMLResponse and HTMLExtract mirror SyncableResource for child sync
	// paths.
	UsesHTMLResponse bool
	HTMLExtract      *spec.HTMLExtract

	// BodyFields mirrors SyncableResource.BodyFields for child sync paths.
	BodyFields []SyncBodyField

	// IDWalkFilterParam mirrors SyncableResource.IDWalkFilterParam.
	IDWalkFilterParam string
	IDWalkLimitParam  string
	IDWalkPageSize    int

	FieldSelector FieldSelector

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

	// Reconciliation metadata (deletion mark-and-sweep). See sync.go.tmpl.
	ReconcileMode        string                // "per_parent" | "none"
	ParentScopeColumn    string                // e.g. "projects_id" (= ParentResource + "_id")
	GenericScopeJSONPath string                // e.g. "$.project" (json path to the parent UUID in the body)
	CascadeJunctions     []CascadeJunctionSpec // filled by the novel-junction seam, not the profiler
}

// CascadeJunctionSpec describes a junction table that must be pruned as part
// of a per-parent reconciliation cascade. Populated by the novel-junction
// registration seam (Task 4), not by the profiler itself.
type CascadeJunctionSpec struct {
	Table    string
	FKColumn string
}

type DependentPathParam struct {
	Param string
	Field string
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
	pathDerivedIDFields := make(map[string]string)
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
			if endpoint.IDFieldFromPathParam && endpoint.IDField != "" {
				key := normalizeName(resourceName)
				// Prefer the lexicographically first path-derived field so the
				// result stays stable regardless of map iteration order.
				if existing := pathDerivedIDFields[key]; existing == "" || endpoint.IDField < existing {
					pathDerivedIDFields[key] = endpoint.IDField
				}
			}

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
				pathCallable := !strings.Contains(endpoint.Path, "{") || resolvable || endpoint.Syncable
				requiredScope := hasRequiredScopeParams(endpoint)
				standaloneList := pathCallable && (!requiredScope || endpoint.Syncable)
				addStandaloneCandidate := func() {
					meta := metaFromEndpoint(s, resourceName, r, endpoint, s.Types, resourceNameIndex)
					if requiredScope && !endpoint.Syncable {
						meta.SkipDefaultSync = true
					}
					addSyncCandidate(resourceName, meta)
				}

				if endpoint.Pagination != nil {
					p.ListEndpoints++

					// Check for enum-parameterized list endpoints: when a required
					// query param has enum values, each value represents a distinct
					// entity type that should sync independently. Example:
					// GET /v1/api/networkentity?entityType=collection|workspace|api|flow
					// → sync resources: collection, workspace, api, flow
					if enumParam := findEntityTypeEnum(endpoint); standaloneList && enumParam != nil && len(enumParam.Enum) >= 2 {
						addStandaloneCandidate()
						for _, val := range enumParam.Enum {
							expandedName := strings.ToLower(val)
							expandedPath := endpoint.Path + "?" + enumParam.Name + "=" + val
							// Enum-expanded paths are more specific than generic resource
							// paths, so they always win on name collision. This ensures
							// deterministic output regardless of Go map iteration order.
							meta := metaFromEndpoint(s, resourceName, r, endpoint, s.Types, resourceNameIndex)
							meta.Path = expandedPath
							syncable[expandedName] = meta
						}
					} else if strings.Contains(endpoint.Path, "{") && !resolvable && !endpoint.Syncable && !hasRequiredDependentScopeParams(endpoint) && isPathTemplateCollection(endpoint.Path) {
						// Parameterized paginated paths can't sync standalone — track
						// them for dependent-resource detection below. Carry the
						// endpoint's metadata so x-resource-id and x-critical
						// annotations on a child path-item flow into the override
						// and critical-resource maps. Store raw names so
						// detectDependentResources can snake-case downstream.
						key := strings.ToUpper(endpoint.Method) + " " + endpoint.Path
						if _, ok := parameterized[key]; !ok {
							parameterized[key] = parameterizedEntry{
								name:       name,
								parentName: parentName,
								meta:       metaFromEndpoint(s, resourceName, r, endpoint, s.Types, resourceNameIndex),
							}
						}
					} else if standaloneList {
						addStandaloneCandidate()
					} else if pathCallable && requiredScope {
						addStandaloneCandidate()
					}
				} else if standaloneList {
					addStandaloneCandidate()
				} else if pathCallable && requiredScope {
					addStandaloneCandidate()
				}
			} else if method == "GET" && (!strings.Contains(endpoint.Path, "{") || pathParamsAllTemplateVars(endpoint.Path, s) || endpoint.Syncable) && looksLikeCollectionEndpoint(endpointNameLower) && (endpoint.Syncable || !isActionGetEndpoint(endpoint.Path)) && !isSamplerEndpoint(endpoint) && !isScalarItemArray(endpoint.Response) {
				// Catch-all for simple GET collection endpoints that isListEndpoint
				// didn't recognise (e.g., response is an untyped object with no
				// wrapper field defined in the spec's types map).
				// Only include endpoints whose name suggests a collection (list, all,
				// index, etc.) — exclude singular getters like "get" or "show".
				// Re-apply the sampler and scalar-array guards here: this branch runs
				// when isListEndpoint returned false, so without them a collection-named
				// sampler/scalar-array endpoint would be re-admitted past those gates.
				meta := metaFromEndpoint(s, resourceName, r, endpoint, s.Types, resourceNameIndex)
				if hasRequiredScopeParams(endpoint) && !endpoint.Syncable {
					meta.SkipDefaultSync = true
				}
				addSyncCandidate(resourceName, meta)
			}

			if endpoint.Pagination != nil {
				if isRuntimePagination(endpoint.Pagination) && endpoint.Pagination.CursorParam != "" {
					cursorParams[endpoint.Pagination.CursorParam]++
				}
				if isRuntimePagination(endpoint.Pagination) && endpoint.Pagination.Type != "" {
					cursorTypes[endpoint.Pagination.Type]++
				}
				if isRuntimePagination(endpoint.Pagination) && endpoint.Pagination.LimitParam != "" {
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
			if sinceParam, _ := detectEndpointSinceParamAndFormat(endpoint, s.Types); sinceParam != "" {
				sinceParams[sinceParam]++
			}
			for _, param := range endpoint.Params {
				name := strings.ToLower(param.Name)
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
	applyPathDerivedIDFields(syncable, pathDerivedIDFields, s.Types)
	applyPathDerivedIDFieldsToParameterized(parameterized, pathDerivedIDFields, s.Types)

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

	p.DependentSyncResources = detectDependentResources(parameterized, syncable, shardedSubResources)
	p.DependentSyncResources = applySpecWalkers(s, p.DependentSyncResources, syncable, s.Types, resourceNameIndex)
	addUnresolvedPathTemplateCollections(syncable, parameterized, p.DependentSyncResources)
	p.SyncableResources = sortedSyncableResources(syncable)
	// Flat resources carry "none" this round — forward-looking metadata for the
	// follow-up flat/tenant reconcile. Set explicitly so the value is "none",
	// not the empty zero value.
	for i := range p.SyncableResources {
		p.SyncableResources[i].ReconcileMode = "none"
	}
	// Populate reconcile metadata for each dependent resource.
	// per_parent is safe only for a single-path-param dependent with a PK.
	for i := range p.DependentSyncResources {
		dep := &p.DependentSyncResources[i]
		if len(dep.PathParams) == 1 && dep.IDField != "" {
			dep.ReconcileMode = "per_parent"
			dep.ParentScopeColumn = dep.ParentResource + "_id"
			dep.GenericScopeJSONPath = "$." + singularParentField(dep.ParentResource)
		} else {
			dep.ReconcileMode = "none"
		}
	}
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
			NeedLevel:          lowHigh(p.HighVolume || p.OfflineValuable || p.hasSyncableStoreResources()),
			Decision:           "local store",
			Rationale:          "Read-heavy, high-volume, or profiler-confirmed syncable APIs benefit from local persistence for repeat access and offline workflows.",
			ImplementationHint: "Use SQLite-backed storage and cache frequently accessed resources.",
		},
		vision.ArchitectureDecision{
			Area:               "search",
			NeedLevel:          lowHigh(p.NeedsSearch || p.hasSyncableStoreResources()),
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
	hasSyncableStoreResources := p.hasSyncableStoreResources()
	if p.HighVolume || hasSyncableStoreResources {
		features = append(features, "sync")
	}
	if p.NeedsSearch || hasSyncableStoreResources {
		features = append(features, "search")
	}
	if p.HighVolume || p.NeedsSearch || p.HasDependencies || hasSyncableStoreResources {
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

func (p *APIProfile) hasSyncableStoreResources() bool {
	if p == nil {
		return false
	}
	return len(p.SyncableResources) > 0 || len(p.DependentSyncResources) > 0
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
		"perpage": true, "first": true, "count": true, "max_results": true,
		"maxrecords": true, "max_records": true, "page[size]": true,
	}
	cursorParamCandidates = map[string]bool{
		"after": true, "cursor": true, "page_token": true, "offset": true, "skip": true,
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
	return hasRequiredScopeParamsForSync(endpoint, true)
}

// hasRequiredDependentScopeParams flags parameterized child endpoints that
// require a caller-supplied query filter that dependent sync cannot satisfy.
// Unlike hasRequiredScopeParams, enum params are not exempt here because
// dependent sync has no per-parent enum-expansion path.
func hasRequiredDependentScopeParams(endpoint spec.Endpoint) bool {
	return hasRequiredScopeParamsForSync(endpoint, false)
}

func hasRequiredScopeParamsForSync(endpoint spec.Endpoint, allowEnumExpansion bool) bool {
	temporalOrFormatParams := map[string]bool{
		"since": true, "updated_after": true, "modified_since": true, "since_id": true,
		"from": true, "to": true, "start_date": true, "end_date": true,
		"start_datetime": true, "end_datetime": true, "start_time": true, "end_time": true,
		"from_date": true, "to_date": true, "from_datetime": true, "to_datetime": true,
		"key": true, "format": true,
	}
	for _, param := range endpoint.Params {
		if param.Required && !param.Positional && !param.PathParam {
			if param.GlobalScope && strings.EqualFold(param.Type, "string") {
				continue
			}
			lower := strings.ToLower(param.Name)
			if pageSizeParamCandidates[lower] || cursorParamCandidates[lower] || temporalOrFormatParams[lower] {
				continue
			}
			// Enum params with 2+ values are handled by enum expansion, not scope
			// for flat resources. Dependent sync has no per-parent enum expansion
			// path, so required enum filters are still unsatisfied there.
			if allowEnumExpansion && len(param.Enum) >= 2 {
				continue
			}
			return true
		}
	}
	return false
}

// samplerPathSegments mark endpoints that return a non-deterministic sample
// rather than a stable, ordered collection. Each call yields a fresh full set,
// so page-based pagination never reaches a natural end and a sync loop runs
// forever. They are excluded from syncable list selection.
var samplerPathSegments = []string{"random", "shuffle", "sample"}

// isSamplerEndpoint reports whether the endpoint path marks a non-deterministic
// sampler (e.g. /assets/random) that must not be treated as a paginated list.
func isSamplerEndpoint(endpoint spec.Endpoint) bool {
	for _, segment := range staticPathSegments(endpoint.Path) {
		if slices.Contains(samplerPathSegments, strings.ToLower(segment)) {
			return true
		}
	}
	return false
}

func isListEndpoint(name string, endpoint spec.Endpoint, types map[string]spec.TypeDef) bool {
	method := strings.ToUpper(endpoint.Method)

	// A sampler endpoint returns a fresh random page every call; paginating it
	// never terminates. Exclude it regardless of response shape or pagination.
	if isSamplerEndpoint(endpoint) {
		return false
	}

	// An array of scalars has no extractable primary key, so it can never
	// populate the store. Exclude it even when paginated, since the
	// Pagination short-circuit below would otherwise admit it.
	if isScalarItemArray(endpoint.Response) {
		return false
	}

	if method == "POST" {
		return endpoint.Pagination != nil &&
			looksLikeCollectionEndpoint(strings.ToLower(name)) &&
			hasListShapedResponse(name, endpoint, types)
	}

	if method != "GET" {
		return false
	}
	if !endpoint.Syncable && isActionGetEndpoint(endpoint.Path) {
		return false
	}
	if endpoint.Pagination != nil {
		return true
	}
	if hasListShapedResponse(name, endpoint, types) {
		return true
	}

	return looksLikeBasicGetListEndpoint(strings.ToLower(name))
}

var nonListActionSegments = map[string]bool{
	"events": true,
	"find":   true,
	"lookup": true,
	"search": true,
}

func isActionGetEndpoint(path string) bool {
	segments := staticPathSegments(path)
	segments = trimVersionPathSegments(segments)
	if len(segments) < 2 {
		return false
	}
	last := actionSegmentBase(segments[len(segments)-1])
	return nonListActionSegments[last]
}

func trimVersionPathSegments(segments []string) []string {
	for len(segments) > 0 && isVersionPathSegment(segments[0]) {
		segments = segments[1:]
	}
	return segments
}

func isVersionPathSegment(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, r := range segment[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func actionSegmentBase(segment string) string {
	for _, suffix := range []string{"-json", "_json", ".json"} {
		segment = strings.TrimSuffix(segment, suffix)
	}
	return segment
}

// scalarItemTypes are the response-array element type names the parser emits
// for primitive (non-object) items via schemaTypeName. An array of these has no
// extractable primary key, so syncing it stores zero rows
// (all_items_failed_id_extraction). The empty string is excluded: an unset Item
// means an object array whose type was not registered, which still syncs.
var scalarItemTypes = map[string]bool{
	"string": true,
	"int":    true,
	"bool":   true,
	"float":  true,
}

// isScalarItemArray reports whether the response is an array whose declared
// element type is a primitive. Such arrays carry no object IDs and must not be
// selected as syncable list resources.
func isScalarItemArray(response spec.ResponseDef) bool {
	return response.Type == "array" && scalarItemTypes[response.Item]
}

func hasListShapedResponse(name string, endpoint spec.Endpoint, types map[string]spec.TypeDef) bool {
	if endpoint.Response.Type == "array" {
		// Scalar-element arrays are rejected upstream in isListEndpoint; a
		// bare object array is list-shaped.
		return true
	}

	// Check for wrapper-object responses: the endpoint returns type "object"
	// and the referenced type has a field that clearly carries the list items.
	return endpoint.Response.Type == "object" &&
		endpoint.Response.Item != "" &&
		hasWrapperArrayField(endpoint.Response.Item, types, name, endpoint.Path)
}

// Multi-array envelopes need a curated tie-breaker; single-array envelopes are
// already unambiguous and can use any resource-shaped key.
var wrapperArrayKeys = map[string]bool{
	"data":     true,
	"results":  true,
	"items":    true,
	"events":   true,
	"entries":  true,
	"features": true,
	"records":  true,
	"nodes":    true,
}

var ancillaryArrayKeys = map[string]bool{
	"errors":            true,
	"warnings":          true,
	"validations":       true,
	"validation_errors": true,
}

// Field metadata is stronger than type-name guesses: once a type is present,
// its fields decide whether the response is extractable.
func hasWrapperArrayField(typeName string, types map[string]spec.TypeDef, endpointName string, path string) bool {
	if typeDef, ok := types[typeName]; ok {
		arrayFields := 0
		var arrayField string
		for _, field := range typeDef.Fields {
			if !strings.EqualFold(field.Type, "array") {
				continue
			}
			fieldKey := normalizedFieldKey(field.Name)
			if wrapperArrayKeys[fieldKey] {
				return true
			}
			if ancillaryArrayKeys[fieldKey] {
				continue
			}
			arrayFields++
			arrayField = field.Name
		}
		if arrayFields == 1 && singleArrayFieldMatchesCollection(arrayField, endpointName, path) {
			return true
		}
		return false
	}

	// Fallback: if the type name itself suggests a list wrapper, treat it
	// as a wrapper only when the types map lacks that type definition.
	nameUpper := strings.ToUpper(typeName)
	return strings.Contains(nameUpper, "RESPONSE") ||
		strings.Contains(nameUpper, "LIST") ||
		strings.Contains(nameUpper, "RESULT") ||
		strings.Contains(nameUpper, "COLLECTION")
}

func singleArrayFieldMatchesCollection(fieldName string, endpointName string, path string) bool {
	if namesOverlap(fieldName, endpointName) {
		return true
	}
	for _, segment := range staticPathSegments(path) {
		if namesOverlap(fieldName, segment) {
			return true
		}
	}
	return false
}

func namesOverlap(a, b string) bool {
	aVariants := nameVariants(a)
	bVariants := nameVariants(b)
	for _, av := range aVariants {
		if slices.Contains(bVariants, av) {
			return true
		}
	}
	bTokens := nameTokens(b)
	for _, av := range aVariants {
		if slices.Contains(bTokens, av) {
			return true
		}
	}
	aTokens := nameTokens(a)
	for _, bv := range bVariants {
		if slices.Contains(aTokens, bv) {
			return true
		}
	}
	return false
}

func normalizedFieldKey(name string) string {
	return normalizeName(spec.ToSnakeCase(name))
}

func nameTokens(name string) []string {
	normalized := normalizeName(spec.ToSnakeCase(name))
	if normalized == "" {
		return nil
	}
	var tokens []string
	for token := range strings.SplitSeq(normalized, "_") {
		if token != "" {
			tokens = append(tokens, nameVariants(token)...)
		}
	}
	return tokens
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
// becomes a dependent resource of "channels", and deeper children can depend
// on already-detected dependent resources. When the same leaf appears under
// multiple parents (or collides with a top-level resource), each parent emits
// a sharded Name so its shard syncs to its own table.
func detectDependentResources(parameterized map[string]parameterizedEntry, syncable map[string]syncableMeta, shardedSubResources spec.SubResourceShards) []DependentResource {
	var deps []DependentResource
	knownParents := make(map[string]bool, len(syncable)+len(parameterized))
	for resource := range syncable {
		knownParents[resource] = true
	}
	depthByResource := map[string]int{}

	keys := sortedKeys(parameterized)
	for len(keys) > 0 {
		var next []string
		progressed := false
		for _, key := range keys {
			entry := parameterized[key]
			dep, ok := dependentResourceFromEntry(entry, knownParents, syncable, shardedSubResources)
			if !ok {
				next = append(next, key)
				continue
			}
			deps = append(deps, dep)
			knownParents[dep.Name] = true
			depthByResource[dep.Name] = depthByResource[dep.ParentResource] + 1
			if dep.Name == spec.ToSnakeCase(entry.name) {
				knownParents[spec.ToSnakeCase(entry.name)] = true
			}
			progressed = true
		}
		if !progressed {
			break
		}
		keys = next
	}
	sortDependentResources(deps, depthByResource)
	return deps
}

func addUnresolvedPathTemplateCollections(syncable map[string]syncableMeta, parameterized map[string]parameterizedEntry, deps []DependentResource) {
	dependentPaths := make(map[string]struct{}, len(deps))
	for _, dep := range deps {
		dependentPaths[dep.Path] = struct{}{}
	}
	for _, key := range sortedKeys(parameterized) {
		entry := parameterized[key]
		if _, ok := dependentPaths[entry.meta.Path]; ok {
			continue
		}
		if !isPathTemplateCollection(entry.meta.Path) {
			continue
		}
		meta := entry.meta
		meta.SkipDefaultSync = true
		addSyncableIfUnique(syncable, strings.ToLower(entry.name), meta)
	}
}

func sortDependentResources(deps []DependentResource, knownDepths map[string]int) {
	depthByResource := make(map[string]int, len(knownDepths)+len(deps))
	maps.Copy(depthByResource, knownDepths)
	byName := make(map[string]DependentResource, len(deps))
	for _, dep := range deps {
		byName[dep.Name] = dep
	}
	var depthOf func(string, map[string]bool) int
	depthOf = func(name string, visiting map[string]bool) int {
		if depth, ok := depthByResource[name]; ok {
			return depth
		}
		if visiting[name] {
			return 1
		}
		dep, ok := byName[name]
		if !ok {
			return 0
		}
		visiting[name] = true
		depth := depthOf(dep.ParentResource, visiting) + 1
		delete(visiting, name)
		depthByResource[name] = depth
		return depth
	}
	for _, dep := range deps {
		depthOf(dep.Name, map[string]bool{})
	}
	sort.Slice(deps, func(i, j int) bool {
		if depthByResource[deps[i].Name] != depthByResource[deps[j].Name] {
			return depthByResource[deps[i].Name] < depthByResource[deps[j].Name]
		}
		return deps[i].Name < deps[j].Name
	})
}

func dependentResourceFromEntry(entry parameterizedEntry, knownParents map[string]bool, syncable map[string]syncableMeta, shardedSubResources spec.SubResourceShards) (DependentResource, bool) {
	ctx, ok := dependentPathContext(entry, knownParents, shardedSubResources)
	if !ok {
		return DependentResource{}, false
	}
	keyField := parentIDFieldForDependent(ctx.parentResource, syncable)

	return DependentResource{
		Name:                  ctx.name,
		ParentResource:        ctx.parentResource,
		ParentIDParam:         dependentParentIDParam(entry.meta.Path, ctx.parentPathSegment, ctx.firstParam),
		Path:                  entry.meta.Path,
		Method:                entry.meta.Method,
		Tier:                  entry.meta.Tier,
		PathParams:            dependentPathParams(entry.meta.Path, ctx.parentPathSegment, ctx.firstParam, keyField),
		IDField:               entry.meta.IDField,
		Critical:              entry.meta.Critical,
		SinceParam:            entry.meta.SinceParam,
		SinceParamFormat:      entry.meta.SinceParamFormat,
		SupportsPagination:    entry.meta.SupportsPagination,
		PaginationCursorParam: entry.meta.PaginationCursorParam,
		PaginationCursorType:  entry.meta.PaginationCursorType,
		PaginationLimitParam:  entry.meta.PaginationLimitParam,
		PaginationPageSize:    entry.meta.PaginationPageSize,
		UsesHTMLResponse:      entry.meta.UsesHTMLResponse,
		HTMLExtract:           entry.meta.HTMLExtract,
		BodyFields:            entry.meta.BodyFields,
		IDWalkFilterParam:     entry.meta.IDWalkFilterParam,
		IDWalkLimitParam:      entry.meta.IDWalkLimitParam,
		IDWalkPageSize:        entry.meta.IDWalkPageSize,
		FieldSelector:         entry.meta.FieldSelector,
		Discriminator:         entry.meta.Discriminator,
	}, true
}

func parentIDFieldForDependent(parentResource string, syncable map[string]syncableMeta) string {
	if syncable == nil {
		return ""
	}
	if meta, ok := syncable[parentResource]; ok {
		return strings.TrimSpace(meta.IDField)
	}
	return ""
}

type dependentContext struct {
	name              string
	parentResource    string
	parentPathSegment string
	firstParam        string
}

func dependentPathContext(entry parameterizedEntry, knownParents map[string]bool, shardedSubResources spec.SubResourceShards) (dependentContext, bool) {
	firstParam, ok := firstPathParam(entry.meta.Path)
	if !ok {
		return dependentContext{}, false
	}

	segments := pathSegments(entry.meta.Path)
	placeholderCount := len(orderedPathPlaceholders(entry.meta.Path))
	parentSegment := spec.ToSnakeCase(entry.parentName)
	childName := spec.ToSnakeCase(entry.name)
	forceShard := false
	if placeholderCount >= 2 {
		if childSegment, parent, ok := pathCollectionContext(segments); ok {
			childName = childSegment
			parentSegment = parent
			forceShard = true
		}
	}
	if parentSegment == "" {
		parentSegment = spec.ToSnakeCase(entry.parentName)
	}

	parentResource := resolvePathParentResource(parentSegment, segments, knownParents, shardedSubResources)
	if parentResource == "" {
		parentResource = resolveParentResourceName(entry.parentName, firstParam, knownParents)
	}
	if parentResource == "" {
		return dependentContext{}, false
	}

	name := childName
	if forceShard {
		name = spec.ShardedSubResourceTableName(parentResource, childName)
	} else if shardedSubResources.IsSharded(childName) {
		shardParent := parentResource
		if entry.parentName != "" {
			shardParent = entry.parentName
		}
		name = spec.ShardedSubResourceTableName(shardParent, childName)
	}
	if parentSegment == "" {
		parentSegment = parentResource
	}

	return dependentContext{
		name:              name,
		parentResource:    parentResource,
		parentPathSegment: parentSegment,
		firstParam:        firstParam,
	}, true
}

func pathCollectionContext(segments []string) (child, parent string, ok bool) {
	lastPlaceholder := -1
	for i, segment := range segments {
		if isPathPlaceholder(segment) {
			lastPlaceholder = i
		}
	}
	if lastPlaceholder < 0 {
		return "", "", false
	}
	childIndex := nextStaticSegmentIndex(segments, lastPlaceholder+1)
	parentIndex := previousStaticSegmentIndex(segments, lastPlaceholder-1)
	if childIndex < 0 || parentIndex < 0 {
		return "", "", false
	}
	return spec.ToSnakeCase(segments[childIndex]), spec.ToSnakeCase(segments[parentIndex]), true
}

func resolvePathParentResource(parentSegment string, segments []string, knownParents map[string]bool, shardedSubResources spec.SubResourceShards) string {
	if parentSegment == "" {
		return ""
	}
	if knownParents[parentSegment] {
		return parentSegment
	}
	parentIndex := lastStaticSegmentIndex(segments, parentSegment)
	if parentIndex < 0 {
		return ""
	}
	ancestorIndex := previousStaticSegmentIndex(segments, parentIndex-1)
	if ancestorIndex >= 0 {
		candidate := spec.ShardedSubResourceTableName(segments[ancestorIndex], parentSegment)
		if knownParents[candidate] {
			return candidate
		}
	}
	if shardedSubResources.IsSharded(parentSegment) && ancestorIndex >= 0 {
		candidate := spec.ShardedSubResourceTableName(segments[ancestorIndex], parentSegment)
		if knownParents[candidate] {
			return candidate
		}
	}
	return ""
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
			if keyField == "" {
				keyField = parentIDFieldForDependent(parent, syncable)
			}
			lookupKey := "GET " + e.Path
			if idx, ok := byPath[lookupKey]; ok {
				deps[idx].ParentResource = parent
				if keyParam != "" {
					deps[idx].ParentIDParam = keyParam
				}
				deps[idx].KeyField = keyField
				deps[idx].PathParams = dependentPathParams(e.Path, parent, deps[idx].ParentIDParam, keyField)
				continue
			}
			meta := metaFromEndpoint(s, resourceName, r, e, types, resourceNameIndex)
			deps = append(deps, DependentResource{
				Name:                  spec.ToSnakeCase(resourceName),
				ParentResource:        parent,
				ParentIDParam:         keyParam,
				Path:                  e.Path,
				Method:                meta.Method,
				Tier:                  meta.Tier,
				PathParams:            dependentPathParams(e.Path, parent, keyParam, keyField),
				IDField:               meta.IDField,
				Critical:              meta.Critical,
				SinceParam:            meta.SinceParam,
				SinceParamFormat:      meta.SinceParamFormat,
				SupportsPagination:    meta.SupportsPagination,
				PaginationCursorParam: meta.PaginationCursorParam,
				PaginationCursorType:  meta.PaginationCursorType,
				PaginationLimitParam:  meta.PaginationLimitParam,
				PaginationPageSize:    meta.PaginationPageSize,
				UsesHTMLResponse:      meta.UsesHTMLResponse,
				HTMLExtract:           meta.HTMLExtract,
				BodyFields:            meta.BodyFields,
				IDWalkFilterParam:     meta.IDWalkFilterParam,
				IDWalkLimitParam:      meta.IDWalkLimitParam,
				IDWalkPageSize:        meta.IDWalkPageSize,
				FieldSelector:         meta.FieldSelector,
				Discriminator:         meta.Discriminator,
				KeyField:              keyField,
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
	sortDependentResources(deps, nil)
	return deps
}

func dependentPathParams(path, parentResource, keyParam, keyField string) []DependentPathParam {
	placeholders := orderedPathPlaceholders(path)
	if len(placeholders) == 0 {
		return nil
	}

	fields := dependentPathParamFields(path, parentResource)
	params := make([]DependentPathParam, 0, len(placeholders))
	for _, placeholder := range placeholders {
		field := fields[placeholder]
		if placeholder == keyParam && keyField != "" {
			field = spec.ToSnakeCase(keyField)
		}
		if field == "" {
			field = spec.ToSnakeCase(placeholder)
		}
		params = append(params, DependentPathParam{
			Param: placeholder,
			Field: field,
		})
	}
	return params
}

func dependentParentIDParam(path, parentResource, fallback string) string {
	for _, pathParam := range dependentPathParams(path, parentResource, fallback, "") {
		if pathParam.Field == "id" {
			return pathParam.Param
		}
	}
	return fallback
}

// parentFieldIrregulars maps plural parent resource names whose singular form a
// naive TrimSuffix("s") would mangle to the correct singular field. The "-ies →
// -y" class is handled by rule below; this table is for the residual irregulars.
var parentFieldIrregulars = map[string]string{
	"statuses":  "status",
	"addresses": "address",
	"buses":     "bus",
	"classes":   "class",
	"indexes":   "index",
	"indices":   "index",
	"matrices":  "matrix",
	"people":    "person",
}

// singularParentField maps a plural parent resource name to the singular field
// the API body carries for that parent (e.g. "projects" → "project"). Mirrors
// the childScopeColumnSources convention used by deriveScopeColumns in the store.
// A bare TrimSuffix("s") mangles irregular plurals ("categories" → "categorie"),
// which would silently break reconciliation (json_extract on a wrong path returns
// NULL for every row, so ReconcilePartition sweeps nothing). Guard the common
// English classes: an explicit irregulars table, then the "-ies → -y" rule, then
// the regular "-s" trim. Genuinely irregular forms outside the table still fall
// through to TrimSuffix, so a future spec with an exotic plural should add it here.
func singularParentField(parentResource string) string {
	if s, ok := parentFieldIrregulars[parentResource]; ok {
		return s
	}
	// "categories" → "category", "activities" → "activity". Guard length so a
	// 3-letter "-ies" word (none in practice) can't underflow to "".
	if strings.HasSuffix(parentResource, "ies") && len(parentResource) > 4 {
		return strings.TrimSuffix(parentResource, "ies") + "y"
	}
	return strings.TrimSuffix(parentResource, "s")
}

func dependentPathParamFields(path, parentResource string) map[string]string {
	fields := map[string]string{}
	segments := pathSegments(path)
	parentSegmentIndex := -1
	childSegmentIndex := -1
	normalizedParent := spec.ToSnakeCase(parentResource)
	for i, segment := range segments {
		if isPathPlaceholder(segment) {
			continue
		}
		if spec.ToSnakeCase(segment) == normalizedParent {
			parentSegmentIndex = i
			childSegmentIndex = nextStaticSegmentIndex(segments, i+1)
		}
	}

	parentIdentityParams := map[string]bool{}
	if parentSegmentIndex >= 0 {
		for i := parentSegmentIndex + 1; i < len(segments); i++ {
			if i == childSegmentIndex {
				break
			}
			if isPathPlaceholder(segments[i]) {
				parentIdentityParams[strings.TrimSuffix(strings.TrimPrefix(segments[i], "{"), "}")] = true
			}
		}
	}
	parentUsesCompositeIdentity := len(parentIdentityParams) > 1

	lastStatic := ""
	for _, segment := range segments {
		if isPathPlaceholder(segment) {
			param := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
			switch {
			case parentIdentityParams[param] && !parentUsesCompositeIdentity:
				fields[param] = dependentIdentityField(param)
			case parentIdentityParams[param] && parentUsesCompositeIdentity:
				fields[param] = spec.ToSnakeCase(param)
			case lastStatic != "":
				fields[param] = spec.ToSnakeCase(lastStatic) + "_id"
			default:
				fields[param] = spec.ToSnakeCase(param)
			}
			continue
		}
		lastStatic = segment
	}
	return fields
}

func dependentIdentityField(param string) string {
	field := spec.ToSnakeCase(param)
	switch {
	case field == "id" || strings.HasSuffix(field, "_id") || strings.HasSuffix(param, "Id") || strings.HasSuffix(param, "ID"):
		return "id"
	case strings.HasSuffix(field, "_slug"):
		return "slug"
	case strings.HasSuffix(field, "_name"):
		return "name"
	case strings.HasSuffix(field, "_key"):
		return "key"
	default:
		return field
	}
}

func orderedPathPlaceholders(path string) []string {
	var params []string
	seen := map[string]bool{}
	for i := 0; i < len(path); i++ {
		if path[i] != '{' {
			continue
		}
		j := strings.IndexByte(path[i:], '}')
		if j < 0 {
			break
		}
		param := path[i+1 : i+j]
		if param != "" && !seen[param] {
			params = append(params, param)
			seen[param] = true
		}
		i += j
	}
	return params
}

func isPathPlaceholder(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") && len(segment) > 2
}

func pathSegments(path string) []string {
	if strings.Trim(path, "/") == "" {
		return nil
	}
	return strings.Split(strings.Trim(path, "/"), "/")
}

func isPathTemplateCollection(path string) bool {
	segments := pathSegments(path)
	if len(segments) == 0 || !strings.Contains(path, "{") {
		return false
	}
	return !isPathPlaceholder(segments[len(segments)-1])
}

func nextStaticSegmentIndex(segments []string, start int) int {
	for i := start; i < len(segments); i++ {
		if !isPathPlaceholder(segments[i]) {
			return i
		}
	}
	return -1
}

func previousStaticSegmentIndex(segments []string, start int) int {
	for i := min(start, len(segments)-1); i >= 0; i-- {
		if !isPathPlaceholder(segments[i]) {
			return i
		}
	}
	return -1
}

func lastStaticSegmentIndex(segments []string, normalized string) int {
	for i, segment := range slices.Backward(segments) {
		if isPathPlaceholder(segment) {
			continue
		}
		if spec.ToSnakeCase(segment) == normalized {
			return i
		}
	}
	return -1
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

// resolveParentResourceName picks a parent name that matches a known flat or
// already-detected dependent resource.
// Prefers the spec-walk parent (correct for multi-param paths like
// /repos/{owner}/{repo}/commits) and falls back to stripping Id/_id from the
// path param. Returns "" when no candidate matches.
func resolveParentResourceName(walkParent, paramName string, knownParents map[string]bool) string {
	if walkParent != "" {
		candidate := strings.ToLower(walkParent)
		if knownParents[candidate] {
			return candidate
		}
	}
	stem := paramName
	stem = strings.TrimSuffix(stem, "_id")
	stem = strings.TrimSuffix(stem, "Id")
	stem = strings.TrimSuffix(stem, "ID")
	stem = strings.ToLower(stem)
	for _, candidate := range []string{stem, stem + "s", stem + "es"} {
		if knownParents[candidate] {
			return candidate
		}
	}
	return ""
}

// syncableMeta carries the chosen list endpoint's metadata while the profiler
// is still selecting between candidates (e.g., flat vs. paginated). It is
// converted into a SyncableResource at the end of Profile().
type syncableMeta struct {
	Path                  string
	Method                string
	Tier                  string
	SkipDefaultSync       bool
	IDField               string
	Critical              bool
	SinceParam            string
	SinceParamFormat      string
	SupportsPagination    bool
	PaginationCursorParam string
	PaginationCursorType  string
	PaginationLimitParam  string
	PaginationPageSize    int
	UsesHTMLResponse      bool
	HTMLExtract           *spec.HTMLExtract
	BodyFields            []SyncBodyField
	IDWalkFilterParam     string
	IDWalkLimitParam      string
	IDWalkPageSize        int
	FieldSelector         FieldSelector
	Discriminator         DiscriminatorDispatch
	ResponseItem          string
	QueryEntity           string
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
func metaFromEndpoint(s *spec.APISpec, resourceName string, resource spec.Resource, e spec.Endpoint, types map[string]spec.TypeDef, resourceNameIndex map[string]string) syncableMeta {
	idWalkFilterParam, idWalkLimitParam, idWalkPageSize := detectIDWalkParams(e)
	sinceParam, sinceParamFormat := detectEndpointSinceParamAndFormat(e, types)
	paginationCursorParam, paginationCursorType, paginationLimitParam, paginationPageSize := syncPaginationDefaultsFromEndpoint(e)
	return syncableMeta{
		Path:                  e.Path,
		Method:                strings.ToUpper(e.Method),
		Tier:                  s.EffectiveTier(resource, e),
		SkipDefaultSync:       isAuthTaggedEndpoint(e) || hasTypedResponseWithoutRuntimeID(resourceName, e, types),
		IDField:               e.IDField,
		Critical:              e.Critical,
		SinceParam:            sinceParam,
		SinceParamFormat:      sinceParamFormat,
		SupportsPagination:    endpointSupportsPagination(e),
		PaginationCursorParam: paginationCursorParam,
		PaginationCursorType:  paginationCursorType,
		PaginationLimitParam:  paginationLimitParam,
		PaginationPageSize:    paginationPageSize,
		UsesHTMLResponse:      e.UsesHTMLResponse(),
		HTMLExtract:           e.HTMLExtract,
		BodyFields:            syncBodyFieldsFromEndpoint(e),
		IDWalkFilterParam:     idWalkFilterParam,
		IDWalkLimitParam:      idWalkLimitParam,
		IDWalkPageSize:        idWalkPageSize,
		FieldSelector:         detectEndpointFieldSelector(e),
		Discriminator:         discriminatorDispatchForEndpoint(e, types, resourceNameIndex),
		ResponseItem:          e.Response.Item,
		QueryEntity:           queryEntityForEndpoint(s, e),
	}
}

// queryEntityForEndpoint returns the SQL-query entity name for a list endpoint
// when the API declares a query_sync hint and this endpoint reads through the
// shared query path with an entity-named envelope. The entity is the
// Response.Item type (e.g. "Customer"), falling back to the ResponsePath leaf
// (e.g. "QueryResponse.Customer" -> "Customer"). Returns "" for non-query
// endpoints and for raw passthrough resources on the same path that declare no
// ResponsePath, so they never get a bogus query injection.
func queryEntityForEndpoint(s *spec.APISpec, e spec.Endpoint) string {
	if s == nil || s.QuerySync == nil || e.Path != s.QuerySync.Path || e.ResponsePath == "" {
		return ""
	}
	if e.Response.Item != "" {
		return e.Response.Item
	}
	if i := strings.LastIndex(e.ResponsePath, "."); i >= 0 {
		return e.ResponsePath[i+1:]
	}
	return ""
}

func hasTypedResponseWithoutRuntimeID(resourceName string, endpoint spec.Endpoint, types map[string]spec.TypeDef) bool {
	if endpoint.IDField != "" || endpoint.Response.Item == "" {
		return false
	}
	typeDef, ok := lookupTypeDef(endpoint.Response.Item, types)
	if !ok {
		return false
	}
	return !typeDefHasRuntimeIDField(resourceName, typeDef)
}

func typeDefHasRuntimeIDField(resourceName string, typeDef spec.TypeDef) bool {
	fieldNames := make(map[string]struct{}, len(typeDef.Fields))
	for _, field := range typeDef.Fields {
		fieldNames[normalizeName(spec.ToSnakeCase(field.Name))] = struct{}{}
	}
	for _, key := range []string{"id", "gid", "sid", "uid", "uuid", "guid", "slug", "key", "code"} {
		if _, ok := fieldNames[key]; ok {
			return true
		}
	}
	for _, base := range resourceIDBaseNames(resourceName) {
		for _, suffix := range []string{"_id", "_code", "_key", "_slug"} {
			if _, ok := fieldNames[base+suffix]; ok {
				return true
			}
		}
	}
	return false
}

func resourceIDBaseNames(resourceName string) []string {
	normalized := normalizeName(spec.ToSnakeCase(resourceName))
	if normalized == "" {
		return nil
	}
	bases := []string{normalized}
	if stripped, ok := strings.CutPrefix(normalized, "get_"); ok && stripped != "" {
		bases = append(bases, stripped)
	}

	out := make([]string, 0, len(bases)*2)
	seen := map[string]struct{}{}
	add := func(base string) {
		if base == "" {
			return
		}
		if _, ok := seen[base]; ok {
			return
		}
		seen[base] = struct{}{}
		out = append(out, base)
	}
	for _, base := range bases {
		add(base)
		add(singularizeResourceIDBase(base))
	}
	return out
}

func singularizeResourceIDBase(base string) string {
	if base == "" {
		return ""
	}
	idx := strings.LastIndex(base, "_")
	prefix, last := "", base
	if idx >= 0 {
		prefix, last = base[:idx+1], base[idx+1:]
	}
	irregulars := map[string]string{
		"properties": "property",
		"companies":  "company",
		"categories": "category",
		"entries":    "entry",
		"statuses":   "status",
		"addresses":  "address",
		"analyses":   "analysis",
		"movies":     "movie",
		"series":     "series",
		"matrices":   "matrix",
		"indices":    "index",
		"vertices":   "vertex",
	}
	if singular, ok := irregulars[last]; ok {
		return prefix + singular
	}
	switch {
	case strings.HasSuffix(last, "ies") && len(last) > 3:
		return prefix + last[:len(last)-3] + "y"
	case strings.HasSuffix(last, "ses"), strings.HasSuffix(last, "xes"), strings.HasSuffix(last, "zes"):
		return prefix + last[:len(last)-2]
	case strings.HasSuffix(last, "s") && !strings.HasSuffix(last, "ss") && len(last) > 1:
		return prefix + last[:len(last)-1]
	default:
		return base
	}
}

func isAuthTaggedEndpoint(endpoint spec.Endpoint) bool {
	for _, tag := range endpoint.Tags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "auth", "authentication", "authorization", "oauth", "oauth2":
			return true
		}
	}
	return false
}

func syncBodyFieldsFromEndpoint(endpoint spec.Endpoint) []SyncBodyField {
	if !strings.EqualFold(endpoint.Method, "POST") || len(endpoint.Body) == 0 {
		return nil
	}
	fields := make([]SyncBodyField, 0, len(endpoint.Body))
	for _, param := range endpoint.Body {
		field := SyncBodyField{Name: param.Name, WireName: param.BodyWireName(), Type: param.Type}
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

func detectIDWalkParams(endpoint spec.Endpoint) (string, string, int) {
	if endpoint.Pagination == nil || endpoint.Pagination.Type != spec.PaginationTypeIDWalk || strings.TrimSpace(endpoint.IDField) == "" {
		return "", "", 0
	}
	limitParam := strings.ToLower(strings.TrimSpace(endpoint.Pagination.LimitParam))
	if limitParam == "" {
		return "", "", 0
	}
	var hasLimit bool
	var filterParam string
	var resolvedLimitParam string
	for _, param := range endpoint.Body {
		switch strings.ToLower(strings.TrimSpace(param.Name)) {
		case limitParam:
			hasLimit = true
			resolvedLimitParam = param.Name
		case "filter", "filters":
			if param.Type == "array" {
				filterParam = param.Name
			}
		}
	}
	if !hasLimit || filterParam == "" {
		return "", "", 0
	}
	pageSize := 100
	if defaultSize, ok := paginationLimitDefault(endpoint, resolvedLimitParam); ok {
		pageSize = defaultSize
	}
	return filterParam, resolvedLimitParam, pageSize
}

func paginationLimitDefault(endpoint spec.Endpoint, limitParam string) (int, bool) {
	if strings.TrimSpace(limitParam) == "" {
		return 0, false
	}
	limitName := strings.ToLower(limitParam)
	params := append(append([]spec.Param{}, endpoint.Params...), endpoint.Body...)
	for _, param := range params {
		if strings.ToLower(param.Name) != limitName {
			continue
		}
		value, ok := syncBodyDefault(param)
		if !ok {
			return 0, false
		}
		switch v := value.(type) {
		case int:
			if v > 0 {
				return v, true
			}
		case int64:
			if v > 0 {
				return int(v), true
			}
		case float64:
			if v > 0 {
				return int(v), true
			}
		}
	}
	return 0, false
}

func syncPaginationDefaultsFromEndpoint(endpoint spec.Endpoint) (string, string, string, int) {
	cursorParam := ""
	cursorType := ""
	limitParam := ""
	if paginationDisabled(endpoint.Pagination) {
		return "", "", "", 0
	}
	if isRuntimePagination(endpoint.Pagination) {
		cursorParam = strings.TrimSpace(endpoint.Pagination.CursorParam)
		cursorType = strings.TrimSpace(endpoint.Pagination.Type)
		limitParam = strings.TrimSpace(endpoint.Pagination.LimitParam)
	}
	if cursorParam == "" || limitParam == "" {
		inferredCursor, inferredLimit := inferPaginationParamsFromEndpoint(endpoint)
		if cursorParam == "" {
			cursorParam = inferredCursor
		}
		if limitParam == "" {
			limitParam = inferredLimit
		}
	}
	if cursorType == "" {
		cursorType = inferPaginationType(cursorParam)
	}
	pageSize := 100
	if defaultSize, ok := paginationLimitDefault(endpoint, limitParam); ok {
		pageSize = defaultSize
	}
	return cursorParam, cursorType, limitParam, pageSize
}

func inferPaginationParamsFromEndpoint(endpoint spec.Endpoint) (string, string) {
	var cursorParam string
	var limitParam string
	for _, param := range endpoint.Params {
		if param.PathParam || param.Positional {
			continue
		}
		lower := strings.ToLower(param.Name)
		if cursorParam == "" && cursorParamCandidates[lower] {
			cursorParam = param.Name
		}
		if limitParam == "" && pageSizeParamCandidates[lower] {
			limitParam = param.Name
		}
	}
	return cursorParam, limitParam
}

func inferPaginationType(cursorParam string) string {
	switch strings.ToLower(strings.TrimSpace(cursorParam)) {
	case "":
		return ""
	case "page", "page_number", "pagenumber":
		return "page"
	case "offset", "skip":
		return "offset"
	case "page_token", "pagetoken":
		return "page_token"
	default:
		return "cursor"
	}
}

func detectEndpointSinceParamAndFormat(endpoint spec.Endpoint, types map[string]spec.TypeDef) (string, string) {
	for _, p := range endpoint.Params {
		name := strings.ToLower(p.Name)
		if isEndpointSinceParamName(name) {
			return p.Name, strings.ToLower(strings.TrimSpace(p.Format))
		}
	}
	for _, p := range endpoint.Params {
		if strings.EqualFold(strings.TrimSpace(p.Name), "conditions") {
			if field := detectODataConditionsTimestampField(endpoint, types); field != "" {
				return p.Name, "odata-conditions:" + field
			}
		}
	}
	return "", ""
}

func detectODataConditionsTimestampField(endpoint spec.Endpoint, types map[string]spec.TypeDef) string {
	typeName := strings.TrimSpace(endpoint.Response.Item)
	if typeName == "" || types == nil {
		return ""
	}
	typeDef, ok := types[typeName]
	if !ok {
		return ""
	}
	for _, field := range typeDef.Fields {
		if field.Name == "_info/lastUpdated" {
			return field.Name
		}
	}
	for _, candidate := range []string{
		"lastUpdated",
		"updated_at",
		"updatedAt",
		"modified_at",
		"modifiedAt",
		"last_modified",
		"lastModified",
		"date_updated",
		"dateUpdated",
		"updated_date",
		"modified_date",
	} {
		if field := responseTypeFieldByNormalizedName(typeDef, candidate); field != "" {
			return field
		}
	}
	for _, field := range typeDef.Fields {
		if strings.TrimSpace(field.Name) == "_info" && strings.EqualFold(strings.TrimSpace(field.Type), "object") {
			return "_info/lastUpdated"
		}
	}
	for _, field := range typeDef.Fields {
		name := strings.ToLower(field.Name)
		format := strings.ToLower(strings.TrimSpace(field.Format))
		if (format == "date" || format == "date-time") && (strings.Contains(name, "updated") || strings.Contains(name, "modified")) {
			return field.Name
		}
	}
	return ""
}

func responseTypeFieldByNormalizedName(typeDef spec.TypeDef, candidate string) string {
	normalizedCandidate := normalizeTemporalFieldName(candidate)
	for _, field := range typeDef.Fields {
		if normalizeTemporalFieldName(field.Name) == normalizedCandidate {
			return field.Name
		}
	}
	return ""
}

func normalizeTemporalFieldName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isEndpointSinceParamName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	normalized := normalizeTemporalFieldName(name)
	if strings.Contains(normalized, "since") ||
		strings.Contains(normalized, "updatedafter") ||
		strings.Contains(normalized, "modifiedafter") ||
		strings.Contains(normalized, "createdafter") ||
		strings.Contains(name, "updated_at") ||
		name == "start_date" ||
		name == "start_datetime" ||
		name == "start_time" ||
		name == "from_date" ||
		name == "from_datetime" {
		return true
	}
	base, op, ok := strings.Cut(name, "__")
	if !ok {
		return false
	}
	switch op {
	case "gt", "gte", "lt", "lte":
		return isTemporalComparisonBase(base)
	default:
		return false
	}
}

func isTemporalComparisonBase(base string) bool {
	normalized := normalizeTemporalFieldName(base)
	switch normalized {
	case "updated", "modified", "created", "updatedat", "modifiedat", "createdat", "updateddate", "modifieddate", "createddate":
		return true
	default:
		base = strings.ToLower(strings.TrimSpace(base))
		return strings.HasSuffix(base, "_at") || strings.HasSuffix(base, "_date")
	}
}

func detectEndpointFieldSelector(endpoint spec.Endpoint) FieldSelector {
	for _, param := range endpoint.Params {
		if param.Purpose != spec.ParamPurposeFieldSelector || strings.TrimSpace(param.FieldSelectorDefault) == "" {
			continue
		}
		// Sync applies one field-selector param per endpoint; the spec order
		// chooses which one wins when an API exposes several.
		return FieldSelector{
			Name:    param.WireName(),
			Default: strings.TrimSpace(param.FieldSelectorDefault),
		}
	}
	return FieldSelector{}
}

func endpointSupportsPagination(endpoint spec.Endpoint) bool {
	if paginationDisabled(endpoint.Pagination) {
		return false
	}
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

func paginationDisabled(p *spec.Pagination) bool {
	return p != nil && p.Type == spec.PaginationTypeNone
}

func isRuntimePagination(p *spec.Pagination) bool {
	return p != nil && p.Type != spec.PaginationTypeIDWalk && p.Type != spec.PaginationTypeNone
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

func applyPathDerivedIDFields(syncable map[string]syncableMeta, pathDerivedIDFields map[string]string, types map[string]spec.TypeDef) {
	for _, resourceName := range sortedKeys(syncable) {
		idField := pathDerivedIDFieldForResource(resourceName, pathDerivedIDFields)
		if idField == "" {
			continue
		}
		meta := syncable[resourceName]
		if !shouldUsePathDerivedIDField(meta.IDField) || !responseTypeHasField(meta.ResponseItem, types, idField) {
			continue
		}
		meta.IDField = idField
		syncable[resourceName] = meta
	}
}

func applyPathDerivedIDFieldsToParameterized(parameterized map[string]parameterizedEntry, pathDerivedIDFields map[string]string, types map[string]spec.TypeDef) {
	for _, key := range sortedKeys(parameterized) {
		entry := parameterized[key]
		idField := pathDerivedIDFieldForResource(entry.name, pathDerivedIDFields)
		if idField == "" || !shouldUsePathDerivedIDField(entry.meta.IDField) || !responseTypeHasField(entry.meta.ResponseItem, types, idField) {
			continue
		}
		entry.meta.IDField = idField
		parameterized[key] = entry
	}
}

func shouldUsePathDerivedIDField(existing string) bool {
	existing = strings.TrimSpace(existing)
	return existing == "" || strings.EqualFold(existing, "name")
}

func pathDerivedIDFieldForResource(resourceName string, pathDerivedIDFields map[string]string) string {
	for _, variant := range nameVariants(resourceName) {
		if idField := pathDerivedIDFields[variant]; idField != "" {
			return idField
		}
	}
	return ""
}

func responseTypeHasField(typeName string, types map[string]spec.TypeDef, fieldName string) bool {
	typeDef, ok := lookupTypeDef(typeName, types)
	if !ok {
		return false
	}
	fieldSnake := spec.ToSnakeCase(fieldName)
	for _, field := range typeDef.Fields {
		if field.Name == fieldName || spec.ToSnakeCase(field.Name) == fieldSnake {
			return true
		}
	}
	return false
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
			Name:                  name,
			Path:                  meta.Path,
			Method:                meta.Method,
			Tier:                  meta.Tier,
			SkipDefaultSync:       meta.SkipDefaultSync,
			IDField:               meta.IDField,
			Critical:              meta.Critical,
			SinceParam:            meta.SinceParam,
			SinceParamFormat:      meta.SinceParamFormat,
			SupportsPagination:    meta.SupportsPagination,
			PaginationCursorParam: meta.PaginationCursorParam,
			PaginationCursorType:  meta.PaginationCursorType,
			PaginationLimitParam:  meta.PaginationLimitParam,
			PaginationPageSize:    meta.PaginationPageSize,
			UsesHTMLResponse:      meta.UsesHTMLResponse,
			HTMLExtract:           meta.HTMLExtract,
			BodyFields:            meta.BodyFields,
			IDWalkFilterParam:     meta.IDWalkFilterParam,
			IDWalkLimitParam:      meta.IDWalkLimitParam,
			IDWalkPageSize:        meta.IDWalkPageSize,
			FieldSelector:         meta.FieldSelector,
			Discriminator:         meta.Discriminator,
			QueryEntity:           meta.QueryEntity,
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

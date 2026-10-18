package generator

import (
	"cmp"
	"regexp"
	"slices"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// AsyncJobInfo describes one async-job endpoint detected from a spec.
// Populated at generation time and carried into template data so generated
// commands can expose --wait and a shared jobs store.
type AsyncJobInfo struct {
	// ResourceName and EndpointName identify the submitting endpoint
	// (the one that returns a job ID).
	ResourceName string
	EndpointName string

	// JobIDField is the response field name that carries the job identifier
	// (e.g. "job_id", "task_id"). Empty when detection fell back to
	// path-sibling inference.
	JobIDField string

	// StatusResource and StatusEndpoint point at the sibling status
	// endpoint used by the --wait polling loop. Empty only if no sibling
	// was found (in which case the endpoint is not marked async).
	StatusResource string
	StatusEndpoint string

	// StatusPath is the URL path template for the polling endpoint,
	// copied from the sibling endpoint's Path. The runtime substitutes
	// {id} or {job_id} with the actual job ID.
	StatusPath string

	// StatusCommand is the printed command path (without the binary name)
	// that reads the status endpoint, e.g. "renders get".
	StatusCommand string

	// StatusRecoveryPrefix is printed before the job ID as the recovery
	// command when --wait fails after submission. It follows the status
	// command's real binding for the ID ("renders get " for a positional,
	// "renders get --id " for a flag) so the user fetches the submitted job,
	// not a default one. When the binding cannot be expressed with the job
	// ID alone it falls back to the local ledger ("jobs get ").
	StatusRecoveryPrefix string

	// TerminalField and TerminalValues describe how the polling loop
	// decides the job is done. Defaults to "status" with a common
	// done/complete/completed/failed/errored set when the spec does not
	// declare one explicitly.
	TerminalField  string
	TerminalValues []string
}

// jobIDFieldPattern matches field names that typically carry a job or
// long-running-operation identifier in API responses.
var jobIDFieldPattern = regexp.MustCompile(`(?i)^(job|task|operation|request|async|run|batch)_?id$`)

// terminalValueDefaults is the set of status values treated as terminal
// when the spec does not declare a status enum on the sibling endpoint.
var terminalValueDefaults = []string{"done", "complete", "completed", "success", "succeeded", "finished", "failed", "errored", "cancelled", "canceled"}

// DetectAsyncJobs walks an APISpec and returns one AsyncJobInfo per detected
// async-job endpoint, keyed by "<resourceName>/<endpointName>".
//
// Detection requires both strong signals:
//
//  1. The endpoint's response payload contains a field whose name matches
//     jobIDFieldPattern (job_id, task_id, operation_id, ...).
//  2. A sibling endpoint exists that looks like a status probe for the same
//     resource - either a GET on the same resource (common get/status/show),
//     a same-resource sibling whose name contains "status" or "poll", or an
//     endpoint in a separate jobs/operations/status-flavored resource.
//     When several siblings qualify, findStatusSibling ranks them so repeated
//     generation of the same spec emits the same polling target.
//
// Both signals are load-bearing: the job-id field names what to track and the
// sibling names where to poll. A plain CRUD POST that happens to have a sibling
// GET is not marked async - without a job-id-shaped response field, we do not
// assume the endpoint is long-running.
//
// HTTP status code 202 would be a third signal; the internal spec model does
// not track per-status-code responses, so we omit it and rely on the two
// remaining signals.
//
// The function is pure - it does not mutate the spec. The caller decides
// how to use the detection map (template data, dogfood checks, scorecard).
func DetectAsyncJobs(s *spec.APISpec) map[string]AsyncJobInfo {
	out := map[string]AsyncJobInfo{}
	if s == nil {
		return out
	}

	for rName, r := range s.Resources {
		for eName, ep := range r.Endpoints {
			info, ok := detectOne(s, rName, eName, ep)
			if !ok {
				continue
			}
			out[rName+"/"+eName] = info
		}
	}
	return out
}

func detectOne(s *spec.APISpec, rName, eName string, ep spec.Endpoint) (AsyncJobInfo, bool) {
	jobField := responseJobIDField(s, ep)
	if jobField == "" {
		return AsyncJobInfo{}, false
	}
	statusRes, statusEP := findStatusSibling(s, rName, eName)
	if statusEP == "" {
		return AsyncJobInfo{}, false
	}

	statusPath := ""
	if r, ok := s.Resources[statusRes]; ok {
		if sep, ok := r.Endpoints[statusEP]; ok {
			statusPath = sep.Path
		}
	}

	return AsyncJobInfo{
		ResourceName:         rName,
		EndpointName:         eName,
		JobIDField:           jobField,
		StatusResource:       statusRes,
		StatusEndpoint:       statusEP,
		StatusPath:           statusPath,
		StatusCommand:        asyncStatusCommandPath(s, statusRes, statusEP),
		StatusRecoveryPrefix: asyncStatusRecoveryPrefix(s, statusRes, statusEP),
		TerminalField:        "status",
		TerminalValues:       terminalValueDefaults,
	}, true
}

// asyncStatusCommandPath mirrors the command tree the generator emits for
// the status endpoint: a promoted single-endpoint resource (or a promoted
// GraphQL get) is invoked as "<resource>", every other endpoint as
// "<resource> <endpoint>".
func asyncStatusCommandPath(s *spec.APISpec, statusRes, statusEP string) string {
	for _, pc := range buildPromotedCommands(s) {
		if pc.ResourceName == statusRes && pc.EndpointName == statusEP {
			return pc.PromotedName
		}
	}
	return toKebab(statusRes) + " " + toKebab(statusEP)
}

func asyncStatusRecoveryPrefix(s *spec.APISpec, statusRes, statusEP string) string {
	const ledgerFallback = "jobs get "
	r, ok := s.Resources[statusRes]
	if !ok {
		return ledgerFallback
	}
	ep, ok := r.Endpoints[statusEP]
	if !ok {
		return ledgerFallback
	}
	// WaitForJob substitutes the job ID into {id} or {job_id}; the recovery
	// command must feed the same parameter.
	var idParam *spec.Param
	for i := range ep.Params {
		name := ep.Params[i].Name
		if (name == "id" || name == "job_id") && strings.Contains(ep.Path, "{"+name+"}") {
			idParam = &ep.Params[i]
			break
		}
	}
	if idParam == nil {
		return ledgerFallback
	}
	// Any other positional (even a defaulted one shifts which argument the
	// job ID binds to) or required input without a default cannot be filled
	// from the job ID alone; point at the local ledger instead.
	for _, p := range ep.Params {
		if p.Name != idParam.Name && (p.Positional || (p.Required && p.Default == nil)) {
			return ledgerFallback
		}
	}
	command := asyncStatusCommandPath(s, statusRes, statusEP)
	if idParam.Positional {
		return command + " "
	}
	return command + " --" + publicFlagName(*idParam) + " "
}

// responseJobIDField returns the matching job-id-shaped field name in the
// endpoint's response type, or "" if none.
func responseJobIDField(s *spec.APISpec, ep spec.Endpoint) string {
	if ep.Response.Item == "" {
		return ""
	}
	td, ok := s.Types[ep.Response.Item]
	if !ok {
		return ""
	}
	for _, f := range td.Fields {
		if jobIDFieldPattern.MatchString(f.Name) {
			return f.Name
		}
	}
	return ""
}

// statusProbeNames is the conventional polling-endpoint order. An exact name
// on the submitting resource wins in this order. Cross-resource endpoints use
// the same order, with an exact name ahead of a substring match.
var statusProbeNames = []string{"get", "status", "show", "retrieve"}

// statusResourceMarkers are the conventional partners of a submitting
// resource ("videos" → "videos_status" or "video_jobs"). A resource can
// still qualify when its name merely contains status, job, task, or operation.
var statusResourceMarkers = []string{
	"status", "job", "jobs", "task", "tasks", "operation", "operations",
}

// Lower ranks are closer name matches to the submitting resource.
const (
	statusAffinityExactName = iota
	statusAffinityExactStem
	statusAffinityContainsName
	statusAffinityContainsStem
	statusAffinityUnrelated
)

// findStatusSibling returns the polling endpoint for an async job submitted
// by (rName, eName), or ("", "") when nothing qualifies.
//
// Candidates are ranked so the same spec always selects the same sibling.
// Map iteration order is not a choice.
//
//  1. Same resource, first conventional probe that exists: get, status,
//     show, retrieve. The submitting endpoint is skipped.
//  2. Same resource, a name containing "status" or "poll". Prefer a name
//     that includes the submitting endpoint as a separator-bounded whole
//     (non-alphanumeric runs are separators, case-insensitive), so
//     submit_render matches submit_render_poll and not submit_renderer.
//     Then a name containing "status" over one that only contains "poll",
//     then the lexicographically smaller endpoint name.
//  3. A different resource whose name contains status, job, task, or
//     operation, with an endpoint whose name contains get, status, show,
//     or retrieve. Prefer an exact "<name>_<marker>" or "<marker>_<name>"
//     resource, using the submitting resource's full name and then its
//     singular stem (a trailing "s" dropped, or a trailing "ies" rewritten
//     to "y"). Next, a name that contains that full name or stem as a
//     separator-bounded whole, then any other qualifying resource. Endpoint names
//     prefer exact get, status, show, retrieve, then those words as
//     substrings, in that order. Remaining ties break on resource name,
//     then endpoint name.
func findStatusSibling(s *spec.APISpec, rName, eName string) (string, string) {
	if r, ok := s.Resources[rName]; ok {
		for _, candidate := range statusProbeNames {
			if _, exists := r.Endpoints[candidate]; exists && candidate != eName {
				return rName, candidate
			}
		}
		if name := bestSameResourceStatusName(r.Endpoints, eName); name != "" {
			return rName, name
		}
	}
	return bestCrossResourceStatus(s, rName)
}

type sameResourceStatusCandidate struct {
	token  int
	status int
	name   string
}

func bestSameResourceStatusName(endpoints map[string]spec.Endpoint, eName string) string {
	var cands []sameResourceStatusCandidate
	submitter := normalizeStatusIdent(eName)
	for name := range endpoints {
		if name == eName || !endpointNameHasStatusOrPoll(name) {
			continue
		}
		cands = append(cands, sameResourceStatusCandidate{
			token:  preferFirst(normalizedIdentHasToken(normalizeStatusIdent(name), submitter)),
			status: preferFirst(strings.Contains(strings.ToLower(name), "status")),
			name:   name,
		})
	}
	if len(cands) == 0 {
		return ""
	}
	return slices.MinFunc(cands, compareSameResourceStatus).name
}

func compareSameResourceStatus(a, b sameResourceStatusCandidate) int {
	if c := cmp.Compare(a.token, b.token); c != 0 {
		return c
	}
	if c := cmp.Compare(a.status, b.status); c != 0 {
		return c
	}
	return cmp.Compare(a.name, b.name)
}

type crossStatusCandidate struct {
	affinity int
	probe    int
	resource string
	endpoint string
}

func bestCrossResourceStatus(s *spec.APISpec, rName string) (string, string) {
	var cands []crossStatusCandidate
	for otherName, other := range s.Resources {
		if otherName == rName || !resourceNameLooksLikeStatusTarget(otherName) {
			continue
		}
		for candName := range other.Endpoints {
			if !endpointNameLooksLikeProbe(candName) {
				continue
			}
			cands = append(cands, crossStatusCandidate{
				affinity: statusResourceAffinity(rName, otherName),
				probe:    statusProbeRank(candName),
				resource: otherName,
				endpoint: candName,
			})
		}
	}
	if len(cands) == 0 {
		return "", ""
	}
	best := slices.MinFunc(cands, compareCrossStatus)
	return best.resource, best.endpoint
}

func compareCrossStatus(a, b crossStatusCandidate) int {
	if c := cmp.Compare(a.affinity, b.affinity); c != 0 {
		return c
	}
	if c := cmp.Compare(a.probe, b.probe); c != 0 {
		return c
	}
	if c := cmp.Compare(a.resource, b.resource); c != 0 {
		return c
	}
	return cmp.Compare(a.endpoint, b.endpoint)
}

func statusResourceAffinity(submitter, candidate string) int {
	full := normalizeStatusIdent(submitter)
	stem := singularStatusIdent(full)
	other := normalizeStatusIdent(candidate)
	switch {
	case exactStatusResourceName(other, full):
		return statusAffinityExactName
	case stem != full && exactStatusResourceName(other, stem):
		return statusAffinityExactStem
	case normalizedIdentHasToken(other, full):
		return statusAffinityContainsName
	case stem != full && normalizedIdentHasToken(other, stem):
		return statusAffinityContainsStem
	default:
		return statusAffinityUnrelated
	}
}

func exactStatusResourceName(name, stem string) bool {
	if stem == "" || name == "" {
		return false
	}
	for _, marker := range statusResourceMarkers {
		if name == stem+"_"+marker || name == marker+"_"+stem {
			return true
		}
	}
	return false
}

func endpointNameHasStatusOrPoll(name string) bool {
	low := strings.ToLower(name)
	return strings.Contains(low, "status") || strings.Contains(low, "poll")
}

func resourceNameLooksLikeStatusTarget(name string) bool {
	low := strings.ToLower(name)
	return strings.Contains(low, "status") ||
		strings.Contains(low, "job") ||
		strings.Contains(low, "task") ||
		strings.Contains(low, "operation")
}

func endpointNameLooksLikeProbe(name string) bool {
	low := strings.ToLower(name)
	return strings.Contains(low, "get") ||
		strings.Contains(low, "status") ||
		strings.Contains(low, "show") ||
		strings.Contains(low, "retrieve")
}

func statusProbeRank(name string) int {
	low := strings.ToLower(name)
	for i, probe := range statusProbeNames {
		if low == probe {
			return i
		}
	}
	for i, probe := range statusProbeNames {
		if strings.Contains(low, probe) {
			return len(statusProbeNames) + i
		}
	}
	return len(statusProbeNames) * 2
}

func normalizeStatusIdent(name string) string {
	var b strings.Builder
	prevSep := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevSep = false
			continue
		}
		if !prevSep && b.Len() > 0 {
			b.WriteByte('_')
			prevSep = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func singularStatusIdent(name string) string {
	switch {
	case strings.HasSuffix(name, "ies") && len(name) > len("ies"):
		return strings.TrimSuffix(name, "ies") + "y"
	case strings.HasSuffix(name, "s") && !strings.HasSuffix(name, "ss") && len(name) > 1:
		return strings.TrimSuffix(name, "s")
	default:
		return name
	}
}

func normalizedIdentHasToken(name, token string) bool {
	if token == "" || name == "" {
		return false
	}
	if name == token || strings.HasPrefix(name, token+"_") || strings.HasSuffix(name, "_"+token) {
		return true
	}
	return strings.Contains(name, "_"+token+"_")
}

func preferFirst(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

package generator

import (
	"regexp"
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
		ResourceName:   rName,
		EndpointName:   eName,
		JobIDField:     jobField,
		StatusResource: statusRes,
		StatusEndpoint: statusEP,
		StatusPath:     statusPath,
		TerminalField:  "status",
		TerminalValues: terminalValueDefaults,
	}, true
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

// findStatusSibling looks for a sibling endpoint that functions as the
// polling target for an async job submitted by (rName, eName). Returns the
// resource/endpoint of the sibling, or ("","") if none is found.
func findStatusSibling(s *spec.APISpec, rName, eName string) (string, string) {
	// Same-resource sibling by common name
	r, ok := s.Resources[rName]
	if ok {
		for _, candidate := range []string{"get", "status", "show", "retrieve"} {
			if _, exists := r.Endpoints[candidate]; exists && candidate != eName {
				return rName, candidate
			}
		}
		// Name-based sibling: <eName>_status or status_<eName>
		for cName := range r.Endpoints {
			low := strings.ToLower(cName)
			if cName == eName {
				continue
			}
			if strings.Contains(low, "status") || strings.Contains(low, "poll") {
				return rName, cName
			}
		}
	}
	// Separate resource named <rName>_status / <rName>_jobs
	for otherName, other := range s.Resources {
		if otherName == rName {
			continue
		}
		low := strings.ToLower(otherName)
		if !strings.Contains(low, "status") && !strings.Contains(low, "job") && !strings.Contains(low, "task") && !strings.Contains(low, "operation") {
			continue
		}
		for candName := range other.Endpoints {
			cl := strings.ToLower(candName)
			if strings.Contains(cl, "get") || strings.Contains(cl, "status") || strings.Contains(cl, "show") || strings.Contains(cl, "retrieve") {
				return otherName, candName
			}
		}
	}
	return "", ""
}

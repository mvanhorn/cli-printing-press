package profiler

import (
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// Abbreviations such as EST and CST in API date filters mean US civil
// time, not a fixed offset and not a similarly spelled word. Specific
// zones are listed before UTC so "Eastern Time (UTC-5)" stays Eastern.
var sinceLocalZones = []struct {
	pattern *regexp.Regexp
	name    string
}{
	{regexp.MustCompile(`(?i)\b(?:america/new_york|us/eastern|eastern time|est|edt)\b`), "America/New_York"},
	{regexp.MustCompile(`(?i)\b(?:america/chicago|us/central|central time|cst|cdt)\b`), "America/Chicago"},
	{regexp.MustCompile(`(?i)\b(?:america/denver|us/mountain|mountain time|mst|mdt)\b`), "America/Denver"},
	{regexp.MustCompile(`(?i)\b(?:america/los_angeles|us/pacific|pacific time|pst|pdt)\b`), "America/Los_Angeles"},
	{regexp.MustCompile(`(?i)\b(?:utc|gmt)\b`), "UTC"},
}

var (
	sinceOperatorExampleRE = regexp.MustCompile(`>=\s*(\d{4}-\d{2}-\d{2})(?:[^0-9T]|$)`)
	sinceDateExampleRE     = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	sinceNaiveClockRE      = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}:\d{2}`)
	sinceZonedClockRE      = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[Zz]|[+-]\d{2}:?\d{2})`)
	// Letter clocks such as YYYY-MM-DDTHH:mm:ssZ are timestamps. The digit
	// patterns above do not see them, and the date-only check would otherwise
	// match the YYYY-MM-DD prefix and drop the time.
	sinceTimestampPlaceholderRE = regexp.MustCompile(`(?i)t['"]?\s*hh:mm:ss`)
)

var sinceRejectTokens = map[string]struct{}{
	"before": {}, "until": {}, "till": {}, "to": {},
	"max": {}, "maximum": {}, "end": {},
	"lt": {}, "lte": {}, "upper": {},
	"by": {}, "id": {}, "ids": {},
	"fields": {}, "field": {},
	"columns": {}, "column": {},
	"include": {}, "count": {}, "status": {},
	"type": {}, "name": {}, "user": {},
	"flag": {}, "flags": {}, "list": {},
	"with": {}, "only": {},
}

func detectEndpointSinceParamAndFormat(endpoint spec.Endpoint, types map[string]spec.TypeDef) (string, string) {
	// Page tokens and watermarks must not share a query key.
	cursor := endpointSyncCursorParam(endpoint)
	bestRank := 0
	var best spec.Param
	found := false
	for _, p := range endpoint.Params {
		if !isSyncQueryParam(p) || sinceParamCollidesWithCursor(p, cursor) {
			continue
		}
		rank := sinceParamRank(p, endpoint.Params)
		if rank > bestRank {
			bestRank = rank
			best = p
			found = true
		}
	}
	if found {
		return best.Name, sinceValueFormat(best)
	}
	for _, p := range endpoint.Params {
		if !strings.EqualFold(strings.TrimSpace(p.Name), "conditions") {
			continue
		}
		if sinceParamCollidesWithCursor(p, cursor) {
			continue
		}
		if field := detectODataConditionsTimestampField(endpoint, types); field != "" {
			return p.Name, "odata-conditions:" + field
		}
	}
	return "", ""
}

func endpointSyncCursorParam(endpoint spec.Endpoint) string {
	cursor, _, _, _ := syncPaginationDefaultsFromEndpoint(endpoint)
	return strings.TrimSpace(cursor)
}

func sinceParamCollidesWithCursor(p spec.Param, cursor string) bool {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return false
	}
	return strings.EqualFold(p.Name, cursor) || strings.EqualFold(p.WireName(), cursor)
}

func isSyncQueryParam(p spec.Param) bool {
	if p.PathParam || p.Positional {
		return false
	}
	loc := strings.TrimSpace(p.In)
	return loc == "" || strings.EqualFold(loc, "query")
}

// Generic start/from names rank below updated/modified/since names so a
// calendar field such as start_date does not beat updated_on. Upper bounds
// and non-value names score zero so they never become the watermark param.
func sinceParamRank(p spec.Param, params []spec.Param) int {
	if isBooleanParamType(p.Type) {
		return 0
	}
	if isNumericParamType(p.Type) && !isEndpointSinceParamName(p.Name) {
		return 0
	}
	if sinceNameRejected(p.Name) {
		return 0
	}
	if updatedModifiedSinceRank(p.Name) {
		return 3
	}
	if isCreatedAfterName(p.Name) || isDRFTemporalLowerBound(p.Name) {
		return 2
	}
	if genericStartQualifies(p, params) {
		return 1
	}
	return 0
}

func isBooleanParamType(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "boolean", "bool":
		return true
	default:
		return false
	}
}

func isNumericParamType(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "integer", "number", "int", "int32", "int64", "float", "float32", "float64", "double":
		return true
	default:
		return false
	}
}

func sinceNameRejected(name string) bool {
	for _, token := range collectionNameTokens(name) {
		if _, ok := sinceRejectTokens[token]; ok {
			return true
		}
	}
	normalized := normalizeTemporalFieldName(name)
	for _, suffix := range []string{"before", "until", "max", "upper", "to", "end", "lt", "lte"} {
		if len(normalized) > len(suffix) && strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func updatedModifiedSinceRank(name string) bool {
	normalized := normalizeTemporalFieldName(name)
	if strings.Contains(normalized, "since") ||
		strings.Contains(normalized, "updatedafter") ||
		strings.Contains(normalized, "modifiedafter") ||
		gluedUpdatedModified(normalized) {
		return true
	}
	tokens := collectionNameTokens(name)
	hasUpdated := false
	hasModified := false
	for _, token := range tokens {
		switch token {
		case "updated", "update":
			hasUpdated = true
		case "modified", "modify":
			hasModified = true
		}
	}
	if !hasUpdated && !hasModified {
		return false
	}
	// collectionNameTokens also keeps the unsplit camel blob ("updatedmin").
	// That blob is not a marker, so judge the split tokens. Every one of
	// those has to be temporal: min_updated_ts is a numeric lower bound,
	// not a calendar filter, even though it contains "min" and "updated".
	sawSplit := false
	for _, token := range tokens {
		if token == normalized && len(tokens) > 1 {
			continue
		}
		sawSplit = true
		switch token {
		case "updated", "update", "modified", "modify",
			"at", "on", "min", "after", "since", "date", "time", "datetime", "timestamp", "last":
			continue
		default:
			return false
		}
	}
	return sawSplit
}

func gluedUpdatedModified(normalized string) bool {
	for _, stem := range []string{"updated", "modified", "update", "modify"} {
		rest, ok := strings.CutPrefix(normalized, stem)
		if !ok {
			rest, ok = strings.CutSuffix(normalized, stem)
		}
		if !ok {
			continue
		}
		switch rest {
		case "", "at", "on", "min", "after", "since", "date", "time", "datetime", "timestamp", "last":
			return true
		}
	}
	return false
}

func isCreatedAfterName(name string) bool {
	return strings.Contains(normalizeTemporalFieldName(name), "createdafter")
}

func isDRFTemporalLowerBound(name string) bool {
	base, op, ok := strings.Cut(strings.ToLower(strings.TrimSpace(name)), "__")
	if !ok {
		return false
	}
	switch op {
	case "gt", "gte":
		return isTemporalComparisonBase(base)
	default:
		return false
	}
}

func genericStartQualifies(p spec.Param, params []spec.Param) bool {
	end, ok := genericStartEndName(p.Name)
	if !ok {
		return false
	}
	if describesModification(p.Description) {
		return true
	}
	if !hasDateEvidence(p) {
		return false
	}
	return hasMatchingQueryEnd(params, end)
}

func genericStartEndName(name string) (string, bool) {
	normalized := normalizeTemporalFieldName(name)
	switch {
	case normalized == "after":
		return "before", true
	case normalized == "from":
		return "to", true
	case normalized == "start":
		return "end", true
	case strings.HasPrefix(normalized, "from") && len(normalized) > len("from"):
		return "to" + strings.TrimPrefix(normalized, "from"), true
	case strings.HasSuffix(normalized, "from") && len(normalized) > len("from"):
		return strings.TrimSuffix(normalized, "from") + "to", true
	case strings.HasPrefix(normalized, "start") && len(normalized) > len("start"):
		return "end" + strings.TrimPrefix(normalized, "start"), true
	case strings.HasSuffix(normalized, "start") && len(normalized) > len("start"):
		return strings.TrimSuffix(normalized, "start") + "end", true
	default:
		return "", false
	}
}

func hasMatchingQueryEnd(params []spec.Param, endNormalized string) bool {
	for _, p := range params {
		if !isSyncQueryParam(p) {
			continue
		}
		if normalizeTemporalFieldName(p.Name) == endNormalized || normalizeTemporalFieldName(p.WireName()) == endNormalized {
			return true
		}
	}
	return false
}

func describesModification(desc string) bool {
	lower := strings.ToLower(desc)
	return strings.Contains(lower, "updated") ||
		strings.Contains(lower, "modified") ||
		strings.Contains(lower, "modification") ||
		strings.Contains(lower, "last changed") ||
		strings.Contains(lower, "last change")
}

func hasDateEvidence(p spec.Param) bool {
	if canonicalDateFormat(p.Format) != "" {
		return true
	}
	desc := strings.ToLower(p.Description)
	if sinceDateExampleRE.MatchString(desc) {
		return true
	}
	return strings.Contains(desc, "rfc3339") ||
		strings.Contains(desc, "date-time") ||
		strings.Contains(desc, "iso8601") ||
		strings.Contains(desc, "iso 8601") ||
		strings.Contains(desc, "yyyy-mm-dd")
}

func sinceValueFormat(p spec.Param) string {
	schema := canonicalDateFormat(p.Format)
	if sinceOperatorExampleRE.MatchString(p.Description) {
		layout := schema
		if layout == "" {
			layout = "date"
		}
		return "operator:>=" + ":" + layout
	}
	if schema != "" {
		return schema
	}
	if zone := naiveLocalZone(p.Description); zone != "" {
		return "local-datetime:" + zone
	}
	if hasDateOnlyHint(p.Description) {
		return "date"
	}
	return ""
}

func canonicalDateFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "date", "date-time":
		return strings.ToLower(strings.TrimSpace(format))
	default:
		return ""
	}
}

func naiveLocalZone(desc string) string {
	if !sinceNaiveClockRE.MatchString(desc) || sinceZonedClockRE.MatchString(desc) {
		return ""
	}
	for _, zone := range sinceLocalZones {
		if zone.pattern.MatchString(desc) {
			return zone.name
		}
	}
	return ""
}

func hasDateOnlyHint(desc string) bool {
	if sinceNaiveClockRE.MatchString(desc) || sinceZonedClockRE.MatchString(desc) || sinceTimestampPlaceholderRE.MatchString(desc) {
		return false
	}
	lower := strings.ToLower(desc)
	// RFC3339 and ISO-8601 datetime wording name a timestamp even when the
	// same sentence also shows a YYYY-MM-DD fragment. Sending that fragment
	// alone makes timestamp-only endpoints reject the filter.
	if strings.Contains(lower, "rfc3339") || iso8601DateTimeWording(lower) {
		return false
	}
	return strings.Contains(lower, "yyyy-mm-dd") || sinceDateExampleRE.MatchString(desc)
}

func iso8601DateTimeWording(lower string) bool {
	if !strings.Contains(lower, "datetime") && !strings.Contains(lower, "date-time") && !strings.Contains(lower, "date time") {
		return false
	}
	compact := strings.NewReplacer("-", "", " ", "").Replace(lower)
	return strings.Contains(compact, "iso8601")
}

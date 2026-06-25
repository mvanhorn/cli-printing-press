package browsersniff

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type EndpointGroup struct {
	// Host is the originating URL host for this group (lowercased), set by
	// host-aware dedup paths like DeduplicateTrafficEndpoints. Empty when the
	// caller doesn't care about host separation (DeduplicateEndpoints flattens
	// across hosts). The cross-entry variance pass keys on this field so two
	// groups sharing method/path-shape but coming from different hosts stay
	// separate.
	Host           string
	Method         string
	NormalizedPath string
	Entries        []EnrichedEntry
	LowConfidence  []LowConfidenceParameter
}

var (
	uuidSegmentPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	hashSegmentPattern = regexp.MustCompile(`(?i)^[0-9a-f]{32,}$`)
	numericPattern     = regexp.MustCompile(`^\d+$`)
	isoDatePattern     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	shortCodePattern   = regexp.MustCompile(`^[A-Z]{2,4}$`)
	// prefixedIDPattern matches application-issued IDs that ship a short
	// type-prefix and a long alphanumeric tail, such as Clay's t_/r_/c_, Stripe's
	// cus_/sub_, or OpenAI's run_/asst_. The tail floor of 8 chars keeps short
	// literal segments like "v1" or two-letter language codes from matching.
	prefixedIDPattern = regexp.MustCompile(`^[a-z]{1,5}_[A-Za-z0-9]{8,}$`)
	// longAlnumIDPattern matches opaque application IDs without separators that
	// are long enough and mixed enough to be implausible as literal route names:
	// nanoid (21 chars), ULID (26 chars), short base62 IDs like OpenArt's
	// `Zu2uNCmGDnmNCel8gbFQ` (20 chars). The mixed-case-or-digit floor below
	// rules out long lowercase words (e.g. "subscriptions", "notifications").
	longAlnumIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{20,}$`)
	// colonCompositePattern matches IDs that carry an embedded type discriminator
	// via colon segments, like OpenArt form_ids
	// (`create-image:reference:gpt-image-2`) or Stripe price tier IDs. Requires
	// at least two colons total so simple key:value or port-style values
	// (e.g. `host:80`) don't get pulled in as composite IDs.
	colonCompositePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+:[A-Za-z0-9._-]+:[A-Za-z0-9._:-]+$`)
	blocklistMu           sync.RWMutex
	additionalBlocklist   []string
	includeListMu         sync.RWMutex
	additionalInclude     []string
	telemetryHosts        = []string{
		"sentry.io",
		"datadoghq.com",
		"intercom.io",
		"intercom.com",
		"segment.com",
		"segment.io",
		"statsig.com",
		"fullstory.com",
		"launchdarkly.com",
		"eppo.cloud",
		"mixpanel.com",
		"amplitude.com",
		"pendo.io",
		"hotjar.com",
		"vortex.data.microsoft.com",
		"dc.services.visualstudio.com",
		"analytics.google.com",
	}
	telemetryPathMarkers = []string{"/sentry/envelope/", "/intake/v1/", "/intercom/"}
	telemetryQueryKeys   = []string{"sentry_key", "dd-api-key", "dd-client-token", "ddsource", "intercom-device-id"}
)

func ClassifyEntries(entries []EnrichedEntry) (api []EnrichedEntry, noise []EnrichedEntry) {
	api = make([]EnrichedEntry, 0, len(entries))
	noise = make([]EnrichedEntry, 0, len(entries))

	blocklistMu.RLock()
	extraBlocklist := append([]string(nil), additionalBlocklist...)
	blocklistMu.RUnlock()

	blocklist := append(DefaultBlocklist(), extraBlocklist...)
	include := includePatterns()
	for _, entry := range entries {
		score := scoreEntry(entry, blocklist, include)
		classified := entry
		if score > 0 {
			classified.Classification = "api"
			classified.IsNoise = false
			api = append(api, classified)
			continue
		}

		classified.Classification = "noise"
		classified.IsNoise = true
		noise = append(noise, classified)
	}

	return api, noise
}

func SetAdditionalBlocklist(domains []string) {
	blocklistMu.Lock()
	defer blocklistMu.Unlock()

	additionalBlocklist = append([]string(nil), domains...)
}

// SetAdditionalIncludeList stores operator-supplied include patterns that
// force a positive score in classification regardless of blocklist matches
// or static-asset suffix demotion. Patterns are matched as case-insensitive
// substrings against the URL's host and path. Include wins over blocklist.
func SetAdditionalIncludeList(patterns []string) {
	includeListMu.Lock()
	defer includeListMu.Unlock()

	additionalInclude = append([]string(nil), patterns...)
}

func includePatterns() []string {
	includeListMu.RLock()
	defer includeListMu.RUnlock()
	if len(additionalInclude) == 0 {
		return nil
	}
	out := make([]string, len(additionalInclude))
	copy(out, additionalInclude)
	return out
}

// matchesIncludePattern returns true when any include pattern is a
// case-insensitive substring of host or path. Substring matching keeps the
// flag friendly to operators: --include "/track/important" or
// --include "api.partner.com" both work without quoting regex metacharacters.
func matchesIncludePattern(host string, path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	lowerHost := strings.ToLower(host)
	lowerPath := strings.ToLower(path)
	for _, pattern := range patterns {
		p := strings.ToLower(strings.TrimSpace(pattern))
		if p == "" {
			continue
		}
		if strings.Contains(lowerHost, p) || strings.Contains(lowerPath, p) {
			return true
		}
	}
	return false
}

func DefaultBlocklist() []string {
	hosts := []string{
		"google-analytics.com",
		"doubleclick.net",
		"facebook.com",
		"googlesyndication.com",
		"googletagmanager.com",
		"fonts.googleapis.com",
		"gstatic.com",
		"bat.bing.com",
		"criteo.com",
		"demdex.net",
		"onetrust.com",
		"cookielaw.org",
		"amazon-adsystem.com",
		"adsymptotic.com",
		"improving.duckduckgo.com",
		"lngtd.com",
		"kargo.com",
		"newrelic.com",
		"branch.io",
		"stats.g.doubleclick.net",
		"adservice.google.com",
		"connect.facebook.net",
	}
	return append(hosts, telemetryHosts...)
}

func DeduplicateEndpoints(entries []EnrichedEntry) []EndpointGroup {
	groups := make([]EndpointGroup, 0)
	indexByKey := make(map[string]int)

	for _, entry := range entries {
		method := strings.ToUpper(strings.TrimSpace(entry.Method))
		normalizedPath, lowConfidence := normalizeEntryPathWithHints(entry.URL)
		key := method + " " + normalizedPath

		if idx, ok := indexByKey[key]; ok {
			groups[idx].Entries = append(groups[idx].Entries, entry)
			continue
		}

		indexByKey[key] = len(groups)
		groups = append(groups, EndpointGroup{
			Method:         method,
			NormalizedPath: normalizedPath,
			Entries:        []EnrichedEntry{entry},
			LowConfidence:  lowConfidence,
		})
	}

	return collapseVariantGroups(groups)
}

func scoreEntry(entry EnrichedEntry, blocklist []string, include []string) int {
	score := 0
	responseType := strings.ToLower(entry.ResponseContentType)
	requestType := strings.ToLower(getHeaderValue(entry.RequestHeaders, "Content-Type"))
	path := strings.ToLower(extractPath(entry.URL))
	host := strings.ToLower(extractHost(entry.URL))
	urlLower := strings.ToLower(entry.URL)

	// Operator-supplied include patterns short-circuit the rest of scoring:
	// a match forces a strong positive score, bypassing blocklist demotion,
	// static-asset suffix demotion, and the response-content-type penalty.
	// Used to rescue a specific endpoint or host that default heuristics
	// would otherwise drop.
	if matchesIncludePattern(host, path, include) {
		return 10
	}

	if isTelemetryEntry(entry) {
		return -100
	}

	if strings.Contains(responseType, "application/json") {
		score += 2
	}

	if strings.Contains(requestType, "application/json") || strings.Contains(requestType, "application/x-www-form-urlencoded") {
		score++
	}

	for _, indicator := range []string{"/api/", "/v1/", "/v2/", "/v3/", "/graphql", "/data/", "/youtubei/"} {
		if strings.Contains(path, indicator) {
			score++
			break
		}
	}

	if isValidJSONBody(entry.ResponseBody) {
		score++
	}

	if hostMatchesBlocklist(host, blocklist) {
		score -= 3
	}

	for _, prefix := range []string{"image/", "text/css", "text/html", "application/javascript", "font/"} {
		if strings.HasPrefix(responseType, prefix) {
			score -= 2
			break
		}
	}

	for _, suffix := range []string{".js", ".css", ".png", ".jpg", ".woff", ".svg", ".ico"} {
		if strings.HasSuffix(urlLower, suffix) {
			score--
			break
		}
	}

	return score
}

func isTelemetryEntry(entry EnrichedEntry) bool {
	host := strings.ToLower(extractHost(entry.URL))
	path := strings.ToLower(extractPath(entry.URL))
	if telemetryHostMatches(host) {
		return true
	}
	return pathMatchesTelemetry(path) && telemetryQueryMatches(entry.URL)
}

func telemetryHostMatches(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return hostMatchesBlocklist(host, telemetryHosts) || strings.HasSuffix(host, "-datadoghq.com")
}

func entriesContainTelemetry(entries []EnrichedEntry) bool {
	return slices.ContainsFunc(entries, isTelemetryEntry)
}

func pathMatchesTelemetry(path string) bool {
	if path == "" {
		return false
	}
	for _, marker := range telemetryPathMarkers {
		if strings.Contains(path, marker) {
			return true
		}
	}
	return path == "/rum" || strings.Contains(path, "/rum/")
}

func telemetryQueryMatches(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	query := parsed.Query()
	for _, key := range telemetryQueryKeys {
		if _, ok := query[key]; ok {
			return true
		}
	}
	return false
}

func selectPrimaryAPIEntries(entries []EnrichedEntry) ([]EnrichedEntry, []EnrichedEntry) {
	if len(entries) == 0 {
		return entries, nil
	}

	primaryHost, counts := primaryHostByFrequency(entries)
	if primaryHost == "" {
		return entries, nil
	}
	if len(counts) <= 1 {
		return entries, nil
	}

	primaryCount := counts[primaryHost]
	primary := make([]EnrichedEntry, 0, primaryCount)
	secondary := make([]EnrichedEntry, 0, len(entries)-primaryCount)
	for _, entry := range entries {
		if strings.EqualFold(normalizedURLHost(entry.URL), primaryHost) {
			primary = append(primary, entry)
			continue
		}
		secondary = append(secondary, entry)
	}

	return primary, secondary
}

func primaryHostByFrequency(entries []EnrichedEntry) (string, map[string]int) {
	counts := map[string]int{}
	order := make([]string, 0)
	for _, entry := range entries {
		host := normalizedURLHost(entry.URL)
		if host == "" {
			continue
		}
		if counts[host] == 0 {
			order = append(order, host)
		}
		counts[host]++
	}

	primaryHost := ""
	primaryCount := 0
	tied := false
	for _, host := range order {
		switch {
		case counts[host] > primaryCount:
			primaryHost = host
			primaryCount = counts[host]
			tied = false
		case counts[host] == primaryCount:
			tied = true
		}
	}
	if tied {
		return "", counts
	}
	return primaryHost, counts
}

func secondaryHostsForEntries(apiEntries []EnrichedEntry, noiseEntries []EnrichedEntry, primaryHost string) []SecondaryHost {
	primaryHost = strings.ToLower(strings.TrimSpace(primaryHost))
	if primaryHost == "" {
		return nil
	}

	counts := map[string]int{}
	reasons := map[string]SecondaryHostReason{}
	for _, entry := range apiEntries {
		host := normalizedURLHost(entry.URL)
		if host == "" || host == primaryHost {
			continue
		}
		counts[host]++
		reasons[host] = SecondaryHostReasonNonPrimary
	}
	for _, entry := range noiseEntries {
		if !isTelemetryEntry(entry) {
			continue
		}
		host := normalizedURLHost(entry.URL)
		if host == "" || host == primaryHost {
			continue
		}
		counts[host]++
		if _, exists := reasons[host]; !exists {
			reasons[host] = SecondaryHostReasonTelemetry
		}
	}
	if len(counts) == 0 {
		return nil
	}

	hosts := make([]string, 0, len(counts))
	for host := range counts {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	out := make([]SecondaryHost, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, SecondaryHost{
			Host:   host,
			Count:  counts[host],
			Reason: reasons[host],
		})
	}
	return out
}

func normalizedURLHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Hostname() != "" {
		return strings.ToLower(parsed.Hostname())
	}
	if !strings.Contains(rawURL, "://") {
		return ""
	}
	return strings.ToLower(extractHost(rawURL))
}

func getHeaderValue(headers map[string]string, want string) string {
	for key, value := range headers {
		if strings.EqualFold(key, want) {
			return value
		}
	}

	return ""
}

func isValidJSONBody(body string) bool {
	if strings.TrimSpace(body) == "" {
		return false
	}

	var payload any
	return json.Unmarshal([]byte(body), &payload) == nil
}

func hostMatchesBlocklist(host string, blocklist []string) bool {
	if host == "" {
		return false
	}

	for _, blocked := range blocklist {
		blocked = strings.ToLower(blocked)
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}

	return false
}

func extractHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Host != "" {
		return parsed.Hostname()
	}

	host := rawURL
	if idx := strings.Index(host, "://"); idx >= 0 {
		host = host[idx+3:]
	}
	if idx := strings.IndexAny(host, "/?"); idx >= 0 {
		host = host[:idx]
	}

	host, _, err = net.SplitHostPort(host)
	if err == nil {
		return host
	}

	return host
}

func extractPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Path != "" {
		return parsed.Path
	}

	path := rawURL
	if idx := strings.Index(path, "://"); idx >= 0 {
		path = path[idx+3:]
		if slash := strings.Index(path, "/"); slash >= 0 {
			path = path[slash:]
		} else {
			return "/"
		}
	}
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return path
}

func normalizeEntryPath(rawURL string) string {
	normalized, _ := normalizeEntryPathWithHints(rawURL)
	return normalized
}

func normalizeEntryPathWithHints(rawURL string) (string, []LowConfidenceParameter) {
	path := extractPath(rawURL)
	segments := strings.Split(path, "/")
	codeRun := compactCodeRunPositions(segments)
	// Track per-path placeholder name use so consecutive IDs that resolve to
	// the same parent (e.g. /resources/123/456) don't both emit
	// {resource_id}; the second collision gets a counter suffix.
	nameCounts := make(map[string]int)
	lowConfidence := make([]LowConfidenceParameter, 0)
	compactIndex := 0
	for i, segment := range segments {
		if segment == "" {
			continue
		}
		// Skip framing segments (api, /v1) so the placeholder name reflects the
		// resource the ID belongs to, not the version prefix.
		parent := previousMeaningfulSegment(segments, i)
		var placeholder string
		var confidenceReason string
		switch {
		case isoDatePattern.MatchString(segment):
			placeholder = "{date}"
			confidenceReason = "single-sample ISO date path segment"
		case codeRun[i]:
			placeholder = "{segment_" + strconv.Itoa(compactIndex) + "}"
			compactIndex++
			confidenceReason = "single-sample compact uppercase code path segment"
		case numericPattern.MatchString(segment):
			placeholder = idPlaceholder(parent, "id")
			if len(segment) <= 4 {
				confidenceReason = "single-sample small integer path segment"
			}
		case uuidSegmentPattern.MatchString(segment):
			placeholder = idPlaceholder(parent, "uuid")
		case hashSegmentPattern.MatchString(segment):
			placeholder = idPlaceholder(parent, "hash")
		case prefixedIDPattern.MatchString(segment):
			placeholder = idPlaceholder(parent, "id")
		case colonCompositePattern.MatchString(segment) && hasNonTrivialToken(segment):
			placeholder = idPlaceholder(parent, "id")
		case longAlnumIDPattern.MatchString(segment) && looksOpaqueID(segment):
			placeholder = idPlaceholder(parent, "id")
		default:
			continue
		}
		segments[i] = disambiguatePlaceholder(placeholder, nameCounts)
		if confidenceReason != "" {
			lowConfidence = append(lowConfidence, LowConfidenceParameter{
				Name:     strings.TrimSuffix(strings.TrimPrefix(segments[i], "{"), "}"),
				Segment:  segment,
				Position: pathSegmentPosition(segments, i),
				Reason:   confidenceReason,
			})
		}
	}

	normalized := strings.Join(segments, "/")
	if normalized == "" {
		return "/", lowConfidence
	}

	return normalized, lowConfidence
}

func compactCodeRunPositions(segments []string) map[int]bool {
	out := map[int]bool{}
	for i := 0; i < len(segments); {
		segment := segments[i]
		if segment == "" {
			i++
			continue
		}
		if !shortCodePattern.MatchString(segment) {
			i++
			continue
		}
		start := i
		for i < len(segments) && shortCodePattern.MatchString(segments[i]) {
			i++
		}
		if i-start < 2 {
			continue
		}
		for pos := start; pos < i; pos++ {
			out[pos] = true
		}
	}
	return out
}

func pathSegmentPosition(segments []string, index int) int {
	position := 0
	for i := 0; i <= index && i < len(segments); i++ {
		if segments[i] == "" {
			continue
		}
		if i == index {
			return position
		}
		position++
	}
	return position
}

// disambiguatePlaceholder appends a counter suffix when the same placeholder
// name has already appeared earlier in the current path. For
// /resources/123/456 the first segment yields {resource_id}; the second walks
// back past the freshly-emitted placeholder, sees `resources` again, and would
// emit a second {resource_id} — instead it becomes {resource_id_2}. This keeps
// downstream spec generation safe: OpenAPI rejects duplicate path-parameter
// names within a single path template.
//
// The `used` map tracks every concrete placeholder string already emitted in
// the current path (including suffixed variants). The walk advances the
// counter as long as the candidate name collides, so chains like
// {resource_id}/{resource_id_2}/{resource_id_3} stay unique even when the
// variance pass populates `used` from a path the per-segment normalizer
// already disambiguated.
func disambiguatePlaceholder(placeholder string, used map[string]int) string {
	if used[placeholder] == 0 {
		used[placeholder] = 1
		return placeholder
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(placeholder, "{"), "}")
	// Find the next free suffix. Start at 2 (the first collision becomes _2)
	// and advance until the candidate isn't already in `used`.
	for n := 2; ; n++ {
		candidate := "{" + inner + "_" + strconv.Itoa(n) + "}"
		if used[candidate] == 0 {
			used[candidate] = 1
			return candidate
		}
	}
}

// previousMeaningfulSegment walks backwards from index i looking for a segment
// that isn't empty, a placeholder, or a routing framing segment (api, vN). The
// placeholder name (table_id, widget_id) reflects the resource the ID belongs
// to, not the version prefix it happens to live under.
func previousMeaningfulSegment(segments []string, i int) string {
	for j := i - 1; j >= 0; j-- {
		s := segments[j]
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			continue
		}
		if s == "api" || isVersionSegment(s) {
			continue
		}
		return s
	}
	return ""
}

// isVersionSegment is the local mirror of discovery.isVersionSegment kept here
// to avoid a package import cycle through the discovery package's spec import.
func isVersionSegment(segment string) bool {
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

// idPlaceholder returns "{parent_id}" when parent is a useful resource segment
// (drops trailing 's', forces snake_case), falling back to "{fallback}" when
// the parent is empty or not safely Go-identifier shaped.
func idPlaceholder(parent string, fallback string) string {
	name := placeholderNameFromParent(parent)
	if name == "" {
		return "{" + fallback + "}"
	}
	return "{" + name + "_id}"
}

func placeholderNameFromParent(parent string) string {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return ""
	}
	// Normalize to snake_case so "user-groups" -> "user_group_id" and an
	// already-singular parent like "user" stays "user_id". Reject parents that
	// aren't safely shaped as a Go identifier root (e.g. all-digit, empty).
	lower := strings.ToLower(parent)
	lower = strings.ReplaceAll(lower, "-", "_")
	for _, r := range lower {
		if r == '_' {
			continue
		}
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		return ""
	}
	// Drop a trailing 's' on plurals; keep "status"/"address" intact via simple
	// "ss" guard. Mirrors the conservative singularizer used elsewhere; complex
	// irregulars (entries -> entry) are left to downstream tooling that owns
	// command naming.
	if strings.HasSuffix(lower, "ies") && len(lower) > 3 {
		lower = lower[:len(lower)-3] + "y"
	} else if strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") && len(lower) > 1 {
		lower = lower[:len(lower)-1]
	}
	return lower
}

// hasNonTrivialToken guards colon-composite detection: at least one
// colon-separated token must have 3+ chars so plain port-style values like
// "host:80" aren't mistaken for composite IDs.
func hasNonTrivialToken(segment string) bool {
	for token := range strings.SplitSeq(segment, ":") {
		if len(token) >= 3 {
			return true
		}
	}
	return false
}

// looksLikeIDShape returns true when a segment matches any of the strong ID
// heuristics (UUID, hex hash, numeric, prefixed application id, colon
// composite, or long opaque alphanumeric). These are the same shapes the
// per-segment normalizer recognizes; kept as a named helper so future variance
// callers can ask the question without inlining the regex list.
func looksLikeIDShape(segment string) bool {
	if numericPattern.MatchString(segment) {
		return true
	}
	if uuidSegmentPattern.MatchString(segment) {
		return true
	}
	if hashSegmentPattern.MatchString(segment) {
		return true
	}
	if prefixedIDPattern.MatchString(segment) {
		return true
	}
	if colonCompositePattern.MatchString(segment) && hasNonTrivialToken(segment) {
		return true
	}
	if longAlnumIDPattern.MatchString(segment) && looksOpaqueID(segment) {
		return true
	}
	return false
}

// looksParameterizable is the weaker gate used by the cross-entry variance
// pass. By construction, any segment that satisfies looksLikeIDShape would
// already have been replaced by normalizeEntryPath, so gating on that shape
// alone makes the variance pass unreachable. The pass exists to catch IDs
// that the per-segment patterns can't classify in isolation — short opaque
// tokens like `abc123` or `xyz456` that only stand out as IDs when two
// entries land at the same position with different literal values.
//
// The widening is deliberately narrow: presence of a digit. Pure mixed-case
// alone (`hasUpper && hasLower`) is not enough — PascalCase is a normal
// shape for action-style REST routes (`/api/CreateDocument` vs
// `/api/ListDocuments`, common in ASP.NET / gRPC-HTTP transcoding), and a
// pure-letter mixed-case check would merge those into `/api/{id}`. The
// short-opaque IDs the variance pass exists to catch all carry at least one
// digit; the rare digit-free opaque ID is left for users to parametrize
// manually rather than risk destroying real route names.
func looksParameterizable(segment string) bool {
	if looksLikeIDShape(segment) {
		return true
	}
	for _, r := range segment {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// looksOpaqueID applies the mixed-case-or-digit floor for the long-alphanumeric
// shape so route literals like "subscriptions" (all-lowercase, no digits) aren't
// flagged. A long segment carrying any digit, or mixing case, is treated as an
// opaque application ID.
func looksOpaqueID(segment string) bool {
	hasDigit := false
	hasUpper := false
	hasLower := false
	for _, r := range segment {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		}
	}
	return hasDigit || (hasUpper && hasLower)
}

// collapseVariantGroups folds endpoint groups whose paths differ in exactly
// one segment into a single parameterized group. Two HAR captures of
// /widgets/abc-a and /widgets/abc-b produce two raw groups; the variance pass
// detects they share the same method and identical path prefix/suffix, and
// merges them into /widgets/{widget_id}. Only runs when neither path already
// carries a placeholder at the diverging segment (preserves explicit hits from
// the per-segment normalizer).
func collapseVariantGroups(groups []EndpointGroup) []EndpointGroup {
	if len(groups) < 2 {
		return groups
	}

	// Bucket by (host, method, segment count, positions of existing
	// placeholders) so only same-shape paths from the same host can collapse
	// together. Two groups from different hosts must stay separate even when
	// their path shapes coincide — DeduplicateTrafficEndpoints keys by host
	// upstream, and the variance pass must preserve that separation.
	type skeletonKey struct {
		host        string
		method      string
		length      int
		placeholder string // bitmask of positions already holding placeholders
	}
	buckets := make(map[skeletonKey][]int)
	skeletons := make(map[int][]string)
	for idx, group := range groups {
		segments := strings.Split(group.NormalizedPath, "/")
		skeletons[idx] = segments
		var placeholderMask strings.Builder
		for _, s := range segments {
			if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
				placeholderMask.WriteByte('1')
			} else {
				placeholderMask.WriteByte('0')
			}
		}
		key := skeletonKey{
			host:        group.Host,
			method:      group.Method,
			length:      len(segments),
			placeholder: placeholderMask.String(),
		}
		buckets[key] = append(buckets[key], idx)
	}

	// Map from group index to merge-target index. Targets stay; non-targets get
	// merged into the target and dropped from output.
	mergeInto := make(map[int]int)

	for _, members := range buckets {
		if len(members) < 2 {
			continue
		}
		segLen := len(skeletons[members[0]])

		// Find positions that vary across the bucket. We only promote one
		// position per bucket pass; if multiple positions vary, the paths are
		// genuinely different resources and shouldn't collapse.
		varyingPositions := make([]int, 0)
		for pos := range segLen {
			seen := make(map[string]struct{})
			for _, idx := range members {
				seen[skeletons[idx][pos]] = struct{}{}
				if len(seen) > 1 {
					break
				}
			}
			if len(seen) > 1 {
				varyingPositions = append(varyingPositions, pos)
			}
		}
		if len(varyingPositions) != 1 {
			continue
		}
		pos := varyingPositions[0]

		// The diverging segment must look like a parameter candidate at every
		// member: not a placeholder (filtered already), not a routing keyword.
		// Use the weaker looksParameterizable check (digit present, mixed case,
		// or strong-ID shape) rather than looksLikeIDShape — by construction
		// any strong-ID-shaped segment was already replaced by
		// normalizeEntryPath, so gating on that alone makes the pass
		// unreachable. The looksParameterizable widening catches short opaque
		// tokens (`abc123` vs `xyz456`) that only stand out as IDs once two
		// entries land at the same position with different values, while still
		// rejecting plain route literals like `health`/`version`.
		anyParameterizable := false
		allParameterizable := true
		for _, idx := range members {
			s := skeletons[idx][pos]
			if s == "" || s == "api" || isVersionSegment(s) {
				allParameterizable = false
				continue
			}
			if looksParameterizable(s) {
				anyParameterizable = true
			} else {
				allParameterizable = false
			}
		}
		if !anyParameterizable || !allParameterizable {
			continue
		}

		// Pick the lowest-index member as the merge target and rewrite its
		// path. Other members fold into it.
		target := members[0]
		for _, idx := range members[1:] {
			if idx < target {
				target = idx
			}
		}
		parent := previousMeaningfulSegment(skeletons[target], pos)
		placeholder := idPlaceholder(parent, "id")
		newSegments := append([]string(nil), skeletons[target]...)
		// Build the in-path placeholder use count from the existing segments so
		// the variance-pass emission disambiguates against placeholders the
		// per-segment normalizer already placed. Without this, a path like
		// /resources/{resource_id}/AbcDef would have its variance position
		// resolved by walking back through the existing {resource_id} to find
		// `resources` again, producing a duplicate `{resource_id}`.
		used := make(map[string]int)
		for _, s := range newSegments {
			if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
				used[s]++
			}
		}
		newSegments[pos] = disambiguatePlaceholder(placeholder, used)
		groups[target].NormalizedPath = strings.Join(newSegments, "/")

		for _, idx := range members {
			if idx == target {
				continue
			}
			mergeInto[idx] = target
		}
	}

	if len(mergeInto) == 0 {
		return groups
	}

	// Two-pass to keep the merge order deterministic: first fold every merged
	// group's Entries into its target in source-index order (map iteration is
	// random and would scramble Entries between runs), then emit survivors in
	// original order.
	sourceIdxs := make([]int, 0, len(mergeInto))
	for idx := range mergeInto {
		sourceIdxs = append(sourceIdxs, idx)
	}
	sort.Ints(sourceIdxs)
	for _, idx := range sourceIdxs {
		target := mergeInto[idx]
		groups[target].Entries = append(groups[target].Entries, groups[idx].Entries...)
	}
	out := make([]EndpointGroup, 0, len(groups)-len(mergeInto))
	for idx := range groups {
		if _, merged := mergeInto[idx]; merged {
			continue
		}
		out = append(out, groups[idx])
	}
	return out
}

package browsersniff

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type EndpointGroup struct {
	Method         string
	NormalizedPath string
	Entries        []EnrichedEntry
}

var (
	uuidSegmentPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	hashSegmentPattern = regexp.MustCompile(`(?i)^[0-9a-f]{32,}$`)
	numericPattern     = regexp.MustCompile(`^\d+$`)
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
	return []string{
		"google-analytics.com",
		"doubleclick.net",
		"sentry.io",
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
		"segment.io",
		"api.segment.io",
		"mixpanel.com",
		"amplitude.com",
		"hotjar.com",
		"newrelic.com",
		"fullstory.com",
		"intercom.io",
		"branch.io",
		"stats.g.doubleclick.net",
		"adservice.google.com",
		"connect.facebook.net",
	}
}

func DeduplicateEndpoints(entries []EnrichedEntry) []EndpointGroup {
	groups := make([]EndpointGroup, 0)
	indexByKey := make(map[string]int)

	for _, entry := range entries {
		method := strings.ToUpper(strings.TrimSpace(entry.Method))
		normalizedPath := normalizeEntryPath(entry.URL)
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
	path := extractPath(rawURL)
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if segment == "" {
			continue
		}
		// Skip framing segments (api, /v1) so the placeholder name reflects the
		// resource the ID belongs to, not the version prefix.
		parent := previousMeaningfulSegment(segments, i)
		switch {
		case numericPattern.MatchString(segment):
			segments[i] = idPlaceholder(parent, "id")
		case uuidSegmentPattern.MatchString(segment):
			segments[i] = idPlaceholder(parent, "uuid")
		case hashSegmentPattern.MatchString(segment):
			segments[i] = idPlaceholder(parent, "hash")
		case prefixedIDPattern.MatchString(segment):
			segments[i] = idPlaceholder(parent, "id")
		case colonCompositePattern.MatchString(segment) && hasNonTrivialToken(segment):
			segments[i] = idPlaceholder(parent, "id")
		case longAlnumIDPattern.MatchString(segment) && looksOpaqueID(segment):
			segments[i] = idPlaceholder(parent, "id")
		}
	}

	normalized := strings.Join(segments, "/")
	if normalized == "" {
		return "/"
	}

	return normalized
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
// composite, or long opaque alphanumeric). Used by the cross-entry variance
// pass to gate parametrization: two route literals that just happen to differ
// (e.g. /api/health vs /api/version) won't be flagged because neither
// segment fits any of these shapes.
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

	// Bucket by (method, segment count, positions of existing placeholders) so
	// only same-shape paths can collapse together. Group all candidates that
	// share the same skeleton and differ in literal positions; for each
	// position with >=2 distinct literals across the bucket, promote it.
	type skeletonKey struct {
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
		// Conservative: require at least one member's value to match one of the
		// strong ID heuristics. Cross-entry variance alone (two literals at the
		// same position) isn't enough to parametrize, because two distinct route
		// names (e.g. /api/health vs /api/version) also vary without being IDs.
		anyOpaque := false
		for _, idx := range members {
			s := skeletons[idx][pos]
			if s == "" || s == "api" || isVersionSegment(s) {
				continue
			}
			if looksLikeIDShape(s) {
				anyOpaque = true
				break
			}
		}
		if !anyOpaque {
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
		newSegments[pos] = placeholder
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

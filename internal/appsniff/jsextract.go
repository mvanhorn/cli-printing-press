package appsniff

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var (
	// Absolute HTTPS API URLs embedded in bundles or binary strings.
	reAbsHTTPSURL = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}(?::\d{2,5})?(?:/[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%{}-]*)?`)

	// axios.post("...") and similar HTTP client method calls.
	reAxiosMethod    = regexp.MustCompile(`\b(?:axios|http|api|client|request|ky|got|superagent)\.(get|post|put|patch|delete|head)\s*\(\s*["'\x60]([^"'\x60\n]{2,240})["'\x60]`)
	reFetchStart     = regexp.MustCompile(`\bfetch\s*\(\s*(["'\x60])([^"'\x60\n]{2,240})["'\x60]`)
	reMethodProp     = regexp.MustCompile(`(?i)\bmethod\s*:\s*["'](GET|POST|PUT|PATCH|DELETE|HEAD)["']`)
	reBodyKeysObj    = regexp.MustCompile(`(?:JSON\.stringify|body|data)\s*[:(]\s*\{([^{}]{1,500})\}`)
	reJSObjectKey    = regexp.MustCompile(`(?:^|[,{\s])["']?([a-zA-Z_][a-zA-Z0-9_-]{1,40})["']?\s*:`)
	reHeaderNameProp = regexp.MustCompile(`["']((?:x-[a-z0-9-]+|authorization|x-api-key|x-csrf-token|x-xsrf-token|x-client-version|x-app-version))["']\s*:`)

	// Relative REST/API route strings inside JS bundles or DEX strings.
	reRelativeAPIPath = regexp.MustCompile(`["'\x60](/(?:api|v[0-9]+(?:\.[0-9]+)?|graphql|trpc|rpc|oauth2?|auth|users?|me|account|sessions?|devices?|items?|orders?|rides?|trips?|bookings?|messages?|channels?|notes?|documents?|meetings?|workspaces?|teams?|search|events?|webhooks?)(?:/[A-Za-z0-9._~{}:$\[\]-]+)*)["'\x60]`)

	// Retrofit-style unslashed versioned paths (e.g. "v1/users/{userId}/sleep").
	reRetrofitRelPath = regexp.MustCompile(`^(?:api/)?v[0-9]+(?:\.[0-9]+)?/[A-Za-z0-9._~{}:-][A-Za-z0-9._~/{}:-]*$`)

	// Slack/RPC-style dot methods: api.call("bookmarks.list") or "/api/conversations.history".
	reDotRPCCall = regexp.MustCompile(`(?:api\.call|callApiMethod|slackApi|rpcCall)\s*\(\s*["']([a-z][a-z0-9_]*\.[a-z][a-zA-Z0-9_.-]{1,60})["']`)
	reDotRPCPath = regexp.MustCompile(`/api/([a-z][a-z0-9_]*\.[a-z][a-zA-Z0-9_.-]{1,60})\b`)

	// GraphQL operation names.
	reGraphQLOp = regexp.MustCompile(`\b(query|mutation)\s+([A-Z][A-Za-z0-9_]{2,64})\s*[({]`)

	// tRPC paths.
	reTRPCPath = regexp.MustCompile(`/trpc/([a-zA-Z0-9_.-]+(?:\.[a-zA-Z0-9_-]+)+)`)

	// Shard map entries like: pecan: "https://pecan.api.example.com" or "maple.api.example.com".
	reShardAssignment = regexp.MustCompile(`["']?([a-z][a-z0-9_-]{1,24})["']?\s*:\s*["'](https://[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})[^"']*["']`)

	// Template interpolation inside paths: ${userId} or :userId -> {userId}.
	reTemplateExpr = regexp.MustCompile(`\$\{([^}]{1,64})\}`)
	reColonParam   = regexp.MustCompile(`/:([a-zA-Z_][a-zA-Z0-9_]*)`)

	// Protobuf / gRPC service path: /pkg.sub.v1.ServiceName/MethodName or pb.api.*
	reGRPCPath = regexp.MustCompile(`^/?(?:(?:[a-z][a-z0-9_]*\.){2,}[A-Za-z0-9_]+/[A-Za-z0-9_]+|pb\.api\.[A-Za-z0-9_./]+)$`)

	// OAuth / token endpoint hints.
	reTokenURL = regexp.MustCompile(`(?i)/(?:oauth2?/token|v\d+/tokens?|auth/token|auth/login|login/oauth)`)

	// Pinning references in JS/DEX/native strings.
	rePinningToken = regexp.MustCompile(`(?i)\b(CertificatePinner|sha256/[A-Za-z0-9+/=]{20,}|TrustKit|SSLPinningMode|pin-set)\b`)

	// Noisy/non-API host suffixes & prefixes.
	ignoredHosts = []string{
		"w3.org",
		"schemas.android.com",
		"schemas.openxmlformats.org",
		"xmlpull.org",
		"apple.com",
		"icloud.com",
		"mozilla.org",
		"mozilla.com",
		"chromium.org",
		"github.com",
		"raw.githubusercontent.com",
		"npmjs.org",
		"npmjs.com",
		"unpkg.com",
		"cdnjs.cloudflare.com",
		"cdn.jsdelivr.net",
		"fonts.googleapis.com",
		"fonts.gstatic.com",
		"play.google.com",
		"apps.apple.com",
		"itunes.apple.com",
		"developer.apple.com",
		"developer.android.com",
		"firebase.google.com",
		"firebaseio.com",
		"crashlytics.com",
		"app-measurement.com",
		"google-analytics.com",
		"googletagmanager.com",
		"doubleclick.net",
		"googlesyndication.com",
		"sentry.io",
		"ingest.sentry.io",
		"datadoghq.com",
		"browser-intake-datadoghq.com",
		"segment.io",
		"segment.com",
		"api.segment.io",
		"mixpanel.com",
		"api.mixpanel.com",
		"amplitude.com",
		"api.amplitude.com",
		"api2.amplitude.com",
		"branch.io",
		"app.link",
		"test-app.link",
		"appsflyer.com",
		"adjust.com",
		"onesignal.com",
		"braze.com",
		"intercom.io",
		"zendesk.com",
		"launchdarkly.com",
		"statsig.com",
		"statsigapi.net",
		"bugsnag.com",
		"newrelic.com",
		"facebook.com",
		"graph.facebook.com",
		"connect.facebook.net",
		"twitter.com",
		"x.com",
		"linkedin.com",
		"instagram.com",
		"youtube.com",
		"youtu.be",
		"vimeo.com",
		"cloudflare.com",
		"akamaihd.net",
		"cloudfront.net",
		"amazonaws.com",
		"s3.amazonaws.com",
		"storage.googleapis.com",
		"example.com",
		"example.org",
		"example.net",
		"localhost",
		"127.0.0.1",
		"0.0.0.0",
		"reactjs.org",
		"react.dev",
		"nodejs.org",
		"typescriptlang.org",
		"webkit.org",
		"ecma-international.org",
		"ietf.org",
		"rfc-editor.org",
		"json-schema.org",
		"apache.org",
		"opensource.org",
		"creativecommons.org",
		"stripe.com",
		"js.stripe.com",
		"checkout.stripe.com",
		"maps.googleapis.com",
		"maps.google.com",
		"recaptcha.net",
		"gstatic.com",
		"hcaptcha.com",
		"google.com",
		"googleapis.com",
		"iterable.com",
		"sprig.com",
		"userleap.com",
		"braintreegateway.com",
		"paypal.com",
		"singular.net",
		"mapbox.com",
		"klaviyo.com",
		"opentelemetry.io",
		"prodregistryv2.org",
		"featureassets.org",
		"kustomer.help",
		"zapier.com",
		"acculynk.net",
		"cloudflare-dns.com",
		"aomedia.org",
		"nocookie.net",
		"weixin.qq.com",
		"twilio.com",
		"plaid.com",
		"sendgrid.com",
		"algolia.net",
		"algolianet.com",
		"cloudinary.com",
		"optimizely.com",
		"fullstory.com",
		"hotjar.com",
		"pendo.io",
		"qualtrics.com",
		"customer.io",
		"pusher.com",
		"pubnub.com",
	}

	staticAssetExts = []string{
		".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp", ".avif",
		".css", ".scss", ".less", ".woff", ".woff2", ".ttf", ".eot", ".otf",
		".mp3", ".mp4", ".wav", ".ogg", ".webm", ".mov", ".avi",
		".pdf", ".zip", ".tar", ".gz", ".dmg", ".exe", ".apk", ".ipa",
		".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".map", ".wasm",
		".html", ".htm", ".xml", ".dtd", ".xsd", ".txt", ".md",
	}
)

type extractorAccumulator struct {
	endpoints               map[string]*Endpoint
	grpcEndpoints           map[string]*Endpoint
	baseURLCounts           map[string]int
	baseURLSource           map[string]string
	shards                  map[string]ShardRoute
	authHeaders             map[string]bool
	authCookies             map[string]bool
	tokenURLs               map[string]bool
	graphqlOps              map[string]bool
	hasBearer               bool
	hasOAuth                bool
	hasAPIKey               bool
	hasCookie               bool
	hasBasic                bool
	pinning                 []PinningFinding
	pinningSeen             map[string]bool
	warnings                []string
	warningSeen             map[string]bool
	exactMethodByOriginPath map[string]string
	exactMethodHostless     map[string]string
}

func newExtractorAccumulator() *extractorAccumulator {
	return &extractorAccumulator{
		endpoints:               make(map[string]*Endpoint),
		grpcEndpoints:           make(map[string]*Endpoint),
		baseURLCounts:           make(map[string]int),
		baseURLSource:           make(map[string]string),
		shards:                  make(map[string]ShardRoute),
		authHeaders:             make(map[string]bool),
		authCookies:             make(map[string]bool),
		tokenURLs:               make(map[string]bool),
		graphqlOps:              make(map[string]bool),
		pinningSeen:             make(map[string]bool),
		warningSeen:             make(map[string]bool),
		exactMethodByOriginPath: make(map[string]string),
		exactMethodHostless:     make(map[string]string),
	}
}

func (a *extractorAccumulator) addWarning(msg string) {
	msg = RedactString(strings.TrimSpace(msg))
	if msg == "" || a.warningSeen[msg] {
		return
	}
	a.warningSeen[msg] = true
	a.warnings = append(a.warnings, msg)
}

func (a *extractorAccumulator) addPinning(p PinningFinding) {
	// Deduplicate noisy per-method references (e.g. multiple OkHttp
	// CertificatePinner signatures in DEX) to one clean finding per kind+domains.
	key := p.Kind + "|" + strings.Join(p.Domains, ",")
	if a.pinningSeen[key] {
		return
	}
	a.pinningSeen[key] = true
	a.pinning = append(a.pinning, p)
}

func (a *extractorAccumulator) addBaseURL(raw string) {
	a.addBaseURLWithWeight(raw, 1, "")
}

func (a *extractorAccumulator) addBaseURLWithWeight(raw string, weight int, source string) {
	origin := normalizeOrigin(raw)
	if origin == "" {
		return
	}
	if weight <= 0 {
		weight = 1
	}
	a.baseURLCounts[origin] += weight
	if source != "" && a.baseURLSource[origin] == "" {
		a.baseURLSource[origin] = source
	}
}

func (a *extractorAccumulator) addShard(name, rawURL, source string) {
	origin := normalizeOrigin(rawURL)
	if origin == "" {
		return
	}
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return
	}
	host := strings.ToLower(u.Hostname())
	cleanName := strings.ToLower(strings.TrimSpace(name))
	if cleanName == "" {
		parts := strings.Split(host, ".")
		if len(parts) > 0 {
			cleanName = parts[0]
		}
	}
	key := cleanName + "|" + host
	if _, ok := a.shards[key]; !ok {
		a.shards[key] = ShardRoute{
			Name:   cleanName,
			Host:   host,
			Source: source,
		}
	}
	a.addBaseURLWithWeight(origin, 2, source)
}

func (a *extractorAccumulator) addEndpoint(ep Endpoint) {
	ep.Method = strings.ToUpper(strings.TrimSpace(ep.Method))
	if ep.Method == "" {
		ep.Method = "GET"
	}
	ep.Path = normalizeEndpointPath(ep.Path)
	if ep.Path == "" {
		return
	}
	if ep.Protocol == "" {
		ep.Protocol = classifyProtocol(ep.Path)
	}
	if ep.Protocol != ProtocolGRPC && isStaticAssetPath(ep.Path) {
		return
	}
	if ep.BaseURL != "" {
		ep.BaseURL = normalizeOrigin(ep.BaseURL)
		if u, err := url.Parse(ep.BaseURL); err == nil && u.Hostname() != "" {
			if isIgnoredHost(strings.ToLower(u.Hostname())) {
				return
			}
			ep.Host = strings.ToLower(u.Host)
			a.addBaseURL(ep.BaseURL)
		}
	} else if ep.Host != "" {
		ep.Host = strings.ToLower(strings.TrimSpace(ep.Host))
		hostOnly := ep.Host
		if idx := strings.IndexByte(hostOnly, ':'); idx >= 0 {
			hostOnly = hostOnly[:idx]
		}
		if isIgnoredHost(hostOnly) {
			return
		}
		ep.BaseURL = "https://" + ep.Host
	}
	if ep.Host != "" {
		hostOnly := ep.Host
		if idx := strings.IndexByte(hostOnly, ':'); idx >= 0 {
			hostOnly = hostOnly[:idx]
		}
		if isIgnoredHost(hostOnly) {
			return
		}
	}
	if reTokenURL.MatchString(ep.Path) {
		tokenURL := ep.Path
		if ep.BaseURL != "" {
			tokenURL = ep.BaseURL + ep.Path
		}
		a.tokenURLs[SanitizeURL(tokenURL)] = true
		if strings.Contains(strings.ToLower(ep.Path), "oauth") {
			a.hasOAuth = true
		} else {
			a.hasBearer = true
		}
	}

	// Auto-populate path params from {param} placeholders.
	ep.Params = mergeParams(ep.Params, extractPathParams(ep.Path))

	targetMap := a.endpoints
	if ep.Protocol == ProtocolGRPC {
		targetMap = a.grpcEndpoints
	}

	originKey := ep.BaseURL
	key := ep.Method + " " + originKey + " " + ep.Path

	// Generic URL/string scans default to GET when no HTTP verb is known.
	// Do not invent a GET if an exact call-site method already exists for the
	// same origin+path (or for a hostless entry when this entry's origin is
	// unknown), and never let one host's method hide a distinct host's route.
	isUnknownMethodGuess := ep.Method == "GET" && ep.Confidence != ConfidenceHigh
	if isUnknownMethodGuess {
		if originKey != "" {
			if m, ok := a.exactMethodByOriginPath[originKey+" "+ep.Path]; ok && m != "GET" {
				return
			}
		} else {
			if m, ok := a.exactMethodHostless[ep.Path]; ok && m != "GET" {
				return
			}
		}
	}
	if ep.Confidence == ConfidenceHigh {
		if originKey != "" {
			a.exactMethodByOriginPath[originKey+" "+ep.Path] = ep.Method
		} else {
			a.exactMethodHostless[ep.Path] = ep.Method
		}
		if ep.Method != "GET" {
			if originKey != "" {
				guessKey := "GET " + originKey + " " + ep.Path
				if prev, ok := targetMap[guessKey]; ok && prev.Confidence != ConfidenceHigh {
					delete(targetMap, guessKey)
				}
			} else {
				guessKey := "GET  " + ep.Path
				if prev, ok := targetMap[guessKey]; ok && prev.Confidence != ConfidenceHigh {
					delete(targetMap, guessKey)
				}
			}
		}
	}

	if existing, ok := targetMap[key]; ok {
		if confidenceRank(ep.Confidence) > confidenceRank(existing.Confidence) {
			existing.Confidence = ep.Confidence
			existing.Source = ep.Source
		}
		if existing.BaseURL == "" && ep.BaseURL != "" {
			existing.Host = ep.Host
			existing.BaseURL = ep.BaseURL
		}
		if existing.RequestContentType == "" && ep.RequestContentType != "" {
			existing.RequestContentType = ep.RequestContentType
		}
		existing.Params = mergeParams(existing.Params, ep.Params)
		existing.BodyKeys = mergeStrings(existing.BodyKeys, ep.BodyKeys)
		existing.HeaderNames = mergeStrings(existing.HeaderNames, ep.HeaderNames)
		existing.ObservedAuth = mergeStrings(existing.ObservedAuth, ep.ObservedAuth)
		existing.Notes = mergeStrings(existing.Notes, ep.Notes)
		return
	}

	cp := ep
	targetMap[key] = &cp
}

// scanJS extracts endpoints, base URLs, shards, auth hints, and headers from JS/TS/JSON content.
func (a *extractorAccumulator) scanJS(fileName, content, source string) {
	if len(content) == 0 {
		return
	}

	// 1. Auth keyword signals (header names and scheme hints only, never values).
	lower := strings.ToLower(content)
	if strings.Contains(lower, "authorization") && strings.Contains(lower, "bearer") {
		a.hasBearer = true
		a.authHeaders["authorization"] = true
	}
	if strings.Contains(lower, "x-api-key") {
		a.hasAPIKey = true
		a.authHeaders["x-api-key"] = true
	}
	if strings.Contains(lower, "x-csrf-token") || strings.Contains(lower, "x-xsrf-token") {
		a.hasCookie = true
		a.authHeaders["x-csrf-token"] = true
	}
	if strings.Contains(lower, "credentials:\"include\"") || strings.Contains(lower, "credentials: \"include\"") || strings.Contains(lower, "credentials:'include'") || strings.Contains(lower, "withcredentials:true") || strings.Contains(lower, "withcredentials: true") {
		a.hasCookie = true
	}
	if strings.Contains(lower, "oauth/token") || strings.Contains(lower, "oauth2/token") || strings.Contains(lower, "grant_type") {
		a.hasOAuth = true
	}

	// Header names found in object literals (never values).
	var fileHeaders []string
	for _, m := range reHeaderNameProp.FindAllStringSubmatch(content, 64) {
		h := strings.ToLower(strings.TrimSpace(m[1]))
		if h != "" && !slices.Contains(fileHeaders, h) {
			fileHeaders = append(fileHeaders, h)
			if isAuthHeaderName(h) {
				a.authHeaders[h] = true
			}
		}
	}

	// 2. Shard assignments.
	for _, m := range reShardAssignment.FindAllStringSubmatch(content, 128) {
		shardName := m[1]
		shardURL := m[2]
		if looksLikeShardHost(shardURL) {
			a.addShard(shardName, shardURL, source)
		}
	}

	// 3. fetch(...) calls with balanced options object for method + body keys.
	for _, fc := range scanFetchCalls(content, 512) {
		method := "GET"
		if mm := reMethodProp.FindStringSubmatch(fc.options); len(mm) == 2 {
			method = strings.ToUpper(mm[1])
		}
		bodyKeys := extractBodyKeys(fc.options)
		if len(bodyKeys) > 0 && method == "GET" {
			method = "POST"
		}
		a.addCandidateURLOrPath(method, fc.target, source, ConfidenceHigh, bodyKeys, fileHeaders, "")
	}

	// 4. axios.get/post/put/patch/delete(...) calls.
	for _, m := range reAxiosMethod.FindAllStringSubmatch(content, 512) {
		method := strings.ToUpper(m[1])
		rawTarget := m[2]
		a.addCandidateURLOrPath(method, rawTarget, source, ConfidenceHigh, nil, fileHeaders, "")
	}

	// 5. Dot-RPC calls (e.g. Slack web-client methods).
	for _, m := range reDotRPCCall.FindAllStringSubmatch(content, 512) {
		a.addEndpoint(Endpoint{
			Method:     "POST",
			Path:       "/api/" + m[1],
			Protocol:   ProtocolRPC,
			Source:     source,
			Confidence: ConfidenceHigh,
		})
	}
	for _, m := range reDotRPCPath.FindAllStringSubmatch(content, 512) {
		a.addEndpoint(Endpoint{
			Method:     "POST",
			Path:       "/api/" + m[1],
			Protocol:   ProtocolRPC,
			Source:     source,
			Confidence: ConfidenceMedium,
		})
	}

	// 6. tRPC routes.
	for _, m := range reTRPCPath.FindAllStringSubmatch(content, 256) {
		a.addEndpoint(Endpoint{
			Method:     "GET",
			Path:       "/trpc/" + m[1],
			Protocol:   ProtocolTRPC,
			Source:     source,
			Confidence: ConfidenceMedium,
		})
	}

	// 7. GraphQL operation names: record as protocol evidence rather than
	// inventing fake `/graphql/<OpName>` HTTP routes.
	for _, m := range reGraphQLOp.FindAllStringSubmatch(content, 256) {
		opKind := strings.ToLower(m[1])
		opName := m[2]
		a.graphqlOps[opKind+" "+opName] = true
	}

	// 8. Absolute HTTPS URLs (extract origins + API paths).
	for _, raw := range reAbsHTTPSURL.FindAllString(content, 1024) {
		a.ingestAbsoluteURL(raw, source, ConfidenceMedium)
	}

	// 9. Relative API path literals.
	for _, m := range reRelativeAPIPath.FindAllStringSubmatch(content, 1024) {
		a.addCandidateURLOrPath("GET", m[1], source, ConfidenceLow, nil, nil, "")
	}

	// 10. Pinning references.
	if pm := rePinningToken.FindStringSubmatch(content); len(pm) > 0 {
		a.addPinning(PinningFinding{
			Kind:   "pinning-reference",
			Detail: fmt.Sprintf("Detected %s reference in %s", pm[1], fileName),
		})
	}
}

// scanStringSlice inspects extracted string pools from DEX, Hermes HBC, Mach-O,
// or Flutter AOT snapshots.
func (a *extractorAccumulator) scanStringSlice(stringsList []string, source string) {
	for _, s := range stringsList {
		s = strings.TrimSpace(s)
		if len(s) < 4 || len(s) > 512 {
			continue
		}
		if strings.HasPrefix(s, "https://") {
			a.ingestAbsoluteURL(s, source, ConfidenceMedium)
			continue
		}
		if reGRPCPath.MatchString(s) {
			p := s
			if !strings.HasPrefix(p, "/") {
				p = "/" + p
			}
			a.addEndpoint(Endpoint{
				Method:     "POST",
				Path:       p,
				Protocol:   ProtocolGRPC,
				Source:     source,
				Confidence: ConfidenceMedium,
			})
			continue
		}
		if reRetrofitRelPath.MatchString(s) {
			a.addEndpoint(Endpoint{
				Method:     "GET",
				Path:       "/" + strings.TrimPrefix(s, "/"),
				Protocol:   ProtocolREST,
				Source:     source,
				Confidence: ConfidenceLow,
			})
			continue
		}
		if strings.HasPrefix(s, "/") && looksLikeAPIPath(s) {
			a.addEndpoint(Endpoint{
				Method:     "GET",
				Path:       s,
				Protocol:   classifyProtocol(s),
				Source:     source,
				Confidence: ConfidenceLow,
			})
			continue
		}
		lower := strings.ToLower(s)
		if lower == "authorization" || lower == "x-api-key" || lower == "x-csrf-token" || lower == "x-client-version" {
			a.authHeaders[lower] = true
			if lower == "authorization" {
				a.hasBearer = true
			}
			if lower == "x-api-key" {
				a.hasAPIKey = true
			}
		}
		if strings.Contains(s, "CertificatePinner") || strings.Contains(s, "TrustKit") {
			a.addPinning(PinningFinding{
				Kind:   "certificate-pinner",
				Detail: fmt.Sprintf("Detected %s in %s", RedactString(s), source),
			})
		}
	}
}

func (a *extractorAccumulator) ingestAbsoluteURL(raw, source string, conf ConfidenceLevel) {
	cleaned := strings.TrimRight(raw, `.,;:'")}]`)
	// Replace JS template placeholders before url.Parse.
	cleaned = reTemplateExpr.ReplaceAllString(cleaned, "{param}")
	u, err := url.Parse(cleaned)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return
	}
	host := strings.ToLower(u.Hostname())
	if isIgnoredHost(host) || !isPublicHost(host) {
		return
	}
	origin := "https://" + u.Host
	weight := 1
	if looksLikeAPIHost(host) {
		weight = 4
	}
	a.addBaseURLWithWeight(origin, weight, source)

	// Record subdomain shards like pecan.api.example.com.
	if parts := strings.Split(host, "."); len(parts) >= 4 && (parts[1] == "api" || strings.HasSuffix(parts[0], "-api")) {
		a.addShard(parts[0], origin, source)
	}

	if u.Path != "" && u.Path != "/" && looksLikeAPIPath(u.Path) {
		ep := Endpoint{
			Method:     "GET",
			Path:       u.Path,
			Host:       strings.ToLower(u.Host),
			BaseURL:    origin,
			Protocol:   classifyProtocol(u.Path),
			Source:     source,
			Confidence: conf,
		}
		for k := range u.Query() {
			if k != "" && !sensitiveQueryKey[strings.ToLower(k)] {
				ep.Params = append(ep.Params, EndpointParam{Name: k, In: ParamInQuery, Type: "string"})
			}
		}
		a.addEndpoint(ep)
	}
}

func (a *extractorAccumulator) addCandidateURLOrPath(method, raw, source string, conf ConfidenceLevel, bodyKeys, headers []string, reqContentType string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return
	}
	if strings.HasPrefix(trimmed, "https://") {
		cleaned := reTemplateExpr.ReplaceAllString(trimmed, "{param}")
		u, err := url.Parse(cleaned)
		if err != nil || u.Hostname() == "" || isIgnoredHost(strings.ToLower(u.Hostname())) {
			return
		}
		origin := "https://" + strings.ToLower(u.Host)
		a.addBaseURLWithWeight(origin, 3, source)
		if u.Path == "" || u.Path == "/" || !looksLikeAPIPath(u.Path) {
			return
		}
		a.addEndpoint(Endpoint{
			Method:             method,
			Path:               u.Path,
			Host:               strings.ToLower(u.Host),
			BaseURL:            origin,
			Protocol:           classifyProtocol(u.Path),
			Source:             source,
			Confidence:         conf,
			BodyKeys:           bodyKeys,
			HeaderNames:        headers,
			RequestContentType: reqContentType,
		})
		return
	}

	// Strip leading `${BASE_URL}` template prefix if present.
	if strings.HasPrefix(trimmed, "${") {
		if idx := strings.Index(trimmed, "}/"); idx > 0 {
			trimmed = trimmed[idx+1:]
		}
	}
	if !strings.HasPrefix(trimmed, "/") {
		return
	}
	if !looksLikeAPIPath(trimmed) {
		return
	}
	a.addEndpoint(Endpoint{
		Method:             method,
		Path:               trimmed,
		Protocol:           classifyProtocol(trimmed),
		Source:             source,
		Confidence:         conf,
		BodyKeys:           bodyKeys,
		HeaderNames:        headers,
		RequestContentType: reqContentType,
	})
}

func finalizeDiscoveryResult(res *DiscoveryResult, acc *extractorAccumulator, opts Options) {
	// Derive non-generic brand tokens from app/package/name metadata so first-party
	// API hosts outrank bundled third-party SDK origins.
	brandTokens := extractBrandTokens(opts.Name, res.Meta.AppName, res.Meta.PackageID, res.Meta.BundleID)

	// Collect and rank base URLs.
	type scoredOrigin struct {
		origin string
		score  int
	}
	scored := make([]scoredOrigin, 0, len(acc.baseURLCounts))
	for origin, count := range acc.baseURLCounts {
		u, err := url.Parse(origin)
		if err != nil || u.Hostname() == "" {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if isIgnoredHost(host) || !isPublicHost(host) {
			continue
		}
		score := count * 10
		if looksLikeAPIHost(host) {
			score += 500
		}
		for _, tok := range brandTokens {
			if strings.Contains(host, tok) {
				score += 1000
				break
			}
		}
		if isNonAPISubdomain(host) {
			score -= 300
		}
		if acc.baseURLSource[origin] == "resources.arsc" {
			score += 200
		}
		scored = append(scored, scoredOrigin{origin: origin, score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].origin < scored[j].origin
	})

	baseURLs := make([]string, 0, len(scored))
	for _, s := range scored {
		baseURLs = append(baseURLs, s.origin)
	}
	res.Meta.BaseURLs = baseURLs

	if opts.BaseURL != "" {
		res.PrimaryBaseURL = normalizeOrigin(opts.BaseURL)
	} else if len(baseURLs) > 0 {
		res.PrimaryBaseURL = baseURLs[0]
	}

	// Collect shards sorted deterministically.
	shards := make([]ShardRoute, 0, len(acc.shards))
	for _, s := range acc.shards {
		shards = append(shards, s)
	}
	sort.Slice(shards, func(i, j int) bool {
		if shards[i].Name != shards[j].Name {
			return shards[i].Name < shards[j].Name
		}
		return shards[i].Host < shards[j].Host
	})
	res.Meta.Shards = shards

	primaryHost := ""
	if res.PrimaryBaseURL != "" {
		if u, err := url.Parse(res.PrimaryBaseURL); err == nil {
			primaryHost = strings.ToLower(u.Host)
		}
	}

	// Attach discovered GraphQL operation names as metadata notes on any real
	// /graphql endpoint (or a summary warning) without inventing fake routes.
	if len(acc.graphqlOps) > 0 {
		ops := make([]string, 0, len(acc.graphqlOps))
		for op := range acc.graphqlOps {
			ops = append(ops, op)
		}
		sort.Strings(ops)
		attached := false
		for _, ep := range acc.endpoints {
			if ep.Protocol == ProtocolGraphQL || strings.HasSuffix(strings.ToLower(ep.Path), "/graphql") {
				ep.Notes = mergeStrings(ep.Notes, ops)
				attached = true
			}
		}
		if !attached {
			limit := min(len(ops), 10)
			acc.addWarning(fmt.Sprintf("detected %d GraphQL operation(s) without an observed /graphql route: %s", len(ops), strings.Join(ops[:limit], ", ")))
		}
	}

	// Flatten and rank endpoints. Defer --top when HAR reconciliation will run
	// next so HAR-confirmed and HAR-only routes are ranked together before capping.
	rest := collectEndpoints(acc.endpoints, primaryHost, res.PrimaryBaseURL, opts.HostFilters)
	grpc := collectEndpoints(acc.grpcEndpoints, primaryHost, res.PrimaryBaseURL, opts.HostFilters)

	if opts.HARPath == "" && opts.Top > 0 && len(rest) > opts.Top {
		rest = rest[:opts.Top]
	}

	res.Endpoints = rest
	res.GRPCEndpoints = grpc
	res.Stats.RESTEndpoints = len(rest)
	res.Stats.GRPCEndpoints = len(grpc)

	// Finalize AuthSignal.
	res.Auth = buildAuthSignal(acc)
	res.Pinning = acc.pinning
	res.Warnings = append(res.Warnings, acc.warnings...)
}

// ApplyResultFilters applies --host filtering, deterministic ranking, and --top
// capping to a DiscoveryResult (used after HAR reconciliation).
func ApplyResultFilters(res *DiscoveryResult, opts Options) {
	if res == nil {
		return
	}
	filterSlice := func(in []Endpoint) []Endpoint {
		out := make([]Endpoint, 0, len(in))
		for _, ep := range in {
			if len(opts.HostFilters) > 0 && !matchesHostFilters(ep.Host, opts.HostFilters) {
				continue
			}
			out = append(out, ep)
		}
		sort.Slice(out, func(i, j int) bool {
			ci, cj := confidenceRank(out[i].Confidence), confidenceRank(out[j].Confidence)
			if ci != cj {
				return ci > cj
			}
			if out[i].Path != out[j].Path {
				return out[i].Path < out[j].Path
			}
			if out[i].Method != out[j].Method {
				return out[i].Method < out[j].Method
			}
			return out[i].BaseURL < out[j].BaseURL
		})
		return out
	}
	res.Endpoints = filterSlice(res.Endpoints)
	res.GRPCEndpoints = filterSlice(res.GRPCEndpoints)
	if opts.Top > 0 && len(res.Endpoints) > opts.Top {
		res.Endpoints = res.Endpoints[:opts.Top]
	}
	res.Stats.RESTEndpoints = len(res.Endpoints)
	res.Stats.GRPCEndpoints = len(res.GRPCEndpoints)
}

func collectEndpoints(m map[string]*Endpoint, primaryHost, primaryBaseURL string, hostFilters []string) []Endpoint {
	// Deduplicate across hostless and primary-origin copies of the same method+path.
	dedup := make(map[string]Endpoint, len(m))
	for _, ep := range m {
		cp := *ep
		if cp.BaseURL == "" && primaryBaseURL != "" {
			cp.Host = primaryHost
			cp.BaseURL = primaryBaseURL
		}
		if len(hostFilters) > 0 && !matchesHostFilters(cp.Host, hostFilters) {
			continue
		}
		key := cp.Method + " " + cp.BaseURL + " " + cp.Path
		if prev, ok := dedup[key]; ok {
			if confidenceRank(cp.Confidence) > confidenceRank(prev.Confidence) {
				prev.Confidence = cp.Confidence
				prev.Source = cp.Source
			}
			if prev.RequestContentType == "" && cp.RequestContentType != "" {
				prev.RequestContentType = cp.RequestContentType
			}
			prev.Params = mergeParams(prev.Params, cp.Params)
			prev.BodyKeys = mergeStrings(prev.BodyKeys, cp.BodyKeys)
			prev.HeaderNames = mergeStrings(prev.HeaderNames, cp.HeaderNames)
			dedup[key] = prev
		} else {
			dedup[key] = cp
		}
	}

	out := make([]Endpoint, 0, len(dedup))
	for _, ep := range dedup {
		out = append(out, ep)
	}
	sort.Slice(out, func(i, j int) bool {
		ci, cj := confidenceRank(out[i].Confidence), confidenceRank(out[j].Confidence)
		if ci != cj {
			return ci > cj
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].BaseURL < out[j].BaseURL
	})
	return out
}

func buildAuthSignal(acc *extractorAccumulator) AuthSignal {
	sig := AuthSignal{Scheme: AuthNone}
	switch {
	case acc.hasOAuth:
		sig.Scheme = AuthOAuth
	case acc.hasBearer:
		sig.Scheme = AuthBearer
	case acc.hasAPIKey:
		sig.Scheme = AuthAPIKey
	case acc.hasCookie:
		sig.Scheme = AuthCookie
	case acc.hasBasic:
		sig.Scheme = AuthBasic
	}
	for h := range acc.authHeaders {
		sig.HeaderNames = append(sig.HeaderNames, h)
	}
	sort.Strings(sig.HeaderNames)
	for c := range acc.authCookies {
		sig.CookieNames = append(sig.CookieNames, c)
	}
	sort.Strings(sig.CookieNames)
	for u := range acc.tokenURLs {
		sig.TokenURLs = append(sig.TokenURLs, u)
	}
	sort.Strings(sig.TokenURLs)
	return sig
}

func matchesHostFilters(host string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	lowerHost := strings.ToLower(host)
	for _, f := range filters {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		if lowerHost == f || strings.Contains(lowerHost, f) {
			return true
		}
	}
	return false
}

func normalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	if strings.ContainsAny(host, "{}$ \t\r\n") {
		return ""
	}
	return "https://" + host
}

func normalizeEndpointPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Strip query/fragment if attached.
	if idx := strings.IndexAny(raw, "?#"); idx >= 0 {
		raw = raw[:idx]
	}
	raw = reTemplateExpr.ReplaceAllStringFunc(raw, func(match string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		name := sanitizeParamName(inner)
		if name == "" {
			name = "id"
		}
		return "{" + name + "}"
	})
	raw = reColonParam.ReplaceAllString(raw, "/{$1}")
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	for strings.Contains(raw, "//") {
		raw = strings.ReplaceAll(raw, "//", "/")
	}
	if len(raw) > 1 {
		raw = strings.TrimRight(raw, "/")
	}
	if raw == "/" || len(raw) > 220 || strings.ContainsAny(raw, " \t\r\n<>\"'\\") {
		return ""
	}
	// Redact any embedded credentials (JWTs, provider tokens, emails) inside path
	// segments before storing or writing specs, traffic analysis, or manifests.
	segs := strings.Split(raw, "/")
	for i, seg := range segs {
		if seg == "" || (strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")) {
			continue
		}
		if RedactString(seg) != seg {
			segs[i] = "{token}"
		}
	}
	return strings.Join(segs, "/")
}

type fetchCall struct {
	target  string
	options string
}

// scanFetchCalls finds `fetch("...", { ... })` calls and extracts balanced
// nested options objects (so nested `headers: { ... }` does not truncate
// subsequent `method:` or `body:` fields).
func scanFetchCalls(content string, maxCalls int) []fetchCall {
	locs := reFetchStart.FindAllStringSubmatchIndex(content, maxCalls)
	if len(locs) == 0 {
		return nil
	}
	out := make([]fetchCall, 0, len(locs))
	for _, loc := range locs {
		target := content[loc[4]:loc[5]]
		pos := loc[1]
		for pos < len(content) && (content[pos] == ' ' || content[pos] == '\t' || content[pos] == '\n' || content[pos] == '\r') {
			pos++
		}
		var opts string
		if pos < len(content) && content[pos] == ',' {
			pos++
			for pos < len(content) && (content[pos] == ' ' || content[pos] == '\t' || content[pos] == '\n' || content[pos] == '\r') {
				pos++
			}
			if pos < len(content) && content[pos] == '{' {
				opts = extractBalancedBraces(content, pos, 4096)
			}
		}
		out = append(out, fetchCall{target: target, options: opts})
	}
	return out
}

func extractBalancedBraces(s string, start, maxLen int) string {
	if start >= len(s) || s[start] != '{' {
		return ""
	}
	limit := min(len(s), start+maxLen)
	depth := 0
	var quote byte
	escaped := false
	for i := start; i < limit; i++ {
		c := s[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start+1 : i]
			}
		}
	}
	return s[start+1 : limit]
}

func sanitizeParamName(raw string) string {
	// Take last dotted identifier, e.g. `params.userId` -> `userId`.
	if idx := strings.LastIndex(raw, "."); idx >= 0 {
		raw = raw[idx+1:]
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func classifyProtocol(path string) ProtocolKind {
	if reGRPCPath.MatchString(path) {
		return ProtocolGRPC
	}
	if strings.HasPrefix(path, "/graphql") {
		return ProtocolGraphQL
	}
	if strings.HasPrefix(path, "/trpc/") {
		return ProtocolTRPC
	}
	if reDotRPCPath.MatchString(path) {
		return ProtocolRPC
	}
	return ProtocolREST
}

func looksLikeAPIPath(path string) bool {
	if !strings.HasPrefix(path, "/") || len(path) < 3 || len(path) > 220 {
		return false
	}
	if strings.ContainsAny(path, " \t\r\n<>\"'\\;()[]") {
		return false
	}
	if isStaticAssetPath(path) {
		return false
	}
	lower := strings.ToLower(path)
	// Reject file-system and Android/iOS internal paths.
	for _, bad := range []string{
		"/system/", "/data/", "/proc/", "/dev/", "/etc/", "/usr/", "/bin/", "/lib/", "/var/", "/tmp/",
		"/assets/", "/res/", "/drawable", "/mipmap", "/layout/", "/values/", "/meta-inf/",
		"/node_modules/", "/webpack/", "/static/", "/fonts/", "/images/", "/img/", "/icons/",
		"/android_asset/", "/content/", "/frameworks/", "/library/", "/applications/",
		"/dtds/", "/schemas/", "/xmlns/",
	} {
		if strings.HasPrefix(lower, bad) || strings.Contains(lower, bad) {
			return false
		}
	}
	// Must contain at least one alphabetic segment and look like a route.
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) == 0 || len(segs) > 10 {
		return false
	}
	first := strings.ToLower(segs[0])
	if first == "api" || first == "graphql" || first == "trpc" || first == "rpc" || first == "oauth" || first == "oauth2" || first == "auth" || isVersionPathSeg(first) {
		return len(segs) >= 2 || first == "graphql"
	}
	// Allow clean 2+ segment REST paths containing a placeholder or API-like verb/noun.
	if len(segs) >= 2 && strings.Contains(path, "{") {
		return true
	}
	if reDotRPCPath.MatchString(path) {
		return true
	}
	return false
}

func isVersionPathSeg(seg string) bool {
	if len(seg) < 2 || seg[0] != 'v' {
		return false
	}
	for _, r := range seg[1:] {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

func isStaticAssetPath(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range staticAssetExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func isIgnoredHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return true
	}
	for _, ig := range ignoredHosts {
		if host == ig || strings.HasSuffix(host, "."+ig) {
			return true
		}
	}
	return false
}

func isPublicHost(host string) bool {
	if !strings.Contains(host, ".") {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
	}
	return true
}

func looksLikeAPIHost(host string) bool {
	h := strings.ToLower(host)
	return strings.HasPrefix(h, "api.") ||
		strings.HasPrefix(h, "api-") ||
		strings.Contains(h, ".api.") ||
		strings.HasPrefix(h, "client-api.") ||
		strings.HasPrefix(h, "app-api.") ||
		strings.HasPrefix(h, "auth-api.") ||
		strings.HasPrefix(h, "mobile-api.") ||
		strings.HasPrefix(h, "gateway.") ||
		strings.HasPrefix(h, "edgeapi.") ||
		strings.HasPrefix(h, "graphql.") ||
		strings.HasPrefix(h, "backend.")
}

func isNonAPISubdomain(host string) bool {
	h := strings.ToLower(host)
	for _, prefix := range []string{
		"www.", "docs.", "help.", "support.", "status.", "blog.",
		"cdn.", "media.", "app-media.", "static.", "assets.", "images.", "img.",
	} {
		if strings.HasPrefix(h, prefix) {
			return true
		}
	}
	return false
}

func looksLikeShardHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if isIgnoredHost(h) {
		return false
	}
	return looksLikeAPIHost(h)
}

func isAuthHeaderName(lower string) bool {
	return lower == "authorization" || lower == "x-api-key" || lower == "x-csrf-token" || lower == "x-xsrf-token" || strings.Contains(lower, "token") || strings.Contains(lower, "auth")
}

func extractBodyKeys(obj string) []string {
	if obj == "" {
		return nil
	}
	var out []string
	for _, m := range reBodyKeysObj.FindAllStringSubmatch(obj, 4) {
		for _, km := range reJSObjectKey.FindAllStringSubmatch(m[1], 24) {
			k := km[1]
			if k != "" && !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func extractPathParams(path string) []EndpointParam {
	var out []EndpointParam
	seen := make(map[string]bool)
	for seg := range strings.SplitSeq(path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") && len(seg) > 2 {
			name := seg[1 : len(seg)-1]
			if !seen[name] {
				seen[name] = true
				out = append(out, EndpointParam{
					Name:     name,
					In:       ParamInPath,
					Type:     "string",
					Required: true,
				})
			}
		}
	}
	return out
}

func mergeParams(a, b []EndpointParam) []EndpointParam {
	seen := make(map[string]int, len(a)+len(b))
	out := make([]EndpointParam, 0, len(a)+len(b))
	for _, p := range a {
		key := string(p.In) + ":" + p.Name
		seen[key] = len(out)
		out = append(out, p)
	}
	for _, p := range b {
		key := string(p.In) + ":" + p.Name
		if idx, ok := seen[key]; ok {
			if p.Required {
				out[idx].Required = true
			}
			if out[idx].Type == "" && p.Type != "" {
				out[idx].Type = p.Type
			}
		} else {
			seen[key] = len(out)
			out = append(out, p)
		}
	}
	return out
}

func mergeStrings(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, s := range b {
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func confidenceRank(c ConfidenceLevel) int {
	switch c {
	case ConfidenceHigh:
		return 3
	case ConfidenceMedium:
		return 2
	default:
		return 1
	}
}

func extractBrandTokens(values ...string) []string {
	stop := map[string]bool{
		"com": true, "org": true, "net": true, "io": true, "co": true, "me": true, "app": true,
		"android": true, "ios": true, "mobile": true, "client": true, "prod": true, "release": true,
		"inc": true, "llc": true, "labs": true, "the": true, "api": true, "cli": true,
	}
	var out []string
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		// Full concatenated alphanumeric form (e.g. "Eight Sleep" -> "eightsleep").
		var compact strings.Builder
		for _, r := range v {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				compact.WriteRune(r)
			}
		}
		if cs := compact.String(); len(cs) >= 4 && !stop[cs] && !slices.Contains(out, cs) {
			out = append(out, cs)
		}
		for _, part := range strings.FieldsFunc(v, func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < '0' || r > '9')
		}) {
			if len(part) >= 4 && !stop[part] && !slices.Contains(out, part) {
				out = append(out, part)
			}
		}
	}
	return out
}

package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// CompileResult reports the compiled Cartridge, companion SKILL.md, and
// provenance summary produced by `pp record`.
type CompileResult struct {
	Cartridge    *cartridge.Cartridge `json:"cartridge"`
	YAMLBytes    []byte               `json:"-"`
	SkillMD      string               `json:"-"`
	CommandCount int                  `json:"command_count"`
	ElapsedMS    int64                `json:"elapsed_ms"`
	Source       string               `json:"source"`
	AllowedHosts []string             `json:"allowed_hosts"`
}

// CompileFromHAR runs Printing Press's existing browsersniff analyzer on a HAR
// or EnrichedCapture file and compiles it into a signed pp-cartridge/v1 bundle.
func CompileFromHAR(capturePath, nameOverride string) (*CompileResult, error) {
	start := time.Now()
	apiSpec, err := browsersniff.Analyze(capturePath)
	if err != nil {
		return nil, fmt.Errorf("analyzing capture %s: %w", capturePath, err)
	}
	if strings.TrimSpace(nameOverride) != "" {
		apiSpec.Name = sanitizeSlug(nameOverride)
	}
	res, err := CompileSpecToCartridge(apiSpec, "har:"+filepath.Base(capturePath))
	if err != nil {
		return nil, err
	}
	res.ElapsedMS = time.Since(start).Milliseconds()
	return res, nil
}

// CompileFromSpecFile loads an OpenAPI or internal Printing Press YAML spec and
// compiles it into a lightweight pp-cartridge/v1 bundle.
func CompileFromSpecFile(specPath, nameOverride string) (*CompileResult, error) {
	start := time.Now()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("reading spec %s: %w", specPath, err)
	}
	var apiSpec *spec.APISpec
	if spec.LooksLikeInternalYAML(raw) {
		apiSpec, err = spec.Parse(specPath)
	} else {
		apiSpec, err = openapi.ParseFile(specPath)
		if err != nil {
			apiSpec, err = spec.Parse(specPath)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("loading spec %s: %w", specPath, err)
	}
	if strings.TrimSpace(nameOverride) != "" {
		apiSpec.Name = sanitizeSlug(nameOverride)
	}
	res, err := CompileSpecToCartridge(apiSpec, "spec:"+filepath.Base(specPath))
	if err != nil {
		return nil, err
	}
	res.ElapsedMS = time.Since(start).Milliseconds()
	return res, nil
}

// ArdiaRecordOptions configures background HAR capture through Ardia Browser
// (`ARDIA_BACKGROUND=1 ardia browse`, zero focus steal, zero Chrome 144 popups).
type ArdiaRecordOptions struct {
	TargetURL    string
	NameOverride string
	WaitSeconds  int
	ArdiaBin     string
}

// RecordWithArdia opens targetURL in a background Ardia tab, records XHR/Fetch
// traffic to a temporary HAR file, closes the tab cleanly, and compiles the
// captured traffic into a pp-cartridge/v1 bundle.
func RecordWithArdia(ctx context.Context, opts ArdiaRecordOptions) (*CompileResult, error) {
	start := time.Now()
	target := strings.TrimSpace(opts.TargetURL)
	if target == "" {
		return nil, fmt.Errorf("target URL is required")
	}
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}

	ardiaBin := opts.ArdiaBin
	if ardiaBin == "" {
		var err error
		ardiaBin, err = exec.LookPath("ardia")
		if err != nil {
			return nil, fmt.Errorf("ardia CLI not found on PATH (enable Ardia > Settings > Plugins > Command Line, or pass --from-har <file>)")
		}
	}

	tmpDir, err := os.MkdirTemp("", "pp-ardia-rec-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	harPath := filepath.Join(tmpDir, "capture.har")

	runArdia := func(args ...string) error {
		cmd := exec.CommandContext(ctx, ardiaBin, args...)
		cmd.Env = append(os.Environ(), "ARDIA_BACKGROUND=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ardia %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	// Open background tab, start HAR capture, reload so initial XHR/Fetch calls are captured, then stop and close tab.
	if err := runArdia("browse", "open", target); err != nil {
		return nil, err
	}
	defer func() { _ = runArdia("browse", "close") }()

	if err := runArdia("browse", "network", "har", "start", "--content", "text"); err != nil {
		return nil, err
	}
	if err := runArdia("browse", "reload"); err != nil {
		return nil, err
	}
	waitSec := opts.WaitSeconds
	if waitSec <= 0 {
		waitSec = 3
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Duration(waitSec) * time.Second):
	}
	if err := runArdia("browse", "network", "har", "stop", harPath); err != nil {
		return nil, err
	}

	res, err := CompileFromHAR(harPath, opts.NameOverride)
	if err != nil {
		return nil, err
	}
	res.Source = "ardia:" + target
	res.ElapsedMS = time.Since(start).Milliseconds()
	return res, nil
}

// CompileSpecToCartridge transforms an analyzed Printing Press APISpec into a
// validated, host-allowlisted pp-cartridge/v1 bundle and matching SKILL.md.
func CompileSpecToCartridge(apiSpec *spec.APISpec, source string) (*CompileResult, error) {
	if apiSpec == nil {
		return nil, fmt.Errorf("api spec is nil")
	}
	name := sanitizeSlug(apiSpec.Name)
	if name == "" {
		name = "app"
	}

	hostSet := make(map[string]struct{})
	addHost := func(rawURL string) {
		if u, err := url.Parse(rawURL); err == nil && u.Hostname() != "" {
			hostSet[strings.ToLower(u.Hostname())] = struct{}{}
		}
	}

	baseURL := strings.TrimRight(apiSpec.BaseURL, "/")
	if baseURL != "" && baseURL != spec.PlaceholderBaseURL {
		addHost(baseURL)
	}

	commands := make(map[string]cartridge.CommandSpec)
	var mutating []string

	resNames := make([]string, 0, len(apiSpec.Resources))
	for rName := range apiSpec.Resources {
		resNames = append(resNames, rName)
	}
	sort.Strings(resNames)

	for _, rName := range resNames {
		res := apiSpec.Resources[rName]
		epNames := make([]string, 0, len(res.Endpoints))
		for eName := range res.Endpoints {
			epNames = append(epNames, eName)
		}
		sort.Strings(epNames)

		for _, eName := range epNames {
			ep := res.Endpoints[eName]
			fullURL := resolveEndpointURL(baseURL, res.BaseURL, ep.BaseURL, ep.Path)
			if fullURL == "" {
				continue
			}
			u, err := url.Parse(fullURL)
			if err != nil || u.Hostname() == "" {
				continue
			}
			if strings.HasPrefix(source, "har:") || strings.HasPrefix(source, "ardia:") {
				if isNoiseHost(u.Hostname()) {
					continue
				}
			}
			addHost(fullURL)

			cmdName := sanitizeSlug(rName + "-" + eName)
			if len(apiSpec.Resources) == 1 {
				cmdName = sanitizeSlug(eName)
			}
			if cmdName == "" {
				cmdName = "run"
			}
			// Deduplicate collisions deterministically.
			if _, exists := commands[cmdName]; exists {
				cmdName = sanitizeSlug(rName + "-" + eName)
			}

			method := strings.ToUpper(strings.TrimSpace(ep.Method))
			if method == "" {
				method = "GET"
			}
			isMut := method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
			if ep.Mutation != nil {
				isMut = *ep.Mutation
			}
			if isMut {
				mutating = append(mutating, cmdName)
			}

			params := make(map[string]cartridge.ParamSpec)
			queryMap := make(map[string]string)
			headerMap := make(map[string]string)
			for _, p := range ep.Params {
				pName := strings.TrimSpace(p.Name)
				if pName == "" {
					continue
				}
				pType := p.Type
				if pType == "" {
					pType = "string"
				}
				defVal := formatParamDefault(p.Default)
				params[pName] = cartridge.ParamSpec{
					Type:        pType,
					Required:    p.Required && defVal == "",
					Default:     defVal,
					Description: p.Description,
				}
				inLower := strings.ToLower(strings.TrimSpace(p.In))
				isPath := inLower == "path" || p.PathParam || p.Positional || strings.Contains(fullURL, "{"+pName+"}")
				switch {
				case isPath:
					// Rendered directly into URL path template.
				case inLower == "header":
					wireHeader := p.URLName
					if wireHeader == "" {
						wireHeader = pName
					}
					headerMap[wireHeader] = "{" + pName + "}"
				default:
					// Spec convention: non-positional, non-header params (including
					// browsersniff's In=="") bind to URL query parameters.
					wireQuery := p.URLName
					if wireQuery == "" {
						wireQuery = pName
					}
					queryMap[wireQuery] = "{" + pName + "}"
				}
			}

			driver := cartridge.DriverHTTP
			bodyTemplate := ""
			gqlQuery := ""
			gqlOp := ""
			if ep.BodyJSONFallback {
				params["body_json"] = cartridge.ParamSpec{
					Type:        "string",
					Required:    ep.BodyRequired,
					Description: "Raw JSON request body",
				}
				bodyTemplate = "{body_json}"
			} else if len(ep.Body) > 0 {
				if isGraphQLBody(ep.Body) {
					driver = cartridge.DriverGraphQL
					for _, bp := range ep.Body {
						bName := strings.TrimSpace(bp.Name)
						defVal := formatParamDefault(bp.Default)
						switch strings.ToLower(bName) {
						case "query":
							if defVal != "" {
								gqlQuery = defVal
							}
						case "operationname", "operation_name":
							if defVal != "" {
								gqlOp = defVal
							}
						case "variables":
							// Variables are supplied as cartridge params.
						default:
							if bName != "" {
								bType := bp.Type
								if bType == "" {
									bType = "string"
								}
								params[bName] = cartridge.ParamSpec{
									Type:        bType,
									Required:    bp.Required && defVal == "",
									Default:     defVal,
									Description: bp.Description,
								}
							}
						}
					}
				} else {
					bodyFields := make([]string, 0, len(ep.Body))
					bodyMap := make(map[string]string, len(ep.Body))
					for _, bp := range ep.Body {
						bName := strings.TrimSpace(bp.Name)
						if bName == "" {
							continue
						}
						bType := bp.Type
						if bType == "" {
							bType = "string"
						}
						defVal := formatParamDefault(bp.Default)
						params[bName] = cartridge.ParamSpec{
							Type:        bType,
							Required:    bp.Required && defVal == "",
							Default:     defVal,
							Description: bp.Description,
						}
						wireKey := bp.BodyName
						if wireKey == "" {
							wireKey = bName
						}
						bodyFields = append(bodyFields, wireKey)
						bodyMap[wireKey] = "{" + bName + "}"
					}
					if len(bodyFields) > 0 {
						if encodedBody, mErr := json.Marshal(bodyMap); mErr == nil {
							bodyTemplate = string(encodedBody)
						}
					}
				}
			}

			desc := strings.TrimSpace(ep.Description)
			if desc == "" {
				desc = fmt.Sprintf("%s %s", method, ep.Path)
			}

			extract := ""
			if apiSpec.ResponseEnvelopeKey != "" {
				extract = "$." + apiSpec.ResponseEnvelopeKey + "[*]"
			} else if strings.EqualFold(ep.Response.Type, "array") {
				extract = "$[*]"
			}

			cmdSpec := cartridge.CommandSpec{
				Description:    desc,
				Driver:         driver,
				Method:         method,
				URL:            fullURL,
				Headers:        headerMap,
				Query:          queryMap,
				Body:           bodyTemplate,
				GraphQLQuery:   gqlQuery,
				PersistedQuery: gqlOp,
				Extract:        extract,
				Params:         params,
				Mutates:        isMut,
				SyncDefault:    method == "GET" && !isMut && len(params) == 0,
			}
			commands[cmdName] = cmdSpec
		}
	}

	if len(commands) == 0 {
		return nil, fmt.Errorf("no usable API endpoints discovered in %s (try interacting with the page while recording or pass a HAR containing XHR/Fetch calls)", source)
	}

	allowedHosts := make([]string, 0, len(hostSet))
	for h := range hostSet {
		allowedHosts = append(allowedHosts, h)
	}
	sort.Strings(allowedHosts)
	slices.Sort(mutating)
	mutating = slices.Compact(mutating)

	authCfg := mapAuthConfig(apiSpec, allowedHosts)
	desc := strings.TrimSpace(apiSpec.CLIDescription)
	if desc == "" {
		desc = strings.TrimSpace(apiSpec.Description)
	}
	if desc == "" {
		desc = fmt.Sprintf("Compiled local Printing Press Cartridge for %s", name)
	}

	domains := append([]string(nil), allowedHosts...)
	homepage := apiSpec.WebsiteURL
	if homepage == "" && len(allowedHosts) > 0 {
		homepage = "https://" + allowedHosts[0]
	}

	c := &cartridge.Cartridge{
		Schema:      cartridge.SchemaVersionV1,
		Name:        name,
		Version:     time.Now().UTC().Format("2006.01.02"),
		Description: desc,
		Category:    apiSpec.Category,
		Homepage:    homepage,
		Domains:     domains,
		Trust: cartridge.TrustPolicy{
			AllowedHosts: allowedHosts,
			Mutates:      mutating,
			ReadOnly:     len(mutating) == 0,
		},
		Auth:     authCfg,
		Commands: commands,
		Source:   source,
	}

	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("compiled cartridge failed validation: %w", err)
	}
	raw, err := c.MarshalYAML()
	if err != nil {
		return nil, err
	}
	c.SHA256 = cartridge.DigestBytes(raw)

	return &CompileResult{
		Cartridge:    c,
		YAMLBytes:    raw,
		SkillMD:      GenerateSkillMarkdown(c),
		CommandCount: len(commands),
		Source:       source,
		AllowedHosts: allowedHosts,
	}, nil
}

// GenerateSkillMarkdown renders a concise, agent-ready SKILL.md for a Cartridge.
func GenerateSkillMarkdown(c *cartridge.Cartridge) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: pp-%s\ndescription: %q\n---\n\n", c.Name, c.Description)
	fmt.Fprintf(&b, "# %s (Printing Press 2.0 Cartridge)\n\n", c.Name)
	fmt.Fprintf(&b, "%s\n\n", c.Description)
	fmt.Fprintf(&b, "Host allowlist: `%s` | SHA-256: `%s`\n\n", strings.Join(c.Trust.AllowedHosts, ", "), c.SHA256)
	fmt.Fprintf(&b, "## Commands\n\n")
	names := make([]string, 0, len(c.Commands))
	for k := range c.Commands {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		cmd := c.Commands[n]
		mutNote := ""
		if c.IsMutating(n) {
			mutNote = " *(mutating — requires `--yes`)*"
		}
		fmt.Fprintf(&b, "- `pp run %s %s --compact --json` — %s%s\n", c.Name, n, cmd.Description, mutNote)
	}
	fmt.Fprintf(&b, "\n## Life Lake Sync\n\n```bash\npp lake sync %s\npp lake search \"<query>\" --source %s --json\n```\n", c.Name, c.Name)
	return b.String()
}

func resolveEndpointURL(specBase, resBase, epBase, epPath string) string {
	if strings.HasPrefix(epPath, "https://") || strings.HasPrefix(epPath, "http://") {
		return epPath
	}
	base := epBase
	if base == "" {
		base = resBase
	}
	if base == "" {
		base = specBase
	}
	if base == "" || base == spec.PlaceholderBaseURL {
		return ""
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(epPath, "/")
}

func mapAuthConfig(apiSpec *spec.APISpec, hosts []string) cartridge.AuthConfig {
	envVar := ""
	if len(apiSpec.Auth.EnvVars) > 0 {
		envVar = apiSpec.Auth.EnvVars[0]
	}
	switch strings.ToLower(strings.TrimSpace(apiSpec.Auth.Type)) {
	case "", "none":
		return cartridge.AuthConfig{Strategy: cartridge.AuthStrategyNone}
	case "cookie", "browser_cookie", "composed":
		return cartridge.AuthConfig{
			Strategy: cartridge.AuthStrategyBrowserCookie,
			Domains:  hosts,
		}
	case "api_key":
		cfg := cartridge.AuthConfig{
			Strategy: cartridge.AuthStrategyEnvAPIKey,
			EnvVar:   envVar,
			Header:   apiSpec.Auth.Header,
			Prefix:   apiSpec.Auth.Prefix,
		}
		if strings.EqualFold(apiSpec.Auth.In, "query") {
			cfg.QueryParam = apiSpec.Auth.Header
			cfg.Header = ""
		}
		return cfg
	default:
		return cartridge.AuthConfig{
			Strategy: cartridge.AuthStrategyEnvBearer,
			EnvVar:   envVar,
			Header:   apiSpec.Auth.Header,
			Prefix:   apiSpec.Auth.Prefix,
		}
	}
}

func sanitizeSlug(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// isNoiseHost filters ad networks, consent banners, telemetry beacons, and
// generic script CDNs out of browser-recorded HAR cartridges.
func isNoiseHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return true
	}
	noisePrefixes := []string{"ads.", "adservice.", "analytics.", "telemetry.", "metrics.", "pixel."}
	for _, p := range noisePrefixes {
		if strings.HasPrefix(h, p) {
			return true
		}
	}
	noiseSuffixes := []string{
		"rubiconproject.com",
		"cookielaw.org",
		"onetrust.com",
		"conviva.com",
		"omtrdc.net",
		"doubleclick.net",
		"googlesyndication.com",
		"googletagmanager.com",
		"google-analytics.com",
		"segment.io",
		"segment.com",
		"sentry.io",
		"jsdelivr.net",
		"datadoghq.com",
		"newrelic.com",
		"hotjar.com",
		"optimizely.com",
		"pubmatic.com",
		"criteo.com",
		"amazon-adsystem.com",
		"scorecardresearch.com",
		"quantserve.com",
		"branch.io",
		"braze.com",
		"amplitude.com",
		"mixpanel.com",
		"demdex.net",
		"adobedtm.com",
	}
	for _, s := range noiseSuffixes {
		if h == s || strings.HasSuffix(h, "."+s) {
			return true
		}
	}
	return false
}

func formatParamDefault(v any) string {
	if v == nil {
		return ""
	}
	switch d := v.(type) {
	case string:
		return d
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(d)
	default:
		if raw, err := json.Marshal(d); err == nil {
			return string(raw)
		}
		return fmt.Sprint(d)
	}
}

func isGraphQLBody(body []spec.Param) bool {
	hasQuery := false
	hasVarsOrOp := false
	for _, bp := range body {
		switch strings.ToLower(strings.TrimSpace(bp.Name)) {
		case "query":
			hasQuery = true
		case "variables", "operationname", "operation_name":
			hasVarsOrOp = true
		}
	}
	return hasQuery && hasVarsOrOp
}

package generator

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoTemplatesEscapeSpecTextInStringLiterals(t *testing.T) {
	t.Parallel()

	specTextField := regexp.MustCompile(`\.(?:[A-Za-z0-9_]*Description|Description|Summary|Instructions)\b`)
	var violations []string

	err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go.tmpl") {
			return nil
		}

		data, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			for start := strings.Index(line, "{{"); start >= 0; {
				end := strings.Index(line[start+2:], "}}")
				if end < 0 {
					break
				}
				end += start + 2
				action := line[start : end+2]
				if isInsideGoDoubleQuotedString(line[:start]) &&
					specTextField.MatchString(action) &&
					!goTemplateActionEscapesSpecText(action) {
					violations = append(violations, path+":"+strconv.Itoa(lineNo+1)+": "+strings.TrimSpace(line))
				}
				next := end + 2
				if next >= len(line) {
					break
				}
				if rel := strings.Index(line[next:], "{{"); rel >= 0 {
					start = next + rel
				} else {
					break
				}
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, violations, "spec-controlled prose inside Go string literals must use oneline/printf %%q; unsafe template sites:\n%s", strings.Join(violations, "\n"))
}

// dangerousSpecValueLeaf names the spec-derived field families that, when
// emitted as a bare reference inside a Go double-quoted string literal, are a
// code-injection vector: text/template does not escape, so a value containing a
// quote can break out of the literal and compile to live Go. URL/auth/header/
// path/method fields are attacker-influenced for domain-import (recovered from
// an untrusted HAR or fetched domain) and are not validated for Go-literal
// safety. The emit must go through printf %q / goLiteral / oneline.
var dangerousSpecValueLeaf = regexp.MustCompile(`(?:URL|Path|Method|Framing|Param|Field|Format|Prefix|Branch|Addr|Domain|Endpoint|Resource|Scheme|Host|Query)$`)

func isDangerousSpecValueLeaf(leaf string) bool {
	switch leaf {
	case "TokenParamName", "Header", "Value", "DefaultRepo", "Type":
		return true
	}
	return dangerousSpecValueLeaf.MatchString(leaf)
}

// rawSpecFieldLeaf returns the leaf identifier of a template action that is a
// bare field reference (e.g. "{{.Auth.TokenURL}}" -> "TokenURL"), and false for
// helper calls ("{{kebab .Name}}"), pipelines ("{{.X | y}}"), template vars
// ("{{$c}}"), and control actions ("{{if ...}}"). Helper-wrapped values are
// assumed sanitized by the helper and are out of scope for this guard.
func rawSpecFieldLeaf(action string) (string, bool) {
	inner := strings.TrimSpace(action)
	inner = strings.TrimPrefix(inner, "{{")
	inner = strings.TrimSuffix(inner, "}}")
	inner = strings.TrimSpace(inner)
	inner = strings.TrimPrefix(inner, "-")
	inner = strings.TrimSuffix(inner, "-")
	inner = strings.TrimSpace(inner)
	if strings.ContainsRune(inner, '|') {
		return "", false // piped through a function — out of scope here
	}
	if !strings.HasPrefix(inner, ".") {
		return "", false // helper call, $var, or control action
	}
	if strings.ContainsAny(inner, " \t") {
		return "", false // a bare field reference has no arguments
	}
	leaf := inner[strings.LastIndex(inner, ".")+1:]
	if leaf == "" {
		return "", false // bare "." (the dot) — not a field
	}
	return leaf, true
}

// nonSanitizingHelpers are template helpers that pass their input's characters
// through unchanged (case/concat/lookup only) — so a spec value with a quote
// survives and can break out of a Go string literal. When one of these is the
// OUTERMOST command of an action inside a Go double-quoted string, the action
// must be wrapped in an escaper (printf %q / oneline). Normalizing helpers that
// emit identifier-only output (kebab, snake, envName, …) are not listed.
var nonSanitizingHelpers = map[string]bool{
	"upper": true, "lower": true, "title": true,
	"join": true, "index": true, "paramWireName": true,
	"authParameterName": true, "graphqlQueryField": true,
	"endpointTemplateEnvName": true,
	// Note: enumDescriptionHint is intentionally NOT listed — it sanitizes its
	// own enum values via OneLine internally, so a raw {{enumDescriptionHint}}
	// in a string literal is safe (and must stay un-%q'd to keep its leading
	// space).
}

// outerTemplateCommand returns the leading token of an action's outermost
// pipeline stage: "printf" for `{{printf "%q" .X}}`, "upper" for
// `{{upper .X}}`, "" for a bare ref / $var / control action.
func outerTemplateCommand(action string) string {
	inner := strings.TrimSpace(action)
	inner = strings.TrimPrefix(inner, "{{")
	inner = strings.TrimSuffix(inner, "}}")
	inner = strings.TrimSpace(inner)
	inner = strings.TrimSpace(strings.TrimPrefix(inner, "-"))
	inner = strings.TrimSpace(strings.TrimSuffix(inner, "-"))
	if i := strings.LastIndex(inner, "|"); i >= 0 {
		inner = strings.TrimSpace(inner[i+1:])
	}
	if inner == "" || strings.HasPrefix(inner, ".") || strings.HasPrefix(inner, "$") {
		return ""
	}
	for j, r := range inner {
		if r == ' ' || r == '\t' || r == '(' {
			return inner[:j]
		}
	}
	return inner
}

func actionEscapesValue(action string) bool {
	return strings.Contains(action, `printf "%q"`) ||
		strings.Contains(action, "oneline ") ||
		strings.Contains(action, "goLiteral ")
}

// TestGoTemplatesEscapeSpecValuesInStringLiterals guards the Go-code-injection
// class for spec-derived values emitted into generated Go double-quoted string
// literals (the supply-chain RCE vector for printed CLIs built from untrusted
// sniffed/imported specs). It flags three shapes that let a quote-bearing value
// break out of the literal into executable Go:
//  1. a bare field reference that is the entire literal ("{{.X}}") — must be %q;
//  2. a bare reference to a high-risk field embedded mid-literal — must be oneline;
//  3. a non-sanitizing helper (upper/lower/join/index/…) as the outermost command,
//     not already wrapped in an escaper.
//
// Sibling to TestGoTemplatesEscapeSpecTextInStringLiterals, which covers prose.
func TestGoTemplatesEscapeSpecValuesInStringLiterals(t *testing.T) {
	t.Parallel()

	var violations []string
	err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go.tmpl") {
			return nil
		}
		data, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			for start := strings.Index(line, "{{"); start >= 0; {
				end := strings.Index(line[start+2:], "}}")
				if end < 0 {
					break
				}
				end += start + 2
				action := line[start : end+2]
				if isInsideGoDoubleQuotedString(line[:start]) {
					prev := byte(0)
					if start > 0 {
						prev = line[start-1]
					}
					next := byte(0)
					if end+2 < len(line) {
						next = line[end+2]
					}
					wholeLiteral := prev == '"' && next == '"'

					unsafe := false
					if leaf, ok := rawSpecFieldLeaf(action); ok {
						// bare field reference: %q when it is the whole literal,
						// oneline when a high-risk field is embedded mid-literal.
						unsafe = wholeLiteral || isDangerousSpecValueLeaf(leaf)
					} else if cmd := outerTemplateCommand(action); cmd == "" {
						// $var, lone-dot, or parenthesized field access ((index x 0).Name):
						// flag when it is the whole literal and not escaped.
						unsafe = wholeLiteral && !actionEscapesValue(action)
					} else if nonSanitizingHelpers[cmd] && !actionEscapesValue(action) {
						unsafe = true
					}
					if unsafe {
						violations = append(violations, path+":"+strconv.Itoa(lineNo+1)+": "+strings.TrimSpace(line))
					}
				}
				next := end + 2
				if next >= len(line) {
					break
				}
				if rel := strings.Index(line[next:], "{{"); rel >= 0 {
					start = next + rel
				} else {
					break
				}
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, violations, "spec-derived values inside Go string literals must use printf %%q / oneline / goLiteral; unsafe template sites:\n%s", strings.Join(violations, "\n"))
}

func isInsideGoDoubleQuotedString(prefix string) bool {
	inString := false
	inRaw := false // inside a Go `...` raw string literal (e.g. a struct tag)
	escaped := false
	for i := 0; i < len(prefix); {
		if strings.HasPrefix(prefix[i:], "{{") {
			end := strings.Index(prefix[i+2:], "}}")
			if end < 0 {
				break
			}
			i += 2 + end + 2
			continue
		}
		if !inString && !inRaw && strings.HasPrefix(prefix[i:], "//") {
			// Rest of the line is a Go comment — not a string context.
			return false
		}
		r := prefix[i]
		i++
		if escaped {
			escaped = false
			continue
		}
		switch {
		case r == '`':
			// Backtick toggles a raw string, where " is a literal, not a
			// double-quoted-string delimiter (this is how struct tags appear).
			if !inString {
				inRaw = !inRaw
			}
		case inRaw:
			// nothing toggles while inside a raw string
		case r == '\\':
			if inString {
				escaped = true
			}
		case r == '"':
			inString = !inString
		}
	}
	return inString
}

func TestIsInsideGoDoubleQuotedStringSkipsTemplateActionSyntax(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix string
		want   bool
	}{
		{
			name:   "inside static string",
			prefix: `Short: "`,
			want:   true,
		},
		{
			name:   "inside after template action with escaped quote",
			prefix: `Short: "{{printf "a\"b" .Foo}} `,
			want:   true,
		},
		{
			name:   "outside after template action and closing quote",
			prefix: `Short: "{{printf "a\"b" .Foo}}"`,
			want:   false,
		},
		{
			name:   "inside static escaped quote",
			prefix: `fmt.Println("a\"b`,
			want:   true,
		},
		{
			name:   "outside string",
			prefix: `fmt.Println(`,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isInsideGoDoubleQuotedString(tt.prefix))
		})
	}
}

func goTemplateActionEscapesSpecText(action string) bool {
	return strings.Contains(action, "oneline ") ||
		strings.Contains(action, "printf \"%q\"")
}

func TestGoTemplateActionEscapesSpecTextRejectsRawStringHelper(t *testing.T) {
	t.Parallel()

	assert.True(t, goTemplateActionEscapesSpecText(`{{oneline .Description}}`))
	assert.True(t, goTemplateActionEscapesSpecText(`{{printf "%q" .Description}}`))
	assert.False(t, goTemplateActionEscapesSpecText(`{{goRawSafe .Description}}`))
}

func TestDoctorTemplateRendersKindAwareAuthEnvPresence(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-rich-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "RICH_AUTH_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			{Name: "RICH_AUTH_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "RICH_AUTH_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
			{Name: "RICH_AUTH_COOKIES", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
			{Name: "RICH_AUTH_OPTIONAL_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Optional elevated-read token."},
			{Name: "RICH_AUTH_ALT_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR RICH_AUTH_ALT_KEY."},
			{Name: "RICH_AUTH_ALT_KEY", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR RICH_AUTH_ALT_TOKEN."},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "doctor-rich-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	content := string(doctorSrc)

	require.Contains(t, content, `report["env_vars"] = "ERROR missing required: " + strings.Join(authEnvRequiredMissing, ", ")`)
	require.Contains(t, content, `authEnvInfo = append(authEnvInfo, "RICH_AUTH_CLIENT_ID set during auth login")`)
	require.Contains(t, content, `authEnvInfo = append(authEnvInfo, "RICH_AUTH_COOKIES set with auth set-token")`)
	require.NotContains(t, content, `RICH_AUTH_COOKIES populated automatically by auth login --chrome`)
	require.Contains(t, content, `report["env_vars"] = "INFO set one of: " + strings.Join(authEnvOptionalNames, " or ")`)
}

func TestAuthStatusHintsOnlyRequestCredentialEnvVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("auth-status-rich-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "STATUS_AUTH_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			{Name: "STATUS_AUTH_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "STATUS_AUTH_CLIENT_SECRET", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: true},
			{Name: "STATUS_AUTH_SESSION_COOKIE", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
			{Name: "STATUS_AUTH_OPTIONAL_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "auth-status-rich-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(authSrc)

	start := strings.Index(content, `fmt.Fprintln(w, "Set your token:")`)
	require.NotEqual(t, -1, start, "auth status hint block should be emitted:\n%s", content)
	hintBlock := content[start:]
	end := strings.Index(hintBlock, `auth set-token <token>`)
	require.NotEqual(t, -1, end, "auth set-token fallback should terminate status hint block:\n%s", hintBlock)
	hintBlock = hintBlock[:end]

	require.Contains(t, hintBlock, `export STATUS_AUTH_TOKEN=\"your-token-here\"`)
	require.Contains(t, hintBlock, `export STATUS_AUTH_OPTIONAL_TOKEN=\"your-token-here\"`)
	require.NotContains(t, hintBlock, `STATUS_AUTH_CLIENT_ID`)
	require.NotContains(t, hintBlock, `STATUS_AUTH_CLIENT_SECRET`)
	require.NotContains(t, hintBlock, `STATUS_AUTH_SESSION_COOKIE`)
}

func TestAuthStatusHintsUseDescriptionAndNameShapePlaceholders(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("auth-status-placeholders")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{
				Name:        "FRESHSERVICE_DOMAIN",
				Kind:        spec.AuthEnvVarKindPerCall,
				Required:    true,
				Sensitive:   false,
				Description: "Tenant domain (for example acme.freshservice.com), not your Freshworks org dashboard URL.",
			},
			{Name: "SEC_EDGAR_USER_AGENT", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "DOMINOS_USERNAME", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "GOOGLE_ADS_LOGIN_CUSTOMER_ID", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: false},
			{Name: "SENTRY_AUTH_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "auth-status-placeholders-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	content := string(authSrc)

	require.Contains(t, content, `export FRESHSERVICE_DOMAIN=\"acme.freshservice.com\" # Tenant domain (for example acme.freshservice.com), not your Freshworks org dashboard URL.`)
	require.Contains(t, content, `export SEC_EDGAR_USER_AGENT=\"you@example.com (Your Tool Name)\"`)
	require.Contains(t, content, `export DOMINOS_USERNAME=\"your-username\"`)
	require.Contains(t, content, `export GOOGLE_ADS_LOGIN_CUSTOMER_ID=\"your-token-here\"`)
	require.Contains(t, content, `export SENTRY_AUTH_TOKEN=\"your-token-here\"`)
}

// An auth env var description containing a double-quote or backslash must not
// break the generated auth.go: the hint is embedded in a Go string literal, so
// the value has to be neutralized. Regression guard for the #1329 fix.
func TestAuthStatusHintEscapesAdversarialDescription(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("auth-status-hint-escape")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{
				Name:        "QUOTED_TOKEN",
				Kind:        spec.AuthEnvVarKindPerCall,
				Required:    true,
				Sensitive:   true,
				Description: `Use the "API key" from C:\creds (see docs).`,
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "auth-status-hint-escape-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	authPath := filepath.Join(outputDir, "internal", "cli", "auth.go")
	authSrc, err := os.ReadFile(authPath)
	require.NoError(t, err)

	// The generated source must parse — a raw " or \ in the hint would produce
	// a string-literal syntax error.
	_, err = parser.ParseFile(token.NewFileSet(), authPath, authSrc, parser.AllErrors)
	require.NoError(t, err, "generated auth.go with an adversarial hint description must compile")

	content := string(authSrc)
	assert.NotContains(t, content, `"API key"`,
		"raw double-quotes from the description must be neutralized in the generated literal")
}

func TestMCPContextOmitsHarvestedAuthEnvVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-rich-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "MCP_AUTH_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			{Name: "MCP_AUTH_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "MCP_AUTH_COOKIES", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "mcp-rich-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	mcpSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	content := string(mcpSrc)

	require.Regexp(t, `"name":\s+"MCP_AUTH_TOKEN"`, content)
	require.Regexp(t, `"name":\s+"MCP_AUTH_CLIENT_ID"`, content)
	require.NotContains(t, content, "MCP_AUTH_COOKIES")
}

func TestAgentContextOmitsHarvestedAuthEnvVars(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("agent-context-rich-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type: "api_key",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "AGENT_CONTEXT_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: true, Sensitive: true},
			{Name: "AGENT_CONTEXT_CLIENT_ID", Kind: spec.AuthEnvVarKindAuthFlowInput, Required: false, Sensitive: false},
			{Name: "AGENT_CONTEXT_COOKIES", Kind: spec.AuthEnvVarKindHarvested, Required: false, Sensitive: true},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "agent-context-rich-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	agentContextSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "agent_context.go"))
	require.NoError(t, err)
	content := string(agentContextSrc)

	require.Regexp(t, `Name:\s+"AGENT_CONTEXT_TOKEN"`, content)
	require.Regexp(t, `Name:\s+"AGENT_CONTEXT_CLIENT_ID"`, content)
	require.NotContains(t, content, "AGENT_CONTEXT_COOKIES")
}

func TestConfigTemplateRendersAuthHeaderORCaseFanOut(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("slack-auth")
	apiSpec.Auth = spec.AuthConfig{
		Type: "bearer_token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "SLACK_BOT_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR SLACK_USER_TOKEN."},
			{Name: "SLACK_USER_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR SLACK_BOT_TOKEN."},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "slack-auth-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(configSrc)

	require.True(t,
		strings.Contains(content, "if c.SlackBotToken != \"\"") &&
			strings.Contains(content, "return \"Bearer \" + c.SlackBotToken"),
		"generated AuthHeader should read SLACK_BOT_TOKEN:\n%s", content)
	require.True(t,
		strings.Contains(content, "if c.SlackUserToken != \"\"") &&
			strings.Contains(content, "return \"Bearer \" + c.SlackUserToken"),
		"generated AuthHeader should fall back to SLACK_USER_TOKEN:\n%s", content)
}

func TestAuthHeaderBearerORCaseFallsThroughToAccessToken(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("slack-auth-token")
	apiSpec.Auth = spec.AuthConfig{
		Type: "bearer_token",
		EnvVarSpecs: []spec.AuthEnvVar{
			{Name: "SLACK_BOT_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR SLACK_USER_TOKEN."},
			{Name: "SLACK_USER_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR SLACK_BOT_TOKEN."},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "slack-auth-token-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = outputDir
	out, err := tidy.CombinedOutput()
	require.NoError(t, err, string(out))

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = outputDir
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	content := string(configSrc)

	fanOutIdx := strings.Index(content, `if c.SlackUserToken != ""`)
	accessTokenIdx := strings.Index(content, `if c.AccessToken != ""`)
	require.NotEqual(t, -1, fanOutIdx, "generated AuthHeader should include OR-case fan-out:\n%s", content)
	require.NotEqual(t, -1, accessTokenIdx, "generated AuthHeader should include AccessToken fallback:\n%s", content)
	assert.Less(t, fanOutIdx, accessTokenIdx, "AccessToken fallback should remain reachable after OR fan-out")
	require.NotContains(t, content[fanOutIdx:accessTokenIdx], `return ""`)
}

func TestAuthHeaderTokenEnvVarsDoNotEmitDuplicateMapKeys(t *testing.T) {
	t.Parallel()

	orTokenEnvVars := []spec.AuthEnvVar{
		{Name: "PRIMARY_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR SECONDARY_TOKEN."},
		{Name: "SECONDARY_TOKEN", Kind: spec.AuthEnvVarKindPerCall, Required: false, Sensitive: true, Description: "Set this OR PRIMARY_TOKEN."},
	}

	tests := []struct {
		name string
		auth spec.AuthConfig
	}{
		{
			name: "bearer-canonical-token",
			auth: spec.AuthConfig{
				Type:    "bearer_token",
				Header:  "Authorization",
				Format:  "Bearer {token}",
				EnvVars: []string{"CANONICAL_TOKEN"},
			},
		},
		{
			name: "bearer-or-token",
			auth: spec.AuthConfig{
				Type:        "bearer_token",
				Header:      "Authorization",
				Format:      "Bearer {token}",
				EnvVarSpecs: orTokenEnvVars,
			},
		},
		{
			name: "api-key-or-token",
			auth: spec.AuthConfig{
				Type:        "api_key",
				Header:      "Authorization",
				Format:      "Bearer {token}",
				EnvVarSpecs: orTokenEnvVars,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.name)
			apiSpec.Auth = tt.auth

			outputDir := filepath.Join(t.TempDir(), tt.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())
			runGoCommand(t, outputDir, "test", "./internal/config")
		})
	}
}

// TestAuthHeaderComposedAndCookieApplySchemePrefix pins the fix for #1419:
// composed and cookie auth types must apply the spec's HeaderPrefix (defaulting
// to "Bearer ") when emitting AuthHeader(), so env-var and chrome-composed
// token paths don't return raw tokens that the upstream API will reject as
// "Authorization: <raw>" instead of "Authorization: Bearer <raw>".
func TestAuthHeaderComposedAndCookieApplySchemePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		authType   string
		envVar     string
		envField   string
		authSource string
	}{
		{
			name:       "composed-single-env-var",
			authType:   "composed",
			envVar:     "SUNO_TOKEN",
			envField:   "SunoToken",
			authSource: "chrome-composed",
		},
		{
			name:       "cookie-single-env-var",
			authType:   "cookie",
			envVar:     "NOTION_TOKEN",
			envField:   "NotionToken",
			authSource: "browser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.name)
			apiSpec.Auth = spec.AuthConfig{
				Type:    tt.authType,
				Header:  "Authorization",
				EnvVars: []string{tt.envVar},
			}

			outputDir := filepath.Join(t.TempDir(), tt.name+"-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
			require.NoError(t, err)
			content := string(configSrc)

			// The raw-return literal must appear nowhere in the emitted config — every
			// composed/cookie return path now flows through ensureAuthScheme. Whole-file
			// NotContains avoids depending on a fragile slice boundary.
			require.NotContains(t, content, "return c."+tt.envField+"\n",
				"%s auth must not return raw env-var token without scheme prefix:\n%s", tt.authType, content)
			require.NotContains(t, content, "return c.AccessToken\n",
				"%s auth must not return raw AccessToken without scheme prefix:\n%s", tt.authType, content)

			// The replacement ensureAuthScheme call must be the wrapper for both paths.
			require.Contains(t, content,
				`return ensureAuthScheme("Bearer", c.`+tt.envField+")",
				"%s auth env-var path should wrap return in ensureAuthScheme:\n%s", tt.authType, content)
			require.Contains(t, content,
				`return ensureAuthScheme("Bearer", c.AccessToken)`,
				"%s auth AccessToken path should wrap return in ensureAuthScheme:\n%s", tt.authType, content)

			// Confirm AuthSource label preserved (no behavior regression on which branch we took).
			require.Contains(t, content, `c.AuthSource = "`+tt.authSource+`"`)

			// Generated config must compile and pass its own tests.
			runGoCommand(t, outputDir, "test", "./internal/config")
		})
	}
}

// TestAuthHeaderSchemeHelperHandlesPreprefixedTokens compiles a composed-auth
// and a cookie-auth CLI and exercises the emitted ensureAuthScheme helper
// through AuthHeader() to confirm the positive (Bearer prefix applied) and
// negative (no double prefix when the user pre-prefixes the env var) cases
// for both auth types. The Bearer prefix here comes from HeaderPrefix()'s
// default (the composed and cookie branches do not consult Auth.Format), so
// the test specs intentionally omit Format.
func TestAuthHeaderSchemeHelperHandlesPreprefixedTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		authType string
		envVar   string
		envField string
	}{
		{name: "composed", authType: "composed", envVar: "FOO_TOKEN", envField: "FooToken"},
		{name: "cookie", authType: "cookie", envVar: "BAR_TOKEN", envField: "BarToken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiSpec := minimalSpec(tt.name + "-auth-runtime")
			apiSpec.Auth = spec.AuthConfig{
				Type:    tt.authType,
				Header:  "Authorization",
				EnvVars: []string{tt.envVar},
			}

			outputDir := filepath.Join(t.TempDir(), tt.name+"-auth-runtime-pp-cli")
			require.NoError(t, New(apiSpec, outputDir).Generate())

			testFile := filepath.Join(outputDir, "internal", "config", "auth_scheme_runtime_test.go")
			testSrc := `package config

import "testing"

func TestEnsureAuthSchemeAppliesBearerPrefix(t *testing.T) {
	c := &Config{` + tt.envField + `: "eyJxxx"}
	if got := c.AuthHeader(); got != "Bearer eyJxxx" {
		t.Fatalf("expected Bearer-prefixed header, got %q", got)
	}
	if c.AuthSource != "env:` + tt.envVar + `" {
		t.Fatalf("AuthSource = %q, want env:` + tt.envVar + `", c.AuthSource)
	}
}

func TestEnsureAuthSchemeDoesNotDoublePrefix(t *testing.T) {
	c := &Config{` + tt.envField + `: "Bearer eyJxxx"}
	if got := c.AuthHeader(); got != "Bearer eyJxxx" {
		t.Fatalf("expected single Bearer prefix, got %q", got)
	}
}

func TestEnsureAuthSchemeBlankReturnsEmpty(t *testing.T) {
	c := &Config{}
	if got := c.AuthHeader(); got != "" {
		t.Fatalf("expected empty header for blank config, got %q", got)
	}
}
`
			require.NoError(t, os.WriteFile(testFile, []byte(testSrc), 0o644))
			runGoCommand(t, outputDir, "test", "./internal/config")
		})
	}
}

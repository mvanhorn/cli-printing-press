package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	apispec "github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

type liveDogfoodRefreshSeed struct {
	stripEnv     []string
	mirror       *liveDogfoodCredentialMirror
	seededEnvVar string
}

func seedLiveDogfoodRotatingRefresh(scopedHome, cliName string, manifest CLIManifest, authEnv string) (liveDogfoodRefreshSeed, error) {
	strip := liveDogfoodRotatingRefreshEnvVars(manifest, authEnv)
	if len(strip) == 0 {
		return liveDogfoodRefreshSeed{}, nil
	}

	token, tokenEnv := liveDogfoodRotatingRefreshToken(authEnv, strip)
	seed := liveDogfoodRefreshSeed{stripEnv: strip, seededEnvVar: tokenEnv}
	if scopedHome == "" || strings.TrimSpace(cliName) == "" {
		return seed, nil
	}

	dst := liveDogfoodCredentialsRelPath(scopedHome, cliName)
	existing, readErr := os.ReadFile(dst)
	if readErr != nil && !os.IsNotExist(readErr) {
		return liveDogfoodRefreshSeed{}, fmt.Errorf("oauth2_refresh live dogfood needs a writable shared credential store: reading %s: %w", dst, readErr)
	}
	if liveDogfoodFileHasRefreshToken(existing) {
		return seed, nil
	}
	if token == "" {
		return seed, nil
	}

	created := os.IsNotExist(readErr)
	data := liveDogfoodAppendRefreshToken(existing, token)
	if err := writeLiveDogfoodCredentialMirrorFile(dst, data, 0o600); err != nil {
		return liveDogfoodRefreshSeed{}, fmt.Errorf("oauth2_refresh live dogfood needs a writable shared credential store: %w", err)
	}
	if created {
		if operatorPath := liveDogfoodOperatorCredentialsPath(cliName); operatorPath != "" {
			seed.mirror = &liveDogfoodCredentialMirror{
				src:         operatorPath,
				dst:         dst,
				mode:        0o600,
				allowCreate: true,
			}
		}
	}
	return seed, nil
}

func liveDogfoodAppendRefreshToken(existing []byte, token string) []byte {
	line := liveDogfoodRefreshTokenTOML(token)
	if len(existing) == 0 {
		return line
	}
	out := append([]byte{}, existing...)
	if out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return append(out, line...)
}

func liveDogfoodCredentialsRelPath(home, cliName string) string {
	return filepath.Join(home, ".local", "share", cliName, "credentials.toml")
}

func liveDogfoodOperatorCredentialsPath(cliName string) string {
	dir := liveDogfoodOperatorDataDir(cliName)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "credentials.toml")
}

func liveDogfoodOperatorDataDir(cliName string) string {
	cliName = strings.TrimSpace(cliName)
	if cliName == "" {
		return ""
	}
	prefix := naming.EnvPrefix(naming.TrimCLISuffix(cliName))
	if prefix != "" {
		if dir := liveDogfoodAbsoluteEnvDir(prefix + "_DATA_DIR"); dir != "" {
			return dir
		}
		if home := liveDogfoodAbsoluteEnvDir(prefix + "_HOME"); home != "" {
			return filepath.Join(home, "data")
		}
	}
	if xdg := liveDogfoodAbsoluteEnvDir("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, cliName)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", cliName)
}

func liveDogfoodAbsoluteEnvDir(name string) string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return ""
	}
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		if raw == "~" {
			raw = home
		} else {
			raw = filepath.Join(home, strings.TrimPrefix(raw, "~/"))
		}
	}
	if !filepath.IsAbs(raw) {
		return ""
	}
	return filepath.Clean(raw)
}

func liveDogfoodRotatingRefreshEnvVars(manifest CLIManifest, authEnv string) []string {
	if !strings.EqualFold(strings.TrimSpace(manifest.AuthType), apispec.AuthTypeOAuth2Refresh) {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	add := func(name string, requireRefreshShape bool) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		if requireRefreshShape && !apispec.IsOAuthRefreshTokenEnvVar(name) {
			return
		}
		seen[name] = true
		names = append(names, name)
	}

	auth := apispec.AuthConfig{
		Type:        manifest.AuthType,
		EnvVars:     append([]string(nil), manifest.AuthEnvVars...),
		EnvVarSpecs: append([]apispec.AuthEnvVar(nil), manifest.AuthEnvVarSpecs...),
	}
	if ev := auth.OAuth2RefreshTokenEnvVar(); ev != nil {
		add(ev.Name, false)
	}
	for _, name := range manifest.AuthEnvVars {
		add(name, true)
	}
	for _, ev := range manifest.AuthEnvVarSpecs {
		add(ev.Name, true)
	}
	add(authEnv, true)
	return names
}

func liveDogfoodRotatingRefreshToken(authEnv string, strip []string) (token, envName string) {
	authEnv = strings.TrimSpace(authEnv)
	names := make([]string, 0, len(strip)+1)
	if apispec.IsOAuthRefreshTokenEnvVar(authEnv) {
		names = append(names, authEnv)
	}
	for _, name := range strip {
		if slices.Contains(names, name) {
			continue
		}
		names = append(names, name)
	}
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v, name
		}
	}
	return "", ""
}

func liveDogfoodFileHasRefreshToken(data []byte) bool {
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(strings.Trim(key, `"`)) != "refresh_token" {
			continue
		}
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		return val != ""
	}
	return false
}

func liveDogfoodRefreshTokenTOML(token string) []byte {
	return []byte("refresh_token = " + tomlQuotedString(token) + "\n")
}

func tomlQuotedString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r == utf8.RuneError && size == 1 {
				b.WriteString(`\u00`)
				b.WriteByte("0123456789abcdef"[s[i-1]>>4])
				b.WriteByte("0123456789abcdef"[s[i-1]&0x0f])
				continue
			}
			if r < 0x20 || r == 0x7f {
				if r <= unicode.MaxASCII {
					b.WriteString(`\u00`)
					b.WriteByte("0123456789abcdef"[byte(r)>>4])
					b.WriteByte("0123456789abcdef"[byte(r)&0x0f])
					continue
				}
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

type invalidGrantCascadeTracker struct {
	sawLivePass bool
}

func (c *invalidGrantCascadeTracker) observe(results []LiveDogfoodTestResult) bool {
	if c == nil {
		return false
	}
	for _, result := range results {
		if c.sawLivePass && liveDogfoodResultHasInvalidGrant(result) {
			return true
		}
		if liveDogfoodResultIsLiveAPIPass(result) {
			c.sawLivePass = true
		}
	}
	return false
}

func liveDogfoodResultIsLiveAPIPass(result LiveDogfoodTestResult) bool {
	if result.Status != LiveDogfoodStatusPass {
		return false
	}
	if result.Kind != LiveDogfoodTestHappy && result.Kind != LiveDogfoodTestJSON {
		return false
	}
	if slices.Contains(result.Args, "--dry-run") {
		return false
	}
	return true
}

func liveDogfoodResultHasInvalidGrant(result LiveDogfoodTestResult) bool {
	if result.Status != LiveDogfoodStatusFail {
		return false
	}
	hay := strings.ToLower(result.Reason + " " + result.OutputSample)
	return strings.Contains(hay, "invalid_grant")
}

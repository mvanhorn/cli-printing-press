package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

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
		if operatorHome, homeErr := os.UserHomeDir(); homeErr == nil && operatorHome != "" {
			seed.mirror = &liveDogfoodCredentialMirror{
				src:         liveDogfoodCredentialsRelPath(operatorHome, cliName),
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

func liveDogfoodRotatingRefreshEnvVars(manifest CLIManifest, authEnv string) []string {
	if !strings.EqualFold(strings.TrimSpace(manifest.AuthType), apispec.AuthTypeOAuth2Refresh) {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
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
		add(ev.Name)
	}
	for _, name := range manifest.AuthEnvVars {
		if apispec.IsOAuthRefreshTokenEnvVar(name) {
			add(name)
		}
	}
	for _, ev := range manifest.AuthEnvVarSpecs {
		if apispec.IsOAuthRefreshTokenEnvVar(ev.Name) {
			add(ev.Name)
		}
	}
	add(authEnv)
	return names
}

func liveDogfoodRotatingRefreshToken(authEnv string, strip []string) (token, envName string) {
	authEnv = strings.TrimSpace(authEnv)
	if authEnv != "" {
		if v := strings.TrimSpace(os.Getenv(authEnv)); v != "" {
			return v, authEnv
		}
	}
	for _, name := range strip {
		if name == authEnv {
			continue
		}
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v, name
		}
	}
	return "", ""
}

func liveDogfoodFileHasRefreshToken(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
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
	for _, arg := range result.Args {
		if arg == "--dry-run" {
			return false
		}
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

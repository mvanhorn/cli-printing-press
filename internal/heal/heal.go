package heal

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
)

// DiagnosisClass is a typed categorical classification for `pp heal` results.
type DiagnosisClass string

const (
	DiagHealthy         DiagnosisClass = "healthy"
	DiagSkippedMutating DiagnosisClass = "skipped_mutating"
	DiagAuthError       DiagnosisClass = DiagnosisClass(cartridge.ClassAuthError)
	DiagRateLimited     DiagnosisClass = DiagnosisClass(cartridge.ClassRateLimited)
	DiagUpstreamDrift   DiagnosisClass = "upstream_drift"
	DiagSchemaDrift     DiagnosisClass = "schema_drift"
	DiagUnknown         DiagnosisClass = "unknown"
)

// Diagnosis classifies why a cartridge command is failing based on journal
// history and a live read-only probe.
type Diagnosis struct {
	Cartridge   string         `json:"cartridge"`
	Command     string         `json:"command"`
	Class       DiagnosisClass `json:"class"`
	HTTPStatus  int            `json:"http_status,omitempty"`
	Detail      string         `json:"detail"`
	RecentFails int            `json:"recent_fails"`
	NextAction  string         `json:"next_action"`
	Hints       []string       `json:"hints,omitempty"`
}

// PatchResult reports the outcome of applying a surgical cartridge heal patch.
type PatchResult struct {
	Cartridge   string   `json:"cartridge"`
	Command     string   `json:"command"`
	Changes     []string `json:"changes"`
	UpdatedPath string   `json:"updated_path,omitempty"`
	NewSHA256   string   `json:"new_sha256,omitempty"`
	Verified    bool     `json:"verified"`
	PRBundleDir string   `json:"pr_bundle_dir,omitempty"`
}

// Diagnose probes a cartridge command (read-only) and inspects journal history
// so `pp heal` separates expired auth from genuine upstream endpoint drift.
func Diagnose(ctx context.Context, c *cartridge.Cartridge, cmdName string, journal *cartridge.Journal, opts cartridge.RunOptions) (*Diagnosis, error) {
	if c == nil {
		return nil, errors.New("cartridge is required")
	}
	if _, ok := c.Commands[cmdName]; !ok {
		return nil, fmt.Errorf("unknown command %q on cartridge %q", cmdName, c.Name)
	}
	diag := &Diagnosis{
		Cartridge: c.Name,
		Command:   cmdName,
	}
	if journal != nil {
		recent, _ := journal.Recent(c.Name, cmdName, 10)
		for _, e := range recent {
			if e.Status != "ok" {
				diag.RecentFails++
			}
		}
	}

	if c.IsMutating(cmdName) {
		diag.Class = DiagSkippedMutating
		diag.Detail = "command mutates remote state; live probe skipped for safety"
		diag.NextAction = "supply a fresh HAR capture via `pp heal <app> <cmd> --from-har <file>`"
		return diag, nil
	}

	opts.DryRun = false
	opts.ConfirmWrite = false
	_, err := cartridge.Execute(ctx, c, cmdName, opts)
	if err == nil {
		diag.Class = DiagHealthy
		diag.Detail = "live read-only probe succeeded"
		diag.NextAction = "no heal required"
		return diag, nil
	}

	if ee, ok := errors.AsType[*cartridge.ExecError](err); ok {
		diag.HTTPStatus = ee.HTTPStatus
		diag.Detail = ee.Message
		switch ee.Class {
		case cartridge.ClassAuthError:
			diag.Class = DiagAuthError
			diag.NextAction = "refresh your browser session / agentcookie or API token (endpoint itself did not drift)"
		case cartridge.ClassRateLimited:
			diag.Class = DiagRateLimited
			diag.NextAction = "wait for rate-limit window to reset"
		case cartridge.ClassDrift:
			if strings.Contains(ee.Message, "extract path") {
				diag.Class = DiagSchemaDrift
			} else {
				diag.Class = DiagUpstreamDrift
			}
			diag.NextAction = fmt.Sprintf("re-sniff with `pp heal %s %s --backend ardia` or `--from-har <capture.har>`", c.Name, cmdName)
		default:
			diag.Class = DiagUnknown
			diag.NextAction = fmt.Sprintf("inspect error and re-record with `pp heal %s %s --from-har <file>`", c.Name, cmdName)
		}
		return diag, nil
	}

	diag.Class = DiagUnknown
	diag.Detail = err.Error()
	diag.NextAction = "re-record endpoint via --backend ardia or --from-har"
	return diag, nil
}

// ApplySurgicalPatch merges updated endpoint URL, headers, persisted query, or
// extract path from fresh onto target for a single command, re-pins the
// cartridge in ~/.pp/lock.json, and optionally emits a PII-scrubbed PR bundle.
func ApplySurgicalPatch(target, fresh *cartridge.Cartridge, cmdName string, prBundleDir string) (*PatchResult, error) {
	if target == nil || fresh == nil {
		return nil, errors.New("both target and fresh cartridges are required")
	}
	oldCmd, ok := target.Commands[cmdName]
	if !ok {
		return nil, fmt.Errorf("command %q not found in target cartridge %q", cmdName, target.Name)
	}
	newCmd, err := matchFreshCommand(fresh, cmdName, oldCmd)
	if err != nil {
		return nil, err
	}

	wasMutating := target.IsMutating(cmdName)
	newMethod := strings.ToUpper(strings.TrimSpace(newCmd.Method))
	if newMethod == "" {
		newMethod = strings.ToUpper(strings.TrimSpace(oldCmd.Method))
	}
	becomesMutating := newCmd.Mutates || isMutatingHTTPMethod(newMethod)
	if !wasMutating && becomesMutating {
		return nil, fmt.Errorf("refusing to heal read-only command %q into mutating %s request; inspect capture and update cartridge manually", cmdName, newMethod)
	}

	// Validate host boundary for the specific replacement command only.
	var newHost string
	if newCmd.URL != "" {
		parsedNew, parseErr := url.Parse(newCmd.URL)
		if parseErr != nil || parsedNew.Hostname() == "" {
			return nil, fmt.Errorf("replacement command URL %q is invalid", newCmd.URL)
		}
		newHost = strings.ToLower(parsedNew.Hostname())
		if !target.HostAllowed(newHost) {
			authStrat := strings.ToLower(strings.TrimSpace(target.Auth.Strategy))
			if authStrat != "" && authStrat != cartridge.AuthStrategyNone {
				return nil, fmt.Errorf("refusing to move authenticated command %q to unpinned host %q; update trust.allowed_hosts manually after review", cmdName, newHost)
			}
		}
	}

	var changes []string
	var scrubbedChanges []string
	if newCmd.URL != "" && newCmd.URL != oldCmd.URL {
		changes = append(changes, fmt.Sprintf("url: %s -> %s", oldCmd.URL, newCmd.URL))
		scrubbedChanges = append(scrubbedChanges, fmt.Sprintf("url: %s -> %s", scrubURLSecrets(oldCmd.URL), scrubURLSecrets(newCmd.URL)))
		oldCmd.URL = newCmd.URL
	}
	if newCmd.Method != "" && !strings.EqualFold(newCmd.Method, oldCmd.Method) {
		changes = append(changes, fmt.Sprintf("method: %s -> %s", oldCmd.Method, newMethod))
		scrubbedChanges = append(scrubbedChanges, fmt.Sprintf("method: %s -> %s", oldCmd.Method, newMethod))
		oldCmd.Method = newMethod
	}
	if becomesMutating {
		if !oldCmd.Mutates {
			oldCmd.Mutates = true
			changes = append(changes, "mutates: false -> true")
			scrubbedChanges = append(scrubbedChanges, "mutates: false -> true")
		}
		if !slices.Contains(target.Trust.Mutates, cmdName) {
			target.Trust.Mutates = append(target.Trust.Mutates, cmdName)
		}
		target.Trust.ReadOnly = false
	}
	if newCmd.PersistedQuery != "" && newCmd.PersistedQuery != oldCmd.PersistedQuery {
		changes = append(changes, fmt.Sprintf("persisted_query: %s -> %s", oldCmd.PersistedQuery, newCmd.PersistedQuery))
		scrubbedChanges = append(scrubbedChanges, fmt.Sprintf("persisted_query: %s -> %s", oldCmd.PersistedQuery, newCmd.PersistedQuery))
		oldCmd.PersistedQuery = newCmd.PersistedQuery
	}
	if newCmd.Extract != "" && newCmd.Extract != oldCmd.Extract {
		changes = append(changes, fmt.Sprintf("extract: %s -> %s", oldCmd.Extract, newCmd.Extract))
		scrubbedChanges = append(scrubbedChanges, fmt.Sprintf("extract: %s -> %s", oldCmd.Extract, newCmd.Extract))
		oldCmd.Extract = newCmd.Extract
	}
	if newHost != "" && !target.HostAllowed(newHost) {
		target.Trust.AllowedHosts = append(target.Trust.AllowedHosts, newHost)
		changes = append(changes, fmt.Sprintf("trust.allowed_hosts: +%s", newHost))
		scrubbedChanges = append(scrubbedChanges, fmt.Sprintf("trust.allowed_hosts: +%s", newHost))
	}

	if len(changes) == 0 {
		return &PatchResult{
			Cartridge: target.Name,
			Command:   cmdName,
			Changes:   []string{"no structural changes detected"},
		}, nil
	}

	target.Commands[cmdName] = oldCmd
	raw, err := target.MarshalYAML()
	if err != nil {
		return nil, err
	}
	updated, path, err := cartridge.InstallBytes(raw, "heal:"+target.Name)
	if err != nil {
		return nil, err
	}

	res := &PatchResult{
		Cartridge:   updated.Name,
		Command:     cmdName,
		Changes:     changes,
		UpdatedPath: path,
		NewSHA256:   updated.SHA256,
	}

	if prBundleDir != "" {
		if err := writePRBundle(prBundleDir, updated, scrubbedChanges); err != nil {
			return nil, err
		}
		res.PRBundleDir = prBundleDir
	}
	return res, nil
}

func isMutatingHTTPMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

// matchFreshCommand finds an exact or unique unambiguous replacement command in
// the fresh capture. Missing or ambiguous captures fail closed rather than
// picking an unrelated endpoint.
func matchFreshCommand(fresh *cartridge.Cartridge, cmdName string, oldCmd cartridge.CommandSpec) (cartridge.CommandSpec, error) {
	if cmd, ok := fresh.Commands[cmdName]; ok {
		return cmd, nil
	}
	lowerTarget := strings.ToLower(strings.TrimSpace(cmdName))
	var matches []string
	for name := range fresh.Commands {
		if strings.Contains(strings.ToLower(name), lowerTarget) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 1 {
		return fresh.Commands[matches[0]], nil
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return cartridge.CommandSpec{}, fmt.Errorf("ambiguous fresh capture for %q (matched commands: %v)", cmdName, matches)
	}
	// If the fresh capture contains exactly one command on the same host as the
	// target command, treat it as an unambiguous single-endpoint re-recording.
	if len(fresh.Commands) == 1 && oldCmd.URL != "" {
		oldParsed, err1 := url.Parse(oldCmd.URL)
		for _, cmd := range fresh.Commands {
			newParsed, err2 := url.Parse(cmd.URL)
			if err1 == nil && err2 == nil && strings.EqualFold(oldParsed.Hostname(), newParsed.Hostname()) {
				return cmd, nil
			}
		}
	}
	return cartridge.CommandSpec{}, fmt.Errorf("no matching command found in fresh capture for %q", cmdName)
}

func writePRBundle(dir string, c *cartridge.Cartridge, scrubbedChanges []string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	scrubbed, err := scrubCartridgeForExport(c)
	if err != nil {
		return err
	}
	scrubbedYAML, err := scrubbed.MarshalYAML()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "cartridge.yaml"), scrubbedYAML, 0o600); err != nil {
		return err
	}
	var md strings.Builder
	fmt.Fprintf(&md, "# Auto-Heal Patch: %s\n\n", c.Name)
	fmt.Fprintf(&md, "SHA-256: `%s`\n\n## Changes\n", c.SHA256)
	for _, ch := range scrubbedChanges {
		fmt.Fprintf(&md, "- %s\n", ch)
	}
	fmt.Fprintf(&md, "\nGenerated by `pp heal` (secret headers and credential query parameters scrubbed).\n")
	return os.WriteFile(filepath.Join(dir, "PR_BODY.md"), []byte(md.String()), 0o600)
}

// scrubCartridgeForExport produces a clean deep copy of c with sensitive
// headers and credential-bearing URL components stripped before sharing.
func scrubCartridgeForExport(c *cartridge.Cartridge) (*cartridge.Cartridge, error) {
	raw, err := c.MarshalYAML()
	if err != nil {
		return nil, err
	}
	clone, err := cartridge.Parse(raw, "export:"+c.Name)
	if err != nil {
		return nil, err
	}
	for name, cmd := range clone.Commands {
		if cmd.URL != "" {
			cmd.URL = scrubURLSecrets(cmd.URL)
		}
		if len(cmd.Headers) > 0 {
			cleanHeaders := make(map[string]string)
			for k, v := range cmd.Headers {
				if isSensitiveHeader(k) {
					continue
				}
				cleanHeaders[k] = v
			}
			cmd.Headers = cleanHeaders
		}
		clone.Commands[name] = cmd
	}
	return clone, nil
}

func isSensitiveHeader(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	sensitiveTokens := []string{
		"authorization",
		"cookie",
		"token",
		"secret",
		"api-key",
		"apikey",
		"session",
		"csrf",
		"xsrf",
		"auth",
		"credential",
		"password",
		"x-key",
	}
	for _, tok := range sensitiveTokens {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

func scrubURLSecrets(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if u.User != nil {
		u.User = nil
	}
	q := u.Query()
	changed := false
	for k := range q {
		lk := strings.ToLower(k)
		if isSensitiveQueryParam(lk) {
			q.Set(k, "REDACTED")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func isSensitiveQueryParam(lowerKey string) bool {
	sensitive := []string{
		"key", "api_key", "apikey", "token", "access_token", "refresh_token",
		"secret", "auth", "sig", "signature", "password", "passwd", "session", "jwt",
	}
	for _, s := range sensitive {
		if lowerKey == s || strings.HasSuffix(lowerKey, "_"+s) || strings.HasPrefix(lowerKey, s+"_") {
			return true
		}
	}
	return false
}

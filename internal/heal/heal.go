package heal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
)

// Diagnosis classifies why a cartridge command is failing based on journal
// history and a live read-only probe.
type Diagnosis struct {
	Cartridge   string   `json:"cartridge"`
	Command     string   `json:"command"`
	Class       string   `json:"class"` // healthy, auth_error, rate_limited, upstream_drift, schema_drift, unknown
	HTTPStatus  int      `json:"http_status,omitempty"`
	Detail      string   `json:"detail"`
	RecentFails int      `json:"recent_fails"`
	NextAction  string   `json:"next_action"`
	Hints       []string `json:"hints,omitempty"`
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
		diag.Class = "skipped_mutating"
		diag.Detail = "command mutates remote state; live probe skipped for safety"
		diag.NextAction = "supply a fresh HAR capture via `pp heal <app> <cmd> --from-har <file>`"
		return diag, nil
	}

	opts.DryRun = false
	opts.ConfirmWrite = false
	_, err := cartridge.Execute(ctx, c, cmdName, opts)
	if err == nil {
		diag.Class = "healthy"
		diag.Detail = "live read-only probe succeeded"
		diag.NextAction = "no heal required"
		return diag, nil
	}

	if ee, ok := errors.AsType[*cartridge.ExecError](err); ok {
		diag.HTTPStatus = ee.HTTPStatus
		diag.Detail = ee.Message
		switch ee.Class {
		case "auth_error":
			diag.Class = "auth_error"
			diag.NextAction = "refresh your browser session / agentcookie or API token (endpoint itself did not drift)"
		case "rate_limited":
			diag.Class = "rate_limited"
			diag.NextAction = "wait for rate-limit window to reset"
		case "drift":
			if strings.Contains(ee.Message, "extract path") {
				diag.Class = "schema_drift"
			} else {
				diag.Class = "upstream_drift"
			}
			diag.NextAction = fmt.Sprintf("re-sniff with `pp heal %s %s --backend ardia` or `--from-har <capture.har>`", c.Name, cmdName)
		default:
			diag.Class = "unknown"
			diag.NextAction = fmt.Sprintf("inspect error and re-record with `pp heal %s %s --from-har <file>`", c.Name, cmdName)
		}
		return diag, nil
	}

	diag.Class = "unknown"
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
	newCmd, ok := matchFreshCommand(fresh, cmdName)
	if !ok {
		return nil, fmt.Errorf("no replacement command found in fresh capture for %q", cmdName)
	}

	var changes []string
	if newCmd.URL != "" && newCmd.URL != oldCmd.URL {
		changes = append(changes, fmt.Sprintf("url: %s -> %s", oldCmd.URL, newCmd.URL))
		oldCmd.URL = newCmd.URL
	}
	if newCmd.Method != "" && !strings.EqualFold(newCmd.Method, oldCmd.Method) {
		changes = append(changes, fmt.Sprintf("method: %s -> %s", oldCmd.Method, newCmd.Method))
		oldCmd.Method = newCmd.Method
	}
	if newCmd.PersistedQuery != "" && newCmd.PersistedQuery != oldCmd.PersistedQuery {
		changes = append(changes, fmt.Sprintf("persisted_query: %s -> %s", oldCmd.PersistedQuery, newCmd.PersistedQuery))
		oldCmd.PersistedQuery = newCmd.PersistedQuery
	}
	if newCmd.Extract != "" && newCmd.Extract != oldCmd.Extract {
		changes = append(changes, fmt.Sprintf("extract: %s -> %s", oldCmd.Extract, newCmd.Extract))
		oldCmd.Extract = newCmd.Extract
	}
	for _, host := range fresh.Trust.AllowedHosts {
		if !target.HostAllowed(host) {
			target.Trust.AllowedHosts = append(target.Trust.AllowedHosts, host)
			changes = append(changes, fmt.Sprintf("trust.allowed_hosts: +%s", host))
		}
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
		if err := writePRBundle(prBundleDir, updated, raw, changes); err != nil {
			return nil, err
		}
		res.PRBundleDir = prBundleDir
	}
	return res, nil
}

func matchFreshCommand(fresh *cartridge.Cartridge, cmdName string) (cartridge.CommandSpec, bool) {
	if cmd, ok := fresh.Commands[cmdName]; ok {
		return cmd, true
	}
	for name, cmd := range fresh.Commands {
		if strings.Contains(strings.ToLower(name), strings.ToLower(cmdName)) {
			return cmd, true
		}
	}
	for _, cmd := range fresh.Commands {
		return cmd, true
	}
	return cartridge.CommandSpec{}, false
}

func writePRBundle(dir string, c *cartridge.Cartridge, yamlBytes []byte, changes []string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "cartridge.yaml"), yamlBytes, 0o600); err != nil {
		return err
	}
	var md strings.Builder
	fmt.Fprintf(&md, "# Auto-Heal Patch: %s\n\n", c.Name)
	fmt.Fprintf(&md, "SHA-256: `%s`\n\n## Changes\n", c.SHA256)
	for _, ch := range changes {
		fmt.Fprintf(&md, "- %s\n", ch)
	}
	fmt.Fprintf(&md, "\nGenerated by `pp heal` (PII and session headers excluded).\n")
	return os.WriteFile(filepath.Join(dir, "PR_BODY.md"), []byte(md.String()), 0o600)
}

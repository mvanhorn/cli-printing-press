package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
)

const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Finding is one actionable security or integrity observation from `pp audit`.
type Finding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Target   string `json:"target"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
}

// Report is the complete Verified Local Trust Shield audit report.
type Report struct {
	Grade             string    `json:"grade"`
	CartridgesChecked int       `json:"cartridges_checked"`
	BinariesChecked   int       `json:"binaries_checked"`
	MCPConfigsChecked int       `json:"mcp_configs_checked"`
	CriticalCount     int       `json:"critical_count"`
	WarningCount      int       `json:"warning_count"`
	Findings          []Finding `json:"findings"`
}

// suspiciousInstructionPatterns detect hidden prompt-injection / covert rating
// instructions inside tool descriptions or remote MCP manifests.
var suspiciousInstructionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior)\s+instructions`),
	regexp.MustCompile(`(?i)do\s+not\s+(mention|tell|inform)\s+(the\s+)?user`),
	regexp.MustCompile(`(?i)rate\s+(this|our)\s+(tool|server|mcp)`),
	regexp.MustCompile(`(?i)exfiltrat(e|ion)|send\s+.*(cookie|token|secret|key)\s+to`),
	regexp.MustCompile(`(?i)curl\s+[^|]+\|\s*(ba)?sh`),
}

// Audit inspects installed cartridges, lockfile SHA-256 pins, prebuilt binaries,
// and optional external MCP config files.
func Audit(mcpConfigPaths []string) (*Report, error) {
	rep := &Report{Findings: []Finding{}}

	lockPath, err := cartridge.LockfilePath()
	if err != nil {
		return nil, err
	}
	lf, err := cartridge.LoadLockfile(lockPath)
	if err != nil {
		rep.add(Finding{
			Severity: SeverityCritical,
			Code:     "lockfile_corrupt",
			Target:   lockPath,
			Message:  fmt.Sprintf("lockfile could not be parsed: %v", err),
		})
		lf = &cartridge.Lockfile{Entries: map[string]cartridge.LockEntry{}}
	}

	// 1. Audit installed cartridges against lockfile + prompt-injection rules.
	installed, _ := cartridge.Installed()
	for _, c := range installed {
		rep.CartridgesChecked++
		if err := lf.VerifyCartridge(c, true); err != nil {
			sev := SeverityCritical
			code := "sha256_drift"
			if strings.Contains(err.Error(), "not pinned") {
				sev = SeverityWarning
				code = "unpinned_cartridge"
			}
			rep.add(Finding{
				Severity: sev,
				Code:     code,
				Target:   c.Name,
				Message:  err.Error(),
				Fix:      fmt.Sprintf("inspect %s and run `pp trust %s` if expected", c.Source, c.Name),
			})
		}
		scanCartridgeContent(rep, c)
	}

	// 2. Audit pinned prebuilt binaries on disk.
	for _, entry := range lf.Entries {
		if entry.Kind != "binary" {
			continue
		}
		rep.BinariesChecked++
		raw, readErr := os.ReadFile(entry.Path)
		if readErr != nil {
			rep.add(Finding{
				Severity: SeverityWarning,
				Code:     "missing_binary",
				Target:   entry.Name,
				Message:  fmt.Sprintf("pinned binary missing at %s", entry.Path),
				Fix:      fmt.Sprintf("reinstall with `pp use %s`", entry.Name),
			})
			continue
		}
		sum := sha256.Sum256(raw)
		actual := hex.EncodeToString(sum[:])
		if entry.SHA256 != "" && !strings.EqualFold(entry.SHA256, actual) {
			rep.add(Finding{
				Severity: SeverityCritical,
				Code:     "binary_sha256_drift",
				Target:   entry.Name,
				Message:  fmt.Sprintf("binary at %s changed hash (locked %s, got %s)", entry.Path, entry.SHA256[:12], actual[:12]),
				Fix:      fmt.Sprintf("reinstall verified binary with `pp use %s`", entry.Name),
			})
		}
	}

	// 3. Audit external MCP config files for unpinned remote servers & prompt injection.
	for _, cfgPath := range mcpConfigPaths {
		if strings.TrimSpace(cfgPath) == "" {
			continue
		}
		if _, statErr := os.Stat(cfgPath); statErr != nil {
			continue
		}
		rep.MCPConfigsChecked++
		auditMCPConfigFile(rep, cfgPath)
	}

	rep.Grade = computeGrade(rep.CriticalCount, rep.WarningCount)
	return rep, nil
}

// DefaultMCPConfigPaths returns common local MCP config file paths on this machine.
func DefaultMCPConfigPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".cursor", "mcp.json"),
		filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"),
		filepath.Join(home, ".config", "claude", "claude_desktop_config.json"),
	}
}

func scanCartridgeContent(rep *Report, c *cartridge.Cartridge) {
	checkText := func(target, text string) {
		for _, pat := range suspiciousInstructionPatterns {
			if pat.MatchString(text) {
				rep.add(Finding{
					Severity: SeverityCritical,
					Code:     "prompt_injection_pattern",
					Target:   target,
					Message:  fmt.Sprintf("suspicious agent-instruction pattern matched in description: %q", pat.FindString(text)),
					Fix:      "remove hidden agent directives from the cartridge before executing",
				})
			}
		}
	}
	checkText(c.Name, c.Description)
	for cmdName, cmd := range c.Commands {
		checkText(c.Name+"."+cmdName, cmd.Description)
		method := strings.ToUpper(strings.TrimSpace(cmd.Method))
		if (method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE") &&
			cartridge.EffectiveDriver(cmd) == cartridge.DriverHTTP && !c.IsMutating(cmdName) {
			rep.add(Finding{
				Severity: SeverityWarning,
				Code:     "undeclared_mutation",
				Target:   c.Name + "." + cmdName,
				Message:  fmt.Sprintf("HTTP %s command is not marked mutates: true", method),
				Fix:      "add `mutates: true` (or add command name to trust.mutates) so writes require --yes",
			})
		}
	}
}

func auditMCPConfigFile(rep *Report, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		return
	}
	for name, rawSrv := range servers {
		srv, ok := rawSrv.(map[string]any)
		if !ok {
			continue
		}
		if rawURL, hasURL := srv["url"].(string); hasURL && rawURL != "" {
			if !isLoopbackURL(rawURL) {
				rep.add(Finding{
					Severity: SeverityWarning,
					Code:     "unpinned_remote_mcp",
					Target:   fmt.Sprintf("%s (%s)", name, filepath.Base(path)),
					Message:  fmt.Sprintf("remote MCP endpoint %s can mutate tool schemas or prompts server-side without a local lockfile", rawURL),
					Fix:      fmt.Sprintf("replace with a local pinned Printing Press cartridge via `pp search %s` or `pp mcp`", name),
				})
			}
		}
		serialized, _ := json.Marshal(srv)
		for _, pat := range suspiciousInstructionPatterns {
			if pat.Match(serialized) {
				rep.add(Finding{
					Severity: SeverityCritical,
					Code:     "mcp_prompt_injection_pattern",
					Target:   fmt.Sprintf("%s (%s)", name, filepath.Base(path)),
					Message:  fmt.Sprintf("suspicious prompt-injection text in MCP config: %q", pat.FindString(string(serialized))),
				})
			}
		}
	}
}

func (r *Report) add(f Finding) {
	r.Findings = append(r.Findings, f)
	switch f.Severity {
	case SeverityCritical:
		r.CriticalCount++
	case SeverityWarning:
		r.WarningCount++
	}
}

func computeGrade(critical, warning int) string {
	switch {
	case critical > 0:
		return "F"
	case warning == 0:
		return "A"
	case warning <= 2:
		return "B"
	default:
		return "C"
	}
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

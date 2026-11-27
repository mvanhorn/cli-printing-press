package ppcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/lake"
	"github.com/mvanhorn/cli-printing-press/v4/internal/onboard"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pressauth"
	"github.com/mvanhorn/cli-printing-press/v4/internal/registry"
	"github.com/mvanhorn/cli-printing-press/v4/internal/sessionbridge"
	"github.com/spf13/cobra"
)

var (
	loadPressAuthState = pressauth.Load
	savePressAuthState = pressauth.Save
)

func newOnboardCmd() *cobra.Command {
	var browserFilter string
	var install bool
	var limit int
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Detect which of the 616+ Printing Press apps you already use in Ardia, Chrome, or Firefox (plaintext domains only, zero Keychain prompts)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sources := onboard.DefaultBrowserSources(browserFilter)
			signals, err := onboard.ScanLocalDomains(sources)
			if err != nil {
				return err
			}
			reg, err := registry.NewClient()
			if err != nil {
				return err
			}
			cat, err := reg.Load(cmd.Context(), false)
			if err != nil {
				return err
			}
			installedCarts, _ := cartridge.Installed()
			builtinCarts, _ := cartridge.Builtins()
			recs := onboard.MatchCatalog(signals, cat, installedCarts, builtinCarts, limit)

			var installedNow []string
			var installErrs []string
			if install {
				for i := range recs {
					if recs[i].Installed || recs[i].Kind != "prebuilt-binary" {
						continue
					}
					if _, err := reg.Install(cmd.Context(), recs[i].App); err != nil {
						installErrs = append(installErrs, fmt.Sprintf("%s: %v", recs[i].App, err))
						continue
					}
					recs[i].Installed = true
					installedNow = append(installedNow, recs[i].App)
				}
			}

			outDoc := map[string]any{
				"domains_scanned": len(signals),
				"matched_apps":    len(recs),
				"recommendations": recs,
				"installed_now":   installedNow,
				"install_errors":  installErrs,
			}
			if wantsJSON(cmd, asJSON) {
				if err := writeJSON(cmd.OutOrStdout(), outDoc); err != nil {
					return err
				}
			} else {
				w := cmd.OutOrStdout()
				fmt.Fprintf(w, "Scanned %d local browser domains (plaintext hostnames only, zero secrets read).\n", len(signals))
				if len(recs) == 0 {
					fmt.Fprintln(w, "No matching apps detected in local browser profiles yet.")
				} else {
					fmt.Fprintf(w, "Found %d apps you actively use that Printing Press already supports:\n\n", len(recs))
					tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
					fmt.Fprintln(tw, "APP\tDOMAIN\tSESSION\tVISITS\tBROWSERS\tSTATUS")
					for _, r := range recs {
						sess := "history"
						if r.HasSession {
							sess = "logged-in"
						}
						status := "ready (auto-hydrate)"
						if r.Installed {
							status = "installed"
						}
						fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n",
							r.App, r.MatchedDomain, sess, r.VisitCount, strings.Join(r.Browsers, ","), status)
					}
					_ = tw.Flush()
					if !install {
						fmt.Fprintln(w, "\nRun `pp onboard --install` to pre-warm these locally, or just call `pp run <app> ...` directly (auto-hydrates on first call).")
					}
				}
			}
			if len(installErrs) > 0 {
				return fmt.Errorf("onboard install failed for %d app(s): %s", len(installErrs), strings.Join(installErrs, "; "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&browserFilter, "browser", "all", "Browser filter: all, ardia, chrome, firefox")
	cmd.Flags().BoolVar(&install, "install", false, "Pre-warm matched prebuilt binaries into ~/.pp/bin (SHA-256 verified, zero Go)")
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum matched apps to display")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newAuthCmd() *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Sync domain-scoped browser sessions from Ardia, Chrome, or Firefox into encrypted local press-auth state",
	}

	var domainOverride string
	var browser string
	var asJSON bool
	syncCmd := &cobra.Command{
		Use:   "sync <app>",
		Short: "Pull strictly one app's cookies from Ardia, Chrome (agentcookie), or Firefox into encrypted ~/.press-auth/<domain>.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := strings.TrimSpace(args[0])
			domain := strings.TrimSpace(domainOverride)
			if domain == "" {
				var err error
				domain, err = resolveAppDomain(cmd.Context(), app)
				if err != nil {
					return err
				}
			}

			var (
				cookieMap map[string]string
				source    string
				err       error
			)
			switch strings.ToLower(strings.TrimSpace(browser)) {
			case "ardia":
				_, cookieMap, err = sessionbridge.ResolveArdiaDomainCookies(cmd.Context(), domain)
				source = "ardia-cdp"
			case "chrome":
				_, cookieMap, err = sessionbridge.ResolveAgentCookieDomainCookies(domain)
				source = "agentcookie"
				if err != nil {
					return &cartridge.ExecError{
						Code:    cartridge.ExitAuth,
						Class:   cartridge.ClassAuthError,
						Message: fmt.Sprintf("no Chrome agentcookie sidecar cookies found for %s: %v", domain, err),
						Hint:    fmt.Sprintf("for Chrome without Keychain prompts, run Cartridges directly via `pp run %s <cmd> --via browse`, or capture a session with `press-auth login %s --login-url https://%s`", app, domain, domain),
						Handoff: &cartridge.HandoffHint{
							Action:     "sign_in",
							URL:        "https://" + domain,
							BrowserCmd: fmt.Sprintf("press-auth login %s --login-url https://%s", domain, domain),
							Reason:     "Chrome encrypts its SQLite cookie store with macOS Keychain; use --via browse, agentcookie sidecar, or press-auth login",
							ResumeCmd:  fmt.Sprintf("pp run %s <command> --via browse", app),
						},
					}
				}
			case "firefox":
				_, cookieMap, err = sessionbridge.ResolveFirefoxDomainCookies(domain)
				source = "firefox-family"
			case "", "auto":
				if _, m, aerr := sessionbridge.ResolveAgentCookieDomainCookies(domain); aerr == nil && len(m) > 0 {
					cookieMap = m
					source = "agentcookie"
				} else if _, m, rerr := sessionbridge.ResolveArdiaDomainCookies(cmd.Context(), domain); rerr == nil && len(m) > 0 {
					cookieMap = m
					source = "ardia-cdp"
				} else if _, m, ferr := sessionbridge.ResolveFirefoxDomainCookies(domain); ferr == nil && len(m) > 0 {
					cookieMap = m
					source = "firefox-family"
				} else {
					return &cartridge.ExecError{
						Code:    cartridge.ExitAuth,
						Class:   cartridge.ClassAuthError,
						Message: fmt.Sprintf("no active session cookies found for %s in agentcookie, Ardia, or Firefox", domain),
						Hint:    fmt.Sprintf("sign in via Ardia and retry `pp auth sync %s`, or run Cartridges inside Chrome with `pp run %s <cmd> --via browse`", app, app),
						Handoff: &cartridge.HandoffHint{
							Action:     "sign_in",
							URL:        "https://" + domain,
							BrowserCmd: fmt.Sprintf("ARDIA_BACKGROUND=1 ardia browse open https://%s", domain),
							Reason:     "no local session found for " + domain,
							ResumeCmd:  fmt.Sprintf("pp auth sync %s", app),
						},
					}
				}
			default:
				return &cartridge.ExecError{
					Code:    cartridge.ExitUsage,
					Class:   cartridge.ClassUsage,
					Message: fmt.Sprintf("invalid --browser %q (expected auto, ardia, chrome, or firefox)", browser),
				}
			}
			if err != nil {
				return err
			}
			existingState, _ := loadPressAuthState(domain)
			if err := savePressAuthState(mergePressAuthCookies(existingState, domain, cookieMap)); err != nil {
				return err
			}
			res := map[string]any{
				"app":          app,
				"domain":       domain,
				"cookie_count": len(cookieMap),
				"source":       source,
				"stored":       "press-auth",
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Synced %d domain-scoped cookie(s) for %s (%s) from %s into encrypted press-auth store.\n",
				len(cookieMap), app, domain, source)
			return nil
		},
	}
	syncCmd.Flags().StringVar(&domainOverride, "domain", "", "Explicit target domain (defaults to app homepage domain)")
	syncCmd.Flags().StringVar(&browser, "browser", "auto", "Browser source: auto, ardia, chrome, firefox")
	syncCmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	authCmd.AddCommand(syncCmd)
	return authCmd
}

func resolveAppDomain(ctx context.Context, app string) (string, error) {
	if c, err := cartridge.Resolve(app); err == nil {
		if len(c.Auth.Domains) > 0 {
			return strings.TrimPrefix(strings.ToLower(c.Auth.Domains[0]), "."), nil
		}
		if c.Homepage != "" {
			if u, perr := url.Parse(c.Homepage); perr == nil && u.Hostname() != "" {
				return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), nil
			}
		}
	}
	reg, err := registry.NewClient()
	if err != nil {
		return "", err
	}
	entry, err := reg.LookupEntry(ctx, app)
	if err != nil {
		return "", fmt.Errorf("could not resolve domain for %q (pass --domain <host>): %w", app, err)
	}
	if entry.Homepage == "" {
		return "", fmt.Errorf("catalog entry %q has no homepage URL (pass --domain <host>)", app)
	}
	u, err := url.Parse(entry.Homepage)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("invalid homepage URL %q for %q", entry.Homepage, app)
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), nil
}

// hydrateAndExecLibraryBinary verifies or auto-hydrates a 616-app library
// binary, optionally syncs domain-scoped Ardia cookies for cookie-auth apps,
// and executes it forwarding all child flags verbatim.
func hydrateAndExecLibraryBinary(cmd *cobra.Command, app string, childArgs []string) error {
	binPath, installed := registry.InstalledBinaryPath(app)
	reg, regErr := registry.NewClient()
	if !installed {
		if strings.TrimSpace(os.Getenv("PP_NO_AUTO_INSTALL")) == "1" || regErr != nil {
			return &cartridge.ExecError{
				Code:    cartridge.ExitNotFound,
				Class:   cartridge.ClassUsage,
				Message: fmt.Sprintf("app %q is not installed", app),
				Hint:    fmt.Sprintf("run `pp use %s` (zero Go) or `pp search %s`", app, app),
			}
		}
		hydrated, downloaded, err := reg.EnsureInstalled(cmd.Context(), app)
		if err != nil {
			return &cartridge.ExecError{
				Code:    cartridge.ExitNotFound,
				Class:   cartridge.ClassUsage,
				Message: fmt.Sprintf("app %q could not be resolved or auto-hydrated: %v", app, err),
				Hint:    fmt.Sprintf("run `pp search %s` or `pp onboard` to browse available apps", app),
			}
		}
		if downloaded {
			fmt.Fprintf(cmd.ErrOrStderr(), "pp: auto-hydrated verified binary %s into %s\n", app, hydrated)
		}
		binPath = hydrated
	} else {
		if verr := registry.VerifyInstalledBinary(app, binPath); verr != nil {
			return &ExitError{Code: cartridge.ExitAuth, Err: verr}
		}
	}

	if len(childArgs) > 0 && childArgs[0] == "--" {
		childArgs = childArgs[1:]
	}

	env := os.Environ()
	// Domain-scoped cookie handoff for cookie-auth library CLIs. When a fresh
	// (<24h) encrypted press-auth snapshot already exists, reuse it in ~1ms and
	// skip the full Ardia CDP cookie dump.
	if regErr == nil {
		if entry, err := reg.LookupEntry(cmd.Context(), app); err == nil && entry.Homepage != "" && entryMayUseCookies(entry) {
			if u, uerr := url.Parse(entry.Homepage); uerr == nil && u.Hostname() != "" {
				dom := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
				needsCookieEnv := entry.MCP != nil && strings.EqualFold(entry.MCP.AuthType, "cookie")
				existingState, existingErr := loadPressAuthState(dom)
				freshCached := existingErr == nil && sessionbridge.IsPressAuthFresh(existingState, time.Now().UTC())
				var cookieHeader string
				switch {
				case freshCached:
					if needsCookieEnv {
						cookieHeader = sessionbridge.JoinCookies(existingState.Cookies)
					}
				case strings.TrimSpace(os.Getenv("PP_NO_ARDIA_COOKIES")) != "1":
					if hdr, cookieMap, aerr := sessionbridge.ResolveArdiaDomainCookies(cmd.Context(), dom); aerr == nil && len(cookieMap) > 0 {
						_ = savePressAuthState(mergePressAuthCookies(existingState, dom, cookieMap))
						cookieHeader = hdr
					} else if needsCookieEnv && existingState != nil && len(existingState.Cookies) > 0 {
						cookieHeader = sessionbridge.JoinCookies(existingState.Cookies)
					}
				case needsCookieEnv && existingState != nil && len(existingState.Cookies) > 0:
					cookieHeader = sessionbridge.JoinCookies(existingState.Cookies)
				}
				if needsCookieEnv && cookieHeader != "" {
					for _, envName := range entry.MCP.EnvVars {
						if strings.Contains(strings.ToUpper(envName), "COOKIE") && os.Getenv(envName) == "" {
							env = append(env, envName+"="+cookieHeader)
						}
					}
				}
			}
		}
	}

	proc := exec.CommandContext(cmd.Context(), binPath, childArgs...)
	proc.Env = env
	proc.Stdin = cmd.InOrStdin()
	proc.Stdout = cmd.OutOrStdout()
	proc.Stderr = cmd.ErrOrStderr()
	return proc.Run()
}

func entryMayUseCookies(entry *registry.Entry) bool {
	if entry == nil || entry.MCP == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(entry.MCP.AuthType)) {
	case "", "cookie", "browser_cookie", "session":
		return true
	default:
		return false
	}
}

// mergePressAuthCookies updates stored cookies while preserving any existing
// press-auth refresh configuration (RefreshEndpoint, JWTCarrierCookie) and
// recomputing JWTExpiry from the newly synced carrier cookie when present.
func mergePressAuthCookies(existing *pressauth.State, domain string, cookies map[string]string) *pressauth.State {
	next := &pressauth.State{
		Domain:     domain,
		CapturedAt: time.Now().UTC(),
		Cookies:    cookies,
	}
	if existing != nil {
		next.RefreshEndpoint = existing.RefreshEndpoint
		next.JWTCarrierCookie = existing.JWTCarrierCookie
		if existing.JWTCarrierCookie != "" {
			if carrierVal, ok := cookies[existing.JWTCarrierCookie]; ok {
				if token, err := pressauth.ExtractJWT(carrierVal); err == nil {
					if claims, err := pressauth.DecodeJWT(token); err == nil {
						if exp, err := pressauth.Exp(claims); err == nil {
							next.JWTExpiry = exp
						}
					}
				}
			}
		}
	}
	return next
}

type batchOp struct {
	Op        string            `json:"op"` // "route", "run", "lake_search", "lake_sql"
	Target    string            `json:"target,omitempty"`
	App       string            `json:"app,omitempty"`
	Command   string            `json:"command,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
	Compact   *bool             `json:"compact,omitempty"`
	Limit     int               `json:"limit,omitempty"`
	DryRun    bool              `json:"dry_run,omitempty"`
	ViaBrowse bool              `json:"via_browse,omitempty"`
	Query     string            `json:"query,omitempty"`
	SQL       string            `json:"sql,omitempty"`
}

type batchItemResult struct {
	Index   int                    `json:"index"`
	Op      string                 `json:"op"`
	OK      bool                   `json:"ok"`
	Result  any                    `json:"result,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Handoff *cartridge.HandoffHint `json:"handoff,omitempty"`
}

func newBatchCmd() *cobra.Command {
	var noBoundaries bool
	cmd := &cobra.Command{
		Use:   "batch [json-array]",
		Short: "Execute multiple route, run, or lake calls concurrently in one process invocation",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var raw []byte
			var err error
			if len(args) == 1 {
				raw = []byte(args[0])
			} else {
				raw, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1<<20))
				if err != nil {
					return err
				}
			}
			var ops []batchOp
			if err := json.Unmarshal(raw, &ops); err != nil {
				return &cartridge.ExecError{Code: cartridge.ExitUsage, Class: cartridge.ClassUsage, Message: fmt.Sprintf("invalid batch JSON array: %v", err)}
			}
			if len(ops) == 0 {
				return writeJSON(cmd.OutOrStdout(), []batchItemResult{})
			}
			if len(ops) > 32 {
				return &cartridge.ExecError{Code: cartridge.ExitUsage, Class: cartridge.ClassUsage, Message: "batch limited to at most 32 operations per call"}
			}

			reg, _ := registry.NewClient()
			results := make([]batchItemResult, len(ops))
			sem := make(chan struct{}, 8)
			var wg sync.WaitGroup
			for i, op := range ops {
				wg.Add(1)
				go func(idx int, item batchOp) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					res, runErr := executeBatchOp(cmd.Context(), reg, item)
					if runErr != nil {
						var handoff *cartridge.HandoffHint
						if ee, ok := errors.AsType[*cartridge.ExecError](runErr); ok {
							handoff = ee.Handoff
						}
						results[idx] = batchItemResult{Index: idx, Op: item.Op, OK: false, Error: runErr.Error(), Handoff: handoff}
						return
					}
					results[idx] = batchItemResult{Index: idx, Op: item.Op, OK: true, Result: res}
				}(i, op)
			}
			wg.Wait()

			encoded, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				return err
			}
			out := string(encoded)
			if !noBoundaries {
				out = cartridge.WrapUntrustedContent(out, "pp-batch", false)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), out)
			return err
		},
	}
	cmd.Flags().BoolVar(&noBoundaries, "no-content-boundaries", false, "Disable random-nonce prompt-injection fences")
	return cmd
}

func executeBatchOp(ctx context.Context, reg *registry.Client, item batchOp) (any, error) {
	switch strings.ToLower(strings.TrimSpace(item.Op)) {
	case "route":
		if reg == nil {
			return nil, errors.New("registry client unavailable")
		}
		return reg.Route(ctx, item.Target)
	case "run":
		c, err := cartridge.Resolve(item.App)
		if err != nil {
			return nil, err
		}
		compact := true
		if item.Compact != nil {
			compact = *item.Compact
		}
		return cartridge.Execute(ctx, c, item.Command, cartridge.RunOptions{
			Params:    item.Params,
			Compact:   compact,
			Limit:     item.Limit,
			DryRun:    item.DryRun,
			ViaBrowse: item.ViaBrowse,
		})
	case "lake_search":
		p, err := lake.DefaultPath()
		if err != nil {
			return nil, err
		}
		lk, err := lake.Open(p)
		if err != nil {
			return nil, err
		}
		defer func() { _ = lk.Close() }()
		return lk.Search(ctx, item.Query, item.App, "", item.Limit)
	case "lake_sql":
		p, err := lake.DefaultPath()
		if err != nil {
			return nil, err
		}
		lk, err := lake.Open(p)
		if err != nil {
			return nil, err
		}
		defer func() { _ = lk.Close() }()
		return lk.QueryReadOnly(ctx, item.SQL)
	default:
		return nil, fmt.Errorf("unsupported batch op %q (supported: route, run, lake_search, lake_sql)", item.Op)
	}
}

func newSetupCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "One-command cross-agent bootstrap (installs Ardia plugin if present, detects local coding agents, and prints MCP + routing wiring)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			var detectedAgents []string
			for _, candidate := range []struct{ name, dir string }{
				{"claude-code", filepath.Join(home, ".claude")},
				{"codex", filepath.Join(home, ".codex")},
				{"opencode", filepath.Join(home, ".config", "opencode")},
				{"cursor", filepath.Join(home, ".cursor")},
				{"agents-skills", filepath.Join(home, ".agents", "skills")},
			} {
				if info, statErr := os.Stat(candidate.dir); statErr == nil && info.IsDir() {
					detectedAgents = append(detectedAgents, candidate.name)
				}
			}

			ardiaInstalled := false
			if _, err := os.Stat(filepath.Join(home, ".config", "ardia")); err == nil {
				ardiaInstalled = true
			} else if _, err := exec.LookPath("ardia"); err == nil {
				ardiaInstalled = true
			}
			ardiaPluginPath := ""
			if ardiaInstalled {
				pluginDir := filepath.Join(home, ".config", "ardia", "plugins", "printing-press")
				if err := installArdiaPlugin(pluginDir); err == nil {
					ardiaPluginPath = pluginDir
				}
			}

			browseInstalled := false
			if _, err := exec.LookPath("browse"); err == nil {
				browseInstalled = true
			} else if _, err := os.Stat(filepath.Join(home, ".local", "bin", "browse")); err == nil {
				browseInstalled = true
			}

			summary := map[string]any{
				"detected_agents":   detectedAgents,
				"ardia_detected":    ardiaInstalled,
				"ardia_plugin_path": ardiaPluginPath,
				"opencode_browse":   browseInstalled,
				"mcp_config_snippet": map[string]any{
					"printing-press": map[string]any{
						"command": "pp",
						"args":    []string{"mcp"},
					},
				},
				"next_step": "run `pp onboard` to see which of the 616+ apps match your local Chrome/Ardia sessions",
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), summary)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Detected agent environments: %s\n", strings.Join(detectedAgents, ", "))
			if ardiaPluginPath != "" {
				fmt.Fprintf(w, "Ardia plugin installed: %s\n", ardiaPluginPath)
			}
			if browseInstalled {
				fmt.Fprintln(w, "OpenCode `browse` detected: Chrome zero-popup recording (`pp record --backend browse`) ready.")
			}
			fmt.Fprintln(w, "\nAdd this single 5-tool MCP entry to your agent config:")
			fmt.Fprintln(w, `  "printing-press": { "command": "pp", "args": ["mcp"] }`)
			fmt.Fprintln(w, "\nNext: run `pp onboard` to match your browser domains against all 616+ Printing Press apps.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

// buildBoundedRunJSON preserves strict JSON validity whenever a RunResult
// exceeds --max-output by fitting whole rows within budget and explicitly
// flagging truncated=true with total_chars and recovery hints.
func buildBoundedRunJSON(res *cartridge.RunResult, maxChars, totalChars int) map[string]any {
	out := map[string]any{
		"cartridge":        res.Cartridge,
		"command":          res.Command,
		"driver":           res.Driver,
		"http_status":      res.HTTPStatus,
		"latency_ms":       res.LatencyMS,
		"row_count":        res.RowCount,
		"auth_source":      res.AuthSource,
		"truncated":        true,
		"max_output_chars": maxChars,
		"total_chars":      totalChars,
		"hint":             "response exceeded --max-output budget; re-run with --compact, --limit, or --full",
	}
	rows, ok := res.Data.([]any)
	if !ok {
		out["data"] = nil
		return out
	}
	out["returned_rows"] = len(rows)
	out["data"] = []any{}
	baseEncoded, _ := json.MarshalIndent(out, "", "  ")
	used := len(baseEncoded)
	kept := make([]any, 0)
	for _, r := range rows {
		rowBytes, err := json.MarshalIndent(r, "    ", "  ")
		if err != nil {
			continue
		}
		// Account for leading indent + comma/newline separator per element.
		cost := len(rowBytes) + 6
		if used+cost > maxChars {
			break
		}
		kept = append(kept, r)
		used += cost
	}
	out["returned_rows"] = len(kept)
	out["data"] = kept
	// Single final guard so indented output is strictly <= maxChars.
	for len(kept) > 0 {
		encoded, err := json.MarshalIndent(out, "", "  ")
		if err != nil || len(encoded) <= maxChars {
			break
		}
		kept = kept[:len(kept)-1]
		out["returned_rows"] = len(kept)
		out["data"] = kept
	}
	return out
}

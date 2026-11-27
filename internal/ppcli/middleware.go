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
		Short: "Sync domain-scoped browser sessions from Ardia into encrypted local press-auth state",
	}

	var domainOverride string
	var asJSON bool
	syncCmd := &cobra.Command{
		Use:   "sync <app>",
		Short: "Pull strictly one app's cookies from Ardia Browser into encrypted ~/.press-auth/<domain>.json",
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
			_, cookieMap, err := sessionbridge.ResolveArdiaDomainCookies(cmd.Context(), domain)
			if err != nil {
				return err
			}
			if err := pressauth.Save(&pressauth.State{
				Domain:     domain,
				CapturedAt: time.Now().UTC(),
				Cookies:    cookieMap,
			}); err != nil {
				return err
			}
			res := map[string]any{
				"app":          app,
				"domain":       domain,
				"cookie_count": len(cookieMap),
				"source":       "ardia-cdp",
				"stored":       "press-auth",
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Synced %d domain-scoped cookie(s) for %s (%s) from Ardia into encrypted press-auth store.\n",
				len(cookieMap), app, domain)
			return nil
		},
	}
	syncCmd.Flags().StringVar(&domainOverride, "domain", "", "Explicit target domain (defaults to app homepage domain)")
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
	// Best-effort domain-scoped Ardia cookie handoff for cookie-auth library CLIs
	// when no press-auth state exists yet for the app's homepage domain.
	if regErr == nil && strings.TrimSpace(os.Getenv("PP_NO_ARDIA_COOKIES")) != "1" {
		if entry, err := reg.LookupEntry(cmd.Context(), app); err == nil && entry.Homepage != "" {
			if u, uerr := url.Parse(entry.Homepage); uerr == nil && u.Hostname() != "" {
				dom := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
				needsCookieEnv := entry.MCP != nil && strings.EqualFold(entry.MCP.AuthType, "cookie")
				_, existingErr := pressauth.Load(dom)
				if needsCookieEnv || errors.Is(existingErr, pressauth.ErrStateNotFound) {
					if hdr, cookieMap, aerr := sessionbridge.ResolveArdiaDomainCookies(cmd.Context(), dom); aerr == nil && len(cookieMap) > 0 {
						if errors.Is(existingErr, pressauth.ErrStateNotFound) {
							_ = pressauth.Save(&pressauth.State{
								Domain:     dom,
								CapturedAt: time.Now().UTC(),
								Cookies:    cookieMap,
							})
						}
						if needsCookieEnv {
							for _, envName := range entry.MCP.EnvVars {
								if strings.Contains(strings.ToUpper(envName), "COOKIE") && os.Getenv(envName) == "" {
									env = append(env, envName+"="+hdr)
								}
							}
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

type batchOp struct {
	Op      string            `json:"op"` // "route", "run", "lake_search", "lake_sql"
	Target  string            `json:"target,omitempty"`
	App     string            `json:"app,omitempty"`
	Command string            `json:"command,omitempty"`
	Params  map[string]string `json:"params,omitempty"`
	Compact *bool             `json:"compact,omitempty"`
	Limit   int               `json:"limit,omitempty"`
	DryRun  bool              `json:"dry_run,omitempty"`
	Query   string            `json:"query,omitempty"`
	SQL     string            `json:"sql,omitempty"`
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
			Params:  item.Params,
			Compact: compact,
			Limit:   item.Limit,
			DryRun:  item.DryRun,
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

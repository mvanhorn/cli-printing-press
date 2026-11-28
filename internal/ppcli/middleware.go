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
	"runtime"
	"slices"
	"strconv"
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

	var doctorJSON bool
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check authentication readiness across all installed Printing Press binaries in one view",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthDoctor(cmd, doctorJSON)
		},
	}
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "Output as JSON")
	authCmd.AddCommand(doctorCmd)
	return authCmd
}

type authDoctorStatus string

const (
	authDoctorStatusOK        authDoctorStatus = "ok"
	authDoctorStatusNeedsAuth authDoctorStatus = "needs_auth"
	authDoctorStatusNoAuth    authDoctorStatus = "no_auth_required"
	authDoctorStatusTimeout   authDoctorStatus = "timeout"
	authDoctorStatusError     authDoctorStatus = "error"
)

type authDoctorItem struct {
	App    string           `json:"app"`
	Status authDoctorStatus `json:"status"`
	Detail string           `json:"detail,omitempty"`
	Hint   string           `json:"hint,omitempty"`
}

func runAuthDoctor(cmd *cobra.Command, asJSON bool) error {
	ppHome, err := cartridge.HomeDir()
	if err != nil {
		return err
	}
	binDir := filepath.Join(ppHome, "bin")
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"installed": 0, "results": []authDoctorItem{}})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "No prebuilt binaries installed yet. Run `pp onboard --install` or `pp use <app>`.")
			return nil
		}
		return err
	}

	type target struct {
		app     string
		binPath string
	}
	var targets []target
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, "-pp-cli") && !strings.HasSuffix(name, "-pp-cli.exe") {
			continue
		}
		app := strings.TrimSuffix(strings.TrimSuffix(name, ".exe"), "-pp-cli")
		if app == "" {
			continue
		}
		targets = append(targets, target{app: app, binPath: filepath.Join(binDir, name)})
	}
	slices.SortFunc(targets, func(a, b target) int {
		return strings.Compare(a.app, b.app)
	})

	// Read local catalog cache once without network access so doctor checks
	// never stall on GitHub or burn per-binary timeouts on registry lookups.
	catalogByApp := make(map[string]*registry.Entry)
	if reg, rErr := registry.NewClient(); rErr == nil {
		if cat, cErr := reg.LoadCachedOnly(); cErr == nil && cat != nil {
			for i := range cat.Entries {
				e := &cat.Entries[i]
				catalogByApp[strings.ToLower(e.Name)] = e
			}
		}
	}

	results := make([]authDoctorItem, len(targets))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i, tgt := range targets {
		wg.Add(1)
		go func(idx int, t target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = probeBinaryAuth(cmd.Context(), catalogByApp[strings.ToLower(t.app)], t.app, t.binPath)
		}(i, tgt)
	}
	wg.Wait()

	if wantsJSON(cmd, asJSON) {
		return writeJSON(cmd.OutOrStdout(), map[string]any{
			"installed": len(results),
			"results":   results,
		})
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "APP\tSTATUS\tDETAIL\tNEXT STEP")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.App, r.Status, r.Detail, r.Hint)
	}
	return tw.Flush()
}

func probeBinaryAuth(ctx context.Context, entry *registry.Entry, app, binPath string) authDoctorItem {
	// Refuse to execute unpinned or checksum-mismatched binaries.
	if err := registry.VerifyInstalledBinary(app, binPath); err != nil {
		return authDoctorItem{
			App:    app,
			Status: authDoctorStatusError,
			Detail: firstNonEmptyLine(err.Error(), 90),
			Hint:   fmt.Sprintf("reinstall verified binary via `~/.local/bin/pp use %s`", app),
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var authType string
	var envVars []string
	if entry != nil && entry.MCP != nil {
		authType = entry.MCP.AuthType
		envVars = entry.MCP.EnvVars
	}

	proc := exec.CommandContext(probeCtx, binPath, "auth", "status")
	proc.Env = os.Environ()
	proc.WaitDelay = 500 * time.Millisecond
	raw, err := proc.CombinedOutput()
	out := strings.TrimSpace(string(raw))
	lower := strings.ToLower(out)

	if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
		return authDoctorItem{App: app, Status: authDoctorStatusTimeout, Detail: "auth status timed out after 5s"}
	}
	if strings.Contains(lower, "unknown command \"auth\"") || strings.Contains(lower, "unknown command 'auth'") {
		return authDoctorItem{App: app, Status: authDoctorStatusNoAuth, Detail: "public or no auth subcommand"}
	}
	firstLine := firstNonEmptyLine(out, 90)
	isAuthProblem := looksLikeAuthFailure(lower)
	if err == nil && !isAuthProblem {
		return authDoctorItem{App: app, Status: authDoctorStatusOK, Detail: firstLine}
	}
	if !isAuthProblem {
		detail := firstLine
		if detail == "" && err != nil {
			detail = firstNonEmptyLine(err.Error(), 90)
		}
		return authDoctorItem{App: app, Status: authDoctorStatusError, Detail: detail}
	}
	hint := fmt.Sprintf("~/.local/bin/pp run %s auth login", app)
	if strings.EqualFold(authType, "cookie") {
		hint = fmt.Sprintf("~/.local/bin/pp auth sync %s", app)
	} else if len(envVars) > 0 {
		hint = fmt.Sprintf("set %s (or run ~/.local/bin/pp run %s auth --help)", envVars[0], app)
	}
	return authDoctorItem{App: app, Status: authDoctorStatusNeedsAuth, Detail: firstLine, Hint: hint}
}

func looksLikeAuthFailure(lower string) bool {
	for _, needle := range []string{
		"not authenticated",
		"not logged in",
		"login required",
		"fail ",
		"expired",
		"set your token",
		"set credentials",
		"unauthorized",
		"sign-in",
		"sign in",
		"http 401",
		"http 403",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func firstNonEmptyLine(s string, maxRunes int) string {
	for line := range strings.SplitSeq(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		runes := []rune(trimmed)
		if len(runes) > maxRunes {
			return string(runes[:maxRunes]) + "..."
		}
		return trimmed
	}
	return ""
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
	// Domain-scoped cookie handoff for cookie-auth library CLIs. Skips meta/help
	// flags and non-cookie apps completely, reuses fresh (<24h) press-auth
	// snapshots in ~1ms, and caps Ardia lookups at 5s.
	if regErr == nil && !sessionbridge.IsMetaOrHelpArgs(childArgs) {
		if entry, err := reg.LookupEntry(cmd.Context(), app); err == nil && entry.Homepage != "" && entryMayUseCookies(entry) {
			extra := sessionbridge.ResolveCookieEnvVars(cmd.Context(), sessionbridge.CookieEnvOptions{
				Homepage:    entry.Homepage,
				AuthType:    entry.MCP.AuthType,
				EnvVarNames: entry.MCP.EnvVars,
				ChildArgs:   childArgs,
				LoadState:   loadPressAuthState,
				SaveState:   savePressAuthState,
			})
			env = append(env, extra...)
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
		return false
	}
	authType := strings.ToLower(strings.TrimSpace(entry.MCP.AuthType))
	return authType == "cookie" || authType == "browser_cookie" || authType == "session"
}

// mergePressAuthCookies updates stored cookies while preserving any existing
// press-auth refresh configuration (RefreshEndpoint, JWTCarrierCookie) and
// recomputing JWTExpiry from the newly synced carrier cookie when present.
func mergePressAuthCookies(existing *pressauth.State, domain string, cookies map[string]string) *pressauth.State {
	return sessionbridge.MergePressAuthCookies(existing, domain, cookies)
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
	var apply bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "One-command cross-agent bootstrap (installs Ardia plugin, detects local coding agents, and wires MCP + skills with --apply)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			var detectedAgents []string
			hasDir := func(p string) bool {
				info, statErr := os.Stat(p)
				return statErr == nil && info.IsDir()
			}
			for _, candidate := range []struct{ name, dir string }{
				{"claude-code", filepath.Join(home, ".claude")},
				{"codex", filepath.Join(home, ".codex")},
				{"antigravity", filepath.Join(home, ".gemini", "config")},
				{"opencode", filepath.Join(home, ".config", "opencode")},
				{"cursor", filepath.Join(home, ".cursor")},
				{"agents-skills", filepath.Join(home, ".agents", "skills")},
			} {
				if hasDir(candidate.dir) {
					detectedAgents = append(detectedAgents, candidate.name)
				}
			}
			t3AntigravityDirs := findT3AntigravityConfigDirs(home)
			if len(t3AntigravityDirs) > 0 {
				detectedAgents = append(detectedAgents, "t3-antigravity")
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

			ppBin := resolvePPBinaryPath(home)
			var applied []string
			var applyErrs []string
			if apply {
				defaultClaudeDir := filepath.Clean(filepath.Join(home, ".claude"))
				claudeCfgDir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
				if claudeCfgDir != "" && filepath.Clean(claudeCfgDir) != defaultClaudeDir && hasDir(claudeCfgDir) {
					claudeCfg := filepath.Join(claudeCfgDir, ".claude.json")
					if err := upsertJSONMCPServer(claudeCfg, "printing-press", ppBin, true); err != nil {
						applyErrs = append(applyErrs, fmt.Sprintf("%s: %v", claudeCfg, err))
					} else {
						applied = append(applied, claudeCfg)
					}
				}
				if hasDir(defaultClaudeDir) {
					claudeCfg := filepath.Join(home, ".claude.json")
					if err := upsertJSONMCPServer(claudeCfg, "printing-press", ppBin, true); err != nil {
						applyErrs = append(applyErrs, fmt.Sprintf("%s: %v", claudeCfg, err))
					} else {
						applied = append(applied, claudeCfg)
					}
				}
				if hasDir(filepath.Join(home, ".cursor")) {
					cursorCfg := filepath.Join(home, ".cursor", "mcp.json")
					if err := upsertJSONMCPServer(cursorCfg, "printing-press", ppBin, false); err != nil {
						applyErrs = append(applyErrs, fmt.Sprintf("%s: %v", cursorCfg, err))
					} else {
						applied = append(applied, cursorCfg)
					}
				}
				if hasDir(filepath.Join(home, ".codex")) {
					codexCfg := filepath.Join(home, ".codex", "config.toml")
					if err := upsertCodexTOMLMCPServer(codexCfg, "printing-press", ppBin); err != nil {
						applyErrs = append(applyErrs, fmt.Sprintf("%s: %v", codexCfg, err))
					} else {
						applied = append(applied, codexCfg)
					}
				}
				if hasDir(filepath.Join(home, ".gemini", "config")) {
					geminiCfg := filepath.Join(home, ".gemini", "config", "mcp_config.json")
					if err := upsertJSONMCPServer(geminiCfg, "printing-press", ppBin, false); err != nil {
						applyErrs = append(applyErrs, fmt.Sprintf("%s: %v", geminiCfg, err))
					} else {
						applied = append(applied, geminiCfg)
					}
					geminiSkill := filepath.Join(home, ".gemini", "config", "skills", "printing-press-pp", "SKILL.md")
					if err := writeDefaultPPSkill(geminiSkill); err != nil {
						applyErrs = append(applyErrs, fmt.Sprintf("%s: %v", geminiSkill, err))
					} else {
						applied = append(applied, geminiSkill)
					}
				}
				for _, cfgDir := range t3AntigravityDirs {
					if !hasDir(cfgDir) {
						continue
					}
					t3Cfg := filepath.Join(cfgDir, "mcp_config.json")
					if err := upsertJSONMCPServer(t3Cfg, "printing-press", ppBin, false); err != nil {
						applyErrs = append(applyErrs, fmt.Sprintf("%s: %v", t3Cfg, err))
					} else {
						applied = append(applied, t3Cfg)
					}
					t3Skill := filepath.Join(cfgDir, "skills", "printing-press-pp", "SKILL.md")
					if err := writeDefaultPPSkill(t3Skill); err != nil {
						applyErrs = append(applyErrs, fmt.Sprintf("%s: %v", t3Skill, err))
					} else {
						applied = append(applied, t3Skill)
					}
				}
				if hasDir(filepath.Join(home, ".agents", "skills")) {
					skillPath := filepath.Join(home, ".agents", "skills", "printing-press-pp", "SKILL.md")
					if err := writeDefaultPPSkill(skillPath); err != nil {
						applyErrs = append(applyErrs, fmt.Sprintf("%s: %v", skillPath, err))
					} else {
						applied = append(applied, skillPath)
					}
				}
			}

			pathShadowedBy := ""
			if lookedUp, lErr := exec.LookPath("pp"); lErr == nil && filepath.Clean(lookedUp) != filepath.Clean(ppBin) {
				pathShadowedBy = lookedUp
			}

			summary := map[string]any{
				"detected_agents":   detectedAgents,
				"ardia_detected":    ardiaInstalled,
				"ardia_plugin_path": ardiaPluginPath,
				"opencode_browse":   browseInstalled,
				"pp_binary":         ppBin,
				"path_shadowed_by":  pathShadowedBy,
				"applied":           applied,
				"apply_errors":      applyErrs,
				"mcp_config_snippet": map[string]any{
					"printing-press": map[string]any{
						"command": ppBin,
						"args":    []string{"mcp"},
					},
				},
				"next_step": "run `pp setup --apply` to wire your coding agents automatically, then `pp onboard` to match your browser domains",
			}
			if apply {
				summary["next_step"] = "open a new Claude Code, Codex, or Cursor session and ask normally (e.g. check scores, orders, or search your apps)"
			}
			if wantsJSON(cmd, asJSON) {
				if err := writeJSON(cmd.OutOrStdout(), summary); err != nil {
					return err
				}
			} else {
				w := cmd.OutOrStdout()
				fmt.Fprintf(w, "Detected agent environments: %s\n", strings.Join(detectedAgents, ", "))
				if pathShadowedBy != "" {
					fmt.Fprintf(w, "Warning: `pp` on $PATH currently resolves to %s (not %s). Put %s earlier in PATH or invoke %s directly.\n",
						pathShadowedBy, ppBin, filepath.Dir(ppBin), ppBin)
				}
				if ardiaPluginPath != "" {
					fmt.Fprintf(w, "Ardia plugin installed: %s\n", ardiaPluginPath)
				}
				if browseInstalled {
					fmt.Fprintln(w, "OpenCode `browse` detected: Chrome zero-popup recording (`pp record --backend browse`) and `--via browse` ready.")
				}
				if apply {
					if len(applied) > 0 {
						fmt.Fprintln(w, "\nWired `pp mcp` (5-tool server) and routing skill into:")
						for _, p := range applied {
							fmt.Fprintf(w, "  %s\n", p)
						}
					}
					fmt.Fprintln(w, "\nDone. Open a new Claude Code, Codex, or Cursor session and ask in plain English.")
				} else {
					fmt.Fprintln(w, "\nRun `pp setup --apply` to automatically wire this 5-tool MCP entry into your detected agents, or add manually:")
					fmt.Fprintf(w, "  \"printing-press\": { \"command\": %s, \"args\": [\"mcp\"] }\n", strconv.Quote(ppBin))
					fmt.Fprintln(w, "\nNext: run `pp onboard` to match your browser domains against all 616+ Printing Press apps.")
				}
			}
			if len(applyErrs) > 0 {
				return fmt.Errorf("setup --apply failed on %d target(s): %s", len(applyErrs), strings.Join(applyErrs, "; "))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Automatically write the 5-tool `printing-press` MCP entry and routing skill into detected Claude Code, Codex, and Cursor configs")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func resolvePPBinaryPath(home string) string {
	exeName := "pp"
	if runtime.GOOS == "windows" {
		exeName = "pp.exe"
	}
	if exe, err := os.Executable(); err == nil && isPPExecutableName(filepath.Base(exe), runtime.GOOS) {
		if abs, absErr := filepath.Abs(exe); absErr == nil {
			return abs
		}
		return exe
	}
	if p, err := exec.LookPath(exeName); err == nil && p != "" {
		if abs, absErr := filepath.Abs(p); absErr == nil {
			return abs
		}
		return p
	}
	candidate := filepath.Join(home, ".local", "bin", exeName)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	return exeName
}

func isPPExecutableName(base, goos string) bool {
	if goos == "windows" {
		return strings.EqualFold(base, "pp.exe")
	}
	return base == "pp"
}

func withConfigFileLock(cfgPath string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		return err
	}
	lockPath := cfgPath + ".pp.lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			defer func() { _ = os.Remove(lockPath) }()
			return fn()
		}
		if !os.IsExist(err) {
			return err
		}
		if info, sErr := os.Stat(lockPath); sErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for config lock %s", lockPath)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func writeFileAtomicUnique(dstPath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(dstPath)+".pp-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func upsertJSONMCPServer(cfgPath, serverName, ppBin string, includeType bool) error {
	return withConfigFileLock(cfgPath, func() error {
		doc := make(map[string]any)
		raw, err := os.ReadFile(cfgPath)
		if err == nil && len(strings.TrimSpace(string(raw))) > 0 {
			if uErr := json.Unmarshal(raw, &doc); uErr != nil {
				return fmt.Errorf("parsing existing JSON: %w", uErr)
			}
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		servers, _ := doc["mcpServers"].(map[string]any)
		if servers == nil {
			servers = make(map[string]any)
		}
		entry := map[string]any{
			"command": ppBin,
			"args":    []string{"mcp"},
		}
		if includeType {
			entry["type"] = "stdio"
		}
		servers[serverName] = entry
		doc["mcpServers"] = servers

		encoded, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		encoded = append(encoded, '\n')
		return writeFileAtomicUnique(cfgPath, encoded, 0o600)
	})
}

func upsertCodexTOMLMCPServer(cfgPath, serverName, ppBin string) error {
	return withConfigFileLock(cfgPath, func() error {
		var existing string
		if raw, err := os.ReadFile(cfgPath); err == nil {
			existing = string(raw)
		} else if !os.IsNotExist(err) {
			return err
		}

		updated := updateCodexTOMLMCPTable(existing, serverName, ppBin)
		return writeFileAtomicUnique(cfgPath, []byte(updated), 0o600)
	})
}

// updateCodexTOMLMCPTable locates any existing [mcp_servers.<name>] table
// (including quoted forms like [mcp_servers."printing-press"]) and updates its
// command and args in place while preserving unrelated keys and subtables. If
// no matching table exists, it appends a clean new table block.
func updateCodexTOMLMCPTable(content, serverName, ppBin string) string {
	lines := strings.Split(content, "\n")
	if content == "" {
		lines = nil
	}
	cmdLine := "command = " + strconv.Quote(ppBin)
	argsLine := `args = ["mcp"]`

	tableStart := -1
	tableEnd := len(lines)
	for i, raw := range lines {
		segs, isHeader := parseTOMLTableHeader(raw)
		if !isHeader {
			continue
		}
		if tableStart >= 0 {
			tableEnd = i
			break
		}
		if len(segs) == 2 && segs[0] == "mcp_servers" && segs[1] == serverName {
			tableStart = i
		}
	}

	if tableStart >= 0 {
		out := make([]string, 0, len(lines)+2)
		out = append(out, lines[:tableStart+1]...)
		wroteCmd := false
		wroteArgs := false
		for i := tableStart + 1; i < tableEnd; i++ {
			trimmed := strings.TrimSpace(stripTOMLComment(lines[i]))
			key, val, hasEq := strings.Cut(trimmed, "=")
			if hasEq {
				switch strings.TrimSpace(key) {
				case "command":
					out = append(out, cmdLine)
					wroteCmd = true
					continue
				case "args":
					// Skip continuation lines when an existing `args = [...]`
					// array spans multiple lines so we never leave dangling elements.
					depth := tomlBracketDelta(val)
					for depth > 0 && i+1 < tableEnd {
						i++
						depth += tomlBracketDelta(stripTOMLComment(lines[i]))
					}
					out = append(out, argsLine)
					wroteArgs = true
					continue
				}
			}
			out = append(out, lines[i])
		}
		if !wroteCmd {
			out = append(out, cmdLine)
		}
		if !wroteArgs {
			out = append(out, argsLine)
		}
		out = append(out, lines[tableEnd:]...)
		res := strings.Join(out, "\n")
		if !strings.HasSuffix(res, "\n") {
			res += "\n"
		}
		return res
	}

	var b strings.Builder
	b.WriteString(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		b.WriteByte('\n')
	}
	if content != "" {
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "[mcp_servers.%s]\n%s\n%s\n", serverName, cmdLine, argsLine)
	return b.String()
}

func parseTOMLTableHeader(line string) ([]string, bool) {
	s := strings.TrimSpace(stripTOMLComment(line))
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") || strings.HasPrefix(s, "[[") {
		return nil, false
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil, false
	}
	var segs []string
	var cur strings.Builder
	inQuote := byte(0)
	escaped := false
	for i := 0; i < len(inner); i++ {
		ch := inner[i]
		if inQuote != 0 {
			if escaped {
				cur.WriteByte(ch)
				escaped = false
				continue
			}
			if inQuote == '"' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == inQuote {
				inQuote = 0
				continue
			}
			cur.WriteByte(ch)
			continue
		}
		switch ch {
		case '"', '\'':
			inQuote = ch
		case '.':
			segs = append(segs, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	segs = append(segs, strings.TrimSpace(cur.String()))
	return segs, true
}

func stripTOMLComment(line string) string {
	inQuote := byte(0)
	escaped := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inQuote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if inQuote == '"' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inQuote = ch
		case '#':
			return line[:i]
		}
	}
	return line
}

func tomlBracketDelta(s string) int {
	delta := 0
	inQuote := byte(0)
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if inQuote == '"' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			inQuote = ch
		case '[':
			delta++
		case ']':
			delta--
		}
	}
	return delta
}

// findT3AntigravityConfigDirs locates T3 Code Antigravity provider config
// directories via os.ReadDir rather than filepath.Glob so home paths with glob
// metacharacters (e.g. brackets) are handled literally.
func findT3AntigravityConfigDirs(home string) []string {
	providersDir := filepath.Join(home, ".t3", "userdata", "providers", "antigravity")
	entries, err := os.ReadDir(providersDir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cfgDir := filepath.Join(providersDir, entry.Name(), "config")
		if info, statErr := os.Stat(cfgDir); statErr == nil && info.IsDir() {
			dirs = append(dirs, cfgDir)
		}
	}
	return dirs
}

func writeDefaultPPSkill(skillPath string) error {
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		return err
	}
	const content = `---
name: printing-press-pp
description: >
  Use Printing Press 2.0 (pp) MCP tools (pp_route, pp_discover, pp_run,
  pp_lake_search, pp_lake_sql) BEFORE calling web search (search_web,
  WebSearch), WebFetch, opening a browser tab, or scraping web pages for live
  sports scores and game recaps, news, consumer apps (Instacart, Amazon Orders,
  DoorDash, Airbnb, Monarch Money, Linear, GitHub, Hacker News, ESPN), or
  personal synced data.
---

# Printing Press 2.0 (pp) Fast Path

When the user asks about live sports scores, recent game results/recaps, news,
or any of the 616+ supported web apps (or personal data synced to the local
Life Lake):

1. Check ` + "`pp_route`" + ` or ` + "`pp_discover`" + ` FIRST - before ` + "`search_web`" + `, ` + "`WebSearch`" + `, ` + "`WebFetch`" + `, or opening a browser.
2. Call ` + "`pp_run`" + ` for direct ~150ms JSON from the source API instead of web search or a browser tab.
   - For sites that need Google Chrome's logged-in HttpOnly cookies, pass ` + "`via_browse: true`" + `.
   - Mutating actions require explicit user confirmation (` + "`confirm_mutation: true`" + `).
3. For cross-app personal queries, check ` + "`pp_lake_search`" + ` or ` + "`pp_lake_sql`" + `.
4. Fall back to web search or a real browser (Ardia or Chrome) only when no fast path matches
   or human login/2FA/Passkey/payment confirmation is needed.
`
	return os.WriteFile(skillPath, []byte(content), 0o600)
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

package ppcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/compiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/heal"
	"github.com/mvanhorn/cli-printing-press/v4/internal/lake"
	"github.com/mvanhorn/cli-printing-press/v4/internal/ppmcp"
	"github.com/mvanhorn/cli-printing-press/v4/internal/registry"
	"github.com/mvanhorn/cli-printing-press/v4/internal/trust"
	"github.com/mvanhorn/cli-printing-press/v4/internal/version"
	"github.com/spf13/cobra"
)

// ExitError wraps an error with a specific exit code.
type ExitError struct {
	Code   int
	Err    error
	Silent bool
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// Execute runs the Printing Press 2.0 `pp` root CLI.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return NewRootCommand().ExecuteContext(ctx)
}

// NewRootCommand builds the complete `pp` Cobra command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:          "pp",
		Short:        "Printing Press 2.0 — Turn any app or website into a 50ms local tool for your agent",
		Long:         "Printing Press 2.0: Skip pixels on repeat work. Route URLs to 150ms local Cartridges and 616+ zero-Go prebuilt CLIs, compile new websites via Ardia/HAR in seconds, query your unified local Life Lake, audit MCP trust, and auto-heal drifted endpoints.",
		SilenceUsage: true,
		Version:      version.Version,
	}
	root.SetVersionTemplate("pp {{.Version}} (Printing Press 2.0)\n")

	root.AddCommand(newRouteCmd())
	root.AddCommand(newSearchCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newUseCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newInfoCmd())
	root.AddCommand(newRecordCmd())
	root.AddCommand(newLakeCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newAuditCmd())
	root.AddCommand(newTrustCmd())
	root.AddCommand(newHealCmd())
	root.AddCommand(newArdiaCmd())
	root.AddCommand(newEjectCmd())

	return root
}

func wantsJSON(cmd *cobra.Command, explicit bool) bool {
	if explicit {
		return true
	}
	if f, ok := cmd.OutOrStdout().(*os.File); ok {
		info, err := f.Stat()
		if err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
			return true
		}
	}
	return false
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func newRouteCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "route <url-or-app>",
		Short: "Check the 150ms fast path before opening a browser tab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := registry.NewClient()
			if err != nil {
				return err
			}
			decision, err := reg.Route(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), decision)
			}
			w := cmd.OutOrStdout()
			if decision.Matched {
				fmt.Fprintf(w, "FAST PATH MATCHED: %s (%s, installed=%v)\n", decision.App, decision.Mode, decision.Installed)
				if decision.Description != "" {
					fmt.Fprintf(w, "  %s\n", decision.Description)
				}
				if decision.Installed {
					fmt.Fprintf(w, "  Run:     %s\n", decision.RecommendedCmd)
				} else {
					fmt.Fprintf(w, "  Install: %s  (zero Go, <2s)\n", decision.InstallCmd)
					fmt.Fprintf(w, "  Then:    %s\n", decision.RecommendedCmd)
				}
				if decision.EstimatedSavings != nil {
					fmt.Fprintf(w, "  Savings: ~%dms vs ~%dms browser | ~%d tokens vs ~%d tokens\n",
						decision.EstimatedSavings.PPMS, decision.EstimatedSavings.BrowserMS,
						decision.EstimatedSavings.PPTokens, decision.EstimatedSavings.BrowserTokens)
				}
				return nil
			}
			fmt.Fprintf(w, "NO FAST PATH YET for %s\n", decision.Input)
			fmt.Fprintf(w, "  Compile once:     %s\n", decision.CompileCmd)
			fmt.Fprintf(w, "  Browser fallback: %s\n", decision.BrowserFallback)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON (auto-enabled when piped)")
	return cmd
}

func newSearchCmd() *cobra.Command {
	var category string
	var limit int
	var refresh bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search starter Cartridges and all 616+ Printing Press Library apps",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			reg, err := registry.NewClient()
			if err != nil {
				return err
			}
			cat, err := reg.Load(cmd.Context(), refresh)
			if err != nil {
				return err
			}
			hits := reg.Search(cat, query, category, limit)
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), hits)
			}
			if len(hits) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No apps matched %q. Compile any site live with `pp record --backend ardia <url>`.\n", query)
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "APP\tCATEGORY\tDESCRIPTION")
			for _, h := range hits {
				desc := h.Description
				if len(desc) > 80 {
					desc = desc[:77] + "..."
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", h.Name, h.Category, desc)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "Filter by catalog category")
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum results")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Force-refresh catalog from GitHub")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed Cartridges, embedded starter Cartridges, and pinned prebuilt binaries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			installed, _ := cartridge.Installed()
			builtins, _ := cartridge.Builtins()
			lockPath, _ := cartridge.LockfilePath()
			lf, _ := cartridge.LoadLockfile(lockPath)

			type row struct {
				Name        string `json:"name"`
				Kind        string `json:"kind"`
				Version     string `json:"version,omitempty"`
				Description string `json:"description,omitempty"`
				Source      string `json:"source,omitempty"`
			}
			seen := map[string]bool{}
			var rows []row
			for _, c := range installed {
				seen[c.Name] = true
				rows = append(rows, row{Name: c.Name, Kind: "cartridge (installed)", Version: c.Version, Description: c.Description, Source: c.Source})
			}
			for _, c := range builtins {
				if seen[c.Name] {
					continue
				}
				seen[c.Name] = true
				rows = append(rows, row{Name: c.Name, Kind: "cartridge (built-in)", Version: c.Version, Description: c.Description, Source: c.Source})
			}
			if lf != nil {
				for _, e := range lf.Entries {
					if e.Kind == "binary" && !seen[e.Name] {
						seen[e.Name] = true
						rows = append(rows, row{Name: e.Name, Kind: "prebuilt-binary", Version: e.Version, Source: e.Path})
					}
				}
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tKIND\tVERSION\tDESCRIPTION")
			for _, r := range rows {
				desc := r.Description
				if len(desc) > 72 {
					desc = desc[:69] + "..."
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, r.Kind, r.Version, desc)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newUseCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "use <app> [app...]",
		Short: "Install one or more Printing Press apps in <2s (zero Go compiler, SHA-256 verified)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := registry.NewClient()
			if err != nil {
				return err
			}
			results := make([]*registry.InstallResult, 0, len(args))
			for _, slug := range args {
				res, err := reg.Install(cmd.Context(), slug)
				if err != nil {
					return err
				}
				results = append(results, res)
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), results)
			}
			for _, r := range results {
				fmt.Fprintf(cmd.OutOrStdout(), "Installed %s (%s, %s, sha256:%s) -> %s\n",
					r.Name, r.Kind, r.Version, r.SHA256[:12], r.Path)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newRunCmd() *cobra.Command {
	var params []string
	var compact bool
	var selectFields []string
	var limit int
	var dryRun bool
	var yes bool
	var asJSON bool
	var strictLock bool

	cmd := &cobra.Command{
		Use:   "run <app> <command> [args...]",
		Short: "Execute a Cartridge command or installed prebuilt PPL CLI",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]

			// 1. Try Cartridge resolution first.
			if cart, err := cartridge.Resolve(app); err == nil {
				if len(args) < 2 {
					return &cartridge.ExecError{
						Code:    cartridge.ExitUsage,
						Class:   "usage",
						Message: fmt.Sprintf("missing command for cartridge %q", app),
						Hint:    fmt.Sprintf("run `pp info %s` to see available commands", app),
					}
				}
				cmdName := args[1]
				if lockPath, lerr := cartridge.LockfilePath(); lerr == nil {
					if lf, lerr := cartridge.LoadLockfile(lockPath); lerr == nil {
						if verr := lf.VerifyCartridge(cart, strictLock); verr != nil {
							return &ExitError{Code: 4, Err: verr}
						}
					}
				}

				paramMap := make(map[string]string, len(params))
				for _, kv := range params {
					k, v, ok := strings.Cut(kv, "=")
					if !ok || strings.TrimSpace(k) == "" {
						return &cartridge.ExecError{Code: cartridge.ExitUsage, Class: "usage", Message: fmt.Sprintf("invalid --param %q (expected key=value)", kv)}
					}
					paramMap[strings.TrimSpace(k)] = v
				}

				var journal *cartridge.Journal
				if jPath, jerr := cartridge.DefaultJournalPath(); jerr == nil {
					if j, jerr := cartridge.OpenJournal(jPath); jerr == nil {
						journal = j
						defer func() { _ = j.Close() }()
					}
				}

				res, execErr := cartridge.Execute(cmd.Context(), cart, cmdName, cartridge.RunOptions{
					Params:       paramMap,
					Compact:      compact,
					SelectFields: selectFields,
					Limit:        limit,
					DryRun:       dryRun,
					ConfirmWrite: yes,
					Journal:      journal,
				})
				if execErr != nil {
					return execErr
				}
				if wantsJSON(cmd, asJSON) {
					return writeJSON(cmd.OutOrStdout(), res)
				}
				return renderRunHuman(cmd.OutOrStdout(), res)
			}

			// 2. Fallback to installed zero-Go prebuilt PPL binary (~/.pp/bin/<app>-pp-cli).
			if binPath, ok := registry.InstalledBinaryPath(app); ok {
				subArgs := args[1:]
				proc := exec.CommandContext(cmd.Context(), binPath, subArgs...)
				proc.Stdin = cmd.InOrStdin()
				proc.Stdout = cmd.OutOrStdout()
				proc.Stderr = cmd.ErrOrStderr()
				return proc.Run()
			}

			return &cartridge.ExecError{
				Code:    cartridge.ExitNotFound,
				Class:   "usage",
				Message: fmt.Sprintf("app %q is not installed", app),
				Hint:    fmt.Sprintf("run `pp use %s` (zero Go) or `pp search %s`", app, app),
			}
		},
	}

	cmd.Flags().StringArrayVarP(&params, "param", "p", nil, "Command parameter as key=value (repeatable)")
	cmd.Flags().BoolVar(&compact, "compact", false, "Return high-gravity compact fields only (60-80% fewer tokens)")
	cmd.Flags().StringSliceVar(&selectFields, "select", nil, "Comma-separated dot-path fields to project")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum rows to return")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the resolved request without executing")
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm execution of mutating commands")
	cmd.Flags().BoolVar(&strictLock, "strict-lock", false, "Fail if cartridge is not explicitly pinned in lock.json")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON (auto-enabled when piped)")
	return cmd
}

func renderRunHuman(w io.Writer, res *cartridge.RunResult) error {
	fmt.Fprintf(w, "%s.%s (%dms, %d rows)\n", res.Cartridge, res.Command, res.LatencyMS, res.RowCount)
	rows, ok := res.Data.([]any)
	if !ok {
		return writeJSON(w, res.Data)
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no results)")
		return nil
	}
	first, ok := rows[0].(map[string]any)
	if !ok {
		return writeJSON(w, rows)
	}
	cols := make([]string, 0, len(first))
	for k := range first {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	if len(cols) > 6 {
		cols = cols[:6]
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.ToUpper(strings.Join(cols, "\t")))
	for _, item := range rows {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		vals := make([]string, 0, len(cols))
		for _, col := range cols {
			s := fmt.Sprint(m[col])
			if m[col] == nil {
				s = "-"
			}
			if len(s) > 48 {
				s = s[:45] + "..."
			}
			vals = append(vals, strings.ReplaceAll(s, "\n", " "))
		}
		fmt.Fprintln(tw, strings.Join(vals, "\t"))
	}
	return tw.Flush()
}

func newInfoCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "info <app>",
		Short: "Inspect a Cartridge's commands, parameters, trust allowlist, and SHA-256 digest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cartridge.Resolve(args[0])
			if err != nil {
				return err
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), c)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%s v%s — %s\n", c.Name, c.Version, c.Description)
			fmt.Fprintf(w, "Source: %s | SHA-256: %s\n", c.Source, c.SHA256)
			if len(c.Trust.AllowedHosts) > 0 {
				fmt.Fprintf(w, "Allowed hosts: %s\n", strings.Join(c.Trust.AllowedHosts, ", "))
			}
			fmt.Fprintln(w, "\nCommands:")
			names := make([]string, 0, len(c.Commands))
			for k := range c.Commands {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, n := range names {
				spec := c.Commands[n]
				fmt.Fprintf(w, "  %-18s (%s) %s\n", n, cartridge.EffectiveDriver(spec), spec.Description)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newRecordCmd() *cobra.Command {
	var fromHAR string
	var fromSpec string
	var backend string
	var nameOverride string
	var waitSeconds int
	var install bool
	var outPath string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "record [url]",
		Short: "Compile any website (via Ardia background browser, HAR, or OpenAPI spec) into a 15KB local Cartridge",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				res *compiler.CompileResult
				err error
			)
			switch {
			case fromHAR != "":
				res, err = compiler.CompileFromHAR(fromHAR, nameOverride)
			case fromSpec != "":
				res, err = compiler.CompileFromSpecFile(fromSpec, nameOverride)
			case len(args) == 1 && (backend == "" || strings.EqualFold(backend, "ardia")):
				res, err = compiler.RecordWithArdia(cmd.Context(), compiler.ArdiaRecordOptions{
					TargetURL:    args[0],
					NameOverride: nameOverride,
					WaitSeconds:  waitSeconds,
				})
			default:
				return &cartridge.ExecError{
					Code:    cartridge.ExitUsage,
					Class:   "usage",
					Message: "provide a target URL (with --backend ardia), --from-har <file>, or --from-spec <file>",
				}
			}
			if err != nil {
				return err
			}

			installedPath := ""
			if install {
				_, installedPath, err = cartridge.InstallBytes(res.YAMLBytes, res.Source)
				if err != nil {
					return err
				}
				if home, herr := cartridge.HomeDir(); herr == nil {
					skillDir := filepath.Join(home, "skills", "pp-"+res.Cartridge.Name)
					_ = os.MkdirAll(skillDir, 0o700)
					_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(res.SkillMD), 0o600)
				}
			}
			if outPath != "" {
				if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
					return err
				}
				if err := os.WriteFile(outPath, res.YAMLBytes, 0o600); err != nil {
					return err
				}
			}

			summary := map[string]any{
				"name":           res.Cartridge.Name,
				"commands":       res.CommandCount,
				"allowed_hosts":  res.AllowedHosts,
				"sha256":         res.Cartridge.SHA256,
				"compile_ms":     res.ElapsedMS,
				"installed_path": installedPath,
				"output_path":    outPath,
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), summary)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Compiled %s (%d commands, %dms, sha256:%s)\n",
				res.Cartridge.Name, res.CommandCount, res.ElapsedMS, res.Cartridge.SHA256[:12])
			fmt.Fprintf(w, "  Allowed hosts: %s\n", strings.Join(res.AllowedHosts, ", "))
			if installedPath != "" {
				fmt.Fprintf(w, "  Installed:     %s (pinned in lock.json)\n", installedPath)
				fmt.Fprintf(w, "  Try now:       pp info %s\n", res.Cartridge.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fromHAR, "from-har", "", "Compile from an existing HAR or EnrichedCapture file")
	cmd.Flags().StringVar(&fromSpec, "from-spec", "", "Compile from an OpenAPI or Printing Press YAML spec")
	cmd.Flags().StringVar(&backend, "backend", "ardia", "Live browser capture backend (ardia)")
	cmd.Flags().StringVar(&nameOverride, "name", "", "Override generated cartridge slug")
	cmd.Flags().IntVar(&waitSeconds, "wait", 3, "Seconds to wait for XHR/Fetch calls during live Ardia capture")
	cmd.Flags().BoolVar(&install, "install", true, "Install and pin compiled cartridge into ~/.pp/cartridges/")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Also write cartridge YAML to this path")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newLakeCmd() *cobra.Command {
	lakeCmd := &cobra.Command{
		Use:   "lake",
		Short: "Unified local SQLite + FTS5 Personal Life Lake (~/.pp/lake.db)",
	}
	lakeCmd.AddCommand(newLakeSyncCmd())
	lakeCmd.AddCommand(newLakeSearchCmd())
	lakeCmd.AddCommand(newLakeSQLCmd())
	lakeCmd.AddCommand(newLakeStatsCmd())
	return lakeCmd
}

func newLakeSyncCmd() *cobra.Command {
	var all bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "sync [app...]",
		Short: "Sync one or more Cartridges into the unified ~/.pp/lake.db FTS5 store",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := lake.DefaultPath()
			if err != nil {
				return err
			}
			lk, err := lake.Open(p)
			if err != nil {
				return err
			}
			defer func() { _ = lk.Close() }()

			var targets []*cartridge.Cartridge
			if all || len(args) == 0 {
				installed, _ := cartridge.Installed()
				targets = append(targets, installed...)
				if len(targets) == 0 {
					for _, name := range []string{"espn", "hackernews"} {
						if c, err := cartridge.Resolve(name); err == nil {
							targets = append(targets, c)
						}
					}
				}
			} else {
				for _, name := range args {
					c, err := cartridge.Resolve(name)
					if err != nil {
						return err
					}
					targets = append(targets, c)
				}
			}

			reports := make([]*lake.SyncReport, 0, len(targets))
			for _, c := range targets {
				rep, _ := lk.SyncCartridge(cmd.Context(), c, cartridge.RunOptions{})
				if rep != nil {
					reports = append(reports, rep)
				}
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), reports)
			}
			for _, r := range reports {
				fmt.Fprintf(cmd.OutOrStdout(), "%-18s %-8s %4d rows (%dms)\n", r.Source, r.Status, r.RowsSynced, r.DurationMS)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Sync all installed Cartridges")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newLakeSearchCmd() *cobra.Command {
	var source string
	var entityType string
	var limit int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "5ms FTS5 full-text search across every app synced into your Personal Life Lake",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) > 0 {
				q = args[0]
			}
			p, err := lake.DefaultPath()
			if err != nil {
				return err
			}
			lk, err := lake.Open(p)
			if err != nil {
				return err
			}
			defer func() { _ = lk.Close() }()

			items, err := lk.Search(cmd.Context(), q, source, entityType, limit)
			if err != nil {
				return err
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), items)
			}
			if len(items) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No matching items in Life Lake. Run `pp lake sync` to populate.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "SOURCE\tTYPE\tTITLE\tURL")
			for _, it := range items {
				title := it.Title
				if len(title) > 54 {
					title = title[:51] + "..."
				}
				u := it.URL
				if len(u) > 48 {
					u = u[:45] + "..."
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", it.Source, it.EntityType, title, u)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "Filter by source app")
	cmd.Flags().StringVar(&entityType, "type", "", "Filter by entity type (item, event, document, trip, order)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum rows")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newLakeSQLCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "sql <query>",
		Short: "Run a strictly read-only SQLite query against ~/.pp/lake.db",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := lake.DefaultPath()
			if err != nil {
				return err
			}
			lk, err := lake.Open(p)
			if err != nil {
				return err
			}
			defer func() { _ = lk.Close() }()

			rows, err := lk.QueryReadOnly(cmd.Context(), args[0])
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			if wantsJSON(cmd, asJSON) || true {
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newLakeStatsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show synced sources, row counts, and size of ~/.pp/lake.db",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := lake.DefaultPath()
			if err != nil {
				return err
			}
			lk, err := lake.Open(p)
			if err != nil {
				return err
			}
			defer func() { _ = lk.Close() }()

			st, err := lk.Stats(cmd.Context())
			if err != nil {
				return err
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), st)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Life Lake: %s (%d rows, %d KB)\n", st.Path, st.TotalRows, st.SizeBytes/1024)
			for _, s := range st.Sources {
				fmt.Fprintf(cmd.OutOrStdout(), "  %-18s %5d rows  (last sync %s)\n", s.Source, s.Rows, s.LastSync)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the single lean 5-tool stdio MCP server covering all 616+ apps and the Life Lake",
		RunE: func(cmd *cobra.Command, _ []string) error {
			reg, err := registry.NewClient()
			if err != nil {
				return err
			}
			var journal *cartridge.Journal
			if jp, jerr := cartridge.DefaultJournalPath(); jerr == nil {
				if j, jerr := cartridge.OpenJournal(jp); jerr == nil {
					journal = j
					defer func() { _ = j.Close() }()
				}
			}
			srv := &ppmcp.Server{
				Registry: reg,
				Journal:  journal,
			}
			return srv.ServeStdio(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

func newAuditCmd() *cobra.Command {
	var mcpConfigs []string
	var skipDefaultMCP bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Verified Local Trust Shield — verify SHA-256 pins and scan for MCP rug-pull / prompt-injection risks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths := append([]string(nil), mcpConfigs...)
			if !skipDefaultMCP {
				paths = append(paths, trust.DefaultMCPConfigPaths()...)
			}
			rep, err := trust.Audit(paths)
			if err != nil {
				return err
			}
			if wantsJSON(cmd, asJSON) {
				_ = writeJSON(cmd.OutOrStdout(), rep)
			} else {
				w := cmd.OutOrStdout()
				fmt.Fprintf(w, "Trust Shield Grade: %s (cartridges=%d binaries=%d mcp_configs=%d critical=%d warnings=%d)\n",
					rep.Grade, rep.CartridgesChecked, rep.BinariesChecked, rep.MCPConfigsChecked, rep.CriticalCount, rep.WarningCount)
				for _, f := range rep.Findings {
					fmt.Fprintf(w, "  [%s] %s (%s): %s\n", strings.ToUpper(f.Severity), f.Code, f.Target, f.Message)
					if f.Fix != "" {
						fmt.Fprintf(w, "      fix: %s\n", f.Fix)
					}
				}
			}
			if rep.CriticalCount > 0 {
				return &ExitError{Code: 3, Err: errors.New("critical trust findings detected"), Silent: true}
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&mcpConfigs, "mcp-config", nil, "Additional MCP JSON config files to audit")
	cmd.Flags().BoolVar(&skipDefaultMCP, "no-default-mcp-scan", false, "Skip scanning default local MCP config paths")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newTrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trust <app>",
		Short: "Pin or re-pin a local Cartridge's current SHA-256 digest and allowed hosts in ~/.pp/lock.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cartridge.Resolve(args[0])
			if err != nil {
				return err
			}
			lockPath, err := cartridge.LockfilePath()
			if err != nil {
				return err
			}
			lf, err := cartridge.LoadLockfile(lockPath)
			if err != nil {
				return err
			}
			lf.Pin(cartridge.LockEntry{
				Kind:         "cartridge",
				Name:         c.Name,
				Version:      c.Version,
				SHA256:       c.SHA256,
				Path:         c.Source,
				AllowedHosts: c.Trust.AllowedHosts,
			})
			if err := lf.Save(lockPath); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Pinned %s (sha256:%s, hosts:%v) in %s\n",
				c.Name, c.SHA256[:12], c.Trust.AllowedHosts, lockPath)
			return nil
		},
	}
}

func newHealCmd() *cobra.Command {
	var fromHAR string
	var backend string
	var targetURL string
	var prBundleDir string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "heal <app> <command>",
		Short: "Diagnose drift on a Cartridge command and apply a surgical patch from fresh Ardia/HAR capture",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cartridge.Resolve(args[0])
			if err != nil {
				return err
			}
			cmdName := args[1]

			var journal *cartridge.Journal
			if jp, jerr := cartridge.DefaultJournalPath(); jerr == nil {
				if j, jerr := cartridge.OpenJournal(jp); jerr == nil {
					journal = j
					defer func() { _ = j.Close() }()
				}
			}

			// If no fresh capture was supplied, run live read-only diagnosis first.
			if fromHAR == "" && backend == "" {
				diag, err := heal.Diagnose(cmd.Context(), c, cmdName, journal, cartridge.RunOptions{Journal: journal})
				if err != nil {
					return err
				}
				if wantsJSON(cmd, asJSON) {
					return writeJSON(cmd.OutOrStdout(), diag)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Diagnosis for %s.%s: %s\n  Detail: %s\n  Next:   %s\n",
					diag.Cartridge, diag.Command, diag.Class, diag.Detail, diag.NextAction)
				return nil
			}

			var compiled *compiler.CompileResult
			switch {
			case fromHAR != "":
				compiled, err = compiler.CompileFromHAR(fromHAR, c.Name)
			case strings.EqualFold(backend, "ardia"):
				u := targetURL
				if u == "" {
					u = c.Homepage
				}
				compiled, err = compiler.RecordWithArdia(cmd.Context(), compiler.ArdiaRecordOptions{
					TargetURL:    u,
					NameOverride: c.Name,
				})
			default:
				return fmt.Errorf("unsupported heal backend %q (use --backend ardia or --from-har <file>)", backend)
			}
			if err != nil {
				return err
			}

			patchRes, err := heal.ApplySurgicalPatch(c, compiled.Cartridge, cmdName, prBundleDir)
			if err != nil {
				return err
			}
			if wantsJSON(cmd, asJSON) {
				return writeJSON(cmd.OutOrStdout(), patchRes)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Healed %s.%s:\n", patchRes.Cartridge, patchRes.Command)
			for _, ch := range patchRes.Changes {
				fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", ch)
			}
			if patchRes.UpdatedPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Updated & pinned: %s\n", patchRes.UpdatedPath)
			}
			if patchRes.PRBundleDir != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  PR bundle ready:  %s\n", patchRes.PRBundleDir)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fromHAR, "from-har", "", "Fresh HAR capture to extract updated endpoint contract from")
	cmd.Flags().StringVar(&backend, "backend", "", "Capture backend to re-sniff live (ardia)")
	cmd.Flags().StringVar(&targetURL, "url", "", "Override URL to open when re-sniffing with --backend ardia")
	cmd.Flags().StringVar(&prBundleDir, "pr-bundle", "", "Directory to write a PII-scrubbed community heal PR bundle")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newArdiaCmd() *cobra.Command {
	ardiaCmd := &cobra.Command{
		Use:   "ardia",
		Short: "Integrate Printing Press 2.0 fast-path tools directly into Ardia Browser",
	}

	ardiaCmd.AddCommand(&cobra.Command{
		Use:   "hook-snippet",
		Short: "Print the Ardia hooks.json omnibox snippet for `pp`",
		RunE: func(cmd *cobra.Command, _ []string) error {
			snippet := `{
  "hooks": [
    {
      "name": "printing-press-omnibox",
      "omnibox": {
        "title": "Printing Press 2.0 (150ms Local Tools & Life Lake)",
        "keyword": "pp"
      },
      "run": ["pp", "route", "--json"]
    }
  ]
}
`
			fmt.Fprint(cmd.OutOrStdout(), snippet)
			return nil
		},
	})

	ardiaCmd.AddCommand(&cobra.Command{
		Use:   "setup",
		Short: "Install the Printing Press 2.0 plugin into Ardia Browser's plugins directory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			pluginDir := filepath.Join(home, ".config", "ardia", "plugins", "printing-press")
			binDir := filepath.Join(pluginDir, "bin")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				return err
			}
			manifest := `{
  "manifest": 2,
  "name": "printing-press",
  "title": "Printing Press 2.0",
  "description": "Skip pixels on repeat work: route URLs to 150ms local Printing Press Cartridges & 616+ CLIs, compile new sites from Ardia HAR traffic, and query your local Life Lake.",
  "version": "2.0.0",
  "requires": "0.2.48",
  "icon": "bolt.fill",
  "keywords": ["printing press", "cli", "cartridge", "agent", "fast path", "life lake"],
  "permissions": { "control": true },
  "contributes": {
    "programs": [
      {
        "name": "pp",
        "path": "bin/pp-ardia-bridge",
        "example": "pp route https://www.espn.com/nba/scoreboard"
      }
    ]
  }
}
`
			bridge := `#!/usr/bin/env sh
set -eu
if command -v pp >/dev/null 2>&1; then
  exec pp "$@"
elif [ -x "$HOME/.local/bin/pp" ]; then
  exec "$HOME/.local/bin/pp" "$@"
else
  echo '{"error":"pp binary not found"}' >&2
  exit 127
fi
`
			if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(binDir, "pp-ardia-bridge"), []byte(bridge), 0o755); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed Ardia plugin at %s\n", pluginDir)
			fmt.Fprintln(cmd.OutOrStdout(), "Verify with: ardia plugins check printing-press")
			fmt.Fprintln(cmd.OutOrStdout(), "Enable in:   Ardia > Settings > Plugins > Printing Press 2.0")
			return nil
		},
	})

	return ardiaCmd
}

func newEjectCmd() *cobra.Command {
	var outputDir string
	cmd := &cobra.Command{
		Use:   "eject <app>",
		Short: "Export a Cartridge's YAML + SKILL.md into a standalone folder for full `cli-printing-press` Go codegen",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cartridge.Resolve(args[0])
			if err != nil {
				return err
			}
			dest := outputDir
			if dest == "" {
				dest = filepath.Join(".", c.Name+"-cartridge")
			}
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			raw, err := c.MarshalYAML()
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dest, "cartridge.yaml"), raw, 0o644); err != nil {
				return err
			}
			skillMD := compiler.GenerateSkillMarkdown(c)
			if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Ejected %s into %s\n", c.Name, dest)
			fmt.Fprintf(cmd.OutOrStdout(), "For full standalone Go binary generation: cli-printing-press generate --spec <openapi.yaml> --name %s\n", c.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "Destination directory")
	return cmd
}

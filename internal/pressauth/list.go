package pressauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// listEmptyMessage is what users see when no state files exist. Hoisted
// so tests can match on it without duplicating the string.
const listEmptyMessage = "no domains captured yet — run: press-auth login <domain> --login-url <url>"

// listRow holds one row's worth of pre-rendered data so the text and JSON
// paths share the same classification logic.
type listRow struct {
	Domain     string
	CapturedAt time.Time
	JWTExpiry  time.Time
	Report     statusReport
	Corrupt    bool
}

func newListCmd(gf *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every captured domain with its capture time and JWT expiry",
		Long: `Walks the state directory (default ~/.press-auth/) and prints one row
per captured domain. Output includes the domain, when it was captured,
and when its stored JWT expires.

Use --json for machine-readable output suitable for piping into other
tools (e.g. a status dashboard).`,
		Example: `  press-auth list
  press-auth list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.OutOrStdout(), cmd.ErrOrStderr(), gf, time.Now().UTC())
		},
	}
	return cmd
}

// runList is the testable body of `list`. It walks the state dir, classifies
// each entry, and emits text or JSON. Stderr carries warnings (corrupt
// files) so they don't poison stdout consumers like a piped --json.
func runList(out, errOut io.Writer, gf *GlobalFlags, now time.Time) error {
	dir, err := StateDir()
	if err != nil {
		return &ExitError{Code: ExitUnknownError, Err: err}
	}

	rows, walkErr := collectListRows(dir, now)
	if walkErr != nil {
		return &ExitError{Code: ExitUnknownError, Err: walkErr}
	}

	if len(rows) == 0 {
		if gf != nil && gf.JSON {
			fmt.Fprintln(out, "[]")
			return nil
		}
		fmt.Fprintln(out, listEmptyMessage)
		return nil
	}

	// Stable sort by domain so output is reproducible.
	sort.Slice(rows, func(i, j int) bool { return rows[i].Domain < rows[j].Domain })

	if gf != nil && gf.JSON {
		emitListJSON(out, rows)
		return nil
	}
	emitListText(out, errOut, rows)
	return nil
}

// collectListRows walks the state directory and builds a row per .json
// file. A missing directory returns an empty slice with nil error so the
// caller can render the empty-state message identically. Read/decrypt
// failures show up as corrupt rows; only fatal walk errors short-circuit.
func collectListRows(dir string, now time.Time) ([]listRow, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading state directory: %w", err)
	}

	var rows []listRow
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		domain := strings.TrimSuffix(name, ".json")

		st, loadErr := Load(domain)
		if loadErr != nil {
			if errors.Is(loadErr, ErrStateNotFound) {
				// Disappeared between ReadDir and Load — skip quietly.
				continue
			}
			// Corrupt entry: surface the row with a recovery hint, never
			// the underlying error (which could contain bytes we'd rather
			// not log).
			rows = append(rows, listRow{
				Domain:  domain,
				Corrupt: true,
				Report: statusReport{
					Domain:   domain,
					State:    "corrupt",
					Recovery: "press-auth forget " + domain + " --yes",
					Now:      now,
				},
			})
			continue
		}
		rows = append(rows, listRow{
			Domain:     domain,
			CapturedAt: st.CapturedAt,
			JWTExpiry:  st.JWTExpiry,
			Report:     classifyStatus(domain, st, now),
		})
	}
	return rows, nil
}

func emitListText(out, errOut io.Writer, rows []listRow) {
	// Surface corrupt entries up top so the user sees them before the
	// table scrolls them off-screen.
	for _, r := range rows {
		if r.Corrupt {
			fmt.Fprintf(errOut, "warning: state file for %q failed to decrypt — run: press-auth forget %s --yes\n", r.Domain, r.Domain)
		}
	}

	tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "DOMAIN\tCAPTURED\tEXPIRY\tSTATE")
	for _, r := range rows {
		captured := formatListTime(r.CapturedAt)
		expiry := formatListTime(r.JWTExpiry)
		state := formatListState(r.Report)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Domain, captured, expiry, state)
	}
	_ = tw.Flush()
}

// formatListTime renders the timestamps in the documented compact form.
// Zero values render as "—" so the column stays width-stable.
func formatListTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04Z")
}

// formatListState produces the "STATE" column value, mirroring status's
// text output but compact enough for a tabular row.
func formatListState(r statusReport) string {
	switch r.State {
	case statusValid:
		return fmt.Sprintf("valid (%s)", humanDuration(r.Expiry.Sub(r.Now)))
	case statusNearExpiry:
		return fmt.Sprintf("near-expiry (%s)", humanDuration(r.Expiry.Sub(r.Now)))
	case statusExpired:
		return fmt.Sprintf("expired (%s ago)", humanDuration(r.Now.Sub(r.Expiry)))
	case statusFreshNoExp:
		return "fresh (no expiry)"
	case statusInvalidJWT:
		return "invalid-jwt"
	case "corrupt":
		return "corrupt"
	default:
		return string(r.State)
	}
}

func emitListJSON(out io.Writer, rows []listRow) {
	jsonRows := make([]statusJSON, 0, len(rows))
	for _, r := range rows {
		if r.Corrupt {
			jsonRows = append(jsonRows, statusJSON{
				Domain:   r.Domain,
				State:    "corrupt",
				Recovery: "press-auth forget " + r.Domain + " --yes",
			})
			continue
		}
		jsonRows = append(jsonRows, reportToJSON(r.Report))
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(jsonRows)
}

// listStateFiles returns the domain names of every *.json file in the
// state directory. Used by forget --all so it shares the same enumeration
// logic the list command uses. A missing directory returns (nil, nil).
func listStateFiles() ([]string, error) {
	dir, err := StateDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading state directory: %w", err)
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(out)
	return out, nil
}

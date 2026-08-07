package pressauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

// statusState is the categorical classification of a captured session's
// freshness. Centralised as a typed constant set so the text + JSON paths
// and the test table all share one source of truth.
type statusState string

const (
	statusValid        statusState = "valid"
	statusNearExpiry   statusState = "near-expiry"
	statusExpired      statusState = "expired"
	statusInvalidJWT   statusState = "invalid-jwt"
	statusNotCaptured  statusState = "not-captured"
	statusFreshNoExp   statusState = "fresh-no-expiry"
	statusFresh24hWall             = 24 * time.Hour
)

// statusReport is the structured shape both text and JSON output paths
// consume. Composed once by classifyStatus, then rendered by the format
// helpers below.
type statusReport struct {
	Domain     string
	State      statusState
	Expiry     time.Time
	Now        time.Time
	CapturedAt time.Time
	// Recovery is the suggested next command, e.g. "press-auth refresh <domain>".
	// Empty for states that don't need action.
	Recovery string
}

// classifyStatus is the pure-function core of `status`: given a State and
// a clock reading, return the categorical state + recovery hint. No I/O so
// it can be table-tested directly.
func classifyStatus(domain string, st *State, now time.Time) statusReport {
	r := statusReport{Domain: domain, Now: now}
	if st == nil {
		r.State = statusNotCaptured
		r.Recovery = "press-auth login " + domain
		return r
	}
	r.CapturedAt = st.CapturedAt

	if st.JWTExpiry.IsZero() {
		r.State = statusFreshNoExp
		return r
	}
	r.Expiry = st.JWTExpiry

	delta := st.JWTExpiry.Sub(now)
	switch {
	case delta <= 0:
		r.State = statusExpired
		// If the JWT has been dead for more than 24h, the refresh endpoint
		// almost certainly won't honor the refresh token either — steer the
		// user to a fresh login instead.
		if -delta > statusFresh24hWall {
			r.Recovery = "press-auth login " + domain
		} else {
			r.Recovery = "press-auth refresh " + domain
		}
	case delta < expiryRefreshWindow:
		r.State = statusNearExpiry
	default:
		r.State = statusValid
	}
	return r
}

func newStatusCmd(gf *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <domain>",
		Short: "Report whether the captured session for <domain> is still valid",
		Long: `Decodes the stored JWT carrier cookie and prints a human-readable
freshness summary, e.g. "valid until 2026-05-12T18:24:00Z (29m remaining)"
or "expired 14m ago — run press-auth refresh".

Exit codes:
  0  state is captured and the JWT is not expired (or near-expiry / fresh)
  2  no captured state for <domain>, or JWT is expired
  3  state exists but the JWT carrier cookie does not decode`,
		Example: `  press-auth status example.com
  press-auth status example.com --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain := args[0]
			if domain == "" {
				return &ExitError{Code: ExitUsageError, Err: fmt.Errorf("domain argument cannot be empty")}
			}
			return runStatus(cmd.OutOrStdout(), domain, gf, time.Now().UTC())
		},
	}
	return cmd
}

// runStatus is split out so tests can inject a fixed clock and an output
// buffer without driving the full Cobra Execute.
func runStatus(out io.Writer, domain string, gf *GlobalFlags, now time.Time) error {
	st, loadErr := Load(domain)
	if loadErr != nil {
		if errors.Is(loadErr, ErrStateNotFound) {
			r := classifyStatus(domain, nil, now)
			emitStatus(out, r, gf, true)
			// Silent: the human-readable line is already on stdout (or the
			// JSON record is). Don't re-print on stderr.
			return &ExitError{
				Code:   ExitNotCaptured,
				Err:    fmt.Errorf("%s: not captured", domain),
				Silent: true,
			}
		}
		// Decryption / read errors: surface the underlying error but keep
		// the message generic (cookie values never leak via state.go's
		// Load implementation).
		return &ExitError{Code: ExitUnknownError, Err: loadErr}
	}

	// Try to decode the JWT carrier cookie. A non-empty carrier name with
	// an unparseable value is its own terminal state, distinct from
	// expired/valid. Empty carrier name (e.g. captured without
	// --jwt-carrier-cookie) is allowed: we fall through to expiry-based
	// classification, which will hit statusFreshNoExp when JWTExpiry is
	// zero.
	if st.JWTCarrierCookie != "" {
		if _, jwtErr := decodeCarrier(st.Cookies[st.JWTCarrierCookie]); jwtErr != nil {
			r := statusReport{
				Domain:     domain,
				State:      statusInvalidJWT,
				CapturedAt: st.CapturedAt,
				Now:        now,
				Recovery:   "press-auth login " + domain,
			}
			emitStatus(out, r, gf, true)
			return &ExitError{
				Code:   ExitInvalidJWT,
				Err:    fmt.Errorf("%s: invalid JWT in carrier cookie", domain),
				Silent: true,
			}
		}
	}

	r := classifyStatus(domain, st, now)
	emitStatus(out, r, gf, false)
	if r.State == statusExpired {
		return &ExitError{
			Code:   ExitNotCaptured,
			Silent: true,
			Err:    fmt.Errorf("%s: expired", domain),
		}
	}
	return nil
}

// decodeCarrier is a thin wrapper that runs the JWT extract+decode pipeline
// on a raw cookie value. Returns the decoded claims for callers that need
// them; the status path only uses the error.
func decodeCarrier(raw string) (map[string]any, error) {
	if raw == "" {
		return nil, ErrNotJWTShape
	}
	token, err := ExtractJWT(raw)
	if err != nil {
		return nil, err
	}
	return DecodeJWT(token)
}

// emitStatus writes either the human or JSON form of the report. The
// errorMode flag is informational only: errors are still emitted via the
// returned *ExitError, but having both paths use the same emit function
// keeps the output single-sourced.
func emitStatus(out io.Writer, r statusReport, gf *GlobalFlags, _ bool) {
	if gf != nil && gf.JSON {
		emitStatusJSON(out, r)
		return
	}
	emitStatusText(out, r)
}

func emitStatusText(out io.Writer, r statusReport) {
	switch r.State {
	case statusValid:
		remaining := r.Expiry.Sub(r.Now)
		fmt.Fprintf(out, "%s: valid until %s (%s remaining)\n",
			r.Domain, r.Expiry.UTC().Format(time.RFC3339), humanDuration(remaining))
	case statusNearExpiry:
		remaining := r.Expiry.Sub(r.Now)
		fmt.Fprintf(out, "%s: near-expiry (will refresh on next cookies call) — exp %s (%s remaining)\n",
			r.Domain, r.Expiry.UTC().Format(time.RFC3339), humanDuration(remaining))
	case statusExpired:
		ago := r.Now.Sub(r.Expiry)
		fmt.Fprintf(out, "%s: expired %s ago — run: %s\n", r.Domain, humanDuration(ago), r.Recovery)
	case statusInvalidJWT:
		fmt.Fprintf(out, "%s: invalid JWT in carrier cookie — run: %s\n", r.Domain, r.Recovery)
	case statusNotCaptured:
		fmt.Fprintf(out, "%s: not captured — run: %s\n", r.Domain, r.Recovery)
	case statusFreshNoExp:
		fmt.Fprintf(out, "%s: fresh capture, expiry not set (will populate on next cookies call)\n", r.Domain)
	}
}

// statusJSON is the documented machine-readable shape.
type statusJSON struct {
	Domain           string `json:"domain"`
	State            string `json:"state"`
	Expiry           string `json:"expiry,omitempty"`
	RemainingSeconds *int64 `json:"remaining_seconds,omitempty"`
	CapturedAt       string `json:"captured_at,omitempty"`
	Recovery         string `json:"recovery,omitempty"`
}

func reportToJSON(r statusReport) statusJSON {
	out := statusJSON{
		Domain:   r.Domain,
		State:    string(r.State),
		Recovery: r.Recovery,
	}
	if !r.Expiry.IsZero() {
		out.Expiry = r.Expiry.UTC().Format(time.RFC3339)
		secs := int64(r.Expiry.Sub(r.Now).Seconds())
		out.RemainingSeconds = &secs
	}
	if !r.CapturedAt.IsZero() {
		out.CapturedAt = r.CapturedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func emitStatusJSON(out io.Writer, r statusReport) {
	// Compact single-line output: easy for shell pipelines and matches the
	// convention used elsewhere in the press for --json modes.
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(reportToJSON(r))
}

// humanDuration renders a Duration in compact "Nd / Nh / Nm / Ns" form.
// Negative durations are flipped to positive — callers add the "ago" /
// "remaining" phrasing themselves.
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d >= 24*time.Hour {
		days := int64(d / (24 * time.Hour))
		return fmt.Sprintf("%dd", days)
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int64(d/time.Hour))
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int64(d/time.Second))
}

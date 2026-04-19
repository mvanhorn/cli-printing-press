package authdoctor

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// RenderJSON writes findings and a summary to w as indented JSON.
func RenderJSON(w io.Writer, findings []Finding) error {
	payload := struct {
		Summary  Summary   `json:"summary"`
		Findings []Finding `json:"findings"`
	}{
		Summary:  Summarize(findings),
		Findings: findings,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// RenderTable writes findings as a human-friendly table to w.
// Column widths adjust to the widest entry in each column with sensible
// minimums. An empty findings slice prints a "no CLIs found" message.
func RenderTable(w io.Writer, findings []Finding) error {
	if len(findings) == 0 {
		if _, err := fmt.Fprintln(w, "No printed CLIs with auth manifests found in ~/printing-press/library/."); err != nil {
			return err
		}
		return nil
	}

	headers := []string{"API", "Type", "Env Var", "Status", "Value", "Notes"}
	rows := make([][]string, 0, len(findings))
	for _, f := range findings {
		rows = append(rows, []string{
			f.API,
			orDash(f.Type),
			orDash(f.EnvVar),
			string(f.Status),
			orDash(f.Fingerprint),
			f.Reason,
		})
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	if err := writeRow(w, widths, headers); err != nil {
		return err
	}
	sep := make([]string, len(headers))
	for i := range sep {
		sep[i] = strings.Repeat("-", widths[i])
	}
	if err := writeRow(w, widths, sep); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(w, widths, row); err != nil {
			return err
		}
	}

	s := Summarize(findings)
	if _, err := fmt.Fprintf(w, "\nSummary: %d ok, %d suspicious, %d not set, %d no auth, %d unknown\n",
		s.OK, s.Suspicious, s.NotSet, s.NoAuth, s.Unknown); err != nil {
		return err
	}

	return nil
}

// writeRow prints one table row with left-aligned cells padded to widths.
func writeRow(w io.Writer, widths []int, cells []string) error {
	var b strings.Builder
	for i, cell := range cells {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(cell)
		if i < len(cells)-1 {
			pad := widths[i] - len(cell)
			if pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

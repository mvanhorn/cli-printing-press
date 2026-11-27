package cartridge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// DefaultMaxOutputChars matches Ardia's 12,000-char safety budget so no single
// tool call blows an agent's context window with a 200K-char dump.
const DefaultMaxOutputChars = 12000

// HandoffHint gives agents a structured, non-focus-stealing recovery action
// whenever a workflow hits a human-only gate (login, 2FA, Passkey, CAPTCHA,
// or payment confirmation).
type HandoffHint struct {
	Action     string `json:"action"` // "sign_in", "passkey_or_2fa", "payment_confirmation"
	URL        string `json:"url,omitempty"`
	BrowserCmd string `json:"browser_cmd,omitempty"`
	Reason     string `json:"reason"`
	ResumeCmd  string `json:"resume_cmd,omitempty"`
}

// WrapUntrustedContent surrounds external API or scraped text with a fresh
// per-call random-nonce prompt-injection fence (following Ardia's
// AGENT_BROWSER_CONTENT_BOUNDARIES pattern). Pass disable=true or set
// PP_CONTENT_BOUNDARIES=0 to return text unchanged.
func WrapUntrustedContent(text string, source string, disable bool) string {
	if disable || strings.TrimSpace(os.Getenv("PP_CONTENT_BOUNDARIES")) == "0" {
		return text
	}
	var nonceBytes [8]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		return text
	}
	nonce := hex.EncodeToString(nonceBytes[:])
	label := strings.TrimSpace(source)
	if label == "" {
		label = "external"
	}
	return fmt.Sprintf(
		"<<<PP_UNTRUSTED_DATA_%s source=%q>>>\n%s\n<<<END_PP_UNTRUSTED_DATA_%s>>>",
		nonce, label, text, nonce,
	)
}

// CapOutput bounds a serialized payload to maxChars runes/bytes (when maxChars > 0)
// and reports whether truncation occurred so callers never silently drop data.
func CapOutput(text string, maxChars int) (string, bool) {
	if maxChars <= 0 || len(text) <= maxChars {
		return text, false
	}
	return text[:maxChars] + fmt.Sprintf("\n... [output capped at %d chars; re-run with --full or --limit]", maxChars), true
}

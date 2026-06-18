package piiplaceholders

import (
	"regexp"
	"strings"
)

const (
	SyntheticOrderID        = "111-1111111-1111111"
	SyntheticDigitalOrderID = "D01-1111111-1111111"
	SyntheticCardLast4      = "LAST4"
	SyntheticRecipient      = "Test User"
	SyntheticAddress        = "123 Test St, Anytown, ST 12345"
	SyntheticMoney          = "12.34"
	SyntheticDate           = "2026-01-15"
	SyntheticCookieValue    = "your-cookie-here"
	SyntheticUUID           = "550e8400-e29b-41d4-a716-446655440000"
	PrimarySyntheticASIN    = "B0EXAMPLE1"
)

var syntheticASINs = [...]string{
	"B0EXAMPLE1",
	"B0EXAMPLE2",
	"B0EXAMPLE3",
	"B0EXAMPLE4",
	"B0EXAMPLE5",
	"B0EXAMPLE6",
	"B0EXAMPLE7",
	"B0EXAMPLE8",
}

var (
	orderIDExampleRE   = regexp.MustCompile(`\b(?:\d{3}|D01)-\d{7}-\d{7}\b`)
	asinExampleRE      = regexp.MustCompile(`\bB0[A-Z0-9]{8}\b`)
	cardLast4ExampleRE = regexp.MustCompile(`(?i)((?:card|visa|mastercard|amex|ending in|last\s+4)[^0-9]{0,10})\d{4}`)
	placeholderRE      = regexp.MustCompile(`^your-[a-z0-9-]+-here$`)
	xPlaceholderRE     = regexp.MustCompile(`^x+$`)
)

func OrderIDPattern() *regexp.Regexp {
	return orderIDExampleRE
}

func ASINPattern() *regexp.Regexp {
	return asinExampleRE
}

// SanitizeCapturedExamples replaces captured-value examples in user-authored
// parameter descriptions with canonical synthetic values.
func SanitizeCapturedExamples(description string) string {
	out := orderIDExampleRE.ReplaceAllStringFunc(description, func(match string) string {
		if strings.HasPrefix(strings.ToUpper(match), "D01-") {
			return SyntheticDigitalOrderID
		}
		return SyntheticOrderID
	})
	out = asinExampleRE.ReplaceAllString(out, PrimarySyntheticASIN)
	out = cardLast4ExampleRE.ReplaceAllString(out, "${1}"+SyntheticCardLast4)
	return out
}

func IsSyntheticOrderID(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == strings.ToLower(SyntheticOrderID) ||
		normalized == strings.ToLower(SyntheticDigitalOrderID)
}

func IsSyntheticASIN(value string) bool {
	for _, candidate := range syntheticASINs {
		if strings.EqualFold(candidate, strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

func IsSyntheticPostalAddress(value string) bool {
	return strings.HasPrefix(strings.ToLower(SyntheticAddress), strings.ToLower(strings.TrimSpace(value)))
}

// IsSyntheticCookieValue reports whether a spec-declared cookie assignment
// uses placeholder text rather than captured session material.
func IsSyntheticCookieValue(value string) bool {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`))
	if normalized == "" {
		return true
	}
	switch normalized {
	case "example", "example-cookie", SyntheticCookieValue, "<redacted>", "redacted":
		return true
	}
	return placeholderRE.MatchString(normalized) || xPlaceholderRE.MatchString(normalized)
}

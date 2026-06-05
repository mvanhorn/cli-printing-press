package ble

import (
	"fmt"
	"regexp"
	"strings"
)

func RedactEvidence(input EvidenceInput) EvidenceInput {
	redactionTerms := append([]string(nil), input.RedactionTerms...)
	input.RedactionTerms = nil
	redactionPatterns := buildRedactionPatterns(redactionTerms)

	input.Name = redactSensitiveTerms(input.Name, redactionPatterns)
	input.DisplayName = redactSensitiveTerms(input.DisplayName, redactionPatterns)

	input.Identity.AdvertisedNames = append([]string(nil), input.Identity.AdvertisedNames...)
	input.Events = append([]Event(nil), input.Events...)
	input.Actions = append([]ActionMarker(nil), input.Actions...)
	input.CommunityReferences = append([]CommunityReference(nil), input.CommunityReferences...)
	addresses := map[string]string{}
	nextAddress := 1
	for i := range input.Events {
		event := &input.Events[i]
		event.DeviceName = redactSensitiveTerms(event.DeviceName, redactionPatterns)
		if event.DeviceAddress == "" {
			continue
		}
		if addresses[event.DeviceAddress] == "" {
			addresses[event.DeviceAddress] = fmt.Sprintf("device-%d", nextAddress)
			nextAddress++
		}
		event.DeviceAddress = addresses[event.DeviceAddress]
	}
	for i, name := range input.Identity.AdvertisedNames {
		input.Identity.AdvertisedNames[i] = redactSensitiveTerms(name, redactionPatterns)
	}
	// Action labels and community command names are slugged into device-spec
	// command names and evidence summaries, so an unredacted term here would
	// leak into the very artifact redaction is meant to make shareable.
	for i := range input.Actions {
		input.Actions[i].Label = redactSensitiveTerms(input.Actions[i].Label, redactionPatterns)
	}
	for i := range input.CommunityReferences {
		input.CommunityReferences[i].CommandName = redactSensitiveTerms(input.CommunityReferences[i].CommandName, redactionPatterns)
	}
	return input
}

func buildRedactionPatterns(terms []string) []*regexp.Regexp {
	effective := effectiveRedactionTerms(terms)
	patterns := make([]*regexp.Regexp, 0, len(effective)*2)
	for _, term := range effective {
		// Case-insensitive: a term the operator asked to redact must not survive
		// just because the advertised name used different casing. Match the
		// possessive form first so "Owner's" collapses before "Owner".
		patterns = append(patterns, regexp.MustCompile("(?i)"+regexp.QuoteMeta(strings.TrimSuffix(term, "'s")+"'s")))
		patterns = append(patterns, regexp.MustCompile("(?i)"+regexp.QuoteMeta(term)))
	}
	return patterns
}

// effectiveRedactionTerms returns the normalized, non-empty terms that drive a
// redaction. The CLI archive note keys off these so it never claims name terms
// were redacted when every supplied term was blank, and never stays silent when
// terms arrive from the evidence file rather than --redact-term.
func effectiveRedactionTerms(terms []string) []string {
	effective := make([]string, 0, len(terms))
	for _, term := range terms {
		if term = strings.TrimSpace(term); term != "" {
			effective = append(effective, term)
		}
	}
	return effective
}

// HasEffectiveRedactionTerms reports whether terms contains at least one term
// that survives normalization and will drive a name redaction. Callers use it
// to describe redaction accurately without duplicating the normalization rule.
func HasEffectiveRedactionTerms(terms []string) bool {
	return len(effectiveRedactionTerms(terms)) > 0
}

func redactSensitiveTerms(value string, patterns []*regexp.Regexp) string {
	for _, pattern := range patterns {
		value = pattern.ReplaceAllString(value, "redacted")
	}
	return value
}

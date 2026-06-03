package ble

import (
	"fmt"
	"regexp"
	"strings"
)

func RedactEvidence(input EvidenceInput) EvidenceInput {
	redactionTerms := append([]string(nil), input.RedactionTerms...)
	input.RedactionTerms = nil

	input.Name = redactSensitiveTerms(input.Name, redactionTerms)
	input.DisplayName = redactSensitiveTerms(input.DisplayName, redactionTerms)

	input.Identity.AdvertisedNames = append([]string(nil), input.Identity.AdvertisedNames...)
	input.Events = append([]Event(nil), input.Events...)
	input.Actions = append([]ActionMarker(nil), input.Actions...)
	input.CommunityReferences = append([]CommunityReference(nil), input.CommunityReferences...)
	addresses := map[string]string{}
	nextAddress := 1
	for i := range input.Events {
		event := &input.Events[i]
		event.DeviceName = redactSensitiveTerms(event.DeviceName, redactionTerms)
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
		input.Identity.AdvertisedNames[i] = redactSensitiveTerms(name, redactionTerms)
	}
	// Action labels and community command names are slugged into device-spec
	// command names and evidence summaries, so an unredacted term here would
	// leak into the very artifact redaction is meant to make shareable.
	for i := range input.Actions {
		input.Actions[i].Label = redactSensitiveTerms(input.Actions[i].Label, redactionTerms)
	}
	for i := range input.CommunityReferences {
		input.CommunityReferences[i].CommandName = redactSensitiveTerms(input.CommunityReferences[i].CommandName, redactionTerms)
	}
	return input
}

func redactSensitiveTerms(value string, terms []string) string {
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		// Case-insensitive: a term the operator asked to redact must not survive
		// just because the advertised name used different casing. Match the
		// possessive form first so "Owner's" collapses before "Owner".
		value = replaceAllFold(value, strings.TrimSuffix(term, "'s")+"'s", "redacted")
		value = replaceAllFold(value, term, "redacted")
	}
	return value
}

// replaceAllFold replaces every case-insensitive occurrence of term in value
// with replacement. term is matched literally (regex metacharacters escaped).
func replaceAllFold(value, term, replacement string) string {
	if term == "" {
		return value
	}
	return regexp.MustCompile("(?i)"+regexp.QuoteMeta(term)).ReplaceAllString(value, replacement)
}

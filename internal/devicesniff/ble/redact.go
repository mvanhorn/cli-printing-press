package ble

import (
	"fmt"
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
	return input
}

func redactSensitiveTerms(value string, terms []string) string {
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		value = strings.ReplaceAll(value, strings.TrimSuffix(term, "'s")+"'s", "redacted")
		value = strings.ReplaceAll(value, term, "redacted")
	}
	return value
}

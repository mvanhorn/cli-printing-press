package ble

import (
	"fmt"
	"strings"
)

func RedactEvidence(input EvidenceInput) EvidenceInput {
	addresses := map[string]string{}
	nextAddress := 1
	for i := range input.Events {
		event := &input.Events[i]
		event.DeviceName = redactHumanNames(event.DeviceName)
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
		input.Identity.AdvertisedNames[i] = redactHumanNames(name)
	}
	return input
}

func redactHumanNames(value string) string {
	value = strings.ReplaceAll(value, "Trevin", "redacted")
	value = strings.ReplaceAll(value, "Trevin's", "redacted")
	return value
}

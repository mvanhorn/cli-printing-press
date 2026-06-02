package ble

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
)

const actionCorrelationWindow = 2 * time.Second

func AnalyzeEvidence(input EvidenceInput) (*Analysis, error) {
	input = RedactEvidence(input)

	spec := &devicespec.DeviceSpec{
		Version:      devicespec.SupportedVersion,
		Name:         input.Name,
		DisplayName:  input.DisplayName,
		Protocol:     devicespec.ProtocolBLE,
		Identity:     input.Identity,
		BLE:          buildBLESurface(input.Events),
		Capabilities: devicespec.DeviceCapabilities{},
		Session:      devicespec.SessionProfile{Mode: devicespec.SessionModeOneShot},
		Evidence:     buildEvidence(input),
	}

	report := Report{}
	report.DeviceCandidates = deviceCandidates(input.Events)
	if len(report.DeviceCandidates) > 1 {
		report.RequiresOperatorSelection = true
		report.Warnings = append(report.Warnings, "multiple matching BLE devices; require explicit operator selection")
	}

	commands, summaries, ambiguities, err := inferCommands(input)
	if err != nil {
		return nil, err
	}
	spec.Capabilities.Commands = append(spec.Capabilities.Commands, commands...)
	report.CommandCandidates = append(report.CommandCandidates, summaries...)
	report.Ambiguities = append(report.Ambiguities, ambiguities...)

	communityCommands, communitySummaries, err := commandsFromCommunityReferences(input.CommunityReferences)
	if err != nil {
		return nil, err
	}
	spec.Capabilities.Commands = append(spec.Capabilities.Commands, communityCommands...)
	report.CommandCandidates = append(report.CommandCandidates, communitySummaries...)

	telemetry, telemetrySummaries := inferTelemetry(input.Events)
	spec.Capabilities.Telemetry = telemetry
	report.TelemetryCandidates = telemetrySummaries

	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &Analysis{Spec: spec, Report: report}, nil
}

func buildBLESurface(events []Event) devicespec.BLESurface {
	services := map[string]map[string]devicespec.BLECharacteristic{}
	for _, event := range events {
		if event.Type != EventServiceDiscovery || event.ServiceUUID == "" || event.CharacteristicUUID == "" {
			continue
		}
		serviceUUID := normalizeUUID(event.ServiceUUID)
		charUUID := normalizeUUID(event.CharacteristicUUID)
		if services[serviceUUID] == nil {
			services[serviceUUID] = map[string]devicespec.BLECharacteristic{}
		}
		services[serviceUUID][charUUID] = devicespec.BLECharacteristic{
			UUID:       charUUID,
			Properties: append([]string(nil), event.Properties...),
		}
	}
	serviceUUIDs := make([]string, 0, len(services))
	for uuid := range services {
		serviceUUIDs = append(serviceUUIDs, uuid)
	}
	sort.Strings(serviceUUIDs)
	out := devicespec.BLESurface{Services: make([]devicespec.BLEService, 0, len(serviceUUIDs))}
	for _, serviceUUID := range serviceUUIDs {
		charUUIDs := make([]string, 0, len(services[serviceUUID]))
		for uuid := range services[serviceUUID] {
			charUUIDs = append(charUUIDs, uuid)
		}
		sort.Strings(charUUIDs)
		service := devicespec.BLEService{UUID: serviceUUID}
		for _, charUUID := range charUUIDs {
			service.Characteristics = append(service.Characteristics, services[serviceUUID][charUUID])
		}
		out.Services = append(out.Services, service)
	}
	return out
}

func buildEvidence(input EvidenceInput) []devicespec.EvidenceRef {
	out := make([]devicespec.EvidenceRef, 0, len(input.Actions)+len(input.Events)+len(input.CommunityReferences))
	for _, action := range input.Actions {
		out = append(out, devicespec.EvidenceRef{ID: action.ID, Source: "action-journal", Summary: action.Label})
	}
	for _, event := range input.Events {
		if event.ID == "" {
			continue
		}
		out = append(out, devicespec.EvidenceRef{ID: event.ID, Source: "ble-event", Summary: event.Type})
	}
	for _, ref := range input.CommunityReferences {
		out = append(out, devicespec.EvidenceRef{ID: ref.ID, Source: "community-reference", Summary: ref.CommandName})
	}
	return out
}

func inferCommands(input EvidenceInput) ([]devicespec.DeviceCommand, []CandidateSummary, []string, error) {
	var commands []devicespec.DeviceCommand
	var summaries []CandidateSummary
	var ambiguities []string
	for _, action := range input.Actions {
		writes, err := writesNearAction(input.Events, action)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(writes) == 0 {
			continue
		}
		if len(writes) > 1 {
			ambiguities = append(ambiguities, fmt.Sprintf("%s matched multiple writes: %s", action.ID, joinEventIDs(writes)))
			continue
		}
		write := writes[0]
		payload, err := decodeHex(write.ValueHex)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s value_hex: %w", write.ID, err)
		}
		evidenceRefs := []string{action.ID, write.ID}
		if notify := firstNotificationAfter(input.Events, write); notify.ID != "" {
			evidenceRefs = append(evidenceRefs, notify.ID)
		}
		name := slug(action.Label)
		commands = append(commands, devicespec.DeviceCommand{
			Name:               name,
			CharacteristicUUID: normalizeUUID(write.CharacteristicUUID),
			Safety:             action.Safety,
			ValidationStatus:   devicespec.ValidationStatusObserved,
			EvidenceRefs:       evidenceRefs,
			Payload: devicespec.DevicePayload{
				Encoding: devicespec.PayloadEncodingHex,
				Bytes:    payload,
			},
		})
		summaries = append(summaries, CandidateSummary{
			Name:         name,
			Summary:      fmt.Sprintf("%s correlated with %s", action.ID, write.ID),
			EvidenceRefs: evidenceRefs,
		})
	}
	return commands, summaries, ambiguities, nil
}

func commandsFromCommunityReferences(refs []CommunityReference) ([]devicespec.DeviceCommand, []CandidateSummary, error) {
	commands := make([]devicespec.DeviceCommand, 0, len(refs))
	summaries := make([]CandidateSummary, 0, len(refs))
	for _, ref := range refs {
		payload, err := decodeHex(ref.PayloadHex)
		if err != nil {
			return nil, nil, fmt.Errorf("%s payload_hex: %w", ref.ID, err)
		}
		name := slug(ref.CommandName)
		commands = append(commands, devicespec.DeviceCommand{
			Name:               name,
			CharacteristicUUID: normalizeUUID(ref.CharacteristicUUID),
			Safety:             ref.Safety,
			ValidationStatus:   devicespec.ValidationStatusInferred,
			EvidenceRefs:       []string{ref.ID},
			Payload: devicespec.DevicePayload{
				Encoding: devicespec.PayloadEncodingHex,
				Bytes:    payload,
			},
		})
		summaries = append(summaries, CandidateSummary{
			Name:         name,
			Summary:      fmt.Sprintf("%s supplied by community reference", ref.CommandName),
			EvidenceRefs: []string{ref.ID},
		})
	}
	return commands, summaries, nil
}

func inferTelemetry(events []Event) ([]devicespec.TelemetryField, []CandidateSummary) {
	byCharacteristic := map[string]map[string]bool{}
	for _, event := range events {
		if event.Type != EventNotification || event.CharacteristicUUID == "" {
			continue
		}
		uuid := normalizeUUID(event.CharacteristicUUID)
		if byCharacteristic[uuid] == nil {
			byCharacteristic[uuid] = map[string]bool{}
		}
		byCharacteristic[uuid][strings.ToLower(event.ValueHex)] = true
	}
	var uuids []string
	for uuid, values := range byCharacteristic {
		if len(values) > 1 {
			uuids = append(uuids, uuid)
		}
	}
	sort.Strings(uuids)
	fields := make([]devicespec.TelemetryField, 0, len(uuids))
	summaries := make([]CandidateSummary, 0, len(uuids))
	for _, uuid := range uuids {
		name := uuid + "_notification"
		fields = append(fields, devicespec.TelemetryField{
			Name:                     name,
			SourceCharacteristicUUID: uuid,
			SampleCadence:            "notification",
			Store:                    true,
		})
		summaries = append(summaries, CandidateSummary{
			Name:    name,
			Summary: fmt.Sprintf("%s emitted changing notification values", uuid),
		})
	}
	return fields, summaries
}

func writesNearAction(events []Event, action ActionMarker) ([]Event, error) {
	actionTime, err := time.Parse(time.RFC3339, action.At)
	if err != nil {
		return nil, fmt.Errorf("%s at: %w", action.ID, err)
	}
	var writes []Event
	for _, event := range events {
		if event.Type != EventWrite {
			continue
		}
		eventTime, err := time.Parse(time.RFC3339, event.At)
		if err != nil {
			return nil, fmt.Errorf("%s at: %w", event.ID, err)
		}
		if eventTime.Before(actionTime) || eventTime.Sub(actionTime) > actionCorrelationWindow {
			continue
		}
		writes = append(writes, event)
	}
	sort.Slice(writes, func(i, j int) bool { return writes[i].ID < writes[j].ID })
	return writes, nil
}

func firstNotificationAfter(events []Event, write Event) Event {
	writeTime, err := time.Parse(time.RFC3339, write.At)
	if err != nil {
		return Event{}
	}
	var notifications []Event
	for _, event := range events {
		if event.Type != EventNotification {
			continue
		}
		eventTime, err := time.Parse(time.RFC3339, event.At)
		if err != nil || eventTime.Before(writeTime) || eventTime.Sub(writeTime) > actionCorrelationWindow {
			continue
		}
		notifications = append(notifications, event)
	}
	sort.Slice(notifications, func(i, j int) bool { return notifications[i].At < notifications[j].At })
	if len(notifications) == 0 {
		return Event{}
	}
	return notifications[0]
}

func deviceCandidates(events []Event) []DeviceCandidate {
	seen := map[string]DeviceCandidate{}
	for _, event := range events {
		if event.Type != EventAdvertisement || event.DeviceAddress == "" {
			continue
		}
		seen[event.DeviceAddress] = DeviceCandidate{
			Address:      event.DeviceAddress,
			Name:         event.DeviceName,
			RSSI:         event.RSSI,
			ServiceUUIDs: append([]string(nil), event.ServiceUUIDs...),
		}
	}
	addresses := make([]string, 0, len(seen))
	for address := range seen {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	out := make([]DeviceCandidate, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, seen[address])
	}
	return out
}

func joinEventIDs(events []Event) string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}

func decodeHex(value string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return hex.DecodeString(strings.TrimSpace(value))
}

func normalizeUUID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

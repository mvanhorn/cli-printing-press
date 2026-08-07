package ble

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

const actionCorrelationWindow = 2 * time.Second

func AnalyzeEvidence(input EvidenceInput) (*Analysis, error) {
	input = RedactEvidence(input)

	spec := &devicespec.DeviceSpec{
		Version: devicespec.SupportedVersion,
		// Slug the (already-redacted) name so an arbitrary advertised device
		// name never aborts Validate's IsSlug check; DisplayName keeps the prose.
		Name:         naming.Slug(input.Name),
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

	communityCommands, communitySummaries, communityAmbiguities, err := commandsFromCommunityReferences(input.CommunityReferences)
	if err != nil {
		return nil, err
	}
	spec.Capabilities.Commands = append(spec.Capabilities.Commands, communityCommands...)
	report.CommandCandidates = append(report.CommandCandidates, communitySummaries...)
	report.Ambiguities = append(report.Ambiguities, communityAmbiguities...)

	telemetry, telemetrySummaries := inferTelemetry(input.Events)
	spec.Capabilities.Telemetry = telemetry
	report.TelemetryCandidates = telemetrySummaries

	// Build the set of discovered characteristics and drop any
	// commands/telemetry that reference an undiscovered characteristic.
	discoveredChars := discoveredCharacteristicSet(spec.BLE.Services)
	spec.Capabilities.Commands, report.Ambiguities = dropUndiscoveredCommands(spec.Capabilities.Commands, discoveredChars, report.Ambiguities)
	spec.Capabilities.Telemetry, report.Ambiguities = dropUndiscoveredTelemetry(spec.Capabilities.Telemetry, discoveredChars, report.Ambiguities)

	report.CommandDiscovery = buildCommandDiscovery(input, spec.Capabilities.Commands, report.Ambiguities)
	if report.CommandDiscovery.Status == CommandDiscoveryInsufficientGuidance && len(report.CommandDiscovery.WritableTargets) > 0 {
		report.Warnings = append(report.Warnings, "writable BLE characteristics found, but no commands emitted without guidance or replay validation")
	}

	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &Analysis{Spec: spec, Report: report}, nil
}

// discoveredCharacteristicSet returns the normalized UUIDs of every
// characteristic present in the BLE service surface.
func discoveredCharacteristicSet(services []devicespec.BLEService) map[string]bool {
	set := map[string]bool{}
	for _, svc := range services {
		for _, ch := range svc.Characteristics {
			set[normalizeUUID(ch.UUID)] = true
		}
	}
	return set
}

// dropUndiscoveredCommands removes commands whose CharacteristicUUID is not in
// the discovered set, appending an ambiguity for each dropped command.
func dropUndiscoveredCommands(commands []devicespec.DeviceCommand, discovered map[string]bool, ambiguities []string) ([]devicespec.DeviceCommand, []string) {
	kept := commands[:0]
	for _, cmd := range commands {
		if discovered[normalizeUUID(cmd.CharacteristicUUID)] {
			kept = append(kept, cmd)
		} else {
			ambiguities = append(ambiguities, fmt.Sprintf("command %q references characteristic %q which was not service-discovered; dropped", cmd.Name, cmd.CharacteristicUUID))
		}
	}
	return kept, ambiguities
}

// dropUndiscoveredTelemetry removes telemetry fields whose
// SourceCharacteristicUUID is not in the discovered set, appending an
// ambiguity for each dropped field.
func dropUndiscoveredTelemetry(telemetry []devicespec.TelemetryField, discovered map[string]bool, ambiguities []string) ([]devicespec.TelemetryField, []string) {
	kept := telemetry[:0]
	for _, field := range telemetry {
		if discovered[normalizeUUID(field.SourceCharacteristicUUID)] {
			kept = append(kept, field)
		} else {
			ambiguities = append(ambiguities, fmt.Sprintf("telemetry field %q references characteristic %q which was not service-discovered; dropped", field.Name, field.SourceCharacteristicUUID))
		}
	}
	return kept, ambiguities
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
		out = append(out, devicespec.EvidenceRef{ID: action.ID, Source: EvidenceSourceActionJournal, Summary: action.Label})
	}
	for _, event := range input.Events {
		if event.ID == "" {
			continue
		}
		out = append(out, devicespec.EvidenceRef{ID: event.ID, Source: EvidenceSourceBLEEvent, Summary: event.Type})
	}
	for _, ref := range input.CommunityReferences {
		out = append(out, devicespec.EvidenceRef{ID: ref.ID, Source: EvidenceSourceCommunityReference, Summary: ref.CommandName})
	}
	return out
}

// safetyOrUnknown returns the safety string, defaulting to SafetyUnknown when
// the value is empty or whitespace-only.
func safetyOrUnknown(safety string) string {
	if strings.TrimSpace(safety) == "" {
		return devicespec.SafetyUnknown
	}
	return safety
}

// commandNameFromLabel attempts to derive a slug from label, falling back to
// fallbackID. Returns ("", true) when both produce an empty slug, signalling
// the caller should skip the command.
func commandNameFromLabel(label, fallbackID string) (name string, skip bool) {
	name = naming.Slug(label)
	if name != "" {
		return name, false
	}
	name = naming.Slug(fallbackID)
	if name != "" {
		return name, false
	}
	return "", true
}

func inferCommands(input EvidenceInput) ([]devicespec.DeviceCommand, []CandidateSummary, []string, error) {
	var commands []devicespec.DeviceCommand
	var summaries []CandidateSummary
	var ambiguities []string
	seenTimestampAmbiguity := map[string]bool{}
	for _, action := range input.Actions {
		writes, timestampAmbiguities := writesNearAction(input.Events, action)
		for _, ambiguity := range timestampAmbiguities {
			if seenTimestampAmbiguity[ambiguity] {
				continue
			}
			seenTimestampAmbiguity[ambiguity] = true
			ambiguities = append(ambiguities, ambiguity)
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
			ambiguities = append(ambiguities, fmt.Sprintf("%s has invalid value_hex %q; skipped: %v", write.ID, write.ValueHex, err))
			continue
		}
		name, skip := commandNameFromLabel(action.Label, action.ID)
		if skip {
			ambiguities = append(ambiguities, fmt.Sprintf("%s produced no usable command name; skipped", action.ID))
			continue
		}
		evidenceRefs := []string{action.ID, write.ID}
		if notify := firstNotificationAfter(input.Events, write); notify.ID != "" {
			evidenceRefs = append(evidenceRefs, notify.ID)
		}
		commands = append(commands, devicespec.DeviceCommand{
			Name:               name,
			CharacteristicUUID: normalizeUUID(write.CharacteristicUUID),
			Safety:             safetyOrUnknown(action.Safety),
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

func commandsFromCommunityReferences(refs []CommunityReference) ([]devicespec.DeviceCommand, []CandidateSummary, []string, error) {
	commands := make([]devicespec.DeviceCommand, 0, len(refs))
	summaries := make([]CandidateSummary, 0, len(refs))
	var ambiguities []string
	for _, ref := range refs {
		payload, err := decodeHex(ref.PayloadHex)
		if err != nil {
			ambiguities = append(ambiguities, fmt.Sprintf("%s has invalid payload_hex %q; skipped: %v", ref.ID, ref.PayloadHex, err))
			continue
		}
		name, skip := commandNameFromLabel(ref.CommandName, ref.ID)
		if skip {
			ambiguities = append(ambiguities, fmt.Sprintf("%s produced no usable command name; skipped", ref.ID))
			continue
		}
		commands = append(commands, devicespec.DeviceCommand{
			Name:               name,
			CharacteristicUUID: normalizeUUID(ref.CharacteristicUUID),
			Safety:             safetyOrUnknown(ref.Safety),
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
	return commands, summaries, ambiguities, nil
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

func buildCommandDiscovery(input EvidenceInput, commands []devicespec.DeviceCommand, ambiguities []string) CommandDiscovery {
	sources := commandGuidanceSources(input, commands)
	targets := writableTargets(input.Events)
	status := CommandDiscoveryNoSurface
	replayValidated := hasReplayValidatedCommand(commands)
	switch {
	case replayValidated:
		status = CommandDiscoveryReplayValidated
	case len(commands) > 0:
		status = CommandDiscoveryGuidedCandidates
	case len(ambiguities) > 0 && len(sources) > 0:
		status = CommandDiscoveryAmbiguousGuidance
	case len(targets) > 0:
		status = CommandDiscoveryInsufficientGuidance
	}
	return CommandDiscovery{
		Status:               status,
		GuidanceSources:      sources,
		WritableTargets:      targets,
		Message:              commandDiscoveryMessage(status),
		ReplayValidationNext: status == CommandDiscoveryGuidedCandidates && !replayValidated,
	}
}

func commandGuidanceSources(input EvidenceInput, commands []devicespec.DeviceCommand) []string {
	seen := map[string]bool{}
	var sources []string
	add := func(source string) {
		if seen[source] {
			return
		}
		seen[source] = true
		sources = append(sources, source)
	}
	if len(input.Actions) > 0 {
		add(GuidanceSourceActionJournal)
	}
	if len(input.CommunityReferences) > 0 {
		add(GuidanceSourceCommunityReference)
	}
	if hasReplayValidatedCommand(commands) {
		add(GuidanceSourceReplayValidation)
	}
	return sources
}

func hasReplayValidatedCommand(commands []devicespec.DeviceCommand) bool {
	for _, command := range commands {
		if command.ValidationStatus == devicespec.ValidationStatusReplayValidated {
			return true
		}
	}
	return false
}

func writableTargets(events []Event) []string {
	seen := map[string]bool{}
	for _, event := range events {
		if event.CharacteristicUUID == "" {
			continue
		}
		switch event.Type {
		case EventServiceDiscovery:
			if !hasWriteProperty(event.Properties) {
				continue
			}
		case EventWrite:
		default:
			continue
		}
		seen[normalizeUUID(event.CharacteristicUUID)] = true
	}
	targets := make([]string, 0, len(seen))
	for target := range seen {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

func hasWriteProperty(properties []string) bool {
	for _, property := range properties {
		switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(property), "_", "-")) {
		case "write", "write-without-response", "writewithoutresponse":
			return true
		}
	}
	return false
}

func commandDiscoveryMessage(status string) string {
	switch status {
	case CommandDiscoveryReplayValidated:
		return "Replay validation confirmed at least one command payload."
	case CommandDiscoveryGuidedCandidates:
		return "Command candidates came from guidance evidence and should be replay-validated before printing a control CLI."
	case CommandDiscoveryAmbiguousGuidance:
		return "Guidance was present, but the BLE write evidence is ambiguous; collect a narrower action journal or replay validation."
	case CommandDiscoveryInsufficientGuidance:
		return "GATT surface does not advertise command semantics; provide a library, docs, action journal, imported capture, or replay validation before emitting commands."
	default:
		return "No writable BLE command surface was discovered."
	}
}

func writesNearAction(events []Event, action ActionMarker) ([]Event, []string) {
	actionTime, err := time.Parse(time.RFC3339, action.At)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s has invalid action timestamp %q; skipped: %v", action.ID, action.At, err)}
	}
	var writes []Event
	var ambiguities []string
	for _, event := range events {
		if event.Type != EventWrite {
			continue
		}
		eventTime, err := time.Parse(time.RFC3339, event.At)
		if err != nil {
			ambiguities = append(ambiguities, fmt.Sprintf("%s has invalid write timestamp %q; skipped: %v", event.ID, event.At, err))
			continue
		}
		if eventTime.Before(actionTime) || eventTime.Sub(actionTime) > actionCorrelationWindow {
			continue
		}
		writes = append(writes, event)
	}
	sort.Slice(writes, func(i, j int) bool { return writes[i].ID < writes[j].ID })
	return writes, ambiguities
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
	return devicespec.NormalizeUUID(value)
}

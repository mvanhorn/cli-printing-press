package ble

import (
	"context"
	"strings"
)

type ReplayAdapter struct {
	input      EvidenceInput
	candidates []DeviceCandidate
}

func NewReplayAdapter(input EvidenceInput) *ReplayAdapter {
	return &ReplayAdapter{
		input:      input,
		candidates: deviceCandidates(input.Events),
	}
}

func (a *ReplayAdapter) Scan(ctx context.Context, opts ScanOptions) ([]DeviceCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	candidates := a.candidates
	if len(opts.ServiceUUIDs) == 0 {
		return candidates, nil
	}

	required := make(map[string]bool, len(opts.ServiceUUIDs))
	for _, uuid := range opts.ServiceUUIDs {
		required[normalizeUUID(uuid)] = true
	}
	filtered := make([]DeviceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		for _, uuid := range candidate.ServiceUUIDs {
			if required[normalizeUUID(uuid)] {
				filtered = append(filtered, candidate)
				break
			}
		}
	}
	return filtered, nil
}

func (a *ReplayAdapter) Inspect(ctx context.Context, req InspectRequest) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !a.hasDevice(req.Address) {
		return nil, adapterError(AdapterErrorDeviceNotFound, "device %q not found in replay evidence", req.Address)
	}
	events := make([]Event, 0, len(a.input.Events))
	for _, event := range a.input.Events {
		if event.Type == EventAdvertisement && strings.TrimSpace(req.Address) != "" && !sameAddress(event.DeviceAddress, req.Address) {
			continue
		}
		events = append(events, normalizeEvent(event))
	}
	return events, nil
}

func (a *ReplayAdapter) Read(ctx context.Context, req CharacteristicRequest) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	if !a.hasDevice(req.Address) {
		return Event{}, adapterError(AdapterErrorDeviceNotFound, "device %q not found in replay evidence", req.Address)
	}
	for _, event := range a.input.Events {
		if event.Type == EventRead && sameCharacteristic(event, req.CharacteristicUUID) && sameService(event, req.ServiceUUID) {
			return normalizeEvent(event), nil
		}
	}
	return Event{}, adapterError(AdapterErrorCharacteristic, "no replayed read for characteristic %q", req.CharacteristicUUID)
}

func (a *ReplayAdapter) Write(ctx context.Context, req WriteRequest) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	if !a.hasDevice(req.Address) {
		return Event{}, adapterError(AdapterErrorDeviceNotFound, "device %q not found in replay evidence", req.Address)
	}
	for _, event := range a.input.Events {
		if event.Type == EventWrite &&
			sameCharacteristic(event, req.CharacteristicUUID) &&
			sameService(event, req.ServiceUUID) &&
			strings.EqualFold(event.ValueHex, req.ValueHex) {
			return normalizeEvent(event), nil
		}
	}
	return Event{}, adapterError(AdapterErrorCharacteristic, "no replayed write for characteristic %q with value %q", req.CharacteristicUUID, req.ValueHex)
}

func (a *ReplayAdapter) Subscribe(ctx context.Context, req CharacteristicRequest) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !a.hasDevice(req.Address) {
		return nil, adapterError(AdapterErrorDeviceNotFound, "device %q not found in replay evidence", req.Address)
	}
	var notifications []Event
	for _, event := range a.input.Events {
		if event.Type == EventNotification && sameCharacteristic(event, req.CharacteristicUUID) && sameService(event, req.ServiceUUID) {
			notifications = append(notifications, normalizeEvent(event))
		}
	}
	if len(notifications) == 0 {
		return nil, adapterError(AdapterErrorCharacteristic, "no replayed notifications for characteristic %q", req.CharacteristicUUID)
	}
	return notifications, nil
}

func (a *ReplayAdapter) hasDevice(address string) bool {
	if strings.TrimSpace(address) == "" {
		return len(a.candidates) == 1
	}
	for _, candidate := range a.candidates {
		if sameAddress(candidate.Address, address) {
			return true
		}
	}
	return false
}

func sameAddress(a string, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func sameCharacteristic(event Event, uuid string) bool {
	return normalizeUUID(event.CharacteristicUUID) == normalizeUUID(uuid)
}

func sameService(event Event, uuid string) bool {
	return strings.TrimSpace(uuid) == "" || normalizeUUID(event.ServiceUUID) == normalizeUUID(uuid)
}

func normalizeEvent(event Event) Event {
	event.ServiceUUID = normalizeUUID(event.ServiceUUID)
	event.CharacteristicUUID = normalizeUUID(event.CharacteristicUUID)
	for i, uuid := range event.ServiceUUIDs {
		event.ServiceUUIDs[i] = normalizeUUID(uuid)
	}
	event.ValueHex = strings.ToLower(strings.TrimSpace(event.ValueHex))
	return event
}

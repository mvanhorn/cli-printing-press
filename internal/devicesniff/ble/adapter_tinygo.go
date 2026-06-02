//go:build ble_live && (darwin || linux || windows)

package ble

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tinyble "tinygo.org/x/bluetooth"
)

const defaultLiveDuration = 10 * time.Second

type LiveSupportInfo struct {
	Compiled bool   `json:"compiled"`
	Backend  string `json:"backend"`
	Platform string `json:"platform"`
	Message  string `json:"message,omitempty"`
}

type TinyGoAdapter struct {
	adapter *tinyble.Adapter
}

func LiveSupport() LiveSupportInfo {
	return LiveSupportInfo{
		Compiled: true,
		Backend:  "tinygo.org/x/bluetooth",
		Platform: "darwin/linux/windows",
	}
}

func NewLiveAdapter() (Adapter, error) {
	return NewTinyGoAdapter(tinyble.DefaultAdapter), nil
}

func NewTinyGoAdapter(adapter *tinyble.Adapter) *TinyGoAdapter {
	return &TinyGoAdapter{adapter: adapter}
}

func (a *TinyGoAdapter) Scan(ctx context.Context, opts ScanOptions) ([]DeviceCandidate, error) {
	if err := a.enable(); err != nil {
		return nil, err
	}

	duration := durationFromMillis(opts.DurationMillis)
	requiredUUIDs, err := parseUUIDs(opts.ServiceUUIDs)
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	candidates := map[string]DeviceCandidate{}
	done := make(chan error, 1)
	go func() {
		done <- a.adapter.Scan(func(adapter *tinyble.Adapter, result tinyble.ScanResult) {
			if !matchesServiceUUIDs(result, requiredUUIDs) {
				return
			}
			address := result.Address.String()
			mu.Lock()
			candidates[address] = DeviceCandidate{
				Address:      address,
				Name:         result.LocalName(),
				RSSI:         int(result.RSSI),
				ServiceUUIDs: uuidStrings(result.ServiceUUIDs()),
			}
			mu.Unlock()
		})
	}()

	select {
	case <-ctx.Done():
		_ = a.adapter.StopScan()
		<-done
		return nil, ctx.Err()
	case err := <-done:
		return nil, mapLiveError(err)
	case <-time.After(duration):
		_ = a.adapter.StopScan()
	}

	select {
	case err := <-done:
		if err != nil {
			return nil, mapLiveError(err)
		}
	case <-time.After(500 * time.Millisecond):
	}

	mu.Lock()
	out := make([]DeviceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate)
	}
	mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].RSSI == out[j].RSSI {
			return out[i].Address < out[j].Address
		}
		return out[i].RSSI > out[j].RSSI
	})
	return out, nil
}

func (a *TinyGoAdapter) Inspect(ctx context.Context, req InspectRequest) ([]Event, error) {
	device, err := a.connect(ctx, req.Address)
	if err != nil {
		return nil, err
	}
	defer func() { _ = device.Disconnect() }()

	services, err := device.DiscoverServices(nil)
	if err != nil {
		return nil, mapLiveError(err)
	}
	events := make([]Event, 0)
	for serviceIndex, service := range services {
		serviceUUID := service.UUID().String()
		chars, err := service.DiscoverCharacteristics(nil)
		if err != nil {
			return nil, mapLiveError(err)
		}
		for charIndex, char := range chars {
			events = append(events, Event{
				ID:                 fmt.Sprintf("svc-%02d-char-%02d", serviceIndex+1, charIndex+1),
				Type:               EventServiceDiscovery,
				ServiceUUID:        normalizeUUID(serviceUUID),
				CharacteristicUUID: normalizeUUID(char.UUID().String()),
			})
		}
	}
	return events, nil
}

func (a *TinyGoAdapter) Read(ctx context.Context, req CharacteristicRequest) (Event, error) {
	char, disconnect, err := a.characteristic(ctx, req.Address, req.ServiceUUID, req.CharacteristicUUID)
	if err != nil {
		return Event{}, err
	}
	defer disconnect()

	buf := make([]byte, 512)
	n, err := char.Read(buf)
	if err != nil {
		return Event{}, mapLiveError(err)
	}
	return Event{
		ID:                 "live-read",
		Type:               EventRead,
		ServiceUUID:        normalizeUUID(req.ServiceUUID),
		CharacteristicUUID: normalizeUUID(char.UUID().String()),
		ValueHex:           hex.EncodeToString(buf[:n]),
	}, nil
}

func (a *TinyGoAdapter) Write(ctx context.Context, req WriteRequest) (Event, error) {
	char, disconnect, err := a.characteristic(ctx, req.Address, req.ServiceUUID, req.CharacteristicUUID)
	if err != nil {
		return Event{}, err
	}
	defer disconnect()

	payload, err := hex.DecodeString(strings.TrimSpace(req.ValueHex))
	if err != nil {
		return Event{}, fmt.Errorf("value_hex: %w", err)
	}
	if _, err := char.Write(payload); err != nil {
		return Event{}, mapLiveError(err)
	}
	return Event{
		ID:                 "live-write",
		Type:               EventWrite,
		ServiceUUID:        normalizeUUID(req.ServiceUUID),
		CharacteristicUUID: normalizeUUID(char.UUID().String()),
		ValueHex:           strings.ToLower(strings.TrimSpace(req.ValueHex)),
	}, nil
}

func (a *TinyGoAdapter) Subscribe(ctx context.Context, req CharacteristicRequest) ([]Event, error) {
	char, disconnect, err := a.characteristic(ctx, req.Address, req.ServiceUUID, req.CharacteristicUUID)
	if err != nil {
		return nil, err
	}
	defer disconnect()

	duration := durationFromMillis(req.DurationMillis)
	var mu sync.Mutex
	var events []Event
	if err := char.EnableNotifications(func(buf []byte) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, Event{
			ID:                 fmt.Sprintf("live-notify-%d", len(events)+1),
			Type:               EventNotification,
			ServiceUUID:        normalizeUUID(req.ServiceUUID),
			CharacteristicUUID: normalizeUUID(char.UUID().String()),
			ValueHex:           hex.EncodeToString(append([]byte(nil), buf...)),
		})
	}); err != nil {
		return nil, mapLiveError(err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(duration):
	}

	mu.Lock()
	out := append([]Event(nil), events...)
	mu.Unlock()
	return out, nil
}

func (a *TinyGoAdapter) connect(ctx context.Context, address string) (tinyble.Device, error) {
	if strings.TrimSpace(address) == "" {
		return tinyble.Device{}, adapterError(AdapterErrorDeviceNotFound, "live BLE requires --address")
	}
	if err := a.enable(); err != nil {
		return tinyble.Device{}, err
	}
	var addr tinyble.Address
	addr.Set(address)

	done := make(chan struct {
		device tinyble.Device
		err    error
	}, 1)
	go func() {
		device, err := a.adapter.Connect(addr, tinyble.ConnectionParams{})
		done <- struct {
			device tinyble.Device
			err    error
		}{device: device, err: err}
	}()

	select {
	case <-ctx.Done():
		return tinyble.Device{}, ctx.Err()
	case result := <-done:
		if result.err != nil {
			return tinyble.Device{}, mapLiveError(result.err)
		}
		return result.device, nil
	}
}

func (a *TinyGoAdapter) characteristic(ctx context.Context, address string, serviceUUID string, characteristicUUID string) (tinyble.DeviceCharacteristic, func(), error) {
	device, err := a.connect(ctx, address)
	if err != nil {
		return tinyble.DeviceCharacteristic{}, func() {}, err
	}
	disconnect := func() { _ = device.Disconnect() }

	serviceFilter, err := parseOptionalUUID(serviceUUID)
	if err != nil {
		disconnect()
		return tinyble.DeviceCharacteristic{}, func() {}, err
	}
	charFilter, err := parseOptionalUUID(characteristicUUID)
	if err != nil {
		disconnect()
		return tinyble.DeviceCharacteristic{}, func() {}, err
	}
	if charFilter == nil {
		disconnect()
		return tinyble.DeviceCharacteristic{}, func() {}, adapterError(AdapterErrorCharacteristic, "characteristic UUID is required")
	}

	var serviceUUIDs []tinyble.UUID
	if serviceFilter != nil {
		serviceUUIDs = []tinyble.UUID{*serviceFilter}
	}
	services, err := device.DiscoverServices(serviceUUIDs)
	if err != nil {
		disconnect()
		return tinyble.DeviceCharacteristic{}, func() {}, mapLiveError(err)
	}
	for _, service := range services {
		chars, err := service.DiscoverCharacteristics([]tinyble.UUID{*charFilter})
		if err != nil {
			disconnect()
			return tinyble.DeviceCharacteristic{}, func() {}, mapLiveError(err)
		}
		if len(chars) > 0 {
			return chars[0], disconnect, nil
		}
	}
	disconnect()
	return tinyble.DeviceCharacteristic{}, func() {}, adapterError(AdapterErrorCharacteristic, "characteristic %q not found", characteristicUUID)
}

func (a *TinyGoAdapter) enable() error {
	if a == nil || a.adapter == nil {
		return adapterError(AdapterErrorUnsupported, "live BLE adapter is not configured")
	}
	if err := a.adapter.Enable(); err != nil {
		return mapLiveError(err)
	}
	return nil
}

func durationFromMillis(ms int) time.Duration {
	if ms <= 0 {
		return defaultLiveDuration
	}
	return time.Duration(ms) * time.Millisecond
}

func parseUUIDs(values []string) ([]tinyble.UUID, error) {
	out := make([]tinyble.UUID, 0, len(values))
	for _, value := range values {
		uuid, err := tinyble.ParseUUID(value)
		if err != nil {
			return nil, fmt.Errorf("service UUID %q: %w", value, err)
		}
		out = append(out, uuid)
	}
	return out, nil
}

func parseOptionalUUID(value string) (*tinyble.UUID, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	uuid, err := tinyble.ParseUUID(value)
	if err != nil {
		return nil, err
	}
	return &uuid, nil
}

func matchesServiceUUIDs(result tinyble.ScanResult, required []tinyble.UUID) bool {
	if len(required) == 0 {
		return true
	}
	for _, uuid := range required {
		if result.HasServiceUUID(uuid) {
			return true
		}
	}
	return false
}

func uuidStrings(uuids []tinyble.UUID) []string {
	out := make([]string, 0, len(uuids))
	for _, uuid := range uuids {
		out = append(out, normalizeUUID(uuid.String()))
	}
	sort.Strings(out)
	return out
}

func mapLiveError(err error) error {
	if err == nil {
		return nil
	}
	return adapterError(AdapterErrorUnsupported, "%v", err)
}

//go:build !ble_replay_only && (darwin || linux || windows)

package ble

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	tinyble "tinygo.org/x/bluetooth"
)

const defaultLiveDuration = 10 * time.Second

// maxSafeDurationMillis is the largest ms value that can be multiplied by
// time.Millisecond without overflowing int64.
var maxSafeDurationMillis = int(math.MaxInt64 / int64(time.Millisecond))

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

	services, err := runWithContext(ctx, func() ([]tinyble.DeviceService, error) {
		return device.DiscoverServices(nil)
	})
	if err != nil {
		return nil, mapLiveError(err)
	}
	// EventServiceDiscovery events carry no Properties: tinygo.org/x/bluetooth
	// exposes no characteristic property bitmask after discovery, so live
	// captures cannot flag writable characteristics the way replay evidence
	// does. Writability is inferred from action markers and community references.
	events := make([]Event, 0)
	for serviceIndex, service := range services {
		serviceUUID := service.UUID().String()
		chars, err := runWithContext(ctx, func() ([]tinyble.DeviceCharacteristic, error) {
			return service.DiscoverCharacteristics(nil)
		})
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
	serviceUUID := normalizeUUID(req.ServiceUUID)
	characteristicUUID := normalizeUUID(char.UUID().String())
	var mu sync.Mutex
	var events []Event
	if err := char.EnableNotifications(func(buf []byte) {
		valueHex := hex.EncodeToString(buf)
		mu.Lock()
		events = append(events, Event{
			ID:                 fmt.Sprintf("live-notify-%d", len(events)+1),
			Type:               EventNotification,
			ServiceUUID:        serviceUUID,
			CharacteristicUUID: characteristicUUID,
			ValueHex:           valueHex,
		})
		mu.Unlock()
	}); err != nil {
		return nil, mapLiveError(err)
	}
	defer func() { _ = char.EnableNotifications(nil) }()

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
	if runtime.GOOS == "linux" {
		if err := a.ensureLinuxDeviceSeen(ctx, address); err != nil {
			return tinyble.Device{}, err
		}
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
		go disconnectLateConnect(done)
		return tinyble.Device{}, ctx.Err()
	case result := <-done:
		if result.err != nil {
			return tinyble.Device{}, mapLiveError(result.err)
		}
		return result.device, nil
	}
}

func (a *TinyGoAdapter) ensureLinuxDeviceSeen(ctx context.Context, address string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	target := strings.ToUpper(strings.TrimSpace(address))
	done := make(chan error, 1)
	go func() {
		done <- a.adapter.Scan(func(adapter *tinyble.Adapter, result tinyble.ScanResult) {
			if strings.EqualFold(result.Address.String(), target) {
				_ = adapter.StopScan()
			}
		})
	}()

	select {
	case <-ctx.Done():
		_ = a.adapter.StopScan()
		<-done
		return ctx.Err()
	case err := <-done:
		return mapLiveError(err)
	}
}

func disconnectLateConnect(done <-chan struct {
	device tinyble.Device
	err    error
}) {
	result := <-done
	if result.err == nil {
		_ = result.device.Disconnect()
	}
}

// runWithContext runs fn on a goroutine and returns its result, or ctx.Err() if
// the context is cancelled first. A cancelled call's goroutine unblocks once the
// caller disconnects the device; the buffered channel keeps it from leaking.
func runWithContext[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	type outcome struct {
		value T
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		value, err := fn()
		done <- outcome{value: value, err: err}
	}()
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case res := <-done:
		return res.value, res.err
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
	services, err := runWithContext(ctx, func() ([]tinyble.DeviceService, error) {
		return device.DiscoverServices(serviceUUIDs)
	})
	if err != nil {
		disconnect()
		return tinyble.DeviceCharacteristic{}, func() {}, mapLiveError(err)
	}
	for _, service := range services {
		chars, err := runWithContext(ctx, func() ([]tinyble.DeviceCharacteristic, error) {
			return service.DiscoverCharacteristics([]tinyble.UUID{*charFilter})
		})
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

// durationFromMillis converts a millisecond count to a time.Duration.
// Non-positive values return the default scan duration.
// Values large enough to overflow int64 are clamped to 24 hours.
func durationFromMillis(ms int) time.Duration {
	if ms <= 0 {
		return defaultLiveDuration
	}
	if ms > maxSafeDurationMillis {
		return 24 * time.Hour
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
	return slices.ContainsFunc(required, result.HasServiceUUID)
}

func uuidStrings(uuids []tinyble.UUID) []string {
	out := make([]string, 0, len(uuids))
	for _, uuid := range uuids {
		out = append(out, normalizeUUID(uuid.String()))
	}
	sort.Strings(out)
	return out
}

// mapLiveError classifies a raw BLE adapter error into a typed AdapterError.
// String heuristics are applied before falling back to AdapterErrorUnsupported.
func mapLiveError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "operation not permitted") ||
		strings.Contains(msg, "not authorized"):
		return adapterError(AdapterErrorPermissionDenied, "%v", err)
	case strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such device") ||
		strings.Contains(msg, "unknown device"):
		return adapterError(AdapterErrorDeviceNotFound, "%v", err)
	case strings.Contains(msg, "disconnected") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused"):
		return adapterError(AdapterErrorDisconnected, "%v", err)
	default:
		return adapterError(AdapterErrorUnsupported, "%v", err)
	}
}

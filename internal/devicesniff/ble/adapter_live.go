package ble

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultLiveDuration = 10 * time.Second

// maxSafeDurationMillis is the largest ms value that can be multiplied by
// time.Millisecond without overflowing int64.
var maxSafeDurationMillis = int(math.MaxInt64 / int64(time.Millisecond))

// liveAdapter drives a bleDriver through the BLE evidence workflow. It holds no
// backend-specific types, so its lifecycle and concurrency logic is testable
// against a stub driver on any platform.
type liveAdapter struct {
	driver bleDriver
}

func newLiveAdapter(driver bleDriver) *liveAdapter {
	return &liveAdapter{driver: driver}
}

func (a *liveAdapter) Scan(ctx context.Context, opts ScanOptions) ([]DeviceCandidate, error) {
	if err := a.enable(); err != nil {
		return nil, err
	}

	duration := durationFromMillis(opts.DurationMillis)

	var mu sync.Mutex
	candidates := map[string]DeviceCandidate{}
	done := make(chan error, 1)
	go func() {
		done <- a.driver.Scan(opts.ServiceUUIDs, func(adv bleAdvertisement) {
			address := adv.Address()
			mu.Lock()
			candidates[address] = DeviceCandidate{
				Address:      address,
				Name:         adv.LocalName(),
				RSSI:         adv.RSSI(),
				ServiceUUIDs: adv.ServiceUUIDs(),
			}
			mu.Unlock()
		})
	}()

	select {
	case <-ctx.Done():
		_ = a.driver.StopScan()
		<-done
		return nil, ctx.Err()
	case err := <-done:
		return nil, mapLiveError(err)
	case <-time.After(duration):
		_ = a.driver.StopScan()
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

func (a *liveAdapter) Inspect(ctx context.Context, req InspectRequest) ([]Event, error) {
	device, err := a.connect(ctx, req.Address)
	if err != nil {
		return nil, err
	}
	defer func() { _ = device.Disconnect() }()

	services, err := runWithContext(ctx, func() ([]bleService, error) {
		return device.DiscoverServices(nil)
	})
	if err != nil {
		return nil, mapLiveError(err)
	}
	// EventServiceDiscovery events carry no Properties: the BLE backend exposes
	// no characteristic property bitmask after discovery, so live captures cannot
	// flag writable characteristics the way replay evidence does. Writability is
	// inferred from action markers and community references.
	events := make([]Event, 0)
	for serviceIndex, service := range services {
		serviceUUID := service.UUID()
		chars, err := runWithContext(ctx, func() ([]bleCharacteristic, error) {
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
				CharacteristicUUID: normalizeUUID(char.UUID()),
			})
		}
	}
	return events, nil
}

func (a *liveAdapter) Read(ctx context.Context, req CharacteristicRequest) (Event, error) {
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
		CharacteristicUUID: normalizeUUID(char.UUID()),
		ValueHex:           hex.EncodeToString(buf[:n]),
	}, nil
}

func (a *liveAdapter) Write(ctx context.Context, req WriteRequest) (Event, error) {
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
		CharacteristicUUID: normalizeUUID(char.UUID()),
		ValueHex:           strings.ToLower(strings.TrimSpace(req.ValueHex)),
	}, nil
}

func (a *liveAdapter) Subscribe(ctx context.Context, req CharacteristicRequest) ([]Event, error) {
	char, disconnect, err := a.characteristic(ctx, req.Address, req.ServiceUUID, req.CharacteristicUUID)
	if err != nil {
		return nil, err
	}
	defer disconnect()

	duration := durationFromMillis(req.DurationMillis)
	serviceUUID := normalizeUUID(req.ServiceUUID)
	characteristicUUID := normalizeUUID(char.UUID())
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

type connectResult struct {
	device bleDevice
	err    error
}

func (a *liveAdapter) connect(ctx context.Context, address string) (bleDevice, error) {
	if strings.TrimSpace(address) == "" {
		return nil, adapterError(AdapterErrorDeviceNotFound, "live BLE requires --address")
	}
	if err := a.enable(); err != nil {
		return nil, err
	}
	if runtime.GOOS == "linux" {
		if err := a.ensureLinuxDeviceSeen(ctx, address); err != nil {
			return nil, err
		}
	}

	done := make(chan connectResult, 1)
	go func() {
		device, err := a.driver.Connect(address)
		done <- connectResult{device: device, err: err}
	}()

	select {
	case <-ctx.Done():
		go disconnectLateConnect(done)
		return nil, ctx.Err()
	case result := <-done:
		if result.err != nil {
			return nil, mapLiveError(result.err)
		}
		return result.device, nil
	}
}

// ensureLinuxDeviceSeen works around BlueZ requiring a device to be observed in
// a scan before Connect succeeds. It scans until the target address appears or
// the bounded context expires.
func (a *liveAdapter) ensureLinuxDeviceSeen(ctx context.Context, address string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	target := strings.ToUpper(strings.TrimSpace(address))
	done := make(chan error, 1)
	go func() {
		done <- a.driver.Scan(nil, func(adv bleAdvertisement) {
			if strings.EqualFold(adv.Address(), target) {
				_ = a.driver.StopScan()
			}
		})
	}()

	select {
	case <-ctx.Done():
		_ = a.driver.StopScan()
		<-done
		return ctx.Err()
	case err := <-done:
		return mapLiveError(err)
	}
}

// disconnectLateConnect drains a connect goroutine that finished after its
// context was cancelled, disconnecting the now-unwanted device.
func disconnectLateConnect(done <-chan connectResult) {
	result := <-done
	if result.err == nil && result.device != nil {
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

func (a *liveAdapter) characteristic(ctx context.Context, address string, serviceUUID string, characteristicUUID string) (bleCharacteristic, func(), error) {
	device, err := a.connect(ctx, address)
	if err != nil {
		return nil, func() {}, err
	}
	disconnect := func() { _ = device.Disconnect() }

	if strings.TrimSpace(characteristicUUID) == "" {
		disconnect()
		return nil, func() {}, adapterError(AdapterErrorCharacteristic, "characteristic UUID is required")
	}

	var serviceFilter []string
	if trimmed := strings.TrimSpace(serviceUUID); trimmed != "" {
		serviceFilter = []string{trimmed}
	}
	services, err := runWithContext(ctx, func() ([]bleService, error) {
		return device.DiscoverServices(serviceFilter)
	})
	if err != nil {
		disconnect()
		return nil, func() {}, mapLiveError(err)
	}
	for _, service := range services {
		chars, err := runWithContext(ctx, func() ([]bleCharacteristic, error) {
			return service.DiscoverCharacteristics([]string{characteristicUUID})
		})
		if err != nil {
			disconnect()
			return nil, func() {}, mapLiveError(err)
		}
		if len(chars) > 0 {
			return chars[0], disconnect, nil
		}
	}
	disconnect()
	return nil, func() {}, adapterError(AdapterErrorCharacteristic, "characteristic %q not found", characteristicUUID)
}

func (a *liveAdapter) enable() error {
	if a == nil || a.driver == nil {
		return adapterError(AdapterErrorUnsupported, "live BLE adapter is not configured")
	}
	if err := a.driver.Enable(); err != nil {
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

// mapLiveError classifies a raw BLE backend error into a typed AdapterError.
// Context errors pass through verbatim; string heuristics are applied before
// falling back to AdapterErrorUnsupported.
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

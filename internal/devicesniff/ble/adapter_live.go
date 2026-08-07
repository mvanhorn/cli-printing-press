package ble

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultLiveDuration = 10 * time.Second
	// scanStopDrainTimeout bounds how long Scan waits for the backend scan
	// goroutine to unwind after StopScan before returning the collected results.
	scanStopDrainTimeout = 500 * time.Millisecond
	// scanSeenTimeout bounds the pre-connect scan that proves a device is in
	// range before Connect on backends that require it.
	scanSeenTimeout = 5 * time.Second
	// bleCharacteristicMaxValueBytes is the ATT protocol maximum characteristic
	// value length, so a single Read never needs a larger buffer.
	bleCharacteristicMaxValueBytes = 512
	// maxSafeDurationMillis caps the millisecond count durationFromMillis scales
	// to a time.Duration; beyond it the nanosecond product overflows int64, so
	// larger requests clamp to 24 hours. Typed int64 so the bound holds on
	// 32-bit builds where int would truncate.
	maxSafeDurationMillis = math.MaxInt64 / int64(time.Millisecond)

	liveReadOp   = "read"
	liveWriteOp  = "write"
	liveNotifyOp = "notify"
)

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

	collect := func() []DeviceCandidate {
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
		return out
	}

	select {
	case <-ctx.Done():
		_ = a.driver.StopScan()
		// Bound the unwind wait: a backend whose scan callback hangs after
		// StopScan must not pin the cancelled command open forever. Mirrors the
		// window-elapsed drain below.
		select {
		case <-done:
		case <-time.After(scanStopDrainTimeout):
		}
		return nil, ctx.Err()
	case err := <-done:
		// Backend finished before the window elapsed: surface a real failure,
		// but keep the advertisements collected when it stopped cleanly.
		if err != nil {
			return nil, mapLiveError(err)
		}
		return collect(), nil
	case <-time.After(duration):
		_ = a.driver.StopScan()
	}

	// The window elapsed and we asked the backend to stop; wait briefly for the
	// scan goroutine to unwind. A stop-time error is expected noise, not a data
	// failure, so the collected candidates stand regardless.
	select {
	case <-done:
	case <-time.After(scanStopDrainTimeout):
	}
	return collect(), nil
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

func liveEventID(op, serviceUUID, characteristicUUID string, capturedAt time.Time, seq int) string {
	parts := []string{"live", op, liveIDPart(serviceUUID), liveIDPart(characteristicUUID), fmt.Sprintf("%d", capturedAt.UnixNano())}
	if seq > 0 {
		parts = append(parts, fmt.Sprintf("%d", seq))
	}
	return strings.Join(parts, "-")
}

func liveIDPart(value string) string {
	normalized := normalizeUUID(value)
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

func (a *liveAdapter) Read(ctx context.Context, req CharacteristicRequest) (Event, error) {
	found, err := a.characteristic(ctx, req.Address, req.ServiceUUID, req.CharacteristicUUID)
	if err != nil {
		return Event{}, err
	}
	defer found.disconnect()

	buf := make([]byte, bleCharacteristicMaxValueBytes)
	n, err := found.char.Read(buf)
	if err != nil {
		return Event{}, mapLiveError(err)
	}
	if n < 0 {
		// A misbehaving backend may report a negative count; treat it as no data
		// rather than slicing buf[:n] out of range.
		n = 0
	} else if n > len(buf) {
		// Some backends report the full attribute length rather than the bytes
		// copied; clamp so a longer-than-buffer value cannot slice out of range.
		n = len(buf)
	}
	capturedAt := time.Now().UTC()
	characteristicUUID := normalizeUUID(found.char.UUID())
	return Event{
		ID:                 liveEventID(liveReadOp, found.serviceUUID, characteristicUUID, capturedAt, 0),
		Type:               EventRead,
		At:                 capturedAt.Format(time.RFC3339Nano),
		ServiceUUID:        found.serviceUUID,
		CharacteristicUUID: characteristicUUID,
		ValueHex:           hex.EncodeToString(buf[:n]),
	}, nil
}

func (a *liveAdapter) Write(ctx context.Context, req WriteRequest) (Event, error) {
	found, err := a.characteristic(ctx, req.Address, req.ServiceUUID, req.CharacteristicUUID)
	if err != nil {
		return Event{}, err
	}
	defer found.disconnect()

	payload, err := hex.DecodeString(strings.TrimSpace(req.ValueHex))
	if err != nil {
		return Event{}, fmt.Errorf("value_hex: %w", err)
	}
	if _, err := found.char.Write(payload); err != nil {
		return Event{}, mapLiveError(err)
	}
	capturedAt := time.Now().UTC()
	characteristicUUID := normalizeUUID(found.char.UUID())
	return Event{
		ID:                 liveEventID(liveWriteOp, found.serviceUUID, characteristicUUID, capturedAt, 0),
		Type:               EventWrite,
		At:                 capturedAt.Format(time.RFC3339Nano),
		ServiceUUID:        found.serviceUUID,
		CharacteristicUUID: characteristicUUID,
		ValueHex:           strings.ToLower(strings.TrimSpace(req.ValueHex)),
	}, nil
}

func (a *liveAdapter) Subscribe(ctx context.Context, req CharacteristicRequest) ([]Event, error) {
	found, err := a.characteristic(ctx, req.Address, req.ServiceUUID, req.CharacteristicUUID)
	if err != nil {
		return nil, err
	}
	defer found.disconnect()

	duration := durationFromMillis(req.DurationMillis)
	serviceUUID := found.serviceUUID
	characteristicUUID := normalizeUUID(found.char.UUID())
	var mu sync.Mutex
	var events []Event
	if err := found.char.EnableNotifications(func(buf []byte) {
		valueHex := hex.EncodeToString(buf)
		capturedAt := time.Now().UTC()
		mu.Lock()
		seq := len(events) + 1
		events = append(events, Event{
			ID:                 liveEventID(liveNotifyOp, serviceUUID, characteristicUUID, capturedAt, seq),
			Type:               EventNotification,
			At:                 capturedAt.Format(time.RFC3339Nano),
			ServiceUUID:        serviceUUID,
			CharacteristicUUID: characteristicUUID,
			ValueHex:           valueHex,
		})
		mu.Unlock()
	}); err != nil {
		return nil, mapLiveError(err)
	}
	defer func() { _ = found.char.EnableNotifications(nil) }()

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
	if a.driver.NeedsPreScan() {
		if err := a.ensureDeviceSeen(ctx, address); err != nil {
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

// ensureDeviceSeen works around backends (BlueZ) that require a device to be
// observed in a scan before Connect succeeds. It scans until the target address
// appears or the bounded window expires, reporting device-not-found when the
// scan ends without the target rather than leaking a raw context deadline or
// letting connect proceed against a device that was never in range.
func (a *liveAdapter) ensureDeviceSeen(parent context.Context, address string) error {
	ctx, cancel := context.WithTimeout(parent, scanSeenTimeout)
	defer cancel()

	notSeen := func() error {
		return adapterError(AdapterErrorDeviceNotFound, "device %q not seen during scan", address)
	}
	target := strings.ToUpper(strings.TrimSpace(address))
	var seen atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- a.driver.Scan(nil, func(adv bleAdvertisement) {
			if strings.EqualFold(adv.Address(), target) {
				seen.Store(true)
				_ = a.driver.StopScan()
			}
		})
	}()

	select {
	case <-ctx.Done():
		_ = a.driver.StopScan()
		// Bound the wait for the scan goroutine to unwind; the buffered channel
		// keeps it from leaking if the backend is slow to honor StopScan.
		select {
		case <-done:
		case <-time.After(scanStopDrainTimeout):
		}
		if parent.Err() != nil {
			return parent.Err()
		}
		return notSeen()
	case err := <-done:
		if err != nil {
			return mapLiveError(err)
		}
		if !seen.Load() {
			return notSeen()
		}
		return nil
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

// discoveredCharacteristic carries a resolved characteristic, the normalized
// UUID of the service it was found under, and the device disconnect handle.
type discoveredCharacteristic struct {
	char        bleCharacteristic
	serviceUUID string
	disconnect  func()
}

// characteristic connects and resolves the requested characteristic, recording
// the owning service so callers can report it even when the request omitted
// --service. The disconnect handle is valid only on the nil-error return.
func (a *liveAdapter) characteristic(ctx context.Context, address, serviceUUID, characteristicUUID string) (discoveredCharacteristic, error) {
	device, err := a.connect(ctx, address)
	if err != nil {
		return discoveredCharacteristic{}, err
	}
	disconnect := func() { _ = device.Disconnect() }

	if strings.TrimSpace(characteristicUUID) == "" {
		disconnect()
		return discoveredCharacteristic{}, adapterError(AdapterErrorCharacteristic, "characteristic UUID is required")
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
		return discoveredCharacteristic{}, mapLiveError(err)
	}
	for _, service := range services {
		chars, err := runWithContext(ctx, func() ([]bleCharacteristic, error) {
			return service.DiscoverCharacteristics([]string{characteristicUUID})
		})
		if err != nil {
			disconnect()
			return discoveredCharacteristic{}, mapLiveError(err)
		}
		if len(chars) > 0 {
			return discoveredCharacteristic{char: chars[0], serviceUUID: normalizeUUID(service.UUID()), disconnect: disconnect}, nil
		}
	}
	disconnect()
	return discoveredCharacteristic{}, adapterError(AdapterErrorCharacteristic, "characteristic %q not found", characteristicUUID)
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

// durationFromMillis maps a request's millisecond count onto a scan/subscribe
// window. Non-positive values fall back to the default; values that would
// overflow int64 nanoseconds clamp to 24 hours.
func durationFromMillis(ms int) time.Duration {
	if ms <= 0 {
		return defaultLiveDuration
	}
	if int64(ms) > maxSafeDurationMillis {
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
	case strings.Contains(msg, "invalid uuid"):
		return adapterError(AdapterErrorCharacteristic, "%v", err)
	default:
		return adapterError(AdapterErrorUnsupported, "%v", err)
	}
}

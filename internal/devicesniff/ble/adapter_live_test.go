package ble

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- in-memory stub driver ------------------------------------------------

type stubDriver struct {
	enableErr    error
	scanResults  []bleAdvertisement
	scanErr      error
	scanBlocks   bool
	stopped      chan struct{}
	scanFilter   []string
	device       bleDevice
	connectFunc  func(address string) (bleDevice, error)
	needsPreScan bool
}

func newStubDriver() *stubDriver {
	return &stubDriver{stopped: make(chan struct{})}
}

func (d *stubDriver) Enable() error { return d.enableErr }

func (d *stubDriver) Scan(filterServiceUUIDs []string, onResult func(bleAdvertisement)) error {
	d.scanFilter = filterServiceUUIDs
	if d.scanErr != nil {
		return d.scanErr
	}
	for _, adv := range d.scanResults {
		onResult(adv)
	}
	if d.scanBlocks {
		<-d.stopped
	}
	return nil
}

func (d *stubDriver) StopScan() error {
	select {
	case <-d.stopped:
	default:
		close(d.stopped)
	}
	return nil
}

func (d *stubDriver) Connect(address string) (bleDevice, error) {
	if d.connectFunc != nil {
		return d.connectFunc(address)
	}
	return d.device, nil
}

func (d *stubDriver) NeedsPreScan() bool { return d.needsPreScan }

type stubAdv struct {
	address      string
	name         string
	rssi         int
	serviceUUIDs []string
}

func (a stubAdv) Address() string        { return a.address }
func (a stubAdv) LocalName() string      { return a.name }
func (a stubAdv) RSSI() int              { return a.rssi }
func (a stubAdv) ServiceUUIDs() []string { return a.serviceUUIDs }

type stubDevice struct {
	services        []bleService
	discoverErr     error
	disconnectCount int
}

func (d *stubDevice) DiscoverServices(serviceUUIDs []string) ([]bleService, error) {
	if d.discoverErr != nil {
		return nil, d.discoverErr
	}
	return d.services, nil
}

func (d *stubDevice) Disconnect() error {
	d.disconnectCount++
	return nil
}

type stubService struct {
	uuid  string
	chars []bleCharacteristic
}

func (s stubService) UUID() string { return s.uuid }

func (s stubService) DiscoverCharacteristics(characteristicUUIDs []string) ([]bleCharacteristic, error) {
	if len(characteristicUUIDs) == 0 {
		return s.chars, nil
	}
	var out []bleCharacteristic
	for _, char := range s.chars {
		for _, want := range characteristicUUIDs {
			if char.UUID() == want {
				out = append(out, char)
			}
		}
	}
	return out, nil
}

type stubCharacteristic struct {
	uuid         string
	readData     []byte
	readErr      error
	readN        *int
	written      [][]byte
	notifyValues [][]byte
}

func (c *stubCharacteristic) UUID() string { return c.uuid }

func (c *stubCharacteristic) Read(buf []byte) (int, error) {
	if c.readErr != nil {
		return 0, c.readErr
	}
	// Backends may report the attribute length rather than the bytes copied into
	// the caller's buffer; honoring that contract exercises the adapter's clamp.
	copy(buf, c.readData)
	if c.readN != nil {
		return *c.readN, nil
	}
	return len(c.readData), nil
}

func (c *stubCharacteristic) Write(payload []byte) (int, error) {
	c.written = append(c.written, append([]byte(nil), payload...))
	return len(payload), nil
}

func (c *stubCharacteristic) EnableNotifications(handler func([]byte)) error {
	if handler == nil {
		return nil
	}
	for _, value := range c.notifyValues {
		handler(value)
	}
	return nil
}

// --- pure-helper tests ----------------------------------------------------

func TestRunWithContextReturnsResult(t *testing.T) {
	t.Parallel()
	got, err := runWithContext(context.Background(), func() (int, error) { return 42, nil })
	assert.NoError(t, err)
	assert.Equal(t, 42, got)
}

func TestRunWithContextPropagatesError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	got, err := runWithContext(context.Background(), func() (int, error) { return 0, sentinel })
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 0, got)
}

func TestRunWithContextCancelledBeforeCompletion(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := runWithContext(ctx, func() (int, error) {
		time.Sleep(20 * time.Millisecond)
		return 7, nil
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, got)
}

func TestMapLiveError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		wantNil  bool
		wantKind AdapterErrorKind
	}{
		{name: "nil passes through", err: nil, wantNil: true},
		{name: "context cancelled passes through verbatim", err: context.Canceled},
		{name: "deadline exceeded passes through verbatim", err: context.DeadlineExceeded},
		{name: "permission denied", err: errors.New("Permission denied by host"), wantKind: AdapterErrorPermissionDenied},
		{name: "device not found", err: errors.New("no such device"), wantKind: AdapterErrorDeviceNotFound},
		{name: "disconnected", err: errors.New("peer disconnected"), wantKind: AdapterErrorDisconnected},
		{name: "unrecognized falls back to unsupported", err: errors.New("gatt error 0x0e"), wantKind: AdapterErrorUnsupported},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapLiveError(tc.err)
			if tc.wantNil {
				assert.NoError(t, got)
				return
			}
			if errors.Is(tc.err, context.Canceled) || errors.Is(tc.err, context.DeadlineExceeded) {
				assert.Equal(t, tc.err, got)
				return
			}
			var ae *AdapterError
			require.True(t, errors.As(got, &ae), "expected *AdapterError, got %T", got)
			assert.Equal(t, tc.wantKind, ae.Kind)
		})
	}
}

func TestDurationFromMillis(t *testing.T) {
	t.Parallel()
	assert.Equal(t, defaultLiveDuration, durationFromMillis(0))
	assert.Equal(t, defaultLiveDuration, durationFromMillis(-5))
	assert.Equal(t, 250*time.Millisecond, durationFromMillis(250))
	assert.Equal(t, 24*time.Hour, durationFromMillis(int(maxSafeDurationMillis+1)))
}

// --- lifecycle tests ------------------------------------------------------

func TestLiveAdapterScanDedupSortAndFilter(t *testing.T) {
	t.Parallel()
	driver := newStubDriver()
	driver.scanBlocks = true
	driver.scanResults = []bleAdvertisement{
		stubAdv{address: "AA", rssi: -50, name: "first"},
		stubAdv{address: "BB", rssi: -40},
		stubAdv{address: "AA", rssi: -60, name: "second"}, // same address overwrites
	}
	adapter := newLiveAdapter(driver)

	out, err := adapter.Scan(context.Background(), ScanOptions{DurationMillis: 20, ServiceUUIDs: []string{"180a"}})
	require.NoError(t, err)
	require.Len(t, out, 2, "duplicate address should collapse")
	assert.Equal(t, "BB", out[0].Address, "stronger RSSI sorts first")
	assert.Equal(t, "AA", out[1].Address)
	assert.Equal(t, -60, out[1].RSSI, "last advertisement for an address wins")
	assert.Equal(t, []string{"180a"}, driver.scanFilter, "service-uuid filter is forwarded to the driver")
}

func TestLiveAdapterScanEnableError(t *testing.T) {
	t.Parallel()
	driver := newStubDriver()
	driver.enableErr = errors.New("permission denied")
	adapter := newLiveAdapter(driver)

	_, err := adapter.Scan(context.Background(), ScanOptions{})
	var ae *AdapterError
	require.True(t, errors.As(err, &ae))
	assert.Equal(t, AdapterErrorPermissionDenied, ae.Kind)
}

func TestLiveAdapterInspectEmitsDiscoveryEventsAndDisconnects(t *testing.T) {
	t.Parallel()
	device := &stubDevice{services: []bleService{
		stubService{uuid: "180A", chars: []bleCharacteristic{
			&stubCharacteristic{uuid: "2A29"},
			&stubCharacteristic{uuid: "2A25"},
		}},
	}}
	driver := newStubDriver()
	driver.device = device
	adapter := newLiveAdapter(driver)

	events, err := adapter.Inspect(context.Background(), InspectRequest{Address: "AA"})
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "svc-01-char-01", events[0].ID)
	assert.Equal(t, EventServiceDiscovery, events[0].Type)
	assert.Equal(t, "180a", events[0].ServiceUUID, "UUID is normalized to lower case")
	assert.Equal(t, "2a29", events[0].CharacteristicUUID)
	assert.Equal(t, "svc-01-char-02", events[1].ID)
	assert.Equal(t, 1, device.disconnectCount, "device is disconnected after inspect")
}

func TestLiveAdapterReadAndWrite(t *testing.T) {
	t.Parallel()
	char := &stubCharacteristic{uuid: "2A29", readData: []byte{0xde, 0xad}}
	device := &stubDevice{services: []bleService{stubService{uuid: "180A", chars: []bleCharacteristic{char}}}}
	driver := newStubDriver()
	driver.device = device
	adapter := newLiveAdapter(driver)

	readEvent, err := adapter.Read(context.Background(), CharacteristicRequest{Address: "AA", ServiceUUID: "180A", CharacteristicUUID: "2A29"})
	require.NoError(t, err)
	assert.Regexp(t, `^live-read-180a-2a29-\d+$`, readEvent.ID)
	assert.NotEmpty(t, readEvent.At)
	assert.Equal(t, "dead", readEvent.ValueHex)
	assert.Equal(t, "2a29", readEvent.CharacteristicUUID)
	assert.Equal(t, "180a", readEvent.ServiceUUID, "discovered service UUID is recorded")

	writeEvent, err := adapter.Write(context.Background(), WriteRequest{Address: "AA", ServiceUUID: "180A", CharacteristicUUID: "2A29", ValueHex: "BEEF"})
	require.NoError(t, err)
	assert.Regexp(t, `^live-write-180a-2a29-\d+$`, writeEvent.ID)
	assert.NotEmpty(t, writeEvent.At)
	assert.Equal(t, "beef", writeEvent.ValueHex)
	assert.Equal(t, "180a", writeEvent.ServiceUUID)
	require.Len(t, char.written, 1)
	assert.Equal(t, []byte{0xbe, 0xef}, char.written[0])
	assert.Equal(t, 2, device.disconnectCount, "device is disconnected after each successful op")
}

func TestLiveAdapterReadInfersServiceUUIDWhenOmitted(t *testing.T) {
	t.Parallel()
	char := &stubCharacteristic{uuid: "2A29", readData: []byte{0x01}}
	device := &stubDevice{services: []bleService{stubService{uuid: "180A", chars: []bleCharacteristic{char}}}}
	driver := newStubDriver()
	driver.device = device
	adapter := newLiveAdapter(driver)

	// No ServiceUUID in the request: the event must still carry the service the
	// characteristic was discovered under, not an empty string.
	readEvent, err := adapter.Read(context.Background(), CharacteristicRequest{Address: "AA", CharacteristicUUID: "2A29"})
	require.NoError(t, err)
	assert.Equal(t, "180a", readEvent.ServiceUUID)
}

func TestLiveAdapterReadClampsOversizedValue(t *testing.T) {
	t.Parallel()
	oversized := make([]byte, bleCharacteristicMaxValueBytes+64) // backend over-reports length
	char := &stubCharacteristic{uuid: "2A29", readData: oversized}
	device := &stubDevice{services: []bleService{stubService{uuid: "180A", chars: []bleCharacteristic{char}}}}
	driver := newStubDriver()
	driver.device = device
	adapter := newLiveAdapter(driver)

	readEvent, err := adapter.Read(context.Background(), CharacteristicRequest{Address: "AA", CharacteristicUUID: "2A29"})
	require.NoError(t, err)
	// Value is clamped to the buffer length; no panic on the oversized read.
	assert.Equal(t, bleCharacteristicMaxValueBytes*2, len(readEvent.ValueHex))
}

func TestLiveAdapterReadClampsNegativeCount(t *testing.T) {
	t.Parallel()
	negative := -1
	char := &stubCharacteristic{uuid: "2A29", readData: []byte{0x01}, readN: &negative} // backend reports a bogus negative count
	device := &stubDevice{services: []bleService{stubService{uuid: "180A", chars: []bleCharacteristic{char}}}}
	driver := newStubDriver()
	driver.device = device
	adapter := newLiveAdapter(driver)

	readEvent, err := adapter.Read(context.Background(), CharacteristicRequest{Address: "AA", CharacteristicUUID: "2A29"})
	require.NoError(t, err)
	// Negative count is clamped to zero; no panic slicing buf[:n].
	assert.Equal(t, "", readEvent.ValueHex)
}

func TestLiveAdapterWriteRejectsInvalidHex(t *testing.T) {
	t.Parallel()
	char := &stubCharacteristic{uuid: "2A29"}
	device := &stubDevice{services: []bleService{stubService{uuid: "180A", chars: []bleCharacteristic{char}}}}
	driver := newStubDriver()
	driver.device = device
	adapter := newLiveAdapter(driver)

	_, err := adapter.Write(context.Background(), WriteRequest{Address: "AA", CharacteristicUUID: "2A29", ValueHex: "zz"})
	require.Error(t, err)
	assert.Empty(t, char.written, "no payload is written when the hex is invalid")
	assert.Equal(t, 1, device.disconnectCount, "device is disconnected on the invalid-hex path")
}

func TestLiveAdapterCharacteristicUUIDRequired(t *testing.T) {
	t.Parallel()
	device := &stubDevice{services: []bleService{stubService{uuid: "180A"}}}
	driver := newStubDriver()
	driver.device = device
	adapter := newLiveAdapter(driver)

	_, err := adapter.Read(context.Background(), CharacteristicRequest{Address: "AA"})
	var ae *AdapterError
	require.True(t, errors.As(err, &ae))
	assert.Equal(t, AdapterErrorCharacteristic, ae.Kind)
	assert.Equal(t, 1, device.disconnectCount, "device is disconnected on the validation error path")
}

func TestLiveAdapterCharacteristicNotFound(t *testing.T) {
	t.Parallel()
	device := &stubDevice{services: []bleService{stubService{uuid: "180A", chars: []bleCharacteristic{
		&stubCharacteristic{uuid: "2A29"},
	}}}}
	driver := newStubDriver()
	driver.device = device
	adapter := newLiveAdapter(driver)

	_, err := adapter.Read(context.Background(), CharacteristicRequest{Address: "AA", CharacteristicUUID: "9999"})
	var ae *AdapterError
	require.True(t, errors.As(err, &ae))
	assert.Equal(t, AdapterErrorCharacteristic, ae.Kind)
}

func TestLiveAdapterSubscribeCollectsNotifications(t *testing.T) {
	t.Parallel()
	char := &stubCharacteristic{uuid: "2A29", notifyValues: [][]byte{{0x01}, {0x02, 0x03}}}
	device := &stubDevice{services: []bleService{stubService{uuid: "180A", chars: []bleCharacteristic{char}}}}
	driver := newStubDriver()
	driver.device = device
	adapter := newLiveAdapter(driver)

	events, err := adapter.Subscribe(context.Background(), CharacteristicRequest{Address: "AA", CharacteristicUUID: "2A29", DurationMillis: 20})
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Regexp(t, `^live-notify-180a-2a29-\d+-1$`, events[0].ID)
	assert.NotEmpty(t, events[0].At)
	assert.Equal(t, "01", events[0].ValueHex)
	assert.Regexp(t, `^live-notify-180a-2a29-\d+-2$`, events[1].ID)
	assert.NotEmpty(t, events[1].At)
	assert.Equal(t, "0203", events[1].ValueHex)
}

func TestLiveAdapterConnectContextCancelled(t *testing.T) {
	t.Parallel()
	released := make(chan struct{})
	driver := newStubDriver()
	driver.connectFunc = func(string) (bleDevice, error) {
		<-released // still connecting when the cancelled context wins
		return &stubDevice{}, nil
	}
	adapter := newLiveAdapter(driver)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := adapter.Inspect(ctx, InspectRequest{Address: "AA"})
	assert.ErrorIs(t, err, context.Canceled)
	close(released) // let the late connect goroutine finish
}

func TestLiveAdapterConnectRequiresAddress(t *testing.T) {
	t.Parallel()
	adapter := newLiveAdapter(newStubDriver())
	_, err := adapter.Inspect(context.Background(), InspectRequest{Address: "  "})
	var ae *AdapterError
	require.True(t, errors.As(err, &ae))
	assert.Equal(t, AdapterErrorDeviceNotFound, ae.Kind)
}

func TestLiveAdapterEnableNotConfigured(t *testing.T) {
	t.Parallel()
	adapter := newLiveAdapter(nil)
	err := adapter.enable()
	var ae *AdapterError
	require.True(t, errors.As(err, &ae))
	assert.Equal(t, AdapterErrorUnsupported, ae.Kind)
}

func TestLiveAdapterScanReturnsCandidatesWhenBackendCompletes(t *testing.T) {
	t.Parallel()
	driver := newStubDriver()
	// scanBlocks is false: the backend delivers its advertisements and returns
	// on its own before the duration window elapses.
	driver.scanResults = []bleAdvertisement{
		stubAdv{address: "AA", rssi: -50},
		stubAdv{address: "BB", rssi: -40},
	}
	adapter := newLiveAdapter(driver)

	out, err := adapter.Scan(context.Background(), ScanOptions{DurationMillis: 10_000})
	require.NoError(t, err)
	require.Len(t, out, 2, "candidates collected before the backend returned are not discarded")
	assert.Equal(t, "BB", out[0].Address)
}

func TestLiveAdapterPreScanDeviceNotSeen(t *testing.T) {
	t.Parallel()
	driver := newStubDriver()
	driver.needsPreScan = true
	driver.device = &stubDevice{}
	// The pre-scan never advertises the target, so connect must report
	// device-not-found rather than proceeding or leaking a context deadline.
	driver.scanResults = []bleAdvertisement{stubAdv{address: "BB"}}
	adapter := newLiveAdapter(driver)

	_, err := adapter.Inspect(context.Background(), InspectRequest{Address: "AA"})
	var ae *AdapterError
	require.True(t, errors.As(err, &ae))
	assert.Equal(t, AdapterErrorDeviceNotFound, ae.Kind)
}

func TestLiveAdapterPreScanDeviceSeenProceeds(t *testing.T) {
	t.Parallel()
	driver := newStubDriver()
	driver.needsPreScan = true
	driver.device = &stubDevice{services: []bleService{stubService{uuid: "180A", chars: []bleCharacteristic{
		&stubCharacteristic{uuid: "2A29"},
	}}}}
	driver.scanResults = []bleAdvertisement{stubAdv{address: "AA"}}
	adapter := newLiveAdapter(driver)

	events, err := adapter.Inspect(context.Background(), InspectRequest{Address: "AA"})
	require.NoError(t, err)
	require.Len(t, events, 1, "connect proceeds once the target is seen in the pre-scan")
}

//go:build ble_live && !ble_replay_only && (darwin || linux || windows)

package ble

import (
	"fmt"
	"runtime"
	"slices"
	"sort"
	"time"

	tinyble "tinygo.org/x/bluetooth"
)

// bleConnectTimeout bounds the backend connection attempt so a wedged peripheral
// cannot hang a caller whose context carries no deadline.
const bleConnectTimeout = 30 * time.Second

func LiveSupport() LiveSupportInfo {
	return LiveSupportInfo{
		Compiled: true,
		Backend:  "tinygo.org/x/bluetooth",
		Platform: "darwin/linux/windows",
	}
}

func NewLiveAdapter() (Adapter, error) {
	return newLiveAdapter(&tinybleDriver{adapter: tinyble.DefaultAdapter}), nil
}

// tinybleDriver implements bleDriver against tinygo.org/x/bluetooth. It is the
// only place that imports the backend; all lifecycle logic lives in liveAdapter.
type tinybleDriver struct {
	adapter *tinyble.Adapter
}

func (d *tinybleDriver) Enable() error {
	return d.adapter.Enable()
}

func (d *tinybleDriver) Scan(filterServiceUUIDs []string, onResult func(bleAdvertisement)) error {
	required, err := parseUUIDs(filterServiceUUIDs)
	if err != nil {
		return err
	}
	return d.adapter.Scan(func(_ *tinyble.Adapter, result tinyble.ScanResult) {
		if !matchesServiceUUIDs(result, required) {
			return
		}
		onResult(tinybleAdvertisement{result: result})
	})
}

func (d *tinybleDriver) StopScan() error {
	return d.adapter.StopScan()
}

func (d *tinybleDriver) Connect(address string) (bleDevice, error) {
	var addr tinyble.Address
	addr.Set(address)
	device, err := d.adapter.Connect(addr, tinyble.ConnectionParams{
		ConnectionTimeout: tinyble.NewDuration(bleConnectTimeout),
	})
	if err != nil {
		return nil, err
	}
	return tinybleDevice{device: device}, nil
}

func (d *tinybleDriver) NeedsPreScan() bool {
	return runtime.GOOS == "linux"
}

type tinybleAdvertisement struct {
	result tinyble.ScanResult
}

func (a tinybleAdvertisement) Address() string        { return a.result.Address.String() }
func (a tinybleAdvertisement) LocalName() string      { return a.result.LocalName() }
func (a tinybleAdvertisement) RSSI() int              { return int(a.result.RSSI) }
func (a tinybleAdvertisement) ServiceUUIDs() []string { return uuidStrings(a.result.ServiceUUIDs()) }

type tinybleDevice struct {
	device tinyble.Device
}

func (d tinybleDevice) DiscoverServices(serviceUUIDs []string) ([]bleService, error) {
	filter, err := parseUUIDs(serviceUUIDs)
	if err != nil {
		return nil, err
	}
	services, err := d.device.DiscoverServices(filter)
	if err != nil {
		return nil, err
	}
	out := make([]bleService, 0, len(services))
	for _, service := range services {
		out = append(out, tinybleService{service: service})
	}
	return out, nil
}

func (d tinybleDevice) Disconnect() error {
	return d.device.Disconnect()
}

type tinybleService struct {
	service tinyble.DeviceService
}

func (s tinybleService) UUID() string {
	return s.service.UUID().String()
}

func (s tinybleService) DiscoverCharacteristics(characteristicUUIDs []string) ([]bleCharacteristic, error) {
	filter, err := parseUUIDs(characteristicUUIDs)
	if err != nil {
		return nil, err
	}
	chars, err := s.service.DiscoverCharacteristics(filter)
	if err != nil {
		return nil, err
	}
	out := make([]bleCharacteristic, 0, len(chars))
	for i := range chars {
		// Store a pointer to the backend characteristic: its EnableNotifications
		// is a pointer receiver that mutates internal state, so a value copy
		// would lose the enable's bookkeeping and make the disable a no-op.
		out = append(out, tinybleCharacteristic{characteristic: &chars[i]})
	}
	return out, nil
}

type tinybleCharacteristic struct {
	characteristic *tinyble.DeviceCharacteristic
}

func (c tinybleCharacteristic) UUID() string {
	return c.characteristic.UUID().String()
}

func (c tinybleCharacteristic) Read(buf []byte) (int, error) {
	return c.characteristic.Read(buf)
}

func (c tinybleCharacteristic) Write(payload []byte) (int, error) {
	return c.characteristic.Write(payload)
}

func (c tinybleCharacteristic) EnableNotifications(handler func([]byte)) error {
	return c.characteristic.EnableNotifications(handler)
}

func parseUUIDs(values []string) ([]tinyble.UUID, error) {
	out := make([]tinyble.UUID, 0, len(values))
	for _, value := range values {
		uuid, err := tinyble.ParseUUID(value)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID %q: %w", value, err)
		}
		out = append(out, uuid)
	}
	return out, nil
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

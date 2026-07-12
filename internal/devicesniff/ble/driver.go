package ble

// bleDriver abstracts the host BLE backend so the live-adapter lifecycle logic
// (scan duration handling, context cancellation, discovery, notification
// collection) can be exercised without real hardware. The production
// implementation wraps tinygo.org/x/bluetooth; tests substitute an in-memory
// stub. Drivers return raw backend errors; the live adapter classifies them via
// mapLiveError.
type bleDriver interface {
	Enable() error
	// Scan invokes onResult for each advertisement matching filterServiceUUIDs
	// (empty means all) and blocks until StopScan is called or an error occurs.
	Scan(filterServiceUUIDs []string, onResult func(bleAdvertisement)) error
	StopScan() error
	Connect(address string) (bleDevice, error)
	// NeedsPreScan reports whether the backend requires a device to be observed
	// in a scan before Connect succeeds (BlueZ does; CoreBluetooth does not).
	// Keeping the platform decision behind the seam keeps the pre-scan path
	// exercisable against the test stub.
	NeedsPreScan() bool
}

type bleAdvertisement interface {
	Address() string
	LocalName() string
	RSSI() int
	// ServiceUUIDs returns the advertised UUIDs, normalized and sorted.
	ServiceUUIDs() []string
}

type bleDevice interface {
	// DiscoverServices returns the device's services, optionally filtered to the
	// given UUIDs (empty means all).
	DiscoverServices(serviceUUIDs []string) ([]bleService, error)
	Disconnect() error
}

type bleService interface {
	UUID() string
	// DiscoverCharacteristics returns the service's characteristics, optionally
	// filtered to the given UUIDs (empty means all).
	DiscoverCharacteristics(characteristicUUIDs []string) ([]bleCharacteristic, error)
}

type bleCharacteristic interface {
	UUID() string
	Read(buf []byte) (int, error)
	Write(payload []byte) (int, error)
	EnableNotifications(handler func([]byte)) error
}

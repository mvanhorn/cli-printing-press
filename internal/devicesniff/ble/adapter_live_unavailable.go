//go:build !ble_live || !(darwin || linux || windows)

package ble

func LiveSupport() LiveSupportInfo {
	return LiveSupportInfo{
		Compiled: false,
		Backend:  "unavailable",
		Platform: "darwin/linux/windows",
		Message:  "live BLE support is not compiled in; rebuild ble-probe with -tags ble_live",
	}
}

func NewLiveAdapter() (Adapter, error) {
	return nil, adapterError(AdapterErrorUnsupported, "live BLE support is not compiled in; rebuild ble-probe with -tags ble_live")
}

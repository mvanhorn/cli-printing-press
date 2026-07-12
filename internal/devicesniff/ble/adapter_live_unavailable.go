//go:build !ble_live || ble_replay_only || !(darwin || linux || windows)

package ble

func LiveSupport() LiveSupportInfo {
	return LiveSupportInfo{
		Compiled: false,
		Backend:  "unavailable",
		Platform: "darwin/linux/windows",
		Message:  "live BLE support is not compiled in for this binary or platform",
	}
}

func NewLiveAdapter() (Adapter, error) {
	return nil, adapterError(AdapterErrorUnsupported, "live BLE support is not compiled in for this binary or platform")
}

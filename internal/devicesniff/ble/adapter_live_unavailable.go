//go:build !ble_live || !(darwin || linux || windows)

package ble

func NewLiveAdapter() (Adapter, error) {
	return nil, adapterError(AdapterErrorUnsupported, "live BLE support is not compiled in; rebuild ble-probe with -tags ble_live")
}

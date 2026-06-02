package ble

import (
	"context"
	"fmt"
)

type Adapter interface {
	Scan(ctx context.Context, opts ScanOptions) ([]DeviceCandidate, error)
	Inspect(ctx context.Context, req InspectRequest) ([]Event, error)
	Read(ctx context.Context, req CharacteristicRequest) (Event, error)
	Write(ctx context.Context, req WriteRequest) (Event, error)
	Subscribe(ctx context.Context, req CharacteristicRequest) ([]Event, error)
}

type ScanOptions struct {
	DurationMillis int      `json:"duration_millis,omitempty"`
	ServiceUUIDs   []string `json:"service_uuids,omitempty"`
}

type InspectRequest struct {
	Address string `json:"address"`
}

type CharacteristicRequest struct {
	Address            string `json:"address"`
	ServiceUUID        string `json:"service_uuid,omitempty"`
	CharacteristicUUID string `json:"characteristic_uuid"`
	DurationMillis     int    `json:"duration_millis,omitempty"`
}

type WriteRequest struct {
	Address            string `json:"address"`
	ServiceUUID        string `json:"service_uuid,omitempty"`
	CharacteristicUUID string `json:"characteristic_uuid"`
	ValueHex           string `json:"value_hex"`
}

type LiveSupportInfo struct {
	Compiled bool   `json:"compiled"`
	Backend  string `json:"backend"`
	Platform string `json:"platform"`
	Message  string `json:"message,omitempty"`
}

type AdapterErrorKind string

const (
	AdapterErrorPermissionDenied AdapterErrorKind = "permission_denied"
	AdapterErrorDeviceNotFound   AdapterErrorKind = "device_not_found"
	AdapterErrorCharacteristic   AdapterErrorKind = "characteristic_not_found"
	AdapterErrorUnsupported      AdapterErrorKind = "unsupported"
	AdapterErrorDisconnected     AdapterErrorKind = "disconnected"
	AdapterErrorConcurrentClient AdapterErrorKind = "concurrent_client"
)

type AdapterError struct {
	Kind    AdapterErrorKind
	Message string
}

func (e *AdapterError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return string(e.Kind)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func adapterError(kind AdapterErrorKind, format string, args ...any) *AdapterError {
	return &AdapterError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

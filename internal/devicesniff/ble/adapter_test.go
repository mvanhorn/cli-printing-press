package ble

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplayAdapterScansFromEvidence(t *testing.T) {
	t.Parallel()

	adapter := NewReplayAdapter(loadEvidenceFixture(t, "ble-events.json"))
	devices, err := adapter.Scan(context.Background(), ScanOptions{})
	require.NoError(t, err)

	require.Len(t, devices, 1)
	assert.Equal(t, "AA:BB:CC:DD:EE:01", devices[0].Address)
	assert.Equal(t, "Trevin SessionThing", devices[0].Name)
	assert.Equal(t, -42, devices[0].RSSI)
	assert.Equal(t, []string{"fd00"}, devices[0].ServiceUUIDs)
}

func TestReplayAdapterInspectReturnsNormalizedEvidenceEvents(t *testing.T) {
	t.Parallel()

	adapter := NewReplayAdapter(loadEvidenceFixture(t, "ble-events.json"))
	events, err := adapter.Inspect(context.Background(), InspectRequest{Address: "AA:BB:CC:DD:EE:01"})
	require.NoError(t, err)

	var types []string
	for _, event := range events {
		types = append(types, event.Type)
	}
	assert.Equal(t, []string{
		EventAdvertisement,
		EventServiceDiscovery,
		EventServiceDiscovery,
		EventWrite,
		EventNotification,
		EventNotification,
	}, types)
	assert.Equal(t, "fd01", events[1].CharacteristicUUID)
	assert.Equal(t, []string{"write"}, events[1].Properties)
}

func TestReplayAdapterReadWriteAndSubscribe(t *testing.T) {
	t.Parallel()

	adapter := NewReplayAdapter(EvidenceInput{Events: []Event{
		{ID: "adv", Type: EventAdvertisement, DeviceAddress: "device-1", DeviceName: "Demo"},
		{ID: "svc", Type: EventServiceDiscovery, ServiceUUID: "180f", CharacteristicUUID: "2a19", Properties: []string{"read", "notify"}},
		{ID: "read-battery", Type: EventRead, ServiceUUID: "180f", CharacteristicUUID: "2a19", ValueHex: "64"},
		{ID: "write-mode", Type: EventWrite, ServiceUUID: "ff00", CharacteristicUUID: "ff01", ValueHex: "0102"},
		{ID: "notify-1", Type: EventNotification, ServiceUUID: "180f", CharacteristicUUID: "2a19", ValueHex: "63"},
		{ID: "notify-2", Type: EventNotification, ServiceUUID: "180f", CharacteristicUUID: "2a19", ValueHex: "62"},
	}})

	read, err := adapter.Read(context.Background(), CharacteristicRequest{Address: "device-1", CharacteristicUUID: "2A19"})
	require.NoError(t, err)
	assert.Equal(t, "read-battery", read.ID)
	assert.Equal(t, "64", read.ValueHex)

	write, err := adapter.Write(context.Background(), WriteRequest{Address: "device-1", CharacteristicUUID: "FF01", ValueHex: "0102"})
	require.NoError(t, err)
	assert.Equal(t, "write-mode", write.ID)

	notifications, err := adapter.Subscribe(context.Background(), CharacteristicRequest{Address: "device-1", CharacteristicUUID: "2a19"})
	require.NoError(t, err)
	require.Len(t, notifications, 2)
	assert.Equal(t, "notify-1", notifications[0].ID)
	assert.Equal(t, "notify-2", notifications[1].ID)
}

func TestReplayAdapterReturnsTypedErrors(t *testing.T) {
	t.Parallel()

	adapter := NewReplayAdapter(EvidenceInput{})
	_, err := adapter.Inspect(context.Background(), InspectRequest{Address: "missing"})
	require.Error(t, err)

	var adapterErr *AdapterError
	require.ErrorAs(t, err, &adapterErr)
	assert.Equal(t, AdapterErrorDeviceNotFound, adapterErr.Kind)
}

func loadEvidenceFixture(t *testing.T, name string) EvidenceInput {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "device", "fixtures", name))
	require.NoError(t, err)

	input, err := ParseEvidence(data)
	require.NoError(t, err)
	return input
}

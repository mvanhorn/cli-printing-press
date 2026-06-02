package devicespec

import (
	"encoding/hex"
	"fmt"
	"strings"
)

const SupportedVersion = 1

const (
	ProtocolBLE = "ble"
)

const (
	SessionModeOneShot  = "one-shot"
	SessionModeOptional = "optional"
	SessionModeRequired = "required"

	SessionModePersistent = "persistent"
)

const (
	SessionReasonLowLatencyControls = "low_latency_controls"
	SessionReasonNotificationStream = "notification_stream"
	SessionReasonTelemetrySampling  = "telemetry_sampling"
	SessionReasonReconnect          = "reconnect"
	SessionReasonUnsafeOneShot      = "unsafe_one_shot"
)

const (
	MatchStrengthWeak   = "weak"
	MatchStrengthMedium = "medium"
	MatchStrengthStrong = "strong"
)

const (
	SafetyReadOnly          = "read-only"
	SafetyLowRiskWrite      = "low-risk-write"
	SafetyPhysicalEffect    = "physical-effect"
	SafetyConfigurationRisk = "configuration-risk"
	SafetyUnknown           = "unknown"
)

const (
	ValidationStatusInferred        = "inferred"
	ValidationStatusObserved        = "observed"
	ValidationStatusReplayValidated = "replay-validated"
)

const (
	PayloadEncodingHex = "hex"
)

const (
	ConfidenceLow    = "low"
	ConfidenceMedium = "medium"
	ConfidenceHigh   = "high"
)

type DeviceSpec struct {
	Version      int                `yaml:"version" json:"version"`
	Name         string             `yaml:"name" json:"name"`
	DisplayName  string             `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Protocol     string             `yaml:"protocol" json:"protocol"`
	Identity     DeviceIdentity     `yaml:"identity,omitempty" json:"identity,omitempty"`
	BLE          BLESurface         `yaml:"ble" json:"ble"`
	Capabilities DeviceCapabilities `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Session      SessionProfile     `yaml:"session,omitempty" json:"session,omitempty"`
	Evidence     []EvidenceRef      `yaml:"evidence,omitempty" json:"evidence,omitempty"`
}

type DeviceIdentity struct {
	AdvertisedNames       []string `yaml:"advertised_names,omitempty" json:"advertised_names,omitempty"`
	AddressPolicy         string   `yaml:"address_policy,omitempty" json:"address_policy,omitempty"`
	ManufacturerDataHints []string `yaml:"manufacturer_data_hints,omitempty" json:"manufacturer_data_hints,omitempty"`
	GATTFingerprint       []string `yaml:"gatt_fingerprint,omitempty" json:"gatt_fingerprint,omitempty"`
	MatchStrength         string   `yaml:"match_strength,omitempty" json:"match_strength,omitempty"`
}

type BLESurface struct {
	Services []BLEService `yaml:"services,omitempty" json:"services,omitempty"`
}

type BLEService struct {
	UUID            string              `yaml:"uuid" json:"uuid"`
	Name            string              `yaml:"name,omitempty" json:"name,omitempty"`
	Characteristics []BLECharacteristic `yaml:"characteristics,omitempty" json:"characteristics,omitempty"`
}

type BLECharacteristic struct {
	UUID       string   `yaml:"uuid" json:"uuid"`
	Name       string   `yaml:"name,omitempty" json:"name,omitempty"`
	Properties []string `yaml:"properties,omitempty" json:"properties,omitempty"`
}

type DeviceCapabilities struct {
	Commands  []DeviceCommand  `yaml:"commands,omitempty" json:"commands,omitempty"`
	Telemetry []TelemetryField `yaml:"telemetry,omitempty" json:"telemetry,omitempty"`
}

type DeviceCommand struct {
	Name               string        `yaml:"name" json:"name"`
	CharacteristicUUID string        `yaml:"characteristic_uuid" json:"characteristic_uuid"`
	Safety             string        `yaml:"safety,omitempty" json:"safety,omitempty"`
	ValidationStatus   string        `yaml:"validation_status,omitempty" json:"validation_status,omitempty"`
	EvidenceRefs       []string      `yaml:"evidence_refs,omitempty" json:"evidence_refs,omitempty"`
	Payload            DevicePayload `yaml:"payload,omitempty" json:"payload,omitempty"`
}

type DevicePayload struct {
	Encoding      string                `yaml:"encoding,omitempty" json:"encoding,omitempty"`
	Bytes         []byte                `yaml:"bytes,omitempty" json:"bytes,omitempty"`
	UnknownFields []UnknownPayloadField `yaml:"unknown_fields,omitempty" json:"unknown_fields,omitempty"`
	Checksum      string                `yaml:"checksum,omitempty" json:"checksum,omitempty"`
}

type UnknownPayloadField struct {
	Name       string `yaml:"name" json:"name"`
	ByteRange  string `yaml:"byte_range" json:"byte_range"`
	Confidence string `yaml:"confidence,omitempty" json:"confidence,omitempty"`
}

type TelemetryField struct {
	Name                     string `yaml:"name" json:"name"`
	SourceCharacteristicUUID string `yaml:"source_characteristic_uuid" json:"source_characteristic_uuid"`
	Unit                     string `yaml:"unit,omitempty" json:"unit,omitempty"`
	SampleCadence            string `yaml:"sample_cadence,omitempty" json:"sample_cadence,omitempty"`
	Store                    bool   `yaml:"store,omitempty" json:"store,omitempty"`
}

type SessionProfile struct {
	Mode               string   `yaml:"mode,omitempty" json:"mode,omitempty"`
	Reasons            []string `yaml:"reasons,omitempty" json:"reasons,omitempty"`
	OneShotFallback    bool     `yaml:"one_shot_fallback,omitempty" json:"one_shot_fallback,omitempty"`
	Reconnect          bool     `yaml:"reconnect,omitempty" json:"reconnect,omitempty"`
	NotificationStream bool     `yaml:"notification_stream,omitempty" json:"notification_stream,omitempty"`
}

type EvidenceRef struct {
	ID      string `yaml:"id" json:"id"`
	Source  string `yaml:"source,omitempty" json:"source,omitempty"`
	Summary string `yaml:"summary,omitempty" json:"summary,omitempty"`
}

func (p *DevicePayload) UnmarshalYAML(unmarshal func(any) error) error {
	var raw struct {
		Encoding      string                `yaml:"encoding"`
		Bytes         string                `yaml:"bytes"`
		UnknownFields []UnknownPayloadField `yaml:"unknown_fields"`
		Checksum      string                `yaml:"checksum"`
	}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	p.Encoding = strings.TrimSpace(raw.Encoding)
	p.UnknownFields = raw.UnknownFields
	p.Checksum = strings.TrimSpace(raw.Checksum)
	if raw.Bytes == "" {
		p.Bytes = nil
		return nil
	}
	bytes, err := hex.DecodeString(strings.TrimSpace(raw.Bytes))
	if err != nil {
		return fmt.Errorf("payload.bytes hex: %w", err)
	}
	p.Bytes = bytes
	return nil
}

func (p DevicePayload) MarshalYAML() (any, error) {
	type rawPayload struct {
		Encoding      string                `yaml:"encoding,omitempty"`
		Bytes         string                `yaml:"bytes,omitempty"`
		UnknownFields []UnknownPayloadField `yaml:"unknown_fields,omitempty"`
		Checksum      string                `yaml:"checksum,omitempty"`
	}
	return rawPayload{
		Encoding:      p.Encoding,
		Bytes:         hex.EncodeToString(p.Bytes),
		UnknownFields: p.UnknownFields,
		Checksum:      p.Checksum,
	}, nil
}

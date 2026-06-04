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
)

const (
	SessionReasonLowLatencyControls = "low_latency_controls"
	SessionReasonNotificationStream = "notification_stream"
	SessionReasonTelemetrySampling  = "telemetry_sampling"
	SessionReasonReconnect          = "reconnect"
	SessionReasonUnsafeOneShot      = "unsafe_one_shot"
)

// Transport write modes. acknowledged (write-with-response) is the default and
// the safe choice for control commands: the peripheral confirms before the next
// write, so a command is not dropped by an immediate disconnect. without-response
// is for characteristics that only permit write-command (no-response) traffic.
const (
	WriteModeAcknowledged    = "acknowledged"
	WriteModeWithoutResponse = "without-response"
)

// Transport teardown semantics: does dropping the BLE connection stop in-flight
// actuation? keep-running means the device sustains its last state after
// disconnect (so a one-shot write can actuate); stop-on-disconnect means control
// only holds while the connection is held.
const (
	TeardownKeepRunning      = "keep-running"
	TeardownStopOnDisconnect = "stop-on-disconnect"
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

// Command parameter types. Informational: the generated CLI passes argument
// strings through to the device codec's EncodeCommand, which parses and encodes
// them. The type guides help text and the codec, not generated parsing.
const (
	ParamTypeString = "string"
	ParamTypeInt    = "int"
	ParamTypeFloat  = "float"
	ParamTypeHex    = "hex"
	ParamTypeBool   = "bool"
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
	Identity     DeviceIdentity     `yaml:"identity,omitempty" json:"identity"`
	BLE          BLESurface         `yaml:"ble" json:"ble"`
	Capabilities DeviceCapabilities `yaml:"capabilities,omitempty" json:"capabilities"`
	Session      SessionProfile     `yaml:"session,omitempty" json:"session"`
	Transport    TransportContract  `yaml:"transport,omitempty" json:"transport"`
	Quirks       []DeviceQuirk      `yaml:"quirks,omitempty" json:"quirks,omitempty"`
	Workflows    []DeviceWorkflow   `yaml:"workflows,omitempty" json:"workflows,omitempty"`
	Evidence     []EvidenceRef      `yaml:"evidence,omitempty" json:"evidence,omitempty"`
}

// Quirk categories. Free-form is allowed (validated as a hint, not a hard enum),
// but these are the common phases a behavioral quirk attaches to.
const (
	QuirkCategoryInit        = "init"
	QuirkCategoryConnect     = "connect"
	QuirkCategoryWrite       = "write"
	QuirkCategoryNotify      = "notify"
	QuirkCategoryTeardown    = "teardown"
	QuirkCategoryPairing     = "pairing"
	QuirkCategoryFirmware    = "firmware"
	QuirkCategoryConcurrency = "concurrency"
	QuirkCategoryOther       = "other"
)

// DeviceQuirk is a qualitative behavioral fact that does not reduce to a typed
// field but must be factored into the printed CLI — an init trick, a stale-session
// gotcha, a firmware-variant opcode shift, a notify-enable dance. Synthesized from
// forums, issues, docs, and reference code during the research gate; it cannot
// drive codegen, so it is carried as cited required-reading for the codec author,
// surfaced by the generated CLI (README / doctor), and verified during dogfood.
// Summary is the quirk; Handling is what the CLI/codec must do about it.
type DeviceQuirk struct {
	Category     string   `yaml:"category,omitempty" json:"category,omitempty"`
	Summary      string   `yaml:"summary" json:"summary"`
	Handling     string   `yaml:"handling,omitempty" json:"handling,omitempty"`
	EvidenceRefs []string `yaml:"evidence_refs,omitempty" json:"evidence_refs,omitempty"`
}

// DeviceWorkflow is a proven end-to-end operating sequence for one user goal —
// the "spine" of how the reference implementation actually drives the device
// (start a walk, stop the belt, pair-then-arm). It composes the atomic facts in
// Transport (ceremony, settle delays, write mode, spacing) and the action map in
// Capabilities into the ordered sequence that is known to work, synthesized from
// reference code during the research gate and cited in EvidenceRefs. It does not
// drive codegen: it is the contract the implemented control flow (codec plus
// held-connection choreography) is checked against in the QA workflow-fidelity
// pass and confirmed on hardware during dogfood. Capturing the spine here is what
// stops an agent from rediscovering a documented sequence by guessing on
// hardware. Steps are ordered and human-readable, each naming the command or
// transport fact it invokes.
type DeviceWorkflow struct {
	Name         string   `yaml:"name" json:"name"`
	Goal         string   `yaml:"goal,omitempty" json:"goal,omitempty"`
	Steps        []string `yaml:"steps" json:"steps"`
	EvidenceRefs []string `yaml:"evidence_refs,omitempty" json:"evidence_refs,omitempty"`
	Notes        string   `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// TransportContract is the operational protocol contract: how to *talk to* the
// device, alongside the action->payload map in Capabilities. It is synthesized
// from reference implementations and protocol captures during the research gate
// and verified on hardware during dogfood — not rediscovered command-by-command.
// The generator consumes WriteMode and CommandSpacingMS directly (emitting the
// matching write path and a paced writer); the remaining fields are the cited
// contract that drives the codec/choreography and the dogfood checklist. Capture
// every field you have evidence for, and cite it in EvidenceRefs.
type TransportContract struct {
	WriteMode        string         `yaml:"write_mode,omitempty" json:"write_mode,omitempty"`
	CommandSpacingMS int            `yaml:"command_spacing_ms,omitempty" json:"command_spacing_ms,omitempty"`
	ConnectCeremony  []CeremonyStep `yaml:"connect_ceremony,omitempty" json:"connect_ceremony,omitempty"`
	SettleDelays     []SettleDelay  `yaml:"settle_delays,omitempty" json:"settle_delays,omitempty"`
	PollCadenceMS    int            `yaml:"poll_cadence_ms,omitempty" json:"poll_cadence_ms,omitempty"`
	Teardown         string         `yaml:"teardown,omitempty" json:"teardown,omitempty"`
	SingleClient     bool           `yaml:"single_client,omitempty" json:"single_client,omitempty"`
	EvidenceRefs     []string       `yaml:"evidence_refs,omitempty" json:"evidence_refs,omitempty"`
}

// CeremonyStep is one step of the post-subscribe connection handshake some
// devices need before they accept control or stream telemetry: write ValueHex to
// CharacteristicUUID (when set), then wait WaitMS. The caller must already be
// subscribed so the device's responses are not dropped.
type CeremonyStep struct {
	Name               string `yaml:"name,omitempty" json:"name,omitempty"`
	CharacteristicUUID string `yaml:"characteristic_uuid,omitempty" json:"characteristic_uuid,omitempty"`
	ValueHex           string `yaml:"value_hex,omitempty" json:"value_hex,omitempty"`
	WaitMS             int    `yaml:"wait_ms,omitempty" json:"wait_ms,omitempty"`
}

// SettleDelay names a required pause between two state changes (for example the
// delay a belt needs after a mode switch before it honors a start command).
type SettleDelay struct {
	Name string `yaml:"name" json:"name"`
	MS   int    `yaml:"ms" json:"ms"`
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
	Name               string             `yaml:"name" json:"name"`
	CharacteristicUUID string             `yaml:"characteristic_uuid" json:"characteristic_uuid"`
	Safety             string             `yaml:"safety,omitempty" json:"safety,omitempty"`
	ValidationStatus   string             `yaml:"validation_status,omitempty" json:"validation_status,omitempty"`
	EvidenceRefs       []string           `yaml:"evidence_refs,omitempty" json:"evidence_refs,omitempty"`
	Payload            DevicePayload      `yaml:"payload,omitempty" json:"payload"`
	Parameters         []CommandParameter `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}

// CommandParameter declares a typed positional argument for a parameterized
// command (e.g. set-speed <kmh>). The generated command parses no types itself:
// it forwards argument strings to the device codec's EncodeCommand, which builds
// the payload. Parameterized commands therefore require a registered DeviceCodec.
type CommandParameter struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type,omitempty" json:"type,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
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

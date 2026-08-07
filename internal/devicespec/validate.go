package devicespec

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

func (s *DeviceSpec) Validate() error {
	if s.Version != SupportedVersion {
		return fmt.Errorf("unsupported device spec version %d: regenerate or migrate this artifact to version %d", s.Version, SupportedVersion)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !naming.IsSlug(s.Name) {
		return fmt.Errorf("name must be a kebab-case slug")
	}
	if s.Protocol != ProtocolBLE {
		return fmt.Errorf("protocol must be %q", ProtocolBLE)
	}
	if err := validateEnum("identity.match_strength", s.Identity.MatchStrength, "", MatchStrengthWeak, MatchStrengthMedium, MatchStrengthStrong); err != nil {
		return err
	}
	if err := validateEnum("session.mode", s.Session.Mode, SessionModeOneShot, SessionModeOptional, SessionModeRequired); err != nil {
		return err
	}
	if s.Session.OneShotFallback && s.Session.Mode != SessionModeOptional {
		return fmt.Errorf("session.one_shot_fallback can only be true when session.mode is %q", SessionModeOptional)
	}
	for i, reason := range s.Session.Reasons {
		if err := validateEnum(fmt.Sprintf("session.reasons[%d]", i), reason, SessionReasonLowLatencyControls, SessionReasonNotificationStream, SessionReasonTelemetrySampling, SessionReasonReconnect, SessionReasonUnsafeOneShot); err != nil {
			return err
		}
	}
	characteristics := map[string]BLECharacteristic{}
	for si, service := range s.BLE.Services {
		if strings.TrimSpace(service.UUID) == "" {
			return fmt.Errorf("ble.services[%d].uuid is required", si)
		}
		for ci, ch := range service.Characteristics {
			if strings.TrimSpace(ch.UUID) == "" {
				return fmt.Errorf("ble.services[%d].characteristics[%d].uuid is required", si, ci)
			}
			characteristics[NormalizeUUID(ch.UUID)] = ch
		}
	}
	for i, cmd := range s.Capabilities.Commands {
		if strings.TrimSpace(cmd.Name) == "" {
			return fmt.Errorf("commands[%d].name is required", i)
		}
		if strings.TrimSpace(cmd.CharacteristicUUID) == "" {
			return fmt.Errorf("commands[%d].characteristic_uuid is required", i)
		}
		if _, ok := characteristics[NormalizeUUID(cmd.CharacteristicUUID)]; !ok {
			return fmt.Errorf("commands[%d] references missing characteristic %q", i, cmd.CharacteristicUUID)
		}
		if strings.TrimSpace(cmd.Safety) == "" {
			return fmt.Errorf("commands[%d].safety is required", i)
		}
		if err := validateEnum(fmt.Sprintf("commands[%d].safety", i), cmd.Safety, SafetyReadOnly, SafetyLowRiskWrite, SafetyPhysicalEffect, SafetyConfigurationRisk, SafetyUnknown); err != nil {
			return err
		}
		if err := validateEnum(fmt.Sprintf("commands[%d].validation_status", i), cmd.ValidationStatus, "", ValidationStatusInferred, ValidationStatusObserved, ValidationStatusReplayValidated); err != nil {
			return err
		}
		if err := validateEnum(fmt.Sprintf("commands[%d].payload.encoding", i), cmd.Payload.Encoding, "", PayloadEncodingHex); err != nil {
			return err
		}
		for j, field := range cmd.Payload.UnknownFields {
			if strings.TrimSpace(field.Name) == "" {
				return fmt.Errorf("commands[%d].payload.unknown_fields[%d].name is required", i, j)
			}
			if strings.TrimSpace(field.ByteRange) == "" {
				return fmt.Errorf("commands[%d].payload.unknown_fields[%d].byte_range is required", i, j)
			}
			if err := validateEnum(fmt.Sprintf("commands[%d].payload.unknown_fields[%d].confidence", i, j), field.Confidence, "", ConfidenceLow, ConfidenceMedium, ConfidenceHigh); err != nil {
				return err
			}
		}
		for j, param := range cmd.Parameters {
			if strings.TrimSpace(param.Name) == "" {
				return fmt.Errorf("commands[%d].parameters[%d].name is required", i, j)
			}
			if err := validateEnum(fmt.Sprintf("commands[%d].parameters[%d].type", i, j), param.Type, "", ParamTypeString, ParamTypeInt, ParamTypeFloat, ParamTypeHex, ParamTypeBool); err != nil {
				return err
			}
		}
	}
	for i, field := range s.Capabilities.Telemetry {
		if strings.TrimSpace(field.Name) == "" {
			return fmt.Errorf("telemetry[%d].name is required", i)
		}
		if strings.TrimSpace(field.SourceCharacteristicUUID) == "" {
			return fmt.Errorf("telemetry[%d].source_characteristic_uuid is required", i)
		}
		if _, ok := characteristics[NormalizeUUID(field.SourceCharacteristicUUID)]; !ok {
			return fmt.Errorf("telemetry[%d] references missing characteristic %q", i, field.SourceCharacteristicUUID)
		}
	}
	if err := validateTransport(s.Transport, characteristics); err != nil {
		return err
	}
	for i, q := range s.Quirks {
		if strings.TrimSpace(q.Summary) == "" {
			return fmt.Errorf("quirks[%d].summary is required", i)
		}
	}
	for i, w := range s.Workflows {
		if strings.TrimSpace(w.Name) == "" {
			return fmt.Errorf("workflows[%d].name is required", i)
		}
		if len(w.Steps) == 0 {
			return fmt.Errorf("workflows[%d] (%q) must list at least one step", i, w.Name)
		}
		for j, step := range w.Steps {
			if strings.TrimSpace(step) == "" {
				return fmt.Errorf("workflows[%d].steps[%d] must not be empty", i, j)
			}
		}
	}
	return nil
}

func validateTransport(t TransportContract, characteristics map[string]BLECharacteristic) error {
	if err := validateEnum("transport.write_mode", t.WriteMode, "", WriteModeAcknowledged, WriteModeWithoutResponse); err != nil {
		return err
	}
	if err := validateNonNegative("transport.command_spacing_ms", t.CommandSpacingMS); err != nil {
		return err
	}
	if err := validateNonNegative("transport.poll_cadence_ms", t.PollCadenceMS); err != nil {
		return err
	}
	if err := validateEnum("transport.teardown", t.Teardown, "", TeardownKeepRunning, TeardownStopOnDisconnect); err != nil {
		return err
	}
	for i, step := range t.ConnectCeremony {
		if err := validateNonNegative(fmt.Sprintf("transport.connect_ceremony[%d].wait_ms", i), step.WaitMS); err != nil {
			return err
		}
		if step.ValueHex != "" {
			if _, err := hex.DecodeString(step.ValueHex); err != nil {
				return fmt.Errorf("transport.connect_ceremony[%d].value_hex must be valid hex: %w", i, err)
			}
			if strings.TrimSpace(step.CharacteristicUUID) == "" {
				return fmt.Errorf("transport.connect_ceremony[%d] has value_hex but no characteristic_uuid", i)
			}
		}
		if step.CharacteristicUUID != "" {
			if _, ok := characteristics[NormalizeUUID(step.CharacteristicUUID)]; !ok {
				return fmt.Errorf("transport.connect_ceremony[%d] references missing characteristic %q", i, step.CharacteristicUUID)
			}
		}
	}
	for i, d := range t.SettleDelays {
		if strings.TrimSpace(d.Name) == "" {
			return fmt.Errorf("transport.settle_delays[%d].name is required", i)
		}
		if err := validateNonNegative(fmt.Sprintf("transport.settle_delays[%d].ms", i), d.MS); err != nil {
			return err
		}
	}
	return nil
}

func validateNonNegative(field string, v int) error {
	if v < 0 {
		return fmt.Errorf("%s must be >= 0", field)
	}
	return nil
}

func validateEnum(field, got string, allowed ...string) error {
	if slices.Contains(allowed, got) {
		return nil
	}
	return fmt.Errorf("%s must be one of %s", field, strings.Join(quoteValues(allowed), ", "))
}

func quoteValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			out = append(out, `""`)
			continue
		}
		out = append(out, fmt.Sprintf("%q", value))
	}
	return out
}

// NormalizeUUID lower-cases and trims a UUID so service/characteristic lookups
// compare equal regardless of source casing or surrounding whitespace. Exported
// as the single source of truth shared by the devicesniff packages.
func NormalizeUUID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

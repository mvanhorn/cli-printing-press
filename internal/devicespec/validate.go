package devicespec

import (
	"fmt"
	"regexp"
	"strings"
)

var deviceSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func (s *DeviceSpec) Validate() error {
	if s.Version != SupportedVersion {
		return fmt.Errorf("unsupported device spec version %d: regenerate or migrate this artifact to version %d", s.Version, SupportedVersion)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !deviceSlugPattern.MatchString(s.Name) {
		return fmt.Errorf("name must be a kebab-case slug")
	}
	if s.Protocol != ProtocolBLE {
		return fmt.Errorf("protocol must be %q", ProtocolBLE)
	}
	if err := validateEnum("identity.match_strength", s.Identity.MatchStrength, "", MatchStrengthWeak, MatchStrengthMedium, MatchStrengthStrong); err != nil {
		return err
	}
	if err := validateEnum("session.mode", s.Session.Mode, SessionModeOneShot, SessionModePersistent); err != nil {
		return err
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
			characteristics[normalizeUUID(ch.UUID)] = ch
		}
	}
	for i, cmd := range s.Capabilities.Commands {
		if strings.TrimSpace(cmd.Name) == "" {
			return fmt.Errorf("commands[%d].name is required", i)
		}
		if strings.TrimSpace(cmd.CharacteristicUUID) == "" {
			return fmt.Errorf("commands[%d].characteristic_uuid is required", i)
		}
		if _, ok := characteristics[normalizeUUID(cmd.CharacteristicUUID)]; !ok {
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
	}
	for i, field := range s.Capabilities.Telemetry {
		if strings.TrimSpace(field.Name) == "" {
			return fmt.Errorf("telemetry[%d].name is required", i)
		}
		if strings.TrimSpace(field.SourceCharacteristicUUID) == "" {
			return fmt.Errorf("telemetry[%d].source_characteristic_uuid is required", i)
		}
		if _, ok := characteristics[normalizeUUID(field.SourceCharacteristicUUID)]; !ok {
			return fmt.Errorf("telemetry[%d] references missing characteristic %q", i, field.SourceCharacteristicUUID)
		}
	}
	return nil
}

func validateEnum(field, got string, allowed ...string) error {
	for _, value := range allowed {
		if got == value {
			return nil
		}
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

func normalizeUUID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

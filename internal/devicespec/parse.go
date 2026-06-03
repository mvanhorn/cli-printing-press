package devicespec

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const legacySessionModePersistent = "persistent"

func Parse(path string) (*DeviceSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseBytes(data)
}

func ParseBytes(data []byte) (*DeviceSpec, error) {
	var ds DeviceSpec
	if err := yaml.Unmarshal(data, &ds); err != nil {
		return nil, fmt.Errorf("parse device spec: %w", err)
	}
	ds.applyDefaults(oneShotFallbackPresent(data))
	if err := ds.Validate(); err != nil {
		return nil, err
	}
	return &ds, nil
}

func LooksLikeDeviceSpec(data []byte) bool {
	var raw struct {
		Protocol string     `yaml:"protocol"`
		BLE      BLESurface `yaml:"ble"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return false
	}
	return raw.Protocol == ProtocolBLE && len(raw.BLE.Services) > 0
}

// oneShotFallbackPresent reports whether session.one_shot_fallback was set
// explicitly in the source YAML, so applyDefaults can default it without
// clobbering an explicit false (which Validate permits for optional mode).
func oneShotFallbackPresent(data []byte) bool {
	var raw struct {
		Session struct {
			OneShotFallback *bool `yaml:"one_shot_fallback"`
		} `yaml:"session"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return false
	}
	return raw.Session.OneShotFallback != nil
}

func (s *DeviceSpec) applyDefaults(oneShotFallbackExplicit bool) {
	if s.Protocol == "" {
		s.Protocol = ProtocolBLE
	}
	if s.Session.Mode == legacySessionModePersistent {
		s.Session.Mode = SessionModeOptional
	}
	if s.Session.Mode == "" {
		s.Session.Mode = SessionModeOneShot
	}
	if s.Session.Mode == SessionModeOptional && !oneShotFallbackExplicit {
		s.Session.OneShotFallback = true
	}
}

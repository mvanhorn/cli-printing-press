package devicespec

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

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
	ds.applyDefaults()
	if err := ds.Validate(); err != nil {
		return nil, err
	}
	return &ds, nil
}

func (s *DeviceSpec) applyDefaults() {
	if s.Protocol == "" {
		s.Protocol = ProtocolBLE
	}
	if s.Session.Mode == SessionModePersistent {
		s.Session.Mode = SessionModeOptional
	}
	if s.Session.Mode == "" {
		s.Session.Mode = SessionModeOneShot
	}
	if s.Session.Mode == SessionModeOptional {
		s.Session.OneShotFallback = true
	}
}

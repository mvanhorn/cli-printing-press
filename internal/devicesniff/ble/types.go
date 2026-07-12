package ble

import (
	"encoding/json"
	"fmt"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
)

const (
	EventAdvertisement    = "advertisement"
	EventServiceDiscovery = "service_discovery"
	EventWrite            = "write"
	EventNotification     = "notification"
	EventRead             = "read"
)

const (
	CommandDiscoveryNoSurface            = "no_surface"
	CommandDiscoveryInsufficientGuidance = "insufficient_guidance"
	CommandDiscoveryAmbiguousGuidance    = "ambiguous_guidance"
	CommandDiscoveryGuidedCandidates     = "guided_candidates"
	CommandDiscoveryReplayValidated      = "replay_validated"
)

const (
	GuidanceSourceActionJournal      = "action_journal"
	GuidanceSourceCommunityReference = "community_reference"
	GuidanceSourceReplayValidation   = "replay_validation"
)

const (
	EvidenceSourceActionJournal      = "action-journal"
	EvidenceSourceBLEEvent           = "ble-event"
	EvidenceSourceCommunityReference = "community-reference"
)

type EvidenceInput struct {
	Name                string                    `json:"name"`
	DisplayName         string                    `json:"display_name,omitempty"`
	RedactionTerms      []string                  `json:"redaction_terms,omitempty"`
	Identity            devicespec.DeviceIdentity `json:"identity"`
	Events              []Event                   `json:"events,omitempty"`
	Actions             []ActionMarker            `json:"actions,omitempty"`
	CommunityReferences []CommunityReference      `json:"community_references,omitempty"`
}

type Event struct {
	ID                 string   `json:"id"`
	Type               string   `json:"type"`
	At                 string   `json:"at,omitempty"`
	DeviceAddress      string   `json:"device_address,omitempty"`
	DeviceName         string   `json:"device_name,omitempty"`
	RSSI               int      `json:"rssi,omitempty"`
	ServiceUUIDs       []string `json:"service_uuids,omitempty"`
	ServiceUUID        string   `json:"service_uuid,omitempty"`
	CharacteristicUUID string   `json:"characteristic_uuid,omitempty"`
	Properties         []string `json:"properties,omitempty"`
	ValueHex           string   `json:"value_hex,omitempty"`
}

type ActionMarker struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	At     string `json:"at"`
	Safety string `json:"safety,omitempty"`
}

type CommunityReference struct {
	ID                 string `json:"id"`
	CommandName        string `json:"command_name"`
	CharacteristicUUID string `json:"characteristic_uuid"`
	PayloadHex         string `json:"payload_hex,omitempty"`
	Safety             string `json:"safety,omitempty"`
}

type Analysis struct {
	Spec   *devicespec.DeviceSpec `json:"spec"`
	Report Report                 `json:"report"`
}

type Report struct {
	DeviceCandidates          []DeviceCandidate  `json:"device_candidates,omitempty"`
	RequiresOperatorSelection bool               `json:"requires_operator_selection,omitempty"`
	CommandDiscovery          CommandDiscovery   `json:"command_discovery"`
	CommandCandidates         []CandidateSummary `json:"command_candidates,omitempty"`
	TelemetryCandidates       []CandidateSummary `json:"telemetry_candidates,omitempty"`
	Ambiguities               []string           `json:"ambiguities,omitempty"`
	Warnings                  []string           `json:"warnings,omitempty"`
}

type CommandDiscovery struct {
	Status               string   `json:"status"`
	GuidanceSources      []string `json:"guidance_sources,omitempty"`
	WritableTargets      []string `json:"writable_targets,omitempty"`
	Message              string   `json:"message"`
	ReplayValidationNext bool     `json:"replay_validation_next,omitempty"`
}

type DeviceCandidate struct {
	Address      string   `json:"address,omitempty"`
	Name         string   `json:"name,omitempty"`
	RSSI         int      `json:"rssi,omitempty"`
	ServiceUUIDs []string `json:"service_uuids,omitempty"`
}

type CandidateSummary struct {
	Name         string   `json:"name"`
	Summary      string   `json:"summary"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

func ParseEvidence(data []byte) (EvidenceInput, error) {
	var input EvidenceInput
	if err := json.Unmarshal(data, &input); err != nil {
		return EvidenceInput{}, fmt.Errorf("parse evidence JSON: %w", err)
	}
	return input, nil
}

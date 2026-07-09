package bleprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicesniff/ble"
	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
	"github.com/spf13/cobra"
)

type probeOptions struct {
	inputPath          string
	live               bool
	address            string
	serviceUUID        string
	characteristicUUID string
	valueHex           string
	serviceUUIDs       []string
	durationMillis     int
	redactionTerms     []string
}

type DoctorReport struct {
	Binary           string              `json:"binary"`
	GOOS             string              `json:"goos"`
	GOARCH           string              `json:"goarch"`
	Live             ble.LiveSupportInfo `json:"live"`
	ReplaySupported  bool                `json:"replay_supported"`
	SmokeCommands    []string            `json:"smoke_commands"`
	HardwareCommands []string            `json:"hardware_commands"`
}

func NewRootCommand(name string) *cobra.Command {
	if name == "" {
		name = "ble-probe"
	}
	cmd := &cobra.Command{
		Use:          name,
		Short:        "Probe BLE devices and emit normalized Printing Press evidence",
		SilenceUsage: true,
	}
	AddProbeCommands(cmd, name)
	return cmd
}

func AddProbeCommands(cmd *cobra.Command, commandPrefix string) {
	if commandPrefix == "" {
		commandPrefix = cmd.CommandPath()
	}
	cmd.AddCommand(newScanCmd())
	cmd.AddCommand(newInspectCmd())
	cmd.AddCommand(newReadCmd())
	cmd.AddCommand(newWriteCmd())
	cmd.AddCommand(newSubscribeCmd())
	cmd.AddCommand(newMergeCmd())
	cmd.AddCommand(newDoctorCmd(commandPrefix))
}

func newDoctorCmd(name string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report BLE probe build and hardware-test readiness",
		RunE: func(cmd *cobra.Command, args []string) error {
			report := DoctorReport{
				Binary:          name,
				GOOS:            runtime.GOOS,
				GOARCH:          runtime.GOARCH,
				Live:            ble.LiveSupport(),
				ReplaySupported: true,
				SmokeCommands: []string{
					name + " scan --input testdata/device/fixtures/ble-events.json",
					name + " inspect --input testdata/device/fixtures/ble-events.json",
					name + " merge scan.json inspect.json read.json notify.json > evidence.json",
				},
				HardwareCommands: []string{
					name + " scan --live --duration-ms 10000",
					name + " inspect --live --address ADDRESS",
					name + " read --live --address ADDRESS --service SERVICE_UUID --characteristic CHARACTERISTIC_UUID",
					name + " subscribe --live --address ADDRESS --service SERVICE_UUID --characteristic CHARACTERISTIC_UUID --duration-ms 10000",
				},
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				return fmt.Errorf("format doctor report: %w", err)
			}
			return nil
		},
	}
	return cmd
}

func newScanCmd() *cobra.Command {
	opts := probeOptions{}
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Emit BLE advertisements as normalized evidence",
		RunE: func(cmd *cobra.Command, args []string) error {
			input, adapter, err := loadBackend(opts)
			if err != nil {
				return err
			}
			devices, err := adapter.Scan(cmd.Context(), ble.ScanOptions{ServiceUUIDs: opts.serviceUUIDs, DurationMillis: liveDurationMillis(opts)})
			if err != nil {
				return err
			}
			events := make([]ble.Event, 0, len(devices))
			for i, device := range devices {
				events = append(events, ble.Event{
					ID:            fmt.Sprintf("scan-%d", i+1),
					Type:          ble.EventAdvertisement,
					DeviceAddress: device.Address,
					DeviceName:    device.Name,
					RSSI:          device.RSSI,
					ServiceUUIDs:  append([]string(nil), device.ServiceUUIDs...),
				})
			}
			return writeEvidence(cmd, evidenceWithEvents(input, events))
		},
	}
	addBackendFlags(cmd, &opts)
	addDurationFlag(cmd, &opts)
	cmd.Flags().StringArrayVar(&opts.serviceUUIDs, "service-uuid", nil, "Filter replayed scan results by advertised service UUID")
	return cmd
}

func newInspectCmd() *cobra.Command {
	opts := probeOptions{}
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Emit BLE discovery and traffic for one device",
		RunE: func(cmd *cobra.Command, args []string) error {
			input, adapter, err := loadBackend(opts)
			if err != nil {
				return err
			}
			events, err := adapter.Inspect(cmd.Context(), ble.InspectRequest{Address: opts.address})
			if err != nil {
				return err
			}
			return writeEvidence(cmd, evidenceWithEvents(input, events))
		},
	}
	addBackendFlags(cmd, &opts)
	cmd.Flags().StringVar(&opts.address, "address", "", "BLE device address from scan output; optional when replay evidence has exactly one device")
	return cmd
}

func newReadCmd() *cobra.Command {
	opts := probeOptions{}
	cmd := &cobra.Command{
		Use:   "read",
		Short: "Emit a BLE characteristic read as normalized evidence",
		RunE: func(cmd *cobra.Command, args []string) error {
			input, adapter, err := loadBackend(opts)
			if err != nil {
				return err
			}
			event, err := adapter.Read(cmd.Context(), ble.CharacteristicRequest{
				Address:            opts.address,
				ServiceUUID:        opts.serviceUUID,
				CharacteristicUUID: opts.characteristicUUID,
				DurationMillis:     opts.durationMillis,
			})
			if err != nil {
				return err
			}
			return writeEvidence(cmd, evidenceWithEvents(input, []ble.Event{event}))
		},
	}
	addCharacteristicFlags(cmd, &opts)
	return cmd
}

func newWriteCmd() *cobra.Command {
	opts := probeOptions{}
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Emit a BLE characteristic write as normalized evidence",
		RunE: func(cmd *cobra.Command, args []string) error {
			input, adapter, err := loadBackend(opts)
			if err != nil {
				return err
			}
			event, err := adapter.Write(cmd.Context(), ble.WriteRequest{
				Address:            opts.address,
				ServiceUUID:        opts.serviceUUID,
				CharacteristicUUID: opts.characteristicUUID,
				ValueHex:           opts.valueHex,
			})
			if err != nil {
				return err
			}
			return writeEvidence(cmd, evidenceWithEvents(input, []ble.Event{event}))
		},
	}
	addCharacteristicFlags(cmd, &opts)
	cmd.Flags().StringVar(&opts.valueHex, "value-hex", "", "Hex payload to match against replayed write evidence")
	_ = cmd.MarkFlagRequired("value-hex")
	return cmd
}

func newSubscribeCmd() *cobra.Command {
	opts := probeOptions{}
	cmd := &cobra.Command{
		Use:   "subscribe",
		Short: "Emit BLE notifications as normalized evidence",
		RunE: func(cmd *cobra.Command, args []string) error {
			input, adapter, err := loadBackend(opts)
			if err != nil {
				return err
			}
			events, err := adapter.Subscribe(cmd.Context(), ble.CharacteristicRequest{
				Address:            opts.address,
				ServiceUUID:        opts.serviceUUID,
				CharacteristicUUID: opts.characteristicUUID,
				DurationMillis:     liveDurationMillis(opts),
			})
			if err != nil {
				return err
			}
			return writeEvidence(cmd, evidenceWithEvents(input, events))
		},
	}
	addCharacteristicFlags(cmd, &opts)
	addDurationFlag(cmd, &opts)
	return cmd
}

func newMergeCmd() *cobra.Command {
	opts := probeOptions{}
	cmd := &cobra.Command{
		Use:   "merge EVIDENCE_JSON...",
		Short: "Merge normalized BLE evidence files for device-sniff",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inputs := make([]ble.EvidenceInput, 0, len(args))
			for _, path := range args {
				data, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("loading %s: %w", path, err)
				}
				input, err := ble.ParseEvidence(data)
				if err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				inputs = append(inputs, input)
			}
			return writeEvidence(cmd, MergeEvidence(inputs, opts.redactionTerms...))
		},
	}
	cmd.Flags().StringArrayVar(&opts.redactionTerms, "redact-term", nil, "Sensitive device-name term to carry into device-sniff redaction; repeat as needed")
	return cmd
}

func addBackendFlags(cmd *cobra.Command, opts *probeOptions) {
	cmd.Flags().StringVar(&opts.inputPath, "input", "", "Path to replay evidence JSON")
	cmd.Flags().BoolVar(&opts.live, "live", false, "Use the live BLE adapter when this binary supports it")
}

func addCharacteristicFlags(cmd *cobra.Command, opts *probeOptions) {
	addBackendFlags(cmd, opts)
	cmd.Flags().StringVar(&opts.address, "address", "", "BLE device address from scan output; optional when replay evidence has exactly one device")
	cmd.Flags().StringVar(&opts.serviceUUID, "service", "", "Service UUID used to disambiguate the characteristic")
	cmd.Flags().StringVar(&opts.characteristicUUID, "characteristic", "", "Characteristic UUID")
	_ = cmd.MarkFlagRequired("characteristic")
}

func addDurationFlag(cmd *cobra.Command, opts *probeOptions) {
	cmd.Flags().IntVar(&opts.durationMillis, "duration-ms", 10_000, "Live scan or notification duration in milliseconds")
}

// dogfoodMaxDurationMillis caps the live scan/subscribe window under the dogfood
// matrix so a long default cannot trip the per-command timeout.
const dogfoodMaxDurationMillis = 3000

func isVerifyEnv() bool  { return os.Getenv("PRINTING_PRESS_VERIFY") == "1" }
func isDogfoodEnv() bool { return os.Getenv("PRINTING_PRESS_DOGFOOD") == "1" }

var liveAdapterFactory = ble.NewLiveAdapter

// liveDurationMillis curtails the live scan/subscribe window under the dogfood
// matrix; replay backends ignore the duration, so the cap is a no-op there.
func liveDurationMillis(opts probeOptions) int {
	if opts.live && isDogfoodEnv() && opts.durationMillis > dogfoodMaxDurationMillis {
		return dogfoodMaxDurationMillis
	}
	return opts.durationMillis
}

func loadBackend(opts probeOptions) (ble.EvidenceInput, ble.Adapter, error) {
	if opts.live {
		// Side-effect floor: never actuate real BLE hardware inside a verify
		// subprocess, regardless of which command invoked us.
		if isVerifyEnv() {
			return ble.EvidenceInput{}, nil, fmt.Errorf("live BLE is disabled under PRINTING_PRESS_VERIFY; pass --input replay evidence instead")
		}
		adapter, err := liveAdapterFactory()
		if err != nil {
			return ble.EvidenceInput{}, nil, err
		}
		return ble.EvidenceInput{Name: "ble-live-capture"}, adapter, nil
	}
	if opts.inputPath == "" {
		return ble.EvidenceInput{}, nil, fmt.Errorf("pass --input for replay evidence or --live for live BLE")
	}
	data, err := os.ReadFile(opts.inputPath)
	if err != nil {
		return ble.EvidenceInput{}, nil, fmt.Errorf("loading replay evidence: %w", err)
	}
	input, err := ble.ParseEvidence(data)
	if err != nil {
		return ble.EvidenceInput{}, nil, err
	}
	return input, ble.NewReplayAdapter(input), nil
}

func evidenceWithEvents(input ble.EvidenceInput, events []ble.Event) ble.EvidenceInput {
	return ble.EvidenceInput{
		Name:                input.Name,
		DisplayName:         input.DisplayName,
		Identity:            input.Identity,
		Events:              events,
		Actions:             input.Actions,
		CommunityReferences: input.CommunityReferences,
		RedactionTerms:      append([]string(nil), input.RedactionTerms...),
	}
}

func writeEvidence(cmd *cobra.Command, evidence ble.EvidenceInput) error {
	data, err := FormatEvidence(evidence)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(data)
	return err
}

func FormatEvidence(evidence ble.EvidenceInput) ([]byte, error) {
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("format evidence JSON: %w", err)
	}
	return append(data, '\n'), nil
}

func MergeEvidence(inputs []ble.EvidenceInput, extraRedactionTerms ...string) ble.EvidenceInput {
	var merged ble.EvidenceInput
	eventIDs := map[string]bool{}
	actionIDs := map[string]bool{}
	referenceIDs := map[string]bool{}
	redactionTerms := map[string]bool{}

	for _, input := range inputs {
		if merged.Name == "" {
			merged.Name = input.Name
		}
		if merged.DisplayName == "" {
			merged.DisplayName = input.DisplayName
		}
		mergeIdentity(&merged.Identity, input.Identity)
		for _, term := range input.RedactionTerms {
			if term == "" || redactionTerms[term] {
				continue
			}
			redactionTerms[term] = true
			merged.RedactionTerms = append(merged.RedactionTerms, term)
		}
		for _, event := range input.Events {
			if skipSeen(event.ID, eventIDs) {
				continue
			}
			merged.Events = append(merged.Events, event)
		}
		for _, action := range input.Actions {
			if skipSeen(action.ID, actionIDs) {
				continue
			}
			merged.Actions = append(merged.Actions, action)
		}
		for _, ref := range input.CommunityReferences {
			if skipSeen(ref.ID, referenceIDs) {
				continue
			}
			merged.CommunityReferences = append(merged.CommunityReferences, ref)
		}
	}
	for _, term := range extraRedactionTerms {
		if term == "" || redactionTerms[term] {
			continue
		}
		redactionTerms[term] = true
		merged.RedactionTerms = append(merged.RedactionTerms, term)
	}

	return merged
}

func mergeIdentity(dst *devicespec.DeviceIdentity, src devicespec.DeviceIdentity) {
	if len(dst.AdvertisedNames) == 0 {
		dst.AdvertisedNames = append([]string(nil), src.AdvertisedNames...)
	}
	if dst.AddressPolicy == "" {
		dst.AddressPolicy = src.AddressPolicy
	}
	if len(dst.ManufacturerDataHints) == 0 {
		dst.ManufacturerDataHints = append([]string(nil), src.ManufacturerDataHints...)
	}
	if len(dst.GATTFingerprint) == 0 {
		dst.GATTFingerprint = append([]string(nil), src.GATTFingerprint...)
	}
	if dst.MatchStrength == "" {
		dst.MatchStrength = src.MatchStrength
	}
}

func skipSeen(id string, seen map[string]bool) bool {
	if id == "" {
		return false
	}
	if seen[id] {
		return true
	}
	seen[id] = true
	return false
}

func Execute(ctx context.Context, name string) error {
	cmd := NewRootCommand(name)
	cmd.SetContext(ctx)
	return cmd.Execute()
}

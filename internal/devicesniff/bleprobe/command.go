package bleprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicesniff/ble"
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
	cmd.AddCommand(newScanCmd())
	cmd.AddCommand(newInspectCmd())
	cmd.AddCommand(newReadCmd())
	cmd.AddCommand(newWriteCmd())
	cmd.AddCommand(newSubscribeCmd())
	cmd.AddCommand(newDoctorCmd(name))
	return cmd
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
				},
				HardwareCommands: []string{
					name + " scan --live --duration-ms 10000",
					name + " inspect --live --address ADDRESS",
					name + " read --live --address ADDRESS --service SERVICE_UUID --characteristic CHARACTERISTIC_UUID",
					name + " subscribe --live --address ADDRESS --service SERVICE_UUID --characteristic CHARACTERISTIC_UUID --duration-ms 10000",
				},
			}
			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return fmt.Errorf("format doctor report: %w", err)
			}
			_, err = cmd.OutOrStdout().Write(append(data, '\n'))
			return err
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
			devices, err := adapter.Scan(cmd.Context(), ble.ScanOptions{ServiceUUIDs: opts.serviceUUIDs, DurationMillis: opts.durationMillis})
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
				DurationMillis:     opts.durationMillis,
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

func addBackendFlags(cmd *cobra.Command, opts *probeOptions) {
	cmd.Flags().StringVar(&opts.inputPath, "input", "", "Path to replay evidence JSON")
	cmd.Flags().BoolVar(&opts.live, "live", false, "Use the live BLE adapter compiled with -tags ble_live")
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

func loadBackend(opts probeOptions) (ble.EvidenceInput, ble.Adapter, error) {
	if opts.live {
		adapter, err := ble.NewLiveAdapter()
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

func Execute(ctx context.Context, name string) error {
	cmd := NewRootCommand(name)
	cmd.SetContext(ctx)
	return cmd.Execute()
}

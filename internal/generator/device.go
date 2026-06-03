package generator

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicespec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

type DeviceGenerator struct {
	Spec      *devicespec.DeviceSpec
	OutputDir string
}

type deviceTemplateData struct {
	Spec           *devicespec.DeviceSpec
	Name           string
	CLIName        string
	MCPName        string
	ModulePath     string
	DisplayName    string
	CurrentYear    int
	StatusFields   []deviceStatusField
	Commands       []deviceCommandField
	AllCommands    []deviceCommandField
	HasCommands    bool
	HasSession     bool
	HasStore       bool
	InstallSection string
}

type deviceStatusField struct {
	Name               string
	CharacteristicUUID string
	Unit               string
	SampleCadence      string
	Store              bool
}

type deviceCommandField struct {
	Name               string
	CharacteristicUUID string
	Safety             string
	ValidationStatus   string
	PayloadHex         string
	EvidenceRefs       []string
	Callable           bool
	WithheldReason     string
}

// This drives the SKILL.md confirmation clause and must stay in lockstep with
// the emitted runtime gate `requiresPhysicalConfirmation`, which repeats the same
// safety set as raw string literals because the generated module cannot import
// devicespec. Adding a safety class here without updating that template branch
// would make the generated CLI gate a write the SKILL never documents (or vice
// versa).
func (f deviceCommandField) RequiresPhysicalConfirmation() bool {
	switch f.Safety {
	case devicespec.SafetyPhysicalEffect, devicespec.SafetyConfigurationRisk:
		return true
	default:
		return false
	}
}

func NewDevice(spec *devicespec.DeviceSpec, outputDir string) *DeviceGenerator {
	return &DeviceGenerator{Spec: spec, OutputDir: outputDir}
}

func (g *DeviceGenerator) Generate() error {
	if g.Spec == nil {
		return fmt.Errorf("device spec is required")
	}
	if err := g.Spec.Validate(); err != nil {
		return err
	}
	data := g.templateData()
	files := map[string]string{
		"go.mod": deviceGoModTemplate,
		filepath.Join("cmd", data.CLIName, "main.go"):        deviceMainTemplate,
		filepath.Join("internal", "cli", "root.go"):          deviceRootTemplate,
		filepath.Join("internal", "cliutil", "verifyenv.go"): "cliutil_verifyenv.go.tmpl",
		filepath.Join("internal", "device", "spec.go"):       deviceSpecTemplate,
		filepath.Join("internal", "device", "transport.go"):  deviceTransportTemplate,
		// BLE adapter seam: device-neutral interfaces (always compiled) plus the
		// tinygo live driver (build tag ble_live) and the pure-Go stub. The
		// default build links no BLE stack; -tags ble_live enables real control.
		filepath.Join("internal", "device", "ble.go"):      deviceBLETemplate,
		filepath.Join("internal", "device", "ble_live.go"): deviceBLELiveTemplate,
		filepath.Join("internal", "device", "ble_stub.go"): deviceBLEStubTemplate,
		"README.md": deviceReadmeTemplate,
		"SKILL.md":  deviceSkillTemplate,
		// MCP surface: a stdio MCP server that mirrors the Cobra tree via the
		// API-agnostic cobratree walker. The walker respects mcp:read-only and
		// mcp:hidden annotations, so each device CLI's own commands decide what an
		// agent can reach. The MCP binary execs the companion CLI (no BLE/CGO).
		filepath.Join("cmd", data.MCPName, "main.go"): deviceMCPMainTemplate,
		filepath.Join("internal", "mcp", "tools.go"):  deviceMCPToolsTemplate,
	}
	// The cobratree walker is API-agnostic and shared with the HTTP generator;
	// single-source the file set. device files are keyed output->template, so
	// invert the shared template->output manifest here.
	for tmpl, out := range cobratreeWalkerTemplateFiles() {
		files[out] = tmpl
	}
	if data.HasSession {
		files[filepath.Join("internal", "device", "session.go")] = deviceSessionTemplate
	}
	if data.HasStore {
		files[filepath.Join("internal", "device", "store.go")] = deviceStoreTemplate
	}
	for path, tmpl := range files {
		if strings.HasSuffix(tmpl, ".tmpl") {
			if err := g.renderEmbedded(path, tmpl, data); err != nil {
				return err
			}
		} else {
			if err := g.render(path, tmpl, data); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *DeviceGenerator) Validate() error {
	if _, err := runCommand(g.OutputDir, qualityGateTimeout, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy: %w", err)
	}
	if _, err := runCommand(g.OutputDir, qualityGateTimeout, "go", "build", "./..."); err != nil {
		return fmt.Errorf("go build ./...: %w", err)
	}
	return nil
}

func (g *DeviceGenerator) templateData() deviceTemplateData {
	name := g.Spec.Name
	displayName := g.Spec.DisplayName
	if displayName == "" {
		displayName = naming.HumanName(name)
	}
	statusFields := make([]deviceStatusField, 0, len(g.Spec.Capabilities.Telemetry))
	hasStore := false
	for _, field := range g.Spec.Capabilities.Telemetry {
		if field.Store {
			hasStore = true
		}
		statusFields = append(statusFields, deviceStatusField{
			Name:               field.Name,
			CharacteristicUUID: field.SourceCharacteristicUUID,
			Unit:               field.Unit,
			SampleCadence:      field.SampleCadence,
			Store:              field.Store,
		})
	}
	commands := make([]deviceCommandField, 0, len(g.Spec.Capabilities.Commands))
	allCommands := make([]deviceCommandField, 0, len(g.Spec.Capabilities.Commands))
	for _, command := range g.Spec.Capabilities.Commands {
		field := deviceCommandField{
			Name:               naming.FlagName(command.Name),
			CharacteristicUUID: command.CharacteristicUUID,
			Safety:             command.Safety,
			ValidationStatus:   command.ValidationStatus,
			PayloadHex:         hex.EncodeToString(command.Payload.Bytes),
			EvidenceRefs:       command.EvidenceRefs,
			Callable:           deviceCommandCallable(command),
			WithheldReason:     deviceCommandWithheldReason(command),
		}
		allCommands = append(allCommands, field)
		if field.Callable {
			commands = append(commands, field)
		}
	}
	return deviceTemplateData{
		Spec:         g.Spec,
		Name:         name,
		CLIName:      naming.CLI(name),
		MCPName:      naming.MCP(name),
		ModulePath:   naming.CLI(name),
		DisplayName:  displayName,
		CurrentYear:  time.Now().Year(),
		StatusFields: statusFields,
		Commands:     commands,
		AllCommands:  allCommands,
		HasCommands:  len(commands) > 0,
		HasSession:   g.Spec.Session.Mode == devicespec.SessionModeOptional || g.Spec.Session.Mode == devicespec.SessionModeRequired,
		HasStore:     hasStore,
		// Device specs carry no catalog category, so the canonical install block
		// uses the category-agnostic installer path — matching what the verify-skill
		// canonical-sections check expects (CanonicalSkillInstallSection(name, "")).
		InstallSection: CanonicalSkillInstallSection(name, ""),
	}
}

func deviceCommandWithheldReason(command devicespec.DeviceCommand) string {
	if deviceCommandCallable(command) {
		return ""
	}
	if command.ValidationStatus != devicespec.ValidationStatusObserved && command.ValidationStatus != devicespec.ValidationStatusReplayValidated {
		return "withheld: command is not observed or replay-validated"
	}
	switch command.Safety {
	case devicespec.SafetyUnknown, "":
		return "withheld: command safety is unknown"
	default:
		return "withheld: command is not callable in the generated replay runtime"
	}
}

func deviceCommandCallable(command devicespec.DeviceCommand) bool {
	if command.Safety == devicespec.SafetyUnknown || command.Safety == "" {
		return false
	}
	return command.ValidationStatus == devicespec.ValidationStatusObserved || command.ValidationStatus == devicespec.ValidationStatusReplayValidated
}

func (g *DeviceGenerator) render(relPath, tmplText string, data deviceTemplateData) error {
	tmpl, err := template.New(relPath).Funcs(template.FuncMap{
		"quote": func(value string) string { return fmt.Sprintf("%q", value) },
	}).Parse(tmplText)
	if err != nil {
		return fmt.Errorf("parse %s template: %w", relPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render %s: %w", relPath, err)
	}
	path := filepath.Join(g.OutputDir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, normalizeRendered(buf.Bytes(), relPath, relPath), 0o644)
}

func (g *DeviceGenerator) renderEmbedded(relPath, tmplName string, data deviceTemplateData) error {
	content, err := templateFS.ReadFile(path.Join("templates", tmplName))
	if err != nil {
		return fmt.Errorf("read %s template: %w", tmplName, err)
	}
	tmpl, err := template.New(tmplName).Funcs(template.FuncMap{
		"currentYear":     func() string { return strconv.Itoa(time.Now().Year()) },
		"copyrightHolder": func() string { return "contributors" },
		"envPrefix":       naming.EnvPrefix,
	}).Parse(string(content))
	if err != nil {
		return fmt.Errorf("parse %s template: %w", tmplName, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render %s: %w", relPath, err)
	}
	outPath := filepath.Join(g.OutputDir, relPath)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, normalizeRendered(buf.Bytes(), tmplName, relPath), 0o644)
}

const deviceGoModTemplate = `module {{.ModulePath}}

go 1.26.4

require (
	github.com/mark3labs/mcp-go v0.47.0
	github.com/spf13/cobra v1.9.1
	tinygo.org/x/bluetooth v0.15.0
)
`

const deviceMainTemplate = `// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.

package main

import (
	"os"

	"{{.ModulePath}}/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
`

const deviceRootTemplate = `// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.

package cli

import (
	"context"
	"encoding/json"
	"fmt"

{{- if .HasCommands}}
	"{{.ModulePath}}/internal/cliutil"
{{- end}}
	"{{.ModulePath}}/internal/device"
	"github.com/spf13/cobra"
)

type rootFlags struct {
	asJSON bool
{{- if .HasCommands}}
	dryRun bool
{{- end}}
{{- if .HasStore}}
	storePath string
{{- end}}
}

// novelCommands is an optional hook for hand-authored commands. It is nil by
// default (no extra commands). To extend this CLI WITHOUT editing generated
// files, add a file in package cli — it is preserved across regeneration — that
// sets this var from an init function:
//
//	func init() {
//		novelCommands = func(root *cobra.Command, flags *rootFlags) {
//			root.AddCommand(newMyCmd(flags))
//		}
//	}
var novelCommands func(root *cobra.Command, flags *rootFlags)

func RootCmd() *cobra.Command {
	var flags rootFlags
	return newRootCmd(&flags)
}

func Execute() error {
	return RootCmd().Execute()
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	return 1
}

func newRootCmd(flags *rootFlags) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "{{.CLIName}}",
		Short:        "Control {{.DisplayName}} over BLE",
		Long:         "Control {{.DisplayName}} over BLE using a generated device-native CLI surface.",
		SilenceUsage: true,
	}
	rootCmd.PersistentFlags().BoolVar(&flags.asJSON, "json", false, "Output as JSON")
	rootCmd.PersistentFlags().BoolVar(&flags.asJSON, "agent", false, "Output agent-friendly JSON")
{{- if .HasCommands}}
	rootCmd.PersistentFlags().BoolVar(&flags.dryRun, "dry-run", false, "Preview device writes without dispatching them")
{{- end}}
{{- if .HasStore}}
	rootCmd.PersistentFlags().StringVar(&flags.storePath, "store", "", "Telemetry store path (default: user cache)")
{{- end}}
	rootCmd.AddCommand(newCapabilitiesCmd(flags))
	rootCmd.AddCommand(newStatusCmd(flags, device.NewReplayTransport()))
{{- if .HasSession}}
	rootCmd.AddCommand(newSessionCmd(flags, device.NewReplaySession()))
{{- end}}
{{- if .HasStore}}
	rootCmd.AddCommand(newTelemetryCmd(flags, device.NewReplayTransport()))
{{- end}}
{{- range .Commands}}
	rootCmd.AddCommand(newDeviceCommandCmd(flags, device.NewReplayTransport(), device.CommandDefinition{Name: {{quote .Name}}, CharacteristicUUID: {{quote .CharacteristicUUID}}, Safety: {{quote .Safety}}, ValidationStatus: {{quote .ValidationStatus}}, PayloadHex: {{quote .PayloadHex}}}))
{{- end}}
	if novelCommands != nil {
		novelCommands(rootCmd, flags)
	}
	return rootCmd
}

func writeJSON(cmd *cobra.Command, value any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func newCapabilitiesCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "capabilities",
		Short: "Show generated BLE capability and safety metadata",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			summary := device.Capabilities()
			if flags.asJSON {
				return writeJSON(cmd, summary)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s capabilities\n", summary.Device)
			fmt.Fprintf(cmd.OutOrStdout(), "protocol: %s\n", summary.Protocol)
			fmt.Fprintf(cmd.OutOrStdout(), "session: %s\n", summary.SessionMode)
			for _, field := range summary.Telemetry {
				fmt.Fprintf(cmd.OutOrStdout(), "telemetry: %s via %s store=%v\n", field.Name, field.SourceCharacteristicUUID, field.Store)
			}
			for _, command := range summary.Commands {
				if command.Callable {
					fmt.Fprintf(cmd.OutOrStdout(), "callable command: %s safety=%s characteristic=%s\n", command.Name, command.Safety, command.CharacteristicUUID)
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "withheld command: %s safety=%s characteristic=%s reason=%s\n", command.Name, command.Safety, command.CharacteristicUUID, command.WithheldReason)
			}
			return nil
		},
	}
}

func newStatusCmd(flags *rootFlags, transport device.Transport) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Read replay-backed device status",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshot, err := transport.Status(cmd.Context())
			if err != nil {
				return err
			}
			if flags.asJSON {
				return writeJSON(cmd, snapshot)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s status\n", snapshot.Device)
			fmt.Fprintf(cmd.OutOrStdout(), "transport: %s\n", snapshot.Transport)
			fmt.Fprintf(cmd.OutOrStdout(), "session: %s\n", snapshot.SessionMode)
			for _, field := range device.StatusFields {
				value := snapshot.Telemetry[field.Name]
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %v\n", field.Name, value)
			}
			return nil
		},
	}
}

{{ if .HasCommands}}
func newDeviceCommandCmd(flags *rootFlags, transport device.Transport, definition device.CommandDefinition) *cobra.Command {
	var confirmPhysicalEffect bool
	command := &cobra.Command{
		Use:   definition.Name,
		Short: fmt.Sprintf("Replay %s against the device transport", definition.Name),
		RunE: func(cmd *cobra.Command, args []string) error {
			if requiresPhysicalConfirmation(definition) && !flags.dryRun && !confirmPhysicalEffect && !cliutil.IsVerifyEnv() {
				return fmt.Errorf("%s has safety class %s; pass --dry-run to preview or --confirm-physical-effect to replay it", definition.Name, definition.Safety)
			}
			result, err := transport.ExecuteCommand(cmd.Context(), definition, flags.dryRun)
			if err != nil {
				return err
			}
			if flags.asJSON {
				return writeJSON(cmd, result)
			}
			if result.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "would write %s to %s for %s\n", result.PayloadHex, result.CharacteristicUUID, result.Command)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "replayed %s via %s\n", result.Command, result.Transport)
			return nil
		},
	}
	if requiresPhysicalConfirmation(definition) {
		command.Flags().BoolVar(&confirmPhysicalEffect, "confirm-physical-effect", false, "Confirm replay of a physical-effect or configuration-risk device command")
	}
	return command
}

func requiresPhysicalConfirmation(definition device.CommandDefinition) bool {
	switch definition.Safety {
	case "physical-effect", "configuration-risk":
		return true
	default:
		return false
	}
}

{{ end}}
{{ if .HasStore}}
func newTelemetryCmd(flags *rootFlags, transport device.Transport) *cobra.Command {
	telemetryCmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Capture and read replay-backed telemetry samples",
	}
	telemetryCmd.AddCommand(newTelemetryCaptureCmd(flags, transport))
	telemetryCmd.AddCommand(newTelemetryLatestCmd(flags))
{{- if .HasSession}}
	telemetryCmd.AddCommand(newTelemetrySessionsCmd(flags))
{{- end}}
	return telemetryCmd
}

func newTelemetryCaptureCmd(flags *rootFlags, transport device.Transport) *cobra.Command {
	return &cobra.Command{
		Use:   "capture",
		Short: "Capture a replay-backed telemetry sample into the local store",
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshot, err := transport.Status(cmd.Context())
			if err != nil {
				return err
			}
			store, err := openTelemetryStore(flags)
			if err != nil {
				return err
			}
			samples, err := store.CaptureStatus(snapshot)
			if err != nil {
				return err
			}
			if flags.asJSON {
				return writeJSON(cmd, samples)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "captured %d telemetry sample(s)\n", len(samples))
			fmt.Fprintf(cmd.OutOrStdout(), "store: %s\n", store.Path())
			return nil
		},
	}
}

func newTelemetryLatestCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "latest",
		Short: "Read the latest locally stored telemetry samples",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openTelemetryStore(flags)
			if err != nil {
				return err
			}
			samples, err := store.Latest()
			if err != nil {
				return err
			}
			if flags.asJSON {
				return writeJSON(cmd, samples)
			}
			if len(samples) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no stored telemetry samples")
				fmt.Fprintf(cmd.OutOrStdout(), "store: %s\n", store.Path())
				return nil
			}
			for _, sample := range samples {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %v\n", sample.Field, sample.Value)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "store: %s\n", store.Path())
			return nil
		},
	}
}

{{ if .HasSession}}
func newTelemetrySessionsCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "Read locally stored BLE session summaries",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openTelemetryStore(flags)
			if err != nil {
				return err
			}
			summaries, err := store.SessionSummaries()
			if err != nil {
				return err
			}
			if flags.asJSON {
				return writeJSON(cmd, summaries)
			}
			if len(summaries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no stored session summaries")
				fmt.Fprintf(cmd.OutOrStdout(), "store: %s\n", store.SessionPath())
				return nil
			}
			for _, summary := range summaries {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s via %s\n", summary.ObservedAt, summary.State, summary.Transport)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "store: %s\n", store.SessionPath())
			return nil
		},
	}
}

{{ end}}
func openTelemetryStore(flags *rootFlags) (*device.TelemetryStore, error) {
	storePath := flags.storePath
	if storePath == "" {
		var err error
		storePath, err = device.DefaultTelemetryStorePath("{{.CLIName}}")
		if err != nil {
			return nil, err
		}
	}
	return device.OpenTelemetryStore(storePath)
}

{{ end}}
{{ if .HasSession}}
func newSessionCmd(flags *rootFlags, session device.Session) *cobra.Command {
	sessionCmd := &cobra.Command{
		Use:   "session",
		Short: "Manage the replay-backed local BLE session runtime",
	}
	sessionCmd.AddCommand(newSessionStatusCmd(flags, session))
	sessionCmd.AddCommand(newSessionStartCmd(flags, session))
	sessionCmd.AddCommand(newSessionStopCmd(flags, session))
	return sessionCmd
}

func newSessionStatusCmd(flags *rootFlags, session device.Session) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show generated BLE session requirements",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := session.Status(cmd.Context())
			if err != nil {
				return err
			}
			return writeSessionStatus(cmd, flags, status)
		},
	}
}

func newSessionStartCmd(flags *rootFlags, session device.Session) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the replay-backed local BLE session runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := session.Start(cmd.Context())
			if err != nil {
				return err
			}
{{- if .HasStore}}
			if err := captureSessionSummary(flags, status); err != nil {
				return err
			}
{{- end}}
			return writeSessionStatus(cmd, flags, status)
		},
	}
}

func newSessionStopCmd(flags *rootFlags, session device.Session) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the replay-backed local BLE session runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := session.Stop(cmd.Context())
			if err != nil {
				return err
			}
{{- if .HasStore}}
			if err := captureSessionSummary(flags, status); err != nil {
				return err
			}
{{- end}}
			return writeSessionStatus(cmd, flags, status)
		},
	}
}

{{ if .HasStore}}
func captureSessionSummary(flags *rootFlags, status device.SessionStatus) error {
	store, err := openTelemetryStore(flags)
	if err != nil {
		return err
	}
	_, err = store.CaptureSession(status)
	return err
}

{{ end}}
func writeSessionStatus(cmd *cobra.Command, flags *rootFlags, status device.SessionStatus) error {
	if flags.asJSON {
		return writeJSON(cmd, status)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s session\n", status.Device)
	fmt.Fprintf(cmd.OutOrStdout(), "state: %s\n", status.State)
	fmt.Fprintf(cmd.OutOrStdout(), "mode: %s\n", status.Mode)
	fmt.Fprintf(cmd.OutOrStdout(), "transport: %s\n", status.Transport)
	fmt.Fprintf(cmd.OutOrStdout(), "endpoint: %s %s\n", status.Endpoint.Kind, status.Endpoint.Path)
	fmt.Fprintf(cmd.OutOrStdout(), "runtime: %s\n", status.RuntimeDir)
	fmt.Fprintf(cmd.OutOrStdout(), "token: %v\n", status.TokenPresent)
	fmt.Fprintf(cmd.OutOrStdout(), "one-shot fallback: %v\n", status.OneShotFallback)
	fmt.Fprintf(cmd.OutOrStdout(), "reconnect: %v\n", status.Reconnect)
	fmt.Fprintf(cmd.OutOrStdout(), "notification stream: %v\n", status.NotificationStream)
	for _, reason := range status.Reasons {
		fmt.Fprintf(cmd.OutOrStdout(), "reason: %s\n", reason)
	}
	if status.Detail != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "detail: %s\n", status.Detail)
	}
	return nil
}

{{ end}}
func ExecuteWithContext(ctx context.Context) error {
	cmd := RootCmd()
	cmd.SetContext(ctx)
	return cmd.Execute()
}
`

const deviceSpecTemplate = `// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.

package device

const (
	Name                = {{quote .Name}}
	DisplayName         = {{quote .DisplayName}}
	Protocol            = "ble"
	SessionMode         = {{quote .Spec.Session.Mode}}
	SessionOneShotFallback = {{if .Spec.Session.OneShotFallback}}true{{else}}false{{end}}
{{- if .HasSession}}
	SessionReconnect = {{if .Spec.Session.Reconnect}}true{{else}}false{{end}}
	SessionNotificationStream = {{if .Spec.Session.NotificationStream}}true{{else}}false{{end}}
{{- end}}
)

{{- if .HasSession}}
var SessionReasons = []string{
{{- range .Spec.Session.Reasons}}
	{{quote .}},
{{- end}}
}
{{- end}}

type StatusField struct {
	Name                     string ` + "`json:\"name\"`" + `
	SourceCharacteristicUUID string ` + "`json:\"source_characteristic_uuid\"`" + `
	Unit                     string ` + "`json:\"unit,omitempty\"`" + `
	SampleCadence            string ` + "`json:\"sample_cadence,omitempty\"`" + `
	Store                    bool   ` + "`json:\"store,omitempty\"`" + `
}

var StatusFields = []StatusField{
{{- range .StatusFields}}
	{Name: {{quote .Name}}, SourceCharacteristicUUID: {{quote .CharacteristicUUID}}, Unit: {{quote .Unit}}, SampleCadence: {{quote .SampleCadence}}, Store: {{if .Store}}true{{else}}false{{end}}},
{{- end}}
}

type CommandDefinition struct {
	Name               string ` + "`json:\"name\"`" + `
	CharacteristicUUID string ` + "`json:\"characteristic_uuid\"`" + `
	Safety             string ` + "`json:\"safety\"`" + `
	ValidationStatus   string ` + "`json:\"validation_status,omitempty\"`" + `
	PayloadHex         string ` + "`json:\"payload_hex\"`" + `
	EvidenceRefs       []string ` + "`json:\"evidence_refs,omitempty\"`" + `
}

var CommandDefinitions = []CommandDefinition{
{{- range .Commands}}
	{Name: {{quote .Name}}, CharacteristicUUID: {{quote .CharacteristicUUID}}, Safety: {{quote .Safety}}, ValidationStatus: {{quote .ValidationStatus}}, PayloadHex: {{quote .PayloadHex}}, EvidenceRefs: []string{ {{- range .EvidenceRefs}}{{quote .}}, {{- end}} }},
{{- end}}
}

type CommandCapability struct {
	Name               string   ` + "`json:\"name\"`" + `
	CharacteristicUUID string   ` + "`json:\"characteristic_uuid\"`" + `
	Safety             string   ` + "`json:\"safety\"`" + `
	ValidationStatus   string   ` + "`json:\"validation_status,omitempty\"`" + `
	EvidenceRefs       []string ` + "`json:\"evidence_refs,omitempty\"`" + `
	Callable           bool     ` + "`json:\"callable\"`" + `
	WithheldReason     string   ` + "`json:\"withheld_reason,omitempty\"`" + `
}

var CommandCapabilities = []CommandCapability{
{{- range .AllCommands}}
	{Name: {{quote .Name}}, CharacteristicUUID: {{quote .CharacteristicUUID}}, Safety: {{quote .Safety}}, ValidationStatus: {{quote .ValidationStatus}}, EvidenceRefs: []string{ {{- range .EvidenceRefs}}{{quote .}}, {{- end}} }, Callable: {{if .Callable}}true{{else}}false{{end}}, WithheldReason: {{quote .WithheldReason}}},
{{- end}}
}

type CapabilitySummary struct {
	Device      string              ` + "`json:\"device\"`" + `
	Protocol    string              ` + "`json:\"protocol\"`" + `
	SessionMode string              ` + "`json:\"session_mode\"`" + `
	Telemetry   []StatusField       ` + "`json:\"telemetry\"`" + `
	Commands    []CommandCapability ` + "`json:\"commands\"`" + `
}

func Capabilities() CapabilitySummary {
	return CapabilitySummary{
		Device:      DisplayName,
		Protocol:    Protocol,
		SessionMode: SessionMode,
		Telemetry:   append([]StatusField(nil), StatusFields...),
		Commands:    append([]CommandCapability(nil), CommandCapabilities...),
	}
}
`

const deviceTransportTemplate = `// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.

package device

import (
	"context"
	"time"

	"{{.ModulePath}}/internal/cliutil"
)

type StatusSnapshot struct {
	Device      string         ` + "`json:\"device\"`" + `
	Transport   string         ` + "`json:\"transport\"`" + `
	SessionMode string         ` + "`json:\"session_mode\"`" + `
	ObservedAt  string         ` + "`json:\"observed_at\"`" + `
	Telemetry   map[string]any ` + "`json:\"telemetry\"`" + `
}

type CommandResult struct {
	Command            string ` + "`json:\"command\"`" + `
	Transport          string ` + "`json:\"transport\"`" + `
	CharacteristicUUID string ` + "`json:\"characteristic_uuid\"`" + `
	PayloadHex         string ` + "`json:\"payload_hex\"`" + `
	Safety             string ` + "`json:\"safety\"`" + `
	ValidationStatus   string ` + "`json:\"validation_status,omitempty\"`" + `
	DryRun             bool   ` + "`json:\"dry_run\"`" + `
	VerifyNoop         bool   ` + "`json:\"verify_noop,omitempty\"`" + `
	Reason             string ` + "`json:\"reason,omitempty\"`" + `
}

type Transport interface {
	Status(context.Context) (StatusSnapshot, error)
	ExecuteCommand(context.Context, CommandDefinition, bool) (CommandResult, error)
}

type ReplayTransport struct{}

func NewReplayTransport() *ReplayTransport {
	return &ReplayTransport{}
}

func (t *ReplayTransport) Status(ctx context.Context) (StatusSnapshot, error) {
	telemetry := map[string]any{}
	for _, field := range StatusFields {
		telemetry[field.Name] = map[string]any{
			"source_characteristic_uuid": field.SourceCharacteristicUUID,
			"sample_cadence":             field.SampleCadence,
			"value":                      nil,
		}
	}
	return StatusSnapshot{
		Device:      DisplayName,
		Transport:   "replay",
		SessionMode: SessionMode,
		ObservedAt:  time.Now().UTC().Format(time.RFC3339),
		Telemetry:   telemetry,
	}, nil
}

func (t *ReplayTransport) ExecuteCommand(ctx context.Context, command CommandDefinition, dryRun bool) (CommandResult, error) {
	verifyNoop := cliutil.IsVerifyEnv()
	return CommandResult{
		Command:            command.Name,
		Transport:          commandTransportName(verifyNoop),
		CharacteristicUUID: command.CharacteristicUUID,
		PayloadHex:         command.PayloadHex,
		Safety:             command.Safety,
		ValidationStatus:   command.ValidationStatus,
		DryRun:             dryRun || verifyNoop,
		VerifyNoop:         verifyNoop,
		Reason:             commandNoopReason(verifyNoop),
	}, nil
}

func commandTransportName(verifyNoop bool) string {
	if verifyNoop {
		return "verify-replay"
	}
	return "replay"
}

func commandNoopReason(verifyNoop bool) string {
	if verifyNoop {
		return "verify_short_circuit"
	}
	return ""
}
`

// deviceBLETemplate emits the device-neutral BLE adapter seam (always compiled).
// The live tinygo backend (deviceBLELiveTemplate, build tag ble_live) and the
// pure-Go stub (deviceBLEStubTemplate) provide newBLEBackend + liveCompiled, so
// the default build links no BLE stack and stays CGO-free.
const deviceBLETemplate = `// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.

package device

import (
	"context"
	"errors"
)

// Advert is a discovered BLE advertisement.
type Advert struct {
	Address      string   ` + "`json:\"address\"`" + `
	Name         string   ` + "`json:\"name\"`" + `
	RSSI         int      ` + "`json:\"rssi\"`" + `
	ServiceUUIDs []string ` + "`json:\"service_uuids,omitempty\"`" + `
}

// ErrLiveUnavailable is returned by every live operation when the binary was
// built without the BLE backend (the default, pure-Go, no-CGO build).
var ErrLiveUnavailable = errors.New("live BLE is not compiled into this binary; rebuild with: go build -tags ble_live ./... (CGO required)")

// ErrDeviceNotFound is returned when a scan finds no matching device.
var ErrDeviceNotFound = errors.New("no matching device found; power it on and close any official app (only one BLE client can connect at a time)")

// bleBackend is the device-neutral BLE central surface. The live implementation
// (tinygo bluetooth, build tag ble_live) and the pure-Go stub live in separate
// build-tagged files; both provide newBLEBackend and the liveCompiled constant.
type bleBackend interface {
	Scan(ctx context.Context, serviceUUIDs []string) ([]Advert, error)
	Connect(ctx context.Context, address string, serviceUUIDs []string) (bleLink, error)
}

// bleLink is a connected device. Write/Read/Subscribe address GATT
// characteristics by UUID, so one connection serves every command and telemetry
// stream the device spec declares.
type bleLink interface {
	Write(characteristicUUID string, payload []byte) error
	Read(characteristicUUID string) ([]byte, error)
	Subscribe(characteristicUUID string, handler func([]byte)) error
	Close() error
}

// LiveAvailable reports whether a live BLE backend is compiled into this binary
// (true only when built with -tags ble_live).
func LiveAvailable() bool { return liveCompiled }
`

const deviceBLELiveTemplate = `//go:build ble_live

// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.
//
// Live BLE backend: binds the device-neutral bleBackend/bleLink seam to a real
// adapter via tinygo.org/x/bluetooth. Compiled only with -tags ble_live and
// requires CGO (CoreBluetooth on macOS, BlueZ/D-Bus on Linux, WinRT on Windows).
// The default build uses ble_stub.go and links no BLE stack.

package device

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tinyble "tinygo.org/x/bluetooth"
)

// liveCompiled is true: the real BLE backend is linked in.
const liveCompiled = true

func newBLEBackend() (bleBackend, error) {
	adapter := tinyble.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("enable BLE adapter (check OS Bluetooth permission for your terminal): %w", err)
	}
	return &liveBackend{adapter: adapter}, nil
}

type liveBackend struct {
	adapter *tinyble.Adapter
}

func (b *liveBackend) Scan(ctx context.Context, serviceUUIDs []string) ([]Advert, error) {
	filters, err := parseUUIDs(serviceUUIDs)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err // don't start a scan with an already-expired context
	}
	dur := 8 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d > 0 {
			dur = d
		}
	}
	var (
		mu   sync.Mutex
		seen = map[string]Advert{}
		done = make(chan struct{})
		once sync.Once
	)
	finish := func() {
		once.Do(func() {
			_ = b.adapter.StopScan()
			close(done)
		})
	}
	timer := time.NewTimer(dur)
	defer timer.Stop()
	go func() {
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		finish()
	}()
	scanErr := b.adapter.Scan(func(_ *tinyble.Adapter, result tinyble.ScanResult) {
		if !matchesAnyService(result, filters) {
			return
		}
		mu.Lock()
		seen[result.Address.String()] = Advert{
			Address:      result.Address.String(),
			Name:         result.LocalName(),
			RSSI:         int(result.RSSI),
			ServiceUUIDs: serviceUUIDs,
		}
		mu.Unlock()
	})
	finish()
	<-done
	if scanErr != nil {
		return nil, fmt.Errorf("scan: %w", scanErr)
	}
	mu.Lock()
	defer mu.Unlock()
	out := make([]Advert, 0, len(seen))
	for _, a := range seen {
		out = append(out, a)
	}
	return out, nil
}

func (b *liveBackend) Connect(ctx context.Context, address string, serviceUUIDs []string) (bleLink, error) {
	timeout := 20 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d > 0 {
			timeout = d
		}
	}
	var addr tinyble.Address
	addr.Set(address)
	dev, err := b.adapter.Connect(addr, tinyble.ConnectionParams{
		ConnectionTimeout: tinyble.NewDuration(timeout),
	})
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", address, err)
	}
	filters, err := parseUUIDs(serviceUUIDs)
	if err != nil {
		_ = dev.Disconnect()
		return nil, err
	}
	services, err := dev.DiscoverServices(filters)
	if err != nil {
		_ = dev.Disconnect()
		return nil, fmt.Errorf("discover services: %w", err)
	}
	chars := map[string]*tinyble.DeviceCharacteristic{}
	for i := range services {
		// nil discovers every characteristic of the service.
		discovered, err := services[i].DiscoverCharacteristics(nil)
		if err != nil {
			_ = dev.Disconnect()
			return nil, fmt.Errorf("discover characteristics: %w", err)
		}
		for j := range discovered {
			chars[normalizeUUID(discovered[j].UUID().String())] = &discovered[j]
		}
	}
	return &liveLink{dev: dev, chars: chars}, nil
}

type liveLink struct {
	dev   tinyble.Device
	chars map[string]*tinyble.DeviceCharacteristic
}

func (l *liveLink) char(characteristicUUID string) (*tinyble.DeviceCharacteristic, error) {
	c, ok := l.chars[normalizeUUID(characteristicUUID)]
	if !ok {
		return nil, fmt.Errorf("characteristic %s not found on device", characteristicUUID)
	}
	return c, nil
}

func (l *liveLink) Write(characteristicUUID string, payload []byte) error {
	c, err := l.char(characteristicUUID)
	if err != nil {
		return err
	}
	// Prefer write-without-response; fall back to write-with-response for
	// characteristics that only permit the latter.
	if _, err := c.WriteWithoutResponse(payload); err == nil {
		return nil
	}
	_, err = c.Write(payload)
	return err
}

func (l *liveLink) Read(characteristicUUID string) ([]byte, error) {
	c, err := l.char(characteristicUUID)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 512)
	n, err := c.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (l *liveLink) Subscribe(characteristicUUID string, handler func([]byte)) error {
	c, err := l.char(characteristicUUID)
	if err != nil {
		return err
	}
	return c.EnableNotifications(handler)
}

func (l *liveLink) Close() error {
	return l.dev.Disconnect()
}

func parseUUIDs(uuids []string) ([]tinyble.UUID, error) {
	out := make([]tinyble.UUID, 0, len(uuids))
	for _, u := range uuids {
		parsed, err := tinyble.ParseUUID(u)
		if err != nil {
			return nil, fmt.Errorf("parse uuid %q: %w", u, err)
		}
		out = append(out, parsed)
	}
	return out, nil
}

func matchesAnyService(result tinyble.ScanResult, filters []tinyble.UUID) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if result.HasServiceUUID(f) {
			return true
		}
	}
	return false
}

func normalizeUUID(u string) string {
	return strings.ToLower(strings.TrimSpace(u))
}
`

const deviceBLEStubTemplate = `//go:build !ble_live

// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.
//
// Default pure-Go build: no BLE stack is linked, so the binary builds without
// CGO and runs verify/dogfood without touching hardware. Rebuild with
// -tags ble_live (CGO required) for real BLE control.

package device

// liveCompiled is false in the default build: no BLE backend is linked.
const liveCompiled = false

func newBLEBackend() (bleBackend, error) {
	return nil, ErrLiveUnavailable
}
`

const deviceSessionTemplate = `// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.

package device

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const sessionCLIName = "{{.CLIName}}"

type SessionEndpoint struct {
	Kind string ` + "`json:\"kind\"`" + `
	Path string ` + "`json:\"path\"`" + `
}

type SessionStatus struct {
	Device             string          ` + "`json:\"device\"`" + `
	Mode               string          ` + "`json:\"mode\"`" + `
	State              string          ` + "`json:\"state\"`" + `
	Transport          string          ` + "`json:\"transport\"`" + `
	Endpoint           SessionEndpoint ` + "`json:\"endpoint\"`" + `
	RuntimeDir         string          ` + "`json:\"runtime_dir\"`" + `
	TokenPresent       bool            ` + "`json:\"token_present\"`" + `
	ObservedAt         string          ` + "`json:\"observed_at\"`" + `
	Reasons            []string        ` + "`json:\"reasons,omitempty\"`" + `
	OneShotFallback    bool            ` + "`json:\"one_shot_fallback\"`" + `
	Reconnect          bool            ` + "`json:\"reconnect\"`" + `
	NotificationStream bool            ` + "`json:\"notification_stream\"`" + `
	Detail             string          ` + "`json:\"detail,omitempty\"`" + `
}

type Session interface {
	Start(context.Context) (SessionStatus, error)
	Status(context.Context) (SessionStatus, error)
	Stop(context.Context) (SessionStatus, error)
}

type ReplaySession struct {
	cliName string
}

func NewReplaySession() *ReplaySession {
	return &ReplaySession{cliName: sessionCLIName}
}

func (s *ReplaySession) Start(ctx context.Context) (SessionStatus, error) {
	runtimeDir, err := s.runtimeDir()
	if err != nil {
		return SessionStatus{}, err
	}
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return SessionStatus{}, fmt.Errorf("create session runtime dir: %w", err)
	}

	lockPath := s.lockPath(runtimeDir)
	lockFile, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return s.statusFromDisk("running", "existing replay session lock is active")
		}
		return SessionStatus{}, fmt.Errorf("create session lock: %w", err)
	}
	defer lockFile.Close()

	token, err := generateSessionToken()
	if err != nil {
		_ = os.Remove(lockPath)
		return SessionStatus{}, err
	}
	if err := os.WriteFile(s.tokenPath(runtimeDir), []byte(token+"\n"), 0o600); err != nil {
		_ = os.Remove(lockPath)
		return SessionStatus{}, fmt.Errorf("write session token: %w", err)
	}

	now := time.Now().UTC()
	record := sessionRecord{
		PID:       os.Getpid(),
		StartedAt: now.Format(time.RFC3339),
		Endpoint:  s.endpoint(runtimeDir),
	}
	if err := json.NewEncoder(lockFile).Encode(record); err != nil {
		_ = os.Remove(lockPath)
		_ = os.Remove(s.tokenPath(runtimeDir))
		return SessionStatus{}, fmt.Errorf("write session lock: %w", err)
	}
	if err := s.writeState(runtimeDir, "running", "replay session runtime started; local IPC endpoint reserved for generated clients"); err != nil {
		_ = os.Remove(lockPath)
		_ = os.Remove(s.tokenPath(runtimeDir))
		return SessionStatus{}, err
	}
	return s.status(runtimeDir, "running", "replay session runtime started; local IPC endpoint reserved for generated clients"), nil
}

func (s *ReplaySession) Status(ctx context.Context) (SessionStatus, error) {
	runtimeDir, err := s.runtimeDir()
	if err != nil {
		return SessionStatus{}, err
	}
	if _, err := os.Stat(s.lockPath(runtimeDir)); err == nil {
		return s.statusFromDisk("running", "replay session runtime is running")
	} else if !os.IsNotExist(err) {
		return SessionStatus{}, fmt.Errorf("stat session lock: %w", err)
	}
	return s.status(runtimeDir, "not-running", "no replay session lock is active"), nil
}

func (s *ReplaySession) Stop(ctx context.Context) (SessionStatus, error) {
	runtimeDir, err := s.runtimeDir()
	if err != nil {
		return SessionStatus{}, err
	}
	if err := os.Remove(s.lockPath(runtimeDir)); err != nil && !os.IsNotExist(err) {
		return SessionStatus{}, fmt.Errorf("remove session lock: %w", err)
	}
	if err := os.Remove(s.tokenPath(runtimeDir)); err != nil && !os.IsNotExist(err) {
		return SessionStatus{}, fmt.Errorf("remove session token: %w", err)
	}
	if err := os.Remove(s.statePath(runtimeDir)); err != nil && !os.IsNotExist(err) {
		return SessionStatus{}, fmt.Errorf("remove session state: %w", err)
	}
	return s.status(runtimeDir, "stopped", "replay session runtime stopped"), nil
}

type sessionRecord struct {
	PID       int             ` + "`json:\"pid\"`" + `
	StartedAt string          ` + "`json:\"started_at\"`" + `
	Endpoint  SessionEndpoint ` + "`json:\"endpoint\"`" + `
}

type sessionStateRecord struct {
	State      string ` + "`json:\"state\"`" + `
	Detail     string ` + "`json:\"detail\"`" + `
	ObservedAt string ` + "`json:\"observed_at\"`" + `
}

func (s *ReplaySession) runtimeDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(cacheDir, s.cliName, "session"), nil
}

func (s *ReplaySession) endpoint(runtimeDir string) SessionEndpoint {
	if runtime.GOOS == "windows" {
		return SessionEndpoint{Kind: "windows-named-pipe", Path: ` + "`\\\\.\\pipe\\`" + ` + sanitizePipeName(s.cliName) + "-session"}
	}
	return SessionEndpoint{Kind: "unix-socket", Path: filepath.Join(runtimeDir, "session.sock")}
}

func (s *ReplaySession) lockPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "session.lock")
}

func (s *ReplaySession) tokenPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "capability.token")
}

func (s *ReplaySession) statePath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "state.json")
}

func (s *ReplaySession) writeState(runtimeDir, state, detail string) error {
	record := sessionStateRecord{
		State:      state,
		Detail:     detail,
		ObservedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session state: %w", err)
	}
	if err := os.WriteFile(s.statePath(runtimeDir), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write session state: %w", err)
	}
	return nil
}

func (s *ReplaySession) statusFromDisk(fallbackState, fallbackDetail string) (SessionStatus, error) {
	runtimeDir, err := s.runtimeDir()
	if err != nil {
		return SessionStatus{}, err
	}
	state := fallbackState
	detail := fallbackDetail
	if data, err := os.ReadFile(s.statePath(runtimeDir)); err == nil {
		var record sessionStateRecord
		if err := json.Unmarshal(data, &record); err == nil {
			if record.State != "" {
				state = record.State
			}
			if record.Detail != "" {
				detail = record.Detail
			}
		}
	}
	return s.status(runtimeDir, state, detail), nil
}

func (s *ReplaySession) status(runtimeDir, state, detail string) SessionStatus {
	return SessionStatus{
		Device:             DisplayName,
		Mode:               SessionMode,
		State:              state,
		Transport:          "replay",
		Endpoint:           s.endpoint(runtimeDir),
		RuntimeDir:         runtimeDir,
		TokenPresent:       fileExists(s.tokenPath(runtimeDir)),
		ObservedAt:         time.Now().UTC().Format(time.RFC3339),
		Reasons:            append([]string(nil), SessionReasons...),
		OneShotFallback:    SessionOneShotFallback,
		Reconnect:          SessionReconnect,
		NotificationStream: SessionNotificationStream,
		Detail:             detail,
	}
}

func generateSessionToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sanitizePipeName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "device-session-" + strconv.Itoa(os.Getpid())
	}
	return out
}
`

const deviceStoreTemplate = `// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.

package device

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type TelemetrySample struct {
	Device                   string ` + "`json:\"device\"`" + `
	Field                    string ` + "`json:\"field\"`" + `
	ObservedAt               string ` + "`json:\"observed_at\"`" + `
	SourceCharacteristicUUID string ` + "`json:\"source_characteristic_uuid\"`" + `
	SampleCadence            string ` + "`json:\"sample_cadence,omitempty\"`" + `
	Unit                     string ` + "`json:\"unit,omitempty\"`" + `
	Value                    any    ` + "`json:\"value\"`" + `
}

{{- if .HasSession}}
type SessionSummary struct {
	Device             string   ` + "`json:\"device\"`" + `
	State              string   ` + "`json:\"state\"`" + `
	Mode               string   ` + "`json:\"mode\"`" + `
	Transport          string   ` + "`json:\"transport\"`" + `
	RuntimeDir         string   ` + "`json:\"runtime_dir,omitempty\"`" + `
	EndpointKind       string   ` + "`json:\"endpoint_kind,omitempty\"`" + `
	EndpointPath       string   ` + "`json:\"endpoint_path,omitempty\"`" + `
	TokenPresent       bool     ` + "`json:\"token_present\"`" + `
	ObservedAt         string   ` + "`json:\"observed_at\"`" + `
	Reasons            []string ` + "`json:\"reasons,omitempty\"`" + `
	OneShotFallback    bool     ` + "`json:\"one_shot_fallback\"`" + `
	Reconnect          bool     ` + "`json:\"reconnect\"`" + `
	NotificationStream bool     ` + "`json:\"notification_stream\"`" + `
	Detail             string   ` + "`json:\"detail,omitempty\"`" + `
}

{{- end}}
type TelemetryStore struct {
	path string
}

func DefaultTelemetryStorePath(cliName string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(cacheDir, cliName, "telemetry.jsonl"), nil
}

func OpenTelemetryStore(path string) (*TelemetryStore, error) {
	if path == "" {
		return nil, fmt.Errorf("telemetry store path is required")
	}
	return &TelemetryStore{path: path}, nil
}

func (s *TelemetryStore) Path() string {
	return s.path
}

{{- if .HasSession}}
func (s *TelemetryStore) SessionPath() string {
	return filepath.Join(filepath.Dir(s.path), "session-summaries.jsonl")
}

{{- end}}
func (s *TelemetryStore) CaptureStatus(snapshot StatusSnapshot) ([]TelemetrySample, error) {
	observedAt := snapshot.ObservedAt
	if observedAt == "" {
		observedAt = time.Now().UTC().Format(time.RFC3339)
	}
	samples := []TelemetrySample{}
	for _, field := range StatusFields {
		if !field.Store {
			continue
		}
		value := snapshot.Telemetry[field.Name]
		if wrapped, ok := value.(map[string]any); ok {
			if inner, ok := wrapped["value"]; ok {
				value = inner
			}
		}
		samples = append(samples, TelemetrySample{
			Device:                   snapshot.Device,
			Field:                    field.Name,
			ObservedAt:               observedAt,
			SourceCharacteristicUUID: field.SourceCharacteristicUUID,
			SampleCadence:            field.SampleCadence,
			Unit:                     field.Unit,
			Value:                    value,
		})
	}
	if len(samples) == 0 {
		return samples, nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return nil, fmt.Errorf("create telemetry store dir: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open telemetry store: %w", err)
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	for _, sample := range samples {
		if err := enc.Encode(sample); err != nil {
			return nil, fmt.Errorf("write telemetry sample: %w", err)
		}
	}
	return samples, nil
}

{{ if .HasSession}}
func (s *TelemetryStore) CaptureSession(status SessionStatus) (SessionSummary, error) {
	summary := SessionSummary{
		Device:             status.Device,
		State:              status.State,
		Mode:               status.Mode,
		Transport:          status.Transport,
		RuntimeDir:         status.RuntimeDir,
		EndpointKind:       status.Endpoint.Kind,
		EndpointPath:       status.Endpoint.Path,
		TokenPresent:       status.TokenPresent,
		ObservedAt:         status.ObservedAt,
		Reasons:            append([]string(nil), status.Reasons...),
		OneShotFallback:    status.OneShotFallback,
		Reconnect:          status.Reconnect,
		NotificationStream: status.NotificationStream,
		Detail:             status.Detail,
	}
	if summary.ObservedAt == "" {
		summary.ObservedAt = time.Now().UTC().Format(time.RFC3339)
	}
	path := s.SessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return SessionSummary{}, fmt.Errorf("create session summary dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return SessionSummary{}, fmt.Errorf("open session summary store: %w", err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(summary); err != nil {
		return SessionSummary{}, fmt.Errorf("write session summary: %w", err)
	}
	return summary, nil
}

func (s *TelemetryStore) SessionSummaries() ([]SessionSummary, error) {
	file, err := os.Open(s.SessionPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open session summary store: %w", err)
	}
	defer file.Close()
	var summaries []SessionSummary
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var summary SessionSummary
		if err := json.Unmarshal(scanner.Bytes(), &summary); err != nil {
			return nil, fmt.Errorf("decode session summary: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read session summary store: %w", err)
	}
	return summaries, nil
}

{{ end}}
func (s *TelemetryStore) Latest() ([]TelemetrySample, error) {
	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open telemetry store: %w", err)
	}
	defer file.Close()
	latestByField := map[string]TelemetrySample{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var sample TelemetrySample
		if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil {
			return nil, fmt.Errorf("decode telemetry sample: %w", err)
		}
		latestByField[sample.Field] = sample
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read telemetry store: %w", err)
	}
	samples := make([]TelemetrySample, 0, len(latestByField))
	for _, sample := range latestByField {
		samples = append(samples, sample)
	}
	sort.Slice(samples, func(i, j int) bool {
		return samples[i].Field < samples[j].Field
	})
	return samples, nil
}
`

const deviceReadmeTemplate = `# {{.DisplayName}} CLI

Generated by the Printing Press from a BLE device spec.

This CLI is device-native: commands refer to BLE device capabilities rather than HTTP endpoints. The first generated runtime uses replay transport so verification does not connect to or actuate a physical device.

## Commands

- ` + "`{{.CLIName}} status --json`" + ` prints replay-backed status fields.
- ` + "`{{.CLIName}} capabilities --json`" + ` prints callable and withheld BLE capabilities with safety metadata.
{{- range .Commands}}
- ` + "`{{$.CLIName}} {{.Name}} --dry-run --json`" + ` previews the {{.Name}} BLE write.
{{- end}}
{{- if .HasSession}}
- ` + "`{{.CLIName}} session start --json`" + ` creates the local replay session runtime lock, token, and endpoint metadata.
- ` + "`{{.CLIName}} session status --json`" + ` prints the local replay session runtime state.
{{- end}}
{{- if .HasStore}}
- ` + "`{{.CLIName}} telemetry capture --json`" + ` stores replay-backed telemetry samples locally.
- ` + "`{{.CLIName}} telemetry latest --json`" + ` reads the latest locally stored telemetry samples.
{{- if .HasSession}}
- ` + "`{{.CLIName}} telemetry sessions --json`" + ` reads locally stored BLE session summaries.
{{- end}}
{{- end}}

## MCP server

` + "`{{.MCPName}}`" + ` is a stdio MCP server that mirrors this CLI's commands as agent tools. It execs ` + "`{{.CLIName}}`" + ` (no BLE dependency of its own), so it builds anywhere. Read commands are exposed with ` + "`readOnlyHint`" + `; commands a device cannot safely expose to an agent are hidden via the ` + "`mcp:hidden`" + ` annotation. Point an MCP host at the ` + "`{{.MCPName}}`" + ` binary.
`

const deviceSkillTemplate = `---
name: {{.Name}}
description: Control {{.DisplayName}} through the generated BLE device CLI.
---

{{.InstallSection}}
Use ` + "`{{.CLIName}} capabilities --json`" + ` to inspect callable and withheld BLE capabilities, including safety classes and evidence refs. Use ` + "`{{.CLIName}} status --json`" + ` to inspect replay-backed status output.{{range .Commands}} Use ` + "`{{$.CLIName}} {{.Name}} --dry-run --json`" + ` to preview the {{.Name}} write.{{if .RequiresPhysicalConfirmation}} To replay it outside verify mode, pass ` + "`--confirm-physical-effect`" + ` after checking the dry-run output.{{end}}{{end}}{{if .HasSession}} Use ` + "`{{.CLIName}} session start --json`" + ` and ` + "`{{.CLIName}} session status --json`" + ` to inspect the local replay session runtime, including lock, capability-token, and endpoint metadata.{{else}} Live BLE control and optional session IPC are generated only when device-session support is enabled by the device spec.{{end}}{{if .HasStore}} Use ` + "`{{.CLIName}} telemetry capture --json`" + ` and ` + "`{{.CLIName}} telemetry latest --json`" + ` for the local telemetry store scaffold.{{if .HasSession}} Use ` + "`{{.CLIName}} telemetry sessions --json`" + ` to inspect stored BLE session summaries.{{end}}{{end}}
`

const deviceMCPMainTemplate = `// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.

package main

import (
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"
	mcptools "{{.ModulePath}}/internal/mcp"
)

func main() {
	s := server.NewMCPServer(
		"{{.DisplayName}}",
		"1.0.0",
		server.WithToolCapabilities(false),
	)
	mcptools.RegisterTools(s)
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}
`

const deviceMCPToolsTemplate = `// Copyright {{.CurrentYear}}. Licensed under Apache-2.0. See LICENSE.
// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.

package mcp

import (
	"context"
	"encoding/json"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"{{.ModulePath}}/internal/cli"
	"{{.ModulePath}}/internal/device"
	"{{.ModulePath}}/internal/mcp/cobratree"
)

// RegisterTools registers the device's MCP tool surface: a read-only context
// tool returning the device's capability + safety metadata, plus every
// user-facing CLI command mirrored by the cobratree walker. The walker honors
// each command's mcp:read-only and mcp:hidden annotations, so commands a device
// cannot safely expose to an agent (for example controls that need a held BLE
// connection) are hidden by the CLI, not here.
func RegisterTools(s *server.MCPServer) {
	s.AddTool(
		mcplib.NewTool("context",
			mcplib.WithDescription("Get device context: protocol, telemetry fields, and callable vs withheld commands with their safety classes. Call this first."),
			mcplib.WithReadOnlyHintAnnotation(true),
		),
		handleContext,
	)
	cobratree.RegisterAll(s, cli.RootCmd(), cobratree.SiblingCLIPath)
}

func handleContext(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	data, err := json.MarshalIndent(device.Capabilities(), "", "  ")
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}
	return mcplib.NewToolResultText(string(data)), nil
}
`

package pipeline

import (
	"archive/zip"
	"debug/macho"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A multi-platform .mcpb carries one MCP binary per target and selects between
// them with manifest platform_overrides.
//
// Why this exists
// ---------------
// BuildMCPBBundle's single-platform path writes the binary to the exact path the
// manifest declares, so command always resolves. A consumer that instead packs
// every released binary into one bundle - which is what you want, because a
// .mcpb is downloaded once and installed on whatever machine the operator has -
// has to reconcile arch-suffixed filenames with a manifest that names one
// command. Doing that by hand is how Servosity/msp-skills shipped 64 bundles
// whose command pointed at bin/<slug>-mcp while the archive contained only
// bin/<slug>-mcp-darwin-arm64 and friends: Claude Desktop installs the
// extension, prompts for credentials, and fails at the first tool call.
// Reported upstream as mvanhorn/cli-printing-press#3547 finding 2.
//
// The MCPB spec keys platform_overrides on OS only - darwin, linux, win32 -
// and defines no ${arch} template variable, so per-OS overrides alone still
// cannot separate Intel from Apple Silicon. #3547 called for a launcher shim or
// a universal binary. This uses a universal binary for darwin, because a shim
// depends on the host preserving the archive's executable bit and on a shell
// being present, and neither is guaranteed. Linux and Windows have no fat-binary
// format, so those overrides name a single architecture and the bundle records
// which one.

// BundleBinary is one target's pair of built binaries.
type BundleBinary struct {
	GOOS    string
	GOARCH  string
	MCPPath string // required: the MCP server binary for this target
	CLIPath string // optional: the companion CLI the MCP server shells out to
}

// mcpbOSKey maps a Go GOOS to the MCPB platform_overrides key.
func mcpbOSKey(goos string) string {
	if goos == "windows" {
		return "win32"
	}
	return goos
}

// archiveName is the in-bundle path for one target's binary.
func archiveName(base, goos, goarch string) string {
	name := fmt.Sprintf("bin/%s-%s-%s", base, goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// preferredArch picks the architecture an OS override should name when the OS
// has no fat-binary format. amd64 first: it is the broadest target, and Windows
// on ARM runs x64 under emulation while the reverse is not true.
var preferredArch = []string{"amd64", "arm64", "386", "arm"}

func pickTarget(targets []BundleBinary, goos string) (BundleBinary, bool) {
	for _, want := range preferredArch {
		for _, t := range targets {
			if t.GOOS == goos && t.GOARCH == want {
				return t, true
			}
		}
	}
	for _, t := range targets {
		if t.GOOS == goos {
			return t, true
		}
	}
	return BundleBinary{}, false
}

// applyPlatformOverrides rewrites the manifest so every OS present in targets
// resolves to a real path inside the bundle, and returns the archive members to
// write. universalDarwin, when non-empty, is the in-bundle path of a merged
// darwin binary that covers both architectures.
func applyPlatformOverrides(m *MCPBManifest, mcpBase, cliBase string, targets []BundleBinary, universalDarwin string) {
	if m.Server.MCPConfig.PlatformOverrides == nil {
		m.Server.MCPConfig.PlatformOverrides = map[string]MCPBLaunchOverride{}
	}
	oses := map[string]bool{}
	for _, t := range targets {
		oses[t.GOOS] = true
	}
	names := make([]string, 0, len(oses))
	for goos := range oses {
		names = append(names, goos)
	}
	sort.Strings(names)

	var defaultCommand string
	for _, goos := range names {
		var member string
		if goos == "darwin" && universalDarwin != "" {
			member = universalDarwin
		} else {
			t, ok := pickTarget(targets, goos)
			if !ok {
				continue
			}
			member = archiveName(mcpBase, t.GOOS, t.GOARCH)
		}
		m.Server.MCPConfig.PlatformOverrides[mcpbOSKey(goos)] = MCPBLaunchOverride{
			Command: "${__dirname}/" + member,
		}
		// Prefer darwin as the base command: Claude Desktop ships on macOS and
		// Windows, and win32 always has its own override anyway.
		if defaultCommand == "" || goos == "darwin" {
			defaultCommand = "${__dirname}/" + member
			m.Server.EntryPoint = member
		}
	}
	if defaultCommand != "" {
		m.Server.MCPConfig.Command = defaultCommand
	}
	_ = cliBase
}

// BuildMCPBMultiPlatformBundle writes a .mcpb carrying every target in params
// and a manifest whose declared commands all resolve inside it. It validates the
// finished archive before returning, so a bundle with an unresolvable command
// cannot be produced.
func BuildMCPBMultiPlatformBundle(params BundleParams, targets []BundleBinary) error {
	if len(targets) == 0 {
		return BuildMCPBBundle(params)
	}
	manifestPath := filepath.Join(params.CLIDir, MCPBManifestFilename)
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading manifest: %w", err)
	}
	var manifest MCPBManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}
	if manifest.Name == "" {
		return fmt.Errorf("manifest name is empty; cannot derive in-bundle binary paths")
	}
	if params.Version != "" {
		manifest.Version = params.Version
	}

	mcpBase := manifest.Name
	cliBase := params.CLIBinaryName

	// Merge the darwin slices into one universal binary when both are present;
	// platform_overrides cannot express an architecture, so this is the only way
	// a single darwin entry serves Intel and Apple Silicon.
	tmp, err := os.MkdirTemp("", "mcpb-universal-")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	universalDarwin := ""
	universalPath := ""
	var darwinSlices []string
	for _, want := range []string{"amd64", "arm64"} {
		for _, t := range targets {
			if t.GOOS == "darwin" && t.GOARCH == want {
				darwinSlices = append(darwinSlices, t.MCPPath)
			}
		}
	}
	if len(darwinSlices) > 1 {
		universalPath = filepath.Join(tmp, mcpBase+"-darwin")
		if err := writeMachoUniversal(darwinSlices, universalPath); err != nil {
			// A merge failure must not silently degrade to a bundle that breaks
			// one darwin architecture; surface it.
			return fmt.Errorf("building universal darwin binary: %w", err)
		}
		universalDarwin = "bin/" + mcpBase + "-darwin"
	}

	applyPlatformOverrides(&manifest, mcpBase, cliBase, targets, universalDarwin)

	rendered, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("rendering manifest: %w", err)
	}
	rendered = append(rendered, '\n')

	if err := os.MkdirAll(filepath.Dir(params.OutputPath), 0o755); err != nil {
		return fmt.Errorf("creating bundle output dir: %w", err)
	}
	out, err := os.Create(params.OutputPath)
	if err != nil {
		return fmt.Errorf("creating bundle file: %w", err)
	}
	defer func() { _ = out.Close() }()

	zw := zip.NewWriter(out)
	if err := writeZipBytes(zw, MCPBManifestFilename, rendered, 0o644); err != nil {
		_ = zw.Close()
		return fmt.Errorf("writing manifest into bundle: %w", err)
	}
	if universalDarwin != "" {
		if err := zipFile(zw, universalDarwin, universalPath); err != nil {
			_ = zw.Close()
			return fmt.Errorf("writing universal darwin binary: %w", err)
		}
	}
	for _, t := range targets {
		if err := zipFile(zw, archiveName(mcpBase, t.GOOS, t.GOARCH), t.MCPPath); err != nil {
			_ = zw.Close()
			return fmt.Errorf("writing %s/%s MCP binary: %w", t.GOOS, t.GOARCH, err)
		}
		if t.CLIPath != "" && cliBase != "" {
			if err := zipFile(zw, archiveName(cliBase, t.GOOS, t.GOARCH), t.CLIPath); err != nil {
				_ = zw.Close()
				return fmt.Errorf("writing %s/%s CLI binary: %w", t.GOOS, t.GOARCH, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalizing bundle archive: %w", err)
	}
	return ValidateMCPBBundle(params.OutputPath)
}

// ValidateMCPBBundle asserts every command path the bundle's manifest declares
// names a file the bundle actually contains. This is the check that turns the
// #3547 defect from "installs, prompts for credentials, then fails at the first
// tool call" into a build-time error.
func ValidateMCPBBundle(path string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("opening bundle: %w", err)
	}
	defer func() { _ = zr.Close() }()

	members := map[string]bool{}
	var manifestData []byte
	for _, f := range zr.File {
		members[f.Name] = true
		if f.Name != MCPBManifestFilename {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("opening manifest in bundle: %w", err)
		}
		manifestData, err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return fmt.Errorf("reading manifest in bundle: %w", err)
		}
	}
	if manifestData == nil {
		return fmt.Errorf("bundle %s has no %s", path, MCPBManifestFilename)
	}
	var manifest MCPBManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("parsing manifest in bundle: %w", err)
	}

	check := func(label, command string) error {
		member := strings.TrimPrefix(command, "${__dirname}/")
		if member == command {
			// Not a bundle-relative path (e.g. "node"); the host resolves it.
			return nil
		}
		if !members[member] {
			return fmt.Errorf(
				"%s: manifest %s is %q but the bundle contains no %q. The extension would install, "+
					"prompt for credentials, and fail at the first tool call. Bundle members: %s",
				filepath.Base(path), label, command, member, strings.Join(sortedMembers(members), ", "))
		}
		return nil
	}
	if err := check("server.mcp_config.command", manifest.Server.MCPConfig.Command); err != nil {
		return err
	}
	keys := make([]string, 0, len(manifest.Server.MCPConfig.PlatformOverrides))
	for k := range manifest.Server.MCPConfig.PlatformOverrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ov := manifest.Server.MCPConfig.PlatformOverrides[k]
		if ov.Command == "" {
			continue
		}
		if err := check("server.mcp_config.platform_overrides."+k+".command", ov.Command); err != nil {
			return err
		}
	}
	if manifest.Server.EntryPoint != "" && !members[manifest.Server.EntryPoint] {
		return fmt.Errorf("%s: manifest server.entry_point is %q but the bundle contains no such member",
			filepath.Base(path), manifest.Server.EntryPoint)
	}
	return nil
}

func sortedMembers(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// writeMachoUniversal merges Mach-O files into one universal ("fat") binary.
//
// The format is a big-endian header followed by one 20-byte record per slice
// and then the slices themselves, each aligned to 2^align. Building it here
// rather than shelling out to lipo keeps the press free of an Xcode dependency
// and lets a Linux CI runner produce a macOS-universal bundle.
func writeMachoUniversal(paths []string, outPath string) error {
	if len(paths) < 2 {
		return fmt.Errorf("need at least two slices, got %d", len(paths))
	}
	type slice struct {
		cpu    uint32
		subCPU uint32
		data   []byte
	}
	slices := make([]slice, 0, len(paths))
	seen := map[uint32]bool{}
	for _, p := range paths {
		f, err := macho.Open(p)
		if err != nil {
			return fmt.Errorf("reading %s as Mach-O: %w", p, err)
		}
		cpu, sub := uint32(f.Cpu), f.SubCpu
		_ = f.Close()
		if seen[cpu] {
			return fmt.Errorf("two slices share CPU type %d; a universal binary needs distinct architectures", cpu)
		}
		seen[cpu] = true
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("reading %s: %w", p, err)
		}
		slices = append(slices, slice{cpu: cpu, subCPU: sub, data: data})
	}
	// Apple orders slices by CPU type; keep it deterministic.
	sort.Slice(slices, func(i, j int) bool { return slices[i].cpu < slices[j].cpu })

	const (
		fatMagic  = 0xCAFEBABE
		headerLen = 8
		archLen   = 20
		// 2^14 = 16 KiB, the alignment arm64 requires and amd64 tolerates.
		align = 14
	)
	offset := uint32(headerLen + archLen*len(slices))
	aligned := func(v uint32) uint32 {
		const a = 1 << align
		if v%a == 0 {
			return v
		}
		return v + (a - v%a)
	}

	var buf []byte
	appendU32 := func(v uint32) { buf = binary.BigEndian.AppendUint32(buf, v) }
	appendU32(fatMagic)
	appendU32(uint32(len(slices)))
	offsets := make([]uint32, len(slices))
	for i, s := range slices {
		offset = aligned(offset)
		offsets[i] = offset
		appendU32(s.cpu)
		appendU32(s.subCPU)
		appendU32(offset)
		appendU32(uint32(len(s.data)))
		appendU32(align)
		offset += uint32(len(s.data))
	}
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("creating %s: %w", outPath, err)
	}
	defer func() { _ = out.Close() }()
	if _, err := out.Write(buf); err != nil {
		return err
	}
	for i, s := range slices {
		if _, err := out.Seek(int64(offsets[i]), io.SeekStart); err != nil {
			return err
		}
		if _, err := out.Write(s.data); err != nil {
			return err
		}
	}
	return nil
}

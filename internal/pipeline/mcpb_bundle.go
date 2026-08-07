package pipeline

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// BundleParams describes one MCPB bundle build. CLIDir must contain a
// manifest.json (emitted by WriteMCPBManifest) and a built MCP binary at
// BinaryPath. BinaryName optionally names the MCP binary inside the zip;
// when empty, the manifest's server.entry_point is used as-is. OutputPath
// is where the .mcpb file will be written; the caller is responsible for
// choosing a path that includes platform information so multi-platform
// builds don't overwrite each other.
//
// CLIBinaryPath is optional — when set, the bundle includes a second
// binary at `bin/<CLIBinaryName>` so the MCP server can shell out to its
// companion CLI for novel-feature tools. Empty CLIBinaryPath produces a
// single-binary bundle (the CLI must be on PATH for novel-feature tools
// to work). CLIBinaryName must be set when CLIBinaryPath is — it names
// the binary inside the zip; we deliberately do NOT serialize this name
// into manifest.json because Claude Desktop's MCPB v0.3 schema
// strictly rejects unknown top-level keys.
//
// Version optionally stamps the printed CLI release version into the
// bundled manifest bytes. It does not mutate manifest.json on disk because
// that file is generated before release tags exist.
type BundleParams struct {
	CLIDir        string
	BinaryPath    string
	BinaryName    string
	CLIBinaryName string
	CLIBinaryPath string
	OutputPath    string
	Version       string
}

// BuildMCPBBundle assembles an MCPB ZIP at OutputPath. The bundle layout is:
//
//	manifest.json
//	bin/<binary>            (the path declared by manifest's server.entry_point)
//
// Returns nil and creates no file when manifest.json is missing — the
// caller's CLI dir is presumably one we don't want to bundle (no MCP
// binary, the same gate WriteMCPBManifest uses).
func BuildMCPBBundle(params BundleParams) error {
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
	if manifest.Server.EntryPoint == "" {
		return errors.New("manifest server.entry_point is empty")
	}
	if params.BinaryName != "" {
		entryPoint := "bin/" + params.BinaryName
		if entryPoint != manifest.Server.EntryPoint {
			manifest.Server.EntryPoint = entryPoint
			manifestData, err = rewriteMCPBManifestLaunch(manifestData, entryPoint)
			if err != nil {
				return err
			}
		}
	}
	if params.Version != "" {
		manifest.Version = params.Version
		manifestData, err = rewriteMCPBManifestVersion(manifestData, params.Version)
		if err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(params.OutputPath), 0o755); err != nil {
		return fmt.Errorf("creating bundle output dir: %w", err)
	}

	out, err := os.Create(params.OutputPath)
	if err != nil {
		return fmt.Errorf("creating bundle file: %w", err)
	}
	defer func() { _ = out.Close() }()

	zw := zip.NewWriter(out)
	if err := writeZipBytes(zw, MCPBManifestFilename, manifestData, 0o644); err != nil {
		_ = zw.Close()
		return fmt.Errorf("writing manifest into bundle: %w", err)
	}
	// Stream binaries (10-30 MB combined) instead of buffering. Preserve exec
	// bits so hosts that honor POSIX zip mode (Claude Desktop on macOS, MCP
	// for Windows) launch directly without chmod.
	if err := zipFile(zw, manifest.Server.EntryPoint, params.BinaryPath); err != nil {
		_ = zw.Close()
		return fmt.Errorf("writing MCP binary into bundle: %w", err)
	}
	if params.CLIBinaryPath != "" && params.CLIBinaryName != "" {
		if err := zipFile(zw, "bin/"+params.CLIBinaryName, params.CLIBinaryPath); err != nil {
			_ = zw.Close()
			return fmt.Errorf("writing CLI binary into bundle: %w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalizing bundle archive: %w", err)
	}
	return nil
}

func rewriteMCPBManifestVersion(data []byte, version string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing manifest document: %w", err)
	}
	doc["version"] = version

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("marshaling manifest document: %w", err)
	}
	return buf.Bytes(), nil
}

func rewriteMCPBManifestLaunch(data []byte, entryPoint string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing manifest document: %w", err)
	}
	server, ok := doc["server"].(map[string]any)
	if !ok {
		return nil, errors.New("manifest server must be an object")
	}
	server["entry_point"] = entryPoint
	mcpConfig, ok := server["mcp_config"].(map[string]any)
	if !ok {
		mcpConfig = map[string]any{}
		server["mcp_config"] = mcpConfig
	}
	mcpConfig["command"] = "${__dirname}/" + entryPoint

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("marshaling manifest document: %w", err)
	}
	return buf.Bytes(), nil
}

// zipFile streams srcPath into the zip at name, preserving exec bits.
func zipFile(zw *zip.Writer, name, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()
	stat, err := src.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", srcPath, err)
	}
	return writeZipReader(zw, name, src, stat.Mode()&0o777)
}

func writeZipBytes(zw *zip.Writer, name string, data []byte, mode os.FileMode) error {
	return writeZipReader(zw, name, bytes.NewReader(data), mode)
}

func writeZipReader(zw *zip.Writer, name string, r io.Reader, mode os.FileMode) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

// DefaultBundleOutputPath returns the conventional path the generator and
// `cli-printing-press bundle` use when no --output is set. Platform suffix in
// the filename keeps cross-compiled bundles from clobbering each other.
func DefaultBundleOutputPath(cliDir, mcpBinary, goos, goarch string) string {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	name := fmt.Sprintf("%s-%s-%s.mcpb", mcpBinary, goos, goarch)
	return filepath.Join(cliDir, "build", name)
}

// StagedMCPBinaryPath returns the conventional path where bundle's
// pre-zip staging copies of the MCP binary live (cliDir/build/stage/bin/).
// Exposed so internal/cli callers don't reach into pipeline internals
// to construct the path themselves.
func StagedMCPBinaryPath(cliDir, mcpBinary string) string {
	return filepath.Join(cliDir, "build", "stage", "bin", mcpBinary)
}

// BuildMCPBBinary centralizes build flags so direct bundle builds and promote
// refreshes produce interchangeable staged binaries.
func BuildMCPBBinary(cliDir, name, outputPath, goos, goarch string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("creating bin dir: %w", err)
	}
	pkg := "./cmd/" + name
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w -buildid=", "-o", outputPath, pkg)
	cmd.Dir = cliDir
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s: %w\n%s", pkg, err, string(out))
	}
	return nil
}

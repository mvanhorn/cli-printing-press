package cartridge

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed builtins/*.yaml
var builtinFS embed.FS

// Builtins loads all starter cartridges embedded in the `pp` binary so fresh
// installs deliver instant <200ms value with zero network setup.
func Builtins() ([]*Cartridge, error) {
	entries, err := fs.ReadDir(builtinFS, "builtins")
	if err != nil {
		return nil, err
	}
	out := make([]*Cartridge, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		raw, err := builtinFS.ReadFile("builtins/" + entry.Name())
		if err != nil {
			return nil, err
		}
		c, err := Parse(raw, "builtin:"+entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// BuiltinRaw returns the raw YAML bytes of an embedded starter cartridge.
func BuiltinRaw(name string) ([]byte, bool) {
	raw, err := builtinFS.ReadFile("builtins/" + strings.TrimSpace(name) + ".yaml")
	if err != nil {
		return nil, false
	}
	return raw, true
}

// Installed loads all cartridges installed under ~/.pp/cartridges/.
func Installed() ([]*Cartridge, error) {
	dir, err := CartridgesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Cartridge
	for _, e := range entries {
		var p string
		if e.IsDir() {
			p = filepath.Join(dir, e.Name(), "cartridge.yaml")
		} else if strings.HasSuffix(e.Name(), ".yaml") {
			p = filepath.Join(dir, e.Name())
		} else {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		c, err := LoadFile(p)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Resolve loads a cartridge by name from ~/.pp/cartridges/ first, then falls
// back to embedded starter cartridges.
func Resolve(name string) (*Cartridge, error) {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return nil, fmt.Errorf("cartridge name is required")
	}
	if dir, err := CartridgesDir(); err == nil {
		candidates := []string{
			filepath.Join(dir, clean, "cartridge.yaml"),
			filepath.Join(dir, clean+".yaml"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return LoadFile(p)
			}
		}
	}
	if raw, ok := BuiltinRaw(clean); ok {
		return Parse(raw, "builtin:"+clean+".yaml")
	}
	return nil, fmt.Errorf("cartridge %q not found (try `pp search %s` or `pp use %s`)", clean, clean, clean)
}

// InstallBytes validates, writes, and pins a cartridge under ~/.pp/cartridges/<name>/cartridge.yaml.
func InstallBytes(raw []byte, source string) (*Cartridge, string, error) {
	c, err := Parse(raw, source)
	if err != nil {
		return nil, "", err
	}
	dir, err := CartridgesDir()
	if err != nil {
		return nil, "", err
	}
	targetDir := filepath.Join(dir, c.Name)
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("creating cartridge dir: %w", err)
	}
	targetFile := filepath.Join(targetDir, "cartridge.yaml")
	if err := os.WriteFile(targetFile, raw, 0o600); err != nil {
		return nil, "", fmt.Errorf("writing cartridge: %w", err)
	}
	c.Source = targetFile

	lockPath, err := LockfilePath()
	if err != nil {
		return nil, "", err
	}
	lf, err := LoadLockfile(lockPath)
	if err != nil {
		return nil, "", err
	}
	lf.Pin(LockEntry{
		Kind:         "cartridge",
		Name:         c.Name,
		Version:      c.Version,
		SHA256:       c.SHA256,
		Path:         targetFile,
		Source:       source,
		AllowedHosts: c.Trust.AllowedHosts,
	})
	if err := lf.Save(lockPath); err != nil {
		return nil, "", err
	}
	return c, targetFile, nil
}

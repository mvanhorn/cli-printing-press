package appsniff

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const (
	// Mach-O header magics.
	machOMagic64    uint32 = 0xfeedfacf
	machOMagic32    uint32 = 0xfeedface
	machOFatMagic   uint32 = 0xcafebabe
	machOFatCigam   uint32 = 0xbebafeca
	lcEncryptInfo32 uint32 = 0x21
	lcEncryptInfo64 uint32 = 0x2c
)

// MachOEncryptionStatus reports FairPlay App Store DRM state for a Mach-O binary.
type MachOEncryptionStatus struct {
	IsMachO   bool
	Encrypted bool
	CryptID   uint32
	Detail    string
}

// CheckMachOEncryption inspects thin (32/64-bit) and universal/FAT Mach-O
// headers for LC_ENCRYPTION_INFO / LC_ENCRYPTION_INFO_64 with cryptid != 0.
// It is read-only detection and never attempts to bypass DRM.
func CheckMachOEncryption(data []byte) MachOEncryptionStatus {
	if len(data) < 28 {
		return MachOEncryptionStatus{}
	}
	beMagic := binary.BigEndian.Uint32(data[0:4])
	if beMagic == machOFatMagic || beMagic == machOFatCigam {
		nfat := int(binary.BigEndian.Uint32(data[4:8]))
		if nfat <= 0 || nfat > 16 || 8+nfat*20 > len(data) {
			return MachOEncryptionStatus{}
		}
		for i := range nfat {
			off := int(binary.BigEndian.Uint32(data[8+i*20+8 : 8+i*20+12]))
			size := int(binary.BigEndian.Uint32(data[8+i*20+12 : 8+i*20+16]))
			if off > 0 && size > 28 && off+size <= len(data) {
				st := checkThinMachOEncryption(data[off : off+size])
				if st.Encrypted {
					return st
				}
				if st.IsMachO {
					return st
				}
			}
		}
		return MachOEncryptionStatus{IsMachO: true}
	}
	return checkThinMachOEncryption(data)
}

func checkThinMachOEncryption(data []byte) MachOEncryptionStatus {
	if len(data) < 28 {
		return MachOEncryptionStatus{}
	}
	magic := binary.LittleEndian.Uint32(data[0:4])
	var hdrSize int
	switch magic {
	case machOMagic64:
		hdrSize = 32
	case machOMagic32:
		hdrSize = 28
	default:
		return MachOEncryptionStatus{}
	}
	if len(data) < hdrSize {
		return MachOEncryptionStatus{}
	}
	ncmds := int(binary.LittleEndian.Uint32(data[16:20]))
	pos := hdrSize
	for i := 0; i < ncmds && pos+8 <= len(data); i++ {
		cmd := binary.LittleEndian.Uint32(data[pos : pos+4])
		cmdSize := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		if cmdSize < 8 || pos+cmdSize > len(data) {
			break
		}
		if (cmd == lcEncryptInfo32 || cmd == lcEncryptInfo64) && pos+20 <= len(data) {
			cryptID := binary.LittleEndian.Uint32(data[pos+16 : pos+20])
			if cryptID != 0 {
				return MachOEncryptionStatus{
					IsMachO:   true,
					Encrypted: true,
					CryptID:   cryptID,
					Detail:    fmt.Sprintf("FairPlay App Store DRM detected (cryptid=%d); encrypted Mach-O code segments skipped, unencrypted assets still scanned", cryptID),
				}
			}
		}
		pos += cmdSize
	}
	return MachOEncryptionStatus{IsMachO: true, Encrypted: false}
}

// DiscoverAndroid analyzes an Android .apk, .xapk (split-APK bundle), .aab, or
// unpacked APK directory in pure Go.
func DiscoverAndroid(targetPath string, opts Options) (*DiscoveryResult, error) {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return nil, fmt.Errorf("resolving android path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", absPath, err)
	}

	res := &DiscoveryResult{
		Target:     absPath,
		TargetKind: TargetKindAndroid,
		Meta: BinaryMeta{
			AppName:   strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath)),
			Platform:  "android",
			Framework: FrameworkNativeAndroid,
		},
		Auth: AuthSignal{Scheme: AuthNone},
	}
	acc := newExtractorAccumulator()

	if info.IsDir() {
		if err := scanUnpackedAndroidDir(absPath, opts, res, acc); err != nil {
			return nil, err
		}
	} else {
		lower := strings.ToLower(absPath)
		switch {
		case strings.HasSuffix(lower, ".xapk"):
			if err := scanXAPKArchive(absPath, opts, res, acc); err != nil {
				return nil, err
			}
		default:
			if err := scanAPKArchiveFile(absPath, opts, res, acc); err != nil {
				return nil, err
			}
		}
	}

	finalizeDiscoveryResult(res, acc, opts)
	return res, nil
}

type xapkManifest struct {
	PackageName string `json:"package_name"`
	Name        string `json:"name"`
	VersionName string `json:"version_name"`
	VersionCode string `json:"version_code"`
}

func scanXAPKArchive(xapkPath string, opts Options, res *DiscoveryResult, acc *extractorAccumulator) error {
	zr, err := zip.OpenReader(xapkPath)
	if err != nil {
		return fmt.Errorf("opening xapk %s: %w", xapkPath, err)
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		if isUnsafeZipPath(f.Name) {
			continue
		}
		base := strings.ToLower(filepath.Base(f.Name))
		if base == "manifest.json" && f.UncompressedSize64 < 2*1024*1024 {
			if data, rerr := readZipEntryBounded(f, 2*1024*1024); rerr == nil {
				var xm xapkManifest
				if json.Unmarshal(data, &xm) == nil {
					if xm.Name != "" {
						res.Meta.AppName = xm.Name
					}
					if xm.PackageName != "" {
						res.Meta.PackageID = xm.PackageName
					}
					if xm.VersionName != "" {
						res.Meta.Version = xm.VersionName
					}
					if xm.VersionCode != "" {
						res.Meta.BuildNumber = xm.VersionCode
					}
				}
			}
			continue
		}
		if strings.HasSuffix(base, ".apk") {
			// Skip language-only splits to save time; keep base + arm64/armeabi splits.
			if strings.HasPrefix(base, "config.") && !strings.Contains(base, "arm") && !strings.Contains(base, "x86") {
				continue
			}
			apkData, rerr := readZipEntryBounded(f, opts.maxTotalBytes())
			if rerr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("skipped split %s: %v", f.Name, rerr))
				continue
			}
			nestedZR, zerr := zip.NewReader(bytes.NewReader(apkData), int64(len(apkData)))
			if zerr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("invalid nested apk %s: %v", f.Name, zerr))
				continue
			}
			scanAPKZipReader(nestedZR, f.Name, opts, res, acc)
		}
	}
	return nil
}

func scanAPKArchiveFile(apkPath string, opts Options, res *DiscoveryResult, acc *extractorAccumulator) error {
	zr, err := zip.OpenReader(apkPath)
	if err != nil {
		return fmt.Errorf("opening apk %s: %w", apkPath, err)
	}
	defer func() { _ = zr.Close() }()
	scanAPKZipReader(&zr.Reader, filepath.Base(apkPath), opts, res, acc)
	return nil
}

func scanAPKZipReader(zr *zip.Reader, archiveLabel string, opts Options, res *DiscoveryResult, acc *extractorAccumulator) {
	maxFile := opts.maxFileBytes()
	var dexFiles []*zip.File

	for _, f := range zr.File {
		if isUnsafeZipPath(f.Name) || IsForbiddenSecretPath(f.Name) {
			continue
		}
		name := filepath.ToSlash(f.Name)
		lower := strings.ToLower(name)
		base := filepath.Base(lower)

		switch {
		case strings.HasSuffix(lower, ".dex") && (strings.HasPrefix(base, "classes") || strings.Contains(lower, "/dex/")):
			dexFiles = append(dexFiles, f)

		case base == "resources.arsc":
			if data, err := readZipEntryBounded(f, maxFile); err == nil {
				urls, count := ExtractARSCBaseURLs(data)
				res.Stats.ARSCStringsParsed += count
				res.Stats.FilesScanned++
				for _, u := range urls {
					acc.ingestAbsoluteURL(u, "resources.arsc", ConfidenceHigh)
					acc.addBaseURLWithWeight(u, 5, "resources.arsc")
				}
			}

		case base == "index.android.bundle" || strings.HasSuffix(lower, ".hbc") || (strings.HasPrefix(lower, "assets/") && (strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".jsbundle"))):
			if data, err := readZipEntryBounded(f, maxFile); err == nil {
				res.Stats.FilesScanned++
				if IsHermesBytecode(data) {
					res.Meta.Framework = FrameworkHermes
					strs := ExtractHermesStrings(data)
					res.Stats.HermesStrings += len(strs)
					acc.scanStringSlice(strs, "hermes:"+name)
				} else {
					if res.Meta.Framework == FrameworkNativeAndroid {
						res.Meta.Framework = FrameworkReactNative
					}
					acc.scanJS(base, string(data), "android-asset:"+name)
				}
			}

		case strings.HasPrefix(lower, "assets/public/") && isScannableCodeAsset(lower):
			res.Meta.Framework = FrameworkCapacitor
			if data, err := readZipEntryBounded(f, maxFile); err == nil {
				res.Stats.FilesScanned++
				acc.scanJS(base, string(data), "capacitor:"+name)
			}

		case strings.HasPrefix(lower, "assets/") && strings.HasSuffix(lower, ".json"):
			if data, err := readZipEntryBounded(f, min(maxFile, 4*1024*1024)); err == nil {
				res.Stats.FilesScanned++
				acc.scanJS(base, string(data), "android-asset:"+name)
			}

		case strings.HasSuffix(lower, "/libapp.so") && strings.Contains(lower, "arm64"):
			res.Meta.Framework = FrameworkFlutter
			if data, err := readZipEntryBounded(f, maxFile); err == nil {
				res.Stats.FilesScanned++
				strs := extractPrintableStrings(data, 6, 256, 150000)
				acc.scanStringSlice(strs, "flutter-aot:"+name)
			}

		case strings.Contains(lower, "network_security_config") && strings.HasSuffix(lower, ".xml"):
			if data, err := readZipEntryBounded(f, 1024*1024); err == nil {
				res.Stats.FilesScanned++
				if pf := parseNetworkSecurityConfig(data, name); pf != nil {
					acc.addPinning(*pf)
				}
			}
		}
	}

	sort.Slice(dexFiles, func(i, j int) bool { return dexFiles[i].Name < dexFiles[j].Name })
	for _, f := range dexFiles {
		data, err := readZipEntryBounded(f, maxFile)
		if err != nil {
			continue
		}
		if !IsDEX(data) {
			continue
		}
		res.Stats.DEXFilesParsed++
		res.Stats.FilesScanned++

		retrofitAnns, strs, err := ExtractDEXRetrofitEndpoints(data)
		if err != nil {
			continue
		}
		for _, ann := range retrofitAnns {
			res.Stats.RetrofitMethods++
			epPath := ann.Path
			if strings.HasPrefix(epPath, "https://") {
				epPath = reTemplateExpr.ReplaceAllString(epPath, "{param}")
				acc.addCandidateURLOrPath(ann.Method, epPath, "dex-retrofit", ConfidenceHigh, nil, ann.HeaderNames, ann.RequestContentType)
				continue
			}
			acc.addEndpoint(Endpoint{
				Method:             ann.Method,
				Path:               epPath,
				Protocol:           classifyProtocol(epPath),
				Source:             "dex-retrofit",
				Confidence:         ConfidenceHigh,
				Params:             ann.Params,
				HeaderNames:        ann.HeaderNames,
				RequestContentType: ann.RequestContentType,
			})
		}
		acc.scanStringSlice(strs, "dex:"+archiveLabel+":"+f.Name)
	}
}

func scanUnpackedAndroidDir(dir string, opts Options, res *DiscoveryResult, acc *extractorAccumulator) error {
	maxFile := opts.maxFileBytes()
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		if IsForbiddenSecretPath(path) {
			res.Stats.FilesSkippedSecret++
			return nil
		}
		lower := strings.ToLower(path)
		switch {
		case strings.HasSuffix(lower, ".apk"):
			_ = scanAPKArchiveFile(path, opts, res, acc)
		case strings.HasSuffix(lower, ".dex"):
			if data, rerr := readBoundedFile(path, maxFile); rerr == nil && IsDEX(data) {
				res.Stats.DEXFilesParsed++
				res.Stats.FilesScanned++
				anns, strs, derr := ExtractDEXRetrofitEndpoints(data)
				if derr == nil {
					for _, ann := range anns {
						res.Stats.RetrofitMethods++
						acc.addEndpoint(Endpoint{
							Method:             ann.Method,
							Path:               ann.Path,
							Protocol:           classifyProtocol(ann.Path),
							Source:             "dex-retrofit",
							Confidence:         ConfidenceHigh,
							Params:             ann.Params,
							HeaderNames:        ann.HeaderNames,
							RequestContentType: ann.RequestContentType,
						})
					}
					acc.scanStringSlice(strs, "dex:"+filepath.Base(path))
				}
			}
		case filepath.Base(lower) == "resources.arsc":
			if data, rerr := readBoundedFile(path, maxFile); rerr == nil {
				urls, count := ExtractARSCBaseURLs(data)
				res.Stats.ARSCStringsParsed += count
				res.Stats.FilesScanned++
				for _, u := range urls {
					acc.ingestAbsoluteURL(u, "resources.arsc", ConfidenceHigh)
					acc.addBaseURLWithWeight(u, 5, "resources.arsc")
				}
			}
		case strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".json") || filepath.Base(lower) == "index.android.bundle":
			if data, rerr := readBoundedFile(path, maxFile); rerr == nil {
				res.Stats.FilesScanned++
				if IsHermesBytecode(data) {
					res.Meta.Framework = FrameworkHermes
					strs := ExtractHermesStrings(data)
					res.Stats.HermesStrings += len(strs)
					acc.scanStringSlice(strs, "hermes:"+filepath.Base(path))
				} else {
					acc.scanJS(filepath.Base(path), string(data), "dir:"+filepath.Base(path))
				}
			}
		}
		return nil
	})
}

// DiscoverIOS analyzes an iOS .ipa archive or an unpacked iOS .app bundle
// (such as a simulator build). It detects FairPlay DRM honestly via Mach-O
// LC_ENCRYPTION_INFO_64 without attempting to bypass encryption, while still
// inspecting unencrypted bundle assets (Info.plist, JS/Hermes bundles, Flutter
// App.framework, JSON configs).
func DiscoverIOS(targetPath string, opts Options) (*DiscoveryResult, error) {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return nil, fmt.Errorf("resolving ios path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", absPath, err)
	}

	res := &DiscoveryResult{
		Target:     absPath,
		TargetKind: TargetKindIOS,
		Meta: BinaryMeta{
			AppName:   strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath)),
			Platform:  "ios",
			Framework: FrameworkNativeIOS,
		},
		Auth: AuthSignal{Scheme: AuthNone},
	}
	acc := newExtractorAccumulator()

	if info.IsDir() {
		if err := scanUnpackedIOSApp(absPath, opts, res, acc); err != nil {
			return nil, err
		}
	} else {
		if err := scanIPAArchive(absPath, opts, res, acc); err != nil {
			return nil, err
		}
	}

	finalizeDiscoveryResult(res, acc, opts)
	return res, nil
}

func scanIPAArchive(ipaPath string, opts Options, res *DiscoveryResult, acc *extractorAccumulator) error {
	zr, err := zip.OpenReader(ipaPath)
	if err != nil {
		return fmt.Errorf("opening ipa %s: %w", ipaPath, err)
	}
	defer func() { _ = zr.Close() }()

	maxFile := opts.maxFileBytes()
	var appPrefix string
	var execName string

	// Pass 1: find Payload/<App>.app/Info.plist.
	for _, f := range zr.File {
		if isUnsafeZipPath(f.Name) {
			continue
		}
		name := filepath.ToSlash(f.Name)
		if strings.HasPrefix(name, "Payload/") && strings.HasSuffix(name, ".app/Info.plist") && strings.Count(name, "/") == 2 {
			appPrefix = strings.TrimSuffix(name, "Info.plist")
			if data, rerr := readZipEntryBounded(f, 2*1024*1024); rerr == nil {
				res.Stats.FilesScanned++
				if pinfo, perr := ParseInfoPlist(data); perr == nil {
					applyIOSPlistInfo(res, acc, pinfo)
				} else {
					acc.addWarning(fmt.Sprintf("failed to parse Info.plist: %v", perr))
				}
				var eerr error
				execName, eerr = extractExecutableNameFromPlist(data)
				if eerr != nil {
					acc.addWarning(fmt.Sprintf("could not resolve CFBundleExecutable from Info.plist: %v", eerr))
				}
			}
			break
		}
	}

	// Pass 2: inspect main Mach-O binary, JS/Hermes bundles, Flutter App.framework, and JSON assets.
	for _, f := range zr.File {
		if isUnsafeZipPath(f.Name) || IsForbiddenSecretPath(f.Name) {
			continue
		}
		name := filepath.ToSlash(f.Name)
		if appPrefix != "" && !strings.HasPrefix(name, appPrefix) {
			continue
		}
		rel := strings.TrimPrefix(name, appPrefix)
		lower := strings.ToLower(rel)
		base := filepath.Base(lower)

		switch {
		case execName != "" && rel == execName:
			if data, rerr := readZipEntryBounded(f, maxFile); rerr == nil {
				res.Stats.FilesScanned++
				inspectIOSMachO(data, rel, res, acc)
			}

		case base == "main.jsbundle" || strings.HasSuffix(lower, ".jsbundle") || strings.HasSuffix(lower, ".hbc"):
			if data, rerr := readZipEntryBounded(f, maxFile); rerr == nil {
				res.Stats.FilesScanned++
				if IsHermesBytecode(data) {
					res.Meta.Framework = FrameworkHermes
					strs := ExtractHermesStrings(data)
					res.Stats.HermesStrings += len(strs)
					acc.scanStringSlice(strs, "hermes:"+rel)
				} else {
					res.Meta.Framework = FrameworkReactNative
					acc.scanJS(base, string(data), "ios-bundle:"+rel)
				}
			}

		case lower == "frameworks/app.framework/app":
			res.Meta.Framework = FrameworkFlutter
			if data, rerr := readZipEntryBounded(f, maxFile); rerr == nil {
				res.Stats.FilesScanned++
				enc := CheckMachOEncryption(data)
				if enc.Encrypted {
					acc.addWarning("Flutter App.framework is FairPlay encrypted; skipped binary string extraction")
				} else {
					acc.scanStringSlice(extractPrintableStrings(data, 6, 256, 150000), "flutter-ios:"+rel)
				}
			}

		case strings.HasPrefix(lower, "public/") && isScannableCodeAsset(lower):
			res.Meta.Framework = FrameworkCapacitor
			if data, rerr := readZipEntryBounded(f, maxFile); rerr == nil {
				res.Stats.FilesScanned++
				acc.scanJS(base, string(data), "capacitor:"+rel)
			}

		case strings.HasSuffix(lower, ".json") && !strings.Contains(lower, ".lproj/"):
			if data, rerr := readZipEntryBounded(f, min(maxFile, 2*1024*1024)); rerr == nil {
				res.Stats.FilesScanned++
				acc.scanJS(base, string(data), "ios-asset:"+rel)
			}
		}
	}
	return nil
}

func scanUnpackedIOSApp(appDir string, opts Options, res *DiscoveryResult, acc *extractorAccumulator) error {
	maxFile := opts.maxFileBytes()
	var execName string
	plistPath := filepath.Join(appDir, "Info.plist")
	if data, err := readBoundedFile(plistPath, 2*1024*1024); err == nil {
		res.Stats.FilesScanned++
		if pinfo, perr := ParseInfoPlist(data); perr == nil {
			applyIOSPlistInfo(res, acc, pinfo)
		} else {
			acc.addWarning(fmt.Sprintf("failed to parse Info.plist: %v", perr))
		}
		var eerr error
		execName, eerr = extractExecutableNameFromPlist(data)
		if eerr != nil {
			acc.addWarning(fmt.Sprintf("could not resolve CFBundleExecutable from Info.plist: %v", eerr))
		}
	}
	if execName != "" {
		binPath, berr := safeBundleExecutablePath(appDir, execName)
		if berr != nil {
			acc.addWarning(fmt.Sprintf("skipped unsafe CFBundleExecutable %q: %v", execName, berr))
		} else if data, err := readBoundedFile(binPath, maxFile); err == nil {
			res.Stats.FilesScanned++
			inspectIOSMachO(data, execName, res, acc)
		}
	}

	return filepath.WalkDir(appDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		if IsForbiddenSecretPath(path) {
			res.Stats.FilesSkippedSecret++
			return nil
		}
		rel, _ := filepath.Rel(appDir, path)
		rel = filepath.ToSlash(rel)
		lower := strings.ToLower(rel)
		base := filepath.Base(lower)

		switch {
		case base == "main.jsbundle" || strings.HasSuffix(lower, ".jsbundle") || strings.HasSuffix(lower, ".hbc"):
			if data, rerr := readBoundedFile(path, maxFile); rerr == nil {
				res.Stats.FilesScanned++
				if IsHermesBytecode(data) {
					res.Meta.Framework = FrameworkHermes
					strs := ExtractHermesStrings(data)
					res.Stats.HermesStrings += len(strs)
					acc.scanStringSlice(strs, "hermes:"+rel)
				} else {
					res.Meta.Framework = FrameworkReactNative
					acc.scanJS(base, string(data), "ios-bundle:"+rel)
				}
			}
		case lower == "frameworks/app.framework/app":
			res.Meta.Framework = FrameworkFlutter
			if data, rerr := readBoundedFile(path, maxFile); rerr == nil {
				res.Stats.FilesScanned++
				if enc := CheckMachOEncryption(data); enc.Encrypted {
					acc.addWarning("Flutter App.framework is FairPlay encrypted; skipped binary string extraction")
				} else {
					acc.scanStringSlice(extractPrintableStrings(data, 6, 256, 150000), "flutter-ios:"+rel)
				}
			}
		case strings.HasPrefix(lower, "public/") && isScannableCodeAsset(lower):
			res.Meta.Framework = FrameworkCapacitor
			if data, rerr := readBoundedFile(path, maxFile); rerr == nil {
				res.Stats.FilesScanned++
				acc.scanJS(base, string(data), "capacitor:"+rel)
			}
		case strings.HasSuffix(lower, ".json") && !strings.Contains(lower, ".lproj/"):
			if data, rerr := readBoundedFile(path, min(maxFile, 2*1024*1024)); rerr == nil {
				res.Stats.FilesScanned++
				acc.scanJS(base, string(data), "ios-asset:"+rel)
			}
		}
		return nil
	})
}

func applyIOSPlistInfo(res *DiscoveryResult, acc *extractorAccumulator, pinfo PlistInfo) {
	if pinfo.AppName != "" {
		res.Meta.AppName = pinfo.AppName
	}
	res.Meta.BundleID = pinfo.BundleID
	res.Meta.Version = pinfo.Version
	res.Meta.BuildNumber = pinfo.BuildNumber
	res.Meta.ATSDomains = pinfo.ATSDomains
	for _, d := range pinfo.ATSDomains {
		acc.addBaseURLWithWeight("https://"+d, 2, "Info.plist")
	}
}

func extractExecutableNameFromPlist(data []byte) (string, error) {
	var root any
	var err error
	if bytes.HasPrefix(data, []byte(bplistMagic)) {
		root, err = parseBinaryPlist(data)
	} else {
		root, err = parseXMLPlist(data)
	}
	if err != nil {
		return "", err
	}
	dict, ok := root.(map[string]any)
	if !ok {
		return "", fmt.Errorf("plist root is not a dictionary")
	}
	return firstNonEmptyString(dict, "CFBundleExecutable"), nil
}

// safeBundleExecutablePath validates that CFBundleExecutable is a plain
// single-segment filename resolving strictly inside appDir, is a regular
// non-symlink file, and is not a forbidden credential path.
func safeBundleExecutablePath(appDir, execName string) (string, error) {
	execName = strings.TrimSpace(execName)
	if execName == "" || execName == "." || execName == ".." ||
		strings.ContainsAny(execName, "/\\\x00") ||
		filepath.Base(execName) != execName {
		return "", fmt.Errorf("must be a single filename inside the app bundle")
	}
	if IsForbiddenSecretPath(execName) {
		return "", fmt.Errorf("refusing forbidden secret path")
	}
	absDir, err := filepath.EvalSymlinks(appDir)
	if err != nil {
		return "", fmt.Errorf("resolving app bundle dir: %w", err)
	}
	candidate := filepath.Join(absDir, execName)
	lfi, err := os.Lstat(candidate)
	if err != nil {
		return "", err
	}
	if lfi.Mode()&os.ModeSymlink != 0 || !lfi.Mode().IsRegular() {
		return "", fmt.Errorf("executable must be a regular non-symlink file")
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("resolved path escapes app bundle")
	}
	if IsForbiddenSecretPath(resolved) {
		return "", fmt.Errorf("resolved path is forbidden")
	}
	return resolved, nil
}

func inspectIOSMachO(data []byte, rel string, res *DiscoveryResult, acc *extractorAccumulator) {
	enc := CheckMachOEncryption(data)
	if !enc.IsMachO {
		return
	}
	if enc.Encrypted {
		res.Meta.Encrypted = true
		res.Meta.EncryptionNote = enc.Detail
		acc.addWarning(enc.Detail)
		return
	}
	strs := extractPrintableStrings(data, 6, 256, 150000)
	acc.scanStringSlice(strs, "macho:"+rel)
}

func parseNetworkSecurityConfig(data []byte, fileName string) *PinningFinding {
	// Handles plain XML network_security_config.xml as well as AXML fallback
	// string search for pin-set / domain declarations.
	trimmed := bytes.TrimSpace(data)
	var domains []string
	hasPins := false

	if bytes.HasPrefix(trimmed, []byte("<")) {
		dec := xml.NewDecoder(bytes.NewReader(trimmed))
		inDomain := false
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				switch strings.ToLower(t.Name.Local) {
				case "pin-set", "pin":
					hasPins = true
				case "domain":
					inDomain = true
				}
			case xml.EndElement:
				if strings.EqualFold(t.Name.Local, "domain") {
					inDomain = false
				}
			case xml.CharData:
				if inDomain {
					d := strings.ToLower(strings.TrimSpace(string(t)))
					if d != "" && strings.Contains(d, ".") && !slices.Contains(domains, d) {
						domains = append(domains, d)
					}
				}
			}
		}
	} else {
		// Compiled Android Binary XML (AXML) stores tag names in UTF-16LE/UTF-8.
		if bytes.Contains(data, []byte("pin-set")) || bytes.Contains(data, []byte{'p', 0, 'i', 0, 'n', 0, '-', 0, 's', 0, 'e', 0, 't', 0}) {
			hasPins = true
		}
	}

	if !hasPins {
		return nil
	}
	sort.Strings(domains)
	return &PinningFinding{
		Kind:    "android-network-security-config",
		Detail:  fmt.Sprintf("Detected TLS pin-set in %s (MITM interception may fail without an unpinned build)", fileName),
		Domains: domains,
	}
}

func isUnsafeZipPath(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return true
	}
	cleaned := filepath.ToSlash(filepath.Clean(name))
	return strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.Contains(cleaned, "/../")
}

func readZipEntryBounded(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("entry %s exceeds size limit (%d > %d)", f.Name, f.UncompressedSize64, limit)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(io.LimitReader(rc, limit+1))
}

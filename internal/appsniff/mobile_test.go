package appsniff

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildSyntheticDEX constructs a valid DEX 035 binary containing:
//   - string_ids (including Retrofit annotations, REST paths, gRPC service path, and API URL)
//   - type_ids
//   - class_defs with annotations_directory_item -> method_annotations (@POST + @DELETE) + parameter_annotations (@Query + @Path)
func buildSyntheticDEX(t *testing.T) []byte {
	t.Helper()

	stringsList := []string{
		"Lapi/Service;",
		"Lretrofit2/http/POST;",
		"Lretrofit2/http/DELETE;",
		"Lretrofit2/http/Query;",
		"Lretrofit2/http/Path;",
		"value",
		"v1/users/{userId}/temperature",
		"v1/sessions/{sessionId}",
		"unit",
		"userId",
		"https://client-api.example-sleep.test/v1/status",
		"pb.api.endpoints.v1.TelemetryService/Report",
	}

	// Type descriptors map 1:1 to first 5 strings.
	typeStringIndices := []uint32{0, 1, 2, 3, 4}

	buf := make([]byte, 4096)
	copy(buf[0:8], []byte("dex\n035\x00"))

	// Lay out tables after 112-byte header.
	stringIDsOff := 112
	typeIDsOff := stringIDsOff + len(stringsList)*4
	classDefsOff := typeIDsOff + len(typeStringIndices)*4
	pos := classDefsOff + 32 // 1 class_def

	// Write string_data_items.
	for i, s := range stringsList {
		binary.LittleEndian.PutUint32(buf[stringIDsOff+i*4:], uint32(pos))
		buf[pos] = byte(len(s)) // ULEB128 for len < 128
		pos++
		copy(buf[pos:], s)
		pos += len(s)
		buf[pos] = 0
		pos++
	}
	// Align to 4 bytes.
	pos = (pos + 3) &^ 3

	// Write type_ids.
	for i, si := range typeStringIndices {
		binary.LittleEndian.PutUint32(buf[typeIDsOff+i*4:], si)
	}

	// Write annotation_item #1: @POST(value="v1/users/{userId}/temperature")
	annPostOff := pos
	buf[pos] = 0x01 // VISIBILITY_RUNTIME
	pos++
	buf[pos] = 1 // type_idx=1 (Lretrofit2/http/POST;)
	pos++
	buf[pos] = 1 // size=1 element
	pos++
	buf[pos] = 5 // name_idx=5 ("value")
	pos++
	buf[pos] = dexValueString // valType=string, valArg=0 (1 byte)
	pos++
	buf[pos] = 6 // string_idx=6 ("v1/users/{userId}/temperature")
	pos++

	// Write annotation_item #2: @DELETE(value="v1/sessions/{sessionId}")
	annDeleteOff := pos
	buf[pos] = 0x01
	pos++
	buf[pos] = 2 // type_idx=2 (Lretrofit2/http/DELETE;)
	pos++
	buf[pos] = 1
	pos++
	buf[pos] = 5
	pos++
	buf[pos] = dexValueString
	pos++
	buf[pos] = 7 // string_idx=7 ("v1/sessions/{sessionId}")
	pos++

	// Write annotation_item #3: @Query(value="unit")
	annQueryOff := pos
	buf[pos] = 0x01
	pos++
	buf[pos] = 3 // type_idx=3 (Lretrofit2/http/Query;)
	pos++
	buf[pos] = 1
	pos++
	buf[pos] = 5
	pos++
	buf[pos] = dexValueString
	pos++
	buf[pos] = 8 // string_idx=8 ("unit")
	pos++

	// Write annotation_item #4: @Path(value="userId")
	annPathOff := pos
	buf[pos] = 0x01
	pos++
	buf[pos] = 4 // type_idx=4 (Lretrofit2/http/Path;)
	pos++
	buf[pos] = 1
	pos++
	buf[pos] = 5
	pos++
	buf[pos] = dexValueString
	pos++
	buf[pos] = 9 // string_idx=9 ("userId")
	pos++

	pos = (pos + 3) &^ 3

	// annotation_set_item for method 0 (@POST)
	setPostOff := pos
	binary.LittleEndian.PutUint32(buf[pos:], 1)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(annPostOff))
	pos += 8

	// annotation_set_item for method 1 (@DELETE)
	setDeleteOff := pos
	binary.LittleEndian.PutUint32(buf[pos:], 1)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(annDeleteOff))
	pos += 8

	// annotation_set_item for param 0 (@Query("unit")) & param 1 (@Path("userId"))
	setQueryOff := pos
	binary.LittleEndian.PutUint32(buf[pos:], 1)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(annQueryOff))
	pos += 8

	setPathOff := pos
	binary.LittleEndian.PutUint32(buf[pos:], 1)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(annPathOff))
	pos += 8

	// annotation_set_ref_list for method 0 parameters (2 params)
	refListOff := pos
	binary.LittleEndian.PutUint32(buf[pos:], 2)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(setQueryOff))
	binary.LittleEndian.PutUint32(buf[pos+8:], uint32(setPathOff))
	pos += 12

	// annotations_directory_item
	annDirOff := pos
	binary.LittleEndian.PutUint32(buf[pos+0:], 0)  // class_annotations_off
	binary.LittleEndian.PutUint32(buf[pos+4:], 0)  // fields_size
	binary.LittleEndian.PutUint32(buf[pos+8:], 2)  // annotated_methods_size
	binary.LittleEndian.PutUint32(buf[pos+12:], 1) // annotated_parameters_size
	pos += 16
	// method_annotations[0]: method_idx=10 -> setPostOff
	binary.LittleEndian.PutUint32(buf[pos+0:], 10)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(setPostOff))
	pos += 8
	// method_annotations[1]: method_idx=11 -> setDeleteOff
	binary.LittleEndian.PutUint32(buf[pos+0:], 11)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(setDeleteOff))
	pos += 8
	// parameter_annotations[0]: method_idx=10 -> refListOff
	binary.LittleEndian.PutUint32(buf[pos+0:], 10)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(refListOff))
	pos += 8

	// Populate class_def[0]
	binary.LittleEndian.PutUint32(buf[classDefsOff+0:], 0)
	binary.LittleEndian.PutUint32(buf[classDefsOff+20:], uint32(annDirOff))

	// Populate DEX header counts/offsets.
	binary.LittleEndian.PutUint32(buf[32:36], uint32(pos))
	binary.LittleEndian.PutUint32(buf[56:60], uint32(len(stringsList)))
	binary.LittleEndian.PutUint32(buf[60:64], uint32(stringIDsOff))
	binary.LittleEndian.PutUint32(buf[64:68], uint32(len(typeStringIndices)))
	binary.LittleEndian.PutUint32(buf[68:72], uint32(typeIDsOff))
	binary.LittleEndian.PutUint32(buf[96:100], 1)
	binary.LittleEndian.PutUint32(buf[100:104], uint32(classDefsOff))

	return buf[:pos]
}

// buildSyntheticARSC constructs a minimal valid UTF-8 resources.arsc binary.
func buildSyntheticARSC(strs ...string) []byte {
	var strData bytes.Buffer
	offsets := make([]uint32, len(strs))
	for i, s := range strs {
		offsets[i] = uint32(strData.Len())
		strData.WriteByte(byte(len(s))) // charLen
		strData.WriteByte(byte(len(s))) // byteLen
		strData.WriteString(s)
		strData.WriteByte(0)
	}
	for strData.Len()%4 != 0 {
		strData.WriteByte(0)
	}

	poolHdrSize := 28
	offsetsSize := len(strs) * 4
	stringsStart := poolHdrSize + offsetsSize
	poolSize := stringsStart + strData.Len()

	var pool bytes.Buffer
	_ = binary.Write(&pool, binary.LittleEndian, resStringPoolType)
	_ = binary.Write(&pool, binary.LittleEndian, uint16(poolHdrSize))
	_ = binary.Write(&pool, binary.LittleEndian, uint32(poolSize))
	_ = binary.Write(&pool, binary.LittleEndian, uint32(len(strs)))
	_ = binary.Write(&pool, binary.LittleEndian, uint32(0)) // styleCount
	_ = binary.Write(&pool, binary.LittleEndian, resUTF8Flag)
	_ = binary.Write(&pool, binary.LittleEndian, uint32(stringsStart))
	_ = binary.Write(&pool, binary.LittleEndian, uint32(0)) // stylesStart
	for _, off := range offsets {
		_ = binary.Write(&pool, binary.LittleEndian, off)
	}
	pool.Write(strData.Bytes())

	var root bytes.Buffer
	_ = binary.Write(&root, binary.LittleEndian, resTableType)
	_ = binary.Write(&root, binary.LittleEndian, uint16(12))
	_ = binary.Write(&root, binary.LittleEndian, uint32(12+pool.Len()))
	_ = binary.Write(&root, binary.LittleEndian, uint32(1))
	root.Write(pool.Bytes())
	return root.Bytes()
}

func buildZipBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestDEXRetrofitAnnotationsAndARSCAndXAPK(t *testing.T) {
	t.Parallel()

	dexBytes := buildSyntheticDEX(t)
	arscBytes := buildSyntheticARSC("app_name", "https://client-api.example-sleep.test", "https://auth-api.example-sleep.test")

	// Also include a Hermes HBC bundle + network_security_config.xml.
	var hermesBuf bytes.Buffer
	_ = binary.Write(&hermesBuf, binary.LittleEndian, hermesMagic)
	hermesBuf.Write(make([]byte, 56))
	hermesBuf.WriteString("\x00https://app-api.example-sleep.test/v2/devices\x00")

	nscXML := `<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
  <domain-config>
    <domain includeSubdomains="true">client-api.example-sleep.test</domain>
    <pin-set><pin digest="SHA-256">AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=</pin></pin-set>
  </domain-config>
</network-security-config>`

	baseAPK := buildZipBytes(t, map[string][]byte{
		"classes.dex":                         dexBytes,
		"resources.arsc":                      arscBytes,
		"assets/index.android.bundle":         hermesBuf.Bytes(),
		"res/xml/network_security_config.xml": []byte(nscXML),
	})

	xapkManifest := `{"package_name":"com.example.sleep","name":"Example Sleep","version_name":"7.64.21","version_code":"76421"}`
	xapkBytes := buildZipBytes(t, map[string][]byte{
		"manifest.json":         []byte(xapkManifest),
		"com.example.sleep.apk": baseAPK,
	})

	xapkPath := filepath.Join(t.TempDir(), "com.example.sleep.xapk")
	require.NoError(t, os.WriteFile(xapkPath, xapkBytes, 0o644))

	res, err := DiscoverAndroid(xapkPath, Options{})
	require.NoError(t, err)
	assert.Equal(t, "Example Sleep", res.Meta.AppName)
	assert.Equal(t, "com.example.sleep", res.Meta.PackageID)
	assert.Equal(t, "7.64.21", res.Meta.Version)
	assert.Equal(t, FrameworkHermes, res.Meta.Framework)
	assert.Equal(t, 1, res.Stats.DEXFilesParsed)
	assert.Equal(t, 2, res.Stats.RetrofitMethods)
	assert.Equal(t, 3, res.Stats.ARSCStringsParsed)
	assert.Contains(t, res.PrimaryBaseURL, "example-sleep.test")

	// Verify exact Retrofit POST and DELETE methods + @Query("unit") were recovered!
	byPath := make(map[string]Endpoint)
	for _, ep := range res.Endpoints {
		byPath[ep.Path] = ep
	}
	tempEp, ok := byPath["/v1/users/{userId}/temperature"]
	require.True(t, ok, "expected Retrofit POST endpoint")
	assert.Equal(t, "POST", tempEp.Method)
	assert.Equal(t, "dex-retrofit", tempEp.Source)
	var hasUnitQuery bool
	for _, p := range tempEp.Params {
		if p.Name == "unit" && p.In == ParamInQuery {
			hasUnitQuery = true
		}
	}
	assert.True(t, hasUnitQuery, "expected @Query(unit) param from DEX parameter annotations")

	delEp, ok := byPath["/v1/sessions/{sessionId}"]
	require.True(t, ok, "expected Retrofit DELETE endpoint")
	assert.Equal(t, "DELETE", delEp.Method)

	// Verify gRPC endpoint was separated from REST endpoints by default.
	require.Len(t, res.GRPCEndpoints, 1)
	assert.Equal(t, ProtocolGRPC, res.GRPCEndpoints[0].Protocol)
	assert.Equal(t, "/pb.api.endpoints.v1.TelemetryService/Report", res.GRPCEndpoints[0].Path)

	// Verify TLS pinning detected cleanly.
	require.NotEmpty(t, res.Pinning)
	assert.Equal(t, "android-network-security-config", res.Pinning[0].Kind)
	assert.Contains(t, res.Pinning[0].Domains, "client-api.example-sleep.test")

	// Verify BuildSpec excludes gRPC by default and includes when IncludeGRPC=true.
	specDefault, err := BuildSpec(res, Options{})
	require.NoError(t, err)
	countEndpoints := func(s map[string]struct{ n int }) int { return len(s) }
	_ = countEndpoints
	defaultCount := 0
	for _, r := range specDefault.Resources {
		defaultCount += len(r.Endpoints)
	}
	specWithGRPC, err := BuildSpec(res, Options{IncludeGRPC: true})
	require.NoError(t, err)
	grpcCount := 0
	for _, r := range specWithGRPC.Resources {
		grpcCount += len(r.Endpoints)
	}
	assert.Equal(t, defaultCount+1, grpcCount)
}

func buildSyntheticMachO64(cryptID uint32, trailingStrings string) []byte {
	var buf bytes.Buffer
	// 32-byte mach_header_64 (ncmds=1, sizeofcmds=24)
	_ = binary.Write(&buf, binary.LittleEndian, machOMagic64)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0x0100000c)) // CPU_TYPE_ARM64
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(2)) // MH_EXECUTE
	_ = binary.Write(&buf, binary.LittleEndian, uint32(1)) // ncmds
	_ = binary.Write(&buf, binary.LittleEndian, uint32(24))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	// LC_ENCRYPTION_INFO_64 (24 bytes)
	_ = binary.Write(&buf, binary.LittleEndian, lcEncryptInfo64)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(24))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4096))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4096))
	_ = binary.Write(&buf, binary.LittleEndian, cryptID)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	buf.WriteString("\x00" + trailingStrings + "\x00")
	return buf.Bytes()
}

func TestDiscoverIOSFairPlayAndUnencrypted(t *testing.T) {
	t.Parallel()

	plistXML := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
	<key>CFBundleDisplayName</key><string>RideApp</string>
	<key>CFBundleIdentifier</key><string>com.example.ride</string>
	<key>CFBundleShortVersionString</key><string>2.1.0</string>
	<key>CFBundleExecutable</key><string>RideApp</string>
</dict></plist>`

	// Encrypted App Store binary: FairPlay detected, encrypted code skipped, JS bundle still scanned.
	encIPA := buildZipBytes(t, map[string][]byte{
		"Payload/RideApp.app/Info.plist":    []byte(plistXML),
		"Payload/RideApp.app/RideApp":       buildSyntheticMachO64(1, "https://secret.example-ride.test/v1/encrypted"),
		"Payload/RideApp.app/main.jsbundle": []byte(`fetch("https://api.example-ride.test/v1/rides", { method: "GET" })`),
	})
	encPath := filepath.Join(t.TempDir(), "encrypted.ipa")
	require.NoError(t, os.WriteFile(encPath, encIPA, 0o644))

	encRes, err := DiscoverIOS(encPath, Options{})
	require.NoError(t, err)
	assert.True(t, encRes.Meta.Encrypted)
	assert.Contains(t, encRes.Meta.EncryptionNote, "FairPlay")
	assert.Equal(t, "https://api.example-ride.test", encRes.PrimaryBaseURL)
	require.Len(t, encRes.Endpoints, 1)
	assert.Equal(t, "/v1/rides", encRes.Endpoints[0].Path)

	// Unencrypted simulator .app: Mach-O strings scanned cleanly.
	simApp := filepath.Join(t.TempDir(), "RideApp.app")
	require.NoError(t, os.MkdirAll(simApp, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(simApp, "Info.plist"), []byte(plistXML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(simApp, "RideApp"), buildSyntheticMachO64(0, "https://api.example-ride.test/v1/drivers"), 0o755))

	simRes, err := DiscoverIOS(simApp, Options{})
	require.NoError(t, err)
	assert.False(t, simRes.Meta.Encrypted)
	assert.Equal(t, "https://api.example-ride.test", simRes.PrimaryBaseURL)
	require.Len(t, simRes.Endpoints, 1)
	assert.Equal(t, "/v1/drivers", simRes.Endpoints[0].Path)
}

func TestValidatePackageIDAndAcquireHelpers(t *testing.T) {
	t.Parallel()

	assert.NoError(t, ValidatePackageID("com.example.app"))
	assert.NoError(t, ValidatePackageID("me.ride.android-prod"))
	assert.Error(t, ValidatePackageID("--flag-injection"))
	assert.Error(t, ValidatePackageID("com.example..evil"))
	assert.Error(t, ValidatePackageID("com.example/app"))
	assert.Error(t, ValidatePackageID("singleword"))

	// Test AcquireApkeep with fake runner.
	dest := t.TempDir()
	fakeRunner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		assert.Equal(t, "apkeep", name)
		assert.Equal(t, []string{"-a", "com.example.app", dest}, args)
		fakePath := filepath.Join(dest, "com.example.app.xapk")
		require.NoError(t, os.WriteFile(fakePath, []byte("PK"), 0o644))
		return []byte("ok"), nil
	}
	got, err := AcquireApkeep(context.Background(), "com.example.app", dest, fakeRunner)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dest, "com.example.app.xapk"), got)
}

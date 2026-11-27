package appsniff

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf16"
)

const (
	dexHeaderMinSize = 112

	// DEX encoded_value types (Dalvik Executable Format spec).
	dexValueByte       = 0x00
	dexValueShort      = 0x02
	dexValueChar       = 0x03
	dexValueInt        = 0x04
	dexValueLong       = 0x06
	dexValueFloat      = 0x10
	dexValueDouble     = 0x11
	dexValueMethodType = 0x15
	dexValueMethodHdl  = 0x16
	dexValueString     = 0x17
	dexValueType       = 0x18
	dexValueField      = 0x19
	dexValueMethod     = 0x1a
	dexValueEnum       = 0x1b
	dexValueArray      = 0x1c
	dexValueAnnotation = 0x1d
	dexValueNull       = 0x1e
	dexValueBoolean    = 0x1f

	// Android resources.arsc chunk types.
	resStringPoolType uint16 = 0x0001
	resTableType      uint16 = 0x0002
	resUTF8Flag       uint32 = 1 << 8
)

// RetrofitMethodAnnotation is a single HTTP endpoint recovered directly from
// DEX runtime annotations (@retrofit2.http.GET/POST/PUT/PATCH/DELETE/HEAD/HTTP
// plus @Query/@Path/@Header/@Field/@Body parameter annotations).
type RetrofitMethodAnnotation struct {
	Method      string
	Path        string
	Params      []EndpointParam
	HeaderNames []string
}

// IsDEX returns true if data starts with a valid Dalvik Executable magic header.
func IsDEX(data []byte) bool {
	return len(data) >= dexHeaderMinSize && bytes.HasPrefix(data, []byte("dex\n")) && data[7] == 0
}

// ExtractDEXStrings parses the exact DEX string_ids table with ULEB128 length
// decoding so strings are never corrupted by leading length bytes.
func ExtractDEXStrings(data []byte) ([]string, error) {
	if !IsDEX(data) {
		return nil, errors.New("not a valid dex binary")
	}
	stringIDsSize := int(binary.LittleEndian.Uint32(data[56:60]))
	stringIDsOff := int(binary.LittleEndian.Uint32(data[60:64]))
	if stringIDsSize <= 0 || stringIDsOff <= 0 || stringIDsOff+stringIDsSize*4 > len(data) {
		return nil, errors.New("invalid dex string_ids bounds")
	}

	out := make([]string, stringIDsSize)
	for i := range stringIDsSize {
		dataOff := int(binary.LittleEndian.Uint32(data[stringIDsOff+i*4 : stringIDsOff+i*4+4]))
		if dataOff <= 0 || dataOff >= len(data) {
			continue
		}
		_, pos, ok := readULEB128(data, dataOff)
		if !ok || pos >= len(data) {
			continue
		}
		end := bytes.IndexByte(data[pos:], 0)
		if end < 0 {
			continue
		}
		out[i] = string(data[pos : pos+end])
	}
	return out, nil
}

// ExtractDEXRetrofitEndpoints parses DEX class_defs -> annotations_directory_item
// -> method_annotations and parameter_annotations directly in binary DEX.
// Because Retrofit uses @Retention(RUNTIME), R8/ProGuard must retain these
// annotations even in heavily obfuscated release APKs, giving exact HTTP
// methods, paths, and query/path/header parameter names without running jadx.
func ExtractDEXRetrofitEndpoints(data []byte) ([]RetrofitMethodAnnotation, []string, error) {
	stringsTable, err := ExtractDEXStrings(data)
	if err != nil {
		return nil, nil, err
	}

	typeIDsSize := int(binary.LittleEndian.Uint32(data[64:68]))
	typeIDsOff := int(binary.LittleEndian.Uint32(data[68:72]))
	classDefsSize := int(binary.LittleEndian.Uint32(data[96:100]))
	classDefsOff := int(binary.LittleEndian.Uint32(data[100:104]))

	if typeIDsSize <= 0 || typeIDsOff <= 0 || typeIDsOff+typeIDsSize*4 > len(data) {
		return nil, stringsTable, nil
	}
	if classDefsSize <= 0 || classDefsOff <= 0 || classDefsOff+classDefsSize*32 > len(data) {
		return nil, stringsTable, nil
	}

	typeDesc := func(typeIdx uint32) string {
		if int(typeIdx) >= typeIDsSize {
			return ""
		}
		off := typeIDsOff + int(typeIdx)*4
		descIdx := binary.LittleEndian.Uint32(data[off : off+4])
		if int(descIdx) >= len(stringsTable) {
			return ""
		}
		return stringsTable[descIdx]
	}

	stringAt := func(idx uint32) string {
		if int(idx) >= len(stringsTable) {
			return ""
		}
		return stringsTable[idx]
	}

	var results []RetrofitMethodAnnotation

	for i := range classDefsSize {
		cdOff := classDefsOff + i*32
		annDirOff := int(binary.LittleEndian.Uint32(data[cdOff+20 : cdOff+24]))
		if annDirOff <= 0 || annDirOff+16 > len(data) {
			continue
		}

		// annotations_directory_item:
		//   [0..3]  class_annotations_off
		//   [4..7]  fields_size
		//   [8..11] annotated_methods_size
		//   [12..15] annotated_parameters_size
		fieldsSize := int(binary.LittleEndian.Uint32(data[annDirOff+4 : annDirOff+8]))
		methodsSize := int(binary.LittleEndian.Uint32(data[annDirOff+8 : annDirOff+12]))
		paramsSize := int(binary.LittleEndian.Uint32(data[annDirOff+12 : annDirOff+16]))
		if methodsSize == 0 {
			continue
		}

		methodsOff := annDirOff + 16 + fieldsSize*8
		paramsOff := methodsOff + methodsSize*8
		if methodsOff < 0 || paramsOff > len(data) || paramsOff+paramsSize*8 > len(data) {
			continue
		}

		// Index parameter annotations by method_idx for this class.
		paramAnnByMethod := make(map[uint32]int, paramsSize)
		for p := range paramsSize {
			pOff := paramsOff + p*8
			mIdx := binary.LittleEndian.Uint32(data[pOff : pOff+4])
			refListOff := int(binary.LittleEndian.Uint32(data[pOff+4 : pOff+8]))
			paramAnnByMethod[mIdx] = refListOff
		}

		for m := range methodsSize {
			mOff := methodsOff + m*8
			mIdx := binary.LittleEndian.Uint32(data[mOff : mOff+4])
			annSetOff := int(binary.LittleEndian.Uint32(data[mOff+4 : mOff+8]))

			parsed := parseMethodRetrofitAnnotationSet(data, annSetOff, typeDesc, stringAt)
			if parsed == nil {
				continue
			}

			if refListOff, ok := paramAnnByMethod[mIdx]; ok && refListOff > 0 {
				parsed.Params = append(parsed.Params, parseParamAnnotationSetRefList(data, refListOff, typeDesc, stringAt)...)
			}
			results = append(results, *parsed)
		}
	}

	return results, stringsTable, nil
}

func parseMethodRetrofitAnnotationSet(
	data []byte,
	annSetOff int,
	typeDesc func(uint32) string,
	stringAt func(uint32) string,
) *RetrofitMethodAnnotation {
	if annSetOff <= 0 || annSetOff+4 > len(data) {
		return nil
	}
	count := int(binary.LittleEndian.Uint32(data[annSetOff : annSetOff+4]))
	if count <= 0 || count > 64 || annSetOff+4+count*4 > len(data) {
		return nil
	}

	var method, path string
	var headers []string

	for i := range count {
		itemOff := int(binary.LittleEndian.Uint32(data[annSetOff+4+i*4 : annSetOff+8+i*4]))
		if itemOff <= 0 || itemOff+2 >= len(data) {
			continue
		}
		// annotation_item: visibility (1 byte) + encoded_annotation.
		annType, fields, ok := parseEncodedAnnotation(data, itemOff+1, typeDesc, stringAt)
		if !ok {
			continue
		}
		switch annType {
		case "Lretrofit2/http/GET;", "Lretrofit/http/GET;":
			method = "GET"
			path = firstStringField(fields, "value")
		case "Lretrofit2/http/POST;", "Lretrofit/http/POST;":
			method = "POST"
			path = firstStringField(fields, "value")
		case "Lretrofit2/http/PUT;", "Lretrofit/http/PUT;":
			method = "PUT"
			path = firstStringField(fields, "value")
		case "Lretrofit2/http/PATCH;", "Lretrofit/http/PATCH;":
			method = "PATCH"
			path = firstStringField(fields, "value")
		case "Lretrofit2/http/DELETE;", "Lretrofit/http/DELETE;":
			method = "DELETE"
			path = firstStringField(fields, "value")
		case "Lretrofit2/http/HEAD;", "Lretrofit/http/HEAD;":
			method = "HEAD"
			path = firstStringField(fields, "value")
		case "Lretrofit2/http/OPTIONS;", "Lretrofit/http/OPTIONS;":
			method = "OPTIONS"
			path = firstStringField(fields, "value")
		case "Lretrofit2/http/HTTP;", "Lretrofit/http/HTTP;":
			if hm := firstStringField(fields, "method"); hm != "" {
				method = strings.ToUpper(hm)
			}
			path = firstStringField(fields, "path")
		case "Lretrofit2/http/Headers;", "Lretrofit/http/Headers;":
			for _, h := range fields["value"] {
				// Record only header name, never literal header values.
				if colon := strings.IndexByte(h, ':'); colon > 0 {
					hName := strings.ToLower(strings.TrimSpace(h[:colon]))
					if hName != "" {
						headers = append(headers, hName)
					}
				}
			}
		}
	}

	if method == "" || strings.TrimSpace(path) == "" {
		return nil
	}
	return &RetrofitMethodAnnotation{
		Method:      method,
		Path:        strings.TrimSpace(path),
		HeaderNames: headers,
	}
}

func parseParamAnnotationSetRefList(
	data []byte,
	refListOff int,
	typeDesc func(uint32) string,
	stringAt func(uint32) string,
) []EndpointParam {
	if refListOff <= 0 || refListOff+4 > len(data) {
		return nil
	}
	paramCount := int(binary.LittleEndian.Uint32(data[refListOff : refListOff+4]))
	if paramCount <= 0 || paramCount > 64 || refListOff+4+paramCount*4 > len(data) {
		return nil
	}

	var out []EndpointParam
	for p := range paramCount {
		annSetOff := int(binary.LittleEndian.Uint32(data[refListOff+4+p*4 : refListOff+8+p*4]))
		if annSetOff <= 0 || annSetOff+4 > len(data) {
			continue
		}
		annCount := int(binary.LittleEndian.Uint32(data[annSetOff : annSetOff+4]))
		if annCount <= 0 || annCount > 32 || annSetOff+4+annCount*4 > len(data) {
			continue
		}
		for a := range annCount {
			itemOff := int(binary.LittleEndian.Uint32(data[annSetOff+4+a*4 : annSetOff+8+a*4]))
			if itemOff <= 0 || itemOff+2 >= len(data) {
				continue
			}
			annType, fields, ok := parseEncodedAnnotation(data, itemOff+1, typeDesc, stringAt)
			if !ok {
				continue
			}
			val := firstStringField(fields, "value")
			switch annType {
			case "Lretrofit2/http/Query;", "Lretrofit/http/Query;":
				if val != "" {
					out = append(out, EndpointParam{Name: val, In: ParamInQuery, Type: "string"})
				}
			case "Lretrofit2/http/Path;", "Lretrofit/http/Path;":
				if val != "" {
					out = append(out, EndpointParam{Name: val, In: ParamInPath, Type: "string", Required: true})
				}
			case "Lretrofit2/http/Header;", "Lretrofit/http/Header;":
				if val != "" {
					out = append(out, EndpointParam{Name: strings.ToLower(val), In: ParamInHeader, Type: "string"})
				}
			case "Lretrofit2/http/Field;", "Lretrofit/http/Field;":
				if val != "" {
					out = append(out, EndpointParam{Name: val, In: ParamInBody, Type: "string"})
				}
			}
		}
	}
	return out
}

func firstStringField(fields map[string][]string, name string) string {
	vals := fields[name]
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func parseEncodedAnnotation(
	data []byte,
	pos int,
	typeDesc func(uint32) string,
	stringAt func(uint32) string,
) (string, map[string][]string, bool) {
	typeIdx, pos, ok := readULEB128(data, pos)
	if !ok {
		return "", nil, false
	}
	size, pos, ok := readULEB128(data, pos)
	if !ok || size > 64 {
		return "", nil, false
	}

	desc := typeDesc(typeIdx)
	if desc == "" {
		return "", nil, false
	}
	fields := make(map[string][]string, size)
	for range size {
		nameIdx, nextPos, ok := readULEB128(data, pos)
		if !ok {
			return desc, fields, true
		}
		pos = nextPos
		fieldName := stringAt(nameIdx)
		vals, nextPos, ok := decodeEncodedValue(data, pos, stringAt, 0)
		if !ok {
			return desc, fields, true
		}
		pos = nextPos
		if len(vals) > 0 && fieldName != "" {
			fields[fieldName] = append(fields[fieldName], vals...)
		}
	}
	return desc, fields, true
}

func decodeEncodedValue(data []byte, pos int, stringAt func(uint32) string, depth int) ([]string, int, bool) {
	if pos >= len(data) || depth > 4 {
		return nil, pos, false
	}
	header := data[pos]
	pos++
	valType := header & 0x1f
	valArg := int((header >> 5) & 0x07)

	switch valType {
	case dexValueByte:
		if pos+1 > len(data) {
			return nil, pos, false
		}
		return nil, pos + 1, true
	case dexValueShort, dexValueChar, dexValueInt, dexValueLong, dexValueFloat, dexValueDouble,
		dexValueMethodType, dexValueMethodHdl, dexValueType, dexValueField, dexValueMethod, dexValueEnum:
		n := valArg + 1
		if pos+n > len(data) {
			return nil, pos, false
		}
		return nil, pos + n, true
	case dexValueString:
		n := valArg + 1
		if pos+n > len(data) || n > 4 {
			return nil, pos, false
		}
		var idx uint32
		for i := range n {
			idx |= uint32(data[pos+i]) << (8 * i)
		}
		return []string{stringAt(idx)}, pos + n, true
	case dexValueArray:
		count, nextPos, ok := readULEB128(data, pos)
		if !ok || count > 256 {
			return nil, pos, false
		}
		pos = nextPos
		var out []string
		for range count {
			vals, nextPos, ok := decodeEncodedValue(data, pos, stringAt, depth+1)
			if !ok {
				return out, pos, false
			}
			pos = nextPos
			out = append(out, vals...)
		}
		return out, pos, true
	case dexValueAnnotation:
		// Skip nested annotation.
		_, nextPos, ok := readULEB128(data, pos)
		if !ok {
			return nil, pos, false
		}
		count, nextPos, ok := readULEB128(data, nextPos)
		if !ok || count > 64 {
			return nil, pos, false
		}
		pos = nextPos
		for range count {
			_, nextPos, ok = readULEB128(data, pos)
			if !ok {
				return nil, pos, false
			}
			_, nextPos, ok = decodeEncodedValue(data, nextPos, stringAt, depth+1)
			if !ok {
				return nil, pos, false
			}
			pos = nextPos
		}
		return nil, pos, true
	case dexValueNull, dexValueBoolean:
		return nil, pos, true
	default:
		return nil, pos, false
	}
}

func readULEB128(data []byte, pos int) (uint32, int, bool) {
	var result uint32
	var shift uint
	for i := range 5 {
		if pos+i >= len(data) {
			return 0, pos, false
		}
		b := data[pos+i]
		result |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return result, pos + i + 1, true
		}
		shift += 7
	}
	return 0, pos, false
}

// ExtractARSCBaseURLs reads the global UTF-8 or UTF-16 string pool from an
// Android binary resources.arsc file and returns discovered HTTPS URL strings.
// Production Android apps commonly store API base URLs (e.g. client-api.*,
// auth-api.*) in res/values/strings.xml which compiles into resources.arsc.
func ExtractARSCBaseURLs(data []byte) ([]string, int) {
	if len(data) < 12 {
		return nil, 0
	}
	rootType := binary.LittleEndian.Uint16(data[0:2])
	rootHdrSize := int(binary.LittleEndian.Uint16(data[2:4]))
	if rootType != resTableType || rootHdrSize < 12 || rootHdrSize >= len(data) {
		return nil, 0
	}

	// Global string pool chunk immediately follows the RES_TABLE header.
	poolOff := rootHdrSize
	if poolOff+28 > len(data) {
		return nil, 0
	}
	poolType := binary.LittleEndian.Uint16(data[poolOff : poolOff+2])
	poolHdrSize := int(binary.LittleEndian.Uint16(data[poolOff+2 : poolOff+4]))
	poolSize := int(binary.LittleEndian.Uint32(data[poolOff+4 : poolOff+8]))
	if poolType != resStringPoolType || poolHdrSize < 28 || poolOff+poolSize > len(data) {
		return nil, 0
	}

	stringCount := int(binary.LittleEndian.Uint32(data[poolOff+8 : poolOff+12]))
	flags := binary.LittleEndian.Uint32(data[poolOff+16 : poolOff+20])
	stringsStart := int(binary.LittleEndian.Uint32(data[poolOff+20 : poolOff+24]))
	isUTF8 := flags&resUTF8Flag != 0

	offsetsBase := poolOff + poolHdrSize
	stringsBase := poolOff + stringsStart
	if stringCount <= 0 || offsetsBase+stringCount*4 > poolOff+poolSize || stringsBase >= poolOff+poolSize {
		return nil, 0
	}

	var urls []string
	seen := make(map[string]bool)
	for i := range stringCount {
		relOff := int(binary.LittleEndian.Uint32(data[offsetsBase+i*4 : offsetsBase+i*4+4]))
		pos := stringsBase + relOff
		if pos < stringsBase || pos >= poolOff+poolSize {
			continue
		}
		var s string
		if isUTF8 {
			s = readARSCUTF8String(data, pos, poolOff+poolSize)
		} else {
			s = readARSCUTF16String(data, pos, poolOff+poolSize)
		}
		if strings.Contains(s, "https://") {
			for _, u := range reAbsHTTPSURL.FindAllString(s, 4) {
				if !seen[u] {
					seen[u] = true
					urls = append(urls, u)
				}
			}
		}
	}
	return urls, stringCount
}

func readARSCUTF8String(data []byte, pos, limit int) string {
	// UTF-8 ARSC entry: charLen (1-2 bytes) + byteLen (1-2 bytes) + utf8 bytes + NUL.
	if pos >= limit {
		return ""
	}
	if data[pos]&0x80 != 0 {
		pos += 2
	} else {
		pos++
	}
	if pos >= limit {
		return ""
	}
	var byteLen int
	if data[pos]&0x80 != 0 {
		if pos+1 >= limit {
			return ""
		}
		byteLen = (int(data[pos]&0x7f) << 8) | int(data[pos+1])
		pos += 2
	} else {
		byteLen = int(data[pos])
		pos++
	}
	if byteLen <= 0 || pos+byteLen > limit {
		return ""
	}
	return string(data[pos : pos+byteLen])
}

func readARSCUTF16String(data []byte, pos, limit int) string {
	if pos+2 > limit {
		return ""
	}
	charLen := int(binary.LittleEndian.Uint16(data[pos : pos+2]))
	pos += 2
	if charLen&0x8000 != 0 {
		if pos+2 > limit {
			return ""
		}
		low := int(binary.LittleEndian.Uint16(data[pos : pos+2]))
		charLen = ((charLen & 0x7fff) << 16) | low
		pos += 2
	}
	if charLen <= 0 || pos+charLen*2 > limit {
		return ""
	}
	u16 := make([]uint16, charLen)
	for i := range charLen {
		u16[i] = binary.LittleEndian.Uint16(data[pos+i*2 : pos+i*2+2])
	}
	return string(utf16.Decode(u16))
}

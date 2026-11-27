package appsniff

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf16"
)

const (
	bplistMagic           = "bplist00"
	bplistTrailerSize     = 32
	bplistMaxObjects      = 200000
	bplistMaxDecodedNodes = 200000
	bplistMaxDepth        = 32
)

// parseBinaryPlist decodes Apple binary plist (bplist00) payloads in pure Go
// without invoking external tools such as plutil.
func parseBinaryPlist(data []byte) (any, error) {
	if len(data) < len(bplistMagic)+bplistTrailerSize || !bytes.HasPrefix(data, []byte(bplistMagic)) {
		return nil, errors.New("not a bplist00 payload")
	}

	trailer := data[len(data)-bplistTrailerSize:]
	offsetIntSize := int(trailer[6])
	objectRefSize := int(trailer[7])
	numObjects := binary.BigEndian.Uint64(trailer[8:16])
	topObject := binary.BigEndian.Uint64(trailer[16:24])
	offsetTableOffset := binary.BigEndian.Uint64(trailer[24:32])

	if offsetIntSize != 1 && offsetIntSize != 2 && offsetIntSize != 4 && offsetIntSize != 8 {
		return nil, fmt.Errorf("invalid bplist offset int size %d", offsetIntSize)
	}
	if objectRefSize != 1 && objectRefSize != 2 && objectRefSize != 4 && objectRefSize != 8 {
		return nil, fmt.Errorf("invalid bplist object ref size %d", objectRefSize)
	}
	if numObjects == 0 || numObjects > bplistMaxObjects || topObject >= numObjects {
		return nil, fmt.Errorf("invalid bplist object counts (num=%d, top=%d)", numObjects, topObject)
	}
	tableBytes := numObjects * uint64(offsetIntSize)
	trailerStart := uint64(len(data) - bplistTrailerSize)
	if offsetTableOffset < uint64(len(bplistMagic)) || offsetTableOffset > trailerStart || tableBytes > trailerStart-offsetTableOffset {
		return nil, errors.New("bplist offset table out of bounds")
	}

	offsets := make([]uint64, numObjects)
	for i := range numObjects {
		pos := int(offsetTableOffset) + int(i)*offsetIntSize
		off := readBigEndianUint(data[pos : pos+offsetIntSize])
		if off < uint64(len(bplistMagic)) || off >= offsetTableOffset {
			return nil, fmt.Errorf("bplist object %d offset out of bounds", i)
		}
		offsets[i] = off
	}

	dec := &bplistDecoder{
		data:          data,
		offsets:       offsets,
		objectRefSize: objectRefSize,
		limit:         int(offsetTableOffset),
		cache:         make(map[uint64]any),
		visiting:      make(map[uint64]bool),
	}
	return dec.decodeObject(topObject, 0)
}

type bplistDecoder struct {
	data          []byte
	offsets       []uint64
	objectRefSize int
	limit         int
	cache         map[uint64]any
	visiting      map[uint64]bool
	decodedNodes  int
}

func (d *bplistDecoder) decodeObject(ref uint64, depth int) (any, error) {
	if depth > bplistMaxDepth {
		return nil, errors.New("bplist nesting too deep")
	}
	if ref >= uint64(len(d.offsets)) {
		return nil, fmt.Errorf("bplist object ref %d out of range", ref)
	}
	d.decodedNodes++
	if d.decodedNodes > bplistMaxDecodedNodes {
		return nil, errors.New("bplist decoded node budget exceeded")
	}
	if d.visiting[ref] {
		return nil, fmt.Errorf("bplist cyclic object reference %d", ref)
	}
	if cached, ok := d.cache[ref]; ok {
		return cached, nil
	}
	d.visiting[ref] = true
	defer delete(d.visiting, ref)

	val, err := d.decodeObjectUncached(ref, depth)
	if err != nil {
		return nil, err
	}
	d.cache[ref] = val
	return val, nil
}

func (d *bplistDecoder) decodeObjectUncached(ref uint64, depth int) (any, error) {
	pos := int(d.offsets[ref])
	if pos >= d.limit {
		return nil, errors.New("bplist object offset out of bounds")
	}
	marker := d.data[pos]
	hi := marker >> 4
	lo := int(marker & 0x0f)
	pos++

	switch hi {
	case 0x0:
		switch lo {
		case 0x0, 0x0f:
			return nil, nil
		case 0x08:
			return false, nil
		case 0x09:
			return true, nil
		default:
			return nil, nil
		}

	case 0x1: // integer (2^lo bytes)
		n := 1 << lo
		if n > 8 || pos+n > d.limit {
			return nil, errors.New("bplist integer out of bounds")
		}
		return int64(readBigEndianUint(d.data[pos : pos+n])), nil

	case 0x2: // real
		n := 1 << lo
		if pos+n > d.limit {
			return nil, errors.New("bplist real out of bounds")
		}
		switch n {
		case 4:
			return float64(math.Float32frombits(binary.BigEndian.Uint32(d.data[pos : pos+4]))), nil
		case 8:
			return math.Float64frombits(binary.BigEndian.Uint64(d.data[pos : pos+8])), nil
		default:
			return nil, nil
		}

	case 0x3: // date (8-byte float64)
		if pos+8 > d.limit {
			return nil, errors.New("bplist date out of bounds")
		}
		return math.Float64frombits(binary.BigEndian.Uint64(d.data[pos : pos+8])), nil

	case 0x4: // raw data
		count, nextPos, err := d.readCount(lo, pos)
		if err != nil || count > uint64(d.limit-nextPos) {
			return nil, errors.New("bplist data out of bounds")
		}
		return string(d.data[nextPos : nextPos+int(count)]), nil

	case 0x5: // ASCII string
		count, nextPos, err := d.readCount(lo, pos)
		if err != nil || count > uint64(d.limit-nextPos) {
			return nil, errors.New("bplist ascii string out of bounds")
		}
		return string(d.data[nextPos : nextPos+int(count)]), nil

	case 0x6: // UTF-16BE string
		count, nextPos, err := d.readCount(lo, pos)
		if err != nil || count > uint64((d.limit-nextPos)/2) {
			return nil, errors.New("bplist utf16 string out of bounds")
		}
		u16 := make([]uint16, count)
		for i := range count {
			u16[i] = binary.BigEndian.Uint16(d.data[nextPos+int(i)*2 : nextPos+int(i)*2+2])
		}
		return string(utf16.Decode(u16)), nil

	case 0x8: // UID
		n := lo + 1
		if n > 8 || pos+n > d.limit {
			return nil, errors.New("bplist uid out of bounds")
		}
		return readBigEndianUint(d.data[pos : pos+n]), nil

	case 0xa, 0xc: // array / set
		count, nextPos, err := d.readCount(lo, pos)
		if err != nil || count > bplistMaxObjects {
			return nil, errors.New("bplist array count invalid")
		}
		need := count * uint64(d.objectRefSize)
		if need > uint64(d.limit-nextPos) {
			return nil, errors.New("bplist array refs out of bounds")
		}
		out := make([]any, 0, count)
		for i := range count {
			rPos := nextPos + int(i)*d.objectRefSize
			elemRef := readBigEndianUint(d.data[rPos : rPos+d.objectRefSize])
			val, err := d.decodeObject(elemRef, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, val)
		}
		return out, nil

	case 0xd: // dict
		count, nextPos, err := d.readCount(lo, pos)
		if err != nil || count > bplistMaxObjects {
			return nil, errors.New("bplist dict count invalid")
		}
		need := count * 2 * uint64(d.objectRefSize)
		if need > uint64(d.limit-nextPos) {
			return nil, errors.New("bplist dict refs out of bounds")
		}
		valsStart := nextPos + int(count)*d.objectRefSize
		out := make(map[string]any, count)
		for i := range count {
			kPos := nextPos + int(i)*d.objectRefSize
			vPos := valsStart + int(i)*d.objectRefSize
			kRef := readBigEndianUint(d.data[kPos : kPos+d.objectRefSize])
			vRef := readBigEndianUint(d.data[vPos : vPos+d.objectRefSize])
			kVal, err := d.decodeObject(kRef, depth+1)
			if err != nil {
				return nil, err
			}
			keyStr, ok := kVal.(string)
			if !ok {
				continue
			}
			val, err := d.decodeObject(vRef, depth+1)
			if err != nil {
				return nil, err
			}
			out[keyStr] = val
		}
		return out, nil

	default:
		return nil, nil
	}
}

func (d *bplistDecoder) readCount(lo, pos int) (uint64, int, error) {
	if lo != 0x0f {
		return uint64(lo), pos, nil
	}
	if pos >= d.limit {
		return 0, pos, errors.New("bplist extended count out of bounds")
	}
	intMarker := d.data[pos]
	if intMarker>>4 != 0x1 {
		return 0, pos, errors.New("bplist extended count is not an integer")
	}
	n := 1 << int(intMarker&0x0f)
	pos++
	if n > 8 || pos+n > d.limit {
		return 0, pos, errors.New("bplist extended count int out of bounds")
	}
	return readBigEndianUint(d.data[pos : pos+n]), pos + n, nil
}

func readBigEndianUint(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = (v << 8) | uint64(x)
	}
	return v
}

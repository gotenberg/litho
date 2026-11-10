package build

// Port of jp2.go.
//
// JP2 containers around a fixed JPEG 2000 codestream.
//
// ISO 19005-2, 6.2.8.3 states five tests over the JPEG 2000 data of a
// `JPXDecode` image, and every one of them reads a box of the JP2 container
// rather than the codestream: the channel count and bit depth from `ihdr` and
// `bpcc`, the colour specification from `colr`. This module writes those
// boxes, so a fixture plants each defect by naming a value rather than by
// re-encoding an image.
//
// The codestreams are embedded. A JPEG 2000 codestream is entropy coded, which
// no builder writes by hand. They were produced once with OpenJPEG:
//
//	python3 -c "open('raw2.raw','wb').write(bytes([50]*64 + [200]*64))"
//	opj_compress -i raw2.raw -o two.j2k -F 8,8,2,8,u -n 1 -r 1
//
// and the three-component one from a 8x8x3 raw of the same shape. The
// optional `COM` marker OpenJPEG writes carries its own version string, so it
// is stripped: the bytes here do not change when the encoder is upgraded.
//
// Both are 8x8 at 8 bits per component. `TWO` has two components, which no
// conforming file may have and which no ordinary encoder emits, and `THREE`
// has three.

import (
	"encoding/base64"
	"encoding/binary"
	"slices"
)

// jp2Two is the two-component codestream. Treat it as read-only.
var jp2Two = jp2MustDecode(
	"/0//UQAsAAAAAAAIAAAACAAAAAAAAAAAAAAACAAAAAgAAAAAAAAAAAACBwEBBwEB/1IA" +
		"DAAAAAEAAAQEAAH/XAAEQED/kAAKAAAAAAA5AAH/k8+0TBFQVKNlXbAAAwkJfXzMAADC" +
		"Qj/PtEgUAFyjZV2wAAMJCNUKGEhL/3//2Q==",
)

// jp2Three is the three-component codestream. Treat it as
// read-only.
var jp2Three = jp2MustDecode(
	"/0//UQAvAAAAAAAIAAAACAAAAAAAAAAAAAAACAAAAAgAAAAAAAAAAAADBwEBBwEBBwEB" +
		"/1IADAAAAAEBAAQEAAH/XAAEQED/kAAKAAAAAAA+AAH/k8PnGBFQVK+CPUgAAYSEv8fU" +
		"GBFQVKJ9tpAAAwkJf8+0PBQAXK+FDRQAAAAYSEv/f//Z",
)

// jp2Size is the codestreams' width and height.
const jp2Size = 8

// ISO/IEC 15444-1, Table I.9: an enumerated colour space of 16 is sRGB and 19
// is CIEJab, which ISO 19005-2, 6.2.8.3 test 4 forbids by name.
const (
	jp2EnumSRGB   = 16
	jp2EnumCIEJab = 19
)

// jp2MustDecode decodes a constant base64 literal.
func jp2MustDecode(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// jp2U32 appends v as struct's ">I": its low 32 bits, big-endian.
func jp2U32(b []byte, v int) []byte {
	return binary.BigEndian.AppendUint32(b, uint32(v)) //nolint:gosec // struct packing keeps the field width; fixture values fit.
}

// jp2Box is one JP2 box, a 32-bit length covering the header, the type,
// the payload.
func jp2Box(tag string, payload []byte) []byte {
	out := jp2U32(nil, 8+len(payload))
	out = append(out, tag...)
	return append(out, payload...)
}

// jp2Ihdr is an image header box of 8-bit components. The depth is
// stored one less than its value.
func jp2Ihdr(components int) []byte {
	const depth = 8
	payload := jp2U32(nil, jp2Size)
	payload = jp2U32(payload, jp2Size)
	payload = binary.BigEndian.AppendUint16(payload, uint16(components)) //nolint:gosec // struct packing keeps the field width; fixture values fit.
	payload = append(payload, depth-1, 7, 0, 0)
	return jp2Box("ihdr", payload)
}

// jp2Colr is one colour specification box, ISO/IEC 15444-1, I.5.3.3.
//
// METH 1 names an enumerated colour space, METH 2 embeds an ICC profile. Any
// other value is what ISO 19005-2, 6.2.8.3 test 3 rejects, and the box then
// carries no colour data at all. enumCS is written only when meth is 1, and
// ignored otherwise.
func jp2Colr(meth, approx, enumCS int) []byte {
	payload := []byte{byte(meth), 0, byte(approx)} //nolint:gosec // struct packing keeps the field width; fixture values fit.
	if meth == 1 {
		payload = jp2U32(payload, enumCS)
	}
	return jp2Box("colr", payload)
}

// jp2Bpcc is a bits-per-component box, one byte per component, each one
// less.
func jp2Bpcc(depths []int) []byte {
	payload := make([]byte, 0, len(depths))
	for _, d := range depths {
		payload = append(payload, byte(d-1)) //nolint:gosec // struct packing keeps the field width; fixture values fit.
	}
	return jp2Box("bpcc", payload)
}

// jp2Container is a JP2 file, signature, file type, the header
// superbox, the codestream. A nil depths writes no bpcc box; a non-nil empty
// one writes an empty box.
func jp2Container(codestream []byte, components int, colrs [][]byte, depths []int) []byte {
	header := jp2Ihdr(components)
	if depths != nil {
		header = append(header, jp2Bpcc(depths)...)
	}
	for _, spec := range colrs {
		header = append(header, spec...)
	}
	signature := append(jp2U32(nil, 12), "jP  \x0d\x0a\x87\x0a"...)
	fileType := jp2Box("ftyp", append(jp2U32([]byte("jp2 "), 0), "jp2 "...))
	return slices.Concat(signature, fileType, jp2Box("jp2h", header), jp2Box("jp2c", codestream))
}

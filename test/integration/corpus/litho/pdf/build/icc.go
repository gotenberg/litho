package build

// Port of icc.go.
//
// Version 2.1 ICC destination profiles for the output-intent fixtures.
//
// Every part of PDF/A admits a version 2 profile of the printer or monitor
// device class as an output intent's destination profile (ISO 19005-1, 6.2.2;
// ISO 19005-2/3/4, 6.2.3 t1). The profiles carry real transforms: the RGB one
// a matrix and gamma 2.2 curves over an XYZ connection space, the CMYK one
// lut8 A2B0 and B2A0 tables over Lab. They are the profiles twoprofile_1b.pdf
// carries, written by the same code.

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// iccVersion21 is the profile version 2.1 header field.
const iccVersion21 = 0x02100000

// iccD50 is the D50 white point in XYZ. Treat it as read-only.
var iccD50 = []float64{0.9642, 1.0, 0.8249}

// iccIdentity is the 3 x 3 identity matrix. Treat it as read-only.
var iccIdentity = []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}

// iccPrimary is one entry of iccPrimaries: a colorant tag and its XYZ.
type iccPrimary struct {
	Sig string
	XYZ []float64
}

// iccPrimaries are the sRGB primaries adapted to D50, the columns of its
// RGB-to-XYZ matrix, in the order the profiles write them. Treat it as
// read-only.
var iccPrimaries = []iccPrimary{
	{"rXYZ", []float64{0.4361, 0.2225, 0.0139}},
	{"gXYZ", []float64{0.3851, 0.7169, 0.0971}},
	{"bXYZ", []float64{0.1431, 0.0606, 0.7141}},
}

// iccTag is one (signature, data) pair of iccProfile's tags.
type iccTag struct {
	Sig  string
	Data []byte
}

// iccCommonTags are the tags every profile carries. Treat it as read-only:
// build a tag list with append on a new slice.
var iccCommonTags = []iccTag{
	{"cprt", append(append([]byte("text"), make([]byte, 4)...), "No copyright\x00"...)},
	{"wtpt", iccXYZ(iccD50...)},
}

// iccU16 appends v as struct's ">H": its low 16 bits, big-endian.
func iccU16(b []byte, v int) []byte {
	return binary.BigEndian.AppendUint16(b, uint16(v)) //nolint:gosec // struct packing keeps the field width; fixture values fit.
}

// iccU32 appends v as struct's ">I" or ">i": its low 32 bits, big-endian.
func iccU32(b []byte, v int) []byte {
	return binary.BigEndian.AppendUint32(b, uint32(v)) //nolint:gosec // struct packing keeps the field width; fixture values fit.
}

// iccS15 encodes each value as an s15Fixed16Number, rounded half to even:
// the committed profiles were computed so, and a value rounded otherwise
// would change their bytes.
func iccS15(values ...float64) []byte {
	var out []byte
	for _, v := range values {
		out = iccU32(out, int(math.RoundToEven(v*65536)))
	}
	return out
}

// iccTextDescription is a v2 textDescriptionType
// (ICC.1:2001-04, 6.5.17): ASCII only. It panics on non-ASCII text.
func iccTextDescription(text string) []byte {
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 {
			panic(fmt.Sprintf("icc: %q is not ASCII", text))
		}
	}
	asciiText := append([]byte(text), 0)
	out := append([]byte("desc"), make([]byte, 4)...)
	out = iccU32(out, len(asciiText))
	out = append(out, asciiText...)
	return append(out, make([]byte, 4+4+2+1+67)...)
}

// iccXYZ is an XYZType of values.
func iccXYZ(values ...float64) []byte {
	return append(append([]byte("XYZ "), make([]byte, 4)...), iccS15(values...)...)
}

// iccGammaCurve is a curveType (ICC.1:2001-04, 6.5.3) of one
// entry: a u8Fixed8 gamma.
func iccGammaCurve(gamma float64) []byte {
	out := append([]byte("curv"), make([]byte, 4)...)
	out = iccU32(out, 1)
	return iccU16(out, int(math.RoundToEven(gamma*256)))
}

// iccLut8 is a lut8Type (ICC.1:2001-04, 6.5.8) over a two-point grid,
// with identity input and output tables. Each clut value must fit a byte; it
// panics otherwise.
func iccLut8(inputs, outputs int, clut []int) []byte {
	ramp := make([]byte, 256)
	for i := range ramp {
		ramp[i] = byte(i)
	}
	out := append([]byte("mft1"), make([]byte, 4)...)
	out = append(out, byte(inputs), byte(outputs), 2, 0) //nolint:gosec // channel counts are small.
	out = append(out, iccS15(iccIdentity...)...)
	for range inputs {
		out = append(out, ramp...)
	}
	for _, v := range clut {
		if v < 0 || v > 255 {
			panic(fmt.Sprintf("icc: clut value %d out of range", v))
		}
		out = append(out, byte(v))
	}
	for range outputs {
		out = append(out, ramp...)
	}
	return out
}

// iccA2B0 is the CMYK to Lab table: lightness falls with total ink, a and b stay
// neutral.
func iccA2B0() []byte {
	clut := make([]int, 0, 16*3)
	for c := range 2 {
		for m := range 2 {
			for y := range 2 {
				for k := range 2 {
					clut = append(clut, max(0, 255-64*(c+m+y+2*k)), 128, 128)
				}
			}
		}
	}
	return iccLut8(4, 3, clut)
}

// iccB2A0 is the Lab to CMYK table: dark colours take black ink only.
func iccB2A0() []byte {
	clut := make([]int, 0, 8*4)
	for lightness := range 2 {
		for range 2 {
			for range 2 {
				k := 255
				if lightness != 0 {
					k = 0
				}
				clut = append(clut, 0, 0, 0, k)
			}
		}
	}
	return iccLut8(3, 4, clut)
}

// iccProfile is a version 2.1 ICC profile of tags, (signature,
// data) pairs. deviceClass, colourSpace and pcs are 4-byte signatures; it
// panics on any other length, which would shift every later header field.
func iccProfile(deviceClass, colourSpace, pcs string, tags []iccTag) []byte {
	for _, sig := range []string{deviceClass, colourSpace, pcs} {
		if len(sig) != 4 {
			panic(fmt.Sprintf("icc: signature %q is not 4 bytes", sig))
		}
	}
	table := iccU32(nil, len(tags))
	var body []byte
	offset := 128 + 4 + 12*len(tags)
	for _, t := range tags {
		table = append(table, t.Sig...)
		table = iccU32(table, offset+len(body))
		table = iccU32(table, len(t.Data))
		body = append(body, t.Data...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	header := make([]byte, 128)
	binary.BigEndian.PutUint32(header[0:4], uint32(128+len(table)+len(body))) //nolint:gosec // fixture profiles are small.
	binary.BigEndian.PutUint32(header[8:12], iccVersion21)
	copy(header[12:16], deviceClass)
	copy(header[16:20], colourSpace)
	copy(header[20:24], pcs)
	copy(header[36:40], "acsp")
	copy(header[68:80], iccS15(iccD50...))
	return slices.Concat(header, table, body)
}

// iccRGBDisplayProfile is a version 2.1 RGB display
// profile.
func iccRGBDisplayProfile() []byte {
	tags := make([]iccTag, 0, 1+len(iccCommonTags)+len(iccPrimaries)+3)
	tags = append(tags, iccTag{"desc", iccTextDescription("litho RGB display")})
	tags = append(tags, iccCommonTags...)
	for _, p := range iccPrimaries {
		tags = append(tags, iccTag{p.Sig, iccXYZ(p.XYZ...)})
	}
	for _, sig := range []string{"rTRC", "gTRC", "bTRC"} {
		tags = append(tags, iccTag{sig, iccGammaCurve(2.2)})
	}
	return iccProfile("mntr", "RGB ", "XYZ ", tags)
}

// iccCMYKPrinterProfile is a version 2.1 CMYK printer
// profile.
func iccCMYKPrinterProfile() []byte {
	tags := make([]iccTag, 0, 1+len(iccCommonTags)+2)
	tags = append(tags, iccTag{"desc", iccTextDescription("litho CMYK printer")})
	tags = append(tags, iccCommonTags...)
	tags = append(tags, iccTag{"A2B0", iccA2B0()}, iccTag{"B2A0", iccB2A0()})
	return iccProfile("prtr", "CMYK", "Lab ", tags)
}

package icc

import (
	"bytes"
	"encoding/binary"
	"flag"
	"math"
	"os"
	"path/filepath"
	"testing"
)

var updateTestProfiles = flag.Bool("update-icc-testdata", false, "rewrite the synthetic profiles of testdata/")

// srgbColorants are the sRGB colorants adapted to D50, as an sRGB profile
// states them, columns red, green, blue.
var srgbColorants = [9]float64{
	0.4361, 0.3851, 0.1431,
	0.2225, 0.7169, 0.0606,
	0.0139, 0.0971, 0.7141,
}

// pad4 pads b to four bytes, the alignment of the elements of a lutAtoBType
// or lutBtoAType (ICC.1:2010, 10.10).
func pad4(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// curves concatenates curve elements, each padded to four bytes.
func curves(cs ...[]byte) []byte {
	var b []byte
	for _, c := range cs {
		b = append(b, pad4(append([]byte(nil), c...))...)
	}
	return b
}

// repeatCurve is n copies of one curve element.
func repeatCurve(c []byte, n int) []byte {
	cs := make([][]byte, n)
	for i := range cs {
		cs[i] = c
	}
	return curves(cs...)
}

// identityCurve is a curveType of no entries, the identity.
func identityCurve() []byte {
	return []byte("curv\x00\x00\x00\x00\x00\x00\x00\x00")
}

// sampledCurveOf samples f at n points as a curveType.
func sampledCurveOf(n int, f func(float64) float64) []byte {
	vs := make([]uint16, n)
	for i := range vs {
		vs[i] = u16(f(float64(i) / float64(n-1)))
	}
	return tableCurve(vs...)
}

// u16 encodes a value on 0 to 1 as a 16-bit number, clipped.
func u16(v float64) uint16 {
	return uint16(math.Round(clamp01(v) * 65535))
}

// clutElement builds the CLUT of a lutAtoBType or lutBtoAType: the grid of
// each input, 16-bit precision, then the values node gives for each grid
// point, the first input varying slowest.
func clutElement(grid []int, out int, node func(in []float64) []float64) []byte {
	b := make([]byte, 20)
	for i, g := range grid {
		b[i] = byte(g)
	}
	b[16] = 2
	points := 1
	for _, g := range grid {
		points *= g
	}
	in := make([]float64, len(grid))
	for p := range points {
		rest := p
		for i := len(grid) - 1; i >= 0; i-- {
			in[i] = float64(rest%grid[i]) / float64(grid[i]-1)
			rest /= grid[i]
		}
		v := node(in)
		for k := range out {
			b = binary.BigEndian.AppendUint16(b, u16(v[k]))
		}
	}
	return b
}

// lutABElement builds a lutAtoBType ("mAB ") or lutBtoAType ("mBA ") tag of
// the elements given, a nil element being absent.
func lutABElement(sig string, in, out int, bCurves, matrix, mCurves, clut, aCurves []byte) []byte {
	b := append([]byte(sig), 0, 0, 0, 0, byte(in), byte(out), 0, 0)
	offsets := make([]byte, 20)
	body := []byte{}
	for i, e := range [][]byte{bCurves, matrix, mCurves, clut, aCurves} {
		if e == nil {
			continue
		}
		binary.BigEndian.PutUint32(offsets[4*i:], uint32(32+len(body)))
		body = append(body, pad4(append([]byte(nil), e...))...)
	}
	return append(append(b, offsets...), body...)
}

// matrixElement builds the twelve s15Fixed16Numbers of a lutAtoBType or
// lutBtoAType matrix: a 3 by 3 matrix, scaled, and no offsets.
func matrixElement(m [9]float64, scale float64) []byte {
	var vs []float64
	for _, v := range m {
		vs = append(vs, v*scale)
	}
	return s15Bytes(append(vs, 0, 0, 0)...)
}

// sf32Identity is an s15Fixed16ArrayType of the identity matrix, the
// chromatic adaptation tag of a profile whose white is D50.
func sf32Identity() []byte {
	return append([]byte("sf32\x00\x00\x00\x00"), s15Bytes(1, 0, 0, 0, 1, 0, 0, 0, 1)...)
}

// srgbDecode is the sRGB transfer function (IEC 61966-2-1) and srgbEncode its
// inverse.
func srgbDecode(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func srgbEncode(v float64) float64 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// v4CMYKProfile is a version 4 CMYK output profile over Lab whose tables are
// lutAtoBType and lutBtoAType: a 9-point AToB CLUT and a 17-point BToA CLUT
// resampled from the stock CMYK printer profile's tables in the version 4
// Lab encoding, behind identity A curves of the parametric and the sampled
// kinds, so that littleCMS reads the same colours through other tag types.
func v4CMYKProfile(t *testing.T) []byte {
	t.Helper()
	cgats, err := Parse(CMYKPrinterProfile())
	if err != nil {
		t.Fatal(err)
	}
	a2b, err := readPipeline(cgats.tags["A2B0"], "Lab ")
	if err != nil {
		t.Fatal(err)
	}
	b2a, err := readBPipeline(cgats.tags["B2A0"], "Lab ")
	if err != nil {
		t.Fatal(err)
	}
	forward := clutElement([]int{9, 9, 9, 9}, 3, func(in []float64) []float64 {
		var v [maxOutputs]float64
		copy(v[:], in)
		a2b.run(&v)
		l, a, b := xyzToLab(a2b.pcs(v[:3]))
		labToEncoding(l, a, b, pcsLabV4, &v)
		return v[:3]
	})
	backward := clutElement([]int{17, 17, 17}, 4, func(in []float64) []float64 {
		var v [maxOutputs]float64
		l, a, b := labFromV4(in)
		labToEncoding(l, a, b, pcsLab16, &v)
		b2a.run(&v)
		return v[:4]
	})
	paraIdentity := paraCurve(0, 1)
	sampledIdentity := sampledCurveOf(256, func(x float64) float64 { return x })
	aToB := lutABElement("mAB ", 4, 3, repeatCurve(identityCurve(), 3), nil, nil, forward, repeatCurve(paraIdentity, 4))
	bToA := lutABElement("mBA ", 3, 4, repeatCurve(paraIdentity, 3), nil, nil, backward, repeatCurve(sampledIdentity, 4))
	wtpt, _ := cgats.xyzTag("wtpt")
	return buildClassProfile("prtr", "CMYK", "Lab ", 4, map[string][]byte{
		"A2B0": aToB, "A2B1": aToB, "A2B2": aToB,
		"B2A0": bToA, "B2A1": bToA, "B2A2": bToA,
		"wtpt": xyzType(wtpt[0], wtpt[1], wtpt[2]),
	})
}

// v4RGBDisplayProfile is a version 4 matrix and tone curve RGB display
// profile of sRGB's colorants with three kinds of parametric curve: type 3,
// the sRGB function, on red; type 4, continuous at its break point as real
// profiles are, on green; type 2 on blue.
func v4RGBDisplayProfile() []byte {
	m := srgbColorants
	return buildClassProfile("mntr", "RGB ", "XYZ ", 4, map[string][]byte{
		"rXYZ": xyzType(m[0], m[3], m[6]),
		"gXYZ": xyzType(m[1], m[4], m[7]),
		"bXYZ": xyzType(m[2], m[5], m[8]),
		"rTRC": paraCurve(3, 2.4, 1/1.055, 0.055/1.055, 1/12.92, 0.04045),
		"gTRC": paraCurve(4, 2.2, 0.95, 0.05, (math.Pow(0.95*0.06+0.05, 2.2)+0.001)/0.06, 0.06, 0.002, 0.001),
		"bTRC": paraCurve(2, 1.8, 0.97, 0.03, 0.004),
		"wtpt": xyzType(d50[0], d50[1], d50[2]),
		"chad": sf32Identity(),
	})
}

// v4RGBLutProfile is a version 4 RGB display profile over XYZ whose tables
// spell sRGB out in every element a lutAtoBType and a lutBtoAType hold: the
// AToB0 table's A curves are the sRGB function, its CLUT the identity and
// its matrix the colorants; the BToA0 table's matrix is their inverse, its M
// curves the inverse sRGB function sampled at 1024 points, then an identity
// CLUT and A curves.
func v4RGBLutProfile(t *testing.T) []byte {
	t.Helper()
	inv, ok := invert3(srgbColorants)
	if !ok {
		t.Fatal("singular colorants")
	}
	identity := func(in []float64) []float64 { return in }
	// The connection space side of the tables holds XYZ as u1Fixed15
	// numbers, 1.0 at 0x8000 (ICC.1:2010, 6.3.4.3).
	xyzScale := 32768.0 / 65535
	aToB := lutABElement("mAB ", 3, 3,
		repeatCurve(identityCurve(), 3),
		matrixElement(srgbColorants, xyzScale),
		repeatCurve(identityCurve(), 3),
		clutElement([]int{2, 2, 2}, 3, identity),
		repeatCurve(paraCurve(3, 2.4, 1/1.055, 0.055/1.055, 1/12.92, 0.04045), 3))
	bToA := lutABElement("mBA ", 3, 3,
		repeatCurve(identityCurve(), 3),
		matrixElement(inv, 1/xyzScale),
		repeatCurve(sampledCurveOf(1024, srgbEncode), 3),
		clutElement([]int{2, 2, 2}, 3, identity),
		repeatCurve(identityCurve(), 3))
	return buildClassProfile("mntr", "RGB ", "XYZ ", 4, map[string][]byte{
		"A2B0": aToB,
		"B2A0": bToA,
		"wtpt": xyzType(d50[0], d50[1], d50[2]),
		"chad": sf32Identity(),
	})
}

// testProfiles are the synthetic profiles of testdata/, which the oracle
// goldens read.
func testProfiles(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		"v4-cmyk-lut.icc":    v4CMYKProfile(t),
		"v4-rgb-display.icc": v4RGBDisplayProfile(),
		"v4-rgb-lut-xyz.icc": v4RGBLutProfile(t),
	}
}

// TestTestdataProfilesAreCommitted pins the synthetic profiles of testdata/
// to their builders. Rewrite them with
// go test ./pkg/internal/graphics/icc -run TestTestdataProfilesAreCommitted -update-icc-testdata,
// then regenerate the goldens (testdata/README.md).
func TestTestdataProfilesAreCommitted(t *testing.T) {
	t.Parallel()

	for name, want := range testProfiles(t) {
		path := filepath.Join("testdata", name)
		if *updateTestProfiles {
			if err := os.WriteFile(path, want, 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from its builder: rerun with -update-icc-testdata", name)
		}
	}
}

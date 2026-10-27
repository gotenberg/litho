package icc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"unicode/utf16"
)

// mlucType builds a multiLocalizedUnicodeType of one record per language and
// country pair, given as "enUS" and the like.
func mlucType(records ...string) []byte {
	b := []byte("mluc\x00\x00\x00\x00")
	b = binary.BigEndian.AppendUint32(b, uint32(len(records)/2))
	b = binary.BigEndian.AppendUint32(b, 12)
	var text []byte
	offset := 16 + 12*len(records)/2
	for i := 0; i < len(records); i += 2 {
		var units []byte
		for _, u := range utf16.Encode([]rune(records[i+1])) {
			units = binary.BigEndian.AppendUint16(units, u)
		}
		b = append(b, records[i]...)
		b = binary.BigEndian.AppendUint32(b, uint32(len(units)))
		b = binary.BigEndian.AppendUint32(b, uint32(offset+len(text)))
		text = append(text, units...)
	}
	return append(b, text...)
}

// bradfordD65ToD50 is the Bradford adaptation from D65 to D50 as lcms writes
// it into a version 4 display profile's chromaticAdaptationTag.
var bradfordD65ToD50 = []float64{
	1.047882, 0.022918, -0.050217,
	0.029586, 0.990478, -0.017075,
	-0.009247, 0.015075, 0.751678,
}

// sGrayV4 is a version 4.2 grey display profile of the shape lcms writes for
// sRGB grey, the one pdfjs issue19532.pdf carries in its output intent: the
// sRGB tone curve as a parametric curve, a D50 media white point with the
// Bradford adaptation from D65, and multi-localized texts.
func sGrayV4() []byte {
	p := buildProfile("GRAY", "XYZ ", 4, map[string][]byte{
		"desc": mlucType("enUS", "sGry"),
		"cprt": mlucType("deDE", "Keine", "enUS", "CC0"),
		"wtpt": xyzType(0.9642, 1, 0.8249),
		"chad": append([]byte("sf32\x00\x00\x00\x00"), s15Bytes(bradfordD65ToD50...)...),
		"kTRC": paraCurve(3, 2.4, 1/1.055, 0.055/1.055, 1/12.92, 0.04045),
	})
	p[9] = 0x20
	copy(p[84:], "0123456789abcdef")
	return p
}

// tagType returns the type signature of tag sig in profile data.
func tagType(t *testing.T, data []byte, sig string) string {
	t.Helper()
	p, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	b, ok := p.tags[sig]
	if !ok {
		t.Fatalf("no %s tag", sig)
	}
	return string(b[:4])
}

// sameColours fails unless both profiles convert every grey, every level of
// each channel alone and a 16-level grid to the same sRGB value.
func sameColours(t *testing.T, a, b []byte) {
	t.Helper()
	pa, err := Parse(a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range []Intent{Perceptual, RelativeColorimetric} {
		ta, err := pa.NewTransform(intent)
		if err != nil {
			// The source has no table for the intent: the conversion must
			// not grow one.
			if _, err := pb.NewTransform(intent); err == nil {
				t.Fatalf("intent %d: converted profile transforms, source does not", intent)
			}
			continue
		}
		tb, err := pb.NewTransform(intent)
		if err != nil {
			t.Fatal(err)
		}
		n := ta.Components()
		for v := range 256 {
			for ch := -1; ch < n; ch++ {
				in := make([]byte, n)
				for i := range in {
					if ch < 0 || i == ch {
						in[i] = byte(v)
					}
				}
				r1, g1, b1 := ta.Convert(in)
				r2, g2, b2 := tb.Convert(in)
				if r1 != r2 || g1 != g2 || b1 != b2 {
					t.Fatalf("intent %d, input %v: %d,%d,%d before, %d,%d,%d after", intent, in, r1, g1, b1, r2, g2, b2)
				}
			}
		}
	}
}

func TestToV2ConvertsGreyDisplayProfile(t *testing.T) {
	t.Parallel()

	src := sGrayV4()
	got, err := ToV2(src)
	if err != nil {
		t.Fatalf("ToV2: %v", err)
	}
	if v := binary.BigEndian.Uint32(got[8:]); v != v2Version {
		t.Errorf("version field = %#08x, want %#08x", v, v2Version)
	}
	if !bytes.Equal(got[84:100], make([]byte, 16)) {
		t.Errorf("profile ID = %x, want zeros (reserved in version 2)", got[84:100])
	}
	if int(binary.BigEndian.Uint32(got)) != len(got) {
		t.Errorf("size field = %d, profile is %d bytes", binary.BigEndian.Uint32(got), len(got))
	}
	for sig, want := range map[string]string{"desc": "desc", "cprt": "text", "kTRC": "curv", "chad": "sf32", "wtpt": "XYZ "} {
		if typ := tagType(t, got, sig); typ != want {
			t.Errorf("%s tag type = %q, want %q", sig, typ, want)
		}
	}
	p, err := Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if desc := p.tags["desc"]; !bytes.Contains(desc, []byte("sGry\x00")) {
		t.Errorf("desc = %q, want the English text sGry", desc)
	}
	if cprt := p.tags["cprt"]; !bytes.Equal(cprt, []byte("text\x00\x00\x00\x00CC0\x00")) {
		t.Errorf("cprt = %q, want the English record CC0", cprt)
	}
	// Version 2 records a display's own white: D65 here, the illuminant the
	// Bradford matrix adapts from.
	wtpt, _ := p.xyzTag("wtpt")
	for i, want := range [3]float64{0.9505, 1, 1.089} {
		if math.Abs(wtpt[i]-want) > 0.001 {
			t.Errorf("wtpt = %v, want D65", wtpt)
			break
		}
	}
	sameColours(t, src, got)
}

func TestToV2ConvertsParametricRGBProfile(t *testing.T) {
	t.Parallel()

	src := rgbProfile()
	src[8] = 4
	got, err := ToV2(src)
	if err != nil {
		t.Fatalf("ToV2: %v", err)
	}
	for _, sig := range []string{"rTRC", "gTRC", "bTRC"} {
		if typ := tagType(t, got, sig); typ != "curv" {
			t.Errorf("%s tag type = %q, want curv", sig, typ)
		}
	}
	sameColours(t, src, got)
	// A media white point that is not D50 is the source's own statement and
	// stays.
	cmyk := cmykProfile()
	cmyk[8] = 4
	got, err = ToV2(cmyk)
	if err != nil {
		t.Fatalf("ToV2 of a lut16Type profile: %v", err)
	}
	sameColours(t, cmyk, got)
}

func TestToV2ConvertsTaglessProfile(t *testing.T) {
	t.Parallel()

	src := buildProfile("CMYK", "XYZ ", 4, nil)
	got, err := ToV2(src)
	if err != nil {
		t.Fatalf("ToV2: %v", err)
	}
	if got[8] != 2 || len(got) != len(src) {
		t.Errorf("got version %d, %d bytes; want version 2, %d bytes", got[8], len(got), len(src))
	}
}

func TestToV2RefusesWhatItWouldResample(t *testing.T) {
	t.Parallel()

	if _, err := ToV2(extendedProfile()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("ToV2 of a lutAtoBType profile = %v, want ErrUnsupported", err)
	}
	v5 := rgbProfile()
	v5[8] = 5
	if _, err := ToV2(v5); !errors.Is(err, ErrUnsupported) {
		t.Errorf("ToV2 of a version 5 profile = %v, want ErrUnsupported", err)
	}
	if _, err := ToV2([]byte("short")); !errors.Is(err, ErrMalformed) {
		t.Errorf("ToV2 of a fragment = %v, want ErrMalformed", err)
	}
}

func TestToV2KeepsVersion2(t *testing.T) {
	t.Parallel()

	src := rgbProfile()
	got, err := ToV2(src)
	if err != nil {
		t.Fatalf("ToV2: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Error("ToV2 changed a version 2 profile")
	}
}

func TestSampledCurveStaysWithinTolerance(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		c    curve
		want int
	}{
		{"sRGB", parametric{kind: 3, g: 2.4, a: 1 / 1.055, b: 0.055 / 1.055, c: 1 / 12.92, d: 0.04045}, 1024},
		// A gamma below 1 is infinitely steep at 0, which no interpolated
		// table of fewer entries than the 16-bit inputs follows.
		{"gamma 0.45", parametric{kind: 0, g: 0.45}, 1 << 16},
	} {
		b := sampledCurve(tc.c)
		if n := int(binary.BigEndian.Uint32(b[8:])); n != tc.want {
			t.Errorf("%s: %d samples, want %d", tc.name, n, tc.want)
		}
		c, _, err := readCurve(b)
		if err != nil {
			t.Fatal(err)
		}
		for i := range 1 << 16 {
			x := float64(i) / 65535
			if d := math.Abs(c.eval(x) - tc.c.eval(x)); d > curveTolerance {
				t.Fatalf("%s: off by %g at %g", tc.name, d, x)
			}
		}
	}
}

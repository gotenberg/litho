package icc

import (
	"encoding/binary"
	"errors"
	"math"
	"math/rand/v2"
	"testing"
)

// TestConvert converts colours through profiles built to exercise each kind
// of transform, and matches LittleCMS 2.17 (relative colorimetric into its
// built-in sRGB, unoptimised, with black point compensation) to within one
// level. The grey profile's black, L* 13, lands on sRGB's black, where it
// reads (34, 34, 34) uncompensated; the CMYK table has no perceptual table,
// so its colorimetric black point estimate is zero and nothing moves.
func TestConvert(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		profile []byte
		cases   [][2][]byte
	}{
		{"matrix and tone curves", rgbProfile(), [][2][]byte{
			{{0, 0, 0}, {0, 0, 0}},
			{{255, 255, 255}, {255, 255, 255}},
			{{128, 64, 200}, {129, 70, 200}},
			{{10, 240, 30}, {3, 244, 30}},
		}},
		{"grey tone curve", greyProfile(), [][2][]byte{
			{{0}, {0, 0, 0}}, {{64}, {97, 97, 97}}, {{128}, {156, 156, 156}}, {{255}, {255, 255, 255}},
		}},
		{"lut16 into Lab", cmykProfile(), [][2][]byte{
			{{0, 0, 0, 0}, {255, 255, 255}},
			{{255, 0, 0, 0}, {0, 202, 255}},
			{{0, 255, 0, 0}, {255, 130, 183}},
			{{0, 0, 255, 0}, {205, 178, 30}},
			{{0, 0, 0, 255}, {27, 27, 27}},
			{{64, 128, 32, 90}, {157, 113, 131}},
			{{200, 10, 150, 20}, {82, 156, 131}},
		}},
		{"extended-range lutAtoB into XYZ", extendedProfile(), [][2][]byte{
			{{0, 0, 0}, {0, 0, 0}},
			{{255, 255, 255}, {255, 255, 255}},
			{{128, 128, 128}, {98, 98, 98}},
			{{192, 0, 0}, {231, 0, 0}},
		}},
	} {
		p, err := Parse(tc.profile)
		if err != nil {
			t.Fatalf("%s: Parse: %v", tc.name, err)
		}
		tr, err := p.NewTransform(RelativeColorimetric)
		if err != nil {
			t.Fatalf("%s: NewTransform: %v", tc.name, err)
		}
		for _, c := range tc.cases {
			r, g, b := tr.Convert(c[0])
			got := [3]byte{r, g, b}
			for i := range 3 {
				if d := int(got[i]) - int(c[1][i]); d < -1 || d > 1 {
					t.Errorf("%s: %v = %v, want %v", tc.name, c[0], got, c[1])
					break
				}
			}
		}
	}
}

// TestNewTransform_BlackPointCompensation pins that a profile whose black is
// lighter than sRGB's draws it as black under the perceptual, relative
// colorimetric and saturation intents, and as the medium's own dark grey
// under absolute colorimetric, which compensates nothing.
func TestNewTransform_BlackPointCompensation(t *testing.T) {
	t.Parallel()

	p, err := Parse(greyProfile())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		intent Intent
		want   byte
	}{{Perceptual, 0}, {RelativeColorimetric, 0}, {Saturation, 0}, {AbsoluteColorimetric, 34}} {
		tr, err := p.NewTransform(tc.intent)
		if err != nil {
			t.Fatalf("%s: %v", tc.intent, err)
		}
		if r, g, b := tr.Convert([]byte{0}); r != tc.want || g != tc.want || b != tc.want {
			t.Errorf("%s: black reads %d,%d,%d, want %d", tc.intent, r, g, b, tc.want)
		}
		if r, _, _ := tr.Convert([]byte{255}); r != 255 {
			t.Errorf("%s: white reads %d, want 255", tc.intent, r)
		}
	}
}

// TestNewTransform_BPCChoice pins that a graphics state's /UseBlackPtComp
// (ISO 32000-2, Table 57) reaches the transform: /OFF keeps the grey
// profile's black at its own dark grey, (34, 34, 34) as Little CMS 2.17
// reads it uncompensated, while /ON and /Default compensate it to black. No
// choice compensates under absolute colorimetric.
func TestNewTransform_BPCChoice(t *testing.T) {
	t.Parallel()

	p, err := Parse(greyProfile())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		intent Intent
		bpc    BPC
		want   byte
	}{
		{RelativeColorimetric, BPCDefault, 0},
		{RelativeColorimetric, BPCOn, 0},
		{RelativeColorimetric, BPCOff, 34},
		{Perceptual, BPCOff, 34},
		{AbsoluteColorimetric, BPCOn, 34},
	} {
		intent := tc.intent.WithBPC(tc.bpc)
		if intent&3 != tc.intent || intent.BPC() != tc.bpc {
			t.Fatalf("%s/%d: WithBPC packed %d", tc.intent, tc.bpc, intent)
		}
		tr, err := p.NewTransform(intent)
		if err != nil {
			t.Fatalf("%s/%d: %v", tc.intent, tc.bpc, err)
		}
		if r, g, b := tr.Convert([]byte{0}); r != tc.want || g != tc.want || b != tc.want {
			t.Errorf("%s/%d: black reads %d,%d,%d, want %d", tc.intent, tc.bpc, r, g, b, tc.want)
		}
	}
}

// TestParseBPC pins the names ISO 32000-2, Table 57 defines, and that any
// other is reported as such.
func TestParseBPC(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]BPC{"ON": BPCOn, "OFF": BPCOff, "Default": BPCDefault} {
		if got, ok := ParseBPC(name); !ok || got != want {
			t.Errorf("ParseBPC(%q) = %d, %v, want %d, true", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "on", "Off", "True"} {
		if got, ok := ParseBPC(name); ok || got != BPCDefault {
			t.Errorf("ParseBPC(%q) = %d, %v, want the default, false", name, got, ok)
		}
	}
}

// TestBuiltinCMYK_KeepsBPCChoicesApart pins that the shared built-in
// transforms are kept per black point compensation choice: an /OFF reading
// must not be served the compensated one.
func TestBuiltinCMYK_KeepsBPCChoicesApart(t *testing.T) {
	t.Parallel()

	on, err := BuiltinCMYK(RelativeColorimetric)
	if err != nil {
		t.Fatal(err)
	}
	off, err := BuiltinCMYK(RelativeColorimetric.WithBPC(BPCOff))
	if err != nil {
		t.Fatal(err)
	}
	if on == off {
		t.Fatal("the /OFF reading shares the compensated transform")
	}
	if off.bpcScale != nil {
		t.Error("the /OFF reading compensates")
	}
}

// TestSRGBTable_MatchesTheFormula pins that the table encoding gives what the
// sRGB formula gives: around every step between levels, where a misplaced
// threshold would show, across the range, and past both ends.
func TestSRGBTable_MatchesTheFormula(t *testing.T) {
	t.Parallel()

	s := srgb()
	check := func(v float64) {
		if got, want := s.encode(v), encode(v); got != want {
			t.Fatalf("encode(%v) = %d, want %d", v, got, want)
		}
	}
	for _, v := range []float64{0, 1, -1, 2, math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64, 0.0031308, math.Nextafter(1, 0)} {
		check(v)
	}
	for _, rise := range s.rise {
		v := rise
		for range 8 {
			v = math.Nextafter(v, 0)
		}
		for range 16 {
			check(v)
			v = math.Nextafter(v, 2)
		}
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 100_000 {
		check(rng.Float64())
		check(math.Exp(-rng.Float64() * 30))
	}
}

// TestParse_Refuses names each profile it cannot read with its sentinel.
func TestParse_Refuses(t *testing.T) {
	t.Parallel()

	noSignature := rgbProfile()
	copy(noSignature[36:], "xxxx")
	pastEnd := rgbProfile()
	binary.BigEndian.PutUint32(pastEnd[128+4+4:], 1<<20)
	for name, data := range map[string][]byte{
		"too short":         make([]byte, 100),
		"no file signature": noSignature,
		"tag past its end":  pastEnd,
	} {
		if _, err := Parse(data); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v, want %v", name, err, ErrMalformed)
		}
	}
}

// TestParse_RefusesICC2 refuses a version 5 profile, an ICC.2 one no PDF
// version admits, while DataComponents still reads its data colour space.
// Version 4 and 2 profiles, and a version 3 one no ICC.1 edition carries,
// parse.
func TestParse_RefusesICC2(t *testing.T) {
	t.Parallel()

	for version, want := range map[byte]bool{2: true, 3: true, 4: true, 5: false, 6: false} {
		data := rgbProfile()
		data[8] = version
		_, err := Parse(data)
		if want && err != nil {
			t.Errorf("version %d: %v, want a profile", version, err)
		}
		if !want && !errors.Is(err, ErrUnsupported) {
			t.Errorf("version %d: %v, want %v", version, err, ErrUnsupported)
		}
		if n, err := DataComponents(data); err != nil || n != 3 {
			t.Errorf("version %d: DataComponents = %d, %v, want 3", version, n, err)
		}
	}
	if _, err := DataComponents(make([]byte, 100)); !errors.Is(err, ErrMalformed) {
		t.Errorf("DataComponents of a short profile: %v, want %v", err, ErrMalformed)
	}
}

// TestNewTransform_Limits refuses a CLUT past maxCLUTEntries before reading
// it: 255 points over eight inputs.
func TestNewTransform_Limits(t *testing.T) {
	t.Parallel()

	b := []byte("mft2\x00\x00\x00\x00")
	b = append(b, 8, 3, 255, 0)
	b = append(b, make([]byte, 40)...)
	data := buildProfile("CMYK", "Lab ", 2, map[string][]byte{"A2B0": b})
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.NewTransform(RelativeColorimetric); !errors.Is(err, ErrLimit) {
		t.Errorf("%v, want %v", err, ErrLimit)
	}
}

// TestTransform_Identity pins that the sRGB profile and a grey profile of the
// sRGB tone curve read as the identity, and that a profile of another tone
// curve or of a table does not.
func TestTransform_Identity(t *testing.T) {
	t.Parallel()

	sGrey := buildProfile("GRAY", "XYZ ", 2, map[string][]byte{
		"kTRC": paraCurve(3, 2.4, 1/1.055, 0.055/1.055, 1/12.92, 0.04045),
		"wtpt": xyzType(0.9642, 1, 0.8249),
	})
	for _, tc := range []struct {
		name    string
		profile []byte
		want    bool
	}{
		{"sRGB", SRGBProfile(), true},
		{"a grey of the sRGB tone curve", sGrey, true},
		{"RGB of other tone curves", rgbProfile(), false},
		{"a grey of another tone curve", greyProfile(), false},
		{"a CMYK table", cmykProfile(), false},
	} {
		p, err := Parse(tc.profile)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		tr, err := p.NewTransform(RelativeColorimetric)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := tr.Identity(); got != tc.want {
			t.Errorf("%s: Identity() = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// greyLutProfile is a grey profile whose lut16Type AToB0 table reads its
// samples through a three-point grid into Lab: black, the middle grey at
// L* 50 with a* aMid, then white.
func greyLutProfile(aMid float64) []byte {
	b := []byte("mft2\x00\x00\x00\x00")
	b = append(b, 1, 3, 3, 0)
	b = append(b, s15Bytes(1, 0, 0, 0, 1, 0, 0, 0, 1)...)
	b = binary.BigEndian.AppendUint16(b, 2)
	b = binary.BigEndian.AppendUint16(b, 2)
	b = binary.BigEndian.AppendUint16(b, 0)
	b = binary.BigEndian.AppendUint16(b, 65535)
	for _, lab := range [][3]float64{{0, 0, 0}, {50, aMid, 0}, {100, 0, 0}} {
		// Legacy 16-bit Lab: L* 100 is 0xFF00, a* and b* 0 is 0x8000.
		b = binary.BigEndian.AppendUint16(b, uint16(math.Round(lab[0]/100*65280)))
		b = binary.BigEndian.AppendUint16(b, uint16(math.Round((lab[1]+128)*256)))
		b = binary.BigEndian.AppendUint16(b, uint16(math.Round((lab[2]+128)*256)))
	}
	for range 3 {
		b = binary.BigEndian.AppendUint16(b, 0)
		b = binary.BigEndian.AppendUint16(b, 65535)
	}
	return buildProfile("GRAY", "Lab ", 2, map[string][]byte{
		"A2B0": b,
		"wtpt": xyzType(0.9642, 1, 0.8249),
	})
}

// TestTransform_Neutral pins which transforms draw every grey sample as a
// grey: a grey tone curve (ICC.1:2010, F.2) and a table on the neutral axis
// do, a table that tints its middle greys and a profile of more than one
// component do not.
func TestTransform_Neutral(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		profile []byte
		want    bool
	}{
		{"a grey tone curve", greyProfile(), true},
		{"a neutral grey table", greyLutProfile(0), true},
		{"a grey table that tints", greyLutProfile(30), false},
		{"sRGB", SRGBProfile(), false},
	} {
		p, err := Parse(tc.profile)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for _, intent := range []Intent{RelativeColorimetric, Perceptual.WithBPC(BPCOff)} {
			tr, err := p.NewTransform(intent)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got := tr.Neutral(); got != tc.want {
				t.Errorf("%s, %v: Neutral() = %t, want %t", tc.name, intent, got, tc.want)
			}
		}
	}
}

// TestBuiltinCMYK converts through the built-in CMYK profile under every
// intent and matches LittleCMS 2.17 to within one level: the process inks
// land on the familiar SWOP cyan, magenta, yellow and rich black.
func TestBuiltinCMYK(t *testing.T) {
	t.Parallel()

	cases := [][2][]byte{
		{{0, 0, 0, 0}, {255, 255, 255}},
		{{255, 0, 0, 0}, {0, 174, 239}},
		{{0, 255, 0, 0}, {236, 0, 140}},
		{{0, 0, 255, 0}, {255, 242, 0}},
		{{0, 0, 0, 255}, {35, 31, 32}},
		{{255, 255, 255, 255}, {0, 0, 0}},
		{{64, 128, 32, 90}, {135, 99, 124}},
		{{200, 10, 150, 20}, {0, 163, 130}},
	}
	for _, intent := range []Intent{Perceptual, RelativeColorimetric, Saturation, AbsoluteColorimetric} {
		tr, err := BuiltinCMYK(intent)
		if err != nil {
			t.Fatalf("intent %d: %v", intent, err)
		}
		if tr.Components() != 4 {
			t.Fatalf("intent %d: %d components, want 4", intent, tr.Components())
		}
		for _, c := range cases {
			r, g, b := tr.Convert(c[0])
			got := [3]byte{r, g, b}
			for i := range 3 {
				if d := int(got[i]) - int(c[1][i]); d < -1 || d > 1 {
					t.Errorf("intent %d: %v = %v, want %v", intent, c[0], got, c[1])
					break
				}
			}
		}
	}
}

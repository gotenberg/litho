package icc

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"slices"
	"sync"
	"testing"
)

func mustParse(t testing.TB, data []byte) *Profile {
	t.Helper()
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustLink(t testing.TB, src, dst *Profile, opts TransformOptions) *Link {
	t.Helper()
	l, err := NewLink(src, dst, opts)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func readTestProfile(t testing.TB, path string) *Profile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return mustParse(t, data)
}

// The CMYK printer profile's BToA tables are a 17-point least squares
// inversion of its AToB table, which profiles/README.md says misses some
// nodes by more than 1 ΔE*ab, so a colour sent to CMYK and back moves by
// what that inversion misses. roundTripMedian and roundTripP95 bound the
// median and 95th percentile of that move, in ΔE*ab. littleCMS reads the
// same tables to within a level (TestLinkMatchesLittleCMS), so the bounds
// measure the profile, not the engine. Over the 4422 sRGB colours of a
// 9-level CMYK grid within the ink limit that sRGB holds unclipped, the move
// is 0.58 at the median, 3.09 at the 95th percentile and 8.59 at most.
const (
	roundTripMedian = 1
	roundTripP95    = 4
)

// TestLinkRoundTripsThroughCMYK sends sRGB colours the printer can print to
// CMYK and back to Lab: the sRGB readings of a CMYK grid within the 300 per
// cent ink limit that sRGB does not clip, so each lies in both gamuts.
func TestLinkRoundTripsThroughCMYK(t *testing.T) {
	t.Parallel()

	srgb, cmyk := mustParse(t, SRGBProfile()), mustParse(t, CMYKPrinterProfile())
	opts := TransformOptions{Intent: RelativeColorimetric}
	toRGB, toCMYK, toLab := mustLink(t, cmyk, srgb, opts), mustLink(t, srgb, cmyk, opts), mustLink(t, cmyk, LabProfile(), opts)
	ink, rgb, back, lab, lab2 := make([]float64, 4), make([]float64, 3), make([]float64, 4), make([]float64, 3), make([]float64, 3)
	var moves []float64
	for i := range 9 * 9 * 9 * 9 {
		ink[0], ink[1], ink[2], ink[3] = float64(i/729)/8, float64(i/81%9)/8, float64(i/9%9)/8, float64(i%9)/8
		if ink[0]+ink[1]+ink[2]+ink[3] > 3 {
			continue
		}
		toRGB.Convert(ink, rgb)
		if slices.ContainsFunc(rgb, func(v float64) bool { return v < 0.001 || v > 0.999 }) {
			continue
		}
		toLab.Convert(ink, lab)
		toCMYK.Convert(rgb, back)
		toLab.Convert(back, lab2)
		l1, a1, b1 := labFromV4(lab)
		l2, a2, b2 := labFromV4(lab2)
		moves = append(moves, math.Sqrt((l1-l2)*(l1-l2)+(a1-a2)*(a1-a2)+(b1-b2)*(b1-b2)))
	}
	if len(moves) < 4000 {
		t.Fatalf("%d colours in both gamuts, so the grid was not walked", len(moves))
	}
	slices.Sort(moves)
	if m, p := moves[len(moves)/2], moves[len(moves)*95/100]; m > roundTripMedian || p > roundTripP95 {
		t.Errorf("round trip moves colours by %.2f ΔE*ab at the median and %.2f at the 95th percentile", m, p)
	}
}

// TestLinkRoundTripsThroughLutProfile sends sRGB through the version 4 RGB
// profile whose tables spell sRGB out and back: matrix, curves and CLUT of
// both table types, inverted.
func TestLinkRoundTripsThroughLutProfile(t *testing.T) {
	t.Parallel()

	srgb, lut := mustParse(t, SRGBProfile()), readTestProfile(t, "testdata/v4-rgb-lut-xyz.icc")
	to := mustLink(t, srgb, lut, TransformOptions{Intent: RelativeColorimetric})
	from := mustLink(t, lut, srgb, TransformOptions{Intent: RelativeColorimetric})
	in, mid, out := make([]float64, 3), make([]float64, 3), make([]float64, 3)
	for i := range 9 * 9 * 9 {
		in[0], in[1], in[2] = float64(i/81)/8, float64(i/9%9)/8, float64(i%9)/8
		to.Convert(in, mid)
		from.Convert(mid, out)
		for k := range 3 {
			if d := math.Abs(out[k]-in[k]) * 255; d > 0.5 {
				t.Fatalf("%v comes back as %v, %.2f levels off", in, out, d)
			}
		}
	}
}

// TestLinkAgreesWithTransform pins that a Link to the sRGB profile reads
// CMYK as the existing device-to-sRGB [Transform] does, to within the level
// the sRGB profile's s15Fixed16 colorants round the analytic sRGB matrix by.
func TestLinkAgreesWithTransform(t *testing.T) {
	t.Parallel()

	cmyk := mustParse(t, CMYKPrinterProfile())
	l := mustLink(t, cmyk, mustParse(t, SRGBProfile()), TransformOptions{Intent: RelativeColorimetric})
	tr, err := cmyk.NewTransform(RelativeColorimetric)
	if err != nil {
		t.Fatal(err)
	}
	in, out := make([]float64, 4), make([]float64, 3)
	for i := range 6 * 6 * 6 * 6 {
		px := []byte{byte(i / 216 * 51), byte(i / 36 % 6 * 51), byte(i / 6 % 6 * 51), byte(i % 6 * 51)}
		for k, v := range px {
			in[k] = float64(v) / 255
		}
		l.Convert(in, out)
		r, g, b := tr.Convert(px)
		for k, want := range []uint8{r, g, b} {
			if d := math.Abs(out[k]*255 - float64(want)); d > 1 {
				t.Fatalf("CMYK %v: Link %v, Transform %d,%d,%d", px, out, r, g, b)
			}
		}
	}
}

// TestLinkAbsoluteColorimetricKeepsPaperWhite pins that the absolute intent
// prints a white that is not the PCS white in the tint of the source medium:
// relative colorimetry maps a printer's paper onto display white, absolute
// colorimetry keeps the paper.
func TestLinkAbsoluteColorimetricKeepsPaperWhite(t *testing.T) {
	t.Parallel()

	tinted := buildClassProfile("prtr", "CMYK", "Lab ", 2, map[string][]byte{
		"A2B0": cmykLut(t),
		"A2B1": cmykLut(t),
		"wtpt": xyzType(0.90, 0.95, 0.70),
	})
	src, dst := mustParse(t, tinted), LabProfile()
	paper, rel, abs := make([]float64, 4), make([]float64, 3), make([]float64, 3)
	mustLink(t, src, dst, TransformOptions{Intent: RelativeColorimetric}).Convert(paper, rel)
	mustLink(t, src, dst, TransformOptions{Intent: AbsoluteColorimetric}).Convert(paper, abs)
	l, a, b := labFromV4(rel)
	if math.Abs(l-100) > 0.01 || math.Abs(a) > 0.01 || math.Abs(b) > 0.01 {
		t.Errorf("relative paper white reads L*a*b* %.2f %.2f %.2f, want 100 0 0", l, a, b)
	}
	want := [3]float64{}
	want[0], want[1], want[2] = xyzToLab([3]float64{0.90, 0.95, 0.70})
	got := [3]float64{}
	got[0], got[1], got[2] = labFromV4(abs)
	for k := range 3 {
		if math.Abs(got[k]-want[k]) > 0.01 {
			t.Fatalf("absolute paper white reads %v, want the media white %v", got, want)
		}
	}
}

// cmykLut is the A2B1 table of cmykProfile.
func cmykLut(t *testing.T) []byte {
	t.Helper()
	return mustParse(t, cmykProfile()).tags["A2B1"]
}

// TestLinkBlackPointCompensation pins that compensation maps the source
// black onto the destination's: sRGB black lands on the printer profile's
// black point instead of below it, which clips, and white stays white.
func TestLinkBlackPointCompensation(t *testing.T) {
	t.Parallel()

	srgb, cmyk := mustParse(t, SRGBProfile()), mustParse(t, CMYKPrinterProfile())
	bd := cmyk.destinationBlackPoint(RelativeColorimetric)
	if bd[1] <= 0 || bd[1] > 0.1 {
		t.Fatalf("destination black point Y %.4f, want a printer's black", bd[1])
	}
	l := mustLink(t, srgb, cmyk, TransformOptions{Intent: RelativeColorimetric, BlackPointCompensation: true})
	// sRGB black's XYZ is 0, so its compensated XYZ is the offset.
	if l.offset == nil {
		t.Fatal("compensation applied no scaling")
	}
	for k := range 3 {
		if math.Abs(l.offset[k]-bd[k]) > 1e-9 {
			t.Errorf("black maps to %v, want %v", *l.offset, bd)
		}
		if w := l.scale[k]*d50[k] + l.offset[k]; math.Abs(w-d50[k]) > 1e-9 {
			t.Errorf("white maps to %.6f in component %d, want %.6f", w, k, d50[k])
		}
	}
	plain := mustLink(t, srgb, cmyk, TransformOptions{Intent: AbsoluteColorimetric, BlackPointCompensation: true})
	if plain.offset == nil || *plain.offset != ([3]float64{}) {
		t.Errorf("absolute colorimetric applies compensation: offset %v", plain.offset)
	}
}

// deviceLinkProfile is an RGB to CMYK DeviceLink profile: a two-point CLUT
// that takes each channel's complement as its ink and no black.
func deviceLinkProfile() []byte {
	clut := clutElement([]int{2, 2, 2}, 4, func(in []float64) []float64 {
		return []float64{1 - in[0], 1 - in[1], 1 - in[2], 0}
	})
	table := lutABElement("mAB ", 3, 4, repeatCurve(identityCurve(), 4), nil, nil, clut, repeatCurve(identityCurve(), 3))
	return buildClassProfile("link", "RGB ", "CMYK", 4, map[string][]byte{"A2B0": table})
}

// TestLinkDeviceLink pins that a DeviceLink profile's table is the whole
// transform: no connection space, no destination.
func TestLinkDeviceLink(t *testing.T) {
	t.Parallel()

	p := mustParse(t, deviceLinkProfile())
	l := mustLink(t, p, nil, TransformOptions{})
	if l.InputChannels() != 3 || l.OutputChannels() != 4 {
		t.Fatalf("%d to %d channels, want 3 to 4", l.InputChannels(), l.OutputChannels())
	}
	out := make([]float64, 4)
	for _, c := range [][3]float64{{0, 0, 0}, {1, 0.5, 0.25}, {0.2, 0.7, 0.9}} {
		l.Convert(c[:], out)
		for k := range 3 {
			if math.Abs(out[k]-(1-c[k])) > 1e-4 {
				t.Fatalf("%v converts to %v", c, out)
			}
		}
	}
	if _, err := NewLink(p, mustParse(t, SRGBProfile()), TransformOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("DeviceLink with a destination: %v, want %v", err, ErrUnsupported)
	}
	if _, err := NewLink(mustParse(t, SRGBProfile()), p, TransformOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("DeviceLink as destination: %v, want %v", err, ErrUnsupported)
	}
}

// sixColourProfile is a 6CLR output profile over Lab: lut16Type tables
// whose AToB0 darkens L* with the first three channels and ignores the
// others, and whose BToA0 puts the lightness deficit on the first three.
func sixColourProfile() []byte {
	// The legacy 16-bit Lab encoding of lut16Type: L* 100 is 0xFF00.
	lut16 := func(in, out, grid int, node func(in []float64) []float64) []byte {
		b := append([]byte("mft2\x00\x00\x00\x00"), byte(in), byte(out), byte(grid), 0)
		b = append(b, s15Bytes(1, 0, 0, 0, 1, 0, 0, 0, 1)...)
		b = binary.BigEndian.AppendUint16(b, 2)
		b = binary.BigEndian.AppendUint16(b, 2)
		for range in {
			b = binary.BigEndian.AppendUint16(b, 0)
			b = binary.BigEndian.AppendUint16(b, 65535)
		}
		grids := make([]int, in)
		for i := range grids {
			grids[i] = grid
		}
		b = append(b, clutElement(grids, out, node)[20:]...)
		for range out {
			b = binary.BigEndian.AppendUint16(b, 0)
			b = binary.BigEndian.AppendUint16(b, 65535)
		}
		return b
	}
	a2b := lut16(6, 3, 2, func(in []float64) []float64 {
		l := 1 - (in[0]+in[1]+in[2])/3
		return []float64{l * 65280 / 65535, 0x8000 / 65535.0, 0x8000 / 65535.0}
	})
	b2a := lut16(3, 6, 2, func(in []float64) []float64 {
		ink := 1 - in[0]*65535/65280
		return []float64{ink, ink, ink, 0, 0, 0}
	})
	return buildClassProfile("prtr", "6CLR", "Lab ", 2, map[string][]byte{"A2B0": a2b, "B2A0": b2a})
}

// TestLinkNColour pins that an n-colour profile converts both ways: six
// channels in, six out.
func TestLinkNColour(t *testing.T) {
	t.Parallel()

	p := mustParse(t, sixColourProfile())
	if p.Channels() != 6 || p.ColorSpace() != "6CLR" {
		t.Fatalf("%q of %d channels", p.ColorSpace(), p.Channels())
	}
	toLab := mustLink(t, p, LabProfile(), TransformOptions{Intent: RelativeColorimetric})
	lab := make([]float64, 3)
	toLab.Convert([]float64{0.5, 0.5, 0.5, 1, 1, 1}, lab)
	if l, _, _ := labFromV4(lab); math.Abs(l-50) > 0.01 {
		t.Errorf("half of the first three inks reads L* %.3f, want 50", l)
	}
	fromLab := mustLink(t, LabProfile(), p, TransformOptions{Intent: RelativeColorimetric})
	ink := make([]float64, 6)
	fromLab.Convert([]float64{0.25, 128.0 / 255, 128.0 / 255}, ink)
	for k, want := range []float64{0.75, 0.75, 0.75, 0, 0, 0} {
		if math.Abs(ink[k]-want) > 0.01 {
			t.Fatalf("L* 25 separates to %v", ink)
		}
	}
}

// TestLabProfileIsTheConnectionSpace pins that a Link between two Lab
// profiles gives Lab back, and that D50 white reads as sRGB white.
func TestLabProfileIsTheConnectionSpace(t *testing.T) {
	t.Parallel()

	l := mustLink(t, LabProfile(), LabProfile(), TransformOptions{Intent: Perceptual})
	out := make([]float64, 3)
	for _, in := range [][]float64{{0, 0.5, 0.5}, {0.3, 0.2, 0.9}, {1, 128.0 / 255, 128.0 / 255}} {
		l.Convert(in, out)
		for k := range 3 {
			if math.Abs(out[k]-in[k]) > 1e-9 {
				t.Fatalf("Lab %v comes back as %v", in, out)
			}
		}
	}
	mustLink(t, LabProfile(), mustParse(t, SRGBProfile()), TransformOptions{Intent: RelativeColorimetric}).
		Convert([]float64{1, 128.0 / 255, 128.0 / 255}, out)
	for k := range 3 {
		if out[k] < 0.999 {
			t.Fatalf("D50 white reads sRGB %v", out)
		}
	}
}

// TestNewLinkRefuses names each connection NewLink cannot build with its
// sentinel.
func TestNewLinkRefuses(t *testing.T) {
	t.Parallel()

	srgb := mustParse(t, SRGBProfile())
	noBToA := mustParse(t, cmykProfile())
	unknownClass := mustParse(t, buildClassProfile("xxxx", "RGB ", "XYZ ", 2, nil))
	abstract := mustParse(t, buildClassProfile("abst", "Lab ", "Lab ", 4, nil))
	unknownSpace := mustParse(t, buildClassProfile("mntr", "ABCD", "XYZ ", 2, nil))
	for name, c := range map[string]struct {
		src, dst *Profile
		want     error
	}{
		"no source":              {nil, srgb, ErrUnsupported},
		"no destination":         {srgb, nil, ErrUnsupported},
		"CMYK without BToA":      {srgb, noBToA, ErrMalformed},
		"unknown device class":   {unknownClass, srgb, ErrMalformed},
		"abstract profile":       {srgb, abstract, ErrUnsupported},
		"unknown data space":     {unknownSpace, srgb, ErrUnsupported},
		"RGB without its curves": {mustParse(t, buildClassProfile("mntr", "RGB ", "XYZ ", 2, nil)), srgb, ErrMalformed},
	} {
		if _, err := NewLink(c.src, c.dst, TransformOptions{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
}

// TestReadBPipelineRefuses refuses the lutBtoAType shapes ICC.1:2010, 10.12
// does not define.
func TestReadBPipelineRefuses(t *testing.T) {
	t.Parallel()

	ident3 := repeatCurve(identityCurve(), 3)
	clut := clutElement([]int{2, 2, 2}, 3, func(in []float64) []float64 { return in })
	matrix := matrixElement([9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}, 1)
	for name, tag := range map[string][]byte{
		"no B curves":                {},
		"CLUT without A curves":      lutABElement("mBA ", 3, 3, ident3, nil, nil, clut, nil),
		"matrix without M curves":    lutABElement("mBA ", 3, 3, ident3, matrix, nil, nil, nil),
		"other outputs without CLUT": lutABElement("mBA ", 3, 4, ident3, nil, nil, nil, nil),
		"truncated header":           []byte("mBA \x00\x00\x00\x00\x03\x03"),
	} {
		if len(tag) == 0 {
			tag = lutABElement("mBA ", 3, 3, nil, nil, nil, nil, nil)
		}
		if _, err := readBPipeline(tag, "Lab "); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v, want %v", name, err, ErrMalformed)
		}
	}
	if _, err := readBPipeline([]byte("mAB \x00\x00\x00\x00\x03\x03\x00\x00"), "Lab "); !errors.Is(err, ErrUnsupported) {
		t.Errorf("an AToB table type: %v, want %v", err, ErrUnsupported)
	}
}

// TestInvertCurve pins that each kind of inverse gives back the input the
// curve was evaluated at.
func TestInvertCurve(t *testing.T) {
	t.Parallel()

	rising := make(sampled, 33)
	falling := make(sampled, 33)
	for i := range rising {
		x := float64(i) / 32
		rising[i], falling[i] = x*x, 1-x
	}
	for name, c := range map[string]curve{
		"gamma":        gamma(2.2),
		"type 0":       parametric{kind: 0, g: 1.8},
		"type 1":       parametric{kind: 1, g: 2.2, a: 1.1, b: -0.1},
		"type 2":       parametric{kind: 2, g: 2.4, a: 0.9, b: 0.1, c: 0.05},
		"type 3":       parametric{kind: 3, g: 2.4, a: 1 / 1.055, b: 0.055 / 1.055, c: 1 / 12.92, d: 0.04045},
		"type 4":       parametric{kind: 4, g: 2.2, a: 0.95, b: 0.05, c: 0.2, d: 0.05, e: 0.01, f: 0.003},
		"rising":       rising,
		"falling":      falling,
		"non-monotone": sampled{0, 0.6, 0.4, 1},
	} {
		inv := invertCurve(c)
		for i := 1; i < 64; i++ {
			x := float64(i) / 64
			if name == "non-monotone" && x > 0.2 && x < 0.8 {
				continue
			}
			y := c.eval(x)
			if got := c.eval(inv.eval(y)); math.Abs(got-y) > 1e-6 {
				t.Errorf("%s: inverse of %.6f gives an input the curve reads as %.6f", name, y, got)
				break
			}
		}
	}
	// A value a discontinuous type 4 function jumps over inverts to the break
	// point.
	jump := parametric{kind: 4, g: 2.2, a: 0.95, b: 0.05, c: 0.1, d: 0.06, e: 0.002, f: 0.001}
	if got := invertCurve(jump).eval(0.008); got != 0.06 {
		t.Errorf("a value in the jump inverts to %.4f, want the break point 0.06", got)
	}
}

// TestLinkConcurrentConvert converts through one Link from many goroutines,
// for the race detector.
func TestLinkConcurrentConvert(t *testing.T) {
	t.Parallel()

	l := mustLink(t, mustParse(t, SRGBProfile()), mustParse(t, CMYKPrinterProfile()), TransformOptions{Intent: Perceptual, BlackPointCompensation: true})
	want := make([]float64, 4)
	l.Convert([]float64{0.2, 0.4, 0.6}, want)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			out := make([]float64, 4)
			for range 200 {
				l.Convert([]float64{0.2, 0.4, 0.6}, out)
				for k := range out {
					if out[k] != want[k] {
						t.Errorf("concurrent conversion %v, want %v", out, want)
						return
					}
				}
			}
		})
	}
	wg.Wait()
}

func benchmarkLink(b *testing.B, src, dst []byte, in int) {
	b.Helper()
	l := mustLink(b, mustParse(b, src), mustParse(b, dst), TransformOptions{Intent: Perceptual})
	px, out := make([]float64, in), make([]float64, l.OutputChannels())
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		for k := range px {
			px[k] = float64((i>>(3*k))&7) / 7
		}
		l.Convert(px, out)
	}
}

func BenchmarkLinkRGBToCMYK(b *testing.B) {
	benchmarkLink(b, SRGBProfile(), CMYKPrinterProfile(), 3)
}

func BenchmarkLinkCMYKToRGB(b *testing.B) {
	benchmarkLink(b, CMYKPrinterProfile(), SRGBProfile(), 4)
}

func BenchmarkLinkCMYKToMatrixGrey(b *testing.B) {
	benchmarkLink(b, CMYKPrinterProfile(), SGrayProfile(), 4)
}

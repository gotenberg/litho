package icc

import (
	"math"
	"testing"
)

// tablesOnly returns a copy of l without its white fix-up: the link's tables
// alone, as Little CMS reads them under cmsFLAGS_NOWHITEONWHITEFIXUP.
func tablesOnly(l *Link) *Link {
	c := *l
	c.white = nil
	return &c
}

// tintedRGBProfile is a matrix and tone curve RGB profile with sRGB's red and
// green colorants and its blue scaled by blue, so its white reads as a yellow
// that tints more as blue falls.
func tintedRGBProfile(blue float64) []byte {
	return buildProfile("RGB ", "XYZ ", 2, map[string][]byte{
		"rXYZ": xyzType(0.4361, 0.2225, 0.0139),
		"gXYZ": xyzType(0.3851, 0.7169, 0.0971),
		"bXYZ": xyzType(0.1431*blue, 0.0606*blue, 0.7141*blue),
		"rTRC": gammaCurve(2.2),
		"gTRC": gammaCurve(2.2),
		"bTRC": gammaCurve(2.2),
		"wtpt": xyzType(0.9642, 1, 0.8249),
	})
}

// TestLinkSendsWhiteToPaper converts white to the stock CMYK printer profile
// from sRGB and from the grey profile: under every intent but absolute
// colorimetric, with and without black point compensation, it lands on bare
// paper, where the tables alone leave a tint of each ink.
func TestLinkSendsWhiteToPaper(t *testing.T) {
	t.Parallel()

	cmyk := mustParse(t, CMYKPrinterProfile())
	for _, src := range []struct {
		name  string
		data  []byte
		white []float64
	}{
		{"sRGB", SRGBProfile(), []float64{1, 1, 1}},
		{"grey", SGrayProfile(), []float64{1}},
	} {
		for _, intent := range []Intent{Perceptual, RelativeColorimetric, Saturation} {
			for _, bpc := range []bool{false, true} {
				l := mustLink(t, mustParse(t, src.data), cmyk, TransformOptions{Intent: intent, BlackPointCompensation: bpc})
				out := make([]float64, 4)
				tablesOnly(l).Convert(src.white, out)
				if max(out[0], out[1], out[2], out[3]) == 0 {
					t.Fatalf("%s %s bpc=%v: the tables alone already send white to paper, so the test checks nothing", src.name, intent, bpc)
				}
				l.Convert(src.white, out)
				if out[0] != 0 || out[1] != 0 || out[2] != 0 || out[3] != 0 {
					t.Errorf("%s %s bpc=%v: white converts to %v, want no ink", src.name, intent, bpc, out)
				}
			}
		}
	}
}

// TestLinkKeepsAbsoluteWhite checks that the absolute colorimetric intent,
// which keeps the medium's tint (ICC.1:2010, 6.2), reads white through the
// tables alone.
func TestLinkKeepsAbsoluteWhite(t *testing.T) {
	t.Parallel()

	l := mustLink(t, mustParse(t, SRGBProfile()), mustParse(t, CMYKPrinterProfile()), TransformOptions{Intent: AbsoluteColorimetric})
	if l.white != nil {
		t.Fatal("an absolute colorimetric link fixes up white")
	}
}

// TestLinkWhiteFixupFades checks that the fix-up moves white by what the
// tables miss, a colour half a grid cell away by half of it, and a colour a
// cell or more away along any component not at all.
func TestLinkWhiteFixupFades(t *testing.T) {
	t.Parallel()

	l := mustLink(t, mustParse(t, SRGBProfile()), mustParse(t, CMYKPrinterProfile()), TransformOptions{Intent: RelativeColorimetric})
	plain := tablesOnly(l)
	convert := func(l *Link, in ...float64) []float64 {
		out := make([]float64, 4)
		l.Convert(in, out)
		return out
	}
	half := 1 - whiteFixupSpan/2
	got, want := convert(l, half, half, half), convert(plain, half, half, half)
	for i := range got {
		if move := want[i] - got[i]; math.Abs(move-l.white.delta[i]/-2) > 1e-12 && got[i] != 0 {
			t.Errorf("half a cell from white, ink %d moves by %.6f, want half of %.6f", i, move, -l.white.delta[i])
		}
	}
	for _, in := range [][]float64{
		{1 - whiteFixupSpan, 1 - whiteFixupSpan, 1 - whiteFixupSpan},
		{1, 1, 0.5},
		{0.2, 0.4, 0.6},
	} {
		got, want := convert(l, in...), convert(plain, in...)
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%v converts to %v, want the tables' %v", in, got, want)
				break
			}
		}
	}
}

// TestLinkWhiteFixupMatchesLittleCMS converts greys near white from sRGB to
// the stock CMYK printer profile under the relative colorimetric intent and
// compares them with Little CMS 2.17, which fixes up white by default.
// Recorded through Pillow 11's ImageCms.buildTransform with flags 0 (white
// fix-up on), 8-bit RGB to CMYK; with cmsFLAGS_NOWHITEONWHITEFIXUP the first
// two read (1, 1, 1, 0) and (2, 1, 1, 0).
func TestLinkWhiteFixupMatchesLittleCMS(t *testing.T) {
	t.Parallel()

	l := mustLink(t, mustParse(t, SRGBProfile()), mustParse(t, CMYKPrinterProfile()), TransformOptions{Intent: RelativeColorimetric})
	for _, c := range []struct {
		grey int
		want [4]int
	}{
		{255, [4]int{0, 0, 0, 0}},
		{254, [4]int{1, 1, 1, 0}},
		{252, [4]int{3, 2, 2, 0}},
		{250, [4]int{4, 3, 3, 0}},
		{247, [4]int{7, 5, 5, 0}},
		{240, [4]int{12, 9, 10, 0}},
	} {
		v := float64(c.grey) / 255
		out := make([]float64, 4)
		l.Convert([]float64{v, v, v}, out)
		for i, w := range c.want {
			if d := int(math.Round(out[i]*255)) - w; d < -1 || d > 1 {
				t.Errorf("grey %d converts to %v, Little CMS gives %v", c.grey, out, c.want)
				break
			}
		}
	}
}

// TestLinkWhiteFixupKeepsAColour checks a source whose white the tables send
// far from the destination's: past whiteFixupLimit the profile means its white
// as a colour, and the link keeps it, while a slight tint is fixed up.
func TestLinkWhiteFixupKeepsAColour(t *testing.T) {
	t.Parallel()

	srgb := mustParse(t, SRGBProfile())
	out := make([]float64, 3)
	yellow := mustLink(t, mustParse(t, tintedRGBProfile(0)), srgb, TransformOptions{Intent: RelativeColorimetric})
	if yellow.white != nil {
		t.Fatal("a white that reads as yellow is fixed up")
	}
	yellow.Convert([]float64{1, 1, 1}, out)
	if out[2] > 0.1 {
		t.Fatalf("the yellow white converts to %v, so the profile does not test the limit", out)
	}
	tint := mustLink(t, mustParse(t, tintedRGBProfile(0.95)), srgb, TransformOptions{Intent: RelativeColorimetric})
	tablesOnly(tint).Convert([]float64{1, 1, 1}, out)
	if out[2] == 1 {
		t.Fatalf("the tinted white converts to %v through its tables, so the test checks nothing", out)
	}
	tint.Convert([]float64{1, 1, 1}, out)
	if out[0] != 1 || out[1] != 1 || out[2] != 1 {
		t.Errorf("the tinted white converts to %v, want white", out)
	}
}

// TestLinkWhiteFixupSkipsSpacesWithoutWhite checks links that have no
// device white on one side: from the connection space and to Lab.
func TestLinkWhiteFixupSkipsSpacesWithoutWhite(t *testing.T) {
	t.Parallel()

	cmyk := mustParse(t, CMYKPrinterProfile())
	pcs, err := NewPCSLink(cmyk, TransformOptions{Intent: RelativeColorimetric})
	if err != nil {
		t.Fatal(err)
	}
	if pcs.white != nil {
		t.Error("a link from the connection space fixes up white")
	}
	if l := mustLink(t, cmyk, LabProfile(), TransformOptions{Intent: RelativeColorimetric}); l.white != nil {
		t.Error("a link to Lab fixes up white")
	}
}

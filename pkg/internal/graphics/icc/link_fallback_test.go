package icc

import "testing"

// TestLinkFallbacks pins which side of a link reports reading another table
// than the intent's: v4-rgb-lut-xyz.icc holds only AToB0 and BToA0, the
// built-in CMYK profile all three intents' tables, and sRGB a matrix that
// serves every intent.
func TestLinkFallbacks(t *testing.T) {
	t.Parallel()
	lut := readTestProfile(t, "testdata/v4-rgb-lut-xyz.icc")
	cmyk := mustParse(t, CMYKPrinterProfile())
	srgb := mustParse(t, SRGBProfile())
	for _, tc := range []struct {
		name     string
		src, dst *Profile
		intent   Intent
		wantSrc  Fallback
		wantDst  Fallback
	}{
		{"own table", lut, cmyk, Perceptual, NoFallback, NoFallback},
		{"source falls back", lut, cmyk, RelativeColorimetric, PerceptualFallback, NoFallback},
		{"destination falls back", cmyk, lut, Saturation, NoFallback, PerceptualFallback},
		{"absolute reads the relative tables", cmyk, cmyk, AbsoluteColorimetric, NoFallback, NoFallback},
		{"absolute without them", lut, lut, AbsoluteColorimetric, PerceptualFallback, PerceptualFallback},
		{"matrix serves every intent", srgb, cmyk, Saturation, NoFallback, NoFallback},
	} {
		l := mustLink(t, tc.src, tc.dst, TransformOptions{Intent: tc.intent})
		if src, dst := l.Fallbacks(); src != tc.wantSrc || dst != tc.wantDst {
			t.Errorf("%s: Fallbacks() = %q, %q, want %q, %q", tc.name, src, dst, tc.wantSrc, tc.wantDst)
		}
	}

	pcs, err := NewPCSLink(lut, TransformOptions{Intent: Saturation})
	if err != nil {
		t.Fatal(err)
	}
	if src, dst := pcs.Fallbacks(); src != NoFallback || dst != PerceptualFallback {
		t.Errorf("PCS link: Fallbacks() = %q, %q, want none, perceptual", src, dst)
	}
}

// TestProfileFallbackToMatrix pins that a profile holding tables for other
// intents, but no perceptual one, reports falling back to its matrix.
func TestProfileFallbackToMatrix(t *testing.T) {
	t.Parallel()
	p := &Profile{tags: map[string][]byte{"A2B1": nil, "B2A1": nil}}
	if got := p.fallback("A2B", Saturation); got != MatrixFallback {
		t.Errorf("fallback = %q, want matrix", got)
	}
	if got := p.fallback("B2A", RelativeColorimetric); got != NoFallback {
		t.Errorf("fallback = %q, want none", got)
	}
	if got := (&Profile{}).fallback("A2B", Saturation); got != NoFallback {
		t.Errorf("no tables: fallback = %q, want none", got)
	}
}

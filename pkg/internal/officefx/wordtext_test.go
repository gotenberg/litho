package officefx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestWordScale(t *testing.T) {
	if got := wordScale(wordScaleSize); got != 1 {
		t.Errorf("scale at %d points %v, want 1", wordScaleSize, got)
	}
	prev := 0.0
	for _, size := range []float64{1, 8, 11, 24, 48, 72, 96, 400} {
		got := wordScale(size)
		want := math.Pow(size/wordScaleSize, 2.0/3)
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("scale at %v points %v, want %v", size, got, want)
		}
		if got <= prev {
			t.Errorf("scale at %v points %v, not above the smaller size's %v", size, got, prev)
		}
		prev = got
	}
	for _, size := range []float64{0, -12, math.NaN(), math.Inf(1)} {
		if got := wordScale(size); got != 1 {
			t.Errorf("scale at %v points %v, want 1", size, got)
		}
	}
	if got, want := wordScale(1e6), wordScale(maxFontSize); got != want {
		t.Errorf("scale at a million points %v, want the cap's %v", got, want)
	}
}

// TestWordScale_SameBitsOnEveryArchitecture pins the bits of the scale:
// its cube root rounds every product on its own, so amd64 and arm64
// compute the same.
func TestWordScale_SameBitsOnEveryArchitecture(t *testing.T) {
	for _, tc := range []struct {
		size float64
		bits uint64
	}{
		{8, 0x3fcd0c466b6cb54b},
		{11, 0x3fd1f58eefdf4103},
		{24, 0x3fde3609a9ae44e5},
		{72, 0x3fef6bb930c6a173},
	} {
		if got := math.Float64bits(wordScale(tc.size)); got != tc.bits {
			t.Errorf("scale at %v points %#x, want %#x", tc.size, got, tc.bits)
		}
	}
}

func TestWordRunReach(t *testing.T) {
	k := wordScale(24)
	black := css.Color{A: 0.4}
	for _, tc := range []struct {
		name          string
		fx            css.OfficeEffects
		above, below  float64
		descent, size float64
	}{
		{"glow", css.OfficeEffects{Glow: &css.OfficeGlow{Color: black, Radius: 8}}, wordGlowReach * 8 * k, wordGlowReach * 8 * k, 6, 24},
		{"shadow down", css.OfficeEffects{Shadow: &css.OfficeShadow{Color: black, Dist: 10, Dir: 90}}, 0, 10 * k, 6, 24},
		{"shadow up", css.OfficeEffects{Shadow: &css.OfficeShadow{Color: black, Dist: 10, Dir: 270}}, 10 * k, 0, 6, 24},
		{"blurred shadow", css.OfficeEffects{Shadow: &css.OfficeShadow{Color: black, Blur: 8}}, 8 * k, 8 * k, 6, 24},
		{"shadow over glow", css.OfficeEffects{
			Glow:   &css.OfficeGlow{Color: black, Radius: 8},
			Shadow: &css.OfficeShadow{Color: black, Blur: 4, Dist: 3, Dir: 90},
		}, wordGlowReach*8*k + k, wordGlowReach*8*k + 7*k, 6, 24},
		{"reflection", css.OfficeEffects{Reflection: &css.OfficeReflection{StartAlpha: 0.5, EndPos: 0.5, Dir: 90, SY: -1}}, 0, 0.5*wordRunBox*24 - 6, 6, 24},
		{"reflection away", css.OfficeEffects{Reflection: &css.OfficeReflection{StartAlpha: 0.5, EndPos: 0.5, Dist: 7, Dir: 90, SY: -1}}, 0, 0.5*wordRunBox*24 + 12 - 6, 6, 24},
		{"invisible", css.OfficeEffects{Glow: &css.OfficeGlow{Radius: 8}}, 0, 0, 6, 24},
		{"powerpoint", css.OfficeEffects{Glow: &css.OfficeGlow{Color: black, Radius: 8}, Rules: css.OfficePowerPoint}, 0, 0, 6, 24},
		{"no size", css.OfficeEffects{Glow: &css.OfficeGlow{Color: black, Radius: 8}}, 0, 0, 6, 0},
	} {
		above, below := WordRunReach(&tc.fx, tc.size, tc.descent)
		if math.Abs(above-tc.above) > 1e-9 || math.Abs(below-tc.below) > 1e-9 {
			t.Errorf("%s: reach %.4f above, %.4f below, want %.4f, %.4f", tc.name, above, below, tc.above, tc.below)
		}
	}
	if a, b := WordRunReach(nil, 24, 6); a != 0 || b != 0 {
		t.Errorf("no effects reach %v, %v, want none", a, b)
	}
}

// wordSquare returns a 20 point square drawn as a run of Word's text of
// size points.
func wordSquare(size float64) *Source {
	s := square()
	s.Text, s.Size = true, size
	return s
}

// imageCentre returns the centre of a fragment that is an image.
func imageCentre(t *testing.T, f typeset.Fragment) (float64, float64) {
	t.Helper()
	img, ok := f.(*typeset.Image)
	if !ok {
		t.Fatalf("fragment %T, want an image", f)
	}
	return img.Rect.X + img.Rect.Width/2, img.Rect.Y + img.Rect.Height/2
}

// TestUnder_WordRunScalesWithFontSize checks that a run of Word's text
// moves its shadow by the distance scaled by its font size, and that a
// shape, whatever Size says, does not.
func TestUnder_WordRunScalesWithFontSize(t *testing.T) {
	fx := &css.OfficeEffects{Shadow: &css.OfficeShadow{Color: css.Color{A: 0.4}, Blur: 1, Dist: 10, Dir: 90, SX: 1, SY: 1}}
	for _, size := range []float64{8, 24, 72} {
		var px effect.Page
		frags, _, err := Under(context.Background(), wordSquare(size), fx, &px)
		if err != nil || len(frags) != 1 {
			t.Fatalf("%v points: %d fragments, error %v, want the shadow", size, len(frags), err)
		}
		_, cy := imageCentre(t, frags[0])
		if want := 20 + 10*wordScale(size); math.Abs(cy-want) > 0.3 {
			t.Errorf("%v points: shadow centred at %.2f, want %.2f", size, cy, want)
		}
	}
	shape := square()
	shape.Size = 8
	var px effect.Page
	frags, _, err := Under(context.Background(), shape, fx, &px)
	if err != nil || len(frags) != 1 {
		t.Fatalf("shape: %d fragments, error %v, want the shadow", len(frags), err)
	}
	if _, cy := imageCentre(t, frags[0]); math.Abs(cy-30) > 0.3 {
		t.Errorf("shape's shadow centred at %.2f, want 30, unscaled", cy)
	}
}

// TestUnder_WordRunCastsShadowOverGlow checks that the shadow of a run of
// Word's text with a glow reaches as far as the glow does, past the
// glyphs, and that its reflection shows the glow.
func TestUnder_WordRunCastsShadowOverGlow(t *testing.T) {
	glow := &css.OfficeGlow{Color: css.Color{R: 1, A: 0.4}, Radius: 8}
	sh := &css.OfficeShadow{Color: css.Color{A: 0.4}, Blur: 1, SX: 1, SY: 1}
	refl := &css.OfficeReflection{StartAlpha: 0.5, EndPos: 1, FadeDir: 90, Dir: 90, SX: 1, SY: -1, Align: css.RectAlign{Y: 1}}
	width := func(fx *css.OfficeEffects) []float64 {
		var px effect.Page
		frags, _, err := Under(context.Background(), wordSquare(72), fx, &px)
		if err != nil {
			t.Fatal(err)
		}
		var w []float64
		for _, f := range frags {
			w = append(w, f.(*typeset.Image).Rect.Width)
		}
		return w
	}
	alone := width(&css.OfficeEffects{Shadow: sh, Reflection: refl})
	both := width(&css.OfficeEffects{Shadow: sh, Glow: glow, Reflection: refl})
	if len(alone) != 2 || len(both) != 3 {
		t.Fatalf("%d and %d fragments, want 2 and 3", len(alone), len(both))
	}
	grown := 2 * Word.GlowGrow * 8 * wordScale(72)
	for i, name := range []string{"reflection", "shadow"} {
		if both[i]-alone[i] < grown {
			t.Errorf("%s %.2f points wide over the glow, %.2f without, want %.2f wider at least", name, both[i], alone[i], grown)
		}
	}
}

// TestUnder_WordReflectionMovesByTheSize checks that a reflection of a run
// of Word's text lies its distance times the size over 14 below the
// baseline, cut off at half that.
func TestUnder_WordReflectionMovesByTheSize(t *testing.T) {
	refl := &css.OfficeReflection{StartAlpha: 0.5, EndPos: 1, FadeDir: 90, Dist: 8, Dir: 90, SX: 1, SY: -1, Align: css.RectAlign{Y: 1}}
	var px effect.Page
	frags, _, err := Under(context.Background(), wordSquare(24), &css.OfficeEffects{Reflection: refl}, &px)
	if err != nil || len(frags) != 1 {
		t.Fatalf("%d fragments, error %v, want the reflection", len(frags), err)
	}
	r := frags[0].(*typeset.Image).Rect
	shift := 8.0 * 24 / wordReflectionDist
	// The run's box ends at the square's bottom, 30; the cut lies half the
	// shift below it, less the blur's margin.
	if top := r.Y; top < 30+shift/2-0.5 || top > 30+shift/2+0.05 {
		t.Errorf("reflection starts at %.2f, want %.2f", top, 30+shift/2)
	}
	// The mirrored box, 0.95 of the size tall from the baseline, ends the
	// whole shift and its height below the square's bottom.
	if end := r.Y + r.Height; math.Abs(end-(30+shift+wordRunBox*24)) > 0.5 {
		t.Errorf("reflection ends at %.2f, want %.2f", end, 30+shift+wordRunBox*24)
	}
}

// TestUnder_WordRunSameBytesOnEveryArchitecture pins the SHA-256 of the
// samples and soft masks a run of Word's text draws for its three
// effects.
func TestUnder_WordRunSameBytesOnEveryArchitecture(t *testing.T) {
	var px effect.Page
	fx := every()
	fx.Reflection.Dist, fx.Reflection.Dir = 4, 90
	frags, _, err := Under(context.Background(), wordSquare(30), fx, &px)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"46ca5d4701c40a63",
		"9f731ffd9b9c0237",
		"4c98cd9f7fa33d01",
	}
	if len(frags) != len(want) {
		t.Fatalf("%d fragments, want %d", len(frags), len(want))
	}
	for i, f := range frags {
		img := f.(*typeset.Image)
		h := sha256.New()
		h.Write(img.Source.Samples)
		h.Write(img.Source.Alpha)
		if got := hex.EncodeToString(h.Sum(nil))[:16]; got != want[i] {
			t.Errorf("effect %d: %s, want %s", i, got, want[i])
		}
	}
}

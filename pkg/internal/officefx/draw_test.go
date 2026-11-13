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

// square returns the source of a 20 point square at (10, 10).
func square() *Source {
	r := typeset.Rect{X: 10, Y: 10, Width: 20, Height: 20}
	ops := []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: 10, Y: 10}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 30, Y: 10}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 30, Y: 30}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 10, Y: 30}}},
		{Kind: typeset.ClosePath},
	}
	return &Source{Paths: []effect.Path{{Ops: ops}}, Bounds: r, Color: typeset.Color{B: 1, A: 1}}
}

// every returns one of each effect.
func every() *css.OfficeEffects {
	return &css.OfficeEffects{
		Reflection: &css.OfficeReflection{Blur: 0.5, StartAlpha: 0.5, EndPos: 0.5, FadeDir: 90, SX: 1, SY: -1, Align: css.RectAlign{Y: 1}},
		Shadow:     &css.OfficeShadow{Color: css.Color{A: 0.5}, Blur: 6, Dist: 5, SX: 1, SY: 1},
		Glow:       &css.OfficeGlow{Color: css.Color{R: 1, A: 0.4}, Radius: 6},
	}
}

func TestUnder_PaintsReflectionShadowGlowInOrder(t *testing.T) {
	var px effect.Page
	frags, _, err := Under(context.Background(), square(), every(), &px)
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 3 {
		t.Fatalf("%d fragments, want 3", len(frags))
	}
	refl, shadow, glow := frags[0].(*typeset.Image), frags[1].(*typeset.Image), frags[2].(*typeset.Image)
	for i, img := range []*typeset.Image{refl, shadow, glow} {
		if img.Parent != nil || img.Source == nil || img.Source.Alpha == nil {
			t.Errorf("image %d: %+v, want an artifact with a soft mask", i, img)
		}
	}
	// The reflection hangs from the square's bottom, within its blur's
	// margin.
	if y := refl.Rect.Y; y < 29 || y > 30 {
		t.Errorf("reflection starts at %.2f, want the bottom 30 less its margin", y)
	}
	// The shadow is moved 5 points right: its box's centre lies 5 points
	// right of the square's.
	if c := shadow.Rect.X + shadow.Rect.Width/2; c < 24.5 || c > 25.5 {
		t.Errorf("shadow centred at %.2f, want 25", c)
	}
	// The glow reaches its radius out of the square on every side.
	if glow.Rect.X > 10-5.5 || glow.Rect.X+glow.Rect.Width < 30+5.5 {
		t.Errorf("glow over %+v, want 6 points out of the square", glow.Rect)
	}
}

// TestUnder_SameBytesOnEveryArchitecture pins the SHA-256 of the samples
// and soft masks every effect writes for a fixed input: the effect engine
// and this package compute in integers and in rounded products, so amd64
// and arm64 write the same bytes.
func TestUnder_SameBytesOnEveryArchitecture(t *testing.T) {
	var px effect.Page
	src := square()
	frags, _, err := Under(context.Background(), src, every(), &px)
	if err != nil {
		t.Fatal(err)
	}
	src.Content = []typeset.Fragment{&typeset.Path{Ops: src.Paths[0].Ops, Fill: &typeset.Fill{Color: typeset.Color{G: 1, A: 1}}}}
	soft, _, err := Content(context.Background(), src, &css.OfficeEffects{SoftEdge: 4}, &px)
	if err != nil {
		t.Fatal(err)
	}
	frags = append(frags, soft)
	want := []string{
		"cb992fc1c4b46fbe",
		"2d8b3dc335b8c794",
		"ff2c1c95edc6f100",
		"a624fda508aac586",
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

func TestUnder_PagePixelsSpent(t *testing.T) {
	var px effect.Page
	for px.Take(effect.Resolution{W: 1 << 13, H: 1 << 13}) {
	}
	frags, notes, err := Under(context.Background(), square(), every(), &px)
	if err != nil || !notes.Skipped {
		t.Errorf("notes %+v, error %v, want the effects skipped", notes, err)
	}
	if len(frags) != 0 {
		t.Errorf("%d fragments past the page's pixels, want none", len(frags))
	}
}

func TestSincosDeg_Exact(t *testing.T) {
	for _, tc := range []struct{ deg, s, c float64 }{{0, 0, 1}, {90, 1, 0}, {180, 0, -1}, {270, -1, 0}, {-90, -1, 0}, {450, 1, 0}} {
		s, c := sincosDeg(tc.deg)
		if s != tc.s || c != tc.c {
			t.Errorf("sincos(%v) = %v, %v, want %v, %v", tc.deg, s, c, tc.s, tc.c)
		}
	}
}

// TestUnder_ShadowSkewsBeforeScaling checks that a shadow skewed and
// scaled about the bottom right of its square is skewed first, as Word
// and PowerPoint draw a perspective shadow: the top-left corner, 20 points
// above the bottom, moves 20 points left by a skew of 45 degrees, not the
// 5 points left a skew after a vertical scale of a quarter moves it.
func TestUnder_ShadowSkewsBeforeScaling(t *testing.T) {
	var px effect.Page
	fx := &css.OfficeEffects{Shadow: &css.OfficeShadow{Color: css.Color{A: 1}, Blur: 1.5, SX: 1, SY: 0.25, KX: 45, Align: css.RectAlign{X: 1, Y: 1}}}
	frags, _, err := Under(context.Background(), square(), fx, &px)
	if err != nil || len(frags) != 1 {
		t.Fatalf("%d fragments, error %v, want the shadow", len(frags), err)
	}
	r := frags[0].(*typeset.Image).Rect
	if left := r.X + 1.5; math.Abs(left-(-10)) > 0.5 {
		t.Errorf("shadow's left edge at %.2f, want -10", left)
	}
	if top := r.Y + 1.5; math.Abs(top-25) > 0.5 {
		t.Errorf("shadow's top edge at %.2f, want 25, a quarter of the square above its bottom", top)
	}
}

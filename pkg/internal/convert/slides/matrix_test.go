package slides

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestMatrix_RoundsEachProduct checks that composing, applying and
// inverting transforms rounds every product before it is summed: inputs
// where one rounding of x*y + z (a fused multiply-add, which the arm64
// compiler emits) and two roundings differ give the two-rounding result,
// the one amd64 computes, so a slide prints the same bytes on both.
func TestMatrix_RoundsEachProduct(t *testing.T) {
	// u*u is 1 + 2^-26 + 2^-54, whose last term a rounded product drops.
	u := 1 + math.Ldexp(1, -27)
	want := math.Ldexp(1, -26)
	m := then(typeset.Matrix{A: u, B: -1, D: 1}, typeset.Matrix{A: u, C: 1, D: 1, E: 0})
	if m.A != want {
		t.Errorf("then: A %v, want %v (the product rounded before the sum)", m.A, want)
	}
	if x, _ := apply(typeset.Matrix{A: u, C: -1, D: 1}, u, 1); x != want {
		t.Errorf("apply: x %v, want %v", x, want)
	}
	inv, ok := invert(typeset.Matrix{A: u, B: 1, C: 1, D: u})
	if !ok {
		t.Fatal("invert: singular")
	}
	// The determinant u*u - 1 rounds to 2^-26.
	if got := inv.A; got != u/want {
		t.Errorf("invert: A %v, want %v", got, u/want)
	}
}

// TestShape_TurnedTextShadowNeedsNoGroup checks that the text of a turned
// shape with a hard shadow is copied without a transform of its own: the
// shape's turn composed with its inverse is the identity on every
// architecture, so no group is written for it.
func TestShape_TurnedTextShadowNeedsNoGroup(t *testing.T) {
	sh := box(100, 100, 200, 100, para("Shadow", arial(20)))
	sh.Fill = Fill{Kind: FillSolid, Color: red}
	sh.Rotation = 30
	sh.Shadow = &Shadow{Opacity: 1, OffsetX: 4, OffsetY: 4, ScaleX: 1, ScaleY: 1}
	d := deck(&Slide{Shapes: []*Shape{sh}})
	d.Effects = EffectsPowerPoint
	doc := convert(t, d, office.Options{})
	var walk func(frags []typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			g, ok := f.(*typeset.Group)
			if !ok {
				continue
			}
			m := g.Transform
			if m != typeset.Identity && math.Abs(m.A-1)+math.Abs(m.B)+math.Abs(m.C)+math.Abs(m.D-1)+math.Abs(m.E)+math.Abs(m.F) < 1e-9 {
				t.Errorf("group under %+v, a rounding away from the identity", m)
			}
			walk(g.Content)
		}
	}
	walk(doc.Pages[0].Content)
}

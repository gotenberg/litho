package slides

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// placed is a path or an image of a page in page coordinates, with the
// opacity of the groups around it.
type placed struct {
	path    *typeset.Path
	img     *typeset.Image
	m       typeset.Matrix
	opacity float64
}

// bounds returns the page bounds of the fragment.
func (p placed) bounds() typeset.Rect {
	if p.img != nil {
		return transformRect(p.m, p.img.Rect)
	}
	return placedPath{p.path, p.m}.bounds()
}

// flatten lists the paths and images of frags in paint order.
func flatten(frags []typeset.Fragment, m typeset.Matrix, opacity float64, out *[]placed) {
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Path:
			*out = append(*out, placed{path: v, m: m, opacity: opacity})
		case *typeset.Image:
			*out = append(*out, placed{img: v, m: m, opacity: opacity})
		case *typeset.Group:
			o := v.Opacity
			if o == 0 {
				o = 1
			}
			flatten(v.Content, then(v.Transform, m), opacity*o, out)
		}
	}
}

// effectPage converts a deck of one slide of shapes, its effects drawn by
// rules, and lists what its page paints.
func effectPage(t *testing.T, rules EffectRules, shapes ...*Shape) ([]placed, []typeset.Warning) {
	t.Helper()
	d := deck(&Slide{Shapes: shapes})
	d.Effects = rules
	doc := convert(t, d, office.Options{})
	var ps []placed
	flatten(doc.Pages[0].Content, typeset.Identity, 1, &ps)
	return ps, doc.Warnings
}

// alphaAt returns the alpha of an effect bitmap at the page point (x, y).
func alphaAt(t *testing.T, p placed, x, y float64) uint8 {
	t.Helper()
	img := decoded(t, p.img.Source)
	r := p.bounds()
	b := img.Bounds()
	px := int((x - r.X) / r.Width * float64(b.Dx()))
	py := int((y - r.Y) / r.Height * float64(b.Dy()))
	return img.NRGBAAt(px, py).A
}

// TestShape_HardShadow checks that a shadow without a blur paints a copy
// of the shape in the shadow's colour under it, moved by its offsets in
// slide coordinates, scaled about the shape's centre, at the shadow's
// opacity as a group's.
func TestShape_HardShadow(t *testing.T) {
	box := typeset.Rect{X: 100, Y: 100, Width: 200, Height: 100}
	for _, tc := range []struct {
		name   string
		shadow Shadow
		rot    float64
		want   typeset.Rect
	}{
		{"offset", Shadow{Color: css.Color{A: 1}, Opacity: 0.5, OffsetX: 10, OffsetY: 20}, 0, typeset.Rect{X: 110, Y: 120, Width: 200, Height: 100}},
		{"scaled", Shadow{Color: css.Color{A: 1}, Opacity: 0.5, ScaleX: 0.5, ScaleY: 2}, 0, typeset.Rect{X: 150, Y: 50, Width: 100, Height: 200}},
		{"turned shape, offsets not turned", Shadow{Color: css.Color{A: 1}, Opacity: 0.5, OffsetX: 10}, 90, typeset.Rect{X: 160, Y: 50, Width: 100, Height: 200}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Shape{Box: box, Rotation: tc.rot, Fill: Fill{Kind: FillSolid, Color: red}, Shadow: &tc.shadow}
			ps, warns := effectPage(t, EffectsPowerPoint, sh)
			if len(ps) != 2 || ps[0].path == nil || ps[1].path == nil {
				t.Fatalf("%d fragments, want the shadow's path then the shape's", len(ps))
			}
			if got := ps[0].bounds(); !rectNear(got, tc.want, 0.01) {
				t.Errorf("shadow bounds %+v, want %+v", got, tc.want)
			}
			if ps[0].path.Fill == nil || ps[0].path.Fill.Color != (typeset.Color{A: 1}) || !near(ps[0].opacity, 0.5, 1e-9) {
				t.Errorf("shadow fill %+v at opacity %v, want black at 0.5", ps[0].path.Fill, ps[0].opacity)
			}
			if len(warns) != 0 {
				t.Errorf("warnings %v, want none", warns)
			}
		})
	}
	t.Run("none", func(t *testing.T) {
		sh := &Shape{Box: box, Fill: Fill{Kind: FillSolid, Color: red}, Shadow: &Shadow{Color: css.Color{A: 1}}}
		ps, _ := effectPage(t, EffectsPowerPoint, sh)
		if len(ps) != 1 {
			t.Fatalf("%d fragments, want the shape alone for a shadow of opacity 0", len(ps))
		}
	})
}

// TestShape_BlurredShadow checks that a blurred shadow drawn by Impress's
// rules is one bitmap under the shape over its box moved by the offsets and grown by the blur, in
// the shadow's colour, opaque inside and half covered on the outline.
func TestShape_BlurredShadow(t *testing.T) {
	box := typeset.Rect{X: 100, Y: 100, Width: 200, Height: 100}
	sh := &Shape{Box: box, Fill: Fill{Kind: FillSolid, Color: red}, Shadow: &Shadow{Color: css.Color{B: 1, A: 1}, Opacity: 0.5, OffsetX: 10, OffsetY: 20, Blur: 8}}
	ps, warns := effectPage(t, EffectsImpress, sh)
	if len(ps) != 2 || ps[0].img == nil || ps[1].path == nil {
		t.Fatalf("%d fragments, want the shadow's bitmap then the shape", len(ps))
	}
	// The grid widens the box to whole pixels of 0.65 pt.
	if got, want := ps[0].bounds(), (typeset.Rect{X: 102, Y: 112, Width: 216, Height: 116}); !rectNear(got, want, 0.66) {
		t.Errorf("shadow bounds %+v, want %+v", got, want)
	}
	img := decoded(t, ps[0].img.Source)
	res, _ := effect.Plan(tentSigma(8), 216, 116)
	if b := img.Bounds(); b.Dx() != res.W || b.Dy() != res.H {
		t.Errorf("shadow bitmap %v, want %d by %d, the engine's grid for the tent's deviation", b, res.W, res.H)
	}
	if c := img.NRGBAAt(res.W/2, res.H/2); c != (color.NRGBA{B: 255, A: 128}) {
		t.Errorf("shadow middle %v, want blue at half alpha", c)
	}
	for _, tc := range []struct {
		x, y float64
		want uint8
		tol  int
	}{{102.5, 170, 0, 1}, {110, 170, 64, 6}, {300, 120, 64, 6}} {
		if got := alphaAt(t, ps[0], tc.x, tc.y); abs(int(got)-int(tc.want)) > tc.tol {
			t.Errorf("shadow alpha at (%v, %v) = %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}
	if len(warns) != 0 {
		t.Errorf("warnings %v, want none", warns)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TestShape_Glow checks that a glow drawn by Impress's rules is a bitmap over the shape grown by its
// radius, opaque at its opacity on the outline and fading out, painted
// under the shape; a rectangle glows twice, an ellipse once.
func TestShape_Glow(t *testing.T) {
	box := typeset.Rect{X: 100, Y: 100, Width: 100, Height: 60}
	glow := &Glow{Color: css.Color{G: 1, A: 1}, Opacity: 0.4, Radius: 10}
	for _, tc := range []struct {
		name  string
		geom  *shape.Geometry
		grown []float64
	}{
		{"rectangle", &shape.Geometry{Preset: "rect"}, []float64{20, 10}},
		{"no geometry", nil, []float64{10}},
		{"ellipse", &shape.Geometry{Preset: "ellipse"}, []float64{10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Shape{Box: box, Geometry: tc.geom, Fill: Fill{Kind: FillSolid, Color: red}, Glow: glow}
			ps, _ := effectPage(t, EffectsImpress, sh)
			if len(ps) != len(tc.grown)+1 || ps[len(ps)-1].path == nil {
				t.Fatalf("%d fragments, want %d glows then the shape", len(ps), len(tc.grown))
			}
			for i, d := range tc.grown {
				if got := ps[i].bounds(); !rectNear(got, grow(box, d), 0.01) {
					t.Errorf("glow %d bounds %+v, want the box grown by %v", i, got, d)
				}
			}
			inner := ps[len(tc.grown)-1]
			if got := alphaAt(t, inner, 150, 100.5); abs(int(got)-102) > 2 {
				t.Errorf("glow alpha on the outline %d, want 102", got)
			}
			if got := alphaAt(t, inner, 150, 90.2); got > 3 {
				t.Errorf("glow alpha a radius out %d, want 0", got)
			}
			if got := alphaAt(t, inner, 150, 95); got < 30 || got > 70 {
				t.Errorf("glow alpha half a radius out %d, want about half of 102", got)
			}
		})
	}
}

// TestShape_SoftEdge checks that a soft edge drawn by Impress's rules draws
// the shape as a bitmap in
// its fill's colour whose alpha fades from the outline inwards, in place of
// its paths.
func TestShape_SoftEdge(t *testing.T) {
	box := typeset.Rect{X: 100, Y: 100, Width: 100, Height: 80}
	for _, tc := range []struct {
		name string
		geom *shape.Geometry
		at   uint8 // alpha one radius in
	}{
		{"once", &shape.Geometry{Preset: "flowChartProcess"}, 128},
		{"twice on a rectangle", &shape.Geometry{Preset: "rect"}, 64},
		{"no geometry", nil, 128},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Shape{Box: box, Geometry: tc.geom, Fill: Fill{Kind: FillSolid, Color: red}, SoftEdge: 10}
			ps, warns := effectPage(t, EffectsImpress, sh)
			if len(ps) != 1 || ps[0].img == nil {
				t.Fatalf("%d fragments, want the shape's bitmap alone", len(ps))
			}
			if got := ps[0].bounds(); !rectNear(got, box, 0.01) {
				t.Errorf("bitmap bounds %+v, want %+v", got, box)
			}
			img := decoded(t, ps[0].img.Source)
			if c := img.NRGBAAt(img.Bounds().Dx()/2, img.Bounds().Dy()/2); c != (color.NRGBA{R: 255, A: 255}) {
				t.Errorf("middle %v, want opaque red", c)
			}
			if got := alphaAt(t, ps[0], 100.2, 140); got > 4 {
				t.Errorf("alpha on the outline %d, want about 0", got)
			}
			if got := alphaAt(t, ps[0], 110, 140); abs(int(got)-int(tc.at)) > 12 {
				t.Errorf("alpha a radius in %d, want %d", got, tc.at)
			}
			if len(warns) != 0 {
				t.Errorf("warnings %v, want none", warns)
			}
		})
	}
}

// TestShape_PictureShadow checks that a picture's shadow is its box's,
// blurred, under the picture.
func TestShape_PictureShadow(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	pic := &layout.Picture{Source: source(t, src), Width: 4, Height: 4}
	sh := &Shape{Kind: KindPicture, Box: typeset.Rect{X: 50, Y: 50, Width: 80, Height: 40}, Picture: pic, Shadow: &Shadow{Color: css.Color{A: 1}, Opacity: 1, OffsetX: 5, Blur: 4}}
	ps, _ := effectPage(t, EffectsImpress, sh)
	if len(ps) != 2 || ps[0].img == nil || ps[1].img == nil {
		t.Fatalf("%d fragments, want the shadow's bitmap then the picture", len(ps))
	}
	// The grid widens the box to whole pixels of 0.33 pt.
	if got, want := ps[0].bounds(), (typeset.Rect{X: 51, Y: 46, Width: 88, Height: 48}); !rectNear(got, want, 0.34) {
		t.Errorf("shadow bounds %+v, want %+v", got, want)
	}
	if got := alphaAt(t, ps[0], 95, 70); got != 255 {
		t.Errorf("shadow middle alpha %d, want 255", got)
	}
}

// TestShadowMatrix_SkewThenScale checks that a shadow's transform skews
// about the centre of the bounds before it scales, then moves by the
// offsets.
func TestShadowMatrix_SkewThenScale(t *testing.T) {
	b := typeset.Rect{X: 0, Y: 0, Width: 20, Height: 10}
	m := shadowMatrix(&Shadow{ScaleX: 1, ScaleY: -0.5, SkewX: 45, OffsetX: 3}, b)
	// The bottom-left corner, 5 below and 10 left of the centre: skewed
	// to (-5, 5), scaled to (-5, -2.5), back about the centre (10, 5),
	// moved by 3.
	if x, y := apply(m, 0, 10); !near(x, 8, 1e-9) || !near(y, 2.5, 1e-9) {
		t.Errorf("bottom left to %v, %v, want 8, 2.5", x, y)
	}
	if m := shadowMatrix(&Shadow{ScaleX: 1, ScaleY: 1, SkewX: math.NaN()}, b); m != typeset.Identity {
		t.Errorf("a skew that is not finite gives %+v, want the identity", m)
	}
}

// TestShape_ShadowUnder checks that the far copy of a preset shadow is a
// hard shadow painted under the near one, both under the shape.
func TestShape_ShadowUnder(t *testing.T) {
	box := typeset.Rect{X: 100, Y: 100, Width: 80, Height: 40}
	gray := css.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
	light := css.Color{R: 0.9, G: 0.9, B: 0.9, A: 1}
	sh := &Shape{Box: box, Fill: Fill{Kind: FillSolid, Color: red},
		Shadow:      &Shadow{Color: gray, Opacity: 1, OffsetX: 4, OffsetY: 4, ScaleX: 1, ScaleY: 1},
		ShadowUnder: &Shadow{Color: light, Opacity: 1, OffsetX: 8, OffsetY: 8, ScaleX: 1, ScaleY: 1},
	}
	ps, _ := effectPage(t, EffectsPowerPoint, sh)
	if len(ps) != 3 || ps[0].path == nil || ps[1].path == nil || ps[2].path == nil {
		t.Fatalf("%d fragments, want the far copy, the near copy, then the shape", len(ps))
	}
	for i, want := range []struct {
		c  css.Color
		at float64
	}{{light, 108}, {gray, 104}, {red, 100}} {
		if got := ps[i].bounds(); ps[i].path.Fill == nil || ps[i].path.Fill.Color != colorOf(want.c) || !near(got.X, want.at, 1e-9) {
			t.Errorf("fragment %d at %+v, want %+v at x %v", i, got, want.c, want.at)
		}
	}
}

// TestShape_ShapeOnlyShadow checks that a hard shadow copies the shape's
// text unless it is ShapeOnly, as the copies of most preset shadows are,
// whether the shape has a fill or not, and that the copy of the text is
// its glyphs' outlines, so the page's text holds the shape's text once
// ([builder.outlinedText]).
func TestShape_ShapeOnlyShadow(t *testing.T) {
	var count func(frags []typeset.Fragment) (int, int)
	count = func(frags []typeset.Fragment) (texts, black int) {
		for _, f := range frags {
			switch f := f.(type) {
			case *typeset.Text:
				texts++
			case *typeset.Path:
				if f.Fill != nil && f.Fill.Color == (typeset.Color{A: 1}) {
					black++
				}
			case *typeset.Group:
				t, b := count(f.Content)
				texts, black = texts+t, black+b
			}
		}
		return texts, black
	}
	for _, tc := range []struct {
		name      string
		filled    bool
		shapeOnly bool
		wantCopy  int
	}{{"filled", true, false, 2}, {"filled, shape only", true, true, 1}, {"text alone", false, false, 1}, {"text alone, shape only", false, true, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			sh := box(100, 100, 200, 100, para("Shadow", arial(20)))
			if tc.filled {
				sh.Fill = Fill{Kind: FillSolid, Color: red}
			}
			sh.Shadow = &Shadow{Color: css.Color{A: 1}, Opacity: 1, OffsetX: 4, OffsetY: 4, ScaleX: 1, ScaleY: 1, ShapeOnly: tc.shapeOnly}
			d := deck(&Slide{Shapes: []*Shape{sh}})
			d.Effects = EffectsPowerPoint
			doc := convert(t, d, office.Options{})
			if texts, black := count(doc.Pages[0].Content); texts != 1 || black != tc.wantCopy {
				t.Errorf("%d runs of text and %d black paths, want 1 and %d", texts, black, tc.wantCopy)
			}
		})
	}
}

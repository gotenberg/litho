package slides

import (
	"image"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// placed is a path or an image of a page in page coordinates, with the
// opacity of the groups around it.
type placed struct {
	path    *compose.Path
	img     *compose.Image
	m       compose.Matrix
	opacity float64
}

// bounds returns the page bounds of the fragment.
func (p placed) bounds() compose.Rect {
	if p.img != nil {
		return transformRect(p.m, p.img.Rect)
	}
	return placedPath{p.path, p.m}.bounds()
}

// flatten lists the paths and images of frags in paint order.
func flatten(frags []compose.Fragment, m compose.Matrix, opacity float64, out *[]placed) {
	for _, f := range frags {
		switch v := f.(type) {
		case *compose.Path:
			*out = append(*out, placed{path: v, m: m, opacity: opacity})
		case *compose.Image:
			*out = append(*out, placed{img: v, m: m, opacity: opacity})
		case *compose.Group:
			o := v.Opacity
			if o == 0 {
				o = 1
			}
			flatten(v.Content, then(v.Transform, m), opacity*o, out)
		}
	}
}

func effectPage(t *testing.T, shapes ...*Shape) ([]placed, []compose.Warning) {
	t.Helper()
	doc := convert(t, deck(&Slide{Shapes: shapes}), office.Options{})
	var ps []placed
	flatten(doc.Pages[0].Content, compose.Identity, 1, &ps)
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
	box := compose.Rect{X: 100, Y: 100, Width: 200, Height: 100}
	for _, tc := range []struct {
		name   string
		shadow Shadow
		rot    float64
		want   compose.Rect
	}{
		{"offset", Shadow{Color: css.Color{A: 1}, Opacity: 0.5, OffsetX: 10, OffsetY: 20}, 0, compose.Rect{X: 110, Y: 120, Width: 200, Height: 100}},
		{"scaled", Shadow{Color: css.Color{A: 1}, Opacity: 0.5, ScaleX: 0.5, ScaleY: 2}, 0, compose.Rect{X: 150, Y: 50, Width: 100, Height: 200}},
		{"turned shape, offsets not turned", Shadow{Color: css.Color{A: 1}, Opacity: 0.5, OffsetX: 10}, 90, compose.Rect{X: 160, Y: 50, Width: 100, Height: 200}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Shape{Box: box, Rotation: tc.rot, Fill: Fill{Kind: FillSolid, Color: red}, Shadow: &tc.shadow}
			ps, warns := effectPage(t, sh)
			if len(ps) != 2 || ps[0].path == nil || ps[1].path == nil {
				t.Fatalf("%d fragments, want the shadow's path then the shape's", len(ps))
			}
			if got := ps[0].bounds(); !rectNear(got, tc.want, 0.01) {
				t.Errorf("shadow bounds %+v, want %+v", got, tc.want)
			}
			if ps[0].path.Fill == nil || ps[0].path.Fill.Color != (compose.Color{A: 1}) || !near(ps[0].opacity, 0.5, 1e-9) {
				t.Errorf("shadow fill %+v at opacity %v, want black at 0.5", ps[0].path.Fill, ps[0].opacity)
			}
			if len(warns) != 0 {
				t.Errorf("warnings %v, want none", warns)
			}
		})
	}
	t.Run("none", func(t *testing.T) {
		sh := &Shape{Box: box, Fill: Fill{Kind: FillSolid, Color: red}, Shadow: &Shadow{Color: css.Color{A: 1}}}
		ps, _ := effectPage(t, sh)
		if len(ps) != 1 {
			t.Fatalf("%d fragments, want the shape alone for a shadow of opacity 0", len(ps))
		}
	})
}

// TestShape_BlurredShadow checks that a blurred shadow is one bitmap under
// the shape over its box moved by the offsets and grown by the blur, in
// the shadow's colour, opaque inside and half covered on the outline.
func TestShape_BlurredShadow(t *testing.T) {
	box := compose.Rect{X: 100, Y: 100, Width: 200, Height: 100}
	sh := &Shape{Box: box, Fill: Fill{Kind: FillSolid, Color: red}, Shadow: &Shadow{Color: css.Color{B: 1, A: 1}, Opacity: 0.5, OffsetX: 10, OffsetY: 20, Blur: 8}}
	ps, warns := effectPage(t, sh)
	if len(ps) != 2 || ps[0].img == nil || ps[1].path == nil {
		t.Fatalf("%d fragments, want the shadow's bitmap then the shape", len(ps))
	}
	if got, want := ps[0].bounds(), (compose.Rect{X: 102, Y: 112, Width: 216, Height: 116}); !rectNear(got, want, 0.01) {
		t.Errorf("shadow bounds %+v, want %+v", got, want)
	}
	img := decoded(t, ps[0].img.Source)
	if b := img.Bounds(); b.Dx()*b.Dy() > effectPixels || b.Dx() != 682 || b.Dy() != 366 {
		t.Errorf("shadow bitmap %v, want 682 by 366 within the budget", b)
	}
	if c := img.NRGBAAt(341, 183); c != (color.NRGBA{B: 255, A: 128}) {
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

// TestShape_Glow checks that a glow is a bitmap over the shape grown by its
// radius, opaque at its opacity on the outline and fading out, painted
// under the shape; a rectangle glows twice, an ellipse once.
func TestShape_Glow(t *testing.T) {
	box := compose.Rect{X: 100, Y: 100, Width: 100, Height: 60}
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
			ps, _ := effectPage(t, sh)
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

// TestShape_SoftEdge checks that a soft edge draws the shape as a bitmap in
// its fill's colour whose alpha fades from the outline inwards, in place of
// its paths.
func TestShape_SoftEdge(t *testing.T) {
	box := compose.Rect{X: 100, Y: 100, Width: 100, Height: 80}
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
			ps, warns := effectPage(t, sh)
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
			if got := alphaAt(t, ps[0], 100.2, 140); got != 0 {
				t.Errorf("alpha on the outline %d, want 0", got)
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
	sh := &Shape{Kind: KindPicture, Box: compose.Rect{X: 50, Y: 50, Width: 80, Height: 40}, Picture: pic, Shadow: &Shadow{Color: css.Color{A: 1}, Opacity: 1, OffsetX: 5, Blur: 4}}
	ps, _ := effectPage(t, sh)
	if len(ps) != 2 || ps[0].img == nil || ps[1].img == nil {
		t.Fatalf("%d fragments, want the shadow's bitmap then the picture", len(ps))
	}
	if got, want := ps[0].bounds(), (compose.Rect{X: 51, Y: 46, Width: 88, Height: 48}); !rectNear(got, want, 0.01) {
		t.Errorf("shadow bounds %+v, want %+v", got, want)
	}
	if got := alphaAt(t, ps[0], 95, 70); got != 255 {
		t.Errorf("shadow middle alpha %d, want 255", got)
	}
}

// TestNewGrid_Resolution checks the effect bitmap's size: 10 pixels a
// point rounded up, reduced to the pixel budget with its aspect kept.
func TestNewGrid_Resolution(t *testing.T) {
	for _, tc := range []struct {
		w, h       float64
		pw, ph     int
		ok         bool
		unitAtMost float64
	}{
		{14.003, 14.003, 141, 141, true, 10},
		{41.953, 41.953, 420, 420, true, 10},
		{171.921, 169.115, 504, 496, true, 2.94},
		{170.05, 155.877, 522, 479, true, 3.08},
		{0.1, 30, 0, 0, false, 0},
	} {
		g, ok := newGrid(compose.Rect{Width: tc.w, Height: tc.h})
		if ok != tc.ok || g.w != tc.pw || g.h != tc.ph {
			t.Errorf("newGrid(%v, %v) = %d by %d, %v, want %d by %d, %v", tc.w, tc.h, g.w, g.h, ok, tc.pw, tc.ph, tc.ok)
		}
		if ok && g.unit > tc.unitAtMost {
			t.Errorf("newGrid(%v, %v) unit %v, want at most %v", tc.w, tc.h, g.unit, tc.unitAtMost)
		}
	}
}

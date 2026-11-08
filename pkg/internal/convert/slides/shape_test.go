package slides

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// paths returns the paths of a page with the transforms they sit under.
func paths(frags []compose.Fragment, m compose.Matrix, out *[]placedPath) {
	for _, f := range frags {
		switch v := f.(type) {
		case *compose.Path:
			*out = append(*out, placedPath{v, m})
		case *compose.Group:
			t := v.Transform
			if t == (compose.Matrix{}) {
				t = compose.Identity
			}
			paths(v.Content, then(t, m), out)
		}
	}
}

type placedPath struct {
	p *compose.Path
	m compose.Matrix
}

// bounds returns the page bounding box of a placed path.
func (pp placedPath) bounds() compose.Rect {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, op := range pp.p.Ops {
		n := 1
		if op.Kind == compose.CurveTo {
			n = 3
		}
		if op.Kind == compose.ClosePath {
			continue
		}
		for _, q := range op.P[:n] {
			x, y := apply(pp.m, q.X, q.Y)
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
		}
	}
	return compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

func rectNear(a, b compose.Rect, tol float64) bool {
	return near(a.X, b.X, tol) && near(a.Y, b.Y, tol) && near(a.Width, b.Width, tol) && near(a.Height, b.Height, tol)
}

var red = css.Color{R: 1, A: 1}

func TestShape_Geometry(t *testing.T) {
	solid := Fill{Kind: FillSolid, Color: red}
	for _, tc := range []struct {
		name string
		sh   *Shape
		want compose.Rect
	}{
		{"plain", &Shape{Box: compose.Rect{X: 100, Y: 100, Width: 200, Height: 100}, Fill: solid}, compose.Rect{X: 100, Y: 100, Width: 200, Height: 100}},
		// p3.pptx: a 200 by 100 box at 400, 300 turned 90 degrees about its
		// centre covers 450..550 by 250..450.
		{"rot90", &Shape{Box: compose.Rect{X: 400, Y: 300, Width: 200, Height: 100}, Rotation: 90, Fill: solid}, compose.Rect{X: 450, Y: 250, Width: 100, Height: 200}},
		{"flip", &Shape{Box: compose.Rect{X: 10, Y: 20, Width: 30, Height: 40}, FlipH: true, FlipV: true, Fill: solid}, compose.Rect{X: 10, Y: 20, Width: 30, Height: 40}},
		{"group", &Shape{Kind: KindGroup, Box: compose.Rect{X: 100, Y: 100, Width: 200, Height: 200}, Child: compose.Rect{Width: 100, Height: 100},
			Kids: []*Shape{{Box: compose.Rect{X: 50, Y: 50, Width: 50, Height: 25}, Fill: solid}}}, compose.Rect{X: 200, Y: 200, Width: 100, Height: 50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := convert(t, deck(&Slide{Shapes: []*Shape{tc.sh}}), office.Options{}).Pages[0]
			var ps []placedPath
			paths(pg.Content, compose.Identity, &ps)
			if len(ps) != 1 {
				t.Fatalf("%d paths", len(ps))
			}
			if got := ps[0].bounds(); !rectNear(got, tc.want, 0.01) {
				t.Errorf("bounds %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestShape_TextTurns checks that text turns with its shape, a vertical
// flip turning it half a turn and a horizontal one leaving it (p3.pptx).
func TestShape_TextTurns(t *testing.T) {
	mk := func(rot float64, fh, fv bool) *Shape {
		s := box(100, 300, 200, 100, para("T", arial(20)))
		s.Rotation, s.FlipH, s.FlipV = rot, fh, fv
		return s
	}
	for _, tc := range []struct {
		name string
		sh   *Shape
		x, y float64
	}{
		{"flipH", mk(0, true, false), 100, 320},
		{"flipV", mk(0, false, true), 300, 380},
		{"rot90", mk(90, false, false), 230, 250},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := convert(t, deck(&Slide{Shapes: []*Shape{tc.sh}}), office.Options{}).Pages[0]
			if r := find(t, pg, "T"); !near(r.x, tc.x, 0.2) || !near(r.y, tc.y, 0.2) {
				t.Errorf("T at %.2f, %.2f, want %v, %v", r.x, r.y, tc.x, tc.y)
			}
		})
	}
}

func TestShape_Fills(t *testing.T) {
	box := compose.Rect{X: 0, Y: 0, Width: 200, Height: 100}
	b := &builder{warned: map[string]bool{}}
	ops := rectOps(box)
	lin := b.fill(&Fill{Kind: FillGradient, Gradient: &Gradient{Stops: []Stop{{0.2, red}, {1, css.Color{B: 1, A: 1}}}}}, box, ops)
	sh := lin[0].(*compose.Path).Fill.Shading
	if sh.Kind != compose.Axial || !near(sh.From.X, 0, 1e-9) || !near(sh.To.X, 200, 1e-9) || len(sh.Stops) != 3 || sh.Stops[0].Offset != 0 {
		t.Errorf("linear shading %+v", sh)
	}
	down := b.fill(&Fill{Kind: FillGradient, Gradient: &Gradient{Angle: 90, Stops: []Stop{{0, red}, {1, red}}}}, box, ops)
	if sh := down[0].(*compose.Path).Fill.Shading; !near(sh.From.Y, 0, 1e-9) || !near(sh.To.Y, 100, 1e-9) {
		t.Errorf("downward shading %+v", sh)
	}
	rad := b.fill(&Fill{Kind: FillGradient, Gradient: &Gradient{Radial: true, Stops: []Stop{{0, red}, {1, red}}}}, box, ops)
	if sh := rad[0].(*compose.Path).Fill.Shading; sh.Kind != compose.Radial || !near(sh.ToRadius, math.Hypot(200, 100)/2, 1e-9) {
		t.Errorf("radial shading %+v", sh)
	}
	pat := b.fill(&Fill{Kind: FillPattern, Pattern: "pct50", Color: red, Background: css.Color{A: 1}}, box, ops)
	if g, ok := pat[0].(*compose.Group); !ok || g.Clip == nil {
		t.Errorf("pattern %+v", pat)
	}
	unknown := b.fill(&Fill{Kind: FillPattern, Pattern: "nope", Color: red}, box, ops)
	if p, ok := unknown[0].(*compose.Path); !ok || p.Fill.Color.A != 0.5 || len(b.warnings) != 1 {
		t.Errorf("unknown pattern %+v, warnings %v", unknown, b.warnings)
	}
	pic := tinyPicture()
	pic.Width, pic.Height = 8, 8
	tiles := b.fill(&Fill{Kind: FillPicture, Tile: true, Picture: pic}, box, ops)
	if g := tiles[0].(*compose.Group); len(g.Content) != 25*13 {
		t.Errorf("%d tiles", len(g.Content))
	}
}

func TestShape_Arrowheads(t *testing.T) {
	b := &builder{warned: map[string]bool{}}
	for _, tc := range []struct {
		kind  ArrowKind
		frags int
		endX  float64
	}{
		{ArrowNone, 1, 100},
		{ArrowTriangle, 2, 100 - 3*minArrowBase/2},
		{ArrowOpen, 2, 100},
		{ArrowOval, 2, 100},
	} {
		l := &Line{Width: 1, Color: red, Tail: Arrow{Kind: tc.kind}}
		out := b.stroke(l, lineOps(0, 0, 100, 0))
		if len(out) != tc.frags {
			t.Errorf("kind %d: %d fragments", tc.kind, len(out))
			continue
		}
		if x := out[0].(*compose.Path).Ops[1].P[0].X; !near(x, tc.endX, 1e-9) {
			t.Errorf("kind %d: line ends at %v, want %v", tc.kind, x, tc.endX)
		}
		if tc.kind == ArrowTriangle {
			if tip := out[1].(*compose.Path).Ops[0].P[0]; !near(tip.X, 100, 1e-9) {
				t.Errorf("tip at %+v", tip)
			}
		}
	}
	if out := b.stroke(&Line{Width: 2, Color: red, Dash: []float64{4, 2}, Head: Arrow{Kind: ArrowTriangle}}, rectOps(compose.Rect{Width: 5, Height: 5})); len(out) != 1 || len(out[0].(*compose.Path).Stroke.Dash) != 2 {
		t.Errorf("closed dashed outline %+v", out)
	}
}

// TestShape_TableGrows checks that a row grows to the text of its cells.
func TestShape_TableGrows(t *testing.T) {
	cell := func(s string) TableCell {
		return TableCell{Text: &TextBody{Paragraphs: []Paragraph{para(s, arial(20))}, Wrap: true}, Borders: [4]Line{css.Bottom: {Width: 1, Color: red}}}
	}
	tbl := &Shape{Kind: KindTable, Box: compose.Rect{X: 0, Y: 0, Width: 200, Height: 20}, Table: &Table{
		ColumnWidths: []float64{100, 100},
		Rows: []TableRow{
			{Height: 10, Cells: []TableCell{cell("one two three four"), cell("x")}},
			{Height: 10, Cells: []TableCell{cell("y"), cell("z")}},
		},
	}}
	pg := convert(t, deck(&Slide{Shapes: []*Shape{tbl}}), office.Options{})
	z := find(t, pg.Pages[0], "z")
	// "one two three four" wraps to two 24-point lines.
	if !near(z.y, 48+20, 0.3) || !near(z.x, 100, 0.3) {
		t.Errorf("z at %.2f, %.2f", z.x, z.y)
	}
}

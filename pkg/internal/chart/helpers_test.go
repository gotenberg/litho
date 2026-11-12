package chart

import (
	"context"
	"math"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

var (
	ahemOnce  sync.Once
	ahemFonts *text.Fonts
	errAhem   error
)

// ahem returns an index holding the Ahem font alone, whose glyphs are full
// em squares, ascent 0.8 em and descent 0.2 em.
func ahem(t testing.TB) *text.Fonts {
	t.Helper()
	ahemOnce.Do(func() {
		ahemFonts, errAhem = text.NewFonts(context.Background(), nil, []string{filepath.Join("..", "layout", "testdata")})
	})
	if errAhem != nil {
		t.Fatalf("indexing Ahem: %v", errAhem)
	}
	return ahemFonts
}

// maxDepth is the group nesting a picture's vector drawing may reach
// (layout.MaxVectorDepth, which this package does not import).
const maxDepth = 5

// walk calls f on every fragment of frags, depth first, with its group
// depth.
func walk(frags []typeset.Fragment, depth int, f func(typeset.Fragment, int)) {
	for _, fr := range frags {
		f(fr, depth)
		if g, ok := fr.(*typeset.Group); ok {
			walk(g.Content, depth+1, f)
		}
	}
}

// paths returns the paths of frags filled with col.
func paths(frags []typeset.Fragment, col typeset.Color) []*typeset.Path {
	var out []*typeset.Path
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		if p, ok := fr.(*typeset.Path); ok && p.Fill != nil && near(p.Fill.Color, col) {
			out = append(out, p)
		}
	})
	return out
}

// near compares colours.
func near(a, b typeset.Color) bool {
	return math.Abs(a.R-b.R)+math.Abs(a.G-b.G)+math.Abs(a.B-b.B)+math.Abs(a.A-b.A) < 1e-6
}

// bounds returns the bounding box of a path's points.
func bounds(p *typeset.Path) typeset.Rect {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, op := range p.Ops {
		n := 1
		switch op.Kind {
		case typeset.MoveTo, typeset.LineTo:
		case typeset.CurveTo:
			n = 3
		case typeset.ClosePath:
			n = 0
		}
		for _, pt := range op.P[:n] {
			x0, y0, x1, y1 = math.Min(x0, pt.X), math.Min(y0, pt.Y), math.Max(x1, pt.X), math.Max(y1, pt.Y)
		}
	}
	return typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// texts returns the characters of the text runs of frags, a string per run.
func texts(frags []typeset.Fragment) []string {
	var out []string
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		if t, ok := fr.(*typeset.Text); ok {
			s := ""
			for _, g := range t.Glyphs {
				s += g.Text
			}
			out = append(out, s)
		}
	})
	return out
}

// nums returns number values.
func nums(vs ...float64) []Value {
	out := make([]Value, len(vs))
	for i, v := range vs {
		out[i] = Num(v)
	}
	return out
}

// strs returns string values.
func strs(vs ...string) []Value {
	out := make([]Value, len(vs))
	for i, v := range vs {
		out[i] = Str(v)
	}
	return out
}

var (
	red  = typeset.Color{R: 1, A: 1}
	blue = typeset.Color{B: 1, A: 1}
)

// plainAxes returns a category and a value axis without labels, lines or
// gridlines, so the plot area fills the chart less its margins.
func plainAxes() []*Axis {
	nan := math.NaN()
	return []*Axis{
		{Kind: CategoryAxis, Labels: LabelNone, BetweenCategories: true, Min: nan, Max: nan, MajorUnit: nan, MinorUnit: nan},
		{Kind: ValueAxis, Labels: LabelNone, Position: AxisLeft, Min: nan, Max: nan, MajorUnit: nan, MinorUnit: nan},
	}
}

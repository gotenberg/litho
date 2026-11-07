package svg

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func TestParsePath_Commands(t *testing.T) {
	p := func(x, y float64) compose.Point { return compose.Point{X: x, Y: y} }
	mv := func(x, y float64) compose.PathOp {
		return compose.PathOp{Kind: compose.MoveTo, P: [3]compose.Point{p(x, y)}}
	}
	ln := func(x, y float64) compose.PathOp {
		return compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{p(x, y)}}
	}
	cl := compose.PathOp{Kind: compose.ClosePath}
	tests := []struct {
		d    string
		want []compose.PathOp
		ok   bool
	}{
		{"M10 20 L30 40", []compose.PathOp{mv(10, 20), ln(30, 40)}, true},
		{"m10 20 l5 5 h10 v-5 z", []compose.PathOp{mv(10, 20), ln(15, 25), ln(25, 25), ln(25, 20), cl}, true},
		{"M0,0 10,10 20,0", []compose.PathOp{mv(0, 0), ln(10, 10), ln(20, 0)}, true},
		{"m1 1 2 2", []compose.PathOp{mv(1, 1), ln(3, 3)}, true},
		{"M0 0L1.5.5", []compose.PathOp{mv(0, 0), ln(1.5, 0.5)}, true},
		{"M0 0L1-2", []compose.PathOp{mv(0, 0), ln(1, -2)}, true},
		{"M1e1 2E-1", []compose.PathOp{mv(10, 0.2)}, true},
		{"M0 0 Z L 5 5", []compose.PathOp{mv(0, 0), cl, mv(0, 0), ln(5, 5)}, true},
		{"L 5 5", nil, false},
		{"M0 0 L 5", []compose.PathOp{mv(0, 0)}, false},
		{"M0 0 L5 5 X 3", []compose.PathOp{mv(0, 0), ln(5, 5)}, false},
		{"", nil, true},
	}
	for _, tt := range tests {
		got, ok := parsePath(tt.d)
		if ok != tt.ok || len(got) != len(tt.want) {
			t.Errorf("parsePath(%q) = %v, %v; want %v, %v", tt.d, got, ok, tt.want, tt.ok)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parsePath(%q)[%d] = %v, want %v", tt.d, i, got[i], tt.want[i])
			}
		}
	}
}

func TestParsePath_CurvesAndArcs(t *testing.T) {
	tests := []struct {
		d         string
		n         int
		end       compose.Point
		bx0, bx1  float64
		by0, by1  float64
		tolerance float64
	}{
		// A half circle of radius 10 from (0,10) to (20,10) bulging up.
		{"M0 10 A10 10 0 0 1 20 10", 2, compose.Point{X: 20, Y: 10}, 0, 20, 0, 10, 0.05},
		// The same with the large arc flag set the other way round.
		{"M0 10 A10 10 0 1 0 20 10", 2, compose.Point{X: 20, Y: 10}, 0, 20, 10, 20, 0.05},
		// Radii too small scale up to reach (F.6.6).
		{"M0 0 A1 1 0 0 1 10 0", 2, compose.Point{X: 10}, 0, 10, -5, 0, 0.05},
		// A zero radius draws a line.
		{"M0 0 A0 5 0 0 1 10 0", 1, compose.Point{X: 10}, 0, 10, 0, 0, 0},
		{"M0 0 Q10 10 20 0", 1, compose.Point{X: 20}, 0, 20, 0, 5, 0.01},
		{"M0 0 Q10 10 20 0 T40 0", 2, compose.Point{X: 40}, 0, 40, -5, 5, 0.01},
		{"M0 0 C0 10 10 10 10 0 S20 -10 20 0", 2, compose.Point{X: 20}, 0, 20, -7.5, 7.5, 0.01},
		// Arc flags may be written without separators (SVG 1.1, 8.3.9).
		{"M0 10 a10 10 0 0120 0", 2, compose.Point{X: 20, Y: 10}, 0, 20, 0, 10, 0.05},
	}
	for _, tt := range tests {
		ops, ok := parsePath(tt.d)
		if !ok || len(ops) != 1+tt.n {
			t.Errorf("parsePath(%q): %d ops, ok %v, want %d", tt.d, len(ops), ok, 1+tt.n)
			continue
		}
		last := ops[len(ops)-1]
		end := last.P[0]
		if last.Kind == compose.CurveTo {
			end = last.P[2]
		}
		if math.Abs(end.X-tt.end.X) > 1e-9 || math.Abs(end.Y-tt.end.Y) > 1e-9 {
			t.Errorf("parsePath(%q) ends at %v, want %v", tt.d, end, tt.end)
		}
		b := opsBBox(ops)
		if math.Abs(b.x0-tt.bx0) > tt.tolerance+1e-9 || math.Abs(b.x1-tt.bx1) > tt.tolerance+1e-9 ||
			math.Abs(b.y0-tt.by0) > tt.tolerance+1e-9 || math.Abs(b.y1-tt.by1) > tt.tolerance+1e-9 {
			t.Errorf("parsePath(%q) bbox %+v, want x %v..%v y %v..%v", tt.d, b, tt.bx0, tt.bx1, tt.by0, tt.by1)
		}
	}
}

func TestParseTransform_Grammar(t *testing.T) {
	tests := []struct {
		s    string
		want compose.Matrix
		ok   bool
	}{
		{"translate(10)", compose.Matrix{A: 1, D: 1, E: 10}, true},
		{"translate(10,20) scale(2)", compose.Matrix{A: 2, D: 2, E: 10, F: 20}, true},
		{"scale(2 3)", compose.Matrix{A: 2, D: 3}, true},
		{"matrix(1 2 3 4 5 6)", compose.Matrix{A: 1, B: 2, C: 3, D: 4, E: 5, F: 6}, true},
		{"rotate(90)", compose.Matrix{A: 0, B: 1, C: -1, D: 0}, true},
		{"rotate(180 5 5)", compose.Matrix{A: -1, D: -1, E: 10, F: 10}, true},
		{"skewX(45)", compose.Matrix{A: 1, C: 1, D: 1}, true},
		{"  translate( 1 , 2 ) , scale( 2 )  ", compose.Matrix{A: 2, D: 2, E: 1, F: 2}, true},
		{"translate(1 2", identity, false},
		{"rotate(1 2)", identity, false},
		{"skew(10)", identity, false},
		{"Scale(2)", identity, false},
	}
	for _, tt := range tests {
		got, ok := parseTransform(tt.s)
		if ok != tt.ok || !matrixNear(got, tt.want) {
			t.Errorf("parseTransform(%q) = %v, %v; want %v, %v", tt.s, got, ok, tt.want, tt.ok)
		}
	}
}

func matrixNear(a, b compose.Matrix) bool {
	e := func(x, y float64) bool { return math.Abs(x-y) < 1e-9 }
	return e(a.A, b.A) && e(a.B, b.B) && e(a.C, b.C) && e(a.D, b.D) && e(a.E, b.E) && e(a.F, b.F)
}

func TestViewBoxTransform_PreserveAspectRatio(t *testing.T) {
	vb := viewBox{w: 10, h: 20, ok: true}
	tests := []struct {
		par  string
		want compose.Matrix
	}{
		{"", compose.Matrix{A: 5, D: 5, E: 25}},
		{"xMinYMin", compose.Matrix{A: 5, D: 5}},
		{"xMaxYMax meet", compose.Matrix{A: 5, D: 5, E: 50}},
		{"xMidYMid slice", compose.Matrix{A: 10, D: 10, F: -50}},
		{"xMinYMax slice", compose.Matrix{A: 10, D: 10, F: -100}},
		{"none", compose.Matrix{A: 10, D: 5}},
		{"defer xMinYMin", compose.Matrix{A: 5, D: 5}},
		{"bogus", compose.Matrix{A: 5, D: 5, E: 25}},
	}
	for _, tt := range tests {
		got, ok := viewBoxTransform(vb, parseAspect(tt.par), 100, 100)
		if !ok || !matrixNear(got, tt.want) {
			t.Errorf("preserveAspectRatio %q: %v, want %v", tt.par, got, tt.want)
		}
	}
}

func TestDocument_Size(t *testing.T) {
	tests := []struct {
		src  string
		want Size
	}{
		{`<svg width="100" height="50"/>`, Size{Width: 75, Height: 37.5, HasWidth: true, HasHeight: true, Ratio: 2}},
		{`<svg viewBox="0 0 40 10"/>`, Size{Ratio: 4}},
		{`<svg width="100%" viewBox="0 0 40 10"/>`, Size{Ratio: 4}},
		{`<svg width="1in"/>`, Size{Width: 72, HasWidth: true}},
		{`<svg width="10mm" height="2em"/>`, Size{Width: 10 * 96 / 25.4 * 0.75, Height: 24, HasWidth: true, HasHeight: true, Ratio: 10 * 96 / 25.4 / 32}},
		{`<svg viewBox="0 0 -1 10" width="20"/>`, Size{Width: 15, HasWidth: true}},
		{`<?xml version="1.0"?><!DOCTYPE svg><svg width="10" height="10"/>`, Size{Width: 7.5, Height: 7.5, HasWidth: true, HasHeight: true, Ratio: 1}},
	}
	for _, tt := range tests {
		got := parse(t, tt.src).Size()
		if math.Abs(got.Width-tt.want.Width) > 1e-9 || math.Abs(got.Height-tt.want.Height) > 1e-9 ||
			got.HasWidth != tt.want.HasWidth || got.HasHeight != tt.want.HasHeight || math.Abs(got.Ratio-tt.want.Ratio) > 1e-9 {
			t.Errorf("%s: size %+v, want %+v", tt.src, got, tt.want)
		}
	}
}

func TestParse_Errors(t *testing.T) {
	if _, err := Parse(context.Background(), []byte("<html><p>no drawing</p>"), Options{}); !errors.Is(err, ErrNotSVG) {
		t.Errorf("no svg: %v, want ErrNotSVG", err)
	}
	if _, err := Parse(context.Background(), []byte(strings.Repeat(" ", maxSourceBytes+1)), Options{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v, want ErrTooLarge", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Parse(ctx, []byte("<svg/>"), Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v, want context.Canceled", err)
	}
}

func TestParse_PrefixedNamespace(t *testing.T) {
	// Elements written with a prefix bound to the SVG namespace are SVG
	// elements (Namespaces in XML 1.0, 4).
	img := draw(t, `<svg:svg xmlns:svg="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 10 10"><svg:rect width="5" height="5" fill="red"/></svg:svg>`, 100, 100)
	checkProbes(t, img, 40, []probe{{25, 25, red}, {75, 75, white}})
	if got := string(unprefixSVG([]byte(`<a:svg xmlns:a="http://www.w3.org/2000/svg"><a:g/></a:svg>`))); got != `<svg xmlns:a="http://www.w3.org/2000/svg"><g/></svg>` {
		t.Errorf("unprefixSVG = %q", got)
	}
	if got := string(unprefixSVG([]byte(`<svg><a:g/></svg>`))); got != `<svg><a:g/></svg>` {
		t.Errorf("undeclared prefix rewritten: %q", got)
	}
}

func TestParse_ForeignContentNames(t *testing.T) {
	// The HTML parser keeps SVG's camel-case names in foreign content,
	// which the renderer relies on.
	d := parse(t, `<svg viewBox="0 0 10 10" preserveAspectRatio="none"><defs><linearGradient id="g" gradientUnits="userSpaceOnUse"/><clipPath id="c" clipPathUnits="objectBoundingBox"/></defs><![CDATA[ ignored ]]></svg>`)
	if !d.viewBox.ok || !d.par.none {
		t.Errorf("viewBox %+v, preserveAspectRatio %+v", d.viewBox, d.par)
	}
	for id, name := range map[string]string{"g": "linearGradient", "c": "clipPath"} {
		n := d.ids[id]
		if n == nil || n.Data != name {
			t.Fatalf("element %s: %v", id, n)
		}
	}
	if attrOf(d.ids["g"], "gradientUnits") != "userSpaceOnUse" || attrOf(d.ids["c"], "clipPathUnits") != "objectBoundingBox" {
		t.Errorf("attribute names lost their case")
	}
}

func TestDraw_Warnings(t *testing.T) {
	tests := []struct {
		src  string
		code string
	}{
		{`<svg><filter id="f"/><rect width="1" height="1" filter="url(#f)"/></svg>`, "unsupported-svg-property"},
		{`<svg><mask id="m"/><rect width="1" height="1" mask="url(#m)"/></svg>`, "unsupported-svg-property"},
		{`<svg><path d="M0 0L1 1" marker-end="url(#m)"/></svg>`, "unsupported-svg-property"},
		{`<svg><pattern id="p"/><rect width="1" height="1" fill="url(#p) red"/></svg>`, "unsupported-svg-paint"},
		{`<svg><linearGradient id="g" spreadMethod="repeat"><stop/><stop offset="1"/></linearGradient><rect width="1" height="1" fill="url(#g)"/></svg>`, "unsupported-svg-paint"},
		{`<svg><foreignObject/></svg>`, "unsupported-svg-element"},
	}
	for _, tt := range tests {
		ws := parse(t, tt.src).Warnings()
		found := false
		for _, w := range ws {
			found = found || w.Code == tt.code
		}
		if !found {
			t.Errorf("%s: warnings %v, want one %s", tt.src, ws, tt.code)
		}
	}
}

func TestDraw_UseRecursionBounded(t *testing.T) {
	// Each level uses the previous one twice: 2^30 rectangles unbounded.
	var b strings.Builder
	b.WriteString(`<svg width="10" height="10"><rect id="l0" width="1" height="1"/>`)
	for i := 1; i <= 30; i++ {
		b.WriteString(`<g id="l` + strconv.Itoa(i) + `"><use href="#l` + strconv.Itoa(i-1) + `"/><use href="#l` + strconv.Itoa(i-1) + `"/></g>`)
	}
	b.WriteString(`<use href="#l30"/><use id="self" href="#self"/></svg>`)
	d := parse(t, b.String())
	if frags := d.Draw(context.Background(), compose.Rect{Width: 10, Height: 10}); len(frags) == 0 {
		t.Errorf("nothing drawn")
	}
	found := false
	for _, w := range d.Warnings() {
		found = found || w.Code == "svg-limit"
	}
	if !found {
		t.Errorf("no limit warning: %v", d.Warnings())
	}
}

func TestDraw_GroupNestingBounded(t *testing.T) {
	src := `<svg width="10" height="10">` + strings.Repeat(`<g opacity="0.9">`, 40) + `<rect width="10" height="10"/>` + strings.Repeat(`</g>`, 40) + `</svg>`
	frags := parse(t, src).Draw(context.Background(), compose.Rect{Width: 10, Height: 10})
	if d := groupDepth(frags); d > MaxGroupDepth {
		t.Errorf("group depth %d, want at most %d", d, MaxGroupDepth)
	}
}

func groupDepth(frags []compose.Fragment) int {
	best := 0
	for _, f := range frags {
		if g, ok := f.(*compose.Group); ok {
			best = max(best, 1+groupDepth(g.Content))
		}
	}
	return best
}

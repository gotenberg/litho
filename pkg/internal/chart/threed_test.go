package chart

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// column3D returns a clustered 3-D column chart of three series over four
// categories, seen from rotX 15 and rotY 20 with right-angled axes, as
// litho/pptx chart-3d's first chart.
func column3D() *Chart {
	g := &Group{Kind: Bar, ThreeD: true, GapWidth: 150, GapDepth: 150, XAxis: 0, YAxis: 1, ZAxis: -1}
	for i, col := range []typeset.Color{red, blue, {R: 0.5, G: 0.5, B: 0.5, A: 1}} {
		g.Series = append(g.Series, &Series{Index: i, Values: Data{Values: nums(4, 2, 3, 4), Cached: true}, Fill: &Paint{Color: col}})
	}
	return &Chart{
		Axes:   plainAxes(),
		Groups: []*Group{g},
		View3D: &View3D{RotX: 15, RotY: 20, Depth: 100, Perspective: 30, RightAngle: true},
	}
}

// The plot of litho/pptx chart-3d's clustered column, in pixels of
// PowerPoint's 100 ppi bitmap: the projected box spans 527.5 by 190.5
// pixels; its front plane is 505 wide and 172 high, and its floor 70
// deep, its back wall 23.9 pixels right of and 18.1 above the front.
func TestProjection_MatchesPowerPointColumn(t *testing.T) {
	c := column3D()
	rd := &renderer{ctx: context.Background(), c: c}
	p := rd.threeD(c.Groups[0])
	if p == nil {
		t.Fatal("no projection for a 3-D bar group")
	}
	x := &dim{category: true, n: 4, between: true}
	inner := typeset.Rect{X: 2, Y: 3.5, Width: 527.5, Height: 190.5}
	f := p.layout(rd, c.Groups[0], x, inner)
	for _, tc := range []struct {
		name      string
		got, want float64
	}{
		{"front width", f.Width, 505},
		{"front height", f.Height, 172},
		{"depth", p.depth, 70},
		{"back wall right", p.depth * p.dx, 23.9},
		{"back wall up", p.depth * p.dy, 18.1},
		{"front left", f.X, 2},
		{"front bottom", f.Y + f.Height, 194},
	} {
		if math.Abs(tc.got-tc.want) > 2 {
			t.Errorf("%s = %.2f, want %.2f within 2 pixels", tc.name, tc.got, tc.want)
		}
	}
	// A bar 28 pixels wide is 28 deep, centred in its row: 21 pixels
	// behind the floor's front edge.
	z0, z1 := p.row(0)
	if math.Abs(z1-z0-28) > 1 || math.Abs(z0-21) > 1 {
		t.Errorf("bar depths %.2f to %.2f, want 21 to 49", z0, z1)
	}
}

func TestRender_ThreeDFacesAreShaded(t *testing.T) {
	c := column3D()
	frags := Render(context.Background(), ahem(t), c, chartRect)
	for _, f := range []float64{1, faceUp, faceSide} {
		if n := len(paths(frags, shade(red, f))); n != 4 {
			t.Errorf("%d faces of red shaded %.2f, want 4", n, f)
		}
	}
	// The top of a bar runs back from its front's upper edge: up and to
	// the right.
	front, top := bounds(paths(frags, red)[0]), bounds(paths(frags, shade(red, faceUp))[0])
	if !(top.Y < front.Y && top.X+top.Width > front.X+front.Width && math.Abs(top.Y+top.Height-front.Y) < 1e-6) {
		t.Errorf("top %+v does not stand on front %+v", top, front)
	}
}

func TestRender_ThreeDPaintsBackToFront(t *testing.T) {
	c := column3D()
	c.Groups[0].Rows = true
	frags := Render(context.Background(), ahem(t), c, chartRect)
	// The first series stands in the front row, so it is drawn last.
	last := map[typeset.Color]int{}
	i := 0
	walk(frags, 0, func(f typeset.Fragment, _ int) {
		if p, ok := f.(*typeset.Path); ok && p.Fill != nil {
			last[p.Fill.Color] = i
		}
		i++
	})
	if !(last[red] > last[blue] && last[blue] > last[typeset.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}]) {
		t.Errorf("fronts drawn at %v, want the first series last", last)
	}
}

func TestRender_FlatWithoutView(t *testing.T) {
	c := column3D()
	c.View3D = nil
	frags := Render(context.Background(), ahem(t), c, chartRect)
	if n := len(paths(frags, shade(red, faceUp))); n != 0 {
		t.Errorf("%d shaded faces without a view, want 0", n)
	}
	if n := len(paths(frags, red)); n != 4 {
		t.Errorf("%d red bars, want 4", n)
	}
}

func TestRender_PieTilt(t *testing.T) {
	c := &Chart{
		Groups: []*Group{{Kind: Pie, ThreeD: true, XAxis: -1, YAxis: -1, Series: []*Series{
			{Values: Data{Values: nums(1), Cached: true}, Fill: &Paint{Color: red}},
		}}},
		View3D: &View3D{RotX: 15, Depth: 100, RightAngle: true},
	}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	top := paths(frags, red)
	rim := paths(frags, shade(red, pieRim))
	if len(top) != 1 || len(rim) != 1 {
		t.Fatalf("got %d tops and %d rims, want 1 each", len(top), len(rim))
	}
	// poi/xlsx 45544: a pie at rotX 15 without perspective is 0.258 of
	// its width high, its rim 0.2375 of its radius.
	b, rb := bounds(top[0]), bounds(rim[0])
	if got := b.Height / b.Width; math.Abs(got-0.258) > 0.005 {
		t.Errorf("height over width %.4f, want 0.258", got)
	}
	if got := (rb.Y + rb.Height - (b.Y + b.Height)) / (b.Width / 2); math.Abs(got-0.2375) > 0.01 {
		t.Errorf("rim over radius %.4f, want 0.2375", got)
	}
}

func TestHull(t *testing.T) {
	pts := []typeset.Point{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 2}, {X: 0, Y: 2}, {X: 1, Y: 0}}
	h := hull(pts)
	if len(h) != 4 {
		t.Errorf("hull %v, want the 4 corners", h)
	}
}

// TestRender_Office3DValueAxisEnd holds a three-dimensional value axis
// under OfficeRules to the multiple of its major unit the data reach,
// without LibreOffice's interval for data at the end: data up to 4 run
// to 4, and data up to 5 run to 5 by the unit of the axis from 0 to 6, 1,
// not by the half unit the axis from 0 to 5 has room for.
func TestRender_Office3DValueAxisEnd(t *testing.T) {
	for _, tc := range []struct {
		top           float64
		want, refused []string
	}{
		{4, []string{" 3.5 ", " 4 "}, []string{" 4.5 "}},
		{5, []string{" 4 ", " 5 "}, []string{" 0.5 ", " 4.5 ", " 6 "}},
	} {
		c := column3D()
		c.Rules = OfficeRules
		c.Groups[0].Series[0].Values = Data{Values: nums(tc.top, 2, 3, 4), Cached: true}
		c.Axes[0].Labels, c.Axes[1].Labels = LabelNextTo, LabelNextTo
		frags := Render(context.Background(), ahem(t), c, typeset.Rect{Width: 361.2, Height: 300})
		// The category labels come first: the value axis's follow them.
		got := " " + strings.Join(texts(frags)[4:], " ") + " "
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("data up to %v: labels = %q, want %q among them", tc.top, got, w)
			}
		}
		for _, r := range tc.refused {
			if strings.Contains(got, r) {
				t.Errorf("data up to %v: labels = %q, want no %q", tc.top, got, r)
			}
		}
	}
}

// TestTrimScale_DropsTheIntervalNoValueReaches pins which end interval a
// trimmed scale loses: the one past the first multiple of the major unit
// the data reach, at an automatic end that is not zero.
func TestTrimScale_DropsTheIntervalNoValueReaches(t *testing.T) {
	fixed := &Axis{Min: math.NaN(), Max: 6, MajorUnit: math.NaN(), MinorUnit: math.NaN()}
	for _, tc := range []struct {
		name             string
		sc               scale
		ax               *Axis
		lo, hi           float64
		wantMin, wantMax float64
	}{
		{"data at the multiple below the end", scale{min: 0, max: 6, major: 1}, nil, 1, 5, 0, 5},
		{"data past the multiple below the end", scale{min: 0, max: 6, major: 2}, nil, 1, 4.8, 0, 6},
		{"a rounding short of the multiple", scale{min: 0, max: 0.6, major: 0.1}, nil, 0.1, 0.6 - 0.1, 0, 0.5},
		{"negative data", scale{min: -6, max: 0, major: 1}, nil, -5, -1, -5, 0},
		{"an end the axis fixes", scale{min: 0, max: 6, major: 1}, fixed, 1, 5, 0, 6},
		{"one interval", scale{min: 0, max: 1, major: 1}, nil, 0, 0, 0, 1},
		{"a logarithmic scale", scale{min: 1, max: 1000, major: 1, log: 10}, nil, 1, 10, 1, 1000},
	} {
		got := trimScale(tc.sc, tc.ax, tc.lo, tc.hi)
		if got.min != tc.wantMin || math.Abs(got.max-tc.wantMax) > 1e-12 || got.major != tc.sc.major {
			t.Errorf("%s: from %v to %v by %v, want from %v to %v by %v", tc.name, got.min, got.max, got.major, tc.wantMin, tc.wantMax, tc.sc.major)
		}
	}
}

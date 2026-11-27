package chart

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestDims_OfficeDeletedAxisSharesTheFirstScale holds a group whose value
// axis is deleted to the first group's value axis under OfficeRules, and
// to a scale of its own under LibreOffice's.
func TestDims_OfficeDeletedAxisSharesTheFirstScale(t *testing.T) {
	for _, tc := range []struct {
		rules Rules
		want  int
	}{{OfficeRules, 1}, {LibreOfficeRules, 2}} {
		c := barChart(1, 2)
		c.Rules = tc.rules
		sec := &Axis{Kind: ValueAxis, Position: AxisLeft, Deleted: true, Min: math.NaN(), Max: math.NaN(), MajorUnit: math.NaN(), MinorUnit: math.NaN()}
		c.Axes = append(c.Axes, sec)
		c.Groups = append(c.Groups, &Group{Kind: Line, XAxis: 0, YAxis: 2, Series: []*Series{{Values: Data{Values: nums(4, 3), Cached: true}}}})
		rd := &renderer{ctx: context.Background(), fonts: ahem(t), c: c, page: chartRect}
		_, ys, yOf := rd.dims(c.Groups[0])
		if len(ys) != tc.want {
			t.Fatalf("rules %v: %d value dimensions, want %d", tc.rules, len(ys), tc.want)
		}
		if tc.want == 1 && (yOf[c.Groups[1]] != ys[0] || ys[0].hi != 4) {
			t.Errorf("rules %v: the line is not drawn on the first axis, its range %v to %v", tc.rules, ys[0].lo, ys[0].hi)
		}
	}
}

// bubbleChart returns a bubble chart of one red series with the given
// sizes at x 1, 2, ... and y 1, 2, ..., its axes unlabelled.
func bubbleChart(rules Rules, sizes ...float64) *Chart {
	var xs, ys []float64
	for i := range sizes {
		xs, ys = append(xs, float64(i+1)), append(ys, float64(i+1))
	}
	ax := plainAxes()
	ax[0].Kind = ValueAxis
	ax[0].Position = AxisBottom
	return &Chart{
		Rules: rules,
		Axes:  ax,
		Groups: []*Group{{Kind: Bubble, BubbleScale: 100, XAxis: 0, YAxis: 1, Series: []*Series{{
			Categories: Data{Values: nums(xs...), Cached: true},
			Values:     Data{Values: nums(ys...), Cached: true},
			Sizes:      Data{Values: nums(sizes...), Cached: true},
			Fill:       &Paint{Color: red},
		}}}},
	}
}

// TestRender_OfficeBubbleSizes holds the largest bubble of an Office chart
// to a quarter of the plot area's smaller side across, the others by the
// square root of their sizes, or by their sizes under sizeRepresents w;
// LibreOffice's largest is twice as wide.
func TestRender_OfficeBubbleSizes(t *testing.T) {
	widths := func(c *Chart) []float64 {
		var out []float64
		for _, p := range paths(Render(context.Background(), ahem(t), c, chartRect), red) {
			out = append(out, bounds(p).Width)
		}
		slices.Sort(out)
		return out
	}
	office := widths(bubbleChart(OfficeRules, 1, 4))
	lo := widths(bubbleChart(LibreOfficeRules, 1, 4))
	if len(office) != 2 || len(lo) != 2 {
		t.Fatalf("got %d and %d bubbles, want 2 each", len(office), len(lo))
	}
	if r := office[1] / lo[1]; math.Abs(r-0.5) > 0.01 {
		t.Errorf("Office's largest bubble is %.3f of LibreOffice's, want 0.5", r)
	}
	if r := office[0] / office[1]; math.Abs(r-0.5) > 0.01 {
		t.Errorf("a bubble of a quarter the size is %.3f as wide, want 0.5", r)
	}
	c := bubbleChart(OfficeRules, 1, 4)
	c.Groups[0].BubbleWidth = true
	if w := widths(c); len(w) != 2 || math.Abs(w[0]/w[1]-0.25) > 0.01 {
		t.Errorf("sized by width: %v, want the small one a quarter as wide", w)
	}
}

// TestRender_OfficeBubbleSizeLabels holds a bubble's size label to the
// right of the bubble under OfficeRules, when the file states no
// position.
func TestRender_OfficeBubbleSizeLabels(t *testing.T) {
	c := bubbleChart(OfficeRules, 7)
	c.Groups[0].Series[0].Labels = Labels{ShowSize: true}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	if got := texts(frags); !slices.Contains(got, "7") {
		t.Fatalf("texts %q, want the size 7", got)
	}
	bubble := bounds(paths(frags, red)[0])
	var label *typeset.Text
	walk(frags, 0, func(f typeset.Fragment, _ int) {
		if tx, ok := f.(*typeset.Text); ok && len(tx.Glyphs) == 1 && tx.Glyphs[0].Text == "7" {
			label = tx
		}
	})
	if label == nil {
		t.Fatal("no label")
	}
	if x := label.Origin.X; x < bubble.X+bubble.Width+officeBubbleLabelGap-0.5 {
		t.Errorf("label at x %.2f, want right of the bubble's edge %.2f", x, bubble.X+bubble.Width)
	}
}

// TestValueText_OfficeGeneralElevenCharacters holds a General data label
// value to eleven characters under OfficeRules.
func TestValueText_OfficeGeneralElevenCharacters(t *testing.T) {
	office := &renderer{c: &Chart{Rules: OfficeRules}}
	lo := &renderer{c: &Chart{}}
	for _, tc := range []struct {
		v          float64
		code, want string
	}{
		{1189.6790093003899, "", "1189.679009"},
		{573.65446810428102, "General", "573.6544681"},
		{1594.36831022005, "", "1594.36831"},
		{-246.235253149996, "", "-246.235253"},
		{1000, "", "1000"},
		{1189.6790093003899, "0.0", "1189.7"},
	} {
		if got := office.valueText(tc.code, tc.v); got != tc.want {
			t.Errorf("Office %v in %q: %q, want %q", tc.v, tc.code, got, tc.want)
		}
	}
	if got := lo.valueText("", 1189.6790093003899); got != "1189.67900930039" {
		t.Errorf("LibreOffice: %q, want every digit", got)
	}
}

// TestRender_InvertedNegativeBars fills a negative bar of a series that
// inverts them with the inverted fill, outlined in black when the series
// has no outline, and leaves its positive bars alone.
func TestRender_InvertedNegativeBars(t *testing.T) {
	c := barChart(2, -1)
	white := typeset.Color{R: 1, G: 1, B: 1, A: 1}
	c.Groups[0].Series[0].Invert = &Paint{Color: white}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	if got := len(paths(frags, red)); got != 1 {
		t.Errorf("%d red bars, want the positive one", got)
	}
	inv := paths(frags, white)
	if len(inv) != 1 || inv[0].Stroke == nil || inv[0].Stroke.Color != typeset.Black {
		t.Fatalf("inverted bars %+v, want one outlined in black", inv)
	}
}

// TestRender_OfficeSpanCap labels a tall Office value axis over -4 to 5 by
// 1 from -5 to 6, eleven intervals, and refuses the unit 0.5 whose span
// with its ends takes 19.8; and one over 1 to 5 from 0 to 6 by 1, refusing
// the unit 0.5 whose extent with its end takes 10.5.
func TestRender_OfficeSpanCap(t *testing.T) {
	c := barChart(-4, 2, 3, 4, 5)
	c.Rules = OfficeRules
	c.Axes[1].Labels = LabelNextTo
	frags := Render(context.Background(), ahem(t), c, typeset.Rect{Width: 300, Height: 400})
	got := texts(frags)
	for _, w := range []string{"-5", "0", "6"} {
		if !slices.Contains(got, w) {
			t.Errorf("labels %q, want %q", got, w)
		}
	}
	for _, r := range []string{"0.5", "7", "-6"} {
		if slices.Contains(got, r) {
			t.Errorf("labels %q, want no %q", got, r)
		}
	}
	c = barChart(1, 2, 3, 4, 5)
	c.Rules = OfficeRules
	c.Axes[1].Labels = LabelNextTo
	got = texts(Render(context.Background(), ahem(t), c, typeset.Rect{Width: 300, Height: 400}))
	if !slices.Contains(got, "6") || slices.Contains(got, "0.5") {
		t.Errorf("labels %q, want 0 to 6 by 1", got)
	}
}

// TestRender_OfficePieLayout holds a flat, unlabelled Office pie to the
// square officePie leaves it, and a labelled one to LibreOffice's layout.
func TestRender_OfficePieLayout(t *testing.T) {
	pie := func(labels bool, explosion float64) *Chart {
		s := &Series{Values: Data{Values: nums(1, 1, 1, 1), Cached: true}, Fill: &Paint{Color: red}}
		if explosion > 0 {
			s.Points = []Point{{Index: 0, Explosion: explosion, HasExplosion: true}}
		}
		if labels {
			s.Labels = Labels{ShowValue: true}
		}
		return &Chart{Rules: OfficeRules, Groups: []*Group{{Kind: Pie, XAxis: -1, YAxis: -1, Series: []*Series{s}}}}
	}
	r := typeset.Rect{Width: 300, Height: 200}
	box := func(c *Chart) typeset.Rect {
		var b typeset.Rect
		for i, p := range paths(Render(context.Background(), ahem(t), c, r), red) {
			if i == 0 {
				b = bounds(p)
			} else {
				b = union(b, bounds(p))
			}
		}
		return b
	}
	// The room is 6.5 pt in from the sides and bottom and officeTopPad
	// under the top; the pie is officeMinLabelRoom/2 under the room's top
	// and officePieInset from its bottom, its radius not shrunk for the
	// exploded sectors that reach past it.
	d := 200 - officeTopPad - officePad - officeMinLabelRoom/2 - officePieInset
	got := box(pie(false, 0))
	if math.Abs(got.Height-d) > 0.05 {
		t.Errorf("pie %.2f high, want %.2f", got.Height, d)
	}
	if cy := got.Y + got.Height/2; math.Abs(cy-(officeTopPad+officeMinLabelRoom/2+d/2)) > 0.05 {
		t.Errorf("centre at %.2f, want %.2f", cy, officeTopPad+officeMinLabelRoom/2+d/2)
	}
	if ex := box(pie(false, 20)); ex.Height <= d+1 {
		t.Errorf("pie with an exploded point %.2f high, want that point past the %.2f pt square", ex.Height, d)
	}
	c := pie(false, 0)
	c.Groups[0].Series[0].Explosion = 20
	if ex := box(c); ex.Height > d+0.05 {
		t.Errorf("pie with its series exploded %.2f high, want it within the %.2f pt square", ex.Height, d)
	}
	if lo := box(pie(true, 0)); math.Abs(lo.Height-got.Height) < 1 {
		t.Errorf("a labelled pie %.2f high, want LibreOffice's layout", lo.Height)
	}
}

// TestRender_OfficePieSeriesFill holds a varied Office pie's point without
// a fill of its own to its series' fill, where LibreOffice's rule varies
// it.
func TestRender_OfficePieSeriesFill(t *testing.T) {
	blue := typeset.Color{B: 1, A: 1}
	for _, rules := range []Rules{OfficeRules, LibreOfficeRules} {
		s := &Series{Values: Data{Values: nums(1, 1, 1), Cached: true}, Fill: &Paint{Color: red},
			Points: []Point{{Index: 1, Fill: &Paint{Color: blue}}}}
		c := &Chart{Rules: rules, Groups: []*Group{{Kind: Pie, VaryColors: true, XAxis: -1, YAxis: -1, Series: []*Series{s}}}}
		frags := Render(context.Background(), ahem(t), c, typeset.Rect{Width: 300, Height: 200})
		want := 2
		if rules != OfficeRules {
			want = 0
		}
		if n := len(paths(frags, red)); n != want {
			t.Errorf("rules %v: %d sectors in the series' fill, want %d", rules, n, want)
		}
		if n := len(paths(frags, blue)); n != 1 {
			t.Errorf("rules %v: %d sectors in the point's fill, want 1", rules, n)
		}
	}
}

// TestRender_OfficeLabelLayout holds an Office pie's data label to its
// manual layout: in the edge mode its text starts officeLabelInsetX right
// of the chart's fraction and its leader line runs from the sector to a
// level stub beside it; in the factor mode the label moves by the
// fraction of the chart from its automatic place.
func TestRender_OfficeLabelLayout(t *testing.T) {
	r := typeset.Rect{Width: 300, Height: 200}
	label := func(lay *LabelLayout) (*typeset.Text, []typeset.Fragment) {
		s := &Series{Values: Data{Values: nums(1, 3), Cached: true}, Fill: &Paint{Color: red},
			Labels: Labels{ShowValue: true, Leaders: true, Style: TextStyle{Size: 10, Color: typeset.Black}}}
		s.Points = []Point{{Index: 0, Labels: Labels{ShowValue: true, Leaders: true, Layout: lay, Style: TextStyle{Size: 10, Color: typeset.Black}}, HasLabels: true}}
		c := &Chart{Rules: OfficeRules, Groups: []*Group{{Kind: Pie, XAxis: -1, YAxis: -1, Series: []*Series{s}}}}
		frags := Render(context.Background(), ahem(t), c, r)
		var got *typeset.Text
		walk(frags, 0, func(fr typeset.Fragment, _ int) {
			if tx, ok := fr.(*typeset.Text); ok && got == nil && len(tx.Glyphs) > 0 && tx.Glyphs[0].Text == "1" {
				got = tx
			}
		})
		if got == nil {
			t.Fatalf("layout %+v: no label", lay)
		}
		return got, frags
	}
	auto, _ := label(nil)
	edge, frags := label(&LabelLayout{X: 0.05, Y: 0.1, EdgeX: true, EdgeY: true})
	if x := 0.05*r.Width + officeLabelInsetX; math.Abs(edge.Origin.X-x) > 0.01 {
		t.Errorf("edge label at x %.2f, want %.2f", edge.Origin.X, x)
	}
	// The line's half leading lies over the ascent.
	if top := edge.Origin.Y - 8; math.Abs(top-(0.1*r.Height+officeLabelInsetY)) > 1 {
		t.Errorf("edge label's top at %.2f, want %.2f", top, 0.1*r.Height+officeLabelInsetY)
	}
	leaders := 0
	walk(frags, 0, func(fr typeset.Fragment, _ int) {
		if p, ok := fr.(*typeset.Path); ok && p.Stroke != nil && p.Fill == nil && len(p.Ops) == 3 {
			b := bounds(p)
			if math.Abs(b.X-(edge.Origin.X+10+officeLeaderGap)) < 0.01 && p.Stroke.Width == officeLeaderWidth {
				leaders++
			}
		}
	})
	if leaders != 1 {
		t.Errorf("%d leader lines from the label's right, want 1", leaders)
	}
	moved, _ := label(&LabelLayout{X: 0.1, Y: -0.05})
	if dx, dy := moved.Origin.X-auto.Origin.X, moved.Origin.Y-auto.Origin.Y; math.Abs(dx-30) > 0.01 || math.Abs(dy+10) > 0.01 {
		t.Errorf("factor label moved by (%.2f, %.2f), want (30, -10)", dx, dy)
	}
}

// TestRender_OfficePieLabelWrap holds an Office pie's data label to a
// fifth of the chart's width, its insets left out, where LibreOffice's
// rule keeps it on one line.
func TestRender_OfficePieLabelWrap(t *testing.T) {
	for _, rules := range []Rules{OfficeRules, LibreOfficeRules} {
		s := &Series{Values: Data{Values: nums(1, 3), Cached: true}, Categories: Data{Values: strs("AAAA BBBB CCCC DDDD", "E"), Cached: true},
			Labels: Labels{ShowCategory: true, Style: TextStyle{Size: 10, Color: typeset.Black}}}
		c := &Chart{Rules: rules, Groups: []*Group{{Kind: Pie, XAxis: -1, YAxis: -1, Series: []*Series{s}}}}
		got := texts(Render(context.Background(), ahem(t), c, typeset.Rect{Width: 500, Height: 300}))
		// 94 points hold two words of Ahem 10 and their space, 90 points.
		want := []string{"AAAA BBBB ", "CCCC DDDD"}
		if rules != OfficeRules {
			want = []string{"AAAA BBBB CCCC DDDD"}
		}
		if !slices.Contains(got, want[0]) || !slices.Contains(got, want[len(want)-1]) {
			t.Errorf("rules %v: lines %q, want %q", rules, got, want)
		}
	}
	// A label that states its box 0.4 of the chart wide wraps in it
	// instead: 194 points hold the four words, 190.
	s := &Series{Values: Data{Values: nums(1, 3), Cached: true}, Categories: Data{Values: strs("AAAA BBBB CCCC DDDD", "E"), Cached: true},
		Labels: Labels{ShowCategory: true, Width: 0.4, Style: TextStyle{Size: 10, Color: typeset.Black}}}
	c := &Chart{Rules: OfficeRules, Groups: []*Group{{Kind: Pie, XAxis: -1, YAxis: -1, Series: []*Series{s}}}}
	if got := texts(Render(context.Background(), ahem(t), c, typeset.Rect{Width: 500, Height: 300})); !slices.Contains(got, "AAAA BBBB CCCC DDDD") {
		t.Errorf("stated width: lines %q, want one line", got)
	}
}

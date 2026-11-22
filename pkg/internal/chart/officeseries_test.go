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

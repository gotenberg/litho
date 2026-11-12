package chart

import (
	"context"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// chartRect is the rectangle the draw tests render into.
var chartRect = compose.Rect{X: 10, Y: 20, Width: 400, Height: 200}

// barChart returns a clustered column chart of one red series with values,
// no title, no legend and no axis labels.
func barChart(values ...float64) *Chart {
	return &Chart{
		Axes: plainAxes(),
		Groups: []*Group{{Kind: Bar, GapWidth: 150, XAxis: 0, YAxis: 1, Series: []*Series{
			{Name: "S", Values: Data{Values: nums(values...), Cached: true}, Fill: &Paint{Color: red}},
		}}},
	}
}

func TestRender_BarGeometry(t *testing.T) {
	c := barChart(1, 2)
	frags := Render(context.Background(), ahem(t), c, chartRect)
	bars := paths(frags, red)
	if len(bars) != 2 {
		t.Fatalf("got %d bars, want 2", len(bars))
	}
	// The plot area is the chart less 2% each side; the scale runs from 0
	// to 2.5; each category is half the width and a bar 1/2.5 of it.
	inner := compose.Rect{X: 18, Y: 24, Width: 384, Height: 192}
	catW := inner.Width / 2
	barW := catW / 2.5
	b := bounds(bars[1])
	want := compose.Rect{X: inner.X + catW + catW/2 - barW/2, Y: inner.Y + inner.Height*(1-2/2.5), Width: barW, Height: inner.Height * 2 / 2.5}
	if math.Abs(b.X-want.X) > 1e-6 || math.Abs(b.Y-want.Y) > 1e-6 || math.Abs(b.Width-want.Width) > 1e-6 || math.Abs(b.Height-want.Height) > 1e-6 {
		t.Errorf("second bar = %+v, want %+v", b, want)
	}
}

func TestRender_GapAndOverlap(t *testing.T) {
	c := barChart(1, 1)
	c.Groups[0].Series = append(c.Groups[0].Series, &Series{Index: 1, Values: Data{Values: nums(1, 1), Cached: true}, Fill: &Paint{Color: blue}})
	c.Groups[0].GapWidth, c.Groups[0].Overlap = 100, -50
	frags := Render(context.Background(), ahem(t), c, chartRect)
	r, b := bounds(paths(frags, red)[0]), bounds(paths(frags, blue)[0])
	// Two bars, half a bar apart, and a bar's width of gap: 3.5 bars a
	// category.
	catW := 384.0 / 2
	barW := catW / 3.5
	if math.Abs(r.Width-barW) > 1e-6 || math.Abs(b.X-(r.X+1.5*barW)) > 1e-6 {
		t.Errorf("bars at %v and %v width %v, want width %v, %v apart", r.X, b.X, r.Width, barW, 1.5*barW)
	}
}

func TestRender_StackedAndHorizontal(t *testing.T) {
	c := barChart(1, 2)
	c.Groups[0].Grouping = Stacked
	c.Groups[0].Horizontal = true
	c.Groups[0].Series = append(c.Groups[0].Series, &Series{Index: 1, Values: Data{Values: nums(1, 1), Cached: true}, Fill: &Paint{Color: blue}})
	frags := Render(context.Background(), ahem(t), c, chartRect)
	reds, blues := paths(frags, red), paths(frags, blue)
	if len(reds) != 2 || len(blues) != 2 {
		t.Fatalf("got %d and %d bars", len(reds), len(blues))
	}
	r, b := bounds(reds[0]), bounds(blues[0])
	if math.Abs(b.X-(r.X+r.Width)) > 1e-6 || math.Abs(b.Y-r.Y) > 1e-6 {
		t.Errorf("stacked bar %+v does not start where %+v ends", b, r)
	}
	// The first category is at the bottom.
	if bounds(reds[0]).Y < bounds(reds[1]).Y {
		t.Errorf("first category above the second")
	}
}

func TestRender_CategoryAxisAtZero(t *testing.T) {
	c := barChart(-1, 3)
	line := &Stroke{Color: blue, Width: 1}
	c.Axes[0].Line = line
	frags := Render(context.Background(), ahem(t), c, chartRect)
	var axis *compose.Path
	walk(frags, 0, func(fr compose.Fragment, _ int) {
		if p, ok := fr.(*compose.Path); ok && p.Stroke != nil && near(p.Stroke.Color, blue) && axis == nil {
			axis = p
		}
	})
	if axis == nil {
		t.Fatal("no category axis line")
	}
	// The scale runs from -1.5 to 3.5: zero is 30% of the way up.
	b := bounds(axis)
	if want := 24 + 192*0.7; math.Abs(b.Y-want) > 1e-6 || b.Height != 0 {
		t.Errorf("axis line at y %v, want %v", b.Y, want)
	}
}

func TestRender_PieStartsAtTwelveClockwise(t *testing.T) {
	c := &Chart{Groups: []*Group{{Kind: Pie, VaryColors: true, XAxis: -1, YAxis: -1, Series: []*Series{
		{Values: Data{Values: nums(1, 3), Cached: true}},
	}}}, Palette: []compose.Color{red, blue}}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	slices := paths(frags, red)
	if len(slices) != 1 {
		t.Fatalf("got %d red slices", len(slices))
	}
	// The first slice, a quarter, runs from twelve to three o'clock.
	b := bounds(slices[0])
	cx, cy := chartRect.X+chartRect.Width/2, chartRect.Y+chartRect.Height/2
	if math.Abs(b.X-cx) > 1e-6 || math.Abs(b.Y+b.Height-cy) > 1e-6 || b.Width <= 0 {
		t.Errorf("first slice bounds %+v, want the top-right quarter of a pie centred on (%v, %v)", b, cx, cy)
	}
	r := chartRect.Height/2 - pieDistance
	if math.Abs(b.Width-r) > 1e-6 {
		t.Errorf("radius %v, want %v", b.Width, r)
	}
}

func TestRender_TitleAndLegendText(t *testing.T) {
	c := barChart(1, 2)
	c.Title = &Title{Text: "Sales", Style: TextStyle{Size: 14, Color: compose.Black}}
	c.Legend = &Legend{Style: TextStyle{Size: 10, Color: compose.Black}}
	c.Groups[0].Series[0].Name = "Revenue"
	frags := Render(context.Background(), ahem(t), c, chartRect)
	got := texts(frags)
	if !slices.Contains(got, "Sales") || !slices.Contains(got, "Revenue") {
		t.Errorf("texts = %q, want the title and the legend entry", got)
	}
	// The legend takes room at the right: the bars end before it.
	for _, p := range paths(frags, red) {
		if b := bounds(p); b.X+b.Width > chartRect.X+chartRect.Width-legendSideMargin-10 {
			t.Errorf("a bar reaches %v, into the legend", b.X+b.Width)
		}
	}
}

func TestRender_DataLabelsAndAxisLabels(t *testing.T) {
	c := barChart(1.5, 2)
	c.Axes[1].Labels = LabelNextTo
	c.Axes[1].Style = TextStyle{Size: 8, Color: compose.Black}
	c.Axes[1].Format = "0.0"
	c.Groups[0].Series[0].Labels = Labels{ShowValue: true, Format: "0.00", Style: TextStyle{Size: 8, Color: compose.Black}}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	got := strings.Join(texts(frags), " ")
	for _, want := range []string{"1.50", "2.00", "0.0", "2.5"} {
		if !strings.Contains(got, want) {
			t.Errorf("texts %q lack %q", got, want)
		}
	}
}

func TestRender_ResolverFillsUncachedSeries(t *testing.T) {
	c := barChart()
	c.Groups[0].Series[0].Values = Data{Ref: "Sheet1!$A$1:$A$3"}
	var asked string
	c.Resolver = func(ref string) []Value {
		asked = ref
		return nums(1, 2, 3)
	}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	if asked != "Sheet1!$A$1:$A$3" || len(paths(frags, red)) != 3 {
		t.Errorf("resolver asked %q, %d bars drawn", asked, len(paths(frags, red)))
	}
}

// kinds returns a chart of every kind, with titles, legends, labels and
// axes, for the invariants every drawing keeps.
func kinds() map[string]*Chart {
	nan := math.NaN()
	axes := func() []*Axis {
		return []*Axis{
			{Kind: CategoryAxis, BetweenCategories: true, Min: nan, Max: nan, MajorUnit: nan, MinorUnit: nan, Line: &Stroke{Color: blue, Width: 1}, MajorTicks: TickOut, Title: &Title{Text: "X"}},
			{Kind: ValueAxis, Position: AxisLeft, Min: nan, Max: nan, MajorUnit: nan, MinorUnit: nan, MajorGrid: &Stroke{Color: blue}, MinorGrid: &Stroke{Color: blue}, Title: &Title{Text: "Y"}},
		}
	}
	series := func() []*Series {
		return []*Series{
			{Name: "A", Categories: Data{Values: strs("one", "two", "three")}, Values: Data{Values: nums(3, -1, 4)}, Sizes: Data{Values: nums(1, 2, 3)}, Labels: Labels{ShowValue: true, ShowPercent: true, ShowCategory: true}, Smooth: true, Explosion: 10},
			{Index: 1, Name: "B", Categories: Data{Values: nums(1, 2, 3)}, Values: Data{Values: []Value{Num(2), {}, Num(5)}}, Sizes: Data{Values: nums(3, 2, 1)}},
		}
	}
	out := map[string]*Chart{}
	for _, k := range []Kind{Bar, Line, Area, Pie, Doughnut, Scatter, Bubble, Radar} {
		for _, gr := range []Grouping{Clustered, Stacked, PercentStacked} {
			c := &Chart{
				Title:    &Title{Text: "A title that is rather long for the chart"},
				Subtitle: &Title{Text: "Sub"},
				Legend:   &Legend{Position: LegendPosition(gr)},
				Axes:     axes(),
				Area:     Frame{Fill: &Paint{Color: compose.Color{R: 1, G: 1, B: 1, A: 1}}, Border: &Stroke{Color: compose.Black}},
				Groups:   []*Group{{Kind: k, Grouping: gr, XAxis: 0, YAxis: 1, Series: series(), VaryColors: k == Pie, Filled: gr == Stacked}},
			}
			out[k.String()+"/"+string(rune('0'+gr))] = c
		}
	}
	return out
}

func TestRender_Invariants(t *testing.T) {
	for name, c := range kinds() {
		t.Run(name, func(t *testing.T) {
			frags := Render(context.Background(), ahem(t), c, chartRect)
			if len(frags) != 1 {
				t.Fatalf("got %d top fragments, want one clipped group", len(frags))
			}
			g, ok := frags[0].(*compose.Group)
			if !ok || g.Clip == nil || !reflect.DeepEqual(bounds(g.Clip), chartRect) {
				t.Fatalf("the drawing is not clipped to the chart rectangle")
			}
			walk(frags, 1, func(fr compose.Fragment, depth int) {
				if depth > maxDepth {
					t.Errorf("group nesting %d past %d", depth, maxDepth)
				}
				switch f := fr.(type) {
				case *compose.Path:
					for _, op := range f.Ops {
						for _, p := range op.P {
							if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
								t.Fatalf("non-finite point in %+v", f.Ops)
							}
						}
					}
				case *compose.Text:
					if f.Size <= 0 || f.Color.A <= 0 {
						t.Errorf("text run of size %v colour %v", f.Size, f.Color)
					}
				}
			})
			again := Render(context.Background(), ahem(t), c, chartRect)
			if !reflect.DeepEqual(frags, again) {
				t.Error("two renderings differ")
			}
		})
	}
}

func TestRender_RefusesNothingToDraw(t *testing.T) {
	ctx := context.Background()
	if Render(ctx, ahem(t), nil, chartRect) != nil || Render(ctx, nil, barChart(1), chartRect) != nil {
		t.Error("drew a nil chart or without fonts")
	}
	if Render(ctx, ahem(t), barChart(1), compose.Rect{Width: 0, Height: 10}) != nil {
		t.Error("drew into an empty rectangle")
	}
	if frags := Render(ctx, ahem(t), &Chart{Area: Frame{Fill: &Paint{Color: red}}}, chartRect); len(paths(frags, red)) != 1 {
		t.Error("an empty chart draws no chart area")
	}
}

func TestRender_MarkerSymbols(t *testing.T) {
	for sym := MarkerSquare; sym <= MarkerVerticalBar; sym++ {
		ops := markerPath(sym, 50, 50, 10)
		b := bounds(&compose.Path{Ops: ops})
		if b.X < 45-1e-9 || b.Y < 45-1e-9 || b.X+b.Width > 55+1e-9 || b.Y+b.Height > 55+1e-9 || b.Width <= 0 {
			t.Errorf("symbol %d bounds %+v outside its 10-point box", sym, b)
		}
	}
}

func TestAutoColor_CyclesTinted(t *testing.T) {
	c := &Chart{Palette: []compose.Color{red}, PaletteCycleTint: true}
	first, second := c.AutoColor(0, 2), c.AutoColor(1, 2)
	if math.IsNaN(first.R) || first.R >= 1 || !positive(second.G) {
		t.Errorf("cycles %v and %v: want the first shaded and the second tinted", first, second)
	}
	if got := (&Chart{}).AutoColor(13, 20); !near(got, defaultPalette[1]) {
		t.Errorf("default palette colour 13 = %v", got)
	}
}

func TestRender_VaryColorsKeepsSeriesFill(t *testing.T) {
	c := barChart(1, 2)
	c.Palette = []compose.Color{blue}
	c.Groups[0].VaryColors = true
	frags := Render(context.Background(), ahem(t), c, chartRect)
	if n := len(paths(frags, red)); n != 2 {
		t.Errorf("a varied bar series with a fill of its own: %d red bars, want 2", n)
	}
	c.Groups[0].Series[0].Fill = nil
	frags = Render(context.Background(), ahem(t), c, chartRect)
	if n := len(paths(frags, red)); n != 0 {
		t.Errorf("a varied bar series without a fill: %d red bars, want 0", n)
	}
}

// rotatedLabels counts the groups of frags that turn their text.
func rotatedLabels(frags []compose.Fragment) int {
	n := 0
	walk(frags, 0, func(fr compose.Fragment, _ int) {
		if g, ok := fr.(*compose.Group); ok && g.Transform.B != 0 {
			if _, isText := g.Content[0].(*compose.Text); isText {
				n++
			}
		}
	})
	return n
}

func TestRender_CategoryLabelsTurnOrBreak(t *testing.T) {
	tests := []struct {
		name    string
		cats    []string
		noBreak bool
		overlap bool
		turned  bool
	}{
		{name: "words break at spaces", cats: []string{"aa bb", "cc dd", "ee ff", "gg hh", "ii jj", "kk ll"}},
		{name: "a word too wide turns them", cats: []string{"aaaaaaa", "bbbbbbb", "ccccccc", "ddddddd", "eeeeeee", "fffffff"}, turned: true},
		{name: "no breaks turn them", cats: []string{"aa bb", "cc dd", "ee ff", "gg hh", "ii jj", "kk ll"}, noBreak: true, turned: true},
		{name: "overlap allowed", cats: []string{"aaaaaaa", "bbbbbbb", "ccccccc", "ddddddd", "eeeeeee", "fffffff"}, noBreak: true, overlap: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := barChart(1, 2, 3, 4, 5, 6)
			c.Groups[0].Series[0].Categories = Data{Values: strs(tt.cats...), Cached: true}
			c.Axes[0].Labels = LabelNextTo
			c.Axes[0].Style = TextStyle{Size: 10, Color: compose.Black}
			c.Axes[0].NoBreak, c.Axes[0].Overlap = tt.noBreak, tt.overlap
			frags := Render(context.Background(), ahem(t), c, compose.Rect{Width: 300, Height: 200})
			if got := rotatedLabels(frags) > 0; got != tt.turned {
				t.Errorf("turned = %v, want %v", got, tt.turned)
			}
		})
	}
}

func TestRender_GradientAndPatternFills(t *testing.T) {
	grad := &shape.Paint{Kind: shape.PaintGradient, Stops: []compose.ColorStop{{Offset: 0, Color: red}, {Offset: 1, Color: blue}}}
	c := barChart(1, 2)
	c.Groups[0].Series[0].Fill = &Paint{Color: red, Shape: grad}
	c.Area = Frame{Fill: &Paint{Color: blue, Shape: &shape.Paint{Kind: shape.PaintPattern, Pattern: "pct50", Color: blue, Background: red}}}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	shaded := 0
	walk(frags, 0, func(fr compose.Fragment, _ int) {
		if p, ok := fr.(*compose.Path); ok && p.Fill != nil && p.Fill.Shading != nil {
			shaded++
		}
	})
	if shaded != 2 {
		t.Errorf("%d shaded bars, want 2", shaded)
	}
	if n := len(paths(frags, red)); n == 0 {
		t.Errorf("the pattern's background is not painted")
	}
}

func TestRender_AreaSeriesBackToFront(t *testing.T) {
	c := barChart(1, 2)
	c.Groups[0].Kind = Area
	c.Groups[0].Series = append(c.Groups[0].Series, &Series{Index: 1, Values: Data{Values: nums(2, 1), Cached: true}, Fill: &Paint{Color: blue}})
	frags := Render(context.Background(), ahem(t), c, chartRect)
	first, second, i := -1, -1, 0
	walk(frags, 0, func(fr compose.Fragment, _ int) {
		i++
		if p, ok := fr.(*compose.Path); ok && p.Fill != nil {
			switch {
			case near(p.Fill.Color, red):
				first = i
			case near(p.Fill.Color, blue):
				second = i
			}
		}
	})
	if first < 0 || second < 0 || first < second {
		t.Errorf("first series drawn at %d, second at %d: want the first in front", first, second)
	}
}

func TestRender_ManualLayouts(t *testing.T) {
	c := barChart(1, 2)
	c.PlotArea = Frame{Fill: &Paint{Color: blue}}
	c.PlotLayout = &ManualLayout{X: 0.25, Y: 0.25, Width: 0.5, Height: 0.5, Inner: true}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	ps := paths(frags, blue)
	if len(ps) != 1 {
		t.Fatalf("%d plot area paths", len(ps))
	}
	want := compose.Rect{X: 110, Y: 70, Width: 200, Height: 100}
	if b := bounds(ps[0]); math.Abs(b.X-want.X)+math.Abs(b.Y-want.Y)+math.Abs(b.Width-want.Width)+math.Abs(b.Height-want.Height) > 1e-6 {
		t.Errorf("plot area = %+v, want %+v", b, want)
	}
	c.Legend = &Legend{Position: LegendRight, Style: TextStyle{Size: 10, Color: compose.Black}, Frame: Frame{Fill: &Paint{Color: compose.Color{G: 1, A: 1}}}, Layout: &ManualLayout{X: 0.1, Y: 0.1}}
	frags = Render(context.Background(), ahem(t), c, chartRect)
	lg := paths(frags, compose.Color{G: 1, A: 1})
	if len(lg) != 1 || math.Abs(bounds(lg[0]).X-50) > 1e-6 || math.Abs(bounds(lg[0]).Y-40) > 1e-6 {
		t.Errorf("legend frames = %d, want one at (50, 40)", len(lg))
	}
}

func TestRender_LegendOrder(t *testing.T) {
	tests := []struct {
		name       string
		horizontal bool
		grouping   Grouping
		pos        LegendPosition
		want       string
	}{
		{"columns", false, Clustered, LegendRight, "AB"},
		{"stacked columns beside", false, Stacked, LegendRight, "BA"},
		{"stacked columns below", false, Stacked, LegendBottom, "AB"},
		{"bars", true, Clustered, LegendBottom, "BA"},
		{"stacked bars", true, Stacked, LegendRight, "AB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := barChart(1, 2)
			c.Groups[0].Series[0].Name = "A"
			c.Groups[0].Series = append(c.Groups[0].Series, &Series{Index: 1, Name: "B", Values: Data{Values: nums(1, 1), Cached: true}})
			c.Groups[0].Horizontal, c.Groups[0].Grouping = tt.horizontal, tt.grouping
			c.Legend = &Legend{Position: tt.pos, Style: TextStyle{Size: 10, Color: compose.Black}}
			got := ""
			for _, s := range texts(Render(context.Background(), ahem(t), c, chartRect)) {
				if s == "A" || s == "B" {
					got += s
				}
			}
			if got != tt.want {
				t.Errorf("legend order %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRender_DataTable(t *testing.T) {
	c := barChart(1.5, 2)
	c.Groups[0].Series[0].Categories = Data{Values: strs("Q1", "Q2"), Cached: true}
	c.Axes[0].Labels = LabelNextTo
	c.DataTable = &DataTable{HBorder: true, VBorder: true, Outline: true, Keys: true, Style: TextStyle{Size: 8, Color: compose.Black}, Line: &Stroke{Color: blue, Width: 1}}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	got := texts(frags)
	count := map[string]int{}
	for _, s := range got {
		count[s]++
	}
	for _, want := range []string{"Q1", "Q2", "S", "1.5", "2"} {
		if count[want] != 1 {
			t.Errorf("text %q drawn %d times in %q, want once", want, count[want], got)
		}
	}
	lines := 0
	walk(frags, 0, func(fr compose.Fragment, _ int) {
		if p, ok := fr.(*compose.Path); ok && p.Stroke != nil && near(p.Stroke.Color, blue) {
			lines++
		}
	})
	if lines == 0 {
		t.Errorf("the table draws no border")
	}
}

func TestRender_CustomLabelFields(t *testing.T) {
	c := barChart(1.5, 2)
	c.Groups[0].Series[0].Categories = Data{Values: strs("Q1", "Q2"), Cached: true}
	c.Groups[0].Series[0].Points = []Point{{Index: 0, HasLabels: true, Labels: Labels{Style: TextStyle{Size: 8, Color: compose.Black}, Custom: []LabelField{
		{Text: "v="}, {Kind: FieldValue}, {Kind: FieldBreak}, {Kind: FieldCategory}, {Text: "/"}, {Kind: FieldSeries},
	}}}}
	got := strings.Join(texts(Render(context.Background(), ahem(t), c, chartRect)), " ")
	for _, want := range []string{"v=1.5", "Q1/S"} {
		if !strings.Contains(got, want) {
			t.Errorf("texts %q lack %q", got, want)
		}
	}
}

func TestDims_CategoryAxisCrossesPrimary(t *testing.T) {
	c := barChart(1, 2)
	c.Groups[0].Kind = Line
	sec := &Axis{Kind: ValueAxis, Position: AxisRight, Min: math.NaN(), Max: math.NaN(), MajorUnit: math.NaN(), MinorUnit: math.NaN()}
	c.Axes = append(c.Axes, sec)
	c.Groups = append([]*Group{{Kind: Line, XAxis: 0, YAxis: 2, Series: []*Series{{Values: Data{Values: nums(-5, 5), Cached: true}}}}}, c.Groups...)
	rd := &renderer{ctx: context.Background(), fonts: ahem(t), c: c, page: chartRect}
	x, ys, _ := rd.dims(c.Groups[0])
	if x == nil || len(ys) != 2 || x.other.ax != c.Axes[1] {
		t.Errorf("the category axis crosses %+v, want the primary value axis", x.other.ax)
	}
}

func TestRender_DeletedPointLabelHidesGroupLabel(t *testing.T) {
	c := barChart(1, 2)
	c.Groups[0].Labels = Labels{ShowValue: true, Style: TextStyle{Size: 10}}
	c.Groups[0].Series[0].Points = []Point{{Index: 0, HasLabels: true}}
	got := texts(Render(context.Background(), ahem(t), c, chartRect))
	if slices.Contains(got, "1") || !slices.Contains(got, "2") {
		t.Errorf("texts %q, want the second point's label alone", got)
	}
}

package chart

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func TestHistogram_Bins(t *testing.T) {
	tests := []struct {
		name   string
		vals   []float64
		b      Binning
		labels []string
		counts []float64
	}{
		// Scott's rule: a standard deviation of 1.92 over five values
		// makes bins 3.94 wide.
		{"scott", []float64{12, 10, 11, 13, 15}, Binning{}, []string{"[10-13.94]", "(13.94-17.87]"}, []float64{4, 1}},
		{"width", []float64{1, 2, 3, 4, 5}, Binning{Width: 2}, []string{"[1-3]", "(3-5]"}, []float64{3, 2}},
		{"count", []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, Binning{Count: 3}, []string{"[0-3]", "(3-6]", "(6-9]"}, []float64{4, 3, 3}},
		{"one value", []float64{2.5}, Binning{}, []string{"[2-4]"}, []float64{1}},
		{"equal values", []float64{3, 3, 3}, Binning{Width: 1}, []string{"[3-4]"}, []float64{3}},
		{"small width", []float64{0.001, 0.002, 0.003}, Binning{Count: 2}, []string{"[0.001-0.002]", "(0.002-0.003]"}, []float64{2, 1}},
		{"none", nil, Binning{}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels, counts := histogram(tt.vals, tt.b)
			if !slices.Equal(labels, tt.labels) || !slices.Equal(counts, tt.counts) {
				t.Errorf("histogram = %q %v, want %q %v", labels, counts, tt.labels, tt.counts)
			}
		})
	}
}

func TestHistogram_CapsBins(t *testing.T) {
	labels, counts := histogram([]float64{0, 1e9}, Binning{Width: 1e-3})
	if len(labels) != maxBins || len(counts) != maxBins {
		t.Fatalf("got %d bins, want the cap %d", len(labels), maxBins)
	}
	if counts[0] != 1 || counts[len(counts)-1] != 1 {
		t.Errorf("the extremes fall in bins %v and %v, want the first and the last", counts[0], counts[len(counts)-1])
	}
}

func TestRender_HistogramBars(t *testing.T) {
	c := barChart()
	c.Groups[0].GapWidth = 0
	c.Groups[0].Series[0].Values = Data{Ref: "Sheet1!$A$1:$A$5"}
	c.Groups[0].Series[0].Bins = &Binning{}
	c.Resolver = func(string) []Value { return nums(12, 10, 11, 13, 15) }
	frags := Render(context.Background(), ahem(t), c, chartRect)
	bars := paths(frags, red)
	if len(bars) != 2 {
		t.Fatalf("got %d bars, want 2", len(bars))
	}
	a, b := bounds(bars[0]), bounds(bars[1])
	if r := a.Height / b.Height; r < 3.99 || r > 4.01 {
		t.Errorf("bar heights %v and %v, want 4 to 1", a.Height, b.Height)
	}
	if d := b.X - (a.X + a.Width); d < -1e-6 || d > 1e-6 {
		t.Errorf("bars %v apart, want touching at gap 0", d)
	}
	if c.Groups[0].Series[0].Bins == nil || c.Groups[0].Series[0].Values.Values != nil {
		t.Errorf("Render changed the chart's series")
	}
}

func TestRender_UnsupportedKind(t *testing.T) {
	nan := func() []*Axis { return plainAxes() }
	white := compose.Color{R: 1, G: 1, B: 1, A: 1}
	c := &Chart{
		Axes: nan(),
		Groups: []*Group{{Kind: Unsupported, XAxis: 0, YAxis: 1, Series: []*Series{
			{Name: "First", Values: Data{Values: nums(100, 200), Cached: true}, Fill: &Paint{Color: red}},
			{Index: 1, Name: "Last", Values: Data{Values: nums(1, 2), Cached: true}, Fill: &Paint{Color: blue}},
		}}},
		Legend: &Legend{Position: LegendTop, Style: TextStyle{Size: 10}},
		Notice: &Title{Text: "Not drawn here", Style: TextStyle{Size: 10}, Frame: Frame{Fill: &Paint{Color: white}}},
	}
	c.Axes[1].Labels = LabelNextTo
	frags := Render(context.Background(), ahem(t), c, chartRect)
	got := strings.Join(texts(frags), "|")
	for _, want := range []string{"Not drawn here", "First", "Last", "2.5"} {
		if !strings.Contains(got, want) {
			t.Errorf("texts %q lack %q", got, want)
		}
	}
	// The value axis scales to the last series alone: up to 2.5, not 250.
	if strings.Contains(got, "250") {
		t.Errorf("texts %q scale to the first series", got)
	}
	// The keys are circles, the only red and blue paints: no series.
	for _, col := range []compose.Color{red, blue} {
		ps := paths(frags, col)
		if len(ps) != 1 || len(ps[0].Ops) != 6 || ps[0].Ops[1].Kind != compose.CurveTo {
			t.Errorf("colour %v paints %d paths, want one circle", col, len(ps))
		}
	}
	// The notice's box is centred on the chart.
	boxes := paths(frags, white)
	if len(boxes) == 0 {
		t.Fatal("no notice box")
	}
	b := bounds(boxes[len(boxes)-1])
	cx, cy := b.X+b.Width/2, b.Y+b.Height/2
	if d := (cx-210)*(cx-210) + (cy-120)*(cy-120); d > 1e-6 || !(b.Width < chartRect.Width*2/3/noticeNarrowing+1e-6) {
		t.Errorf("notice box %+v, want it centred on (210, 120)", b)
	}
}

func TestRender_NoticeBreaksAtTwoThirds(t *testing.T) {
	c := barChart(1)
	c.Notice = &Title{Text: strings.Repeat("word ", 40), Style: TextStyle{Size: 10}}
	frags := Render(context.Background(), ahem(t), c, chartRect)
	var lines []string
	for _, s := range texts(frags) {
		if strings.Contains(s, "word") {
			lines = append(lines, s)
		}
	}
	// Ahem's em-square glyphs: 27 ten-point characters fit 2/3 of 400
	// points widened by the narrowing, so five words a line.
	if len(lines) != 8 || !reflect.DeepEqual(strings.Fields(lines[0]), strings.Fields(strings.Repeat("word ", 5))) {
		t.Errorf("notice lines %q, want 8 of five words", lines)
	}
}

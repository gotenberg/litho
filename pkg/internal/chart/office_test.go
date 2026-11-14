package chart

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestOfficeIntervals_ProbeCharts holds Office's automatic scale to the
// charts Excel and PowerPoint printed: a column chart of one series, its
// data range, the axis length between its lowest and highest label and
// the size of its Calibri labels in, the minimum, maximum and major unit
// the application printed out.
//
// # Corpus evidence
//
//   - probe, xlsx: 80 column charts of three literal values, frames 5 to
//     30 rows high, labels of 8, 10, 14 and 18 pt; Excel's labels.
//   - probe, pptx: 12 column charts in frames 70 to 200 pt high, labels
//     of 10 pt; PowerPoint's labels.
func TestOfficeIntervals_ProbeCharts(t *testing.T) {
	// calibriLine is Calibri's ascent, descent and line gap, in ems.
	const calibriLine = 1.2207
	tests := []struct {
		name            string
		lo, hi          float64
		length, size    float64
		min, max, major float64
	}{
		{"xlsx probe1 K01", 0.2333, 0.7, 145.96, 10, 0, 0.8, 0.1},
		{"xlsx probe1 K02", 0.3333, 1, 145.95, 10, 0, 1.2, 0.2},
		{"xlsx probe1 K03", 1.5, 4.5, 145.94, 10, 0, 5, 0.5},
		{"xlsx probe1 K04", 2.3333, 7, 145.96, 10, 0, 8, 1},
		{"xlsx probe1 K05", 3.1667, 9.5, 145.95, 10, 0, 10, 1},
		{"xlsx probe1 K06", 3.3333, 10, 145.94, 10, 0, 12, 2},
		{"xlsx probe1 K07", 3.6667, 11, 145.96, 10, 0, 12, 2},
		{"xlsx probe1 K08", 4.6667, 14, 145.95, 10, 0, 16, 2},
		{"xlsx probe1 K09", 6.3333, 19, 145.94, 10, 0, 20, 2},
		{"xlsx probe1 K10", 7.6667, 23, 145.96, 10, 0, 25, 5},
		{"xlsx probe1 K11", 15.6667, 47, 145.95, 10, 0, 50, 5},
		{"xlsx probe1 K12", 24.3333, 73, 145.94, 10, 0, 80, 10},
		{"xlsx probe1 K13", 31.6667, 95, 145.96, 10, 0, 100, 10},
		{"xlsx probe1 K14", 33, 99, 145.95, 10, 0, 120, 20},
		{"xlsx probe1 K15", 33.3333, 100, 145.94, 10, 0, 120, 20},
		{"xlsx probe1 K16", 33.6667, 101, 145.96, 10, 0, 120, 20},
		{"xlsx probe1 K17", 40, 120, 145.95, 10, 0, 140, 20},
		{"xlsx probe1 K18", 56.6667, 170, 145.94, 10, 0, 180, 20},
		{"xlsx probe1 K19", 316.667, 950, 145.96, 10, 0, 1000, 100},
		{"xlsx probe1 K20", 95, 99, 145.95, 10, 93, 100, 1},
		{"xlsx probe1 K21", 90, 100, 145.94, 10, 84, 102, 2},
		{"xlsx probe1 K22", 80, 100, 145.96, 10, 0, 120, 20},
		{"xlsx probe1 K23", 84, 100, 145.95, 10, 75, 105, 5},
		{"xlsx probe1 K24", 980, 1000, 145.94, 10, 970, 1005, 5},
		{"xlsx probe1 K25", 47, 49, 145.96, 10, 46, 49.5, 0.5},
		{"xlsx probe1 K26", -5, 10, 145.95, 10, -6, 12, 2},
		{"xlsx probe1 K27", -47, -3, 145.94, 10, -50, 0, 5},
		{"xlsx probe1 K28", -100, 100, 145.96, 10, -150, 150, 50},
		{"xlsx probe1 K29", 15.6667, 47, 58.80, 10, 0, 60, 20},
		{"xlsx probe1 K30", 15.6667, 47, 262.14, 10, 0, 50, 5},
		{"xlsx probe1 K31", 15.6667, 47, 378.33, 10, 0, 50, 5},
		{"xlsx probe1 K32", 56.6667, 170, 58.80, 10, 0, 200, 100},
		{"xlsx probe1 K33", 56.6667, 170, 262.14, 10, 0, 180, 20},
		{"xlsx probe1 K34", 56.6667, 170, 378.33, 10, 0, 180, 20},
		{"xlsx probe2 K01", 2.3333, 7, 15.28, 10, 0, 10, 10},
		{"xlsx probe2 K02", 2.3333, 7, 29.76, 10, 0, 10, 10},
		{"xlsx probe2 K03", 2.3333, 7, 44.32, 10, 0, 10, 5},
		{"xlsx probe2 K04", 2.3333, 7, 58.83, 10, 0, 10, 5},
		{"xlsx probe2 K05", 2.3333, 7, 73.32, 10, 0, 8, 2},
		{"xlsx probe2 K06", 2.3333, 7, 87.88, 10, 0, 8, 2},
		{"xlsx probe2 K07", 2.3333, 7, 102.38, 10, 0, 8, 2},
		{"xlsx probe2 K08", 2.3333, 7, 116.91, 10, 0, 8, 1},
		{"xlsx probe2 K09", 7.6667, 23, 15.22, 10, 0, 50, 50},
		{"xlsx probe2 K10", 7.6667, 23, 29.80, 10, 0, 50, 50},
		{"xlsx probe2 K11", 7.6667, 23, 44.28, 10, 0, 40, 20},
		{"xlsx probe2 K12", 7.6667, 23, 58.80, 10, 0, 30, 10},
		{"xlsx probe2 K13", 7.6667, 23, 73.35, 10, 0, 25, 5},
		{"xlsx probe2 K14", 7.6667, 23, 87.86, 10, 0, 25, 5},
		{"xlsx probe2 K15", 7.6667, 23, 102.40, 10, 0, 25, 5},
		{"xlsx probe2 K16", 7.6667, 23, 116.90, 10, 0, 25, 5},
		{"xlsx probe2 K17", 15.6667, 47, 15.29, 10, 0, 50, 50},
		{"xlsx probe2 K18", 15.6667, 47, 29.76, 10, 0, 50, 50},
		{"xlsx probe2 K19", 15.6667, 47, 44.28, 10, 0, 50, 50},
		{"xlsx probe2 K20", 15.6667, 47, 58.80, 10, 0, 60, 20},
		{"xlsx probe2 K21", 15.6667, 47, 73.34, 10, 0, 50, 10},
		{"xlsx probe2 K22", 15.6667, 47, 87.87, 10, 0, 50, 10},
		{"xlsx probe2 K23", 15.6667, 47, 102.38, 10, 0, 50, 10},
		{"xlsx probe2 K24", 15.6667, 47, 116.92, 10, 0, 50, 10},
		{"xlsx probe2 K25", 31.6667, 95, 15.28, 10, 0, 100, 100},
		{"xlsx probe2 K26", 31.6667, 95, 29.76, 10, 0, 100, 100},
		{"xlsx probe2 K27", 31.6667, 95, 44.28, 10, 0, 100, 50},
		{"xlsx probe2 K28", 31.6667, 95, 58.82, 10, 0, 100, 50},
		{"xlsx probe2 K29", 31.6667, 95, 73.36, 10, 0, 100, 20},
		{"xlsx probe2 K30", 31.6667, 95, 87.86, 10, 0, 100, 20},
		{"xlsx probe2 K31", 31.6667, 95, 102.39, 10, 0, 100, 20},
		{"xlsx probe2 K32", 31.6667, 95, 116.90, 10, 0, 100, 20},
		{"xlsx probe2 K33", 56.6667, 170, 15.24, 10, 0, 200, 200},
		{"xlsx probe2 K34", 56.6667, 170, 29.76, 10, 0, 200, 200},
		{"xlsx probe2 K35", 56.6667, 170, 44.28, 10, 0, 200, 100},
		{"xlsx probe2 K36", 56.6667, 170, 58.83, 10, 0, 200, 100},
		{"xlsx probe2 K37", 56.6667, 170, 73.32, 10, 0, 200, 50},
		{"xlsx probe2 K38", 56.6667, 170, 87.88, 10, 0, 200, 50},
		{"xlsx probe2 K39", 56.6667, 170, 102.38, 10, 0, 200, 50},
		{"xlsx probe2 K40", 56.6667, 170, 116.91, 10, 0, 200, 50},
		{"xlsx probe2 K41", 15.6667, 47, 87.94, 8, 0, 50, 10},
		{"xlsx probe2 K42", 15.6667, 47, 85.42, 14, 0, 60, 20},
		{"xlsx probe2 K43", 15.6667, 47, 82.97, 18, 0, 50, 50},
		{"xlsx probe2 K44", 56.6667, 170, 87.96, 8, 0, 200, 50},
		{"xlsx probe2 K45", 56.6667, 170, 85.42, 14, 0, 200, 100},
		{"xlsx probe2 K46", 56.6667, 170, 82.97, 18, 0, 200, 100},
		{"pptx K01", 15.6667, 47, 12.70, 10, 0, 50, 50},
		{"pptx K02", 15.6667, 47, 32.70, 10, 0, 50, 50},
		{"pptx K03", 15.6667, 47, 52.70, 10, 0, 60, 20},
		{"pptx K04", 15.6667, 47, 72.80, 10, 0, 50, 10},
		{"pptx K05", 15.6667, 47, 102.80, 10, 0, 50, 10},
		{"pptx K06", 15.6667, 47, 142.90, 10, 0, 50, 5},
		{"pptx K07", 56.6667, 170, 12.70, 10, 0, 200, 200},
		{"pptx K08", 56.6667, 170, 32.70, 10, 0, 200, 200},
		{"pptx K09", 56.6667, 170, 52.70, 10, 0, 200, 100},
		{"pptx K10", 56.6667, 170, 72.80, 10, 0, 200, 50},
		{"pptx K11", 56.6667, 170, 102.80, 10, 0, 200, 50},
		{"pptx K12", 56.6667, 170, 142.90, 10, 0, 180, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := officeIntervals(tt.length, calibriLine*tt.size)
			s := autoScale(nil, tt.lo, tt.hi, scaleOptions{zero: true, border: true, maxCount: n})
			if math.Abs(s.min-tt.min) > 1e-9 || math.Abs(s.max-tt.max) > 1e-9 || math.Abs(s.major-tt.major) > 1e-9 {
				t.Errorf("scale = [%v, %v] by %v, want [%v, %v] by %v", s.min, s.max, s.major, tt.min, tt.max, tt.major)
			}
		})
	}
}

func TestOfficeIntervals_Bounds(t *testing.T) {
	tests := []struct {
		name          string
		length, lineH float64
		want          int
	}{
		{"too short for two labels keeps one interval", 5, 12.2, 1},
		{"long axis stops at the automatic cap", 1000, 12.2, maxAutoTicks},
		{"no line height leaves the cap", 100, 0, maxAutoTicks},
		{"NaN line height leaves the cap", 100, math.NaN(), maxAutoTicks},
		{"a 10 pt Calibri line takes 16 pixels", 72.8, 12.207, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := officeIntervals(tt.length, tt.lineH); got != tt.want {
				t.Errorf("officeIntervals(%v, %v) = %d, want %d", tt.length, tt.lineH, got, tt.want)
			}
		})
	}
}

// TestRender_OfficeLayout holds a titled column chart under OfficeRules to
// Office's distances: Ahem's line is one em, so a 10 pt title and labels
// take 10 pt lines and a label 9.75 pt (13 pixels) of the count.
func TestRender_OfficeLayout(t *testing.T) {
	c := barChart(2.3333, 3.5, 7)
	c.Rules = OfficeRules
	c.Axes[0].Labels, c.Axes[1].Labels = LabelNextTo, LabelNextTo
	c.Title = &Title{Text: "T", Style: TextStyle{Size: 10, Color: typeset.Black}}
	c.PlotArea = Frame{Fill: &Paint{Color: blue}}
	r := typeset.Rect{Width: 361.2, Height: 145.2}
	frags := Render(context.Background(), ahem(t), c, r)
	ps := paths(frags, blue)
	if len(ps) != 1 {
		t.Fatalf("%d plot area paths", len(ps))
	}
	// Top: the title's line, officeTitleTop above and officeTitleGap under
	// it, then half of officeMinLabelRoom. Bottom: the category labels'
	// line, officePad under and 0.634 em over it. Left: officePad, a
	// one-digit label, 0.928 em. Right: officePad and officeRightRoom.
	want := typeset.Rect{X: 6.5 + 10 + 9.28, Y: 7.5 + 10 + 6.5 + 6}
	want.Width = 361.2 - 11 - want.X
	want.Height = 145.2 - (6.5 + 10 + 6.34) - want.Y
	if b := bounds(ps[0]); math.Abs(b.X-want.X)+math.Abs(b.Y-want.Y)+math.Abs(b.Width-want.Width)+math.Abs(b.Height-want.Height) > 1e-6 {
		t.Errorf("plot area = %+v, want %+v", b, want)
	}
	// 92.36 pt hold nine labels of 9.75 pt: eight intervals of 1.
	got := strings.Join(texts(frags), " ")
	if !strings.Contains(got, "8 7 6 5 4 3 2 1 0") && !strings.Contains(got, "0 1 2 3 4 5 6 7 8") {
		t.Errorf("labels = %q, want 0 to 8 by 1", got)
	}
}

// TestOfficeHorizontalIntervals_ProbeCharts holds Office's automatic scale
// across the chart to the charts Excel printed: a bar chart's value axis
// or a scatter chart's horizontal one, its data range, the axis length
// and the size of its Calibri labels in, the minimum, maximum and major
// unit Excel printed out. A chart cut by the page edge has its length
// from the distance between its labels.
//
// # Corpus evidence
//
//   - probe, xlsx: 34 bar charts of three literal values, frames 3 to 14
//     columns wide, labels of 8, 10 and 14 pt; 9 scatter charts of four
//     points, frames 4 to 10 columns wide, labels of 10 pt; Excel's
//     labels.
func TestOfficeHorizontalIntervals_ProbeCharts(t *testing.T) {
	const calibriLine = 1.2207
	tests := []struct {
		name            string
		lo, hi          float64
		zero            bool
		length, size    float64
		min, max, major float64
	}{
		{"bar K01", 2.3333, 7, true, 117.72, 10, 0, 10, 5},
		{"bar K02", 2.3333, 7, true, 171.87, 10, 0, 8, 2},
		{"bar K03", 2.3333, 7, true, 223.49, 10, 0, 8, 2},
		{"bar K04", 2.3333, 7, true, 326.72, 10, 0, 8, 1},
		{"bar K05", 2.3333, 7, true, 481.5, 10, 0, 8, 1},
		{"bar K06", 2.3333, 7, true, 688.0, 10, 0, 8, 1},
		{"bar K07", 15.6667, 47, true, 117.72, 10, 0, 60, 20},
		{"bar K08", 15.6667, 47, true, 169.32, 10, 0, 60, 20},
		{"bar K09", 15.6667, 47, true, 220.93, 10, 0, 50, 10},
		{"bar K10", 15.6667, 47, true, 324.15, 10, 0, 50, 10},
		{"bar K11", 15.6667, 47, true, 479.0, 10, 0, 50, 5},
		{"bar K12", 15.6667, 47, true, 685.4, 10, 0, 50, 5},
		{"bar K13", 56.6667, 170, true, 115.21, 10, 0, 200, 100},
		{"bar K14", 56.6667, 170, true, 166.83, 10, 0, 200, 50},
		{"bar K15", 56.6667, 170, true, 218.43, 10, 0, 200, 50},
		{"bar K16", 56.6667, 170, true, 321.65, 10, 0, 200, 50},
		{"bar K17", 56.6667, 170, true, 476.5, 10, 0, 180, 20},
		{"bar K18", 56.6667, 170, true, 682.9, 10, 0, 180, 20},
		{"bar K19", 316.667, 950, true, 112.67, 10, 0, 1000, 500},
		{"bar K20", 316.667, 950, true, 164.29, 10, 0, 1000, 500},
		{"bar K21", 316.667, 950, true, 215.90, 10, 0, 1000, 200},
		{"bar K22", 316.667, 950, true, 319.12, 10, 0, 1000, 200},
		{"bar K23", 316.667, 950, true, 474.0, 10, 0, 1000, 100},
		{"bar K24", 316.667, 950, true, 680.4, 10, 0, 1000, 100},
		{"bar K25", 4115, 12345, true, 110.13, 10, 0, 15000, 5000},
		{"bar K26", 4115, 12345, true, 161.73, 10, 0, 15000, 5000},
		{"bar K27", 4115, 12345, true, 213.35, 10, 0, 15000, 5000},
		{"bar K28", 4115, 12345, true, 316.55, 10, 0, 14000, 2000},
		{"bar K29", 4115, 12345, true, 471.4, 10, 0, 14000, 2000},
		{"bar K30", 4115, 12345, true, 677.9, 10, 0, 14000, 2000},
		{"bar K31", 15.6667, 47, true, 325.20, 8, 0, 50, 5},
		{"bar K32", 15.6667, 47, true, 322.14, 14, 0, 50, 10},
		{"bar K33", 56.6667, 170, true, 323.18, 8, 0, 180, 20},
		{"bar K34", 56.6667, 170, true, 318.57, 14, 0, 200, 50},
		{"scatter K01", 1.0556, 9.5, false, 164.42, 10, 0, 10, 5},
		{"scatter K02", 1.0556, 9.5, false, 319.25, 10, 0, 10, 2},
		{"scatter K03", 1.0556, 9.5, false, 474.0, 10, 0, 10, 1},
		{"scatter K06", 5.2222, 47, false, 164.42, 10, 0, 60, 20},
		{"scatter K07", 5.2222, 47, false, 319.25, 10, 0, 50, 10},
		{"scatter K08", 5.2222, 47, false, 474.0, 10, 0, 50, 5},
		{"scatter K11", 18.8889, 170, false, 156.87, 10, 0, 200, 50},
		{"scatter K12", 18.8889, 170, false, 311.69, 10, 0, 200, 50},
		{"scatter K13", 18.8889, 170, false, 466.4, 10, 0, 180, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := officeHorizontalIntervals(tt.length, calibriLine*tt.size)
			s := autoScale(nil, tt.lo, tt.hi, scaleOptions{zero: tt.zero, border: true, maxCount: n})
			if math.Abs(s.min-tt.min) > 1e-9 || math.Abs(s.max-tt.max) > 1e-9 || math.Abs(s.major-tt.major) > 1e-9 {
				t.Errorf("scale = [%v, %v] by %v, want [%v, %v] by %v", s.min, s.max, s.major, tt.min, tt.max, tt.major)
			}
		})
	}
}

func TestOfficeHorizontalIntervals_Bounds(t *testing.T) {
	tests := []struct {
		name          string
		length, lineH float64
		want          int
	}{
		{"short axis keeps one interval", 20, 12.2, 1},
		{"long axis stops at the automatic cap", 1000, 12.2, maxAutoTicks},
		{"no line height leaves the cap", 100, 0, maxAutoTicks},
		{"NaN line height leaves the cap", 100, math.NaN(), maxAutoTicks},
		{"three 10 pt Calibri lines take 36 points", 108, 12.207, 3},
		{"just short of three intervals", 107.99, 12.207, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := officeHorizontalIntervals(tt.length, tt.lineH); got != tt.want {
				t.Errorf("officeHorizontalIntervals(%v, %v) = %d, want %d", tt.length, tt.lineH, got, tt.want)
			}
		})
	}
}

// officeChart returns barChart(values...) under OfficeRules with labelled
// axes, its plot area filled blue.
func officeChart(values ...float64) *Chart {
	c := barChart(values...)
	c.Rules = OfficeRules
	c.Axes[0].Labels, c.Axes[1].Labels = LabelNextTo, LabelNextTo
	c.PlotArea = Frame{Fill: &Paint{Color: blue}}
	return c
}

// plotBounds renders c into r and returns its plot area.
func plotBounds(t *testing.T, c *Chart, r typeset.Rect) (typeset.Rect, []typeset.Fragment) {
	t.Helper()
	frags := Render(context.Background(), ahem(t), c, r)
	ps := paths(frags, blue)
	if len(ps) != 1 {
		t.Fatalf("%d plot area paths", len(ps))
	}
	return bounds(ps[0]), frags
}

// TestRender_OfficeScaleOnEveryValueAxis holds the axes LibreOffice's
// rules count otherwise to Office's count: a percent-stacked axis, a
// horizontal bar chart's value axis and a secondary axis.
func TestRender_OfficeScaleOnEveryValueAxis(t *testing.T) {
	t.Run("percent-stacked", func(t *testing.T) {
		// 20.16 pt hold two labels of 9.75 pt: one interval, where
		// LibreOffice's count draws two.
		c := officeChart(1, 2)
		c.Groups[0].Grouping = PercentStacked
		frags := Render(context.Background(), ahem(t), c, typeset.Rect{Width: 361.2, Height: 54})
		got := strings.Join(texts(frags), " ")
		if !strings.Contains(got, "100%") || strings.Contains(got, "50%") {
			t.Errorf("labels = %q, want 0%% and 100%% alone", got)
		}
	})
	t.Run("horizontal bar", func(t *testing.T) {
		// About 108 pt across hold three intervals of three 9.75 pt
		// lines: 0 to 10 by 5, where LibreOffice's count steps by 1.
		c := officeChart(2.3333, 3.5, 7)
		c.Groups[0].Horizontal = true
		frags := Render(context.Background(), ahem(t), c, typeset.Rect{Width: 150, Height: 145.2})
		got := strings.Join(texts(frags), " ")
		if !strings.Contains(got, "10") || strings.Contains(got, "4") {
			t.Errorf("labels = %q, want 0 to 10 by 5", got)
		}
	})
	t.Run("secondary", func(t *testing.T) {
		// The secondary axis counts as the primary one: about 41 pt hold
		// four labels, three intervals: 0 to 200 by 100.
		c := officeChart(15.6667, 23.5, 47)
		nan := math.NaN()
		c.Axes = append(c.Axes,
			&Axis{Kind: CategoryAxis, Deleted: true, Labels: LabelNextTo, Min: nan, Max: nan, MajorUnit: nan, MinorUnit: nan},
			&Axis{Kind: ValueAxis, Labels: LabelNextTo, Position: AxisRight, Crosses: CrossMax, Min: nan, Max: nan, MajorUnit: nan, MinorUnit: nan})
		c.Groups = append(c.Groups, &Group{Kind: Line, XAxis: 2, YAxis: 3, Series: []*Series{
			{Name: "T", Values: Data{Values: nums(56.6667, 85, 170), Cached: true}, Border: &Stroke{Width: 1, Color: red}},
		}})
		frags := Render(context.Background(), ahem(t), c, typeset.Rect{Width: 361.2, Height: 75})
		got := strings.Join(texts(frags), " ")
		if !strings.Contains(got, "200") || strings.Contains(got, "150") || strings.Contains(got, "50 ") {
			t.Errorf("labels = %q, want the secondary axis 0 to 200 by 100", got)
		}
	})
}

// TestRender_OfficeLayoutUntitled holds a chart without a title under
// OfficeRules to Office's distances: the room of the axis labels starts
// officeTopPad under the chart's top.
func TestRender_OfficeLayoutUntitled(t *testing.T) {
	b, _ := plotBounds(t, officeChart(2.3333, 3.5, 7), typeset.Rect{Width: 361.2, Height: 145.2})
	if want := officeTopPad + officeMinLabelRoom/2; math.Abs(b.Y-want) > 1e-6 {
		t.Errorf("plot area top = %v, want %v", b.Y, want)
	}
}

// TestRender_OfficeValueAxisTitle holds the title of a value axis beside
// the plot area to Office's room: its line and three officeAxisTitlePad.
func TestRender_OfficeValueAxisTitle(t *testing.T) {
	r := typeset.Rect{Width: 361.2, Height: 145.2}
	base, _ := plotBounds(t, officeChart(2.3333, 3.5, 7), r)
	c := officeChart(2.3333, 3.5, 7)
	c.Axes[1].Title = &Title{Text: "UU", Style: TextStyle{Size: 10, Color: typeset.Black}}
	b, _ := plotBounds(t, c, r)
	if got, want := b.X-base.X, 10+3*officeAxisTitlePad; math.Abs(got-want) > 1e-6 {
		t.Errorf("title room = %v, want %v", got, want)
	}
}

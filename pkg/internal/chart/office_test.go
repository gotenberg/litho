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

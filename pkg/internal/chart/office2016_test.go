package chart

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestRender_FlushLabel locks the outside-end label of an Office 2016
// chart against its column's end: its line's bottom is the column's top,
// where another chart keeps labelSpacing between them.
func TestRender_FlushLabel(t *testing.T) {
	for _, flush := range []bool{false, true} {
		c := barChart(10)
		c.Groups[0].Series[0].Labels = Labels{ShowValue: true, Placement: LabelOutsideEnd, Flush: flush, Style: TextStyle{Size: 10}}
		frags := Render(context.Background(), ahem(t), c, chartRect)
		bar := bounds(paths(frags, red)[0])
		var base float64
		walk(frags, 0, func(fr typeset.Fragment, _ int) {
			if tx, ok := fr.(*typeset.Text); ok {
				base = tx.Origin.Y
			}
		})
		// Ahem's em is centred on the line, 0.2 of it under the baseline.
		want := bar.Y - (lineHeight(10)-10)/2 - 2
		if !flush {
			want -= labelSpacing
		}
		if math.Abs(base-want) > 0.01 {
			t.Errorf("flush %v: label baseline at %v, want %v over the bar's top %v", flush, base, want, bar.Y)
		}
	}
}

// TestRender_Office2016RightRoom locks the plot area of an Office 2016
// chart at officePad from the chart's right edge, where an Office chart
// keeps officeRightRoom more.
func TestRender_Office2016RightRoom(t *testing.T) {
	grid := typeset.Color{R: 0.25, G: 0.75, B: 0.25, A: 1}
	for _, ex := range []bool{false, true} {
		c := barChart(1, 2)
		c.Rules, c.Office2016 = OfficeRules, ex
		c.Axes[1].MajorGrid = &Stroke{Color: grid, Width: 0.5}
		right := math.Inf(-1)
		walk(Render(context.Background(), ahem(t), c, chartRect), 0, func(fr typeset.Fragment, _ int) {
			if p, ok := fr.(*typeset.Path); ok && p.Stroke != nil && near(p.Stroke.Color, grid) {
				b := bounds(p)
				right = math.Max(right, b.X+b.Width)
			}
		})
		want := chartRect.X + chartRect.Width - officePad
		if !ex {
			want -= officeRightRoom
		}
		if math.Abs(right-want) > 1e-6 {
			t.Errorf("Office2016 %v: plot area ends at %v, want %v", ex, right, want)
		}
	}
}

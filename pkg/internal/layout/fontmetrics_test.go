package layout

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestPixelMetrics_BlinkRounding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, d, g    float64
		wa, wd, wg float64
	}{
		{px(10.4), px(2.6), px(0), px(10), px(3), px(0)},
		{px(10.5), px(2.5), px(1.5), px(11), px(3), px(2)},
		{px(29.1), px(7.4), px(0.4), px(29), px(7), px(0)},
		{px(1.6), px(0.4), px(-1.5), px(2), px(0), px(-2)},
	}
	for _, tt := range tests {
		a, d, g := pixelMetrics(tt.a, tt.d, tt.g)
		if !near(a, tt.wa) || !near(d, tt.wd) || !near(g, tt.wg) {
			t.Errorf("pixelMetrics(%v, %v, %v) = %v, %v, %v; want %v, %v, %v", tt.a, tt.d, tt.g, a, d, g, tt.wa, tt.wd, tt.wg)
		}
	}
}

func TestPixelFontMetrics_MovesTheBaseline(t *testing.T) {
	t.Parallel()
	// Ahem at 13px: ascent 10.4px, descent 2.6px; Blink lays the line out
	// at 10 and 3.
	normal := func(s *css.Style) {
		s.FontSize = px(13)
		s.LineHeight = css.Dimension{Kind: css.Normal}
	}
	for _, tt := range []struct {
		pixel    bool
		baseline float64
	}{{false, px(10.4)}, {true, px(10)}} {
		st := sty(normal)
		res, err := Layout(context.Background(), doc(el(st, txt(st, "a")), el(sty(height(1), bg(red)))), ahem(t), Options{Page: page(40, 40), PixelFontMetrics: tt.pixel})
		if err != nil {
			t.Fatal(err)
		}
		ts := texts(res, 1)
		if len(ts) != 1 || !near(ts[0].y, tt.baseline) {
			t.Errorf("pixel %v: texts %+v, want the baseline at %v", tt.pixel, ts, tt.baseline)
		}
	}
}

// TestPixelFontMetrics_FloorsTheLeadingAbove locks that pixel font
// metrics split a leading as Blink does, the half above floored to a
// whole pixel and the rest below, while fractional metrics split it in
// halves (CSS 2.1, 10.8.1).
func TestPixelFontMetrics_FloorsTheLeadingAbove(t *testing.T) {
	t.Parallel()
	// Ahem at 10px, ascent 8px and descent 2px, on lines of 13px: a
	// leading of 3px.
	tall := func(s *css.Style) { s.LineHeight = css.Pts(px(13)) }
	for _, tt := range []struct {
		pixel    bool
		baseline float64
	}{{false, px(9.5)}, {true, px(9)}} {
		st := sty(tall)
		res, err := Layout(context.Background(), doc(el(st, txt(st, "a"))), ahem(t), Options{Page: page(40, 40), PixelFontMetrics: tt.pixel})
		if err != nil {
			t.Fatal(err)
		}
		ts := texts(res, 1)
		if len(ts) != 1 || !near(ts[0].y, tt.baseline) {
			t.Errorf("pixel %v: texts %+v, want the baseline at %v", tt.pixel, ts, tt.baseline)
		}
	}
}

// TestPixelFontMetrics_LineHeightOnLayoutUnits locks that pixel font
// metrics lay a line-height out on Blink's layout unit of 1/64 px: a
// number times the font size floored to a unit, the size rounded to one
// first, and a length rounded to one, so twenty lines stack as Blink stacks
// them. Fractional metrics keep the computed value (CSS 2.1, 10.8.1).
func TestPixelFontMetrics_LineHeightOnLayoutUnits(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		size  float64
		lh    css.Dimension
		pixel bool
		want  float64
	}{
		// 15.33 px rounds to 981/64; times 1.5, 1471.5/64 floors to 1471.
		{"number", 11.5, css.Dimension{Kind: css.Number, Value: 1.5}, true, px(20 * 1471.0 / 64)},
		// 11.67 px rounds up to 747/64; times 1.8, 1344.6/64 floors to 1344.
		{"number, size rounded up", 8.75, css.Dimension{Kind: css.Number, Value: 1.8}, true, px(20 * 1344.0 / 64)},
		{"number fractional", 11.5, css.Dimension{Kind: css.Number, Value: 1.5}, false, 20 * 11.5 * 1.5},
		// 20.3 px is 1299.2/64, rounded to 1299.
		{"length", 12, css.Pts(px(20.3)), true, px(20 * 1299.0 / 64)},
		{"length fractional", 12, css.Pts(px(20.3)), false, px(20 * 20.3)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(func(s *css.Style) { s.FontSize = tt.size; s.LineHeight = tt.lh })
			kids := []*Node{txt(st, "a")}
			for range 19 {
				kids = append(kids, br(st), txt(st, "a"))
			}
			pb := page(100, 1000)
			rb := boxesWith(t, doc(el(st, kids...)), Options{Page: pb, PixelFontMetrics: tt.pixel})
			if got := kid(t, rb, 0).h; !near(got, tt.want) {
				t.Errorf("twenty lines %v pt, want %v", got, tt.want)
			}
		})
	}
}

// TestPixelFontMetrics_HalvesTheLeadingInUnits locks Blink's split of a
// leading of an odd number of layout units: half of it truncated to a unit
// and then floored to a pixel, so a leading of 383/64 px puts 2 px above
// the content, not the 3 px its half rounded up would.
func TestPixelFontMetrics_HalvesTheLeadingInUnits(t *testing.T) {
	t.Parallel()
	// Ahem at 10px, ascent 8px and descent 2px, on lines of 10 + 383/64 px.
	st := sty(func(s *css.Style) { s.LineHeight = css.Pts(px(10 + 383.0/64)) })
	res, err := Layout(context.Background(), doc(el(st, txt(st, "a"))), ahem(t), Options{Page: page(40, 40), PixelFontMetrics: true})
	if err != nil {
		t.Fatal(err)
	}
	ts := texts(res, 1)
	if len(ts) != 1 || !near(ts[0].y, px(10)) {
		t.Errorf("texts %+v, want the baseline at %v", ts, px(10))
	}
}

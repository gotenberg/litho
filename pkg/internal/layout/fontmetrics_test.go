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

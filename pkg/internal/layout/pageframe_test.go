package layout

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// frameOf returns a page frame inset by in px with a solid border w px
// wide in colour c.
func frameOf(in, w float64, c css.Color) *PageFrame {
	st := css.Initial()
	for i := range st.Border {
		st.Border[i] = css.Border{Width: px(w), Style: css.Solid, Color: c}
	}
	return &PageFrame{Inset: [4]float64{px(in), px(in), px(in), px(in)}, Style: st}
}

// TestPageFrame_Draws paints the page frame over the page background and
// under the margin content and the page area's content, on the pages
// whose box has one.
func TestPageFrame_Draws(t *testing.T) {
	bgSt := css.Initial()
	bgSt.BackgroundColor = blue
	pb := PageBox{Width: px(10), Height: px(10), Margin: [4]float64{px(4), px(4), px(4), px(4)}, Background: bgSt}
	framed := pb
	framed.Frame = frameOf(1, 2, red)
	opts := Options{
		Page: pb,
		PageFor: func(info PageInfo) PageBox {
			if info.Index == 0 {
				return framed
			}
			return pb
		},
		MarginContent: func(info PageInfo, _ int) []compose.Fragment {
			// A lime square over the frame's top right corner.
			return []compose.Fragment{&compose.Path{Ops: rectOps(compose.Rect{X: px(7), Y: 0, Width: px(3), Height: px(3)}), Fill: &compose.Fill{Color: compose.Color{G: 1, A: 1}}}}
		},
	}
	// The page area content, black, overlaps the frame's inner edge.
	content := el(sty(bg(black), width(2), height(2), margin(0, 0, 0, -3)))
	brk := el(sty(func(s *css.Style) { s.BreakBefore = css.Page }, height(1)))
	res, err := Layout(context.Background(), doc(content, brk), ahem(t), opts)
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	assertPixels(t, res, 1, `
		BBBBBBBGGG
		BRRRRRRGGG
		BRRRRRRGGG
		BRRBBBBRRB
		BKKBBBBRRB
		BKKBBBBRRB
		BRRBBBBRRB
		BRRRRRRRRB
		BRRRRRRRRB
		BBBBBBBBBB`)
	assertPixels(t, res, 2, `
		BBBBBBBGGG
		BBBBBBBGGG
		BBBBBBBGGG
		BBBBBBBBBB
		BBBBBBBBBB
		BBBBBBBBBB
		BBBBBBBBBB
		BBBBBBBBBB
		BBBBBBBBBB
		BBBBBBBBBB`)
}

// TestPageFrame_StyleAndBleed draws each side of the frame in its own
// style and colour, the corners where two colours meet split along the
// diagonal, and places it in the page box when the sheet bleeds.
func TestPageFrame_StyleAndBleed(t *testing.T) {
	f := frameOf(2, 1, red)
	f.Style.Border[css.Top].Style = css.BorderNone
	f.Style.Border[css.Left].Color = blue
	pb := PageBox{Width: px(8), Height: px(8), Margin: [4]float64{px(3), px(3), px(3), px(3)}, Bleed: px(1), Frame: f}
	res, err := Layout(context.Background(), doc(), ahem(t), Options{Page: pb})
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	assertPixels(t, res, 1, `
		__________
		__________
		__________
		___B__R___
		___B__R___
		___B__R___
		___?RR?___
		__________
		__________
		__________`)
}

// TestPageFrame_Invalid refuses a frame whose insets leave no box or are
// not finite.
func TestPageFrame_Invalid(t *testing.T) {
	for _, in := range [][4]float64{{-1, 0, 0, 0}, {0, 0, 0, math.NaN()}, {0, 50, 0, 50}} {
		pb := page(100, 100)
		pb.Frame = &PageFrame{Inset: in, Style: css.Initial()}
		if _, err := Layout(context.Background(), doc(), ahem(t), Options{Page: pb}); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("insets %v: %v, want ErrInvalidOptions", in, err)
		}
	}
}

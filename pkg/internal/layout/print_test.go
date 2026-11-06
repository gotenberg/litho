package layout

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestContentWidth_ScrollableOverflow(t *testing.T) {
	t.Parallel()
	white := func(s *css.Style) { s.WhiteSpace = css.Pre }
	hidden := func(s *css.Style) { s.Overflow = css.OverflowHidden }
	tests := []struct {
		name string
		root *Node
		want float64
	}{
		{"fits", doc(el(sty(height(5)))), px(20)},
		{"wide block", doc(el(sty(width(50), height(5)))), px(50)},
		{"unbreakable line", doc(para(white)(1), el(sty(), txt(sty(white), "aaaaa"))), px(50)},
		{"inline block past the edge", doc(el(sty(), el(sty(display(css.InlineBlock), width(35), height(5))))), px(35)},
		{"clipped by a box", doc(el(sty(width(10), hidden), el(sty(width(80), height(5))))), px(20)},
		{"margin-left moves the box", doc(el(sty(margin(0, 0, 0, 15), width(10), height(5)))), px(25)},
		{"floats count", doc(el(sty(float(css.FloatLeft), width(30), height(5)))), px(30)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := run(t, tt.root, page(20, 40))
			if !near(res.ContentWidth, tt.want) {
				t.Errorf("content width %v, want %v", res.ContentWidth, tt.want)
			}
		})
	}
}

func TestContentWidth_ViewportOverflowCounts(t *testing.T) {
	t.Parallel()
	// The body's overflow: hidden goes to the viewport: the page area
	// clips, the body does not, and its overflow still counts.
	root := el(sty(), el(sty(func(s *css.Style) { s.Overflow = css.OverflowHidden }), el(sty(width(60), height(5)))))
	if res := run(t, root, page(20, 40)); !near(res.ContentWidth, px(60)) {
		t.Errorf("content width %v, want %v", res.ContentWidth, px(60))
	}
}

func TestFitFirstPage_TrimsToContent(t *testing.T) {
	t.Parallel()
	brk := func(s *css.Style) { s.BreakBefore = css.Page }
	tests := []struct {
		name  string
		root  *Node
		pb    PageBox
		want  float64
		pages int
	}{
		{"short content", doc(el(sty(height(12), bg(red)))), page(20, 100), px(12), 1},
		{"margins added", doc(el(sty(height(12)))), PageBox{Width: px(20), Height: px(100), Margin: [4]float64{px(3), 0, px(4), 0}}, px(19), 1},
		{"forced break ends the page", doc(el(sty(height(12))), el(sty(height(30), brk))), page(20, 100), px(12), 2},
		{"taller than the box keeps the box", doc(el(sty(height(150)))), page(20, 100), px(100), 2},
		{"empty document takes a point", doc(), page(20, 100), 1, 1},
		{"body margin counts", doc(el(sty(height(12), margin(0, 0, 6, 0)))), page(20, 100), px(18), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := Layout(context.Background(), tt.root, ahem(t), Options{Page: tt.pb, FitFirstPage: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := document(res).Validate(); err != nil {
				t.Fatal(err)
			}
			if got := res.Pages[0].Height; !near(got, tt.want) {
				t.Errorf("first page height %v, want %v", got, tt.want)
			}
			if len(res.Pages) != tt.pages {
				t.Errorf("%d pages, want %d", len(res.Pages), tt.pages)
			}
			for _, p := range res.Pages[1:] {
				if p.Height != tt.pb.Height {
					t.Errorf("later page height %v, want the box's %v", p.Height, tt.pb.Height)
				}
			}
		})
	}
}

func TestFitFirstPage_PaintsTheTrimmedPage(t *testing.T) {
	t.Parallel()
	root := el(sty(bg(lime)), el(sty(), el(sty(height(2), width(3), bg(red)))))
	res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(4, 40), FitFirstPage: true})
	if err != nil {
		t.Fatal(err)
	}
	assertPixels(t, res, 1, `
		RRRG
		RRRG
	`)
}

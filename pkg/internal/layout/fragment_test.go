package layout

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// para returns a block of n lines "a".
func para(mods ...func(*css.Style)) func(n int) *Node {
	return func(n int) *Node {
		st := sty(mods...)
		return el(st, txt(st, strings.TrimSpace(strings.Repeat("a ", n))))
	}
}

// pageLines returns the number of text lines on each page of res.
func pageLines(res *Result) []int {
	out := make([]int, len(res.Pages))
	for i := range res.Pages {
		lines, _ := lineTexts(res, i+1)
		out[i] = len(lines)
	}
	return out
}

func TestFragmentation_Lines(t *testing.T) {
	tests := []struct {
		name string
		root *Node
		want []int
	}{
		{"fills pages", doc(para()(5)), []int{3, 2}},
		{"widows pull a line over", doc(para()(4)), []int{2, 2}},
		{"one widow allowed", doc(para(func(s *css.Style) { s.Widows = 1 })(4)), []int{3, 1}},
		{"orphans push the block", doc(el(sty(height(20))), para()(3)), []int{0, 3}},
		{"one orphan allowed", doc(el(sty(height(20))), para(func(s *css.Style) { s.Orphans = 1 })(3)), []int{1, 2}},
		{"forced break", doc(para()(1), para(func(s *css.Style) { s.BreakBefore = css.Page })(1)), []int{1, 1}},
		{"forced break after", doc(para(func(s *css.Style) { s.BreakAfter = css.Page })(1), para()(1)), []int{1, 1}},
		{"no break before the first box", doc(para(func(s *css.Style) { s.BreakBefore = css.Page })(1)), []int{1}},
		{"break-inside avoid", doc(el(sty(height(10))), para(func(s *css.Style) { s.BreakInside = css.InsideAvoid; s.Orphans, s.Widows = 1, 1 })(3)), []int{0, 3}},
		{"break-after avoid", doc(el(sty(height(10))), para(func(s *css.Style) { s.BreakAfter = css.BreakAvoid })(1), para(func(s *css.Style) { s.BreakInside = css.InsideAvoid })(2)), []int{0, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, tt.root, page(10, 30))
			got := pageLines(res)
			if len(got) != len(tt.want) {
				t.Fatalf("lines per page %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("lines per page %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFragmentation_ForcedBreakKeepsMargin(t *testing.T) {
	rb := boxes(t, doc(para()(1), para(margin(5, 0), func(s *css.Style) { s.BreakBefore = css.Page })(1)), page(10, 30))
	if got := rect(kid(t, rb, 0, 1))[1]; got != 35 {
		t.Errorf("top %v, want 35: page 2 plus the margin", got)
	}
}

func TestFragmentation_BackgroundSliced(t *testing.T) {
	root := doc(el(sty(bg(red), padding(1, 0)), el(sty(height(5))), el(sty(height(5)))))
	res := run(t, root, page(2, 4))
	if len(res.Pages) != 3 {
		t.Fatalf("%d pages, want 3", len(res.Pages))
	}
	for i := 1; i <= 3; i++ {
		assertPixels(t, res, i, "RR\nRR\nRR\nRR")
	}
}

func TestFragmentation_MonolithicMovesAndClips(t *testing.T) {
	img := func(h int) *Node {
		return &Node{Kind: Replaced, Style: sty(), Picture: pic(color.NRGBA{B: 255, A: 255}, 2, h)}
	}
	res := run(t, doc(el(sty(height(3))), img(2)), page(2, 4))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2: the image moves to the next page", len(res.Pages))
	}
	assertPixels(t, res, 2, "BB\nBB\n__\n__")
	res = run(t, doc(img(6), el(sty(height(1), bg(red)))), page(2, 4))
	found := false
	for _, w := range res.Warnings {
		found = found || w.Code == codeClipped
	}
	if !found || len(res.Pages) != 2 {
		t.Fatalf("%d pages, warnings %+v: want the tall image clipped on page 1", len(res.Pages), res.Warnings)
	}
	assertPixels(t, res, 1, "BB\nBB\nBB\nBB")
	assertPixels(t, res, 2, "RR\n__\n__\n__")
}

func TestFragmentation_PageMargins(t *testing.T) {
	pb := page(4, 4)
	pb.Margin = [4]float64{px(1), px(1), px(1), px(1)}
	res := run(t, doc(el(sty(height(3), bg(red)))), pb)
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	assertPixels(t, res, 1, "____\n_RR_\n_RR_\n____")
	assertPixels(t, res, 2, "____\n_RR_\n____\n____")
}

func TestFragmentation_FloatFragments(t *testing.T) {
	st := sty(float(css.FloatLeft), width(10), func(s *css.Style) { s.Orphans, s.Widows = 1, 1 })
	res := run(t, doc(el(st, txt(st, "a a a a a"))), page(20, 30))
	if got := pageLines(res); len(got) != 2 || got[0] != 3 || got[1] != 2 {
		t.Errorf("lines per page %v, want [3 2]", got)
	}
}

func TestLayout_MaxPages(t *testing.T) {
	_, err := Layout(context.Background(), doc(para()(50)), ahem(t), Options{Page: page(10, 30), MaxPages: 3})
	if !errors.Is(err, ErrTooManyPages) {
		t.Errorf("error %v, want ErrTooManyPages", err)
	}
}

func TestLayout_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Layout(ctx, doc(para()(5)), ahem(t), Options{Page: page(10, 30)}); !errors.Is(err, context.Canceled) {
		t.Errorf("error %v, want context.Canceled", err)
	}
}

func TestLayout_InvalidOptions(t *testing.T) {
	for _, pb := range []PageBox{{}, {Width: 10, Height: 10, Margin: [4]float64{6, 0, 6, 0}}, {Width: 10, Height: -1}} {
		if _, err := Layout(context.Background(), doc(), ahem(t), Options{Page: pb}); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("page %+v: error %v, want ErrInvalidOptions", pb, err)
		}
	}
	if _, err := Layout(context.Background(), nil, ahem(t), Options{Page: page(10, 10)}); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("nil root: error %v", err)
	}
}

func TestLayout_EmptyHasOnePage(t *testing.T) {
	res := run(t, el(sty(func(s *css.Style) { s.Display = css.DisplayNone })), page(10, 10))
	if len(res.Pages) != 1 || res.Structure.Role != "Document" {
		t.Errorf("%d pages, structure %+v", len(res.Pages), res.Structure)
	}
}

func TestLinks_AndAnchors(t *testing.T) {
	a := el(inlineSty(), txt(inlineSty(), "aa bb"))
	a.Href = "https://example.com/"
	a.Role = "Link"
	in := el(inlineSty(), txt(inlineSty(), "c"))
	in.Href = "#target"
	bad := el(inlineSty(), txt(inlineSty(), "d"))
	bad.Href = "#nowhere"
	target := el(sty(height(10)))
	target.ID = "target"
	res := run(t, doc(el(sty(), a, txt(sty(), " "), in, bad), el(sty(height(30))), target), page(30, 40))
	links := res.Pages[0].Links
	if len(links) != 3 {
		t.Fatalf("%d links on page 1, want two lines of the external link and the internal one: %+v", len(links), links)
	}
	if links[0].URI != "https://example.com/" || links[0].Parent == nil || links[0].Parent.Role != "Link" {
		t.Errorf("first link %+v", links[0])
	}
	if !near(links[0].Rect.Width/css.Pt, 20) || !near(links[1].Rect.Y/css.Pt, 10) {
		t.Errorf("link rectangles %+v %+v", links[0].Rect, links[1].Rect)
	}
	d, ok := res.Anchors["target"]
	if !ok || d.Page != 2 || !near(d.Y/css.Pt, 20) {
		t.Errorf("anchor %+v, want page 2 at 20px", d)
	}
	if links[2].Dest == nil || *links[2].Dest != d {
		t.Errorf("internal link %+v, want the anchor", links[2])
	}
	found := false
	for _, w := range res.Warnings {
		found = found || w.Code == codeLink
	}
	if !found {
		t.Errorf("warnings %+v, want the unresolved link", res.Warnings)
	}
}

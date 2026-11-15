package layout

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
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
		{"orphans win over widows", doc(el(sty(height(10))), para()(3)), []int{2, 1}},
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

// TestFragmentation_EmptyBoxEdgesUnbroken locks that a page cut never runs
// through the block-start border and padding of a box with no content,
// nor through one with no content height: the box moves to the next page.
// A cut at its content edge breaks its content height (CSS Fragmentation
// 3, 4.4, class C).
func TestFragmentation_EmptyBoxEdgesUnbroken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spacer  float64
		mods    []func(*css.Style)
		wantTop float64
	}{
		{name: "border crosses", spacer: 9, mods: []func(*css.Style){border(2, css.Solid, red), height(8)}, wantTop: 10},
		{name: "padding crosses", spacer: 7, mods: []func(*css.Style){border(2, css.Solid, red), padding(2, 0, 0), height(6)}, wantTop: 10},
		{name: "edges alone", spacer: 8, mods: []func(*css.Style){border(2, css.Solid, red)}, wantTop: 10},
		{name: "border ends at the cut", spacer: 8, mods: []func(*css.Style){border(2, css.Solid, red), height(8)}, wantTop: 8},
		{name: "cut in the content", spacer: 5, mods: []func(*css.Style){border(2, css.Solid, red), height(8)}, wantTop: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rb := boxes(t, doc(el(sty(height(tt.spacer))), el(sty(tt.mods...))), page(20, 10))
			if got := rect(kid(t, rb, 0, 1))[1]; got != tt.wantTop {
				t.Errorf("top %v, want %v", got, tt.wantTop)
			}
		})
	}
}

func TestFragmentation_MonolithicMovesAndOverflows(t *testing.T) {
	img := func(h int) *Node {
		return &Node{Kind: Replaced, Style: sty(), Picture: pic(color.NRGBA{B: 255, A: 255}, 2, h)}
	}
	res := run(t, doc(el(sty(height(3))), img(2)), page(2, 4))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2: the image moves to the next page", len(res.Pages))
	}
	assertPixels(t, res, 2, "BB\nBB\n__\n__")
	// An image taller than a page overflows onto the next one, and what
	// follows it starts below its full height.
	res = run(t, doc(img(6), el(sty(height(1), bg(red)))), page(2, 4))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	assertPixels(t, res, 1, "BB\nBB\nBB\nBB")
	assertPixels(t, res, 2, "BB\nBB\nRR\n__")
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
	if !errors.Is(err, limits.ErrTooManyPages) {
		t.Errorf("error %v, want limits.ErrTooManyPages", err)
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
		found = found || w.Code == warn.LinkUnresolved
	}
	if !found {
		t.Errorf("warnings %+v, want the unresolved link", res.Warnings)
	}
}

// TestLinks_TopOfDocument sends a link to the empty fragment, and one to
// "top" in any ASCII case that no ID matches, to the top left corner of
// the first page under BrowserRules (HTML Standard, 7.4.6.4, "select the
// indicated part"): an element with the ID wins, another fragment that
// names nothing is left out, and without BrowserRules a link names an
// element or nothing.
func TestLinks_TopOfDocument(t *testing.T) {
	link := func(href string) *Node {
		a := el(inlineSty(), txt(inlineSty(), "a"))
		a.Href, a.Role = href, "Link"
		return a
	}
	tree := func() *Node {
		named := el(sty(height(10)))
		named.ID = "TOP"
		return doc(el(sty(height(40))), el(sty(), link("#"), link("#top"), link("#Top"), link("#TOP"), link("#nowhere")), el(sty(height(40))), named)
	}
	res := runWith(t, tree(), Options{Page: page(60, 40), BrowserRules: true})
	if len(res.Pages) != 3 {
		t.Fatalf("%d pages, want the links on page 2 and the element on page 3", len(res.Pages))
	}
	links := res.Pages[1].Links
	if len(links) != 4 {
		t.Fatalf("%d links on page 2, want every one but the fragment that names nothing: %+v", len(links), links)
	}
	for i, l := range links[:3] {
		if l.Dest == nil || *l.Dest != (typeset.Dest{Page: 1}) {
			t.Errorf("link %d goes to %+v, want the corner of page 1", i, l.Dest)
		}
	}
	if d := links[3].Dest; d == nil || d.Page != 3 {
		t.Errorf("#TOP goes to %+v, want the element with that ID on page 3", d)
	}
	unresolved := 0
	for _, w := range res.Warnings {
		if w.Code == warn.LinkUnresolved {
			unresolved++
		}
	}
	if unresolved != 1 {
		t.Errorf("%d unresolved links reported, want #nowhere alone: %+v", unresolved, res.Warnings)
	}

	plain := runWith(t, tree(), Options{Page: page(60, 40)})
	if links := plain.Pages[1].Links; len(links) != 1 || links[0].Dest == nil || links[0].Dest.Page != 3 {
		t.Errorf("without BrowserRules: links %+v, want #TOP alone", links)
	}
}

// TestFragment_LastLineKeepsTheEndPadding moves a block's last line to the
// next column with the block-end padding that follows it, under
// BrowserRules, as Chromium breaks it: no break point lies inside the
// padding (CSS Fragmentation 3, 4.1), and orphans and widows give way.
// Office documents keep the cut through the padding.
func TestFragment_LastLineKeepsTheEndPadding(t *testing.T) {
	t.Parallel()
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	small := func(s *css.Style) { s.FontSize = px(1) }
	ib := func() *Node { return el(sty(display(css.InlineBlock), width(2), height(1), bg(lime))) }
	root := doc(el(sty(columns(2, 1), height(5), auto), strip(black), el(sty(small, padding(0, 0, 3, 0), bg(blue)), ib(), ib(), ib())))
	res := runWith(t, root, Options{Page: page(6, 6), BrowserRules: true})
	assertPixels(t, res, 1, `
		KK_GG_
		GG_BB_
		GG_BB_
		BB_BB_
		BB____
		______`)
	res = runWith(t, root, Options{Page: page(6, 6)})
	assertPixels(t, res, 1, `
		KK_BB_
		GG_BB_
		GG_BB_
		GG____
		BB____
		______`)
	// The break before the moved line does not count against orphans.
	root = doc(el(sty(columns(2, 1), height(5), auto), strip(black, height(2)), el(sty(small, padding(0, 0, 3, 0), bg(blue)), ib(), ib())))
	res = runWith(t, root, Options{Page: page(6, 6), BrowserRules: true})
	assertPixels(t, res, 1, `
		KK_GG_
		KK_BB_
		GG_BB_
		BB_BB_
		BB____
		______`)
}

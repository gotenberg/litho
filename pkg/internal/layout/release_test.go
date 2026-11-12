package layout

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// pdfOf writes res as PDF.
func pdfOf(t testing.TB, res *Result) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := typeset.Write(context.Background(), document(res), &b, typeset.Options{}); err != nil {
		t.Fatalf("typeset.Write: %v", err)
	}
	return b.Bytes()
}

// assertReleaseKeepsOutput lays root out again with o under the release
// policies other than o's and fails unless each writes the PDF res, laid
// out under o's, writes: a released paragraph shaped again is the one
// released.
func assertReleaseKeepsOutput(t testing.TB, root *Node, o Options, res *Result) {
	t.Helper()
	want := pdfOf(t, res)
	for _, p := range []releasePolicy{releaseFinal, releaseNever, releaseEagerly} {
		if p == o.release {
			continue
		}
		po := o
		po.release = p
		other, err := Layout(context.Background(), root, ahem(t), po)
		if err != nil {
			t.Fatalf("Layout under release policy %d: %v", p, err)
		}
		if !bytes.Equal(pdfOf(t, other), want) {
			t.Fatalf("release policy %d writes another PDF than policy %d", p, o.release)
		}
	}
}

// layoutEngine lays root out in one pass on pb under policy p and returns
// the engine and the root box.
func layoutEngine(t testing.TB, root *Node, pb PageBox, p releasePolicy) (*engine, *box) {
	t.Helper()
	e, rb, err := layoutFlow(context.Background(), root, ahem(t), Options{Page: pb, release: p}, pb, nil, nil)
	if err != nil {
		t.Fatalf("layoutFlow: %v", err)
	}
	return e, rb
}

// shapedHeld returns the paragraphs of rb's subtree that hold shaped text.
func shapedHeld(rb *box) int {
	n := 0
	stack := []*box{rb}
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if b.ifc != nil {
			for _, p := range b.ifc.paras {
				if p.p != nil {
					n++
				}
			}
		}
		stack = append(stack, b.kids...)
	}
	return n
}

// TestRelease_FinalParagraphsGiveBackTheirText releases the shaped text of
// every paragraph block flow will not lay out again, all but the last, and
// gives back what the budget was charged for it.
func TestRelease_FinalParagraphsGiveBackTheirText(t *testing.T) {
	var kids []*Node
	for range 30 {
		kids = append(kids, el(sty(width(40)), txt(inlineSty(), "aa bb cc dd ee ff")))
	}
	root := doc(kids...)
	kept, krb := layoutEngine(t, root, page(40, 50), releaseNever)
	freed, frb := layoutEngine(t, root, page(40, 50), releaseFinal)
	if got := shapedHeld(krb); got != 30 {
		t.Fatalf("kept %d shaped paragraphs, want 30", got)
	}
	if got := shapedHeld(frb); got != 1 {
		t.Errorf("held %d shaped paragraphs once laid out, want the last one alone", got)
	}
	if freed.reshaped != 0 {
		t.Errorf("shaped %d contents again, want none", freed.reshaped)
	}
	if want := kept.held - 29*int64(len("aa bb cc dd ee ff"))*paraRuneCost; freed.held != want {
		t.Errorf("accounted %d bytes, want %d", freed.held, want)
	}
}

// TestRelease_RetriedLayoutsKeepTheirText keeps the shaped text of the
// boxes whose layout block flow repeats: siblings a break avoids between or
// inside, a table, a multi-column container.
func TestRelease_RetriedLayoutsKeepTheirText(t *testing.T) {
	para := func(mods ...func(*css.Style)) *Node {
		return el(sty(append([]func(*css.Style){width(40)}, mods...)...), txt(inlineSty(), "aa bb"))
	}
	keep := func(s *css.Style) { s.BreakAfter = css.BreakAvoid }
	whole := func(s *css.Style) { s.BreakInside = css.InsideAvoid }
	cols := func(s *css.Style) { s.Columns.Count = 2 }
	for _, tt := range []struct {
		name string
		kids []*Node
	}{
		{"break-after avoid", []*Node{el(sty(keep), para(), para()), para()}},
		{"break-inside avoid", []*Node{el(sty(whole), para(), para()), para()}},
		{"columns", []*Node{el(sty(cols), para(), para(), para()), para()}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, rb := layoutEngine(t, doc(tt.kids...), page(40, 50), releaseFinal)
			// The container's paragraphs are released with the container
			// once its next sibling is placed, never by its own flow.
			outer := kid(t, rb, 0, 0)
			if outer.settled {
				t.Errorf("the container is settled")
			}
		})
	}
}

// TestRelease_RelaidAncestorShapesAgain lays a block out again on the next
// page, its content starting there although its top fits on the page
// before: the paragraphs it released are shaped again, and the layout
// writes what one keeping them writes.
func TestRelease_RelaidAncestorShapesAgain(t *testing.T) {
	// The inline box with edges takes room its paragraph's position
	// indexes give line building.
	edged := inlineSty(func(s *css.Style) {
		s.Padding[css.Left] = css.Pts(px(3))
		s.Border[css.Right] = css.Border{Width: px(2), Style: css.Solid, Color: red}
	})
	var paras []*Node
	for range 4 {
		paras = append(paras, el(sty(width(40)), txt(inlineSty(), "a "), el(edged, txt(edged, "a"))))
	}
	padded := func(s *css.Style) { s.Padding[css.Top] = css.Pts(px(6)) }
	root := doc(el(sty(height(45))), el(sty(width(40), padded), paras...))
	e, rb := layoutEngine(t, root, page(40, 50), releaseFinal)
	if e.reshaped == 0 {
		t.Fatalf("no content shaped again: the block was not laid out again")
	}
	if got := e.pageOf(kid(t, rb, 0, 1).y); got != 1 {
		t.Fatalf("the block starts page %d, want 1", got)
	}
	runWith(t, root, Options{Page: page(40, 50)})
}

// TestRelease_LaterPassesKeepOutput lays out a document whose earlier pages
// depend on later content, under every release policy: footnotes whose
// bodies push their calls' lines to the next page, a heading kept with the
// paragraph after it, widows and orphans, a table cut across pages, a
// page-anchored box the flow wraps around (a second layout pass), and a
// page count every page's margin shows. Each policy writes the same PDF,
// the final policy releasing paragraphs and the eager one shaping text
// again.
func TestRelease_LaterPassesKeepOutput(t *testing.T) {
	small := func(s *css.Style) { s.FontSize = px(2); s.LineHeight = css.Dimension{Kind: css.Number, Value: 1} }
	// An inline box with edges takes room at its start and end, which line
	// building reads from the paragraph's position indexes.
	edged := inlineSty(small, func(s *css.Style) {
		s.Padding[css.Left], s.Padding[css.Right] = css.Pts(px(1)), css.Pts(px(2))
		s.Border[css.Left] = css.Border{Width: px(1), Style: css.Solid, Color: red}
	})
	var kids []*Node
	for i := range 12 {
		kids = append(kids,
			el(sty(small, func(s *css.Style) { s.BreakAfter = css.BreakAvoid }), txt(inlineSty(small), "head")),
			el(sty(small, func(s *css.Style) { s.Widows, s.Orphans = 2, 2 }),
				txt(inlineSty(small), "aa bb cc dd ee"), el(edged, txt(edged, "ff gg")),
				txt(inlineSty(small), " hh ii jj kk"), call("x", "note body text")),
		)
		if i%4 == 1 {
			kids = append(kids, table(mods(small),
				tr(nil, td(nil, "c1 c1"), td(nil, "c2")),
				tr(nil, td(nil, "c3"), td(nil, "c4 c4 c4"))))
		}
		if i == 2 {
			kids[len(kids)-1].Children = append(kids[len(kids)-1].Children, wrapFrame(css.FrameWrapLargest, 0, 4, 6, 6))
		}
	}
	root := doc(kids...)
	o := Options{
		Page: page(20, 24),
		MarginContent: func(_ PageInfo, pages int) []typeset.Fragment {
			// A bar as long as the page count, the count a header prints.
			return []typeset.Fragment{&typeset.Path{Ops: rectOps(typeset.Rect{Width: px(float64(pages)) / 4, Height: 1}), Fill: &typeset.Fill{Color: typeset.Black}}}
		},
	}
	res, err := Layout(context.Background(), root, ahem(t), o)
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if len(res.Pages) < 3 {
		t.Fatalf("%d pages, want several", len(res.Pages))
	}
	assertReleaseKeepsOutput(t, root, o, res)
	_, rb := layoutEngine(t, root, page(20, 24), releaseFinal)
	if held, kept := shapedHeld(rb), shapedHeldUnder(t, root, page(20, 24), releaseNever); held >= kept {
		t.Errorf("held %d shaped paragraphs, %d without release: none released", held, kept)
	}
	if e, _ := layoutEngine(t, root, page(20, 24), releaseEagerly); e.reshaped == 0 {
		t.Errorf("released eagerly, no text shaped again")
	}
}

// shapedHeldUnder returns the paragraphs that hold shaped text once root is
// laid out on pb under policy p.
func shapedHeldUnder(t testing.TB, root *Node, pb PageBox, p releasePolicy) int {
	t.Helper()
	_, rb := layoutEngine(t, root, pb, p)
	return shapedHeld(rb)
}

package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestTextBoxTrim_CutsFirstAndLastLines(t *testing.T) {
	// CSS Inline 3, 5.1: trim-both cuts the block's first line at its
	// text-over edge and its last at its text-under edge: the half
	// leading of a tall line-height goes on both sides.
	lh := func(s *css.Style) { s.LineHeight = css.Pts(px(60)) }
	plain := boxes(t, doc(el(sty(lh), txt(sty(lh), "a"))), page(200, 200))
	trimmed := boxes(t, doc(el(sty(lh, func(s *css.Style) { s.TextBoxTrim = css.TrimBoth }), txt(sty(lh), "a"))), page(200, 200))
	p, q := kid(t, plain, 0, 0), kid(t, trimmed, 0, 0)
	l := q.lines[0]
	d := q.contentY() - l.y
	if d <= 0 {
		t.Fatalf("first line not moved up: %v", d/css.Pt)
	}
	if !near(p.h/css.Pt, 60) || !near(q.h/css.Pt, (l.h-2*d)/css.Pt) {
		t.Errorf("heights %v and %v, want 60 and %v: the half leading off both sides", p.h/css.Pt, q.h/css.Pt, (l.h-2*d)/css.Pt)
	}
}

func TestTextBoxTrim_PassesOverCollapsedWhiteSpace(t *testing.T) {
	// CSS Inline 3, 6.2: the first and last formatted lines of a block
	// lie in its first and last in-flow block children, flow roots
	// included. The white space between them generates no box (CSS 2.1,
	// 9.2.2.1): its anonymous blocks hide no line.
	lh := func(s *css.Style) { s.LineHeight = css.Pts(px(60)) }
	root := func(s *css.Style) { s.Display = css.FlowRoot }
	trim := func(s *css.Style) { s.TextBoxTrim = css.TrimBoth }
	b := boxes(t, doc(el(sty(lh, trim),
		txt(sty(lh), " "), el(sty(lh, root), txt(sty(lh), "a")), txt(sty(lh), " "),
		el(sty(lh, root), txt(sty(lh), "a")), txt(sty(lh), " "),
	)), page(200, 200))
	c := kid(t, b, 0, 0)
	// Two 60px lines of 10px Ahem lose their outer 25px half leadings.
	if !near(c.h/css.Pt, 70) {
		t.Errorf("height %v, want 70: the outer half leadings trimmed", c.h/css.Pt)
	}
}

func TestTextBoxTrim_TrimsFormattingContextRoots(t *testing.T) {
	// CSS Inline 3, 6.2: text-box-trim applies to every block
	// container, one that establishes a formatting context included: a
	// flow root, an absolutely positioned box, a scroll container.
	lh := func(s *css.Style) { s.LineHeight = css.Pts(px(60)) }
	trim := func(s *css.Style) { s.TextBoxTrim = css.TrimBoth }
	for name, mod := range map[string]func(*css.Style){
		"flow-root": func(s *css.Style) { s.Display = css.FlowRoot },
		"absolute":  func(s *css.Style) { s.Position = css.Absolute; s.Width = css.Pts(px(100)) },
		"overflow":  func(s *css.Style) { s.Overflow = css.OverflowHidden },
	} {
		b := boxes(t, doc(el(sty(lh, trim, mod), txt(sty(lh), "a"), br(sty(lh)), txt(sty(lh), "a"))), page(200, 200))
		if c := kid(t, b, 0, 0); !near(c.h/css.Pt, 70) {
			t.Errorf("%s: height %v, want 70: the outer half leadings trimmed", name, c.h/css.Pt)
		}
	}
}

func TestTextBoxTrim_MovesTheFloatAfterTheLastLine(t *testing.T) {
	// CSS Inline 3, 6.2: trim-end moves the block-end edge of the last
	// line up; a float placed after that line goes up with it, and the
	// formatting context root that contains the float ends at it.
	lh := func(s *css.Style) { s.LineHeight = css.Pts(px(60)) }
	trim := func(s *css.Style) { s.TextBoxTrim = css.TrimBoth; s.Display = css.FlowRoot }
	fl := el(sty(float(css.FloatRight), width(10), height(10)))
	b := boxes(t, doc(el(sty(lh, trim), txt(sty(lh), "a"), br(sty(lh)), fl)), page(200, 200))
	c := kid(t, b, 0, 0)
	f := c.kids[len(c.kids)-1]
	for f.float == css.FloatNone && len(f.kids) > 0 {
		f = f.kids[len(f.kids)-1]
	}
	if f.float == css.FloatNone {
		t.Fatal("no float box")
	}
	// The 60px line keeps its 10px glyph box: the float sits under it
	// and ends the root 10px lower.
	if !near((f.y-c.y)/css.Pt, 10) || !near(c.h/css.Pt, 20) {
		t.Errorf("float at %v, root %v high; want 10 and 20", (f.y-c.y)/css.Pt, c.h/css.Pt)
	}
}

func TestTextBoxTrim_ReachesThroughMarginsOnce(t *testing.T) {
	// CSS Inline 3, 6.2: margins do not stop the trim, and a line edge
	// that a nested block container trims is not trimmed again by its
	// ancestors.
	lh := func(s *css.Style) { s.LineHeight = css.Pts(px(60)) }
	trim := func(s *css.Style) { s.TextBoxTrim = css.TrimBoth }
	pulled := boxes(t, doc(el(sty(lh, trim, padding(10)), el(sty(lh, margin(-10)), txt(sty(lh), "a"), br(sty(lh)), txt(sty(lh), "a")))), page(200, 200))
	if c := kid(t, pulled, 0, 0); !near(c.borderH()/css.Pt, 70) {
		t.Errorf("negative margin: height %v, want 70: two 60px lines less two half leadings", c.borderH()/css.Pt)
	}
	nested := boxes(t, doc(el(sty(lh, trim), el(sty(lh, trim), txt(sty(lh), "a")))), page(200, 200))
	if c := kid(t, nested, 0, 0); !near(c.h/css.Pt, 10) {
		t.Errorf("nested: height %v, want 10: the half leadings trimmed once", c.h/css.Pt)
	}
}

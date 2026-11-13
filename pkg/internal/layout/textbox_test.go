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

package layout

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// commented returns an inline Annot element carrying c over kids.
func commented(c *typeset.Comment, kids ...*Node) *Node {
	n := el(inlineSty(), kids...)
	n.Role, n.Comment = "Annot", c
	return n
}

// TestComments_LinesAndPoint holds what a comment's element places on the
// pages (Node.Comment): the box of each line of the text it covers, one
// mark a page, and an empty element's point, a square as tall as its
// line, both under the element's Annot structure element.
func TestComments_LinesAndPoint(t *testing.T) {
	t.Parallel()
	over := &typeset.Comment{Text: "over"}
	at := &typeset.Comment{Text: "at"}
	// The page is 60px wide: the lines are "aa bb", "cc dd" and "ee", the
	// element covering "bb" on the first and "cc dd" on the second.
	root := doc(
		el(sty(), txt(inlineSty(), "aa "), commented(over, txt(inlineSty(), "bb cc dd")), txt(inlineSty(), " ee"), commented(at)),
		el(sty(height(30))),
		el(sty(), commented(over, txt(inlineSty(), "ff"))),
	)
	res := run(t, root, page(60, 40))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	marks := res.Pages[0].Comments
	if len(marks) != 2 {
		t.Fatalf("page 1 comments %+v, want the lines of one and the point of the other", marks)
	}
	m := marks[0]
	if m.Comment != over || len(m.Lines) != 2 || m.Parent == nil || m.Parent.Role != "Annot" {
		t.Fatalf("first mark %+v", m)
	}
	if l := m.Lines[0]; !near(l.X/css.Pt, 30) || !near(l.Width/css.Pt, 20) || !near(l.Y, 0) || !near(l.Height/css.Pt, 10) {
		t.Errorf("first line %+v, want bb at 30px, 20px wide, 10px tall", l)
	}
	if l := m.Lines[1]; !near(l.X, 0) || !near(l.Y/css.Pt, 10) || !near(l.Width/css.Pt, 50) {
		t.Errorf("second line %+v, want cc dd from 0, 50px wide", l)
	}
	p := marks[1]
	if p.Comment != at || p.Lines != nil || !near(p.At.Width, p.At.Height) || !near(p.At.Height/css.Pt, 10) || p.Parent == nil {
		t.Errorf("point %+v", p)
	}
	if got := res.Pages[1].Comments; len(got) != 1 || got[0].Comment != over || len(got[0].Lines) != 1 {
		t.Errorf("page 2 comments %+v, want the line of ff", got)
	}
}

// TestComments_DrawNothing lays the same document out with its comments
// and without: the pages write the same content.
func TestComments_DrawNothing(t *testing.T) {
	t.Parallel()
	build := func(c *typeset.Comment) *Node {
		a, b := commented(c, txt(inlineSty(), "bb cc")), commented(c)
		if c == nil {
			a.Role, b.Role = "", ""
		}
		return doc(el(sty(), txt(inlineSty(), "aa "), a, txt(inlineSty(), " dd"), b))
	}
	with, without := run(t, build(&typeset.Comment{Text: "x"}), page(30, 40)), run(t, build(nil), page(30, 40))
	if len(with.Pages[0].Comments) != 2 {
		t.Fatalf("comments %+v", with.Pages[0].Comments)
	}
	if a, b := grid(render(t, with, 1)), grid(render(t, without, 1)); !slices.Equal(a, b) {
		t.Errorf("the comments change what the page draws:\n%v\n%v", a, b)
	}
	a, b := texts(with, 1), texts(without, 1)
	if len(a) != len(b) {
		t.Fatalf("%d runs with the comments, %d without", len(a), len(b))
	}
	for i := range a {
		if a[i].text != b[i].text || a[i].x != b[i].x || a[i].y != b[i].y {
			t.Errorf("run %d at %v with the comments, %v without", i, a[i], b[i])
		}
	}
}

// TestComments_InColumns places a comment's lines where the columns
// paint them: on each a link region over the same element takes.
func TestComments_InColumns(t *testing.T) {
	t.Parallel()
	c := &typeset.Comment{Text: "c"}
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	dd := commented(c, txt(inlineSty(), "dd"))
	dd.Href = "https://example.com/"
	root := doc(el(sty(columns(2, 2), height(20), auto), el(sty(), txt(inlineSty(), "aa bb cc "), dd)))
	res := run(t, root, page(50, 20))
	marks, links := res.Pages[0].Comments, res.Pages[0].Links
	if len(marks) != 1 || len(marks[0].Lines) != 1 || len(links) != 1 {
		t.Fatalf("comments %+v, links %+v", marks, links)
	}
	if marks[0].Lines[0] != links[0].Rect {
		t.Errorf("line %+v, link %+v", marks[0].Lines[0], links[0].Rect)
	}
}

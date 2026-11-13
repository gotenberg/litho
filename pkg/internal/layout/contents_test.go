package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// contentsSty returns a display: contents style that sets box properties
// an element without a box never uses.
func contentsSty(mods ...func(*css.Style)) *css.Style {
	return sty(append([]func(*css.Style){func(s *css.Style) {
		s.Display = css.Contents
		s.Float = css.FloatRight
		s.Position = css.Absolute
		s.Border[css.Top] = css.Border{Style: css.Solid, Width: 5}
		s.BackgroundColor = css.Color{R: 255, A: 255}
	}}, mods...)...)
}

// hasNodeBox reports a box of the node n in the tree of b.
func hasNodeBox(b *box, n *Node) bool {
	if b.node == n {
		return true
	}
	for _, k := range b.kids {
		if hasNodeBox(k, n) {
			return true
		}
	}
	return false
}

// TestGenerate_DisplayContentsHandsItsChildrenUp pins CSS Display 3, 2.5:
// an element of display: contents makes no box, whatever its float,
// position, border and background; its text and its block child sit
// among its parent's children, the text in the anonymous block of the
// parent's inline run, the block beside it.
func TestGenerate_DisplayContentsHandsItsChildrenUp(t *testing.T) {
	c := contentsSty()
	n := el(c, txt(c, "b"), el(sty(), txt(sty(), "c")))
	rb := boxes(t, doc(txt(sty(), "a"), n, txt(sty(), "d")), page(100, 50))
	p := kid(t, rb, 0)
	if hasNodeBox(rb, n) {
		t.Fatal("the display: contents element made a box")
	}
	if len(p.kids) != 3 || !p.kids[0].anon || p.kids[1].anon || !p.kids[2].anon {
		t.Fatalf("%d kids, want an anonymous block of a and b, the block c, an anonymous block of d", len(p.kids))
	}
	if r := p.kids[0].kids; len(r) != 2 || r[0].text != "a" || r[1].text != "b" {
		t.Errorf("first anonymous block holds %d boxes, want the texts a and b", len(r))
	}
	if p.abs || p.float != css.FloatNone || p.kids[1].float != css.FloatNone {
		t.Error("the contents element's float or position reached a box")
	}
}

// TestGenerate_DisplayContentsInTableAndFlex pins that the children of a
// display: contents element are fixed up by the box that holds them (CSS
// Display 3, 2.5): in a table row, its text takes an anonymous cell beside
// its cell (CSS 2.1, 17.2.1, rule 2); in a flex container, its text is an
// anonymous flex item beside its block, a flex item (CSS Flexbox 1, 4).
func TestGenerate_DisplayContentsInTableAndFlex(t *testing.T) {
	c := contentsSty()
	cell := sty(display(css.TableCell))
	row := el(sty(display(css.TableRow)), el(c, txt(c, "x"), el(cell, txt(cell, "y"))))
	rb := boxes(t, doc(el(sty(display(css.Table)), row)), page(100, 50))
	var cells int
	var walk func(b *box)
	walk = func(b *box) {
		if b.tpart == partCell {
			cells++
		}
		for _, k := range b.kids {
			walk(k)
		}
	}
	walk(rb)
	if cells != 2 {
		t.Errorf("%d cells, want an anonymous cell for x and the cell y", cells)
	}
	flex := el(sty(display(css.Flex)), el(c, txt(c, "x"), el(sty(), txt(sty(), "y"))))
	rb = boxes(t, doc(flex), page(100, 50))
	f := kid(t, rb, 0, 0)
	if len(f.kids) != 2 || !f.kids[0].flexItem || !f.kids[0].anon || !f.kids[1].flexItem || f.kids[1].anon {
		t.Fatalf("%d flex items, want an anonymous item for x and the block y", len(f.kids))
	}
}

// TestGenerate_DisplayContentsDecoratesNothing pins that an element of
// display: contents is no decorating box (CSS Text Decoration 3, 2.1: the
// decorations are those of the box), while an ancestor's decorations reach
// its text through it.
func TestGenerate_DisplayContentsDecoratesNothing(t *testing.T) {
	c := contentsSty(func(s *css.Style) { s.TextDecoration = css.LineThrough })
	rb := boxes(t, doc(el(c, txt(c, "a"))), page(100, 50))
	if a := kid(t, rb, 0, 0); a.kind != kindText || len(a.decos) != 0 {
		t.Errorf("text under display: contents carries %d decorations, want none", len(a.decos))
	}
	u := inlineSty(func(s *css.Style) { s.TextDecoration = css.Underline })
	rb = boxes(t, doc(el(u, el(c, txt(c, "a")))), page(100, 50))
	if a := kid(t, rb, 0, 0, 0); a.kind != kindText || len(a.decos) != 1 {
		t.Errorf("text under display: contents carries %d decorations, want the ancestor's underline", len(a.decos))
	}
}

// TestStructure_DisplayContentsKeepsItsRole pins that an element of
// display: contents still starts its structure element, holding its
// children's content, beside the paragraph of the run its text sits in
// (ISO 32005, Table 5: a P holds no P), and that a link of display:
// contents makes no link annotation and no Link element, which holds one
// (ISO 32000-1, 14.8.4.4.2, Table 340).
func TestStructure_DisplayContentsKeepsItsRole(t *testing.T) {
	c := contentsSty()
	p := el(c, txt(c, "para"))
	p.Role = "P"
	a := el(c, txt(c, "link"))
	a.Role, a.Href, a.ID = "Link", "https://example.com/", "target"
	res := run(t, doc(p, a), page(200, 50))
	var roles []string
	var walk func(e *typeset.StructElem)
	walk = func(e *typeset.StructElem) {
		roles = append(roles, e.Role)
		for _, k := range e.Kids {
			walk(k)
		}
	}
	walk(res.Structure)
	if got := strings.Join(roles, " "); got != "Document P P" {
		t.Errorf("structure %q, want the P and the paragraph of the link's text", got)
	}
	for _, pg := range res.Pages {
		if len(pg.Links) != 0 {
			t.Errorf("%d link annotations, want none", len(pg.Links))
		}
	}
	if _, ok := res.Anchors["target"]; ok {
		t.Error("an element without a box landed as an anchor")
	}
}

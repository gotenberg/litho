package layout

import (
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// role returns n with role r.
func role(r string, n *Node) *Node {
	n.Role = r
	return n
}

// shape returns the roles of the tree under e, kids in brackets, "#" for
// an element that owns content (a fragment of res hangs from it).
func shape(res *Result, e *compose.StructElem) string {
	own := map[*compose.StructElem]bool{}
	for p := range res.Pages {
		for _, t := range texts(res, p+1) {
			own[t.run.Parent] = true
		}
	}
	var walk func(e *compose.StructElem) string
	walk = func(e *compose.StructElem) string {
		s := e.Role
		if own[e] {
			s += "#"
		}
		if len(e.Kids) > 0 {
			parts := make([]string, len(e.Kids))
			for i, k := range e.Kids {
				parts[i] = walk(k)
			}
			s += "[" + strings.Join(parts, " ") + "]"
		}
		return s
	}
	return walk(e)
}

// TestTags_NoElementOwnsContentAndKids locks the anonymous elements of
// [engine.anonymize]: content beside elements, or under a grouping
// element, gets an element of its own at its place in document order.
func TestTags_NoElementOwnsContentAndKids(t *testing.T) {
	in := inlineSty()
	for name, tc := range map[string]struct {
		root *Node
		want string
	}{
		"text under the document": {
			doc(el(sty(), txt(sty(), "aa"))),
			"Document[P#]",
		},
		"inline span under a division": {
			doc(role("Div", el(sty(), txt(in, "aa "), role("Span", el(in, txt(in, "bb"))), txt(in, " cc")))),
			"Document[Div[P[Span# Span# Span#]]]",
		},
		"text between spans of a paragraph": {
			doc(role("P", el(sty(), txt(in, "aa "), role("Span", el(in, txt(in, "bb"))), txt(in, " cc")))),
			"Document[P[Span# Span# Span#]]",
		},
		"text beside a paragraph in a section": {
			doc(role("Sect", el(sty(), txt(in, "aa"), role("P", el(sty(), txt(in, "bb"))), txt(in, "cc")))),
			"Document[Sect[P# P# P#]]",
		},
		"text beside a paragraph in a cell": {
			doc(role("Table", el(sty(display(css.Table)), role("TR", el(sty(display(css.TableRow)),
				role("TD", el(sty(display(css.TableCell)), txt(in, "aa"), role("P", el(sty(), txt(in, "bb")))))))))),
			"Document[Table[TR[TD[P# P#]]]]",
		},
		"text alone in a cell": {
			doc(role("Table", el(sty(display(css.Table)), role("TR", el(sty(display(css.TableRow)),
				role("TD", el(sty(display(css.TableCell)), txt(in, "aa")))))))),
			"Document[Table[TR[TD#]]]",
		},
		"inline element as a flex item": {
			doc(role("Div", el(sty(display(css.Flex)), role("Span", el(in, txt(in, "aa")))))),
			"Document[Div[P[Span#]]]",
		},
		"text of a table of contents entry": {
			doc(role("TOC", el(sty(), role("TOCI", el(sty(), txt(in, "aa "), role("Link", el(in, txt(in, "1")))))))),
			"Document[TOC[TOCI[P[Span# Link#]]]]",
		},
		"paragraph alone": {
			doc(role("P", el(sty(), txt(in, "aa")))),
			"Document[P#]",
		},
	} {
		res := run(t, tc.root, page(200, 100))
		if got := shape(res, res.Structure); got != tc.want {
			t.Errorf("%s: structure %s, want %s", name, got, tc.want)
		}
	}
}

// TestTags_OrderAndTextKept checks that the anonymous elements read in
// document order and lose no word.
func TestTags_OrderAndTextKept(t *testing.T) {
	in := inlineSty()
	root := doc(role("P", el(sty(), txt(in, "aa "), role("Span", el(in, txt(in, "bb"))), txt(in, " cc"))))
	res := run(t, root, page(200, 100))
	byElem := map[*compose.StructElem]string{}
	var all []string
	for _, pt := range texts(res, 1) {
		byElem[pt.run.Parent] += pt.text
		all = append(all, pt.text)
	}
	var order []string
	for _, k := range res.Structure.Kids[0].Kids {
		order = append(order, strings.TrimSpace(byElem[k]))
	}
	if got := strings.Join(order, "|"); got != "aa|bb|cc" {
		t.Errorf("kids read %q, want aa|bb|cc", got)
	}
	if got := strings.Join(strings.Fields(strings.Join(all, " ")), " "); got != "aa bb cc" {
		t.Errorf("text %q, want aa bb cc", got)
	}
}

func TestTags_SpansScopeAndNumbering(t *testing.T) {
	cell := role("TH", el(sty(display(css.TableCell)), txt(inlineSty(), "aa")))
	cell.ColSpan, cell.RowSpan, cell.Scope = 2, compose.MaxCellSpan+5, compose.ScopeColumn
	li := sty(display(css.ListItem), func(s *css.Style) { s.ListStyleType = css.UpperRoman })
	list := role("L", el(sty(), role("LI", el(li, txt(inlineSty(), "x")))))
	root := doc(role("Table", el(sty(display(css.Table)), role("TR", el(sty(display(css.TableRow)), cell)))), list)
	res := run(t, root, page(200, 100))
	var th, l *compose.StructElem
	var walk func(e *compose.StructElem)
	walk = func(e *compose.StructElem) {
		switch e.Role {
		case "TH":
			th = e
		case "L":
			l = e
		}
		for _, k := range e.Kids {
			walk(k)
		}
	}
	walk(res.Structure)
	if th == nil || th.ColSpan != 2 || th.RowSpan != compose.MaxCellSpan || th.Scope != compose.ScopeColumn {
		t.Errorf("TH %+v, want ColSpan 2, RowSpan clamped to MaxCellSpan, Scope Column", th)
	}
	if l == nil || l.Numbering != compose.NumberingUpperRoman {
		t.Errorf("L %+v, want the UpperRoman numbering of its markers", l)
	}
}

func TestTags_LinkContentsAndDestElem(t *testing.T) {
	in := inlineSty()
	target := role("H1", el(sty(), txt(in, "Title")))
	target.ID = "t"
	shown := role("Link", el(in, txt(in, "Read   the\ntitle")))
	shown.Href = "#t"
	alt := role("Link", el(in, txt(in, "here")))
	alt.Href, alt.Alt = "https://example.com/", "Example site"
	bare := role("Link", el(in, &Node{Kind: Replaced, Style: in, Picture: pic(color.NRGBA{A: 255}, 4, 4)}))
	bare.Href = "https://example.com/x"
	res := run(t, doc(target, role("P", el(sty(), shown, txt(in, " "), alt, txt(in, " "), bare))), page(400, 100))
	got := map[string]compose.Link{}
	for _, l := range res.Pages[0].Links {
		got[l.Contents] = l
	}
	for _, want := range []string{"Read the title", "Example site", "https://example.com/x"} {
		if _, ok := got[want]; !ok {
			t.Errorf("no link described %q (links %+v)", want, res.Pages[0].Links)
		}
	}
	l := got["Read the title"]
	if l.Dest == nil || l.Dest.Elem == nil || l.Dest.Elem.Role != "H1" {
		t.Errorf("internal link destination %+v, want the H1 element", l.Dest)
	}
	if d := res.Anchors["t"]; d.Elem == nil || d.Elem.Role != "H1" {
		t.Errorf("anchor %+v, want the H1 element", d)
	}
}

func TestTags_HeaderScopes(t *testing.T) {
	in := inlineSty()
	cell := func(r, s string) *Node { return role(r, el(sty(display(css.TableCell)), txt(in, s))) }
	row := func(cells ...*Node) *Node { return role("TR", el(sty(display(css.TableRow)), cells...)) }
	given := cell("TH", "g")
	given.Scope = compose.ScopeBoth
	table := role("Table", el(sty(display(css.Table)),
		role("THead", el(sty(display(css.TableHeaderGroup)), row(cell("TH", "a"), cell("TD", "b")))),
		role("TBody", el(sty(display(css.TableRowGroup)), row(cell("TH", "c"), cell("TH", "d")), row(cell("TH", "e"), cell("TD", "f"), given))),
	))
	res := run(t, doc(table), page(200, 100))
	want := map[string]compose.TableScope{"a": compose.ScopeColumn, "c": compose.ScopeColumn, "d": compose.ScopeColumn, "e": compose.ScopeRow, "g": compose.ScopeBoth}
	for _, pt := range texts(res, 1) {
		if w, ok := want[pt.text]; ok {
			th := pt.run.Parent
			if th.Role != "TH" || th.Scope != w {
				t.Errorf("cell %q: %s with scope %d, want TH with scope %d", pt.text, th.Role, th.Scope, w)
			}
			delete(want, pt.text)
		}
	}
	if len(want) != 0 {
		t.Errorf("cells not found: %v", want)
	}
}

// TestTags_EmptyCellsKept keeps the empty cells and rows of a table with
// content, so its rows count the same columns and its columns the same
// rows.
func TestTags_EmptyCellsKept(t *testing.T) {
	in := inlineSty()
	cell := func(kids ...*Node) *Node { return role("TD", el(sty(display(css.TableCell)), kids...)) }
	row := func(cells ...*Node) *Node { return role("TR", el(sty(display(css.TableRow)), cells...)) }
	wide := cell(txt(in, "a"))
	wide.ColSpan = 2
	table := role("Table", el(sty(display(css.Table)), row(wide), row(cell(txt(in, "b")), cell()), row(cell(), cell())))
	res := run(t, doc(table), page(200, 100))
	if got := shape(res, res.Structure); got != "Document[Table[TR[TD#] TR[TD# TD] TR[TD TD]]]" {
		t.Errorf("structure %s, want the empty cell kept", got)
	}
}

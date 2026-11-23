package layout

import (
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// role returns n with role r.
func role(r string, n *Node) *Node {
	n.Role = r
	return n
}

// shape returns the roles of the tree under e, kids in brackets, "#" for
// an element that owns content (a fragment of res hangs from it).
func shape(res *Result, e *typeset.StructElem) string {
	own := map[*typeset.StructElem]bool{}
	for p := range res.Pages {
		for _, t := range texts(res, p+1) {
			own[t.run.Parent] = true
		}
	}
	var walk func(e *typeset.StructElem) string
	walk = func(e *typeset.StructElem) string {
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
	contents := sty(display(css.Contents))
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
		// A P holds no P (ISO 32005, Table 5): a paragraph of display:
		// contents, whose text sits in the run of its parent, stays
		// between the paragraphs of that run.
		"paragraph of display: contents in a run": {
			doc(role("Div", el(sty(), txt(in, "aa "), role("P", el(contents, txt(contents, "bb"))), txt(in, " cc")))),
			"Document[Div[P# P# P#]]",
		},
		// An L holds no content (ISO 32005, Table 5): the space after an
		// item of display: contents ends its line and is an artifact.
		"space after a list item of display: contents": {
			doc(role("L", el(sty(), role("LI", el(sty(), txt(in, "aa"))), role("LI", el(contents, txt(contents, "bb"))),
				txt(in, " "), role("LI", el(sty(), txt(in, "cc")))))),
			"Document[L[LI[LBody#] LI[LBody#] LI[LBody#]]]",
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
	byElem := map[*typeset.StructElem]string{}
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
	cell.ColSpan, cell.RowSpan, cell.Scope = 2, typeset.MaxCellSpan+5, typeset.ScopeColumn
	li := sty(display(css.ListItem), func(s *css.Style) { s.ListStyleType = css.UpperRoman })
	list := role("L", el(sty(), role("LI", el(li, txt(inlineSty(), "x")))))
	root := doc(role("Table", el(sty(display(css.Table)), role("TR", el(sty(display(css.TableRow)), cell)))), list)
	res := run(t, root, page(200, 100))
	var th, l *typeset.StructElem
	var walk func(e *typeset.StructElem)
	walk = func(e *typeset.StructElem) {
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
	if th == nil || th.ColSpan != 2 || th.RowSpan != 1 || th.Scope != typeset.ScopeColumn {
		t.Errorf("TH %+v, want ColSpan 2, RowSpan clamped to its one-row group, Scope Column", th)
	}
	if l == nil || l.Numbering != typeset.NumberingUpperRoman {
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
	got := map[string]typeset.Link{}
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
	given.Scope = typeset.ScopeBoth
	table := role("Table", el(sty(display(css.Table)),
		role("THead", el(sty(display(css.TableHeaderGroup)), row(cell("TH", "a"), cell("TD", "b")))),
		role("TBody", el(sty(display(css.TableRowGroup)), row(cell("TH", "c"), cell("TH", "d")), row(cell("TH", "e"), cell("TD", "f"), given))),
	))
	res := run(t, doc(table), page(200, 100))
	want := map[string]typeset.TableScope{"a": typeset.ScopeColumn, "c": typeset.ScopeColumn, "d": typeset.ScopeColumn, "e": typeset.ScopeRow, "g": typeset.ScopeBoth}
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

// TestTags_BlocksLeaveParagraphs locks the rule of ISO 32005, Table 5 that
// a paragraph, a heading and an inline element hold no grouping or
// block-level element, and that a table cell or a list body holds no
// DocumentFragment, whatever box starts them: an inline-block division or
// a frame in a paragraph splits it, rising past the inline elements
// around it, and a frame in a cell gets a Sect, which Table 5 does not
// look through as it looks through a Div.
func TestTags_BlocksLeaveParagraphs(t *testing.T) {
	in := inlineSty()
	block := sty(display(css.InlineBlock))
	frame := func() *Node {
		return &Node{Kind: Replaced, Style: sty(width(3), height(2), display(css.InlineBlock)), Role: "DocumentFragment",
			Picture: embedded(func(float64, float64) *Embedded { return redDoc(1) })}
	}
	cell := func(kids ...*Node) *Node {
		return doc(role("Table", el(sty(display(css.Table)), role("TR", el(sty(display(css.TableRow)),
			role("TD", el(sty(display(css.TableCell)), kids...)))))))
	}
	for name, tc := range map[string]struct {
		root *Node
		want string
	}{
		"inline-block division in a paragraph": {
			doc(role("P", el(sty(), txt(in, "aa "), role("Div", el(block, txt(block, "bb"))), txt(in, " cc")))),
			"Document[P[Span#] Div[P#] P[Span#]]",
		},
		"frame in a paragraph": {
			doc(role("P", el(sty(), txt(in, "aa "), frame(), txt(in, " cc")))),
			"Document[P[Span#] DocumentFragment[P] P[Span#]]",
		},
		"frame ending a paragraph": {
			doc(role("P", el(sty(), txt(in, "aa "), frame()))),
			"Document[P[Span#] DocumentFragment[P]]",
		},
		"frame in a span of a paragraph": {
			doc(role("P", el(sty(), txt(in, "aa "), role("Span", el(in, txt(in, "bb "), frame(), txt(in, " cc"))), txt(in, " dd")))),
			"Document[P[Span# Span[Span#]] DocumentFragment[P] P[Span[Span#] Span#]]",
		},
		"inline-block division in a heading": {
			doc(role("H2", el(sty(), txt(in, "aa "), role("Div", el(block, txt(block, "bb"))), txt(in, " cc")))),
			"Document[H2[Span#] Div[P#] H2[Span#]]",
		},
		"frame in a cell's text": {
			cell(txt(in, "aa "), frame(), txt(in, " cc")),
			"Document[Table[TR[TD[P# Sect[DocumentFragment[P]] P#]]]]",
		},
		"frame alone in a cell": {
			cell(frame()),
			"Document[Table[TR[TD[Sect[DocumentFragment[P]]]]]]",
		},
		"inline table in a paragraph": {
			doc(role("P", el(sty(), txt(in, "aa "), role("Table", el(sty(display(css.InlineTable)), role("TR", el(sty(display(css.TableRow)),
				role("TD", el(sty(display(css.TableCell)), txt(in, "bb"))))))), txt(in, " cc")))),
			"Document[P[Span# Table[TR[TD#]] Span#]]",
		},
	} {
		res := run(t, tc.root, page(200, 100))
		if got := shape(res, res.Structure); got != tc.want {
			t.Errorf("%s: structure %s, want %s", name, got, tc.want)
		}
	}
}

// TestTags_LanguageOfARun locks that an inline node in a language other
// than the text around it takes a Span stating it (ISO 32000-1, 14.9.2.2;
// ISO 14289-1, 7.2), and one in the language around it takes none: a
// presentation's runs each state their language, which the structure
// would otherwise lose.
func TestTags_LanguageOfARun(t *testing.T) {
	in := inlineSty()
	lang := func(l, s string) *Node { n := el(in, txt(in, s)); n.Lang = l; return n }
	root := el(sty(), role("P", el(sty(), lang("fr", "bonjour "), lang("EN", "hello "), txt(in, "plain"))))
	root.Role, root.Lang = "Document", "en"
	res := run(t, root, page(400, 100))
	if got := shape(res, res.Structure); got != "Document[P[Span# Span#]]" {
		t.Fatalf("structure %s, want the French run in a Span and the rest in their P", got)
	}
	if sp := res.Structure.Kids[0].Kids[0]; sp.Role != "Span" || sp.Lang != "fr" {
		t.Errorf("first kid %+v, want a Span stating fr", sp)
	}
}

// TestTags_BodyRowsBesideAHeader locks that a Table holding a THead or a
// TFoot holds its other rows in a TBody (ISO 32000-1, 14.8.4.3.4, Table
// 337; ISO 14289-1, 7.2), and that a table of rows alone keeps them.
func TestTags_BodyRowsBesideAHeader(t *testing.T) {
	in := inlineSty()
	cell := func(r string, s string) *Node { return role(r, el(sty(display(css.TableCell)), txt(in, s))) }
	row := func(r, s string) *Node { return role("TR", el(sty(display(css.TableRow)), cell(r, s))) }
	head := role("THead", el(sty(display(css.TableHeaderGroup)), row("TH", "h")))
	for name, tc := range map[string]struct {
		root *Node
		want string
	}{
		"rows after a header": {
			doc(role("Table", el(sty(display(css.Table)), head, row("TD", "a"), row("TD", "b")))),
			"Document[Table[THead[TR[TH#]] TBody[TR[TD#] TR[TD#]]]]",
		},
		"rows alone": {
			doc(role("Table", el(sty(display(css.Table)), row("TD", "a")))),
			"Document[Table[TR[TD#]]]",
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := run(t, tc.root, page(200, 200))
			if got := shape(res, res.Structure); got != tc.want {
				t.Errorf("structure %s, want %s", got, tc.want)
			}
		})
	}
}

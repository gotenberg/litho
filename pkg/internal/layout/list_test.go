package layout

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

func TestMarkerText_Styles(t *testing.T) {
	tests := []struct {
		t    css.ListStyleType
		n    int
		want string
	}{
		{css.Disc, 1, "• "},
		{css.Circle, 1, "◦ "},
		{css.Square, 1, "▪ "},
		{css.Decimal, 12, "12. "},
		{css.DecimalLeadingZero, 7, "07. "},
		{css.DecimalLeadingZero, 12, "12. "},
		{css.LowerRoman, 1994, "mcmxciv. "},
		{css.UpperRoman, 4, "IV. "},
		{css.UpperRoman, 4000, "4000. "},
		{css.LowerAlpha, 1, "a. "},
		{css.LowerAlpha, 27, "aa. "},
		{css.UpperAlpha, 52, "AZ. "},
		{css.LowerGreek, 25, "αα. "},
		{css.Armenian, 1, "Ա. "},
		{css.Armenian, 1984, "ՌՋՁԴ. "},
		{css.Georgian, 11, "ია. "},
		{css.Georgian, 20000, "20000. "},
		{css.LowerAlpha, 0, "0. "},
		{css.Decimal, -3, "-3. "},
		{css.ListNone, 1, ""},
	}
	for _, tt := range tests {
		if got := markerText(tt.t, tt.n); got != tt.want {
			t.Errorf("markerText(%d, %d) = %q, want %q", tt.t, tt.n, got, tt.want)
		}
	}
}

// list returns a list of items of style type lt and position pos, with
// the ListValue of each item given.
func list(lt css.ListStyleType, pos css.ListStylePosition, values ...int) *Node {
	ul := el(sty(padding(0, 0, 0, 40)))
	for _, v := range values {
		st := sty(display(css.ListItem), func(s *css.Style) { s.ListStyleType, s.ListStylePosition = lt, pos })
		li := el(st, txt(st, "x"))
		li.ListValue = v
		li.Role = "LI"
		ul.Children = append(ul.Children, li)
	}
	ul.Role = "L"
	return ul
}

func TestListMarkers_Numbering(t *testing.T) {
	res := run(t, doc(list(css.Decimal, css.Outside, 0, 0, 5, 0)), page(200, 100))
	got, pos := lineTexts(res, 1)
	want := []string{"1. x", "2. x", "5. x", "6. x"}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Fatalf("lines %q, want %q", got, want)
		}
	}
	// The outside marker ends at the item's content edge, 40px in.
	if !near(pos[0][0], 10) {
		t.Errorf("marker at x %v, want 10", pos[0][0])
	}
}

func TestListMarkers_Inside(t *testing.T) {
	res := run(t, doc(list(css.Disc, css.Inside, 0)), page(200, 100))
	got, pos := lineTexts(res, 1)
	if len(got) != 1 || got[0] != "• x" || !near(pos[0][0], 40) {
		t.Errorf("lines %q at %v, want the bullet inside at 40", got, pos)
	}
}

// TestListMarkers_ClippedByTheItemsOverflow locks that the overflow clip
// of a list item cuts its outside marker, a child box of the item that
// lies outside it (CSS Lists 3, 3.1; CSS Overflow 3, 3): of two items
// with square markers, only the first, of overflow visible, shows ink
// left of the content edge.
func TestListMarkers_ClippedByTheItemsOverflow(t *testing.T) {
	ul := list(css.Square, css.Outside, 0, 0)
	ul.Children[1].Style.Overflow = css.OverflowHidden
	img := render(t, run(t, doc(ul), page(100, 60)), 1)
	var inked []int
	for y := range 60 {
		for x := range 40 {
			if code(img.At(x, y)) != '_' {
				inked = append(inked, y)
				break
			}
		}
	}
	if len(inked) == 0 || inked[len(inked)-1] > inked[0]+len(inked)-1 {
		t.Errorf("rows with marker ink %v, want one band: the first item's marker alone", inked)
	}
}

func TestListMarkers_LblLBody(t *testing.T) {
	res := run(t, doc(list(css.Decimal, css.Outside, 0)), page(200, 100))
	l := res.Structure.Kids[0]
	if l.Role != "L" || len(l.Kids) != 1 || l.Kids[0].Role != "LI" {
		t.Fatalf("structure %+v", l)
	}
	li := l.Kids[0]
	if len(li.Kids) != 2 || li.Kids[0].Role != "Lbl" || li.Kids[1].Role != "LBody" {
		t.Fatalf("LI kids %+v, want Lbl then LBody", li.Kids)
	}
	for _, r := range texts(res, 1) {
		want := li.Kids[1]
		if r.text == "1. " {
			want = li.Kids[0]
		}
		if r.run.Parent != want {
			t.Errorf("run %q hangs from %+v", r.text, r.run.Parent)
		}
	}
}

func TestListMarkers_Image(t *testing.T) {
	st := sty(display(css.ListItem), func(s *css.Style) { s.ListStyleImage = "dot.png"; s.ListStyleType = css.Square })
	li := el(st, txt(st, "x"))
	li.Role = "LI"
	root := el(sty(), el(sty(padding(0, 0, 0, 20)), li))
	loader := func(_ context.Context, url string) (*Picture, error) {
		if url != "dot.png" {
			return nil, errors.New("unknown")
		}
		return pic(color.NRGBA{R: 255, A: 255}, 4, 4), nil
	}
	res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(40, 20), Image: loader})
	if err != nil {
		t.Fatal(err)
	}
	if err := document(res).Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	lbl := res.Structure.Kids[0].Kids[0]
	if lbl.Role != "Lbl" || lbl.ActualText != "▪" {
		t.Errorf("Lbl %+v, want the square bullet as its actual text", lbl)
	}
	// The 4px marker image sits on the baseline, 8px down, 5px of gap
	// before the content edge at 20px.
	assertPixels(t, res, 1, "\n"+
		"____________________KKKKKKKKKK__________\n"+
		"____________________KKKKKKKKKK__________\n"+
		"____________________KKKKKKKKKK__________\n"+
		"____________________KKKKKKKKKK__________\n"+
		"___________RRRR_____KKKKKKKKKK__________\n"+
		"___________RRRR_____KKKKKKKKKK__________\n"+
		"___________RRRR_____KKKKKKKKKK__________\n"+
		"___________RRRR_____KKKKKKKKKK__________\n"+
		"____________________KKKKKKKKKK__________\n"+
		"____________________KKKKKKKKKK__________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n"+
		"________________________________________\n")
}

func TestListMarkers_ImageFailureWarns(t *testing.T) {
	st := sty(display(css.ListItem), func(s *css.Style) { s.ListStyleImage = "missing.png" })
	res, err := Layout(context.Background(), doc(el(st, txt(st, "x"))), ahem(t), Options{Page: page(100, 20)})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		found = found || w.Code == warn.PictureUnreadable
	}
	if !found {
		t.Errorf("warnings %+v, want an image failure", res.Warnings)
	}
	if got, _ := lineTexts(res, 1); len(got) != 1 || got[0] != "• x" {
		t.Errorf("lines %q, want the disc marker as fallback", got)
	}
	rb := boxesWith(t, doc(el(st, txt(st, "x"))), Options{Page: page(100, 20), BrowserRules: true})
	if m := kid(t, rb, 0, 0).outside; m == nil || !m.symbol {
		t.Errorf("marker %+v, want the disc shape as fallback under BrowserRules", m)
	}
}

func TestListMarkers_SymbolGeometry(t *testing.T) {
	// Blink's symbol marker: an Ahem ascent of 24px at 30px gives a bullet
	// of (16+1)/2 = 8px, its ink 23px left of the content edge, its top
	// 3*(24-16)/2 = 12px below the ascent's (Chromium 9.0.0, measured on
	// Liberation Serif and Sans at 50 and 100px).
	for _, lt := range []css.ListStyleType{css.Disc, css.Circle, css.Square} {
		ul := list(lt, css.Outside, 0)
		ul.Children[0].Style.FontSize = px(30)
		rb := boxesWith(t, doc(ul), Options{Page: page(200, 100), BrowserRules: true})
		m := kid(t, rb, 0, 0, 0).outside
		if m == nil || !m.symbol {
			t.Fatalf("type %v: marker %+v, want a symbol", lt, m)
		}
		r := [4]float64{m.sym.X / css.Pt, m.sym.Y / css.Pt, m.sym.Width / css.Pt, m.sym.Height / css.Pt}
		if r != [4]float64{17, 12, 8, 8} {
			t.Errorf("type %v: bullet at %v px, want [17 12 8 8]", lt, r)
		}
	}
}

func TestListMarkers_SymbolMovesWithItsBox(t *testing.T) {
	// A list inside an inline-block is laid out, then moved to its place
	// on the line: the bullet moves with the item (CSS 2.1, 12.5).
	ul := list(css.Square, css.Outside, 0)
	ib := el(sty(display(css.InlineBlock)), ul)
	rb := boxesWith(t, doc(el(sty(), txt(sty(), "xxxx"), ib)), Options{Page: page(200, 100), BrowserRules: true})
	var li *box
	var find func(b *box)
	find = func(b *box) {
		if b.outside != nil && li == nil {
			li = b
		}
		for _, k := range b.kids {
			find(k)
		}
		for _, l := range b.lines {
			for _, it := range l.items {
				if it.atomic != nil {
					find(it.atomic)
				}
			}
		}
	}
	find(rb)
	if li == nil {
		t.Fatal("no list item with an outside marker")
	}
	m := li.outside
	if !m.symbol {
		t.Fatalf("marker %+v, want a symbol", m)
	}
	if dx, dy := (li.contentX()-m.sym.X)/css.Pt, (m.sym.Y-li.contentY())/css.Pt; dx <= 0 || dx > 20 || dy < 0 || dy > 10 {
		t.Errorf("bullet %v px left of and %v px below the item's content box, want beside its first line", dx, dy)
	}
}

func TestListMarkersDraw_Square(t *testing.T) {
	// At 10px: ascent 8px, a 3px square 12px left of the content edge at
	// 40px, its top 4px below the line's.
	if m := kid(t, boxes(t, doc(list(css.Square, css.Outside, 0)), page(40, 8)), 0, 0, 0).outside; m == nil || m.symbol {
		t.Errorf("marker %+v, want text without BrowserRules", m)
	}
	assertPixels(t, runWith(t, doc(list(css.Square, css.Outside, 0)), Options{Page: page(40, 8), BrowserRules: true}), 1, `
		________________________________________
		________________________________________
		________________________________________
		________________________________________
		____________________________KKK_________
		____________________________KKK_________
		____________________________KKK_________
		________________________________________`)
}

// TestListItems_HoldLblAndLBody locks the structure of list items: an LI
// holds a Lbl and a LBody alone (ISO 32000-2, 14.8.4.8.3; ISO/TS 32005,
// Table 5, which PDF/UA-2 tests), its content in the LBody whether a
// marker draws or not.
//
// # Corpus evidence
//
//   - probe, html: list items with list-style none, display block or flex
//     left an LI holding a Link or a Span after PDF/UA-2 remediation; none
//     with this structure, which veraPDF passes at PDF/UA-1 and PDF/UA-2.
//
// TestListMarkersDraw_InsideSymbol locks the inside symbol marker under
// BrowserRules: at 10px Ahem, ascent 8px, a 3px square at the content edge,
// 40px in, its top 4px below the line's, and the text after the bullet, 1em
// and a pixel, at 54px, whether the item's content starts with a space or
// not, as Chromium draws it (symbolMarkerOf).
func TestListMarkersDraw_InsideSymbol(t *testing.T) {
	for _, text := range []string{"x", " x"} {
		ul := list(css.Square, css.Inside, 0)
		li := ul.Children[0]
		li.Children = []*Node{txt(li.Style, text)}
		assertPixels(t, runWith(t, doc(ul), Options{Page: page(66, 10), BrowserRules: true}), 1, `
			______________________________________________________KKKKKKKKKK__
			______________________________________________________KKKKKKKKKK__
			______________________________________________________KKKKKKKKKK__
			______________________________________________________KKKKKKKKKK__
			________________________________________KKK___________KKKKKKKKKK__
			________________________________________KKK___________KKKKKKKKKK__
			________________________________________KKK___________KKKKKKKKKK__
			______________________________________________________KKKKKKKKKK__
			______________________________________________________KKKKKKKKKK__
			______________________________________________________KKKKKKKKKK__`)
	}
}

func TestListItems_HoldLblAndLBody(t *testing.T) {
	in := inlineSty()
	item := func(lt css.ListStyleType, kids ...*Node) *Node {
		return role("LI", el(sty(display(css.ListItem), func(s *css.Style) { s.ListStyleType = lt }), kids...))
	}
	for name, tc := range map[string]struct {
		root *Node
		want string
	}{
		"item with a marker": {
			doc(role("L", el(sty(), item(css.Disc, txt(in, "a"))))),
			"Document[L[LI[Lbl# LBody#]]]",
		},
		"item with no marker": {
			doc(role("L", el(sty(), item(css.ListNone, txt(in, "a"))))),
			"Document[L[LI[LBody#]]]",
		},
		"item holding a link and no marker": {
			doc(role("L", el(sty(), item(css.ListNone, role("Link", el(in, txt(in, "a"))))))),
			"Document[L[LI[LBody[Link#]]]]",
		},
		"item holding paragraphs and a list": {
			doc(role("L", el(sty(), item(css.Decimal, role("P", el(sty(), txt(in, "a"))), role("L", el(sty(), item(css.Disc, txt(in, "b")))))))),
			"Document[L[LI[Lbl# LBody[P# L[LI[Lbl# LBody#]]]]]]",
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := run(t, tc.root, page(200, 200))
			if got := shape(res, res.Structure); got != tc.want {
				t.Errorf("structure %s, want %s", got, tc.want)
			}
			if err := document(res).Validate(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestListItems_GroupDefinitions locks the structure of a definition list:
// its terms (Lbl) and descriptions (LBody) group into one LI per run of
// terms and the descriptions after them (ISO 32000-1, 14.8.4.3.3, Table
// 336; HTML Standard, 4.4.9).
//
// # Corpus evidence
//
//   - litho/html markdown-definition-list: each term and each description
//     was an LI of its own, the description's holding a P, six Table 5
//     LI-P violations PDF/UA-2 remediation left.
//   - probe, html: several terms or descriptions to a group, a description
//     with no term, div-wrapped groups, a nested definition list and a
//     term and description outside a list left 2 PDF/UA-1 and 8 PDF/UA-2
//     violations after remediation (LI holding L, Span or Link, L holding
//     Div, Document holding LI), and none with this structure; veraPDF
//     passes PDF/UA-1 and PDF/UA-2 on the output.
//   - probe, html: a term holding a p and a div gave Lbl > P and Lbl > Div,
//     which veraPDF fails at PDF/UA-2 (ISO/TS 32005, Table 5, Lbl-P and
//     Lbl-Div) and passes at PDF/UA-1; the term's blocks starting no
//     element pass both.
func TestListItems_GroupDefinitions(t *testing.T) {
	in := inlineSty()
	part := func(r, s string) *Node { return role(r, el(sty(), txt(in, s))) }
	for name, tc := range map[string]struct {
		root *Node
		want string
	}{
		"definitions grouped by term": {
			doc(role("L", el(sty(), part("Lbl", "t1"), part("Lbl", "t2"), part("LBody", "d1"), part("LBody", "d2"), part("Lbl", "t3"), part("LBody", "d3")))),
			"Document[L[LI[Lbl# Lbl# LBody# LBody#] LI[Lbl# LBody#]]]",
		},
		"description with no term": {
			doc(role("L", el(sty(), part("LBody", "d1"), part("Lbl", "t2"), part("LBody", "d2")))),
			"Document[L[LI[LBody#] LI[Lbl# LBody#]]]",
		},
		"groups wrapped in blocks with no role": {
			doc(role("L", el(sty(), el(sty(), part("Lbl", "t1"), part("LBody", "d1")), el(sty(), part("Lbl", "t2"), part("LBody", "d2"))))),
			"Document[L[LI[Lbl# LBody#] LI[Lbl# LBody#]]]",
		},
		"description holding a list": {
			doc(role("L", el(sty(), part("Lbl", "t"), role("LBody", el(sty(), role("L", el(sty(), part("Lbl", "u"), part("LBody", "e")))))))),
			"Document[L[LI[Lbl# LBody[L[LI[Lbl# LBody#]]]]]]",
		},
		"term holding paragraphs and a span": {
			doc(role("L", el(sty(), role("Lbl", el(sty(), part("P", "t"), role("Span", el(in, txt(in, "u"), part("Div", "v"))))), part("LBody", "d")))),
			"Document[L[LI[Lbl[Span# Span#] LBody#]]]",
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := run(t, tc.root, page(200, 200))
			if got := shape(res, res.Structure); got != tc.want {
				t.Errorf("structure %s, want %s", got, tc.want)
			}
			if err := document(res).Validate(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestListMarkers_EmptyItemTakesTheMarkerLine(t *testing.T) {
	// CSS Lists 3, 3.1 leaves the place of a marker without a line box to
	// the user agent. As Chromium does, the marker of an empty item sits
	// in a line of its own at the item's top, and the item is that line
	// tall: its margins do not collapse through it. An item whose
	// marker draws nothing stays empty.
	lh := func(s *css.Style) { s.LineHeight = css.Pts(px(40)) }
	item := func(lt css.ListStyleType, kids ...*Node) *Node {
		return el(sty(lh, display(css.ListItem), func(s *css.Style) { s.ListStyleType = lt }), kids...)
	}
	ul := el(sty(lh, padding(0, 0, 0, 40)), item(css.Square), item(css.Square, el(sty(height(5)))), item(css.ListNone))
	rb := boxesWith(t, doc(ul), Options{Page: page(200, 200), BrowserRules: true})
	for i, want := range []float64{40, 40, 0} {
		if b := kid(t, rb, 0, 0, i); !near(b.h/css.Pt, want) {
			t.Errorf("item %d: height %v, want %v", i, b.h/css.Pt, want)
		}
	}
	if m := kid(t, rb, 0, 0, 0).outside; m == nil || !m.placed {
		t.Errorf("marker %+v, want placed", m)
	}
}

func TestListMarkers_CascadedMarkerStyle(t *testing.T) {
	// CSS Lists 3, 3.1.1: the text of a ::marker takes the inherited
	// properties of its cascaded style. Its white-space of pre keeps its
	// spaces and, as Chromium lays it out, wraps; text-transform applies
	// to an outside marker as to an inside one.
	marker := func(mods ...func(*css.Style)) *Node {
		return &Node{Kind: Element, Style: sty(append([]func(*css.Style){func(s *css.Style) {
			s.WhiteSpace = css.Pre
			s.ListStyleType, s.ListStyleString = css.ListString, "ab cd"
		}}, mods...)...)}
	}
	item := func(pos css.ListStylePosition, m *Node) *Node {
		st := sty(display(css.ListItem), width(0), func(s *css.Style) { s.ListStylePosition = pos })
		li := el(st)
		li.Marker = m
		return li
	}
	res := runWith(t, doc(item(css.Inside, marker())), Options{Page: page(100, 100), BrowserRules: true})
	if got, _ := lineTexts(res, 1); len(got) != 2 || got[0] != "ab " || got[1] != "cd" {
		t.Errorf("inside marker lines %q, want ab |cd", got)
	}
	upper := marker(func(s *css.Style) { s.TextTransform = css.Uppercase })
	rb := boxesWith(t, doc(item(css.Outside, upper)), Options{Page: page(100, 100), BrowserRules: true})
	if m := kid(t, rb, 0, 0).outside; m == nil || m.text != "AB CD" {
		t.Errorf("outside marker %+v, want the text AB CD", m)
	}
}

func TestListMarkers_OutsideTheBorderBox(t *testing.T) {
	// CSS 2.1, 12.5.1: an outside marker box lies outside the principal
	// block box. Under BrowserRules it ends at the item's border edge, as
	// Chromium draws it, whatever the item's padding and border.
	st := sty(display(css.ListItem), padding(0, 0, 0, 30), func(s *css.Style) {
		s.ListStyleType, s.ListStyleString = css.ListString, "X"
		s.Border[css.Left] = css.Border{Width: px(10), Style: css.Solid, Color: black}
	})
	rb := boxesWith(t, doc(el(sty(padding(0, 0, 0, 50)), el(st, txt(st, "x")))), Options{Page: page(200, 100), BrowserRules: true})
	li := kid(t, rb, 0, 0, 0)
	if m := li.outside; m == nil || !near((m.x+m.width-li.x)/css.Pt, 0) {
		t.Errorf("marker %+v, want its end on the border edge at %v", m, li.x/css.Pt)
	}
}

// TestListMarkers_MarkerWidthSizesNothing locks that under BrowserRules a
// ::marker's width does not make a marker box: CSS Pseudo 4, 3.2 lists no
// box property among those a ::marker takes, and the outside marker ends
// on the item's border edge.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 ends an outside
//     ::marker of width 100px, or of display inline-block and width
//     100px, at the item's border edge, and paints no background, margin,
//     padding or border of it.
func TestListMarkers_MarkerWidthSizesNothing(t *testing.T) {
	st := sty(display(css.ListItem), func(s *css.Style) { s.ListStylePosition = css.Outside })
	mst := sty(func(s *css.Style) { s.Width = css.Pts(px(100)) })
	li := el(st, txt(st, "x"))
	li.Marker = el(mst, txt(mst, "X"))
	rb := boxesWith(t, doc(el(sty(padding(0, 0, 0, 150)), li)), Options{Page: page(300, 100), BrowserRules: true})
	item := kid(t, rb, 0, 0, 0)
	if m := item.outside; m == nil || !near((m.x+m.width-item.x)/css.Pt, 0) {
		t.Errorf("marker %+v, want its end on the border edge at %v", m, item.x/css.Pt)
	}
}

// TestListMarkers_DisclosureGeometry locks Blink's disclosure triangles
// (CSS Counter Styles 3, 6.2 leaves their look to the user agent): a
// square of 0.66em on the baseline, an outside one ending 8px before the
// item's border edge, an inside one followed by 0.4em. At 100px: a 66px
// square from -34px, and inside text at 40 + 66 + 40 = 146px.
func TestListMarkers_DisclosureGeometry(t *testing.T) {
	for _, lt := range []css.ListStyleType{css.DisclosureOpen, css.DisclosureClosed} {
		ul := list(lt, css.Outside, 0)
		ul.Children[0].Style.FontSize = px(100)
		rb := boxesWith(t, doc(ul), Options{Page: page(300, 200), BrowserRules: true})
		m := kid(t, rb, 0, 0, 0).outside
		if m == nil || !m.symbol {
			t.Fatalf("type %v: marker %+v, want a symbol", lt, m)
		}
		r := [4]float64{m.sym.X / css.Pt, (m.baseline - m.sym.Y) / css.Pt, m.sym.Width / css.Pt, m.sym.Height / css.Pt}
		if r != [4]float64{-34, 66, 66, 66} {
			t.Errorf("type %v: triangle square %v px (x, height above the baseline, size), want [-34 66 66 66]", lt, r)
		}
		in := list(lt, css.Inside, 0)
		in.Children[0].Style.FontSize = px(100)
		if got := glyphLayout(runWith(t, doc(in), Options{Page: page(300, 200), BrowserRules: true}), 1); !strings.HasPrefix(got, "x@146,") {
			t.Errorf("type %v: inside glyphs %s, want x at 146px", lt, got)
		}
	}
}

// TestListMarkers_MarkerDirectionOrdersItsText locks that under
// BrowserRules an outside marker's text is ordered in the ::marker's own
// direction (CSS Pseudo 4, 3.2 lists direction among the properties a
// ::marker takes): "2. " in a right-to-left ::marker of a left-to-right
// item reads " .2", the full stop left of the number.
//
// # Corpus evidence
//
//   - wpt: css/css-pseudo/marker-content-023 and marker-text-align-001,
//     in the screenshot of the Chromium of gotenberg/gotenberg:9.4.1,
//     draw the decimal marker of a left-to-right item whose ::marker is
//     direction: rtl as " .2", ending at the item's edge.
func TestListMarkers_MarkerDirectionOrdersItsText(t *testing.T) {
	st := sty(display(css.ListItem), func(s *css.Style) { s.ListStylePosition = css.Outside })
	mst := sty(func(s *css.Style) { s.Direction = css.RTL; s.WhiteSpace = css.Pre })
	li := el(st, txt(st, "x"))
	li.Marker = el(mst, txt(mst, "2."))
	res := runWith(t, doc(el(sty(padding(0, 0, 0, 50)), li)), Options{Page: page(200, 50), BrowserRules: true})
	got := glyphLayout(res, 1)
	dot, two := strings.Index(got, ".@"), strings.Index(got, "2@")
	if dot < 0 || two < 0 || dot > two {
		t.Errorf("glyphs %s, want the full stop before the 2", got)
	}
}

// TestListMarkers_OutsideMarkerKeepsRoomAbove locks that under
// BrowserRules the first line of a list item, its own or a nested
// block's, keeps above its baseline the height an outside marker's line
// takes there, and nothing more below it (CSS Lists 3, 3.1 leaves it to
// the user agent; this is Chromium's), while the Office rules leave the
// line as its content sets it.
func TestListMarkers_OutsideMarkerKeepsRoomAbove(t *testing.T) {
	big := func(s *css.Style) { s.FontSize = px(40) }
	for _, tc := range []struct {
		name    string
		nested  bool
		browser bool
		want    float64
	}{
		{"own line", false, true, 34},
		{"nested block", true, true, 34},
		{"office rules", false, false, 10},
	} {
		st := sty(display(css.ListItem), func(s *css.Style) { s.ListStylePosition = css.Outside })
		li := el(st, txt(st, "x"))
		if tc.nested {
			li = el(st, el(sty(), txt(sty(), "x")))
		}
		li.Marker = el(sty(big, display(css.ListItem), func(s *css.Style) { s.ListStylePosition = css.Outside }))
		rb := boxesWith(t, doc(el(sty(padding(0, 0, 0, 50)), li)), Options{Page: page(200, 100), BrowserRules: tc.browser})
		item := kid(t, rb, 0, 0, 0)
		if got := item.borderH() / css.Pt; !near(got, tc.want) {
			t.Errorf("%s: item %vpx tall, want %v", tc.name, got, tc.want)
		}
	}
}

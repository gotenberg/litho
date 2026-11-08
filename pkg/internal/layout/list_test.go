package layout

import (
	"context"
	"errors"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
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
	if err := document(res).Validate(); err != nil {
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
		found = found || w.Code == codeImage
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

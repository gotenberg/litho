package html

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestDocumentMode_FollowsTheDoctype holds the document mode to the
// conditions of the HTML Standard, 13.2.6.4.1, and the DOCTYPE states of its
// tokenizer, 13.2.5.53 to 13.2.5.68.
func TestDocumentMode_FollowsTheDoctype(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want css.DocumentMode
	}{
		{`<!DOCTYPE html><p>`, css.NoQuirks},
		{`<!doctype HTML>`, css.NoQuirks},
		{"\n <!-- c --> <!DOCTYPE html>", css.NoQuirks},
		{`<!DOCTYPE html SYSTEM "about:legacy-compat">`, css.NoQuirks},
		{`<!DOCTYPE html SYSTEM "about:legacy-compat" junk>`, css.NoQuirks},
		{`<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">`, css.NoQuirks},
		{`<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">`, css.NoQuirks},
		{`<p>no doctype`, css.Quirks},
		{``, css.Quirks},
		{`x<!DOCTYPE html>`, css.Quirks},
		{`<!DOCTYPE>`, css.Quirks},
		{`<!DOCTYPE svg>`, css.Quirks},
		{`<!DOCTYPE html bogus>`, css.Quirks},
		{`<!DOCTYPE html PUBLIC>`, css.Quirks},
		{`<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN`, css.Quirks},
		{`<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.0 Transitional//EN">`, css.Quirks},
		{`<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN">`, css.Quirks},
		{`<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "">`, css.Quirks},
		{`<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 3.2 Final//EN">`, css.Quirks},
		{`<!DOCTYPE html PUBLIC "html">`, css.Quirks},
		{`<!DOCTYPE html SYSTEM "http://www.ibm.com/data/dtd/v11/ibmxhtml1-transitional.dtd">`, css.Quirks},
		{`<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">`, css.LimitedQuirks},
		{`<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Frameset//EN" "http://www.w3.org/TR/html4/frameset.dtd">`, css.LimitedQuirks},
		{`<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">`, css.LimitedQuirks},
		{`<!DOCTYPE html PUBLIC '-//W3C//DTD XHTML 1.0 Frameset//EN'>`, css.LimitedQuirks},
	} {
		if got := documentMode(tc.src); got != tc.want {
			t.Errorf("documentMode(%q) = %d, want %d", tc.src, got, tc.want)
		}
	}
}

// TestQuirksMode_UserAgentRules holds a document in quirks mode to the rules
// the HTML Standard's Rendering section adds in that mode alone: a table
// takes its font size, weight, style, line height, white space and
// alignment from their initial values (15.3.8), a form a margin under it
// (15.3.3), a text field box-sizing: border-box (15.3.10) and an aligned
// picture 3 pixels of margin (15.4.3); a document with an HTML5 DOCTYPE
// takes none of them.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 computes these
//     values in quirks mode and not with <!DOCTYPE html>, on screen and in
//     print, and keeps li { list-style-position: outside }.
func TestQuirksMode_UserAgentRules(t *testing.T) {
	const body = `<body><div style="font: italic bold 30px/3 serif; white-space: pre; text-align: center"><table id=t><tr><td>x</td></tr></table></div>` +
		`<form id=f>f</form><input id=i><img id=a align=LEFT><ul><li id=l>x</li></ul>`
	for _, quirks := range []bool{true, false} {
		src := body
		if !quirks {
			src = "<!DOCTYPE html>" + body
		}
		p := prep(t, src, Options{})
		tbl := byID(t, p.root, "t").Style
		form := byID(t, p.root, "f").Style
		input := byID(t, p.root, "i").Style
		img := byID(t, p.root, "a").Style
		li := byID(t, p.root, "l").Style
		if got := tbl.FontSize == 12 && tbl.FontWeight == 400 && tbl.FontStyle == css.FontNormal && tbl.LineHeight.Kind == css.Normal && tbl.WhiteSpace == css.WhiteSpaceNormal; got != quirks {
			t.Errorf("quirks %v: table font %v %v %v, line height %v, white space %v", quirks, tbl.FontSize, tbl.FontWeight, tbl.FontStyle, tbl.LineHeight, tbl.WhiteSpace)
		}
		if got := form.Margin[css.Bottom].Value == 12; got != quirks {
			t.Errorf("quirks %v: form margin-bottom %v", quirks, form.Margin[css.Bottom])
		}
		if got := input.BoxSizing == css.BorderBox; got != quirks {
			t.Errorf("quirks %v: input box-sizing %v", quirks, input.BoxSizing)
		}
		if got := img.Margin[css.Right].Value == 2.25; got != quirks {
			t.Errorf("quirks %v: img margin-right %v", quirks, img.Margin[css.Right])
		}
		if li.ListStylePosition != css.Outside {
			t.Errorf("quirks %v: li list-style-position %v", quirks, li.ListStylePosition)
		}
	}
}

// TestQuirksMode_TablesTakeTheBodyColour gives a table of a document in
// quirks mode the colour of the body, not its parent's (Quirks Mode
// Standard, 3.12); color: inherit still inherits, and a document with an
// HTML5 DOCTYPE inherits as usual.
func TestQuirksMode_TablesTakeTheBodyColour(t *testing.T) {
	const body = `<body text=blue><font color=red><table id=t><tr><td>x</td></tr></table>` +
		`<table id=i style="color: inherit"><tr><td>x</td></tr></table></font></body>`
	blue, red := css.Color{B: 1, A: 1}, css.Color{R: 1, A: 1}
	for _, quirks := range []bool{true, false} {
		src := body
		if !quirks {
			src = "<!DOCTYPE html>" + body
		}
		p := prep(t, src, Options{})
		want := red
		if quirks {
			want = blue
		}
		if got := byID(t, p.root, "t").Style.Color; got != want {
			t.Errorf("quirks %v: table colour %v, want %v", quirks, got, want)
		}
		if got := byID(t, p.root, "i").Style.Color; got != red {
			t.Errorf("quirks %v: inheriting table colour %v", quirks, got)
		}
	}
}

// TestQuirksMode_MarksQuirkyMargins marks the body, td and th elements of a
// document in quirks mode as quirky containers and the margins the user
// agent sheet gives the elements of HTML Standard, 15.3.3 as quirky: both
// of a p, the top one of an h1, none an author sets; a document with an
// HTML5 DOCTYPE has none.
func TestQuirksMode_MarksQuirkyMargins(t *testing.T) {
	const body = `<body id=b><table><tr><td id=c><p id=p>a</p><h1 id=h>b</h1><p id=a style="margin-top: 1em">c</p><figure id=f>d</figure></td></tr></table></body>`
	for _, quirks := range []bool{true, false} {
		src := body
		if !quirks {
			src = "<!DOCTYPE html>" + body
		}
		p := prep(t, src, Options{})
		want := map[string]layout.MarginQuirks{
			"b": layout.QuirkyContainer, "c": layout.QuirkyContainer, "p": layout.QuirkyTop | layout.QuirkyBottom,
			"h": layout.QuirkyTop, "a": layout.QuirkyBottom, "f": 0,
		}
		for id, w := range want {
			if !quirks {
				w = 0
			}
			if got := byID(t, p.root, id).MarginQuirks; got != w {
				t.Errorf("quirks %v: #%s margin quirks %v, want %v", quirks, id, got, w)
			}
		}
	}
}

package html

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestFirstLetterSpan locks the characters ::first-letter takes (CSS
// Pseudo 4, 2.3.2) as Chromium takes them: the white space before it that
// stays, every punctuation category and the spaces between before the
// letter, the letter's grapheme cluster, then Ps, Pe, Pi, Pf, Po and Pc,
// through spaces other than U+0020; and no letter past a line break that
// stays.
func TestFirstLetterSpan(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in        string
		pre       int
		letter    string
		punctOnly bool
	}{
		{"Test", 0, "T", false},
		{"  Test", 2, "T", false},
		{"\u00a0nbsp", 0, "\u00a0n", false},
		{"❱T❱est", 0, "❱T❱", false},
		{"\"  quoted", 0, "\"  q", false},
		{"-hyphen", 0, "-h", false},
		{"_low", 0, "_l", false},
		{"—dash", 0, "—d", false},
		{"T- after", 0, "T", false},
		{"T_ after", 0, "T_", false},
		{"T . after", 0, "T", false},
		{"T . after", 0, "T .", false},
		{"T x", 0, "T", false},
		{"+plus", 0, "+", false},
		{"éacute", 0, "é", false},
		{"\U0001F1EC\U0001F1E7 UK", 0, "\U0001F1EC\U0001F1E7", false},
		{"(", 0, "(", true},
		{" “ ", 1, "“", true},
	} {
		pre, n, punctOnly, _ := firstLetterSpan(tc.in, css.WhiteSpaceNormal)
		if pre != tc.pre || n < 0 || tc.in[pre:n] != tc.letter || punctOnly != tc.punctOnly {
			t.Errorf("firstLetterSpan(%q) = %d, %d, %v: want the letter %q after %d bytes, punctuation alone %v",
				tc.in, pre, n, punctOnly, tc.letter, tc.pre, tc.punctOnly)
		}
	}
	if _, n, _, _ := firstLetterSpan(" \n\t", css.WhiteSpaceNormal); n != -1 {
		t.Errorf("white space alone: n = %d, want -1", n)
	}
	for _, tc := range []struct {
		in     string
		ws     css.WhiteSpace
		pre    int
		letter string
		ended  bool
	}{
		{"  Abc", css.Pre, 0, "  A", false},
		{"\tAbc", css.PreWrap, 0, "\tA", false},
		{"  Abc", css.PreLine, 2, "A", false},
		{"\nAbc", css.Pre, 0, "", true},
		{"\nAbc", css.PreLine, 0, "", true},
		{"\nAbc", css.WhiteSpaceNormal, 1, "A", false},
		{"\" \nAbc", css.PreWrap, 0, "", true},
	} {
		pre, n, _, ended := firstLetterSpan(tc.in, tc.ws)
		got := ""
		if n >= 0 {
			got = tc.in[pre:n]
		}
		if ended != tc.ended || got != tc.letter || !ended && pre != tc.pre {
			t.Errorf("firstLetterSpan(%q, %d) = %d, %d, ended %v: want %q after %d bytes, ended %v", tc.in, tc.ws, pre, n, ended, tc.letter, tc.pre, tc.ended)
		}
	}
}

// firstLetterOf returns the first-letter box under n, nil for none: the
// first element drawn in colour, as the test's rules draw it.
func firstLetterOf(n *layout.Node) *layout.Node {
	var out *layout.Node
	eachNode(n, func(c *layout.Node) {
		if out == nil && c.Kind == layout.Element && c.Style != nil && (c.Style.Color.G > 0 || c.Style.Color.B > 0) {
			out = c
		}
	})
	return out
}

func TestFirstLetter_Cut(t *testing.T) {
	t.Parallel()
	p := prep(t, `<style>p::first-letter, li::first-letter, td::first-letter { color: green; font-size: 2em; position: relative }
		.f::first-letter { float: left } .b::before { content: "(" } .im::before { content: url(x.png) }
		div::first-letter { color: blue }</style>
		<p id=span><span style="font-style: italic">Span</span> text</p>
		<p id=f class=f>Drop cap</p>
		<p id=b class=b>before</p>
		<p id=dash>&#x2014;</p>
		<p id=br><br>after</p>
		<p id=ib><span style="display: inline-block">ib</span> after</p>
		<p id=fl><span style="float: right">floated</span> Flow</p>
		<p id=ct><span style="display: contents; float: right"><b style="display: contents">Contents</b></span> after</p>
		<ol><li id=li>item</ol>
		<table><tr><td id=td>cell</table>
		<div id=div><p id=inner>nested</p></div>
		<div id=outer><section><h2 id=deep>Deep</h2></section></div>`, Options{})
	for id, want := range map[string]string{
		"span": "S", "f": "D", "b": "(", "li": "i", "td": "c", "inner": "n", "deep": "D", "fl": "F",
		// An element of display: contents has no box: its float applies to
		// nothing, and its text is on the first line (CSS Display 3, 2.5).
		"ct": "C",
	} {
		n := byID(t, p.root, id)
		fl := firstLetterOf(n)
		if fl == nil || treeText(fl) != want {
			t.Errorf("#%s: first letter %v, want %q", id, fl, want)
			continue
		}
		if fl.Style.Position != css.Static {
			t.Errorf("#%s: first letter positioned", id)
		}
	}
	for _, id := range []string{"dash", "br", "ib"} {
		if fl := firstLetterOf(byID(t, p.root, id)); fl != nil {
			t.Errorf("#%s: first letter %q, want none", id, treeText(fl))
		}
	}
	if got := treeText(byID(t, p.root, "span")); got != "Span text" {
		t.Errorf("#span text %q, want the text whole", got)
	}
	span := firstLetterOf(byID(t, p.root, "span"))
	if st := span.Style; st.FontStyle != css.FontItalic || st.Display != css.Inline {
		t.Errorf("first letter in an italic span: style %v display %v, want inherited italic, inline", st.FontStyle, st.Display)
	}
	if f := firstLetterOf(byID(t, p.root, "f")); f.Style.Float != css.FloatLeft || f.Style.Display != css.Block {
		t.Errorf("floated first letter: float %v display %v", f.Style.Float, f.Style.Display)
	}
	// The inner block's own ::first-letter is the one that applies.
	if in := firstLetterOf(byID(t, p.root, "div")); in == nil || in.Style.Color.B > 0 {
		t.Errorf("nested first letter %v, want the inner p's", in)
	}
	if deep := firstLetterOf(byID(t, p.root, "outer")); deep == nil || deep.Style.Color.B == 0 {
		t.Errorf("first letter of a descendant block %v, want the outer div's", deep)
	}
}

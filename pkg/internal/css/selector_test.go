package css

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const selectorDoc = `<!doctype html><html lang="en"><body>
<div id="d1" class="a b" title="hello world" data-x="en-US">
  <p id="p1">first</p>
  <p id="p2" class="b">second</p>
  <span id="s1"></span>
  <p id="p3" lang="fr-CA">third <!-- c --></p>
  <p id="p4"><!-- only a comment --></p>
</div>
<ul id="list"><li id="l1">1</li><li id="l2">2</li><li id="l3">3</li><li id="l4">4</li><li id="l5">5</li></ul>
<a id="link" href="/x">x</a><a id="anchor" name="y">y</a>
<form><input id="cb" type="checkbox" checked><input id="dis" disabled><fieldset disabled><legend><input id="inleg"></legend><input id="infs"></fieldset></form>
<svg id="svg"><rect id="rect"/></svg>
</body></html>`

// matching returns the ids of the elements sel matches, in document order.
func matching(t *testing.T, doc *html.Node, sel string) []string {
	t.Helper()
	l, _ := parseComponentValues(sel)
	list, err := parseSelectorList(trimWS(l), 0, false)
	if err != nil {
		t.Fatalf("%s: does not parse (%q)", sel, err.unsupported)
	}
	m := newMatcher("", maxComputeSteps)
	var out []string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && m.anyMatches(list, n) {
			if id, ok := attr(n, "id"); ok {
				out = append(out, id)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

func TestSelector_Matches(t *testing.T) {
	t.Parallel()
	doc := parseHTML(t, selectorDoc)
	tests := []struct{ sel, want string }{
		{"p", "p1 p2 p3 p4"},
		{"*.b", "d1 p2"},
		{".a.b", "d1"},
		{"#p2.b", "p2"},
		{"div > p", "p1 p2 p3 p4"},
		{"body p", "p1 p2 p3 p4"},
		{"p + p", "p2 p4"},
		{"p ~ p", "p2 p3 p4"},
		{"p ~ span", "s1"},
		{"[title]", "d1"},
		{"[title='hello world']", "d1"},
		{"[title~=world]", "d1"},
		{"[data-x|=en]", "d1"},
		{"[title^=hel]", "d1"},
		{"[title$=rld]", "d1"},
		{"[title*='o w']", "d1"},
		{"[title='HELLO WORLD' i]", "d1"},
		{"[title='HELLO WORLD']", ""},
		{"input[type=CHECKBOX]", "cb"},
		{"input[type=CHECKBOX s]", ""},
		{"li:first-child", "l1"},
		{"li:last-child", "l5"},
		{"span:only-of-type", "s1"},
		{"p:first-of-type", "p1"},
		{"p:last-of-type", "p4"},
		{"li:nth-child(2n+1)", "l1 l3 l5"},
		{"li:nth-child(even)", "l2 l4"},
		{"li:nth-child(-n+2)", "l1 l2"},
		{"li:nth-last-child(1)", "l5"},
		{"p:nth-of-type(2)", "p2"},
		{"p:nth-last-of-type(2)", "p3"},
		{"div :nth-child(2 of .b)", ""},
		{"div :nth-child(1 of .b)", "p2"},
		{"#d1 :empty", "s1 p4"},
		{"li:not(:first-child):not(:last-child)", "l2 l3 l4"},
		{":is(#p1, #p3)", "p1 p3"},
		{":where(#p1, :unknown-thing)", "p1"},
		{"a:link", "link"},
		{"a:any-link", "link"},
		{"a:hover, a:visited, a:focus, a:active", ""},
		{"p:lang(fr)", "p3"},
		{"p:lang(en)", "p1 p2 p4"},
		{":checked", "cb"},
		{"input:disabled", "dis infs"},
		{"input:enabled", "cb inleg"},
		{":root", ""},
		{"svg rect", "rect"},
		{"ns|p, *|p", ""},
		{"p::before", ""},
		{"li:nth-child(n+6)", ""},
	}
	for _, tt := range tests {
		got := strings.Join(matching(t, doc, tt.sel), " ")
		if got != tt.want {
			t.Errorf("%s matches %q, want %q", tt.sel, got, tt.want)
		}
	}
	if got := matching(t, doc, ":root"); got != nil {
		t.Errorf(":root matched %v; the root has no id", got)
	}
	if html := byTag(doc, "html"); !newMatcher("", maxComputeSteps).anyMatches(mustSelectors(t, ":root"), html) {
		t.Errorf(":root does not match <html>")
	}
}

func mustSelectors(t *testing.T, sel string) []*complexSel {
	t.Helper()
	l, _ := parseComponentValues(sel)
	list, err := parseSelectorList(trimWS(l), 0, false)
	if err != nil {
		t.Fatalf("%s: does not parse", sel)
	}
	return list
}

func TestSelector_Invalid(t *testing.T) {
	t.Parallel()
	for _, sel := range []string{
		"", "p,", ",p", "p >", "> p", "#1a", ".", "p..q", "[", "[a=]", "[a=b c]", "p:unknown",
		":nth-child(foo)", "::before p", "::before.x", ":not(::before)", "p:has(a)",
		"a || b", "p::unknown", ":not()", ":lang()",
		strings.Repeat(":not(", 10) + "p" + strings.Repeat(")", 10),
		strings.Repeat("a ", 40) + "b",
	} {
		l, _ := parseComponentValues(sel)
		if _, err := parseSelectorList(trimWS(l), 0, false); err == nil {
			t.Errorf("%q parses", sel)
		}
	}
	l, _ := parseComponentValues("a:has(b)")
	if _, err := parseSelectorList(l, 0, false); err == nil || err.unsupported != ":has()" {
		t.Errorf("a:has(b) error = %+v, want unsupported :has()", err)
	}
}

func TestSelector_Specificity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		sel     string
		a, b, c int
	}{
		{"*", 0, 0, 0},
		{"li", 0, 0, 1},
		{"ul li", 0, 0, 2},
		{"ul ol+li", 0, 0, 3},
		{"h1 + *[rel=up]", 0, 1, 1},
		{"ul ol li.red", 0, 1, 3},
		{"li.red.level", 0, 2, 1},
		{"#x34y", 1, 0, 0},
		{"#s12:not(FOO)", 1, 0, 1},
		{".foo :is(.bar, #baz)", 1, 1, 0},
		{":where(#a, .b) p", 0, 0, 1},
		{"p::before", 0, 0, 2},
		{"li:nth-child(2n of .x, #y)", 1, 1, 1},
		{"a:hover", 0, 1, 1},
	}
	for _, tt := range tests {
		s := mustSelectors(t, tt.sel)[0]
		a, b, c := unpackSpec(s.spec)
		if a != tt.a || b != tt.b || c != tt.c {
			t.Errorf("%s: specificity (%d,%d,%d), want (%d,%d,%d)", tt.sel, a, b, c, tt.a, tt.b, tt.c)
		}
	}
}

func TestSelector_UnsupportedWarned(t *testing.T) {
	t.Parallel()
	s := mustParse(t, `p:has(a) { color: red } p { color: blue }`)
	if len(s.rules) != 1 || !hasWarning(s.Warnings(), "unsupported-css-selector", ":has()") {
		t.Errorf("rules %d warnings %v", len(s.rules), s.Warnings())
	}
}

func TestMatcher_BudgetStopsBacktracking(t *testing.T) {
	t.Parallel()
	deep := strings.Repeat("<div>", 200) + strings.Repeat("</div>", 200)
	doc := parseHTML(t, deep)
	sel := mustSelectors(t, strings.Repeat("div ", 30)+"span")
	m := newMatcher("", 1000)
	var leaf *html.Node
	for n := byTag(doc, "div"); n != nil; n = n.FirstChild {
		if n.Type == html.ElementNode {
			leaf = n
		}
	}
	m.anyMatches(sel, leaf)
	m.anyMatches(mustSelectors(t, "span "+strings.Repeat("div ", 30)), leaf)
	if !m.over {
		t.Errorf("budget not exhausted after %d steps", m.steps)
	}
}

// TestSelector_ClassListSplitsOnASCIIWhitespace locks the class and ~=
// word lists to ASCII whitespace (Selectors 4, 6.3 and 6.6; HTML,
// 2.3.8): a no-break space stays inside a class name, for the subject and
// for its ancestors alike.
func TestSelector_ClassListSplitsOnASCIIWhitespace(t *testing.T) {
	t.Parallel()
	c := compute(t, "<div class=\"a b\"><p id=d>x</p></div><p id=s class=\"a b\">y</p>",
		`.a\a0 b p { font-size: 20px } p.a\a0 b { text-indent: 2px } [class~=a] { font-weight: 700 }`)
	if d := c.of("d"); d.FontSize != 15 {
		t.Errorf("descendant font size %v, want the ancestor's class to match", d.FontSize)
	}
	if s := c.of("s"); s.FontWeight != 400 {
		t.Errorf("[class~=a] matched the class a b: weight %v", s.FontWeight)
	}
}

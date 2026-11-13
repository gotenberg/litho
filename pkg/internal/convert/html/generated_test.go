package html

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

func TestGenerated_BeforeAndAfter(t *testing.T) {
	t.Parallel()
	p := prep(t, `<style>p::before { content: "[" attr(title) "|" attr(missing, "fb") "] " } p::after { content: " end"; display: block }
		div::after { content: "" } span::before { content: normal }</style>
		<p id=p title=T>body</p><div id=d>x</div><span id=s>y</span>`, Options{})
	pn := byID(t, p.root, "p")
	if got := treeText(pn); got != "[T|fb] body end" {
		t.Errorf("p text %q", got)
	}
	first, last := pn.Children[0], pn.Children[len(pn.Children)-1]
	if first.Kind != layout.Element || first.Role != "" || first.Style == pn.Style || last.Role != "" || last.Style == first.Style {
		t.Errorf("pseudo-elements %+v, %+v: want role-less elements with their own style", first, last)
	}
	// content: "" generates an empty box, the clearfix idiom; normal
	// generates none.
	if d := byID(t, p.root, "d"); len(d.Children) != 2 || len(d.Children[1].Children) != 0 {
		t.Errorf("div children %+v, want the text and an empty ::after", d.Children)
	}
	if s := byID(t, p.root, "s"); len(s.Children) != 1 {
		t.Errorf("span children %+v, want the text alone", s.Children)
	}
}

func TestGenerated_CounterScopes(t *testing.T) {
	t.Parallel()
	p := prep(t, `<style>
		body { counter-reset: h }
		h2 { counter-increment: h; counter-reset: s }
		h3 { counter-increment: s }
		h2::before { content: counter(h, upper-roman) ". " }
		h3::before { content: counter(h) "." counter(s, lower-alpha) " " }
		li::before { content: counters(list-item, ".") ":" }
		.u::after { content: counter(unknown) }
		</style>
		<h2 id=a>A</h2><h3 id=a1>x</h3><h3 id=a2>y</h3><h2 id=b>B</h2><h3 id=b1>z</h3>
		<ol><li id=l1>a<ol><li id=l11>b<li id=l12>c</ol><li id=l2>d</ol><p class=u id=u>u</p>`, Options{})
	for id, want := range map[string]string{
		"a": "I. A", "a1": "1.a x", "a2": "1.b y", "b": "II. B", "b1": "2.a z",
		"l1": "1:a1.1:b1.2:c", "l11": "1.1:b", "l2": "2:d", "u": "u0",
	} {
		if got := treeText(byID(t, p.root, id)); got != want {
			t.Errorf("#%s: %q, want %q", id, got, want)
		}
	}
}

func TestGenerated_Quotes(t *testing.T) {
	t.Parallel()
	p := prep(t, `<style>.c::before { content: open-quote } .c::after { content: close-quote } .n { quotes: "<" ">" "{" "}" }
		.x::before { content: no-open-quote }</style>
		<p id=en><q>a <q>b</q></q></p><p id=fr lang=fr><q>c</q></p><p id=n class=n><q>d<q>e<q>f</q></q></q></p>
		<p id=z><span class=c>g</span><span class=c>h</span></p><p id=close><q style="quotes: none">i</q></p>`, Options{})
	for id, want := range map[string]string{
		"en": "“a ‘b’”", "fr": "«c»", "n": "<d{e{f}}>", "z": "“g”“h”", "close": "i",
	} {
		if got := treeText(byID(t, p.root, id)); got != want {
			t.Errorf("#%s: %q, want %q", id, got, want)
		}
	}
}

// TestGenerated_QuotesByLanguage locks the marks quotes: auto draws: the
// Unicode CLDR locale of the tag, then of the tag cut a subtag at a time,
// then the basic ones (CSS Generated Content 3, 2.1, left to the user
// agent), a q taking its parent's language, as Chromium draws them.
func TestGenerated_QuotesByLanguage(t *testing.T) {
	t.Parallel()
	p := prep(t, `<style>.o::before { content: open-quote } .o::after { content: close-quote }</style>
		<p id=de lang=de><q>a <q>b</q></q></p><p id=chde lang=DE_ch><q>c</q></p><p id=frch lang=fr-CH><q>d <q>e</q></q></p>
		<p id=zhtw lang=zh-Hant-TW><q>f</q></p><p id=unknown lang=zz-Latn><q>g</q></p><p id=basic lang=ast><q>h</q></p>
		<p id=qparent lang=de><q lang=fr>i <q>j</q></q></p><p id=span lang=de><span class=o lang=fr>k</span></p>`, Options{})
	for id, want := range map[string]string{
		"de": "„a ‚b‘“", "chde": "„c“", "frch": "«d ‹e›»", "zhtw": "「f」", "unknown": "“g”", "basic": "“h”",
		"qparent": "„i «j»“", "span": "«k»",
	} {
		if got := treeText(byID(t, p.root, id)); got != want {
			t.Errorf("#%s: %q, want %q", id, got, want)
		}
	}
}

func TestGenerated_Markers(t *testing.T) {
	t.Parallel()
	p := prep(t, `<style>#a::marker { content: "(" counter(list-item) ")"; color: red } #b::marker { content: none }
		#c::marker { color: blue } .il { display: inline list-item }</style>
		<ol><li id=a>x<li id=b>y<li id=c>z</ol><ol start=0><li id=zero>w</ol><div><span class=il id=il>i</span></div>`, Options{})
	a := byID(t, p.root, "a")
	if a.Marker == nil || treeText(a.Marker) != "(1)" || a.Marker.Style == a.Style {
		t.Errorf("#a marker %+v", a.Marker)
	}
	if b := byID(t, p.root, "b"); b.Marker == nil || len(b.Marker.Children) != 1 || b.Marker.Children[0].Text != "" {
		t.Errorf("#b marker %+v, want an empty label", b.Marker)
	}
	if c := byID(t, p.root, "c"); c.Marker == nil || len(c.Marker.Children) != 0 || c.ListValue != 3 {
		t.Errorf("#c marker %+v, value %d: want the default marker in its own style", c.Marker, c.ListValue)
	}
	if z := byID(t, p.root, "zero"); z.Marker == nil || treeText(z.Marker) != "0. " {
		t.Errorf("start=0 marker %+v", z.Marker)
	}
	if il := byID(t, p.root, "il"); il.ListValue != 1 {
		t.Errorf("inline list item numbered %d", il.ListValue)
	}
}

// TestGenerated_CounterStyles locks the markers and counters a counter
// style the layout does not spell draws (CSS Counter Styles 3): a style
// of an @counter-style rule with its own suffix, a predefined one, a
// predefined one a rule redefines, an unknown name drawn as decimal with
// a warning, and counter() in a predefined style.
func TestGenerated_CounterStyles(t *testing.T) {
	t.Parallel()
	p := prep(t, `<style>@counter-style s { system: cyclic; symbols: "◆"; suffix: " " } .a { list-style-type: s }
		.b { list-style-type: upper-armenian } @counter-style lower-roman { system: fixed; symbols: R } .c { list-style-type: lower-roman }
		.d::before { content: counter(list-item, bengali) } .u { list-style-type: nowhere }</style>
		<ol class=a><li id=a>x</ol><ol class=b start=3><li id=b>y</ol><ol class=c><li id=c>z</ol><ol><li id=d class=d>w</ol>
		<ol class=u><li id=u>v</ol>`, Options{})
	if b := byID(t, p.root, "b"); b.Marker != nil || layout.MarkerText(b.Style, b.ListValue) != "Գ. " {
		t.Errorf("#b marker %v, want the layout's Գ. ", b.Marker)
	}
	for id, want := range map[string]string{"a": "◆ ", "c": "R. ", "u": "1. "} {
		if m := byID(t, p.root, id).Marker; m == nil || treeText(m) != want {
			t.Errorf("#%s marker %v, want %q", id, m, want)
		}
	}
	if got := treeText(byID(t, p.root, "d")); got != "১w" {
		t.Errorf("#d text %q", got)
	}
	if !hasWarning(p.warnings, warn.CSSUnsupported, "nowhere") {
		t.Errorf("no warning for the unknown counter style: %v", p.warnings)
	}
}

func TestGenerated_Images(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"a.png": {Data: pngBytes(t, 3, 3)}}
	p := prep(t, `<style>#p::before { content: "a" url(a.png) url(missing.png) "b" }</style><p id=p>x</p>`, Options{Resources: fsys})
	g := byID(t, p.root, "p").Children[0]
	if len(g.Children) != 3 || g.Children[1].Kind != layout.Replaced || g.Children[1].Picture == nil || treeText(g) != "ab" {
		t.Errorf("generated %+v, want a, the picture, b", g.Children)
	}
	if !hasWarning(p.warnings, warn.ResourceRefused, "missing.png") && !hasWarning(p.warnings, warn.ResourceFailed, "missing.png") {
		t.Errorf("no warning for the missing image: %v", p.warnings)
	}
}

func TestGenerated_CapsText(t *testing.T) {
	t.Parallel()
	sep := strings.Repeat("s", 1000)
	var b strings.Builder
	b.WriteString(`<style>div { counter-reset: c } div::before { content: counters(c, "` + sep + `") counters(c, "` + sep + `") }</style>`)
	for range 100 {
		b.WriteString("<div>")
	}
	p := prep(t, b.String(), Options{})
	var longest int
	eachNode(p.root, func(n *layout.Node) {
		if n.Kind == layout.Text {
			longest = max(longest, len(n.Text))
		}
	})
	if longest > maxGeneratedBytes || !hasWarning(p.warnings, warn.LimitReached, "") {
		t.Errorf("longest generated text %d bytes, warnings %v", longest, p.warnings)
	}
}

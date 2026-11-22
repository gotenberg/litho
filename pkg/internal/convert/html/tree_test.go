package html

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
	"github.com/gotenberg/litho/pkg/models"
)

func TestPrepare_MapsElementsToRoles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		html, role string
	}{
		{`<article id=x>a</article>`, "Sect"},
		{`<section id=x>a</section>`, "Sect"},
		{`<nav id=x>a</nav>`, "Sect"},
		{`<aside id=x>a</aside>`, "Sect"},
		{`<div id=x>a</div>`, "Div"},
		{`<main id=x>a</main>`, "Div"},
		{`<header id=x>a</header>`, "Div"},
		{`<footer id=x>a</footer>`, "Div"},
		{`<h1 id=x>a</h1>`, "H1"},
		{`<h4 id=x>a</h4>`, "H4"},
		{`<h6 id=x>a</h6>`, "H6"},
		{`<p id=x>a</p>`, "P"},
		{`<ul id=x><li>a</ul>`, "L"},
		{`<ol id=x><li>a</ol>`, "L"},
		{`<dl id=x><dt>a<dd>b</dl>`, "L"},
		{`<ul><li id=x>a</ul>`, "LI"},
		{`<dl><dt id=x>a</dl>`, "Lbl"},
		{`<dl><dd id=x>a</dl>`, "LBody"},
		{`<dl><div id=x><dt>a<dd>b</div></dl>`, ""},
		{`<dl><div><dt id=x>a<dd>b</div></dl>`, "Lbl"},
		{`<div><dt id=x>a</div>`, ""},
		{`<div><dd id=x>a</div>`, ""},
		{`<blockquote id=x>a</blockquote>`, "BlockQuote"},
		{`<pre id=x>a</pre>`, "Code"},
		{`<p><code id=x>a</code>`, "Code"},
		{`<p><kbd id=x>a</kbd>`, "Code"},
		{`<p><samp id=x>a</samp>`, "Code"},
		{`<p><q id=x>a</q>`, "Quote"},
		{`<p><cite id=x>a</cite>`, "Quote"},
		{`<figure id=x>a</figure>`, "Figure"},
		{`<figure><figcaption id=x>a</figcaption></figure>`, "Caption"},
		{`<table id=x><tr><td>a</table>`, "Table"},
		{`<table><caption id=x>a</caption><tr><td>a</table>`, "Caption"},
		{`<table><thead id=x><tr><td>a</table>`, "THead"},
		{`<table><tbody id=x><tr><td>a</table>`, "TBody"},
		{`<table><tfoot id=x><tr><td>a</table>`, "TFoot"},
		{`<table><tr id=x><td>a</table>`, "TR"},
		{`<table><tr><th id=x>a</table>`, "TH"},
		{`<table><tr><td id=x>a</table>`, "TD"},
		{`<p><a id=x href="https://example.com/">a</a>`, "Link"},
		{`<p><a id=x>a</a>`, ""},
		{`<p><span id=x>a</span>`, "Span"},
		{`<p><em id=x>a</em>`, "Span"},
		{`<p><strong id=x>a</strong>`, "Span"},
		{`<p><b id=x>a</b>`, "Span"},
		{`<p><i id=x>a</i>`, "Span"},
		{`<p><u id=x>a</u>`, "Span"},
		{`<p><small id=x>a</small>`, "Span"},
		{`<p><sub id=x>a</sub>`, "Span"},
		{`<p><sup id=x>a</sup>`, "Span"},
		{`<p><abbr id=x>a</abbr>`, "Span"},
		{`<p><mark id=x>a</mark>`, "Span"},
		{`<body id=x>a</body>`, ""},
		{`<address id=x>a</address>`, ""},
	} {
		p := prep(t, tc.html, Options{})
		if got := byID(t, p.root, "x").Role; got != tc.role {
			t.Errorf("%s: role %q, want %q", tc.html, got, tc.role)
		}
	}
}

// TestPrepare_FigureName locks the Alt of a figure's Figure: its
// aria-label, else its first figcaption's text, else its title, as
// HTML-AAM names a figure.
func TestPrepare_FigureName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ html, alt string }{
		{`<figure id=x aria-label=" Named " title=t><figcaption>c</figcaption></figure>`, "Named"},
		{`<figure id=x title=t><p>x</p><figcaption> A   caption </figcaption><figcaption>b</figcaption></figure>`, "A caption"},
		{`<figure id=x title="The title">a</figure>`, "The title"},
		{`<figure id=x>a</figure>`, ""},
	} {
		p := prep(t, tc.html, Options{})
		if got := byID(t, p.root, "x").Alt; got != tc.alt {
			t.Errorf("%s: alt %q, want %q", tc.html, got, tc.alt)
		}
	}
}

func TestPrepare_RootStartsNoRole(t *testing.T) {
	t.Parallel()
	p := prep(t, `<html lang="fr-CA"><p>Bonjour</p>`, Options{})
	if p.root.Role != "" || p.root.Kind != layout.Element {
		t.Errorf("root %+v, want an element without a role", p.root)
	}
	if p.layout.Lang != "fr-CA" || p.meta.Lang != "fr-CA" {
		t.Errorf("languages %q and %q, want fr-CA", p.layout.Lang, p.meta.Lang)
	}
}

func TestPrepare_BuildsTextAndBreaks(t *testing.T) {
	t.Parallel()
	p := prep(t, `<p id=x lang=de>one<br>two<wbr>three <!-- gone --> four</p>`, Options{})
	n := byID(t, p.root, "x")
	if got := treeText(n); got != "one|two\u200bthree  four" {
		t.Errorf("text %q", got)
	}
	if n.Lang != "de" {
		t.Errorf("lang %q, want de", n.Lang)
	}
	for _, c := range n.Children {
		if c.Style == nil {
			t.Errorf("node %+v has no style", c)
		}
		if c.Kind == layout.Text && c.Style != n.Style {
			t.Errorf("text %q does not share its parent's style", c.Text)
		}
	}
}

func TestPrepare_PrunesWhatDrawsNothing(t *testing.T) {
	t.Parallel()
	src := `<head><title>T</title><style>.h{display:none}</style><script>var s</script></head>
<body><p>kept</p><p class=h>css-hidden</p><p hidden>attr-hidden</p><script>script</script>
<noscript>noscript</noscript><template><p>template</p></template><input type=hidden value=hidden-input></body>`
	p := prep(t, src, Options{})
	got := treeText(p.root)
	for _, gone := range []string{"T", "css-hidden", "attr-hidden", "script", "noscript", "template", "hidden-input", "var s", "display"} {
		if strings.Contains(got, gone) {
			t.Errorf("text %q holds %q", got, gone)
		}
	}
	if !strings.Contains(got, "kept") {
		t.Errorf("text %q lost the visible paragraph", got)
	}
}

// TestPrepare_ImageWithoutPictureKeepsItsBox locks that an <img> that
// draws no picture and has no alt text to show is a replaced box without
// content, which its style sizes and paints, as Chromium draws it
// ([emptyImage]); one with alt text shows the text instead.
func TestPrepare_ImageWithoutPictureKeepsItsBox(t *testing.T) {
	t.Parallel()
	p := prep(t, `<p><img id=a style="width:60px;height:40px;background:yellow"><img id=b src=""><img id=c alt="">`+
		`<img id=d src="missing.png"><span id=e><img src="missing.png" alt="gone"></span></p>`, Options{Resources: fstest.MapFS{}})
	for _, id := range []string{"a", "b", "c", "d"} {
		n := byID(t, p.root, id)
		if n.Kind != layout.Replaced || n.Picture != nil || n.Role != "" {
			t.Errorf("%s: kind %v picture %v role %q, want an empty replaced box", id, n.Kind, n.Picture, n.Role)
		}
	}
	if w := byID(t, p.root, "a").Style.Width; w.Kind != css.Points || w.Value != 45 {
		t.Errorf("a: width %+v, want its style's 60px", w)
	}
	if got := treeText(byID(t, p.root, "e")); got != "gone" {
		t.Errorf("alt text %q, want gone", got)
	}
}

// TestPrepare_FailedImageKeepsItsSizeOnlyWhenFullySized locks the size of
// an <img> whose source fails to load and that has no alt text to show
// ([emptyImage]): its style's width and height hold when both are given,
// when an aspect ratio completes one of them, or when its display is not
// inline; otherwise both compute to auto. An image naming no source keeps
// what its style gives.
func TestPrepare_FailedImageKeepsItsSizeOnlyWhenFullySized(t *testing.T) {
	t.Parallel()
	p := prep(t, `<style>.w { width: 60px }</style><p>`+
		`<img id=both src="missing.png" style="width:60px;height:40px">`+
		`<img id=ratio src="missing.png" alt="" style="width:60px;aspect-ratio:3/2">`+
		`<img id=block src="missing.png" alt="" class=w style="display:block">`+
		`<img id=float src="missing.png" alt="" class=w style="float:left">`+
		`<img id=nosrc alt="" class=w>`+
		`<img id=width src="missing.png" alt="" class=w>`+
		`<img id=attr src="missing.png" width=60>`+
		`<img id=height src="missing.png" alt="" style="height:40px"></p>`, Options{Resources: fstest.MapFS{}})
	for _, id := range []string{"both", "ratio", "block", "float", "nosrc"} {
		if w := byID(t, p.root, id).Style.Width; w.Kind != css.Points || w.Value != 45 {
			t.Errorf("%s: width %+v, want its style's 60px", id, w)
		}
	}
	for _, id := range []string{"width", "attr", "height"} {
		st := byID(t, p.root, id).Style
		if st.Width.Kind != css.Auto || st.Height.Kind != css.Auto || st.AspectRatio.Ratio() != 0 {
			t.Errorf("%s: width %+v, height %+v, ratio %v, want auto", id, st.Width, st.Height, st.AspectRatio.Ratio())
		}
	}
}

func TestPrepare_DecodesImages(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"img/a.png":   {Data: pngBytes(t, 40, 20)},
		"img/b.jpg":   {Data: jpegBytes(t, 8, 4)},
		"img/c.gif":   {Data: gifBytes(t, 4, 4)},
		"img/d.svg":   {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
		"img/bad.png": {Data: []byte("not a picture")},
	}
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes(t, 2, 2))
	src := `<p><img id=a src="img/a.png" alt="A"><img id=b src="img/b.jpg"><img id=c src="img/c.gif" alt="">` +
		`<img id=t src="img/a.png" title="Titled"><img id=u src="` + data + `" alt="U">` +
		`<span id=d><img src="img/d.svg" alt="vector"></span><span id=m><img src="img/missing.png" alt="gone"></span>` +
		`<span id=f><img src="img/bad.png" alt="broken"></span><span id=e><img src="" alt="empty"></span></p>`
	p := prep(t, src, Options{Resources: fsys})

	a := byID(t, p.root, "a")
	if a.Kind != layout.Replaced || a.Picture == nil || a.Picture.Width != 40*css.Pt || a.Picture.Height != 20*css.Pt {
		t.Fatalf("PNG node %+v, picture %+v", a, a.Picture)
	}
	if a.Role != "Figure" || a.Alt != "A" || len(a.Picture.Source.Samples) == 0 {
		t.Errorf("PNG role %q alt %q", a.Role, a.Alt)
	}
	if a.Style == nil || a.Style.Display != css.Inline {
		t.Errorf("PNG keeps its own style: %+v", a.Style)
	}
	if b := byID(t, p.root, "b"); b.Picture == nil || len(b.Picture.Source.JPEG) == 0 || b.Picture.Source.Samples != nil {
		t.Errorf("JPEG not kept as its bytes: %+v", b.Picture)
	}
	if c := byID(t, p.root, "c"); c.Picture == nil || c.Role != "" {
		t.Errorf("decorative GIF role %q picture %+v", c.Role, c.Picture)
	}
	if tt := byID(t, p.root, "t"); tt.Alt != "Titled" {
		t.Errorf("title fallback alt %q", tt.Alt)
	}
	if u := byID(t, p.root, "u"); u.Picture == nil || u.Picture.Width != 2*css.Pt {
		t.Errorf("data: URL picture %+v", u.Picture)
	}
	if d := byID(t, p.root, "d").Children; len(d) != 1 || d[0].Kind != layout.Replaced || d[0].Picture == nil || d[0].Picture.Vector == nil {
		t.Errorf("SVG image not a vector picture: %+v", d)
	}
	for id, alt := range map[string]string{"m": "gone", "f": "broken", "e": "empty"} {
		if got := treeText(byID(t, p.root, id)); got != alt {
			t.Errorf("%s: fallback text %q, want %q", id, got, alt)
		}
	}
	if !hasWarning(p.warnings, warn.ResourceFailed, "img/missing.png") ||
		!hasWarning(p.warnings, warn.PictureFormatUnsupported, "img/bad.png") {
		t.Errorf("warnings %+v", p.warnings)
	}
	if len(p.warnings) != 2 {
		t.Errorf("%d warnings, want 2: %+v", len(p.warnings), p.warnings)
	}
}

func TestPrepare_RefusesURLsOutsideTheResources(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"a.png": {Data: pngBytes(t, 1, 1)}, "sub/a.png": {Data: pngBytes(t, 1, 1)}}
	for _, tc := range []struct {
		src  string
		fsys fstest.MapFS
		want string
		code warn.Code
	}{
		{"https://example.com/a.png", fsys, "no HTTP client", warn.ResourceNoNetwork},
		{"file:///etc/a.png", fsys, "file: URLs are never fetched", warn.ResourceRefused},
		{"ftp://example.com/a.png", fsys, "absolute URLs", warn.ResourceRefused},
		{"//example.com/a.png", fsys, "absolute URLs", warn.ResourceRefused},
		{"../a.png", fsys, "leaves the resources root", warn.ResourceRefused},
		{"sub/../../a.png", fsys, "leaves the resources root", warn.ResourceRefused},
		{"/a.png", fsys, "leaves the resources root", warn.ResourceRefused},
		{"a.png", nil, "no resources", warn.ResourceRefused},
	} {
		o := Options{}
		if tc.fsys != nil {
			o.Resources = tc.fsys
		}
		p := prep(t, `<p id=x><img src="`+tc.src+`" alt="alt">`, o)
		if !hasWarning(p.warnings, tc.code, tc.want) {
			t.Errorf("%s: warnings %+v, want %s", tc.src, p.warnings, tc.want)
		}
		if got := treeText(byID(t, p.root, "x")); got != "alt" {
			t.Errorf("%s: text %q", tc.src, got)
		}
	}
	// Inside the root, a ".." that stays in it reads the file.
	p := prep(t, `<p id=x><img id=i src="sub/../a.png">`, Options{Resources: fsys})
	if byID(t, p.root, "i").Picture == nil {
		t.Errorf("sub/../a.png not read: %+v", p.warnings)
	}
}

func TestPrepare_ResolvesLinks(t *testing.T) {
	t.Parallel()
	p := prep(t, `<p><a id=f href="#target">f</a><a id=e href="#caf%C3%A9">e</a><a id=x href=" https://example.com/a?b#c " title="Ex">x</a>`+
		`<a id=r href="docs/page.html">r</a><a id=s href="javascript:void(0)">s</a><a id=h href="#">h</a>`+
		`<a name=named>n</a><span id=target>t</span>`, Options{})
	for id, want := range map[string]string{
		"f": "#target", "e": "#café", "x": "https://example.com/a?b#c", "r": "docs/page.html", "s": "", "h": "#",
	} {
		n := byID(t, p.root, id)
		if n.Href != want {
			t.Errorf("%s: href %q, want %q", id, n.Href, want)
		}
		if (n.Role == "Link") != (want != "") {
			t.Errorf("%s: role %q", id, n.Role)
		}
	}
	if byID(t, p.root, "x").Alt != "Ex" {
		t.Errorf("link title not its alt")
	}
	byID(t, p.root, "named")

	abs := prep(t, `<base href="https://example.com/dir/"><p><a id=r href="page.html">r</a><a id=f href="#x">f</a>`, Options{})
	if got := byID(t, abs.root, "r").Href; got != "https://example.com/dir/page.html" {
		t.Errorf("absolute base: href %q", got)
	}
	if got := byID(t, abs.root, "f").Href; got != "#x" {
		t.Errorf("absolute base: fragment %q", got)
	}
	rel := prep(t, `<base href="sub/"><p><a id=r href="page.html">r</a>`, Options{})
	if got := byID(t, rel.root, "r").Href; got != "sub/page.html" {
		t.Errorf("relative base: href %q", got)
	}
}

func TestPrepare_HonoursBaseForResources(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"assets/a.png": {Data: pngBytes(t, 3, 3)}}
	p := prep(t, `<base href="assets/"><p><img id=i src="a.png">`, Options{Resources: fsys})
	if byID(t, p.root, "i").Picture == nil {
		t.Errorf("relative base not applied: %+v", p.warnings)
	}
	// An http(s) base moves the resources to the network (HTML Standard,
	// 4.2.3), which needs a client.
	abs := prep(t, `<base href="https://cdn.example.com/assets/"><p id=x><img src="a.png" alt=alt>`, Options{Resources: fsys})
	if !hasWarning(abs.warnings, warn.ResourceNoNetwork, "") || treeText(byID(t, abs.root, "x")) != "alt" {
		t.Errorf("absolute base not applied to a resource: %+v", abs.warnings)
	}
	// Any other absolute base resolves links alone.
	other := prep(t, `<base href="ftp://example.com/assets/"><p><img id=i src="assets/a.png">`, Options{Resources: fsys})
	if byID(t, other.root, "i").Picture == nil {
		t.Errorf("ftp base applied to a resource: %+v", other.warnings)
	}
}

func TestPrepare_NumbersLists(t *testing.T) {
	t.Parallel()
	p := prep(t, `<ol start=5><li id=a>a<li id=b>b<li id=c value=10>c<li id=d>d</ol>`+
		`<ol reversed><li id=r1>a<li id=r2>b<li id=r3>c</ol>`+
		`<ol reversed start=10><li id=s1>a<li id=s2 value=3>b<li id=s3>c</ol>`+
		`<ol><li id=p1>a</ol><ul><li id=u1 value=7>a</ul>`+
		`<ol start=99999999999><li id=big>a</ol>`, Options{})
	for id, want := range map[string]int{
		"a": 5, "b": 6, "c": 10, "d": 11,
		"r1": 3, "r2": 2, "r3": 1,
		"s1": 10, "s2": 3, "s3": 2,
		"p1": 1, "u1": 7, "big": css.MaxInteger,
	} {
		if got := byID(t, p.root, id).ListValue; got != want {
			t.Errorf("%s: list value %d, want %d", id, got, want)
		}
	}
}

func TestPrepare_ReadsSpans(t *testing.T) {
	t.Parallel()
	p := prep(t, `<table><colgroup id=g span=3></colgroup><col id=c span=0><tr>`+
		`<td id=a colspan=2 rowspan=3>a<td id=b colspan=5000 rowspan=99999>b<td id=n colspan=x rowspan=-1>n<td id=z colspan=0 rowspan=0>z</table>`, Options{})
	for _, tc := range []struct {
		id              string
		col, row, group int
	}{
		{"a", 2, 3, 0}, {"b", maxColSpan, maxRowSpan, 0}, {"n", 0, 0, 0}, {"g", 0, 0, 3}, {"c", 0, 0, 0},
		{"z", 0, maxRowSpan, 0},
	} {
		n := byID(t, p.root, tc.id)
		if n.ColSpan != tc.col || n.RowSpan != tc.row || n.Span != tc.group {
			t.Errorf("%s: spans %d %d %d, want %d %d %d", tc.id, n.ColSpan, n.RowSpan, n.Span, tc.col, tc.row, tc.group)
		}
	}
}

func TestPrepare_DrawsFormControlsAsText(t *testing.T) {
	t.Parallel()
	p := prep(t, `<form><input id=t value="typed"><input id=ph placeholder="hint"><input id=s type=submit>`+
		`<input id=pw type=password value="abc"><input id=cb type=checkbox>`+
		`<select id=sel><option>one<option selected>two</select><textarea id=ta>long text</textarea>`+
		`<button id=btn>Press</button></form>`, Options{})
	for id, want := range map[string]string{
		"t": "typed", "ph": "hint", "s": "Submit", "pw": "•••", "cb": "", "sel": "two", "ta": "long text", "btn": "Press",
	} {
		n := byID(t, p.root, id)
		if got := treeText(n); got != want {
			t.Errorf("%s: text %q, want %q", id, got, want)
		}
		if n.Style.Display != css.InlineBlock {
			t.Errorf("%s: display %v, want inline-block", id, n.Style.Display)
		}
	}
	if got := codes(p.warnings); len(got) != 1 || got[0] != warn.FormWidgetOmitted.String() {
		t.Errorf("warnings %v, want one form-control", got)
	}
}

// TestPrepare_SizesFormControlsByTheirText holds a control to the text
// it shows (HTML Standard, 15.5.15 and 15.5.17): an empty textarea shows
// its placeholder, and an input's text stays on one line.
func TestPrepare_SizesFormControlsByTheirText(t *testing.T) {
	t.Parallel()
	p := prep(t, `<textarea id=ta placeholder="Tell us"></textarea><input id=in value="a long answer" style="width: 5px">`, Options{})
	if got := treeText(byID(t, p.root, "ta")); got != "Tell us" {
		t.Errorf("textarea text %q, want its placeholder", got)
	}
	in := byID(t, p.root, "in")
	if len(in.Children) != 1 || in.Children[0].Style.WhiteSpace != css.Pre {
		t.Fatalf("input text %+v, want one run that does not wrap", in.Children)
	}
	if in.Style.WhiteSpace == css.Pre {
		t.Error("the input's own style changed: the computed style is shared")
	}
}

// TestPrepare_FormControlsSayTheirDefaultSize gives each control the
// attributes its default size comes from (HTML Standard, 15.5.15 to
// 15.5.17): a text field its size, 20 when absent, zero or invalid, a
// textarea its cols and rows, a select its option labels; a button and a
// checkbox none.
func TestPrepare_FormControlsSayTheirDefaultSize(t *testing.T) {
	t.Parallel()
	p := prep(t, `<input id=a><input id=b size=5><input id=c size=0><input id=d size=x type=email><input id=e type=checkbox>`+
		`<input id=f type=submit><textarea id=g cols=7 rows=4></textarea><textarea id=h cols=-3></textarea>`+
		`<select id=i><option>one<optgroup label=G><option label="Two">2</optgroup></select><select id=j multiple size=1><option>x</select>`, Options{})
	for id, want := range map[string]*layout.Control{
		"a": {Kind: layout.ControlTextField, Chars: 20}, "b": {Kind: layout.ControlTextField, Chars: 5},
		"c": {Kind: layout.ControlTextField, Chars: 20}, "d": {Kind: layout.ControlTextField, Chars: 20},
		"e": nil, "f": nil,
		"g": {Kind: layout.ControlTextArea, Chars: 7, Rows: 4}, "h": {Kind: layout.ControlTextArea, Chars: 20, Rows: 2},
		"i": {Kind: layout.ControlDropDown, Labels: []layout.ControlLabel{{Text: "one"}, {Text: "Two"}}},
		"j": {Kind: layout.ControlDropDown, Labels: []layout.ControlLabel{{Text: "x"}}},
	} {
		got := byID(t, p.root, id).Control
		if (got == nil) != (want == nil) || got != nil && fmt.Sprint(*got) != fmt.Sprint(*want) {
			t.Errorf("%s: control %+v, want %+v", id, got, want)
		}
	}
}

// TestPrepare_ListBoxShowsItsRows draws a select of display size above 1
// as a list box (HTML Standard, 15.5.16): a row for each option and each
// optgroup's label, as many rows as its display size, from the one that
// shows its last selected option, the selected marked, the rows past its
// options a line break alone ("|").
func TestPrepare_ListBoxShowsItsRows(t *testing.T) {
	t.Parallel()
	p := prep(t, `<select id=a size=3><option>One<option>Two<option>Three<option>Four<option selected>Five</select>`+
		`<select id=b multiple><optgroup label=Group><option selected>In</optgroup><option>Out</select>`, Options{})
	for id, want := range map[string][]string{"a": {"Three", "Four", "Five"}, "b": {"Group", "In", "Out", "|"}} {
		n := byID(t, p.root, id)
		if n.Control == nil || n.Control.Kind != layout.ControlListBox || n.Control.Rows != len(want) {
			t.Fatalf("%s: control %+v, want a list box of %d rows", id, n.Control, len(want))
		}
		if len(n.Children) != len(want) {
			t.Fatalf("%s: %d rows, want %d", id, len(n.Children), len(want))
		}
		for i, row := range n.Children {
			if got := treeText(row); got != want[i] || row.Style.Display != css.Block {
				t.Errorf("%s: row %d %q display %v, want %q as a block", id, i, got, row.Style.Display, want[i])
			}
		}
		if n.Style.VerticalAlign.Kind != css.TextBottom {
			t.Errorf("%s: vertical-align %v, want text-bottom", id, n.Style.VerticalAlign)
		}
	}
	a, b := byID(t, p.root, "a"), byID(t, p.root, "b")
	if a.Children[2].Style.BackgroundColor != listSelected || a.Children[1].Style.BackgroundColor.A != 0 {
		t.Error("the selected option is not the one marked")
	}
	if g, in := b.Children[0].Style, b.Children[1].Style; g.FontWeight < 700 || in.FontWeight != 400 || in.Padding[css.Left].Value <= g.Padding[css.Left].Value {
		t.Errorf("group label weight %d, option weight %d, indents %v and %v", g.FontWeight, in.FontWeight, g.Padding[css.Left], in.Padding[css.Left])
	}
}

func TestPrepare_LeavesEmbeddedContentOut(t *testing.T) {
	t.Parallel()
	p := prep(t, `<p id=x>a<iframe src="x.html">i</iframe><audio>v</audio><math><mi>m</mi></math>`+
		`<object data="x.swf">fallback</object><canvas>canvas-fallback</canvas><audio>v2</audio><video>v3</video></p>`, Options{})
	if got := treeText(byID(t, p.root, "x")); got != "amfallbackcanvas-fallback" {
		t.Errorf("text %q", got)
	}
	want := []string{"content-unsupported", "content-unsupported"}
	if got := codes(p.warnings); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("warnings %v, want two unsupported-element (audio once): an iframe's document is read when it is laid out", got)
	}
}

func TestPrepare_CapsTheTreeDepth(t *testing.T) {
	t.Parallel()
	// The parser refuses such a document; one built by hand reaches the
	// builder's own cap.
	doc, err := dom.Parse(strings.NewReader("<body>"))
	if err != nil {
		t.Fatal(err)
	}
	body := doc.FirstChild.LastChild
	parent := body
	for range maxTreeDepth + 20 {
		div := &dom.Node{Type: dom.ElementNode, Data: "div", DataAtom: atom.Div}
		parent.AppendChild(div)
		parent = div
	}
	parent.AppendChild(&dom.Node{Type: dom.TextNode, Data: "deep"})
	o, err := Options{}.withDefaults()
	if err != nil {
		t.Fatal(err)
	}
	p, err := prepareDoc(context.Background(), doc, o, nil, newPrintSetup(o))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(treeText(p.root), "deep") {
		t.Errorf("content past the depth cap kept")
	}
	if !hasWarning(p.warnings, warn.LimitReached, "512") {
		t.Errorf("warnings %+v", p.warnings)
	}
}

func TestPrepare_RefusesTooDeepADocument(t *testing.T) {
	t.Parallel()
	o, err := Options{}.withDefaults()
	if err != nil {
		t.Fatal(err)
	}
	_, err = prepare(context.Background(), []byte(strings.Repeat("<div>", 600)), o, nil)
	if !errors.Is(err, ErrTooDeep) {
		t.Errorf("600 nested divs: %v", err)
	}
}

// TestScanDocument_CapsTheElements checks the element count a document may
// hold to the element: [MaxElements] pass, one more does not.
func TestScanDocument_CapsTheElements(t *testing.T) {
	t.Parallel()
	// html, head and body come with the parser.
	for n, want := range map[int]error{MaxElements - 3: nil, MaxElements - 2: ErrTooManyElements} {
		doc, err := parse([]byte(strings.Repeat("<p>", n)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := scanDocument(t.Context(), doc, documentURL, MaxElements); !errors.Is(err, want) {
			t.Errorf("%d elements: %v, want %v", n+3, err, want)
		}
	}
}

// TestPrepare_CapsTheElements checks that a document at the element cap
// prepares whole and that one past it is refused before anything is built,
// at a cap lowered through the options: a document at [MaxElements] takes
// seconds to cascade, and TestScanDocument_CapsTheElements holds that
// count.
func TestPrepare_CapsTheElements(t *testing.T) {
	t.Parallel()
	const capped = 1024
	o, err := Options{maxElements: capped}.withDefaults()
	if err != nil {
		t.Fatal(err)
	}
	// html, head and body come with the parser.
	_, err = prepare(context.Background(), []byte(strings.Repeat("<p>", capped-2)), o, nil)
	if !errors.Is(err, ErrTooManyElements) {
		t.Fatalf("past the cap: %v", err)
	}
	p, err := prepare(context.Background(), []byte(strings.Repeat("<p>", capped-3)), o, nil)
	if err != nil {
		t.Fatalf("at the cap: %v", err)
	}
	if p == nil {
		t.Fatal("at the cap: nothing prepared")
	}
}

// TestOptions_ElementCapNeverRaisesTheCap checks that the options lower the
// element cap and never raise it past [MaxElements].
func TestOptions_ElementCapNeverRaisesTheCap(t *testing.T) {
	t.Parallel()
	for set, want := range map[int]int{0: MaxElements, 10: 10, MaxElements + 1: MaxElements, -1: MaxElements} {
		if got := (Options{maxElements: set}).elementCap(); got != want {
			t.Errorf("maxElements %d: cap %d, want %d", set, got, want)
		}
	}
}

func TestResources_CapImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	r := newResources(fstest.MapFS{"huge.png": {Data: pngHeader(t, 1<<14, 1<<13)}})
	if _, err := r.picture(ctx, rootPrefix+"huge.png"); !errors.Is(err, errTooManyPx) || warningCode(err) != warn.LimitReached {
		t.Errorf("huge picture: %v", err)
	}

	files := fstest.MapFS{}
	for i := range 2 {
		files[fmt.Sprintf("%d.png", i)] = &fstest.MapFile{Data: pngHeader(t, 1<<13, 1<<13)}
	}
	// A picture passes the pixel cap, then fails to decode its missing
	// pixels and gives its charge back; one the memory budget cannot hold
	// is refused before it is decoded.
	r = newResources(files)
	if _, err := r.picture(ctx, rootPrefix+"0.png"); err == nil || errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("picture under the budget: %v", err)
	}
	mctx, mem, done := limits.WithBudget(ctx, models.Limits{MaxConvertMemoryBytes: 1 << 27})
	defer done()
	// The budget holds the file's bytes, never its pixels.
	if _, err := r.picture(mctx, rootPrefix+"1.png"); err == nil || !errors.Is(mem.Err(), limits.ErrMemoryBudget) || mem.Held() >= 1<<10 {
		t.Errorf("past the memory budget: %v, budget %v, held %d", err, mem.Err(), mem.Held())
	}

	r = newResources(nil)
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes(t, 1, 1))
	for i := range limits.MaxConvertPictures {
		if _, err := r.picture(ctx, fmt.Sprintf("%s#%d", data, i)); err != nil {
			t.Fatalf("image %d: %v", i, err)
		}
	}
	if _, err := r.picture(ctx, data+"#again"); !errors.Is(err, errTooManyImgs) {
		t.Errorf("past the image cap: %v", err)
	}
	if _, err := r.picture(ctx, data+"#0"); err != nil {
		t.Errorf("a decoded picture is served again past the cap: %v", err)
	}
}

func TestResources_CapBytes(t *testing.T) {
	t.Parallel()
	r := newResources(zeroFS{})
	if _, err := r.fetch(context.Background(), rootPrefix+"endless.css"); !errors.Is(err, errTooLarge) {
		t.Errorf("endless file: %v", err)
	}
	r.read = maxTotalResourceBytes - 10
	r.fsys = fstest.MapFS{"a.css": {Data: make([]byte, 11)}, "b.css": {Data: make([]byte, 1)}}
	if _, err := r.fetch(context.Background(), rootPrefix+"a.css"); !errors.Is(err, errBudget) {
		t.Errorf("past the budget: %v", err)
	}
	if _, err := r.fetch(context.Background(), rootPrefix+"b.css"); !errors.Is(err, errBudget) {
		t.Errorf("after the budget: %v", err)
	}
}

func TestDecodeDataURL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"data:text/css,p%7Bcolor:red%7D", "p{color:red}", true},
		{"data:text/css;base64,cHt9", "p{}", true},
		{"data:;base64,cH t9\n", "p{}", true},
		{"data:;base64,cHt9==", "p{}", true},
		{"data:text/css", "", false},
		{"data:;base64,!!!", "", false},
	} {
		got, err := decodeDataURL(tc.in)
		if (err == nil) != tc.ok || string(got) != tc.want {
			t.Errorf("%q: %q, %v", tc.in, got, err)
		}
	}
}

func TestPrepare_ScopesHeaderCells(t *testing.T) {
	t.Parallel()
	p := prep(t, `<table><thead><tr><th id=h1>a<th id=h2 scope=row>b</thead>`+
		`<tbody><tr><th id=r1>r<td>x<tr><th id=c1>c<th id=c2>d<tr><td>y<th id=c3 scope=colgroup>z</tbody></table>`, Options{})
	for id, want := range map[string]typeset.TableScope{
		"h1": typeset.ScopeColumn, "h2": typeset.ScopeRow, "r1": typeset.ScopeRow,
		"c1": typeset.ScopeColumn, "c2": typeset.ScopeColumn, "c3": typeset.ScopeColumn,
	} {
		if got := byID(t, p.root, id).Scope; got != want {
			t.Errorf("%s: scope %d, want %d", id, got, want)
		}
	}
}

func TestPrepare_NumbersListElements(t *testing.T) {
	t.Parallel()
	p := prep(t, `<style>.r{list-style-type:upper-roman}.n{list-style:none}.g{list-style-type:lower-greek}</style>`+
		`<ul id=u><li>a</ul><ol id=o><li>a</ol><ol id=r class=r><li>a</ol><ul id=n class=n><li>a</ul>`+
		`<ol id=g class=g><li>a</ol><ul id=ug class=g><li>a</ul><dl id=d><dt>t<dd>d</dl>`, Options{})
	for id, want := range map[string]typeset.ListNumbering{
		"u": typeset.NumberingDisc, "o": typeset.NumberingDecimal, "r": typeset.NumberingUpperRoman,
		"n": typeset.NumberingNone, "g": typeset.NumberingOrdered, "ug": typeset.NumberingUnordered,
		"d": typeset.NumberingDescription,
	} {
		if got := byID(t, p.root, id).Numbering; got != want {
			t.Errorf("%s: numbering %d, want %d", id, got, want)
		}
	}
}

// TestPrepare_PictureFaults checks that a picture the document names is
// reported by what is wrong with it, as an Office converter reports the
// same picture: one that is absent as a resource that failed, bytes of no
// known format as a format not decoded, and data that does not decode or
// parse as an unreadable picture.
func TestPrepare_PictureFaults(t *testing.T) {
	t.Parallel()
	png := pngBytes(t, 4, 4)
	fsys := fstest.MapFS{
		"unknown.png": {Data: []byte("not a picture")},
		"cut.png":     {Data: png[:len(png)/2]},
		"broken.svg":  {Data: []byte(`<?xml version="1.0"?><html/>`)},
	}
	for _, tc := range []struct {
		src  string
		code warn.Code
		name string
	}{
		{`<img src="gone.png">`, warn.ResourceFailed, "gone.png"},
		{`<img src="unknown.png">`, warn.PictureFormatUnsupported, "unknown.png"},
		{`<img src="cut.png">`, warn.PictureUnreadable, "cut.png"},
		{`<img src="broken.svg">`, warn.PictureUnreadable, "broken.svg"},
	} {
		p := prep(t, "<p>"+tc.src+"</p>", Options{Resources: fsys})
		if !hasWarning(p.warnings, tc.code, tc.name) {
			t.Errorf("%s: warnings %+v, want %s naming %q", tc.src, p.warnings, tc.code, tc.name)
		}
	}
}

// TestPrepare_DerivedAltText holds the descriptions an image without alt
// takes under Options.DerivedAltText: its aria-label, else the caption of
// a figure it alone fills (HTML Standard, 4.8.4.4.11); an svg element its
// aria-label, else its desc. An image beside other content, an image
// marked decorative and one its markup describes take none, and without
// the option no image takes one.
func TestPrepare_DerivedAltText(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"a.png": {Data: pngBytes(t, 4, 4)}}
	src := `<figure><img id=alone src=a.png> <!-- c --><figcaption> Sales  by region </figcaption></figure>` +
		`<figure><figcaption>Shared</figcaption><img id=shared src=a.png><p>text</p></figure>` +
		`<figure><figcaption>Nested</figcaption><a href="#"><img id=nested src=a.png></a></figure>` +
		`<figure><figcaption>Has alt</figcaption><img id=described src=a.png alt="Own"></figure>` +
		`<figure><figcaption>Decor</figcaption><img id=decorative src=a.png alt=""></figure>` +
		`<figure><figcaption>T</figcaption><img id=titled src=a.png title="Title"></figure>` +
		`<p><img id=labelled src=a.png aria-label=" A  label "><img id=bare src=a.png></p>` +
		`<svg id=desc width=10 height=10><desc>A  square</desc><rect width=10 height=10/></svg>` +
		`<svg id=svglabel aria-label="Labelled" width=10 height=10><desc>d</desc></svg>`
	want := map[string]string{
		"alone": "Sales by region", "shared": "", "nested": "", "described": "Own", "decorative": "",
		"titled": "Title", "labelled": "A label", "bare": "", "desc": "A square", "svglabel": "Labelled",
	}
	on := prep(t, src, Options{Resources: fsys, DerivedAltText: true})
	off := prep(t, src, Options{Resources: fsys})
	for id, alt := range want {
		if got := byID(t, on.root, id).Alt; got != alt {
			t.Errorf("%s: alt %q, want %q", id, got, alt)
		}
		offWant := map[string]string{"described": "Own", "titled": "Title"}[id]
		if got := byID(t, off.root, id).Alt; got != offWant {
			t.Errorf("%s without the option: alt %q, want %q", id, got, offWant)
		}
	}
	if r := byID(t, on.root, "decorative").Role; r != "" {
		t.Errorf("an image alt=\"\" marks decorative has role %q", r)
	}
}

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
		{`<dl><dt id=x>a</dl>`, "LI"},
		{`<dl><dd id=x>a</dl>`, "LI"},
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
	if a.Role != "Figure" || a.Alt != "A" || a.Picture.Source.Image == nil {
		t.Errorf("PNG role %q alt %q", a.Role, a.Alt)
	}
	if a.Style == nil || a.Style.Display != css.Inline {
		t.Errorf("PNG keeps its own style: %+v", a.Style)
	}
	if b := byID(t, p.root, "b"); b.Picture == nil || len(b.Picture.Source.JPEG) == 0 || b.Picture.Source.Image != nil {
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
	if !hasWarning(p.warnings, CodeResourceFailed, "img/missing.png") ||
		!hasWarning(p.warnings, CodeResourceFailed, "img/bad.png") {
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
	}{
		{"https://example.com/a.png", fsys, "absolute URLs"},
		{"//example.com/a.png", fsys, "absolute URLs"},
		{"../a.png", fsys, "leaves the resources root"},
		{"sub/../../a.png", fsys, "leaves the resources root"},
		{"/a.png", fsys, "leaves the resources root"},
		{"a.png", nil, "no resources"},
	} {
		o := Options{}
		if tc.fsys != nil {
			o.Resources = tc.fsys
		}
		p := prep(t, `<p id=x><img src="`+tc.src+`" alt="alt">`, o)
		if !hasWarning(p.warnings, CodeResourceRefused, tc.want) {
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
		"f": "#target", "e": "#café", "x": "https://example.com/a?b#c", "r": "docs/page.html", "s": "", "h": "",
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
	abs := prep(t, `<base href="https://cdn.example.com/assets/"><p><img id=i src="assets/a.png">`, Options{Resources: fsys})
	if byID(t, abs.root, "i").Picture == nil {
		t.Errorf("absolute base applied to a resource: %+v", abs.warnings)
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
		"p1": 1, "u1": 7, "big": maxListValue,
	} {
		if got := byID(t, p.root, id).ListValue; got != want {
			t.Errorf("%s: list value %d, want %d", id, got, want)
		}
	}
}

func TestPrepare_ReadsSpans(t *testing.T) {
	t.Parallel()
	p := prep(t, `<table><colgroup id=g span=3></colgroup><col id=c span=0><tr>`+
		`<td id=a colspan=2 rowspan=3>a<td id=b colspan=5000 rowspan=99999>b<td id=n colspan=x rowspan=-1>n</table>`, Options{})
	for _, tc := range []struct {
		id              string
		col, row, group int
	}{
		{"a", 2, 3, 0}, {"b", maxColSpan, maxRowSpan, 0}, {"n", 0, 0, 0}, {"g", 0, 0, 3}, {"c", 0, 0, 0},
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
	if got := codes(p.warnings); len(got) != 1 || got[0] != CodeFormControl {
		t.Errorf("warnings %v, want one form-control", got)
	}
}

func TestPrepare_LeavesEmbeddedContentOut(t *testing.T) {
	t.Parallel()
	p := prep(t, `<p id=x>a<iframe src="x.html">i</iframe><video>v</video><math><mi>m</mi></math>`+
		`<object data="x.swf">fallback</object><canvas>canvas-fallback</canvas><video>v2</video></p>`, Options{})
	if got := treeText(byID(t, p.root, "x")); got != "amfallbackcanvas-fallback" {
		t.Errorf("text %q", got)
	}
	want := []string{CodeUnsupportedElement, CodeUnsupportedElement, CodeUnsupportedElement}
	if got := codes(p.warnings); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("warnings %v, want three unsupported-element (video once)", got)
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
	if !hasWarning(p.warnings, CodeTooDeep, "512") {
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

func TestPrepare_CapsTheElements(t *testing.T) {
	t.Parallel()
	o, err := Options{}.withDefaults()
	if err != nil {
		t.Fatal(err)
	}
	// html, head and body come with the parser.
	_, err = prepare(context.Background(), []byte(strings.Repeat("<p>", MaxElements-3)), o, nil)
	if err != nil {
		t.Fatalf("at the cap: %v", err)
	}
	_, err = prepare(context.Background(), []byte(strings.Repeat("<p>", MaxElements-2)), o, nil)
	if !errors.Is(err, ErrTooManyElements) {
		t.Fatalf("past the cap: %v", err)
	}
}

func TestResources_CapImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	r := newResources(fstest.MapFS{"huge.png": {Data: pngHeader(t, 1<<14, 1<<13)}})
	if _, err := r.picture(ctx, rootPrefix+"huge.png"); !errors.Is(err, errTooManyPx) || warningCode(err) != CodeResourceLimit {
		t.Errorf("huge picture: %v", err)
	}

	files := fstest.MapFS{}
	for i := range 5 {
		files[fmt.Sprintf("%d.png", i)] = &fstest.MapFile{Data: pngHeader(t, 1<<13, 1<<13)}
	}
	r = newResources(files)
	for i := range 4 {
		// Each passes the caps, then fails to decode its missing pixels.
		if _, err := r.picture(ctx, fmt.Sprintf("%s%d.png", rootPrefix, i)); err == nil || errors.Is(err, errPixelBudget) {
			t.Errorf("picture %d: %v", i, err)
		}
	}
	if _, err := r.picture(ctx, rootPrefix+"4.png"); !errors.Is(err, errPixelBudget) {
		t.Errorf("past the pixel budget: %v", err)
	}

	r = newResources(nil)
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes(t, 1, 1))
	for i := range maxImages {
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

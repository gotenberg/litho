package css

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const cascadeDoc = `<!doctype html>
<html><head><title>t</title></head>
<body text="#333333">
  <div id="outer" class="box" style="padding: 1em">
    <p id="p1" class="lead">One <em id="em">two</em> <a id="link" href="x">three</a></p>
    <p id="p2" class="lead" style="color: green !important">Four</p>
    <font id="font" color="red" size="5">five</font>
    <table id="table" border="2" cellpadding="4" width="50%" bgcolor="ffcc00"><tr><td id="cell">x</td></tr></table>
  </div>
  <h1 id="h1">Title</h1>
  <ul><li id="li">a<ul><li id="li2">b</li></ul></li></ul>
</body></html>`

const cascadeCSS = `
html { font-size: 20px }
body { font-family: "Helvetica Neue", Arial, sans-serif; line-height: 1.5 }
.box { margin: 10px 2em; color: navy; border: 2px solid; }
div { color: red }
#outer p { color: maroon !important; }
p.lead { color: olive; font-size: 1.5em; line-height: 150%; }
#p2 { color: purple !important }
em { font-weight: bolder; letter-spacing: 0.1rem }
a:hover { color: pink }
h1 { font-size: 2rem; margin-top: calc(1em + 4px) }
`

func TestCompute_Cascade(t *testing.T) {
	t.Parallel()
	c := compute(t, cascadeDoc, cascadeCSS)

	outer := c.of("outer")
	if !colorEq(outer.Color, rgb(0, 0, 128)) {
		t.Errorf("outer color = %+v, want navy (class beats type)", outer.Color)
	}
	if outer.FontSize != 15 {
		t.Errorf("outer font-size = %v, want 15 (inherited 20px)", outer.FontSize)
	}
	if !dimEq(outer.Margin[Top], pts(7.5)) || !dimEq(outer.Margin[Right], pts(30)) {
		t.Errorf("outer margins = %+v, want 7.5pt and 2em = 30pt", outer.Margin)
	}
	if !dimEq(outer.Padding[Left], pts(15)) {
		t.Errorf("outer padding = %+v, want 1em = 15pt from the style attribute", outer.Padding)
	}
	if outer.Border[Top].Width != 1.5 || outer.Border[Top].Style != Solid || !colorEq(outer.Border[Top].Color, outer.Color) {
		t.Errorf("outer border = %+v, want 1.5pt solid currentColor", outer.Border[Top])
	}
	if got := outer.FontFamilies; !reflect.DeepEqual(got, []string{"Helvetica Neue", "Arial", "sans-serif"}) {
		t.Errorf("font families = %q", got)
	}
	if outer.LineHeight != (Dimension{Kind: Number, Value: 1.5}) {
		t.Errorf("line-height = %+v, want the number 1.5 inherited", outer.LineHeight)
	}

	p1 := c.of("p1")
	if !colorEq(p1.Color, rgb(128, 0, 0)) {
		t.Errorf("p1 color = %+v, want maroon (!important)", p1.Color)
	}
	if p1.FontSize != 22.5 || p1.LineHeight != Pts(33.75) {
		t.Errorf("p1 font-size %v line-height %+v, want 22.5 and 33.75pt", p1.FontSize, p1.LineHeight)
	}
	if !dimEq(p1.Margin[Top], pts(22.5)) {
		t.Errorf("p1 margin-top = %+v, want the UA 1em = 22.5pt", p1.Margin[Top])
	}
	if p1.Display != Block {
		t.Errorf("p1 display = %v", p1.Display)
	}

	p2 := c.of("p2")
	if !colorEq(p2.Color, rgb(0, 128, 0)) {
		t.Errorf("p2 color = %+v, want green (important style attribute)", p2.Color)
	}

	em := c.of("em")
	if em.FontWeight != 700 || em.FontStyle != FontItalic {
		t.Errorf("em weight %d style %v, want bolder 700 and UA italic", em.FontWeight, em.FontStyle)
	}
	if !dimEq(em.LetterSpacing, pts(1.5)) {
		t.Errorf("em letter-spacing = %+v, want 0.1rem = 1.5pt", em.LetterSpacing)
	}
	if em.Display != Inline {
		t.Errorf("em display = %v", em.Display)
	}

	link := c.of("link")
	if !colorEq(link.Color, rgb(0, 0, 0xee)) || link.TextDecoration != Underline {
		t.Errorf("link color %+v decoration %v, want UA :link blue underline (:hover never matches)", link.Color, link.TextDecoration)
	}

	font := c.of("font")
	if !colorEq(font.Color, rgb(255, 0, 0)) || !near(font.FontSize, 18) {
		t.Errorf("font color %+v size %v, want red and x-large = 18pt", font.Color, font.FontSize)
	}

	table := c.of("table")
	if table.Border[Left].Width != 1.5 || table.Border[Left].Style != Outset {
		t.Errorf("table border = %+v, want 2px outset", table.Border[Left])
	}
	if !dimEq(table.Width, pct(0.5)) || !colorEq(table.BackgroundColor, rgb(0xff, 0xcc, 0)) {
		t.Errorf("table width %+v background %+v", table.Width, table.BackgroundColor)
	}
	if table.BorderSpacing != [2]float64{1.5, 1.5} || table.Display != Table {
		t.Errorf("table spacing %v display %v", table.BorderSpacing, table.Display)
	}
	cell := c.of("cell")
	if !dimEq(cell.Padding[Top], pts(3)) || cell.Border[Top].Style != Inset || cell.Border[Top].Width != 0.75 {
		t.Errorf("cell padding %+v border %+v, want cellpadding 4px and 1px inset", cell.Padding[Top], cell.Border[Top])
	}
	if cell.Display != TableCell || cell.VerticalAlign.Kind != Middle {
		t.Errorf("cell display %v valign %v", cell.Display, cell.VerticalAlign)
	}

	h1 := c.of("h1")
	if h1.FontSize != 30 || h1.FontWeight != 700 {
		t.Errorf("h1 size %v weight %d, want 2rem = 30pt bold", h1.FontSize, h1.FontWeight)
	}
	if !dimEq(h1.Margin[Top], pts(33)) {
		t.Errorf("h1 margin-top = %+v, want calc(1em + 4px) = 33pt", h1.Margin[Top])
	}
	if !colorEq(h1.Color, rgb(0x33, 0x33, 0x33)) {
		t.Errorf("h1 color = %+v, want the body text hint", h1.Color)
	}

	if li := c.of("li"); li.Display != ListItem || li.ListStyleType != Disc {
		t.Errorf("li display %v type %v", li.Display, li.ListStyleType)
	}
	if li2 := c.of("li2"); li2.ListStyleType != Circle {
		t.Errorf("nested li type %v, want circle", li2.ListStyleType)
	}
	if s := c.c.Style(byTag(c.doc, "head")); s == nil || s.Display != DisplayNone {
		t.Errorf("head display = %+v", s)
	}
	if s := c.c.Style(byTag(c.doc, "html")); s.FontSize != 15 {
		t.Errorf("root font size %v", s.FontSize)
	}
}

func TestCompute_Deterministic(t *testing.T) {
	t.Parallel()
	doc := parseHTML(t, cascadeDoc)
	css := cascadeCSS + `.box { background: conic-gradient(red, blue); display: flex; foo: bar; --x: 1px; width: var(--x) }`
	r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: mustParse(t, css), Origin: Author}})
	a, err := r.Compute(context.Background(), doc, ComputeOptions{Hints: HTMLHints})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Compute(context.Background(), doc, ComputeOptions{Hints: HTMLHints})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Warnings(), b.Warnings()) {
		t.Errorf("warnings differ:\n%v\n%v", a.Warnings(), b.Warnings())
	}
	if len(a.styles) != len(b.styles) {
		t.Fatalf("style counts differ")
	}
	for n, s := range a.styles {
		if !reflect.DeepEqual(s, b.styles[n]) {
			t.Errorf("style of <%s> differs", n.Data)
		}
	}
	for _, want := range []struct{ code, part string }{
		{"unsupported-css-value", "conic-gradient"},
		{"unsupported-css-value", "display: flex"},
		{"unsupported-css-property", `"foo"`},
	} {
		if !hasWarning(a.Warnings(), want.code, want.part) {
			t.Errorf("no %s warning about %s in %v", want.code, want.part, a.Warnings())
		}
	}
}

func TestResolver_ConcurrentCompute(t *testing.T) {
	t.Parallel()
	doc := parseHTML(t, cascadeDoc)
	r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: mustParse(t, cascadeCSS+":root{--x:1px} p{margin:var(--x)}"), Origin: Author}})
	want, err := r.Compute(context.Background(), doc, ComputeOptions{Hints: HTMLHints})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			got, err := r.Compute(context.Background(), doc, ComputeOptions{Hints: HTMLHints})
			if err != nil {
				t.Error(err)
				return
			}
			for n, s := range want.styles {
				if !reflect.DeepEqual(s, got.styles[n]) {
					t.Errorf("style of <%s> differs under concurrency", n.Data)
				}
			}
		})
	}
	wg.Wait()
}

func TestCompute_ContextAndBudget(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doc := parseHTML(t, "<div>"+strings.Repeat("<p>x</p>", 300)+"</div>")
	r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}})
	if _, err := r.Compute(ctx, doc, ComputeOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled Compute: %v", err)
	}
	if c, err := r.Compute(context.Background(), nil, ComputeOptions{}); err != nil || c.Style(doc) != nil {
		t.Errorf("nil root: %v", err)
	}
	if c, err := r.Compute(context.Background(), byTag(doc, "div"), ComputeOptions{}); err != nil || c.Style(byTag(doc, "p")) == nil || c.Style(byTag(doc, "body")) != nil {
		t.Errorf("element root: %v", err)
	}
}

package css

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

func TestMedia_Queries(t *testing.T) {
	t.Parallel()
	// a4 is 793.7 by 1122.5 CSS pixels.
	tests := []struct {
		q    string
		want bool
	}{
		{"", true},
		{"all", true},
		{"print", true},
		{"PRINT", true},
		{"screen", false},
		{"not screen", true},
		{"not print", false},
		{"only print", true},
		{"tv", false},
		{"screen, print", true},
		{"print and (min-width: 700px)", true},
		{"print and (min-width: 800px)", false},
		{"(max-width: 50em)", true},
		{"(max-width: 40em)", false},
		{"(min-height: 29cm)", true},
		{"(orientation: portrait)", true},
		{"(orientation: landscape)", false},
		{"(width >= 700px)", true},
		{"(700px < width < 800px)", true},
		{"(width < 700px)", false},
		{"(min-aspect-ratio: 1/2)", true},
		{"(min-aspect-ratio: 1/1)", false},
		{"(prefers-color-scheme: dark)", false},
		{"(hover: none)", true},
		{"(color)", true},
		{"(unknown-feature: 1)", false},
		{"not (min-width: 800px)", true},
		{"(min-width: 800px) or (orientation: portrait)", true},
		{"(min-width: 100px) and (max-width: 200px)", false},
		{"print and", false},
		{"print and (min-width: 800px), screen", false},
		{"@media", false},
		{"garbage(, print", false},
	}
	for _, tt := range tests {
		l, _ := parseComponentValues(tt.q)
		if got := evalMediaList(l, a4); got != tt.want {
			t.Errorf("@media %s = %v, want %v", tt.q, got, tt.want)
		}
	}
	l, _ := parseComponentValues("screen")
	if !evalMediaList(l, Media{Type: "screen"}) {
		t.Errorf("screen does not match a screen medium")
	}
}

// TestMedia_UnknownIsNeitherTrueNorFalse locks the three-valued logic of
// Media Queries 4, 3.2: an unknown feature, a value its feature does not
// take and a <general-enclosed> are unknown, not unknown is unknown, and
// a query that is unknown matches nothing.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156's matchMedia is false for each of
//     "(unknown)", "not (unknown)", "not (width: foo)", "not (hover:
//     foo)", "not (orientation: foo)", "not foo(bar)", "not (1px <
//     unknown)" and "not ((unknown) and (color))", and true for
//     "(unknown) or (color)" and "not unknown".
func TestMedia_UnknownIsNeitherTrueNorFalse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		q    string
		want bool
	}{
		{"(unknown)", false},
		{"not (unknown)", false},
		{"not (unknown: 1px)", false},
		{"not (width: foo)", false},
		{"not (min-width: 1px 2px)", false},
		{"not (hover: foo)", false},
		{"not (orientation: foo)", false},
		{"not (color: foo)", false},
		{"not foo(bar)", false},
		{"not (foo(bar))", false},
		{"not (1px < unknown)", false},
		{"not (width < 1foo)", false},
		{"(unknown) or (color)", true},
		{"not ((unknown) or (color))", false},
		{"(unknown) and (color)", false},
		{"not ((unknown) and (color))", false},
		{"print and not (unknown)", false},
		{"not unknown", true},
		{"not ((color) bad)", false},
		{"((color) bad) or (color)", true},
		{"(monochrome)", false},
		{"not (monochrome)", true},
		{"not (hover: hover)", true},
	}
	for _, tt := range tests {
		l, _ := parseComponentValues(tt.q)
		if got := evalMediaList(l, a4); got != tt.want {
			t.Errorf("@media %s = %v, want %v", tt.q, got, tt.want)
		}
	}
	l, _ := parseComponentValues("not print and (unknown)")
	if !evalMediaList(l, Media{Type: "screen"}) {
		t.Errorf("not print and (unknown) does not match a screen medium")
	}
}

func TestSupports_Conditions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		c    string
		want bool
	}{
		{"(display: block)", true},
		{"(display: flex)", true},
		{"(gap: 1px)", true},
		{"(color: notacolor)", false},
		{"(scroll-snap-type: x)", false},
		{"(--x: anything)", true},
		{"(width: var(--w))", true},
		// CSS Variables 1, 2 and 3: a var() fallback and a custom
		// property value are <declaration-value>, which holds no
		// top-level "!" or ";".
		{"(color: var(--a,!))", false},
		{"(color: var(--a,;))", false},
		{"(color: var(--a);)", false},
		{"(color: var(--a) !important !important)", false},
		{"(--a: var(--b,!important))", false},
		{"(--a: var(--b) !important !important)", false},
		{"(color: var(--a,(!)))", true},
		{"not (scroll-snap-type: x)", true},
		{"(color: red) and (width: 1px)", true},
		{"(color: red) and (scroll-snap-type: x)", false},
		{"(scroll-snap-type: x) or (color: red)", true},
		{"((color: red) or (scroll-snap-type: x)) and (width: 0)", true},
		{"selector(a > b)", true},
		{"selector(a:has(b))", true},
		{"selector(a:heading)", false},
		{"selector(::column)", true},
		{"(color: red) and (width: 1px) or (scroll-snap-type: x)", false},
		{"font-tech(color-COLRv1)", false},
		// CSS Conditional 3, 6: a parenthesised value that is no
		// condition is a <general-enclosed>, false.
		{"((color: green) bad) or (color: green)", true},
		{"not ((color: green) bad)", true},
		// CSS Conditional 4, 2: one <complex-selector>, every part of
		// it known, forgiving lists included.
		{"selector(div, div)", false},
		{"selector(:is(.a))", true},
		{"selector(:is(:foo, .a))", false},
		{"selector(:where(:foo, .a))", false},
		{"selector(:not(:foo, .a))", false},
		{"selector(:has(:foo, .a))", false},
		{"selector(a :is(b, :where(c, :foo)))", false},
		// CSS Conditional 5, 2.2: a <font-format> keyword litho reads.
		{"font-format(opentype)", true},
		{"font-format(TrueType)", true},
		{"font-format(woff2)", true},
		{"font-format(collection)", true},
		{"font-format(svg)", false},
		{"font-format(embedded-opentype)", false},
		{"font-format(truetype-variations)", false},
		{"font-format(\"woff\")", false},
		{"font-format()", false},
		{"font-format(truetype opentype)", false},
		{"font-format(truetype, opentype)", false},
		// A <font-tech> keyword litho supports.
		{"font-tech(features-opentype)", true},
		{"font-tech(color-COLRv0)", true},
		{"font-tech(palettes)", true},
		{"font-tech(variations)", true},
		{"font-tech(color-svg)", false},
		{"font-tech(features-opentype, color-COLRv0)", false},
		{"font-tech(features-opentype color-COLRv0)", false},
		{"font-tech(\"features-opentype\")", false},
		{"font-tech()", false},
		// CSS Conditional 5, 2.3: an at-rule litho reads.
		{"at-rule(@supports)", true},
		{"at-rule( @media )", true},
		{"at-rule(@SUPPORTS)", true},
		{"at-rule(@support\\73)", true},
		{"at-rule(@top-left)", true},
		{"at-rule(@swash)", true},
		{"at-rule(@charset)", false},
		{"at-rule(@foo)", false},
		{"at-rule(supports)", false},
		{"at-rule(@supports;)", false},
		{"at-rule(@media) and (color: red)", true},
		{"named-feature(foo)", false},
	}
	for _, tt := range tests {
		l, _ := parseComponentValues(tt.c)
		if got := evalSupports(l, nil); got != tt.want {
			t.Errorf("@supports %s = %v, want %v", tt.c, got, tt.want)
		}
	}
}

func TestAtRules_ConditionalGroups(t *testing.T) {
	t.Parallel()
	css := `
@charset "utf-8";
@import url(ignored-after-rules.css);
p { color: black }
@media screen { p { color: red } }
@media print { @media (min-width: 100px) { p { color: green } } }
@supports (display: grid) { p { font-size: 30px } }
@supports not (display: grid) { p { font-size: 10px } }
@layer base { p { margin-top: 5px } }
@layer base, utilities;
@keyframes spin { from { color: red } }
@container (min-width: 1px) { p { color: blue } }
@namespace svg url(http://www.w3.org/2000/svg);
@font-feature-values Font { @swash { fancy: 1 } }
`
	c := compute(t, `<p id=p>x</p>`, css)
	p := c.of("p")
	if !colorEq(p.Color, rgb(0, 128, 0)) || p.FontSize != 22.5 || !dimEq(p.Margin[Top], pts(3.75)) {
		t.Errorf("color %+v size %v margin %+v", p.Color, p.FontSize, p.Margin[Top])
	}
	// No ancestor answers the @container rule's query: it is unknown and
	// its rule does not apply (CSS Conditional 5, 5.4).
	if hasWarning(c.c.Warnings(), warn.CSSUnsupported, "@container") {
		t.Errorf("warnings = %v", c.c.Warnings())
	}
	if hasWarning(c.c.Warnings(), warn.CSSUnsupported, "keyframes") {
		t.Errorf("@keyframes warned")
	}
}

func TestPage_Cascade(t *testing.T) {
	t.Parallel()
	css := `
@page { size: A4; margin: 2cm }
@page :first { margin-top: 4cm }
@page :left { margin-left: 3cm; margin-right: 10% }
@page :right { margin-right: auto }
@page wide { size: A4 landscape }
@page wide:first { margin: 0 !important }
@page { margin-bottom: 1in }
@page :first { margin-bottom: 0.5in }
@media screen { @page { size: letter } }
`
	r := NewResolver(a4, []Sheet{{Sheet: mustParse(t, css), Origin: Author}})
	cm := 72 / 2.54
	first := r.pageOf(t, PageSelector{First: true, Right: true})
	if !near(first.Width, 210*72/25.4) || !near(first.Height, 297*72/25.4) {
		t.Errorf("first page size %v x %v", first.Width, first.Height)
	}
	if !dimEq(first.Margin[Top], pts(4*cm)) || first.Margin[Right].Kind != Auto || !dimEq(first.Margin[Bottom], pts(36)) {
		t.Errorf("first page margins %+v", first.Margin)
	}
	left := r.pageOf(t, PageSelector{Left: true})
	if !dimEq(left.Margin[Left], pts(3*cm)) || !dimEq(left.Margin[Right], pct(0.1)) || !dimEq(left.Margin[Top], pts(2*cm)) || !dimEq(left.Margin[Bottom], pts(72)) {
		t.Errorf("left page margins %+v", left.Margin)
	}
	wide := r.pageOf(t, PageSelector{Name: "wide", First: true})
	if wide.Width <= wide.Height || !dimEq(wide.Margin[Top], pts(0)) || !dimEq(wide.Margin[Bottom], pts(0)) {
		t.Errorf("wide first page %+v", wide)
	}
	none := NewResolver(a4, nil).pageOf(t, PageSelector{})
	if none.Width != 0 || none.Margin[Top].Kind != Auto {
		t.Errorf("no rules page %+v", none)
	}
	tests := []struct {
		v      string
		w, h   float64
		orient Orientation
		ok     bool
	}{
		{"auto", 0, 0, OrientationAuto, true},
		{"letter", 612, 792, OrientationAuto, true},
		{"landscape legal", 1008, 612, OrientationAuto, true},
		{"landscape", 0, 0, Landscape, true},
		{"8in 5in", 576, 360, OrientationAuto, true},
		{"10cm", 10 * cm, 10 * cm, OrientationAuto, true},
		{"A4 A5", 0, 0, OrientationAuto, false},
		{"0 1in", 0, 0, OrientationAuto, false},
		{"50%", 0, 0, OrientationAuto, false},
	}
	for _, tt := range tests {
		s, ok := parsePageSize(oneValue(tt.v))
		if ok != tt.ok || ok && (!near(s.w, tt.w) || !near(s.h, tt.h) || s.orient != tt.orient) {
			t.Errorf("size: %s = %+v %v", tt.v, s, ok)
		}
	}
}

func TestFontFace_Collected(t *testing.T) {
	t.Parallel()
	css := `
@font-face {
  font-family: "Open Sans";
  src: local("Open Sans Regular"), url(fonts/os.woff2) format("woff2"), url('fonts/os.ttf') format(truetype);
  font-weight: 300 700;
  font-style: italic;
  unicode-range: U+0000-00FF, U+0131, U+0152-0153, U+02??;
  font-display: swap;
}
@font-face { font-family: NoSrc; }
@font-face { src: url(x.woff); }
@media screen { @font-face { font-family: Screen; src: url(s.woff) } }
`
	s, err := Parse(context.Background(), []byte(css), ParseOptions{BaseURL: "https://e.com/css/"})
	if err != nil {
		t.Fatal(err)
	}
	faces := NewResolver(a4, []Sheet{{Sheet: s, Origin: Author}}).FontFaces()
	if len(faces) != 1 {
		t.Fatalf("faces = %+v", faces)
	}
	f := faces[0]
	if f.Family != "Open Sans" || f.WeightMin != 300 || f.WeightMax != 700 || f.Style != FontItalic {
		t.Errorf("face = %+v", f)
	}
	want := []FontSource{{Local: "Open Sans Regular"}, {URL: "https://e.com/css/fonts/os.woff2", Format: "woff2", Formats: []string{"woff2"}}, {URL: "https://e.com/css/fonts/os.ttf", Format: "truetype", Formats: []string{"truetype"}}}
	if fmt.Sprint(f.Sources) != fmt.Sprint(want) {
		t.Errorf("sources = %+v", f.Sources)
	}
	if fmt.Sprint(f.UnicodeRange) != fmt.Sprint([][2]rune{{0, 0xFF}, {0x131, 0x131}, {0x152, 0x153}, {0x200, 0x2FF}}) {
		t.Errorf("unicode-range = %v", f.UnicodeRange)
	}
	for _, bad := range []string{"U+110000", "U+20-10", "U+1-2-3", "U+???????", "V+20"} {
		l, _ := parseComponentValues(bad)
		if r := parseUnicodeRanges(l); r != nil {
			t.Errorf("unicode-range %s = %v", bad, r)
		}
	}
}

func TestImport_FetchesAndGuards(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"https://e.com/main.css":   `@import "a.css"; @import url(print.css) screen; @import url(self.css); @import url(missing.css); p { color: red }`,
		"https://e.com/a.css":      `@import "deep/b.css"; p { font-size: 30px }`,
		"https://e.com/deep/b.css": `p { margin-top: 1px; background: url(bg.png) }`,
		"https://e.com/print.css":  `p { font-size: 99px }`,
		"https://e.com/self.css":   `@import url(self.css); p { margin-bottom: 2px }`,
	}
	var fetched []string
	fetch := func(_ context.Context, u string) ([]byte, string, error) {
		fetched = append(fetched, u)
		if b, ok := files[u]; ok {
			return []byte(b), "", nil
		}
		return nil, "", errors.New("not found")
	}
	s, err := Parse(context.Background(), []byte(files["https://e.com/main.css"]), ParseOptions{BaseURL: "https://e.com/main.css", Fetch: fetch})
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(s.Warnings(), warn.ResourceFailed, "missing.css") || !hasWarning(s.Warnings(), warn.ResourceFailed, "imports itself") {
		t.Errorf("warnings = %v", s.Warnings())
	}
	doc := parseHTML(t, `<p id=p>`)
	c, err := NewResolver(a4, []Sheet{{Sheet: s, Origin: Author}}).Compute(context.Background(), doc, ComputeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p := c.Style(byID(doc, "p"))
	if p.FontSize != 22.5 || !dimEq(p.Margin[Top], pts(0.75)) || !dimEq(p.Margin[Bottom], pts(1.5)) || !colorEq(p.Color, rgb(255, 0, 0)) {
		t.Errorf("p = size %v margins %+v color %+v", p.FontSize, p.Margin, p.Color)
	}
	if p.BackgroundImage != "https://e.com/deep/bg.png" {
		t.Errorf("imported url() resolved to %q", p.BackgroundImage)
	}

	// A chain past maxImportDepth stops with a warning.
	chain := func(_ context.Context, u string) ([]byte, string, error) {
		return []byte(fmt.Sprintf(`@import url(%s-x.css);`, strings.TrimSuffix(u, ".css"))), "", nil
	}
	s, err = Parse(context.Background(), []byte(`@import url(c.css);`), ParseOptions{BaseURL: "https://e.com/", Fetch: chain})
	if err != nil || !hasWarning(s.Warnings(), warn.LimitReached, "@import") {
		t.Errorf("deep chain: %v %v", err, s.Warnings())
	}
	// Without Fetch, imports are skipped.
	if _, err := Parse(context.Background(), []byte(`@import url(c.css); p{}`), ParseOptions{}); err != nil {
		t.Error(err)
	}
}

func TestImport_ResolvesAgainstTheFinalURL(t *testing.T) {
	t.Parallel()
	// moved.css redirects to new/sheet.css: its url() values and its own
	// imports resolve against the URL it came from (CSS Values 4, 4.5.1).
	files := map[string]string{
		"https://e.com/moved.css":     `@import "inner.css"; p { background: url(bg.png) }`,
		"https://e.com/new/inner.css": `p { margin-top: 1px }`,
		"https://e.com/inner.css":     `p { margin-top: 99px }`,
	}
	fetch := func(_ context.Context, u string) ([]byte, string, error) {
		final := ""
		if u == "https://e.com/moved.css" {
			final = "https://e.com/new/sheet.css"
		}
		if b, ok := files[u]; ok {
			return []byte(b), final, nil
		}
		return nil, "", errors.New("not found")
	}
	s, err := Parse(context.Background(), []byte(`@import url(moved.css);`), ParseOptions{BaseURL: "https://e.com/main.css", Fetch: fetch})
	if err != nil {
		t.Fatal(err)
	}
	doc := parseHTML(t, `<p id=p>`)
	c, err := NewResolver(a4, []Sheet{{Sheet: s, Origin: Author}}).Compute(context.Background(), doc, ComputeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p := c.Style(byID(doc, "p"))
	if p.BackgroundImage != "https://e.com/new/bg.png" {
		t.Errorf("url() resolved to %q", p.BackgroundImage)
	}
	if !dimEq(p.Margin[Top], pts(0.75)) {
		t.Errorf("margin-top %+v, want the import next to the final URL", p.Margin[Top])
	}
}

func TestParse_Caps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, err := Parse(ctx, make([]byte, maxStylesheetBytes+1), ParseOptions{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversize: %v", err)
	}
	deep := "p{a:" + strings.Repeat("(", maxNestingDepth+5) + "}"
	if _, err := Parse(ctx, []byte(deep), ParseOptions{}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("deep: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Parse(cctx, []byte("p{}"), ParseOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
	if s, err := Parse(ctx, []byte(`p { color: red`), ParseOptions{}); err != nil || len(s.rules) != 1 {
		t.Errorf("unclosed block: %v", err)
	}
}

func TestParse_RecoversFromErrors(t *testing.T) {
	t.Parallel()
	css := `p { color: red; width: ; height: 10px junk; margin-top: 5px }
	} stray { }
	div { color: red;;; @x; junk }
	@media print { span { color: blue } 
	`
	c := compute(t, `<p id=p></p><span id=s></span>`, css)
	p := c.of("p")
	if !colorEq(p.Color, rgb(255, 0, 0)) || p.Height.Kind != Auto || !dimEq(p.Margin[Top], pts(3.75)) {
		t.Errorf("p = %+v %+v %+v", p.Color, p.Height, p.Margin[Top])
	}
	if !colorEq(c.of("s").Color, rgb(0, 0, 255)) {
		t.Errorf("unclosed @media lost its rule")
	}
}

func TestPage_Background(t *testing.T) {
	t.Parallel()
	css := `
@page { background: linear-gradient(currentcolor, red) no-repeat; background-origin: border-box }
@page :first { background-color: blue; background-size: 1em }
`
	r := NewResolver(a4, []Sheet{{Sheet: mustParse(t, css), Origin: Author}})
	first := r.pageOf(t, PageSelector{First: true})
	bg := first.Background
	if bg == nil {
		t.Fatal("no page background")
	}
	g := bg.BackgroundGradient
	if g == nil || len(g.Stops) != 2 || !colorEq(g.Stops[0].Color, rgb(0, 0, 0)) || !colorEq(g.Stops[1].Color, rgb(255, 0, 0)) {
		t.Errorf("page gradient %+v", g)
	}
	if !colorEq(bg.BackgroundColor, rgb(0, 0, 255)) || bg.BackgroundRepeat != [2]Repeat{RepeatNoRepeat, RepeatNoRepeat} ||
		bg.BackgroundOrigin != BorderEdge || !dimEq(bg.BackgroundSize.Width, pts(12)) {
		t.Errorf("page background %+v %+v %v %+v", bg.BackgroundColor, bg.BackgroundRepeat, bg.BackgroundOrigin, bg.BackgroundSize)
	}
	if bg.Display != initialStyle.Display || bg.FontSize != initialStyle.FontSize {
		t.Error("page background style is not initial outside the background")
	}
	if NewResolver(a4, nil).pageOf(t, PageSelector{}).Background != nil {
		t.Error("background without rules")
	}
}

// TestPage_BackgroundPositionAndLayers locks that the page box takes
// every background longhand (CSS Page 3, 3.3): background-position sets
// both axes, background-position-y alone sets the vertical one, and a
// list of layers keeps each layer.
func TestPage_BackgroundPositionAndLayers(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		css  string
		x, y Dimension
	}{
		{`@page { background: url(a.png) no-repeat right bottom }`, Dimension{Kind: Percent, Value: 1}, Dimension{Kind: Percent, Value: 1}},
		{`@page { background-image: url(a.png); background-position-y: 10px; background-position-x: center }`, Dimension{Kind: Percent, Value: 0.5}, pts(7.5)},
	} {
		bg := NewResolver(a4, []Sheet{{Sheet: mustParse(t, tt.css), Origin: Author}}).pageOf(t, PageSelector{}).Background
		if bg == nil {
			t.Fatalf("%s: no page background", tt.css)
		}
		if !dimEq(bg.BackgroundPosition[0], tt.x) || !dimEq(bg.BackgroundPosition[1], tt.y) {
			t.Errorf("%s: position %+v, want %+v %+v", tt.css, bg.BackgroundPosition, tt.x, tt.y)
		}
	}
	bg := NewResolver(a4, []Sheet{{Sheet: mustParse(t, `@page { background: url(a.png) 0 5px, url(b.png) 0 10px }`), Origin: Author}}).pageOf(t, PageSelector{}).Background
	if bg == nil || bg.BackgroundLayerCount() != 2 {
		t.Fatal("page background of two layers not kept")
	}
	if l := bg.BackgroundLayer(1); !dimEq(l.Position[1], pts(7.5)) {
		t.Errorf("second layer position %+v, want 0 10px", l.Position)
	}
}

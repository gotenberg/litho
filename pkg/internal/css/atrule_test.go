package css

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
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
		l, _ := parseComponentValues(tt.q, maxTokens)
		if got := evalMediaList(l, a4); got != tt.want {
			t.Errorf("@media %s = %v, want %v", tt.q, got, tt.want)
		}
	}
	l, _ := parseComponentValues("screen", maxTokens)
	if !evalMediaList(l, Media{Type: "screen"}) {
		t.Errorf("screen does not match a screen medium")
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
		{"(color: notacolor)", false},
		{"(gap: 1px)", false},
		{"(--x: anything)", true},
		{"(width: var(--w))", true},
		{"not (gap: 1px)", true},
		{"(color: red) and (width: 1px)", true},
		{"(color: red) and (gap: 1px)", false},
		{"(gap: 1px) or (color: red)", true},
		{"((color: red) or (gap: 1px)) and (width: 0)", true},
		{"selector(a > b)", true},
		{"selector(a:has(b))", false},
		{"(color: red) and (width: 1px) or (gap: 1px)", false},
		{"font-tech(color-COLRv1)", false},
	}
	for _, tt := range tests {
		l, _ := parseComponentValues(tt.c, maxTokens)
		if got := evalSupports(l); got != tt.want {
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
	if !hasWarning(c.c.Warnings(), "unsupported-css-at-rule", "@container") {
		t.Errorf("warnings = %v", c.c.Warnings())
	}
	if hasWarning(c.c.Warnings(), "unsupported-css-at-rule", "keyframes") {
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
	first := r.Page(PageSelector{First: true, Right: true})
	if !near(first.Width, 210*72/25.4) || !near(first.Height, 297*72/25.4) {
		t.Errorf("first page size %v x %v", first.Width, first.Height)
	}
	if !dimEq(first.Margin[Top], pts(4*cm)) || first.Margin[Right].Kind != Auto || !dimEq(first.Margin[Bottom], pts(36)) {
		t.Errorf("first page margins %+v", first.Margin)
	}
	left := r.Page(PageSelector{Left: true})
	if !dimEq(left.Margin[Left], pts(3*cm)) || !dimEq(left.Margin[Right], pct(0.1)) || !dimEq(left.Margin[Top], pts(2*cm)) || !dimEq(left.Margin[Bottom], pts(72)) {
		t.Errorf("left page margins %+v", left.Margin)
	}
	wide := r.Page(PageSelector{Name: "wide", First: true})
	if wide.Width <= wide.Height || !dimEq(wide.Margin[Top], pts(0)) || !dimEq(wide.Margin[Bottom], pts(0)) {
		t.Errorf("wide first page %+v", wide)
	}
	none := NewResolver(a4, nil).Page(PageSelector{})
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
	want := []FontSource{{Local: "Open Sans Regular"}, {URL: "https://e.com/css/fonts/os.woff2", Format: "woff2"}, {URL: "https://e.com/css/fonts/os.ttf", Format: "truetype"}}
	if fmt.Sprint(f.Sources) != fmt.Sprint(want) {
		t.Errorf("sources = %+v", f.Sources)
	}
	if fmt.Sprint(f.UnicodeRange) != fmt.Sprint([][2]rune{{0, 0xFF}, {0x131, 0x131}, {0x152, 0x153}, {0x200, 0x2FF}}) {
		t.Errorf("unicode-range = %v", f.UnicodeRange)
	}
	for _, bad := range []string{"U+110000", "U+20-10", "U+1-2-3", "U+???????", "V+20"} {
		l, _ := parseComponentValues(bad, maxTokens)
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
	fetch := func(_ context.Context, u string) ([]byte, error) {
		fetched = append(fetched, u)
		if b, ok := files[u]; ok {
			return []byte(b), nil
		}
		return nil, errors.New("not found")
	}
	s, err := Parse(context.Background(), []byte(files["https://e.com/main.css"]), ParseOptions{BaseURL: "https://e.com/main.css", Fetch: fetch})
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(s.Warnings(), "css-import-failed", "missing.css") || !hasWarning(s.Warnings(), "css-import-failed", "imports itself") {
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
	chain := func(_ context.Context, u string) ([]byte, error) {
		return []byte(fmt.Sprintf(`@import url(%s-x.css);`, strings.TrimSuffix(u, ".css"))), nil
	}
	s, err = Parse(context.Background(), []byte(`@import url(c.css);`), ParseOptions{BaseURL: "https://e.com/", Fetch: chain})
	if err != nil || !hasWarning(s.Warnings(), "css-limit", "@import") {
		t.Errorf("deep chain: %v %v", err, s.Warnings())
	}
	// Without Fetch, imports are skipped.
	if _, err := Parse(context.Background(), []byte(`@import url(c.css); p{}`), ParseOptions{}); err != nil {
		t.Error(err)
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

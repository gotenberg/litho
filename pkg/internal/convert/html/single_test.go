package html

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// singleCutWarned reports whether doc carries the warning of content left
// out past the bound of a single page.
func singleCutWarned(doc *typeset.Document) bool {
	for _, w := range doc.Warnings {
		if w.Code == warn.LimitReached && w.Effect == warn.Omitted && strings.Contains(w.Message, "single page") {
			return true
		}
	}
	return false
}

// TestConvert_SinglePageKeepsTheLastLine holds the single page's height:
// the document laid out at the page area's width, as it prints, so text
// that wraps into more lines there than on a wider screen keeps its last
// line, with no warning.
func TestConvert_SinglePageKeepsTheLastLine(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	words := strings.Repeat("The paragraph runs on in words of several lengths, wrapping at the page area. ", 6)
	for range 40 {
		b.WriteString("<p>" + words + "</p>")
	}
	b.WriteString("<p>theend</p>")
	o := Options{SinglePage: true, MarginTop: 27.75, MarginRight: 27.75, MarginBottom: 27.75, MarginLeft: 27.75}
	doc := conv(t, b.String(), o)
	if len(doc.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(doc.Pages))
	}
	if !strings.Contains(pageText(doc.Pages[0]), "theend") {
		t.Errorf("the last line is left out of the page %v tall", doc.Pages[0].Height)
	}
	if singleCutWarned(doc) {
		t.Errorf("a warning of content past the bound: %+v", doc.Warnings)
	}
	paged := conv(t, b.String(), Options{MarginTop: 27.75, MarginRight: 27.75, MarginBottom: 27.75, MarginLeft: 27.75})
	area := DefaultPageHeight - 2*27.75
	if want := float64(len(paged.Pages)-1) * area; doc.Pages[0].Height < want {
		t.Errorf("page %v tall, shorter than the %d full page areas the paged print fills", doc.Pages[0].Height, len(paged.Pages)-1)
	}
}

// TestConvert_SinglePage holds the size of a single page: the paper's
// width in its orientation, the height of the content as it prints plus
// the top and bottom margins, however short, and the initial containing
// block of the paged print, so a root of height 100%, a box positioned at
// bottom 0 and a block 100vh tall print on a page as tall as the paper.
func TestConvert_SinglePage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		body  string
		o     Options
		w, h  float64
		scale float64
	}{
		{
			"short content makes a short page", `<div style="height:50pt"></div>`,
			Options{MarginTop: 7, MarginBottom: 3},
			DefaultPageWidth, 60, 1,
		},
		{
			"no forced break ends the page", `<div style="height:400pt"></div><div style="break-before:right;height:300pt"></div><div style="page-break-after:always;height:50pt"></div><div style="height:50pt"></div>`,
			Options{MarginTop: 7, MarginBottom: 3},
			DefaultPageWidth, 810, 1,
		},
		{
			"a later named page changes nothing", `<style>@page wide{size:800pt 600pt;margin:0}</style><div style="height:40pt"></div><div style="page:wide;height:60pt"></div>`,
			Options{MarginTop: 7, MarginBottom: 3},
			DefaultPageWidth, 110, 1,
		},
		{
			"landscape keeps the paper's width", `<div style="height:40pt"></div>`,
			Options{Landscape: true, MarginLeft: 10, MarginRight: 20},
			DefaultPageHeight, 40, 1,
		},
		{
			"landscape shrinks wide content to the paper", `<div style="width:1000pt;height:40pt"></div>`,
			Options{Landscape: true, MarginLeft: 10, MarginRight: 20},
			DefaultPageHeight, 40 * (DefaultPageHeight - 30) / 1000, (DefaultPageHeight - 30) / 1000,
		},
		{
			"portrait shrinks wide content and its height follows", `<div style="width:800pt;height:300pt"></div>`,
			Options{},
			DefaultPageWidth, 300 * DefaultPageWidth / 800, DefaultPageWidth / 800,
		},
		{
			"a wide table shrinks and its height follows", `<table style="border-spacing:0;width:800pt"><tr><td style="padding:0;height:300pt">x</td></tr></table>`,
			Options{MarginTop: 5, MarginBottom: 5},
			DefaultPageWidth, 10 + 300*DefaultPageWidth/800, DefaultPageWidth / 800,
		},
		{
			"the scale draws the content smaller", `<div style="height:100pt"></div>`,
			Options{Scale: 0.5, MarginTop: 10, MarginBottom: 10},
			DefaultPageWidth, 70, 0.5,
		},
		{
			"root percentage heights measure the paper's page area", `<style>html,body{height:100%}</style><p style="margin:0">a</p>`,
			Options{MarginTop: 7, MarginBottom: 3},
			DefaultPageWidth, DefaultPageHeight, 1,
		},
		{
			"a box positioned at bottom 0 measures the paper's page area", `<div style="position:absolute;top:0;bottom:0;width:10pt"></div>`,
			Options{MarginTop: 7, MarginBottom: 3},
			DefaultPageWidth, DefaultPageHeight, 1,
		},
		{
			"100vh is the paper's page area", `<div style="height:100vh"></div>`,
			Options{MarginTop: 7, MarginBottom: 3},
			DefaultPageWidth, DefaultPageHeight, 1,
		},
		{
			"overflow of a box that fits counts", `<div style="height:100pt"><div style="height:600pt"></div></div>`,
			Options{},
			DefaultPageWidth, 600, 1,
		},
		{
			"content a box clips takes no room", `<div style="height:100pt;overflow:hidden"><div style="height:600pt"></div></div>`,
			Options{},
			DefaultPageWidth, 100, 1,
		},
		{
			"past the 14,400 points of Annex C.2", `<div style="height:20000pt"></div>`,
			Options{},
			DefaultPageWidth, 20000, 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.o.SinglePage = true
			doc := conv(t, `<body style="margin:0">`+tt.body, tt.o)
			if len(doc.Pages) != 1 {
				t.Fatalf("%d pages, want 1", len(doc.Pages))
			}
			pg := doc.Pages[0]
			if !near6(pg.Width, tt.w) || !near6(pg.Height, tt.h) {
				t.Errorf("page %vx%v, want %vx%v", pg.Width, pg.Height, tt.w, tt.h)
			}
			if m := placementOf(pg); !near6(m.A, tt.scale) {
				t.Errorf("placement %+v, want a scale of %v", m, tt.scale)
			}
			if !pg.Fitted {
				t.Error("the page is not marked as sized from its content")
			}
		})
	}
}

// TestConvert_SinglePageMediaSeesThePaper holds what the media queries of
// a single page read: the paper's page area, as a paged print of it.
func TestConvert_SinglePageMediaSeesThePaper(t *testing.T) {
	t.Parallel()
	src := `<style>div{height:10pt}@media (min-height:800pt){div{height:30pt}}</style><body style="margin:0"><div></div>`
	doc := conv(t, src, Options{SinglePage: true})
	if h := doc.Pages[0].Height; !near6(h, 30) {
		t.Errorf("page %v tall, want 30: the media height is the paper's page area", h)
	}
}

// TestConvert_SinglePageCutWarns states the single page's bound: content
// laid out past [Options.FittedLimit] is left out of the single page, and
// the conversion says so, as a text document's does, while content past
// the 14,400 points of ISO 32000-1, Annex C.2, an informative limit, stays
// whole. A forced break ends nothing, and warns of nothing.
func TestConvert_SinglePageCutWarns(t *testing.T) {
	t.Parallel()
	src := `<body style="margin:0"><div style="height:20000pt"></div><p>theend</p>`
	whole := conv(t, src, Options{SinglePage: true, MarginTop: 10, MarginBottom: 10})
	if len(whole.Pages) != 1 || whole.Pages[0].Height <= maxPageSide || !whole.Pages[0].Fitted || !strings.Contains(pageText(whole.Pages[0]), "theend") || singleCutWarned(whole) {
		t.Fatalf("%d pages, the first %v tall, fitted %v; want one whole page past %v points", len(whole.Pages), whole.Pages[0].Height, whole.Pages[0].Fitted, maxPageSide)
	}
	// The content is laid out within the bound: below a scale of 1 the
	// page stops at the bound times the scale.
	for _, tc := range []struct{ scale, h float64 }{{1, 15000}, {2, 15000}, {0.5, 7500}} {
		scale := tc.scale
		tall := conv(t, src, Options{SinglePage: true, MarginTop: 10, MarginBottom: 10, FittedLimit: 15000, Scale: scale})
		if len(tall.Pages) != 1 || !near6(tall.Pages[0].Height, tc.h) {
			t.Fatalf("scale %v: %d pages, the first %v tall; want one %v tall", scale, len(tall.Pages), tall.Pages[0].Height, tc.h)
		}
		if strings.Contains(pageText(tall.Pages[0]), "theend") {
			t.Errorf("scale %v: the content past the tallest page is drawn", scale)
		}
		if !singleCutWarned(tall) {
			t.Errorf("scale %v: no warning of the content left out: %+v", scale, tall.Warnings)
		}
	}
	broken := conv(t, `<body style="margin:0"><p>before</p><p style="break-before:page">after</p>`, Options{SinglePage: true})
	if singleCutWarned(broken) || !strings.Contains(pageText(broken.Pages[0]), "after") {
		t.Errorf("a forced break warns of content past the tallest page, or loses what follows it: %+v", broken.Warnings)
	}
}

// TestConvert_SinglePageKeepsWhatFollowsAForcedBreak holds the single
// page as one column nothing ends: a forced page break in the middle of
// the document breaks nothing, its last line is on the page and the page
// is as tall as the whole content, while a column break inside a
// multi-column container still moves its content to the next column.
func TestConvert_SinglePageKeepsWhatFollowsAForcedBreak(t *testing.T) {
	t.Parallel()
	src := `<body style="margin:0"><div style="height:300pt"></div><h1 style="break-before:page;margin:0;height:100pt">middle</h1><div style="height:200pt"></div><p style="margin:0;height:20pt">theend</p>`
	doc := conv(t, src, Options{SinglePage: true, MarginTop: 10, MarginBottom: 10})
	if len(doc.Pages) != 1 || !near6(doc.Pages[0].Height, 640) {
		t.Fatalf("%d pages, the first %v tall; want one 640 tall", len(doc.Pages), doc.Pages[0].Height)
	}
	if text := pageText(doc.Pages[0]); !strings.Contains(text, "middle") || !strings.Contains(text, "theend") {
		t.Errorf("text %q, want what follows the forced break", text)
	}
	cols := conv(t, `<body style="margin:0"><div style="columns:2;column-gap:0;width:400pt"><p style="margin:0">left</p><p style="margin:0;break-before:column">right</p></div>`, Options{SinglePage: true})
	var x float64
	for _, r := range pageTexts(cols.Pages[0]) {
		if strings.Contains(r.text, "right") {
			x = r.x
		}
	}
	if x < 200 {
		t.Errorf("the text after a column break is at x %v, want it in the second column, from 200", x)
	}
}

// TestConvert_SinglePageLaysOutOnce holds the cost of a single page: the
// print is the measure, one layout for a document that fits its page
// area, and one more for one that shrinks to fit, as on the pages.
func TestConvert_SinglePageLaysOutOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		body    string
		layouts int
	}{
		{`<p>a</p><p style="break-before:page">b</p>`, 1},
		{`<div style="width:800pt;height:10pt"></div>`, 2},
	} {
		layouts := 0
		lay := func(ctx context.Context, root *layout.Node, fonts *text.Fonts, opts layout.Options) (*layout.Result, error) {
			layouts++
			return layout.Layout(ctx, root, fonts, opts)
		}
		if _, err := convert(t.Context(), strings.NewReader(tc.body), testFonts(t), Options{SinglePage: true}, lay); err != nil {
			t.Fatal(err)
		}
		if layouts != tc.layouts {
			t.Errorf("%s: %d layouts, want %d", tc.body, layouts, tc.layouts)
		}
	}
}

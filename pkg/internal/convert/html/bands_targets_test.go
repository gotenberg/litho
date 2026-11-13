package html

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// refPages is a document of three pages whose last paragraph is #end.
const refPages = `<style>body { margin: 0 } p { margin: 0; break-after: page } p:last-child { break-after: auto }</style>` +
	`<p>A</p><p>B</p><p id="end">C</p>`

// refBand is a band that shows the page of #end and its own page.
var refBand = Band{
	HTML: `<div style="height: 80pt"><span class="r"></span></div>`,
	CSS:  `.r::after { content: "end " target-counter(url(#end), page) " here " counter(page) " of " counter(pages) " " target-text(url(#end)) }`,
}

func TestBands_CrossReferences(t *testing.T) {
	t.Parallel()
	for _, room := range []Room{RoomMargin, RoomContent} {
		for _, start := range []int{0, 9} {
			b := refBand
			b.Room = room
			o := bandOptions(b)
			o.PageCounterStart = start
			doc := convertDoc(t, refPages, o)
			if len(doc.Pages) != 3 {
				t.Fatalf("room %d: %d pages, want 3", room, len(doc.Pages))
			}
			for i, pg := range doc.Pages {
				want := fmt.Sprintf("end %d here %d of 3 C", start+3, start+i+1)
				if got := find(t, pg, "end "); !strings.HasPrefix(got.text, want) {
					t.Errorf("room %d, start %d, page %d: band reads %q, want %q", room, start, i+1, got.text, want)
				}
			}
		}
	}
}

func TestBands_UnresolvedCrossReferenceWarns(t *testing.T) {
	t.Parallel()
	doc := convertDoc(t, refPages, bandOptions(Band{HTML: `<span class="r">X</span>`, CSS: `.r::after { content: target-counter(url(#none), page) }`}))
	if !hasWarning(doc.Warnings, warn.LinkUnresolved, `"#none"`) {
		t.Errorf("no warning of the missing target in %+v", doc.Warnings)
	}
}

// TestBands_LayoutBound locks the layouts a conversion with a band of
// RoomContent and cross-references that read a page runs at worst: the
// layouts that settle the page numbers, at most maxTargetLayouts, then the
// layout with the grown margins and its own, at most maxTargetLayouts
// again. A layout whose anchors move on every call keeps the numbers from
// settling.
func TestBands_LayoutBound(t *testing.T) {
	t.Parallel()
	src := refPages + `<p>see <span class="x"></span></p><style>.x::after { content: target-counter(url(#end), page) }</style>`
	calls := 0
	unsettled := func(ctx context.Context, root *layout.Node, fonts *text.Fonts, opts layout.Options) (*layout.Result, error) {
		calls++
		res, err := layout.Layout(ctx, root, fonts, opts)
		if err == nil && calls%2 == 0 {
			for id, d := range res.Anchors {
				d.Page = 1
				res.Anchors[id] = d
			}
		}
		return res, err
	}
	b := refBand
	b.Room = RoomContent
	if _, err := convert(context.Background(), strings.NewReader(src), testFonts(t), bandOptions(b), unsettled); err != nil {
		t.Fatal(err)
	}
	if want := 2 * maxTargetLayouts; calls != want {
		t.Errorf("%d layouts, want %d", calls, want)
	}
	calls = 0
	settled := func(ctx context.Context, root *layout.Node, fonts *text.Fonts, opts layout.Options) (*layout.Result, error) {
		calls++
		return layout.Layout(ctx, root, fonts, opts)
	}
	if _, err := convert(context.Background(), strings.NewReader(src), testFonts(t), bandOptions(b), settled); err != nil {
		t.Fatal(err)
	}
	// One layout reads the page numbers, one draws them; the grown margins
	// take one more, whose numbers stand.
	if calls != 3 {
		t.Errorf("%d layouts of a document whose numbers settle, want 3", calls)
	}
}

func TestBands_PageGroups(t *testing.T) {
	t.Parallel()
	src := `<style>
		@page { size: 200pt 100pt; margin: 20pt }
		@page :nth(1 of chapter) { @top-center { content: "first" } }
		@page chapter:nth(2) { @top-center { content: "second" } }
		body { margin: 0; font: 10pt "Test Serif" }
		section { page: chapter; break-before: page }
		div { height: 150pt }
	</style>
	<p>intro</p><section><div></div></section><section><div style="height: 100pt"></div></section>`
	o := Options{PreferCSSPageSize: true, Bands: []Band{
		{HTML: "BAND", Pages: PageSet{{Kind: PageRange, From: 5, To: 5}}},
		{Footer: true, HTML: "CHAPTER", Pages: PageSet{{Kind: PageNamed, Name: "chapter"}}},
	}}
	doc := convertDoc(t, src, o)
	// The page group headers of the document show where no header band
	// takes their margin: page 5, the first of the second chapter, shows
	// the band.
	header := []string{"", "first", "second", "", "BAND", "second"}
	if len(doc.Pages) != len(header) {
		t.Fatalf("%d pages, want %d", len(doc.Pages), len(header))
	}
	for i, w := range header {
		for _, s := range []string{"first", "second", "BAND"} {
			if got := hasLine(doc.Pages[i], s); got != (s == w) {
				t.Errorf("page %d: %q shown %v, want %q", i+1, s, got, w)
			}
		}
		if got := hasLine(doc.Pages[i], "CHAPTER"); got != (i > 0) {
			t.Errorf("page %d: the footer of the chapter pages shown %v", i+1, got)
		}
	}
}

func TestBands_NoOutlineEntries(t *testing.T) {
	t.Parallel()
	src := `<style>p { break-after: page }</style><h1>Doc</h1><p>a</p><h2>Part</h2>`
	doc := convertDoc(t, src, bandOptions(Band{HTML: `<h1>Band heading</h1><span style="bookmark-level: 1; bookmark-label: 'Band label'">x</span>`}))
	var titles []string
	var walk func([]typeset.Outline)
	walk = func(os []typeset.Outline) {
		for _, o := range os {
			titles = append(titles, o.Title)
			walk(o.Kids)
		}
	}
	walk(doc.Outline)
	if strings.Join(titles, "|") != "Doc|Part" {
		t.Errorf("outline %q, want the document's headings alone", titles)
	}
}

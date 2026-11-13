package html

import (
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestNamedStrings_RunningHeaders(t *testing.T) {
	src := `<style>
		@page { size: 400pt 300pt; margin: 40pt 50pt; font: 10pt "Test Serif";
			@top-left { content: string(chapter) }
			@top-right { content: string(chapter, first-except) "|" }
			@bottom-left { content: string(chapter, last) }
			@bottom-right { content: string(chapter, start) "/" string(sec) }
			@top-center { content: element(head) }
		}
		body { margin: 0; font: 10pt "Test Serif"; counter-reset: ch }
		h1 { margin: 0; font-size: 10pt; string-set: chapter "C" counter(ch) " " content(); counter-increment: ch }
		h2 { margin: 0; font-size: 10pt; string-set: sec content(first-letter) }
		p { margin: 0 }
		.brk { break-before: page }
		header { position: running(head) }
	</style>
	<header>Running one</header>
	<h1>Alpha</h1><h2>Xyz</h2><p>text</p>
	<p class=brk>more</p><h1>Beta</h1>
	<header>Running two</header>
	<h1 class=brk>Gamma</h1>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	if len(doc.Pages) != 3 {
		t.Fatalf("%d pages, want 3", len(doc.Pages))
	}
	for i, want := range []struct{ tl, tr, bl, br, tc string }{
		{"C1 Alpha", "|", "C1 Alpha", "C1 Alpha/X", "Running one"},
		{"C2 Beta", "|", "C2 Beta", "C1 Alpha/X", "Running two"},
		{"C3 Gamma", "|", "C3 Gamma", "C3 Gamma/X", "Running two"},
	} {
		pg := doc.Pages[i]
		for _, s := range []string{want.tl, want.bl, want.br, want.tc} {
			if !hasText(pg, s) {
				t.Errorf("page %d: no %q in %+v", i+1, s, pageTexts(pg))
			}
		}
		if tr := find(t, pg, "|"); tr.text != want.tr {
			t.Errorf("page %d: top-right %q, want %q", i+1, tr.text, want.tr)
		}
	}
	if hasText(doc.Pages[0], "Running two") {
		t.Error("the second running element shows on the first page")
	}
}

// TestPagePicks_FirstStartLastFirstExcept locks the four keywords of
// string() and element() (CSS GCPM 3, 1.1.2 and 1.2.2) on pages where the
// assignment starts the page behind its kept top margin (3, 4), behind the
// border and padding of the section it opens (5), sits in the middle (1,
// 6), is absent (2), and is made twice (4). The running element of page 3
// starts its page; those of pages 1, 4 and 6 do not.
func TestPagePicks_FirstStartLastFirstExcept(t *testing.T) {
	const body = `
	<p>Intro</p><div class=r>rMid1</div><h2>Mid1</h2>
	<p class=brk>None</p>
	<section class=brk><div class=r>rTop3</div><h2>Top3</h2><p>text</p></section>
	<h2 class=brk>Top4a</h2><p>p</p><div class=r>r4b</div><h2>Twice4b</h2>
	<section class="brk boxed"><h2>Nested5</h2><p>x</p></section>
	<p class=brk>Text</p><div class=r>r6</div><h2>Mid6</h2>`
	for _, tc := range []struct {
		content string
		want    [6]string
	}{
		{`string(h, first)`, [6]string{"Mid1", "Mid1", "Top3", "Top4a", "Nested5", "Mid6"}},
		{`string(h, start)`, [6]string{"", "Mid1", "Top3", "Top4a", "Nested5", "Nested5"}},
		{`string(h, last)`, [6]string{"Mid1", "Mid1", "Top3", "Twice4b", "Nested5", "Mid6"}},
		{`string(h, first-except)`, [6]string{"", "Mid1", "", "", "", ""}},
		{`element(r, first)`, [6]string{"rMid1", "rMid1", "rTop3", "r4b", "r4b", "r6"}},
		{`element(r, start)`, [6]string{"", "rMid1", "rTop3", "rTop3", "r4b", "r4b"}},
		{`element(r, last)`, [6]string{"rMid1", "rMid1", "rTop3", "r4b", "r4b", "r6"}},
		{`element(r, first-except)`, [6]string{"", "rMid1", "", "", "r4b", ""}},
	} {
		t.Run(tc.content, func(t *testing.T) {
			src := `<style>
				@page { size: 400pt 300pt; margin: 60pt; font: 10pt "Test Serif"; @top-center { content: ` + tc.content + ` } }
				body { font: 10pt "Test Serif" }
				h2 { string-set: h content(); font-size: 12pt; margin: 10pt 0 }
				.r { position: running(r) }
				.brk { break-before: page }
				.boxed { padding-top: 10pt; border-top: 1pt solid; margin-top: 15pt }
			</style>` + body
			doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
			if len(doc.Pages) != 6 {
				t.Fatalf("%d pages, want 6", len(doc.Pages))
			}
			for i, want := range tc.want {
				var got strings.Builder
				for _, r := range pageTexts(doc.Pages[i]) {
					if r.y < 60 {
						got.WriteString(r.text)
					}
				}
				if got.String() != want {
					t.Errorf("page %d: top margin shows %q, want %q", i+1, got.String(), want)
				}
			}
		})
	}
}

func TestFootnotes_CallsAndBodies(t *testing.T) {
	src := `<style>
		@page { size: 300pt 200pt; margin: 20pt; @footnote { border-top: 1pt solid black; padding-top: 2pt } }
		body { margin: 0; font: 10pt "Test Serif" }
		p { margin: 0 }
		.fn { float: footnote }
	</style>
	<p>One<span class=fn>First note</span> two.</p>
	<p>Three<span class=fn>Second note</span>.</p>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	if len(doc.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(doc.Pages))
	}
	pg := doc.Pages[0]
	one, two := find(t, pg, "1. First note"), find(t, pg, "2. Second note")
	if math.IsNaN(one.y) || math.IsNaN(two.y) || one.y <= 150 || two.y <= one.y {
		t.Errorf("bodies at %v and %v, want at the bottom of the page area in order", one.y, two.y)
	}
	call := find(t, pg, "One")
	runs := pageTexts(pg)
	found := false
	for _, r := range runs {
		if r.text == "1" && r.size < 10 && r.y < call.y {
			found = true
		}
	}
	if !found {
		t.Errorf("no superscript call 1 after One in %+v", runs)
	}
	if hasText(pg, "One First") {
		t.Error("the footnote body stayed in the flow")
	}
}

func TestMarginBoxes_MinMaxWidth(t *testing.T) {
	src := `<style>
		@page { size: 400pt 300pt; margin: 40pt 50pt; font: 10pt "Test Serif";
			@top-left { content: "A"; text-align: right; max-width: 40pt }
			@top-right { content: "B"; text-align: left }
			@bottom-left { content: "C"; min-width: 250pt; text-align: right }
			@bottom-right { content: "D" }
		}
		body { margin: 0 }
	</style><p>x</p>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	pg := doc.Pages[0]
	if a := find(t, pg, "A"); !approx(a.x, 85, 0.01) {
		t.Errorf("top-left text at %v, want 85: the box is 40pt wide", a.x)
	}
	if b := find(t, pg, "B"); !approx(b.x, 90, 0.01) {
		t.Errorf("top-right text at %v, want 90: the box takes the rest", b.x)
	}
	if c := find(t, pg, "C"); !approx(c.x, 295, 0.01) {
		t.Errorf("bottom-left text at %v, want 295: the box is 250pt wide", c.x)
	}
}

func TestNotes_CallsAndBodies(t *testing.T) {
	src := `<style>
		@page { size: 300pt 200pt; margin: 20pt; @note-area { content: element(note, all-once) } }
		body { margin: 0; font: 10pt "Test Serif" }
		p { margin: 0 }
		.n { position: note(note) }
		.n::note-callback { content: " back" }
	</style>
	<p>One<span class=n>First note</span> two.</p>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	if len(doc.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(doc.Pages))
	}
	pg := doc.Pages[0]
	body, flow := find(t, pg, "1. First note back"), find(t, pg, "One")
	if math.IsNaN(body.y) || math.IsNaN(flow.y) || body.y >= flow.y {
		t.Errorf("body at %v, flow at %v: want the note area above the flow", body.y, flow.y)
	}
	if hasText(pg, "One First") {
		t.Error("the note body stayed in the flow")
	}
}

func TestRunning_ShrinksToFitItsBox(t *testing.T) {
	src := `<style>
		@page { size: 400pt 300pt; margin: 40pt 50pt; font: 10pt "Test Serif";
			@top-center { content: element(head) }
			@bottom-right { content: element(foot); text-align: right }
		}
		body { margin: 0; font: 10pt "Test Serif" }
		header { position: running(head); background: green }
		footer { position: running(foot) }
	</style>
	<header>Head</header><footer>Foot</footer><p>x</p>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	pg := doc.Pages[0]
	head, foot := find(t, pg, "Head"), find(t, pg, "Foot")
	if math.IsNaN(head.x) || head.x <= 150 || head.x >= 200 {
		t.Errorf("running header at %v, want centred on the page", head.x)
	}
	if math.IsNaN(foot.x) || foot.x <= 300 {
		t.Errorf("running footer at %v, want at the right edge", foot.x)
	}
}

// TestRunning_PageCounters locks the page and pages counters of a running
// element: shown in a page-margin box, it reads the values of the page it
// prints on (CSS GCPM 3, 1.2.1, the example of p.rh::before), and the
// document's counters keep the values of its place in the document.
func TestRunning_PageCounters(t *testing.T) {
	src := `<style>
		@page { size: 400pt 300pt; margin: 40pt; @top-center { content: element(head) } }
		@page :first { counter-reset: page 3 }
		body { margin: 0; font: 10pt "Test Serif"; counter-reset: ch }
		h1 { counter-increment: ch; break-before: page; font-size: 10pt; margin: 0 }
		header { position: running(head) }
		header .pn::after { content: "p" counter(page) "/" counter(pages, upper-roman) "c" counter(ch) "." counters(page, "-") }
	</style>
	<header>H<span class=pn></span></header><p>one</p><h1>A</h1><h1>B</h1>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	if len(doc.Pages) != 3 {
		t.Fatalf("%d pages, want 3", len(doc.Pages))
	}
	for i, want := range []string{"p3/IIIc0.3", "p4/IIIc0.4", "p5/IIIc0.5"} {
		if !hasText(doc.Pages[i], want) {
			t.Errorf("page %d: no %q in %+v", i+1, want, pageTexts(doc.Pages[i]))
		}
	}
}

// TestRunning_LinksOnEveryPage locks the links of a running element: each
// page that shows it carries its link annotations, an internal one going
// to the document's element, each tagged in a Link element that holds the
// annotation alone, its text staying an artifact (ISO 14289-1, 7.18.5).
func TestRunning_LinksOnEveryPage(t *testing.T) {
	src := `<style>
		@page { size: 400pt 300pt; margin: 40pt; @top-center { content: element(head) } }
		body { margin: 0; font: 10pt "Test Serif" }
		header { position: running(head) }
		p { margin: 0 } .b { break-before: page }
	</style>
	<header><a href="https://example.com/">Home</a> <a href="#end">End</a></header>
	<p>one</p><p class=b>two</p><p id=end class=b>end</p>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	if len(doc.Pages) != 3 {
		t.Fatalf("%d pages, want 3", len(doc.Pages))
	}
	parents := map[*typeset.StructElem]bool{}
	for i, pg := range doc.Pages {
		if len(pg.Links) != 2 {
			t.Fatalf("page %d: %d links, want 2", i+1, len(pg.Links))
		}
		home, end := pg.Links[0], pg.Links[1]
		if home.URI != "https://example.com/" || home.Rect.Y > 40 || home.Contents != "Home" {
			t.Errorf("page %d: first link %+v, want Home to example.com in the top margin", i+1, home)
		}
		if end.Dest == nil || end.Dest.Page != 3 || end.Contents != "End" {
			t.Errorf("page %d: second link %+v, want End to page 3", i+1, end)
		}
		for _, l := range pg.Links {
			if l.Parent == nil || l.Parent.Role != "Link" || len(l.Parent.Kids) != 0 {
				t.Errorf("page %d: link parent %+v, want an empty Link element", i+1, l.Parent)
				continue
			}
			parents[l.Parent] = true
		}
	}
	found := 0
	for _, k := range doc.Structure.Kids {
		if parents[k] {
			found++
		}
	}
	if found != 6 {
		t.Errorf("%d of the 6 Link elements hang from the root", found)
	}
}

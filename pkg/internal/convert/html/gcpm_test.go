package html

import (
	"testing"
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
	if !(one.y > 150 && two.y > one.y) {
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

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

package html

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// tocDoc is a table of contents whose entries read the page and the
// chapter number of the headings they link to, and chapters that start
// on pages of their own.
const tocDoc = `<style>
	@page { size: 300pt 200pt; margin: 20pt }
	@page :first { counter-reset: page 4 }
	body { margin: 0; font: 10pt "Test Serif"; counter-reset: ch }
	h1 { counter-increment: ch; break-before: page; font-size: 10pt; margin: 0 }
	h1::before { content: counter(ch, upper-roman) ". " }
	a::after { content: " " target-counter(attr(href), ch, upper-roman) " p" target-counter(attr(href url), page) "/" target-counter(attr(href), pages) }
	a.t::after { content: " [" target-text(attr(href)) "|" target-text(attr(href), before) "|" target-text(attr(href), first-letter) "]" }
	a.s::after { content: " " target-counters(attr(href), ch, ".", lower-roman) }
</style>
<p><a href="#b">To B</a></p><p><a class=t href="#a">To A</a></p><p><a class=s href="#c">To C</a></p>
<h1 id=a>Alpha</h1><p>one</p><h1 id=b>Beta</h1><p>two</p><h1 id=c>Gamma</h1>`

// TestTargets_CountersTextAndPages locks the cross-references of CSS
// Generated Content 3, 6: a counter at the target, its text and its
// ::before text, and its page, as the page counter numbers it under an
// @page counter-reset, the pages counter the page count; a forward
// reference resolves as a backward one does.
func TestTargets_CountersTextAndPages(t *testing.T) {
	t.Parallel()
	doc := convertDoc(t, tocDoc, Options{PreferCSSPageSize: true})
	if len(doc.Pages) != 4 {
		t.Fatalf("%d pages, want 4", len(doc.Pages))
	}
	for _, want := range []string{"To B II p6/4", "To A [Alpha|I.|A]", "To C iii"} {
		if !hasLine(doc.Pages[0], want) {
			t.Errorf("no %q on page 1: %+v", want, pageLines(doc.Pages[0]))
		}
	}
}

// TestTargets_LayoutsOnlyWhenAPageIsRead locks the cost of the
// cross-references: a document that reads no page through them is laid out
// once, one that does twice when the second layout keeps its pages.
func TestTargets_LayoutsOnlyWhenAPageIsRead(t *testing.T) {
	t.Parallel()
	count := func(src string) int {
		n := 0
		lay := func(ctx context.Context, root *layout.Node, fonts *text.Fonts, opts layout.Options) (*layout.Result, error) {
			n++
			return layout.Layout(ctx, root, fonts, opts)
		}
		if _, err := convert(context.Background(), strings.NewReader(src), testFonts(t), Options{PreferCSSPageSize: true}, lay); err != nil {
			t.Fatalf("convert: %v", err)
		}
		return n
	}
	if n := count(`<p id=x>x</p><a href="#x">a</a><style>a::after { content: target-counter(attr(href), list-item) target-text(attr(href)) }</style>`); n != 1 {
		t.Errorf("%d layouts without a page read, want 1", n)
	}
	if n := count(tocDoc); n != 2 {
		t.Errorf("%d layouts of the table of contents, want 2", n)
	}
	if n := count(`<p>x</p>`); n != 1 {
		t.Errorf("%d layouts of a plain document, want 1", n)
	}
}

// TestTargets_Unresolved locks the error handling the draft leaves open: a
// URL that names no element, or another document, generates nothing and
// warns; the rest of the content value draws.
func TestTargets_Unresolved(t *testing.T) {
	t.Parallel()
	src := `<style>a::after { content: "[" target-counter(attr(href), page) target-text(attr(href)) "]" }</style>
		<p><a href="#nowhere">A</a> <a href="https://example.com/x#y">B</a> <a href="#h">C</a></p><p id=h style="display: none">hidden</p>`
	doc := convertDoc(t, src, Options{})
	for _, want := range []string{"A[]", "B[]", "C[]"} {
		if !hasLine(doc.Pages[0], want) {
			t.Errorf("no %q: %+v", want, pageLines(doc.Pages[0]))
		}
	}
	found := false
	for _, w := range doc.Warnings {
		if w.Code == warn.LinkUnresolved && strings.Contains(w.Message, "#nowhere") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning for #nowhere: %+v", doc.Warnings)
	}
}

// TestTargets_MarginBoxAndRunningElement locks a cross-reference in a
// page-margin box, which a URL string names, and one in a running element,
// which shows the value on every page.
func TestTargets_MarginBoxAndRunningElement(t *testing.T) {
	t.Parallel()
	src := `<style>
		@page { size: 300pt 200pt; margin: 30pt; @bottom-center { content: "end p" target-counter("#end", page) } @top-center { content: element(h) } }
		body { margin: 0; font: 10pt "Test Serif" }
		header { position: running(h) }
		header a::after { content: " p" target-counter(attr(href), page) }
		p { break-before: page; margin: 0 }
	</style>
	<header><a href="#end">End</a></header><p>one</p><p>two</p><p id=end>three</p>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	if len(doc.Pages) != 3 {
		t.Fatalf("%d pages, want 3", len(doc.Pages))
	}
	for i, pg := range doc.Pages {
		if !hasLine(pg, "end p3") || !hasLine(pg, "End p3") {
			t.Errorf("page %d: %+v", i+1, pageLines(pg))
		}
	}
}

// pageLines returns the text of pg's lines: the runs on one baseline
// joined, in paint order.
func pageLines(pg *typeset.Page) []string {
	var out []string
	var ys []float64
	for _, r := range pageTexts(pg) {
		i := slices.Index(ys, r.y)
		if i < 0 {
			ys, out = append(ys, r.y), append(out, "")
			i = len(out) - 1
		}
		out[i] += r.text
	}
	return out
}

// hasLine reports whether a line of pg contains s.
func hasLine(pg *typeset.Page, s string) bool {
	return slices.ContainsFunc(pageLines(pg), func(l string) bool { return strings.Contains(l, s) })
}

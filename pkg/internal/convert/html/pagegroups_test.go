package html

import (
	"testing"
)

// TestPageGroups_NthOfName locks the page groups of CSS GCPM 3, 3.2: each
// section whose page property names a page and whose break-before forces
// a break makes a group of its pages, which :nth(An+B of name) and :nth()
// of a named page count in; a page outside every group matches neither.
func TestPageGroups_NthOfName(t *testing.T) {
	t.Parallel()
	src := `<style>
		@page { size: 200pt 100pt; margin: 20pt }
		@page :nth(1 of chapter) { @top-center { content: "first" } }
		@page chapter:nth(2) { @top-center { content: "second" } }
		@page :nth(2) { @bottom-center { content: "doc2" } }
		body { margin: 0; font: 10pt "Test Serif" }
		section { page: chapter; break-before: page }
		div { height: 150pt }
	</style>
	<p>intro</p><section><div></div></section><section><div style="height: 100pt"></div></section>`
	doc := convertDoc(t, src, Options{PreferCSSPageSize: true})
	want := []string{"", "first", "second", "", "first", "second"}
	if len(doc.Pages) != len(want) {
		t.Fatalf("%d pages, want %d", len(doc.Pages), len(want))
	}
	for i, w := range want {
		for _, s := range []string{"first", "second"} {
			if got := hasLine(doc.Pages[i], s); got != (s == w) {
				t.Errorf("page %d: %q shown %v, want the header %q: %v", i+1, s, got, w, pageLines(doc.Pages[i]))
			}
		}
		if got := hasLine(doc.Pages[i], "doc2"); got != (i == 1) {
			t.Errorf("page %d: :nth(2) of the document shown %v", i+1, got)
		}
	}
}

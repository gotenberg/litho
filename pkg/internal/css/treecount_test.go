package css

import "testing"

// TestTreeCounting_SiblingIndexAndCount resolves sibling-index() and
// sibling-count() on the element styled (CSS Values 5, 9), as Chromium
// 156 printed the same rules: in a length, an integer and a number; an
// unregistered custom property keeps them for the element that uses it,
// a registered one computes them on its own element; a pseudo-element
// takes its originating element's; one with an argument is invalid.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints three divs of width
//     calc(sibling-index() * 20px) 20, 40 and 60px wide; children of a
//     parent of --i: sibling-index() 10 and 20px wide for calc(var(--i) *
//     10px), at opacity 0.5 for calc(1 / sibling-count()); and, the
//     parent fifth among its siblings declaring a registered <integer>
//     --r: sibling-index(), its two children 50px wide.
func TestTreeCounting_SiblingIndexAndCount(t *testing.T) {
	t.Parallel()
	c := compute(t, `<!DOCTYPE html><div id=l><div id=a></div><div id=b></div><div id=c></div></div><p></p>`+
		`<div id=p><div id=pa></div><div id=pb></div></div><p></p><div id=reg><div id=ra></div><div id=rb></div></div>`+
		`<div id=x></div>`, `
@property --r { syntax: '<integer>'; inherits: true; initial-value: 0 }
#l > div { width: calc(sibling-index() * 20px); z-index: sibling-count() }
#l > div::before { content: ""; width: calc(sibling-index() * 1px) }
#p { --i: sibling-index() } #p > div { width: calc(var(--i) * 10px); opacity: calc(1 / sibling-count()) }
#reg { --r: sibling-index() } #reg > div { width: calc(var(--r) * 10px) }
#x { width: calc(sibling-index(1) * 10px) }`)
	for id, want := range map[string]float64{"a": 20, "b": 40, "c": 60, "pa": 10, "pb": 20, "ra": 50, "rb": 50} {
		if got := c.of(id).Width; got != Pts(want*Pt) {
			t.Errorf("%s: width %v, want %vpx", id, got, want)
		}
	}
	if z := c.of("b").ZIndex; z.Auto || z.Level != 3 {
		t.Errorf("b: z-index %+v, want 3", z)
	}
	if o := c.of("pb").Opacity; o != 0.5 {
		t.Errorf("pb: opacity %v, want 0.5", o)
	}
	if ps := c.c.Pseudo(byID(c.doc, "c"), PseudoBefore); ps == nil || ps.Width != Pts(3*Pt) {
		t.Errorf("c::before: %v, want its originating element's index", ps)
	}
	if w := c.of("x").Width; w.Kind != Auto {
		t.Errorf("x: width %v, want auto: an argument makes it invalid", w)
	}
}

package css

import "testing"

// TestQuirksMode_MatchesClassesAndIDsCaseInsensitively matches class and ID
// selectors ASCII case-insensitively in quirks mode alone (Selectors 4,
// 6.6 and 6.7), through the rule index, the ancestor filter and a compound
// selector alike; an attribute selector on class stays case-sensitive.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 matches .Foo on
//     class=foo and #BAR on id=bar in quirks mode, not with <!DOCTYPE
//     html>.
func TestQuirksMode_MatchesClassesAndIDsCaseInsensitively(t *testing.T) {
	const author = `.Foo { color: rgb(1, 2, 3) } #BAR { background-color: rgb(4, 5, 6) } .OUTER p.In { margin-top: 3px } [class~=FOO] { width: 9px }`
	const doc = `<div id=bar class=foo></div><div class=outer><p id=p class=in></p></div>`
	for _, mode := range []DocumentMode{Quirks, LimitedQuirks, NoQuirks} {
		c := computeQuirks(t, doc, author, mode)
		quirks := mode == Quirks
		d, p := c.of("bar"), c.of("p")
		if got := colorEq(d.Color, rgb(1, 2, 3)); got != quirks {
			t.Errorf("mode %d: class colour %v", mode, d.Color)
		}
		if got := colorEq(d.BackgroundColor, rgb(4, 5, 6)); got != quirks {
			t.Errorf("mode %d: ID background %v", mode, d.BackgroundColor)
		}
		if got := dimEq(p.Margin[Top], pts(2.25)); got != quirks {
			t.Errorf("mode %d: descendant margin %v", mode, p.Margin[Top])
		}
		if d.Width.Kind != Auto {
			t.Errorf("mode %d: attribute selector matched: width %v", mode, d.Width)
		}
	}
}

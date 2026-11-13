package css

import "testing"

// TestAttr_TypedSubstitution locks attr() with a type or a unit and
// env() (CSS Values 5, 7.7; CSS Environment Variables 1, 3), as Chromium
// 156 prints them: the attribute parsed as the type, the fallback when it
// is missing or does not parse, the declaration invalid without one.
//
// # Corpus evidence
//
//   - probe, html: in a 400px parent, Chromium prints width:
//     attr(data-w px) with data-w="42" at 42px, attr(data-w
//     type(<length>)) with data-w="42" at 400px (invalid, auto), and
//     env(safe-area-inset-left, 77px) at 0px.
func TestAttr_TypedSubstitution(t *testing.T) {
	t.Parallel()
	doc := `<div id=a data-w="42" data-l="200px" data-n="3" data-bad="qq" data-z="0" data-c="rgb(0 128 0)"></div>`
	tests := []struct {
		decl string
		want Dimension
	}{
		{"width: 5px; width: attr(data-w px)", pts(42 * Pt)},
		{"width: 5px; width: attr(data-w type(<length>))", Dimension{Kind: Auto}},
		{"width: 5px; width: attr(data-l type(<length>))", pts(150)},
		{"width: 5px; width: max(attr(data-l type(<length>)))", pts(150)},
		{"width: 5px; width: attr(data-bad type(<length>), 200px)", pts(150)},
		{"width: 5px; width: attr(data-missing type(<length>), 10px)", pts(7.5)},
		{"width: 5px; width: attr(data-z type(<length>), 0)", pts(0)},
		{"width: 5px; width: attr(data-l type(<number> | <length>))", pts(150)},
		{"width: 5px; width: calc(attr(data-n type(<number>)) * 10px)", pts(22.5)},
		{"width: 5px; width: attr(data-n %)", pct(0.03)},
		{"width: 5px; width: env(safe-area-inset-left, 77px)", pts(0)},
		{"width: 5px; width: env(nonexistent, 66px)", pts(49.5)},
		{"width: 5px; width: calc(env(safe-area-inset-top) + 55px)", pts(41.25)},
		{"width: 5px; width: env(nonexistent)", Dimension{Kind: Auto}},
	}
	for _, tt := range tests {
		c := compute(t, doc, "#a { "+tt.decl+" }")
		if got := c.of("a").Width; !dimEq(got, tt.want) {
			t.Errorf("%s = %+v, want %+v", tt.decl, got, tt.want)
		}
	}
	c := compute(t, doc, "#a { color: attr(data-c type(<color>)); background-image: url(attr(data-c)) }")
	if got := c.of("a").Color; !colorEq(got, rgb(0, 128, 0)) {
		t.Errorf("color from attr() = %+v", got)
	}
	if got := c.of("a").BackgroundImage; got != "" {
		t.Errorf("url(attr()) = %q, want no URL from an attribute", got)
	}
}

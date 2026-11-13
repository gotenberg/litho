package css

import "testing"

// TestNesting_Relaxed locks CSS Nesting 1 beyond the nesting selector: a
// nested rule that starts with a type selector, a combinator or a
// pseudo-class (2.1), declarations after a nested rule coming after it
// (3.3), nested conditional rules (3.2), the specificity of & (6), and
// the recovery of a nested rule cut by a semicolon (CSS Syntax 3, 5.4.4).
func TestNesting_Relaxed(t *testing.T) {
	t.Parallel()
	lime, blue, red := Color{G: 1, A: 1}, Color{B: 1, A: 1}, Color{R: 1, A: 1}
	for _, tt := range []struct {
		sheet string
		want  Color
	}{
		{`#c { color: blue; span { color: lime } }`, lime},
		{`#c { color: blue; > span { color: lime } }`, lime},
		{`#c { color: blue; + span { color: lime } }`, blue},
		{`div { color: blue; :is(span) { color: lime } }`, lime},
		{`#c { span { color: lime } color: red }`, lime},
		{`#c span { color: blue } #c { & span { color: lime } }`, lime},
		{`span { color: red; @media print { color: lime } }`, lime},
		{`span { color: red; @media screen { color: lime } }`, red},
		{`#c { @media print { span { color: lime } } }`, lime},
		{`#c { @supports (display: block) { span { color: lime } } }`, lime},
		{`#z, div { & span { color: red } } div span { color: blue }`, red},
		{`#c { color: blue; span:hover; span { color: lime } }`, lime},
		{`#c { .a & { color: red } }`, Color{A: 1}},
	} {
		c := compute(t, `<div id=c><span id=t>x</span></div>`, tt.sheet)
		if got := c.of("t").Color; got != tt.want {
			t.Errorf("%s: color %+v, want %+v", tt.sheet, got, tt.want)
		}
	}
}

// TestNesting_StyleAttributeKeepsDeclarations locks the declaration
// lists that are no style rule's block: a style attribute reads no
// nested rule.
func TestNesting_StyleAttributeKeepsDeclarations(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=t style="color: blue; span { color: red } color: lime">x</div>`, ``)
	if got := c.of("t").Color; got == (Color{R: 1, A: 1}) {
		t.Errorf("style attribute color %+v: a nested rule applied", got)
	}
}

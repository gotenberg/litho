package css

import "testing"

// TestEmphasis_Properties locks the values of the emphasis properties as
// Chromium 156 computes them (CSS Text Decoration 3, 3).
func TestEmphasis_Properties(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		decls, mark string
		pos         EmphasisPosition
	}{
		{"", "", EmphasisOver},
		{"text-emphasis-style: filled", "•", EmphasisOver},
		{"text-emphasis-style: open", "◦", EmphasisOver},
		{"text-emphasis-style: circle", "●", EmphasisOver},
		{"text-emphasis-style: circle open", "○", EmphasisOver},
		{"text-emphasis-style: filled double-circle", "◉", EmphasisOver},
		{"text-emphasis-style: open triangle", "△", EmphasisOver},
		{"text-emphasis-style: sesame", "﹅", EmphasisOver},
		{"text-emphasis-style: 'xp'", "x", EmphasisOver},
		{"text-emphasis-style: 'éz'", "é", EmphasisOver},
		{"text-emphasis-style: dot; text-emphasis-style: filled filled", "•", EmphasisOver},
		{"text-emphasis: green circle", "●", EmphasisOver},
		{"text-emphasis: circle green", "●", EmphasisOver},
		{"text-emphasis: dot; text-emphasis: red", "", EmphasisOver},
		{"text-emphasis-position: under right", "", EmphasisUnder},
		{"text-emphasis-position: left under", "", EmphasisUnder},
		{"text-emphasis-position: under; text-emphasis-position: left left", "", EmphasisUnder},
	} {
		if s := styleOf(t, c.decls); s.TextEmphasisMark != c.mark || s.TextEmphasisPosition != c.pos {
			t.Errorf("%q: mark %q position %v, want %q %v", c.decls, s.TextEmphasisMark, s.TextEmphasisPosition, c.mark, c.pos)
		}
	}
}

// TestEmphasis_ColorIsCurrentColor locks text-emphasis-color: inherited,
// currentColor by default, resolved against each element's own color.
func TestEmphasis_ColorIsCurrentColor(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><p id=p style="text-emphasis: dot"><span id=s style="color: red">x</span></p><p id=q style="text-emphasis-color: blue"><span id=r style="color: red">x</span></p></body>`, ``)
	if got := c.of("p").TextEmphasisColor; !colorEq(got, rgb(0, 0, 0)) {
		t.Errorf("p: %+v, want black", got)
	}
	if got := c.of("s").TextEmphasisColor; !colorEq(got, rgb(255, 0, 0)) {
		t.Errorf("span: %+v, want its own red", got)
	}
	if got := c.of("r").TextEmphasisColor; !colorEq(got, rgb(0, 0, 255)) {
		t.Errorf("span under a blue emphasis: %+v, want blue", got)
	}
	if got := c.of("s").TextEmphasisMark; got != "•" {
		t.Errorf("span mark %q, want the inherited dot", got)
	}
}

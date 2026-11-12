package css

import "testing"

// TestUserAgent_RenderingRules checks rules of the user-agent stylesheet
// against the HTML Standard's Rendering section, where Chromium computes
// the same: a summary is a block unless it is the first summary of a
// details (15.5.5), a fieldset's border is groove ThreeDFace and its
// minimum inline size min-content (15.3.12), and a marquee clips its
// content and aligns it by text-align's initial value (15.5.13).
//
// # Corpus evidence
//
//   - probe, html: Chromium in gotenberg/gotenberg:9.4.1 computes a
//     summary outside details as block with no marker, the first summary
//     of a details as list-item, disclosure-closed, inside, a fieldset's
//     border colour as rgb(239, 239, 239) and its min-inline-size as
//     min-content, and a marquee's overflow as hidden and its text-align
//     as start.
func TestUserAgent_RenderingRules(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><summary id=lone>a</summary>
<details><summary id=first>b</summary><summary id=second>c</summary>d</details>
<fieldset id=f>e</fieldset><marquee id=m>f</marquee></body>`, ``)
	if s := c.of("lone"); s.Display != Block {
		t.Errorf("summary outside details: display %v", s.Display)
	}
	if s := c.of("first"); s.Display != ListItem || s.ListStylePosition != Inside {
		t.Errorf("first summary of details: display %v, list-style-position %v", s.Display, s.ListStylePosition)
	}
	if s := c.of("second"); s.Display != Block {
		t.Errorf("second summary of details: display %v", s.Display)
	}
	f := c.of("f")
	if !colorEq(f.Border[Top].Color, rgb(239, 239, 239)) || f.Border[Top].Style != Groove || f.MinWidth.Kind != MinContent {
		t.Errorf("fieldset: border %+v, min-width %v", f.Border[Top], f.MinWidth)
	}
	if m := c.of("m"); m.Overflow != OverflowHidden || m.TextAlign != AlignStart {
		t.Errorf("marquee: overflow %v, text-align %v", m.Overflow, m.TextAlign)
	}
}

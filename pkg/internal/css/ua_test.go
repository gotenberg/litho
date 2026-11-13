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

// TestUserAgent_TableBorderColors locks the border colours of tables and
// cells: currentColor, as the HTML Standard's Rendering section leaves
// them (15.3.8), black for the cells of a table with a rules attribute,
// and a form a table holds hidden.
//
// # Corpus evidence
//
//   - probe, html: Chromium in gotenberg/gotenberg:9.4.1 computes the
//     top border colour of a table with border=1 and of its cell, of a
//     cell of a table with rules=all and of a plain table's cell as
//     rgb(0, 0, 0), and a form between tr and td as display none.
func TestUserAgent_TableBorderColors(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><table id=t border=1><tr><td id=c>a</td></tr></table>
<table rules=all><tr><td id=r>b</td></tr></table><table><tr><form id=f><td>c</td></form></tr></table></body>`, `table { color: red } #r { color: blue }`)
	if s := c.of("t"); !colorEq(s.Border[Top].Color, rgb(255, 0, 0)) {
		t.Errorf("table border colour %+v, want currentColor", s.Border[Top].Color)
	}
	if s := c.of("c"); !colorEq(s.Border[Top].Color, rgb(255, 0, 0)) {
		t.Errorf("cell border colour %+v, want currentColor", s.Border[Top].Color)
	}
	if s := c.of("r"); !colorEq(s.Border[Top].Color, rgb(0, 0, 0)) {
		t.Errorf("cell of rules=all: border colour %+v, want black", s.Border[Top].Color)
	}
	if s := c.of("f"); s != nil && s.Display != DisplayNone {
		t.Errorf("form in a table: display %v", s.Display)
	}
}

// TestUserAgent_FormControlText locks the text properties a form control
// sets apart from its ancestors (HTML Standard, 15.3.10): spacing, line
// height, transform, indent, shadow and alignment back to their initial
// values, a button's text centred, a button sized by its border box, a
// textarea's white space kept.
//
// # Corpus evidence
//
//   - probe, html: Chromium in gotenberg/gotenberg:9.4.1, under a body
//     with line-height 3, letter-spacing 2px, text-transform uppercase,
//     text-indent 7px and text-align right, computes an input's as
//     normal, normal, none, 0px and start, a button's text-align as center
//     and box-sizing as border-box, a textarea's white-space as pre-wrap.
func TestUserAgent_FormControlText(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><input id=i value=x><button id=b>b</button><textarea id=t>t</textarea></body>`,
		`body { line-height: 3; letter-spacing: 2px; text-transform: uppercase; text-indent: 7px; text-align: right }`)
	i := c.of("i")
	if i.LineHeight.Kind != Normal || i.LetterSpacing.Kind != Normal || i.TextTransform != TransformNone || i.TextAlign != AlignStart {
		t.Errorf("input: line-height %v, letter-spacing %v, text-align %v", i.LineHeight, i.LetterSpacing, i.TextAlign)
	}
	if b := c.of("b"); b.TextAlign != AlignCenter || b.BoxSizing != BorderBox {
		t.Errorf("button: text-align %v, box-sizing %v", b.TextAlign, b.BoxSizing)
	}
	if ta := c.of("t"); ta.WhiteSpace != PreWrap || ta.TextIndent != Pts(0) {
		t.Errorf("textarea: white-space %v, text-indent %v", ta.WhiteSpace, ta.TextIndent)
	}
}

// TestUserAgent_DialogAndPopover locks an open dialog's box (HTML
// Standard, 15.3.3): absolutely positioned, centred by its auto margins,
// with a solid border, a 1em padding and the Canvas colours; and a
// popover, never open on paper, hidden.
//
// # Corpus evidence
//
//   - probe, html: Chromium in gotenberg/gotenberg:9.4.1 computes an open
//     dialog's position as absolute, its border style as solid, its
//     padding as 16px and its colours as white and black, and a popover's
//     display as none.
func TestUserAgent_DialogAndPopover(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><dialog id=d open>d</dialog><div id=p popover>p</div><dialog id=q popover open>q</dialog></body>`, ``)
	d := c.of("d")
	if d.Position != Absolute || d.Border[Top].Style != Solid || !colorEq(d.BackgroundColor, rgb(255, 255, 255)) {
		t.Errorf("open dialog: position %v, border %v, background %v", d.Position, d.Border[Top].Style, d.BackgroundColor)
	}
	if p := c.of("p"); p.Display != DisplayNone {
		t.Errorf("popover: display %v", p.Display)
	}
	if q := c.of("q"); q.Display == DisplayNone {
		t.Errorf("open dialog with popover: hidden")
	}
}

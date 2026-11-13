package css

import "testing"

// TestFirstLine_AppliesItsProperties checks that ::first-line sets the
// properties that apply to it and that the others inherit or take their
// initial values (CSS Pseudo 4, 2.1.2, as Chromium filters them).
func TestFirstLine_AppliesItsProperties(t *testing.T) {
	c := compute(t, `<p id=p>text</p>`, `
		p { text-align: center; letter-spacing: 2px }
		p::first-line { color: red; font-size: 30px; margin-left: 10px; padding-left: 5px;
			text-align: right; text-indent: 9px; white-space: pre; text-transform: uppercase;
			background-color: blue; vertical-align: 20px }`)
	p := byID(c.doc, "p")
	fl := c.c.FirstLine(p, c.of("p"))
	if fl == nil {
		t.Fatal("no ::first-line style")
	}
	if !colorEq(fl.Color, rgb(255, 0, 0)) || !near(fl.FontSize, 30*Pt) || fl.TextTransform != Uppercase || !colorEq(fl.BackgroundColor, rgb(0, 0, 255)) {
		t.Errorf("applied properties: color %v size %v transform %v background %v", fl.Color, fl.FontSize, fl.TextTransform, fl.BackgroundColor)
	}
	if fl.Margin[Left].Value != 0 || fl.Padding[Left].Value != 0 || fl.TextIndent.Value != 0 || fl.WhiteSpace != WhiteSpaceNormal || fl.VerticalAlign.Kind != Baseline {
		t.Errorf("dropped properties set: margin %v padding %v indent %v white-space %v vertical-align %v", fl.Margin[Left], fl.Padding[Left], fl.TextIndent, fl.WhiteSpace, fl.VerticalAlign)
	}
	if fl.TextAlign != AlignCenter || !dimEq(fl.LetterSpacing, pts(2*Pt)) || fl.Display != Inline {
		t.Errorf("inherited and initial values: text-align %v letter-spacing %v display %v", fl.TextAlign, fl.LetterSpacing, fl.Display)
	}
}

// TestFirstLine_InheritsFromTheLineBlock checks that the ::first-line of an
// ancestor inherits from the block whose line it styles, as the fictional
// tag sequence nests it there (CSS Pseudo 4, 2.1.1).
func TestFirstLine_InheritsFromTheLineBlock(t *testing.T) {
	c := compute(t, `<div id=d><p id=p>text</p></div>`, `
		div { font-size: 10px } div::first-line { color: red }
		p { font-size: 20px; color: blue }`)
	fl := c.c.FirstLine(byID(c.doc, "d"), c.of("p"))
	if fl == nil || !near(fl.FontSize, 20*Pt) || !colorEq(fl.Color, rgb(255, 0, 0)) {
		t.Fatalf("div::first-line on p's line = %+v", fl)
	}
	if c.c.FirstLine(byID(c.doc, "p"), c.of("p")) != nil {
		t.Error("p has no ::first-line of its own")
	}
}

// TestRestyle_InheritsFromTheFirstLine checks that an element on a first
// line inherits from the ::first-line, its own declarations winning, and
// that its selectors match again after the cascade (CSS Pseudo 4, 2.1.3).
func TestRestyle_InheritsFromTheFirstLine(t *testing.T) {
	c := compute(t, `<div><p id=p>a <span id=s>b</span> <em id=e>c</em></p></div>`, `
		p::first-line { color: red; font-size: 20px }
		div span { font-size: 1.5em }
		em { color: green }
		p::before { content: "x"; letter-spacing: 1em }`)
	p := byID(c.doc, "p")
	fl := c.c.FirstLine(p, c.of("p"))
	s := c.c.Restyle(byID(c.doc, "s"), fl)
	if s == nil || !colorEq(s.Color, rgb(255, 0, 0)) || !near(s.FontSize, 30*Pt) {
		t.Errorf("span on the first line: %+v", s)
	}
	if e := c.c.Restyle(byID(c.doc, "e"), fl); e == nil || !colorEq(e.Color, rgb(0, 128, 0)) || !near(e.FontSize, 20*Pt) {
		t.Errorf("em on the first line: %+v", e)
	}
	if got := c.c.Restyle(byID(c.doc, "s"), c.of("p")); got != c.of("s") {
		t.Error("restyled under its own parent's style, a span keeps its style")
	}
	b := c.c.RestylePseudo(p, PseudoBefore, fl)
	if b == nil || !colorEq(b.Color, rgb(255, 0, 0)) || !dimEq(b.LetterSpacing, pts(20*Pt)) {
		t.Errorf("::before on the first line: %+v", b)
	}
}

// TestRestyle_WithoutFirstLine checks that a document without a
// ::first-line restyles nothing.
func TestRestyle_WithoutFirstLine(t *testing.T) {
	c := compute(t, `<p id=p><span id=s>b</span></p>`, `span { color: red }`)
	if got := c.c.Restyle(byID(c.doc, "s"), Initial()); got != c.of("s") {
		t.Error("Restyle changed a style without a ::first-line")
	}
}

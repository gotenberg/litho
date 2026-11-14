package odt

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// formulaRuns returns the glyph runs of frags tagged Formula.
func formulaRuns(frags []typeset.Fragment, out *[]*typeset.Text) {
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Text:
			if v.Parent != nil && v.Parent.Role == "Formula" {
				*out = append(*out, v)
			}
		case *typeset.Group:
			formulaRuns(v.Content, out)
		}
	}
}

// TestFormula_DrawsAnAsCharacterFormula locks the drawing of a formula
// object anchored as a character: its glyphs on the page, under a Formula
// structure element whose alternate description is the frame's title.
func TestFormula_DrawsAnAsCharacterFormula(t *testing.T) {
	d := doc{body: `<text:p>Before <draw:frame text:anchor-type="as-char" svg:width="44.3pt" svg:height="15.05pt">` +
		`<svg:title>Euler</svg:title><draw:object><math xmlns="http://www.w3.org/1998/Math/MathML" display="block"><semantics><mrow>` +
		`<msup><mi mathvariant="normal">e</mi><mrow><mi>i</mi><mi>π</mi></mrow></msup><mo stretchy="false">+</mo><mn>1</mn>` +
		`<mo stretchy="false">=</mo><mn>0</mn></mrow><annotation encoding="StarMath 5.0">{func e}^{i %pi} + 1 = 0</annotation>` +
		`</semantics></math></draw:object></draw:frame> after</text:p>`}
	out := convertDoc(t, d.flat(), office.Options{})
	var fr []*typeset.Text
	formulaRuns(out.Pages[0].Content, &fr)
	if len(fr) == 0 {
		t.Fatal("no formula glyphs drawn")
	}
	var b strings.Builder
	for _, r := range fr {
		if r.Parent.Alt != "Euler" {
			t.Errorf("formula's alternate description %q, want the frame's title", r.Parent.Alt)
		}
		for _, g := range r.Glyphs {
			b.WriteString(g.Text)
		}
	}
	if got := b.String(); got != "eiπ+1=0" {
		t.Errorf("formula draws %q, want eiπ+1=0", got)
	}
	if got := pageText(out.Pages[0]); !strings.Contains(got, "Before") || !strings.Contains(got, "after") {
		t.Errorf("page text %q, want the paragraph's text around the formula", got)
	}
}

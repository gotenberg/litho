package html

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// fixtureGotenbergFonts holds the fonts of the reference image,
// provisioned with the corpus fonts.
const fixtureGotenbergFonts = "../../../../test/integration/testdata/fonts/gotenberg"

// gotenbergFonts indexes the fonts of the reference image, skipping the
// test when the fixture fonts are absent.
func gotenbergFonts(t *testing.T) *text.Fonts {
	t.Helper()
	if _, err := os.Stat(fixtureGotenbergFonts); err != nil {
		t.Skipf("fixture fonts absent: %v", err)
	}
	f, err := text.NewFonts(context.Background(), nil, []string{fixtureGotenbergFonts})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestMath_InlineAndBlock builds an inline formula as a replaced box
// with a baseline of its own, and a block formula as such a box centred
// in a block of the math element's style, each a Formula with its
// alternate description (MathML Core, 2.1.1).
func TestMath_InlineAndBlock(t *testing.T) {
	src := `<p>Let <math id=a><msup><mi>x</mi><mn>2</mn></msup></math> be.</p>
<math id=b display="block" alttext="sum of n" style="border:1px solid"><mo>∑</mo><mi>n</mi></math>`
	p, err := prepare(context.Background(), []byte(src), Options{}, testFonts(t))
	if err != nil {
		t.Fatal(err)
	}
	a := byID(t, p.root, "a")
	if a.Kind != layout.Replaced || a.Role != "Formula" || a.Alt != "x 2" || a.Picture == nil || !a.Picture.HasBaseline {
		t.Fatalf("inline formula: kind %d, role %q, alt %q, picture %+v", a.Kind, a.Role, a.Alt, a.Picture)
	}
	if a.Style.Display != css.Inline {
		t.Fatalf("inline formula displayed %d", a.Style.Display)
	}
	b := byID(t, p.root, "b")
	if b.Kind != layout.Element || len(b.Children) != 1 || b.Style.TextAlign != css.AlignCenter || b.Style.Border[css.Top].Width == 0 {
		t.Fatalf("block formula: kind %d, %d children, align %d", b.Kind, len(b.Children), b.Style.TextAlign)
	}
	if f := b.Children[0]; f.Kind != layout.Replaced || f.Role != "Formula" || f.Alt != "sum of n" || f.Style.Border[css.Top].Width != 0 {
		t.Fatalf("block formula's box: kind %d, role %q, alt %q", f.Kind, f.Role, f.Alt)
	}
}

// TestMath_DrawsAFormulaTagged lays a formula out with the fonts of the
// reference image: its text reads back in order, the operator once, and
// it is a Formula element of the structure tree with its description.
func TestMath_DrawsAFormulaTagged(t *testing.T) {
	fonts := gotenbergFonts(t)
	src := `<p>See <math><mrow><mo>(</mo><mfrac><mi>a</mi><mi>b</mi></mfrac><mo>)</mo></mrow><mo>=</mo><msqrt><mn>2</mn></msqrt></math>.</p>`
	doc, err := Convert(context.Background(), strings.NewReader(src), fonts, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var formula *typeset.StructElem
	var find func(e *typeset.StructElem)
	find = func(e *typeset.StructElem) {
		if e.Role == "Formula" && formula == nil {
			formula = e
		}
		for _, k := range e.Kids {
			find(k)
		}
	}
	find(doc.Structure)
	if formula == nil || formula.Alt != "( a b ) = 2" {
		t.Fatalf("formula element %+v", formula)
	}
	var b strings.Builder
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Text:
				if v.Parent == formula {
					for _, g := range v.Glyphs {
						b.WriteString(g.Text)
					}
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(doc.Pages[0].Content)
	if got := b.String(); got != "(\U0001D44E\U0001D44F)=√2" {
		t.Fatalf("formula text %q", got)
	}
	for _, w := range doc.Warnings {
		t.Errorf("warning: %s", w.Message)
	}
}

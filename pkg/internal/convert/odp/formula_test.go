package odp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// mathFrame is a 14 by 2 cm frame holding a² + b² = c² inline, as
// sd/Math.fodp of libreoffice/odp holds it.
const mathFrame = `<draw:frame draw:name="Formula" svg:width="14cm" svg:height="2cm" svg:x="8cm" svg:y="8cm"><draw:object>` +
	`<math xmlns="http://www.w3.org/1998/Math/MathML" display="block"><semantics><mrow><msup><mi>a</mi><mn>2</mn></msup>` +
	`<mo stretchy="false">+</mo><msup><mi>b</mi><mn>2</mn></msup><mo stretchy="false">=</mo><msup><mi>c</mi><mn>2</mn></msup></mrow>` +
	`</semantics></math></draw:object></draw:frame>`

// TestFrame_DrawsItsFormula locks the drawing of a formula object: a
// picture shape of the frame's box, drawing the formula's glyphs under a
// Formula structure element whose alternate description is its tokens.
func TestFrame_DrawsItsFormula(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "fonts", "gotenberg", "truetype")
	if _, err := os.Stat(filepath.Join(dir, "libreoffice")); err != nil {
		t.Skip("fixture fonts not fetched (make corpus-fetch CORPUS=fonts)")
	}
	fonts, err := text.NewFonts(context.Background(), nil, []string{filepath.Join(dir, "liberation"), filepath.Join(dir, "libreoffice")})
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := Read(context.Background(), flat{body: page(mathFrame)}.bytes(), fonts, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Slides) != 1 || len(doc.Slides[0].Shapes) != 1 {
		t.Fatalf("slides = %+v, want one shape", doc.Slides)
	}
	s := doc.Slides[0].Shapes[0]
	if s.Kind != slides.KindPicture || !s.Formula || !nearRect(s.Box, rect(8, 8, 14, 2)) {
		t.Fatalf("shape kind %v, formula %v, box %+v; want a formula picture of the frame's box", s.Kind, s.Formula, s.Box)
	}
	out, err := slides.Convert(context.Background(), doc, fonts, office.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var texts []*typeset.Text
	var walk func([]typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Text:
				texts = append(texts, v)
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(out.Pages[0].Content)
	if len(texts) != 8 {
		t.Fatalf("got %d glyph runs, want the formula's 8", len(texts))
	}
	for _, r := range texts {
		if r.Parent == nil || r.Parent.Role != "Formula" || r.Parent.Alt != "a 2 + b 2 = c 2" {
			t.Fatalf("run tagged %+v, want a Formula of the formula's tokens", r.Parent)
		}
	}
	if !strings.Contains(texts[0].Face.Key, "Italic") || !strings.Contains(texts[2].Face.Key, "opens") {
		t.Errorf("a drawn in %s and + in %s, want Liberation Serif Italic and OpenSymbol", texts[0].Face.Key, texts[2].Face.Key)
	}
}

// TestFrame_FormulaWithoutPresentationIsLeftOut locks the warning of a
// formula holding only its StarMath text.
func TestFrame_FormulaWithoutPresentationIsLeftOut(t *testing.T) {
	t.Parallel()
	doc, warnings := read(t, flat{body: page(`<draw:frame svg:width="2cm" svg:height="1cm"><draw:object><math xmlns="http://www.w3.org/1998/Math/MathML">` +
		`<semantics><mrow/><annotation encoding="StarMath 5.0">a+b</annotation></semantics></math></draw:object></draw:frame>`)}.bytes())
	if len(doc.Slides[0].Shapes) != 0 {
		t.Errorf("shapes %+v, want none", doc.Slides[0].Shapes)
	}
	found := false
	for _, w := range warnings {
		found = found || strings.Contains(w.Message, "StarMath")
	}
	if !found {
		t.Errorf("warnings %v, want one of the StarMath text", warnings)
	}
}

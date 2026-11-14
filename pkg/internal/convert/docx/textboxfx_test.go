package docx

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestTextBoxEffects locks that a text box with a glow draws the glow's
// bitmap around the box and its text over it, as a shape with the effect
// does, and that a box without effects draws no bitmap.
//
// # Corpus evidence
//
//   - libreoffice/docx: ooxmlexport/fdo73247.docx prints its text box's
//     glow and reflection as bitmaps around the box.
func TestTextBoxEffects(t *testing.T) {
	doc := func(effects string) *typeset.Document {
		sp := `<wps:wsp><wps:cNvPr id="1" name="s"/><wps:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="914400" cy="914400"/></a:xfrm>` +
			`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="4472C4"/></a:solidFill><a:ln><a:noFill/></a:ln>` +
			effects + `</wps:spPr><wps:txbx><w:txbxContent><w:p><w:r><w:t>Boxed</w:t></w:r></w:p></w:txbxContent></wps:txbx><wps:bodyPr/></wps:wsp>`
		body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="914400" cy="914400"/><wp:docPr id="1" name="s"/>` +
			`<a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` + sp +
			`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
		return convertBytes(t, docx(t, body, nil), office.Options{})
	}
	images := func(d *typeset.Document) int {
		n := 0
		var walk func([]typeset.Fragment)
		walk = func(frags []typeset.Fragment) {
			for _, f := range frags {
				switch v := f.(type) {
				case *typeset.Image:
					n++
				case *typeset.Group:
					walk(v.Content)
				}
			}
		}
		walk(d.Pages[0].Content)
		return n
	}
	glow := doc(`<a:effectLst><a:glow rad="139700"><a:srgbClr val="FFC000"><a:alpha val="60000"/></a:srgbClr></a:glow></a:effectLst>`)
	if n := images(glow); n == 0 {
		t.Error("a text box with a glow draws no bitmap")
	}
	if text := docText(glow); !strings.Contains(text, "Boxed") {
		t.Errorf("text %q lacks the box's text", text)
	}
	if n := images(doc("")); n != 0 {
		t.Errorf("a text box without effects draws %d bitmaps", n)
	}
}

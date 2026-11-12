package pptx

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_SVGPictureWarnings checks that what the svg renderer does
// not draw in an SVG picture of a slide reaches the conversion's warnings,
// once, named after the picture's part.
func TestConvert_SVGPictureWarnings(t *testing.T) {
	t.Parallel()
	pic := `<p:pic><p:nvPicPr><p:cNvPr id="4" name="Pic"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="rIdP"/><a:stretch/></p:blipFill>` +
		`<p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="508000" cy="254000"/></a:xfrm><a:prstGeom prst="rect"/></p:spPr></p:pic>`
	files := deckFiles(testDeck{
		slides: []testSlide{{tree: pic, rels: `<Relationship Id="rIdP" Type="` + tRelImage + `" Target="../media/a.svg"/>`}},
		parts:  map[string]string{"ppt/media/a.svg": `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><mask id="m"/><rect width="40" height="20" mask="url(#m)"/></svg>`},
	})
	out, err := Convert(context.Background(), bytes.NewReader(zipFiles(t, files)), testFonts(t), office.Options{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range out.Warnings {
		if w.Message == "in the SVG picture ppt/media/a.svg: masks are not drawn; the masked content is drawn unmasked" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d mask warnings, want 1: %+v", n, out.Warnings)
	}
}

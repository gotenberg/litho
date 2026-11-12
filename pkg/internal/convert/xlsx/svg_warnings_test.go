package xlsx

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_SVGPictureWarnings checks that what the svg renderer does
// not draw in an SVG picture of a sheet reaches the conversion's
// warnings, once, named after the picture's part.
func TestConvert_SVGPictureWarnings(t *testing.T) {
	t.Parallel()
	pic := `<xdr:pic><xdr:nvPicPr><xdr:cNvPr id="2" name="P"/><xdr:cNvPicPr/></xdr:nvPicPr><xdr:blipFill><a:blip r:embed="rIdImg"/></xdr:blipFill>` +
		`<xdr:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="127000" cy="63500"/></a:xfrm><a:prstGeom prst="rect"/></xdr:spPr></xdr:pic>`
	drawing := `<xdr:wsDr ` + drawingNS + `><xdr:absoluteAnchor><xdr:pos x="0" y="0"/><xdr:ext cx="508000" cy="254000"/>` + pic + `<xdr:clientData/></xdr:absoluteAnchor></xdr:wsDr>`
	files := workbookFiles([]testSheet{{
		name: "S", xml: `<sheetData><row r="1"><c r="A1"><v>1</v></c></row></sheetData><drawing r:id="rIdD"/>`,
		rels: `<Relationship Id="rIdD" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/>`,
	}}, minimalStyles, "", "", map[string]string{
		"xl/drawings/drawing1.xml": drawing,
		"xl/drawings/_rels/drawing1.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdImg" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/a.svg"/></Relationships>`,
		"xl/media/a.svg": `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><mask id="m"/><rect width="40" height="20" mask="url(#m)"/></svg>`,
	})
	out, err := Convert(context.Background(), bytes.NewReader(zipFiles(t, files)), testFonts(t), office.Options{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range out.Warnings {
		if w.Message == "in the SVG picture xl/media/a.svg: masks are not drawn; the masked content is drawn unmasked" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d mask warnings, want 1: %+v", n, out.Warnings)
	}
}

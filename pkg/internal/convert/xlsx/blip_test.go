package xlsx

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// TestRead_ColorChange checks a picture's clrChange: its opaque black
// pixels take the transparent black clrTo names, its red one stays.
func TestRead_ColorChange(t *testing.T) {
	t.Parallel()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	drawing := `<xdr:wsDr ` + drawingNS + `><xdr:absoluteAnchor><xdr:pos x="0" y="0"/><xdr:ext cx="254000" cy="127000"/>` +
		`<xdr:pic><xdr:nvPicPr><xdr:cNvPr id="2" name="Logo"/><xdr:cNvPicPr/></xdr:nvPicPr><xdr:blipFill><a:blip r:embed="rIdImg">` +
		`<a:clrChange><a:clrFrom><a:prstClr val="black"/></a:clrFrom><a:clrTo><a:prstClr val="black"><a:alpha val="0"/></a:prstClr></a:clrTo></a:clrChange></a:blip></xdr:blipFill>` +
		`<xdr:spPr><a:prstGeom prst="rect"/></xdr:spPr></xdr:pic><xdr:clientData/></xdr:absoluteAnchor></xdr:wsDr>`
	files := workbookFiles([]testSheet{{
		name: "S", xml: `<sheetData/><drawing r:id="rIdD"/>`,
		rels: `<Relationship Id="rIdD" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/>`,
	}}, minimalStyles, "", "", map[string]string{
		"xl/drawings/drawing1.xml": drawing,
		"xl/drawings/_rels/drawing1.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdImg" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/></Relationships>`,
		"xl/media/image1.png": buf.String(),
	})
	doc, _ := readBytes(t, zipFiles(t, files))
	ds := doc.Sheets[0].Drawings
	if len(ds) != 1 || ds[0].Picture == nil || ds[0].Picture.Source == nil {
		t.Fatalf("drawings %+v", ds)
	}
	got, err := ds[0].Picture.Source.Decode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	black := color.NRGBAModel.Convert(got.At(0, 0)).(color.NRGBA)
	red := color.NRGBAModel.Convert(got.At(1, 0)).(color.NRGBA)
	if black.A != 0 || red != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("pixels %+v %+v, want the black one transparent and the red one kept", black, red)
	}
}

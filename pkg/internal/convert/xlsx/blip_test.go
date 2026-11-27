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

// TestRead_Recolor checks a picture's colour effects against the levels
// Excel prints for eight bands under each: black, white, grey 128, red,
// green 160, blue, FFC800 and 40B4DC.
func TestRead_Recolor(t *testing.T) {
	t.Parallel()
	bands := []color.NRGBA{{A: 255}, {R: 255, G: 255, B: 255, A: 255}, {R: 128, G: 128, B: 128, A: 255}, {R: 255, A: 255},
		{G: 160, A: 255}, {B: 255, A: 255}, {R: 255, G: 200, A: 255}, {R: 64, G: 180, B: 220, A: 255}}
	img := image.NewNRGBA(image.Rect(0, 0, len(bands), 1))
	for i, c := range bands {
		img.SetNRGBA(i, 0, c)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, blip string
		want       [8]uint32
	}{
		{"grayscl", `<a:grayscl/>`, [8]uint32{0x000000, 0xffffff, 0x808080, 0x363636, 0x727272, 0x121212, 0xc5c5c5, 0x9e9e9e}},
		{"washout", `<a:lum bright="70000" contrast="-70000"/>`, [8]uint32{0xcdcdcd, 0xffffff, 0xf3f3f3, 0xffcdcd, 0xcdfdcd, 0xcdcdff, 0xffffcd, 0xe0ffff}},
		{"bright", `<a:lum bright="30000"/>`, [8]uint32{0x4c4c4c, 0xffffff, 0xcccccc, 0xff4c4c, 0x4cec4c, 0x4c4cff, 0xffff4c, 0x8cffff}},
		{"contrast", `<a:lum contrast="50000"/>`, [8]uint32{0x000000, 0xffffff, 0x808080, 0xff0000, 0x00c000, 0x0000ff, 0xffff00, 0x00e8ff}},
		{"duotone", `<a:duotone><a:prstClr val="black"/><a:srgbClr val="FF8000"/></a:duotone>`, [8]uint32{0x000000, 0xff8000, 0x804000, 0x361b00, 0x723900, 0x120900, 0xc56300, 0x9e4f00}},
		{"biLevel 50", `<a:biLevel thresh="50000"/>`, [8]uint32{0x000000, 0xffffff, 0xffffff, 0x000000, 0x000000, 0x000000, 0xffffff, 0xffffff}},
		{"biLevel 25", `<a:biLevel thresh="25000"/>`, [8]uint32{0x000000, 0xffffff, 0xffffff, 0x000000, 0xffffff, 0x000000, 0xffffff, 0xffffff}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			drawing := `<xdr:wsDr ` + drawingNS + `><xdr:absoluteAnchor><xdr:pos x="0" y="0"/><xdr:ext cx="254000" cy="127000"/>` +
				`<xdr:pic><xdr:nvPicPr><xdr:cNvPr id="2" name="Bands"/><xdr:cNvPicPr/></xdr:nvPicPr><xdr:blipFill><a:blip r:embed="rIdImg">` +
				tc.blip + `</a:blip></xdr:blipFill><xdr:spPr><a:prstGeom prst="rect"/></xdr:spPr></xdr:pic><xdr:clientData/></xdr:absoluteAnchor></xdr:wsDr>`
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
			for i, want := range tc.want {
				c := color.NRGBAModel.Convert(got.At(got.Bounds().Min.X+i, got.Bounds().Min.Y)).(color.NRGBA)
				if hex := uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B); hex != want || c.A != 255 {
					t.Errorf("band %d = %06x alpha %d, want %06x", i, hex, c.A, want)
				}
			}
		})
	}
}

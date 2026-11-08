package xlsx

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

// pngBytes returns a 4 by 2 pixel PNG.
func pngBytes(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// drawingNS declares the namespaces of a drawing part.
const drawingNS = `xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart"`

// TestRead_Drawings checks the three anchors, a picture, a chart frame,
// shapes and a group drawn as one picture, and a hidden shape left out.
func TestRead_Drawings(t *testing.T) {
	t.Parallel()
	pic := `<xdr:pic><xdr:nvPicPr><xdr:cNvPr id="2" name="Logo"/><xdr:cNvPicPr/></xdr:nvPicPr><xdr:blipFill><a:blip r:embed="rIdImg"/></xdr:blipFill>` +
		`<xdr:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="127000" cy="63500"/></a:xfrm><a:prstGeom prst="rect"/></xdr:spPr></xdr:pic>`
	shape := func(name, geom, extra string) string {
		return `<xdr:sp><xdr:nvSpPr><xdr:cNvPr id="3" name="` + name + `"` + extra + `/><xdr:cNvSpPr/></xdr:nvSpPr><xdr:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="12700" cy="12700"/></a:xfrm>` +
			`<a:prstGeom prst="` + geom + `"/><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill></xdr:spPr></xdr:sp>`
	}
	drawing := `<xdr:wsDr ` + drawingNS + `>` +
		`<xdr:twoCellAnchor editAs="oneCell"><xdr:from><xdr:col>1</xdr:col><xdr:colOff>12700</xdr:colOff><xdr:row>2</xdr:row><xdr:rowOff>25400</xdr:rowOff></xdr:from>` +
		`<xdr:to><xdr:col>3</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>5</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to>` + pic + `<xdr:clientData/></xdr:twoCellAnchor>` +
		`<xdr:oneCellAnchor><xdr:from><xdr:col>0</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>0</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from><xdr:ext cx="254000" cy="127000"/>` +
		`<xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="4" name="Chart 1"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr><xdr:xfrm/>` +
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart r:id="rIdChart"/></a:graphicData></a:graphic></xdr:graphicFrame><xdr:clientData/></xdr:oneCellAnchor>` +
		`<xdr:absoluteAnchor><xdr:pos x="127000" y="254000"/><xdr:ext cx="127000" cy="127000"/>` + shape("Oval", "ellipse", "") + `<xdr:clientData/></xdr:absoluteAnchor>` +
		`<xdr:absoluteAnchor><xdr:pos x="0" y="0"/><xdr:ext cx="127000" cy="127000"/>` + shape("Gone", "rect", ` hidden="1"`) + `<xdr:clientData/></xdr:absoluteAnchor>` +
		`<xdr:absoluteAnchor><xdr:pos x="0" y="0"/><xdr:ext cx="254000" cy="127000"/><xdr:grpSp><xdr:nvGrpSpPr><xdr:cNvPr id="9" name="Group"/><xdr:cNvGrpSpPr/></xdr:nvGrpSpPr>` +
		`<xdr:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="254000" cy="127000"/><a:chOff x="0" y="0"/><a:chExt cx="25400" cy="12700"/></a:xfrm></xdr:grpSpPr>` +
		shape("In", "star5", "") + `</xdr:grpSp><xdr:clientData/></xdr:absoluteAnchor>` +
		`</xdr:wsDr>`
	files := workbookFiles([]testSheet{{
		name: "S", xml: `<sheetData/><drawing r:id="rIdD"/>`,
		rels: `<Relationship Id="rIdD" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/>`,
	}},
		minimalStyles, "", "", map[string]string{
			"xl/drawings/drawing1.xml": drawing,
			"xl/drawings/_rels/drawing1.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
				`<Relationship Id="rIdImg" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/>` +
				`<Relationship Id="rIdChart" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart" Target="../charts/chart1.xml"/></Relationships>`,
			"xl/media/image1.png":  pngBytes(t),
			"xl/charts/chart1.xml": `<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart"/>`,
		})
	doc, warnings := readBytes(t, zipFiles(t, files))
	ds := doc.Sheets[0].Drawings
	if len(ds) != 4 {
		t.Fatalf("%d drawings, want 4: %+v", len(ds), ds)
	}
	if d := ds[0]; d.Picture == nil || d.Picture.Source == nil || d.Name != "Logo" || d.From != (sheet.Anchor{Row: 2, Column: 1, DX: 1, DY: 2}) || d.To == nil || *d.To != (sheet.Anchor{Row: 5, Column: 3}) {
		t.Errorf("picture %+v", d)
	}
	if d := ds[1]; d.Chart == nil || d.Picture != nil || d.Width != 20 || d.Height != 10 || d.To != nil {
		t.Errorf("chart %+v", d)
	}
	if d := ds[2]; !d.Absolute || d.X != 10 || d.Y != 20 || d.Width != 10 || d.Picture == nil || d.Picture.Vector == nil {
		t.Errorf("shape %+v", d)
	} else if frags := d.Picture.Vector(context.Background(), compose.Rect{Width: 10, Height: 10}); len(frags) != 1 {
		t.Errorf("shape fragments %+v", frags)
	}
	if d := ds[3]; d.Name != "Group" || d.Picture == nil {
		t.Errorf("group %+v", d)
	}
	if !hasWarning(warnings, "star5 shape is drawn as its bounding box") || !hasWarning(warnings, "the chart has no chart type the reader draws") {
		t.Errorf("warnings %v", warnings)
	}
}

// TestRead_ShapeText checks a text box's paragraphs drawn over its shape:
// aligned, anchored and in its runs' sizes and colours.
func TestRead_ShapeText(t *testing.T) {
	t.Parallel()
	drawing := `<xdr:wsDr ` + drawingNS + `><xdr:absoluteAnchor><xdr:pos x="0" y="0"/><xdr:ext cx="2540000" cy="1270000"/>` +
		`<xdr:sp><xdr:nvSpPr><xdr:cNvPr id="2" name="Box"/><xdr:cNvSpPr txBox="1"/></xdr:nvSpPr><xdr:spPr><a:prstGeom prst="rect"/><a:noFill/></xdr:spPr>` +
		`<xdr:txBody><a:bodyPr lIns="0" tIns="0" rIns="0" bIns="0" anchor="b"/><a:p><a:pPr algn="r"/><a:r><a:rPr sz="2000" b="1"><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></a:rPr><a:t>Hi</a:t></a:r></a:p></xdr:txBody></xdr:sp>` +
		`<xdr:clientData/></xdr:absoluteAnchor></xdr:wsDr>`
	files := workbookFiles([]testSheet{{name: "S", xml: `<sheetData/><drawing r:id="rIdD"/>`,
		rels: `<Relationship Id="rIdD" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/>`}},
		minimalStyles, "", "", map[string]string{"xl/drawings/drawing1.xml": drawing})
	doc, _ := readBytes(t, zipFiles(t, files))
	ds := doc.Sheets[0].Drawings
	if len(ds) != 1 || ds[0].Picture == nil || ds[0].Picture.Vector == nil {
		t.Fatalf("drawings %+v", ds)
	}
	frags := ds[0].Picture.Vector(context.Background(), compose.Rect{Width: 200, Height: 100})
	var texts []*compose.Text
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch f := f.(type) {
			case *compose.Text:
				texts = append(texts, f)
			case *compose.Group:
				walk(f.Content)
			}
		}
	}
	walk(frags)
	if len(texts) != 1 {
		t.Fatalf("%d text runs, want 1", len(texts))
	}
	tx := texts[0]
	if tx.Size != 20 || tx.Color != (compose.Color{R: 1, A: 1}) || tx.Origin.X != 180 || tx.Origin.Y <= 80 || tx.Origin.Y >= 100 {
		t.Errorf("run at %+v size %v colour %+v, want 20-point red text right-aligned at the bottom", tx.Origin, tx.Size, tx.Color)
	}
}

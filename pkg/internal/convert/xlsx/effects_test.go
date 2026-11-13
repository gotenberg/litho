package xlsx

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestDrawing_ShapeShadowUnderTheShape reads a shape with an outer shadow
// and one keeping its shadow in a14:hiddenEffects: the first paints a
// bitmap under its paths and past its anchor's clip, the second none.
func TestDrawing_ShapeShadowUnderTheShape(t *testing.T) {
	t.Parallel()
	sp := func(effects string) string {
		return `<xdr:sp><xdr:nvSpPr><xdr:cNvPr id="2" name="Box"/><xdr:cNvSpPr/></xdr:nvSpPr><xdr:spPr><a:prstGeom prst="rect"/><a:solidFill><a:srgbClr val="4472C4"/></a:solidFill>` +
			effects + `</xdr:spPr></xdr:sp>`
	}
	for _, tc := range []struct {
		name, effects string
		images        int
	}{
		{"shadow", `<a:effectLst><a:outerShdw blurRad="57150" dist="38100" dir="2700000" algn="tl"><a:prstClr val="black"><a:alpha val="40000"/></a:prstClr></a:outerShdw></a:effectLst>`, 1},
		{"hidden", `<a:extLst><a:ext uri="{AF507438-7753-43E0-B8FC-AC1667EBCBE1}"><a14:hiddenEffects xmlns:a14="http://schemas.microsoft.com/office/drawing/2010/main"><a:effectLst><a:outerShdw dist="35921" dir="2700000" algn="ctr"><a:srgbClr val="808080"/></a:outerShdw></a:effectLst></a14:hiddenEffects></a:ext></a:extLst>`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drawing := `<xdr:wsDr ` + drawingNS + `><xdr:absoluteAnchor><xdr:pos x="0" y="0"/><xdr:ext cx="914400" cy="914400"/>` +
				sp(tc.effects) + `<xdr:clientData/></xdr:absoluteAnchor></xdr:wsDr>`
			files := workbookFiles([]testSheet{{
				name: "S", xml: `<sheetData/><drawing r:id="rIdD"/>`,
				rels: `<Relationship Id="rIdD" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/>`,
			}},
				minimalStyles, "", "", map[string]string{"xl/drawings/drawing1.xml": drawing})
			doc, _ := readBytes(t, zipFiles(t, files))
			ds := doc.Sheets[0].Drawings
			if len(ds) != 1 || ds[0].Picture == nil || ds[0].Picture.Vector == nil {
				t.Fatalf("drawings %+v", ds)
			}
			frags := ds[0].Picture.Vector(context.Background(), typeset.Rect{X: 10, Y: 10, Width: 72, Height: 72})
			var images []*typeset.Image
			for _, f := range frags {
				if img, ok := f.(*typeset.Image); ok {
					images = append(images, img)
				}
			}
			if len(images) != tc.images {
				t.Fatalf("%d images outside the clip, want %d", len(images), tc.images)
			}
			if tc.images == 1 {
				img := images[0]
				// The shadow moves 3 points down and right and blurs 1.5
				// points either side three deviations wide.
				if img.Rect.X+img.Rect.Width < 10+72+3 || img.Rect.Y+img.Rect.Height < 10+72+3 || img.Parent != nil {
					t.Errorf("shadow over %+v, want past the shape's box to the bottom right", img.Rect)
				}
				if _, ok := frags[len(frags)-1].(*typeset.Group); !ok {
					t.Errorf("last fragment %T, want the shape's clipped group over its shadow", frags[len(frags)-1])
				}
			}
		})
	}
}

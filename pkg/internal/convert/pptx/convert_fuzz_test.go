package pptx

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
)

// fuzzTree is a slide's shape tree exercising most of the reader:
// placeholders, a styled shape with text, a group, a picture, a table and
// a connector.
const fuzzTree = `<p:sp><p:nvSpPr><p:cNvPr id="2" name="T"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:p><a:r><a:t>Title</a:t></a:r></a:p></p:txBody></p:sp>` +
	`<p:sp><p:nvSpPr><p:cNvPr id="3" name="S"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm rot="60000"><a:off x="1" y="2"/><a:ext cx="300000" cy="200000"/></a:xfrm><a:prstGeom prst="wedgeRectCallout"><a:avLst><a:gd name="adj1" fmla="val -20000"/></a:avLst></a:prstGeom>` +
	`<a:gradFill><a:gsLst><a:gs pos="0"><a:schemeClr val="accent1"><a:lumMod val="50000"/></a:schemeClr></a:gs></a:gsLst><a:lin ang="1"/></a:gradFill><a:ln w="9525"><a:custDash><a:ds d="100000" sp="50000"/></a:custDash></a:ln></p:spPr>` +
	`<p:style><a:lnRef idx="1"><a:schemeClr val="accent2"/></a:lnRef><a:fillRef idx="2"><a:schemeClr val="accent1"/></a:fillRef><a:effectRef idx="0"><a:schemeClr val="accent1"/></a:effectRef><a:fontRef idx="major"><a:schemeClr val="lt1"/></a:fontRef></p:style>` +
	`<p:txBody><a:bodyPr vert="eaVert" numCol="2"><a:normAutofit fontScale="90%"/></a:bodyPr><a:lstStyle><a:lvl2pPr><a:buAutoNum type="arabicParenR"/></a:lvl2pPr></a:lstStyle><a:p><a:pPr lvl="1"><a:lnSpc><a:spcPct val="90000"/></a:lnSpc></a:pPr><a:r><a:rPr sz="1200" b="1"><a:hlinkClick r:id="rIdH"/></a:rPr><a:t>one</a:t></a:r><a:br/><a:fld id="{1}" type="slidenum"><a:t>1</a:t></a:fld></a:p></p:txBody></p:sp>` +
	`<p:grpSp><p:nvGrpSpPr><p:cNvPr id="4" name="G"/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="10" cy="10"/><a:chOff x="0" y="0"/><a:chExt cx="5" cy="5"/></a:xfrm></p:grpSpPr>` +
	`<p:pic><p:nvPicPr><p:cNvPr id="5" name="P"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="rIdP"/><a:srcRect t="10000"/></p:blipFill><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="5" cy="5"/></a:xfrm></p:spPr></p:pic></p:grpSp>` +
	`<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="6" name="Tb"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="0" y="0"/><a:ext cx="10" cy="10"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table">` +
	`<a:tbl><a:tblPr firstRow="1" bandCol="1" lastCol="1"/><a:tblGrid><a:gridCol w="5"/><a:gridCol w="5"/></a:tblGrid><a:tr h="5"><a:tc rowSpan="2"><a:txBody><a:bodyPr/><a:p><a:r><a:t>c</a:t></a:r></a:p></a:txBody><a:tcPr/></a:tc></a:tr><a:tr h="5"><a:tc vMerge="1"><a:tcPr/></a:tc></a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>` +
	`<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="7" name="C"/><p:cNvCxnSpPr/><p:nvPr/></p:nvCxnSpPr><p:spPr><a:xfrm flipV="1"><a:off x="0" y="0"/><a:ext cx="5" cy="5"/></a:xfrm><a:prstGeom prst="bentConnector3"/></p:spPr><p:style><a:lnRef idx="3"><a:schemeClr val="tx1"/></a:lnRef><a:fillRef idx="0"><a:schemeClr val="tx1"/></a:fillRef><a:effectRef idx="0"><a:schemeClr val="tx1"/></a:effectRef><a:fontRef idx="minor"/></p:style></p:cxnSp>`

func FuzzConvert(f *testing.F) {
	f.Add([]byte(fuzzTree))
	f.Add([]byte(fuzzTree[:len(fuzzTree)/2]))
	fonts := testFonts(f)
	png := pngBytes(f)
	f.Fuzz(func(t *testing.T, tree []byte) {
		files := deckFiles(testDeck{
			slides: []testSlide{{
				tree: string(tree), notes: string(tree),
				rels: `<Relationship Id="rIdP" Type="` + tRelImage + `" Target="../media/image1.png"/><Relationship Id="rIdH" Type="` + tRelLink + `" Target="x" TargetMode="External"/>`,
			}},
			layoutTree: string(tree),
			parts:      map[string]string{"ppt/media/image1.png": png},
		})
		doc, _, err := load(context.Background(), bytes.NewReader(zipFiles(t, files)), fonts, office.Options{})
		if err != nil {
			return
		}
		for _, s := range doc.Slides {
			checkShapes(t, s.Shapes)
		}
	})
}

// checkShapes checks the model's invariants on shapes: boxes and sizes
// not negative, gradients of two stops at least, tables of rows as wide
// as their grid.
func checkShapes(t *testing.T, shapes []*slides.Shape) {
	for _, s := range shapes {
		if s.Box.Width < 0 || s.Box.Height < 0 {
			t.Fatalf("negative box %+v", s.Box)
		}
		if g := s.Fill.Gradient; s.Fill.Kind == slides.FillGradient && (g == nil || len(g.Stops) < 2) {
			t.Fatalf("gradient %+v", g)
		}
		if tb := s.Table; tb != nil {
			for _, r := range tb.Rows {
				if len(r.Cells) != len(tb.ColumnWidths) {
					t.Fatalf("row of %d cells in a grid of %d", len(r.Cells), len(tb.ColumnWidths))
				}
			}
		}
		if s.Text != nil {
			for _, p := range s.Text.Paragraphs {
				for _, r := range p.Runs {
					if r.Style.Size <= 0 {
						t.Fatalf("run size %v", r.Style.Size)
					}
				}
			}
		}
		checkShapes(t, s.Kids)
	}
}

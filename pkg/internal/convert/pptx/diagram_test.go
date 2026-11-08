package pptx

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
)

const dgmDecl = `xmlns:dgm="http://schemas.openxmlformats.org/drawingml/2006/diagram" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:dsp="http://schemas.microsoft.com/office/drawing/2008/diagram"`

// diagramDataXML returns a data model of two nodes "a" and "b" under the
// document point, laid out by the layout node names root and node; the
// drawing relationship rIdD when drawing.
func diagramDataXML(drawing bool) string {
	var pts, cxns strings.Builder
	pts.WriteString(`<dgm:pt modelId="0" type="doc"/><dgm:pt modelId="p0" type="pres"><dgm:prSet presAssocID="0" presName="root"/><dgm:spPr/></dgm:pt>`)
	cxns.WriteString(`<dgm:cxn modelId="c0" type="presOf" srcId="0" destId="p0"/>`)
	for i, t := range []string{"a", "b"} {
		fmt.Fprintf(&pts, `<dgm:pt modelId="n%d"><dgm:prSet/><dgm:spPr/><dgm:t><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US"/><a:t>%s</a:t></a:r></a:p></dgm:t></dgm:pt>`, i, t)
		fmt.Fprintf(&pts, `<dgm:pt modelId="pn%d" type="pres"><dgm:prSet presAssocID="n%d" presName="node" presStyleLbl="node1" presStyleIdx="%d"/><dgm:spPr/></dgm:pt>`, i, i, i)
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="x%d" srcId="0" destId="n%d" srcOrd="%d" parTransId="t%d" sibTransId="s%d"/><dgm:cxn modelId="o%d" type="presOf" srcId="n%d" destId="pn%d"/>`, i, i, i, i, i, i, i, i)
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="r%d" type="presParOf" srcId="p0" destId="pn%d" srcOrd="%d"/>`, i, i, i)
	}
	ext := ""
	if drawing {
		ext = `<dgm:extLst><a:ext uri="http://schemas.microsoft.com/office/drawing/2008/diagram"><dsp:dataModelExt relId="rIdD" minVer="http://schemas.openxmlformats.org/drawingml/2006/diagram"/></a:ext></dgm:extLst>`
	}
	return `<dgm:dataModel ` + dgmDecl + `><dgm:ptLst>` + pts.String() + `</dgm:ptLst><dgm:cxnLst>` + cxns.String() + `</dgm:cxnLst>` +
		`<dgm:bg><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill></dgm:bg>` + ext + `</dgm:dataModel>`
}

// diagramDeck returns a one-slide deck with a SmartArt frame of 254 by 127
// points at 12.7, 25.4: two ellipses side by side, the drawing part
// holding the shapes drawing when it is not "".
func diagramDeck(drawing string) testDeck {
	frame := `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="4" name="Diagram 1"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr>` +
		`<p:xfrm><a:off x="161290" y="322580"/><a:ext cx="3225800" cy="1612900"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/diagram">` +
		`<dgm:relIds ` + dgmDecl + ` r:dm="rIdDm" r:lo="rIdLo" r:qs="rIdQs" r:cs="rIdCs"/></a:graphicData></a:graphic></p:graphicFrame>`
	rels := `<Relationship Id="rIdDm" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/diagramData" Target="../diagrams/data1.xml"/>` +
		`<Relationship Id="rIdLo" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/diagramLayout" Target="../diagrams/layout1.xml"/>` +
		`<Relationship Id="rIdQs" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/diagramQuickStyle" Target="../diagrams/quickStyle1.xml"/>` +
		`<Relationship Id="rIdCs" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/diagramColors" Target="../diagrams/colors1.xml"/>`
	parts := map[string]string{
		"ppt/diagrams/data1.xml": diagramDataXML(drawing != ""),
		"ppt/diagrams/layout1.xml": `<dgm:layoutDef ` + dgmDecl + `><dgm:layoutNode name="root"><dgm:alg type="lin"/><dgm:shape/>` +
			`<dgm:forEach axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="ellipse"/></dgm:layoutNode></dgm:forEach></dgm:layoutNode></dgm:layoutDef>`,
		"ppt/diagrams/quickStyle1.xml": `<dgm:styleDef ` + dgmDecl + `><dgm:styleLbl name="node1"><dgm:style><a:lnRef idx="2"><a:scrgbClr r="0" g="0" b="0"/></a:lnRef>` +
			`<a:fillRef idx="1"><a:scrgbClr r="0" g="0" b="0"/></a:fillRef><a:effectRef idx="0"/><a:fontRef idx="minor"><a:schemeClr val="lt1"/></a:fontRef></dgm:style></dgm:styleLbl></dgm:styleDef>`,
		"ppt/diagrams/colors1.xml": `<dgm:colorsDef ` + dgmDecl + `><dgm:styleLbl name="node0"><dgm:txFillClrLst><a:schemeClr val="lt1"/><a:schemeClr val="dk1"/></dgm:txFillClrLst></dgm:styleLbl><dgm:styleLbl name="node1"><dgm:fillClrLst meth="repeat"><a:schemeClr val="accent1"/><a:schemeClr val="accent2"/></dgm:fillClrLst>` +
			`<dgm:linClrLst><a:schemeClr val="lt1"/></dgm:linClrLst><dgm:txFillClrLst><a:schemeClr val="lt1"/></dgm:txFillClrLst></dgm:styleLbl></dgm:colorsDef>`,
	}
	if drawing != "" {
		rels += `<Relationship Id="rIdD" Type="` + relDiagramDrawing + `" Target="../diagrams/drawing1.xml"/>`
		parts["ppt/diagrams/drawing1.xml"] = `<dsp:drawing ` + dgmDecl + `><dsp:spTree><dsp:nvGrpSpPr><dsp:cNvPr id="0" name=""/><dsp:cNvGrpSpPr/></dsp:nvGrpSpPr><dsp:grpSpPr/>` + drawing + `</dsp:spTree></dsp:drawing>`
	}
	return testDeck{slides: []testSlide{{tree: frame, rels: rels}}, parts: parts}
}

func TestDiagram(t *testing.T) {
	white := css.Color{R: 1, G: 1, B: 1, A: 1}
	cases := []struct {
		name    string
		drawing string
		check   func(t *testing.T, g *slides.Shape)
	}{
		{"laid out from the layout definition", "", func(t *testing.T, g *slides.Shape) {
			if len(g.Kids) != 3 {
				t.Fatalf("%d kids, want the background and two ellipses", len(g.Kids))
			}
			bg := g.Kids[0]
			if bg.Box != (compose.Rect{Width: 254, Height: 127}) || bg.Fill.Color != (css.Color{G: 1, A: 1}) {
				t.Errorf("background %+v %+v", bg.Box, bg.Fill)
			}
			for i, want := range []struct {
				box  compose.Rect
				fill css.Color
				text string
			}{
				{compose.Rect{Width: 127, Height: 127}, css.Color{R: 0x4F / 255.0, G: 0x81 / 255.0, B: 0xBD / 255.0, A: 1}, "a"},
				{compose.Rect{X: 127, Width: 127, Height: 127}, css.Color{R: 0xC0 / 255.0, G: 0x50 / 255.0, B: 0x4D / 255.0, A: 1}, "b"},
			} {
				k := g.Kids[i+1]
				if k.Box != want.box || k.Geometry == nil || k.Geometry.Preset != "ellipse" {
					t.Errorf("ellipse %d box %+v geometry %+v", i, k.Box, k.Geometry)
				}
				if k.Fill.Kind != slides.FillSolid || !nearColor(k.Fill.Color, want.fill) || k.Line.Width != 2 || k.Line.Color != white {
					t.Errorf("ellipse %d fill %+v line %+v", i, k.Fill, k.Line)
				}
				if k.Text == nil || plain(k.Text) != want.text || k.Text.Anchor != slides.AnchorMiddle || k.Text.Autofit != slides.AutofitShrink ||
					k.Text.Paragraphs[0].Align != css.AlignCenter || k.Text.Paragraphs[0].Runs[0].Style.Color != white {
					t.Errorf("ellipse %d text %+v", i, k.Text)
				}
			}
		}},
		{"drawn from the saved drawing", `<dsp:sp modelId="{1}"><dsp:nvSpPr><dsp:cNvPr id="0" name="saved"/><dsp:cNvSpPr/></dsp:nvSpPr>` +
			`<dsp:spPr><a:xfrm><a:off x="12700" y="0"/><a:ext cx="254000" cy="127000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="0000FF"/></a:solidFill></dsp:spPr></dsp:sp>`,
			func(t *testing.T, g *slides.Shape) {
				if len(g.Kids) != 2 || g.Kids[1].Name != "saved" || g.Kids[1].Box != (compose.Rect{X: 1, Width: 20, Height: 10}) {
					t.Fatalf("kids %+v", g.Kids)
				}
			}},
		{"a saved text box turns with its shape", `<dsp:sp modelId="{1}"><dsp:nvSpPr><dsp:cNvPr id="0" name="turned"/><dsp:cNvSpPr/></dsp:nvSpPr>` +
			`<dsp:spPr><a:xfrm rot="10800000"><a:off x="0" y="0"/><a:ext cx="254000" cy="127000"/></a:xfrm></dsp:spPr>` +
			`<dsp:txBody><a:bodyPr/><a:p><a:r><a:t>up</a:t></a:r></a:p></dsp:txBody><dsp:txXfrm rot="10800000"><a:off x="0" y="0"/><a:ext cx="254000" cy="127000"/></dsp:txXfrm></dsp:sp>`,
			func(t *testing.T, g *slides.Shape) {
				if len(g.Kids) != 2 || g.Kids[1].Kind != slides.KindGroup || len(g.Kids[1].Kids) != 2 {
					t.Fatalf("kids %+v", g.Kids)
				}
				if shape, text := g.Kids[1].Kids[0], g.Kids[1].Kids[1]; shape.Rotation != 180 || text.Rotation != 0 || plain(text.Text) != "up" {
					t.Errorf("shape turned %v, text turned %v", shape.Rotation, text.Rotation)
				}
			}},
		{"a saved drawing's font references take node0's last text colour", `<dsp:sp modelId="{1}"><dsp:nvSpPr><dsp:cNvPr id="0" name="font"/><dsp:cNvSpPr/></dsp:nvSpPr>` +
			`<dsp:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="254000" cy="127000"/></a:xfrm></dsp:spPr><dsp:style><a:lnRef idx="0"/><a:fillRef idx="0"/><a:effectRef idx="0"/><a:fontRef idx="minor"><a:schemeClr val="lt1"/></a:fontRef></dsp:style>` +
			`<dsp:txBody><a:bodyPr/><a:p><a:r><a:t>dark</a:t></a:r></a:p></dsp:txBody></dsp:sp>`,
			func(t *testing.T, g *slides.Shape) {
				if len(g.Kids) != 2 || g.Kids[1].Text == nil || g.Kids[1].Text.Paragraphs[0].Runs[0].Style.Color != (css.Color{A: 1}) {
					t.Fatalf("kids %+v", g.Kids)
				}
			}},
		{"an empty saved drawing is laid out", `<dsp:cxnSp/>`, func(t *testing.T, g *slides.Shape) {
			if len(g.Kids) != 3 {
				t.Fatalf("%d kids, want the background and two ellipses", len(g.Kids))
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, _ := readDeck(t, diagramDeck(c.drawing))
			g := shapeNamed(doc.Slides[0].Shapes, "Diagram 1")
			if g == nil || g.Kind != slides.KindGroup {
				t.Fatalf("no diagram group: %+v", doc.Slides[0].Shapes)
			}
			if g.Box != (compose.Rect{X: 12.7, Y: 25.4, Width: 254, Height: 127}) || g.Child != (compose.Rect{Width: 254, Height: 127}) {
				t.Errorf("frame %+v child %+v", g.Box, g.Child)
			}
			c.check(t, g)
		})
	}
}

// nearColor reports two colours within rounding of each other.
func nearColor(a, b css.Color) bool {
	d := func(x, y float64) bool { return x-y < 1e-3 && y-x < 1e-3 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

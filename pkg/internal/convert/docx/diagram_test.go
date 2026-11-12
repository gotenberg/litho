package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestDiagram_SavedDrawing checks that a SmartArt frame draws the shapes of
// the drawing Word saved for it, at their offsets in the frame, with their
// text in the font reference's colour, centred as the body anchors it.
func TestDiagram_SavedDrawing(t *testing.T) {
	t.Parallel()
	frame := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="5486400" cy="1270000"/><wp:docPr id="1" name="Diagram 1"/>` +
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/diagram">` +
		`<dgm:relIds xmlns:dgm="http://schemas.openxmlformats.org/drawingml/2006/diagram" r:dm="rIddata1"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	data := `<dgm:dataModel xmlns:dgm="http://schemas.openxmlformats.org/drawingml/2006/diagram" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><dgm:ptLst/>` +
		`<dgm:extLst><a:ext uri="http://schemas.microsoft.com/office/drawing/2008/diagram"><dsp:dataModelExt xmlns:dsp="http://schemas.microsoft.com/office/drawing/2008/diagram" relId="rIddrawing1"/></a:ext></dgm:extLst></dgm:dataModel>`
	drawing := `<dsp:drawing xmlns:dsp="http://schemas.microsoft.com/office/drawing/2008/diagram" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><dsp:spTree>` +
		`<dsp:sp><dsp:spPr><a:xfrm><a:off x="127000" y="254000"/><a:ext cx="1270000" cy="635000"/></a:xfrm><a:prstGeom prst="roundRect"/><a:solidFill><a:srgbClr val="4472C4"/></a:solidFill></dsp:spPr>` +
		`<dsp:style><a:fontRef idx="minor"><a:srgbClr val="FFFFFF"/></a:fontRef></dsp:style>` +
		`<dsp:txBody><a:bodyPr anchor="ctr"/><a:p><a:pPr algn="ctr"/><a:r><a:rPr sz="1800"/><a:t>Plan</a:t></a:r></a:p></dsp:txBody></dsp:sp>` +
		`</dsp:spTree></dsp:drawing>`
	b := build(t, frame, map[string]string{"word/diagrams/data1.xml": data, "word/diagrams/drawing1.xml": drawing})
	figs := role(b.root, "Figure")
	if len(figs) != 1 || figs[0].Alt != "Plan" {
		t.Fatalf("%d figures, want 1 described Plan", len(figs))
	}
	f := figs[0]
	if len(f.Children) != 2 {
		t.Fatalf("%d children, want the shape and its text", len(f.Children))
	}
	sh, tx := f.Children[0], f.Children[1]
	if sh.Kind != layout.Replaced || sh.Style.Left != css.Pts(10) || sh.Style.Top != css.Pts(20) || sh.Style.Width != css.Pts(100) {
		t.Errorf("shape at %v, %v, %v wide", sh.Style.Left, sh.Style.Top, sh.Style.Width)
	}
	if tx.Style.JustifyContent != css.BoxCenter || tx.Style.Position != css.Absolute {
		t.Errorf("text justify %v position %v", tx.Style.JustifyContent, tx.Style.Position)
	}
	if r := runWith(t, tx, "Plan"); r.Style.Color != (css.Color{R: 1, G: 1, B: 1, A: 1}) || r.Style.FontSize != 18 {
		t.Errorf("run colour %v size %v", r.Style.Color, r.Style.FontSize)
	}
	none := build(t, frame, nil)
	if len(none.d.warnings) == 0 {
		t.Error("a diagram without parts was not reported")
	}
}

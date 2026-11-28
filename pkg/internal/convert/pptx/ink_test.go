package pptx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
)

// TestInkStrokes checks that the ink annotations of a slide's legacy VML
// drawing print over its shapes: each ink shape its path, stroked in its
// colour, weight and opacity with its end caps, unfilled, in the box its
// style places; a VML shape without ink prints nothing.
func TestInkStrokes(t *testing.T) {
	vml := `<xml xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office">` +
		`<v:shape id="pen" style="position:absolute;left:38pt;top:39pt;width:168pt;height:71pt" coordorigin="1352,1375" coordsize="5926,2514" path="m1368,2468v-19,71,-1,133,14,208e" filled="f" strokecolor="#7030a0" strokeweight="1.5pt"><v:stroke endcap="round"/><o:ink i="AA=="/></v:shape>` +
		`<v:shape id="marker" style="position:absolute;left:1in;top:294pt;width:44pt;height:68pt" coordsize="1538,2391" path="m0,0l1538,2391e" filled="f" strokecolor="yellow" strokeweight="10pt"><v:stroke opacity="21845f" endcap="square"/><o:ink i="AA=="/></v:shape>` +
		`<v:shape id="plain" style="position:absolute;left:0;top:0;width:10pt;height:10pt" path="m0,0l10,10e"/></xml>`
	doc, _ := readDeck(t, testDeck{
		slides: []testSlide{{
			tree: phShape("Title 1", "title", -1, 0, 0, 0, 0, para("Title")),
			rels: `<Relationship Id="rIdV" Type="` + relVMLDrawing + `" Target="../drawings/vmlDrawing1.vml"/>`,
		}},
		parts: map[string]string{"ppt/drawings/vmlDrawing1.vml": vml},
	})
	shapes := doc.Slides[0].Shapes
	pen, marker := shapeNamed(shapes, "pen"), shapeNamed(shapes, "marker")
	if pen == nil || marker == nil {
		t.Fatalf("no ink shapes in %d shapes", len(shapes))
	}
	if shapeNamed(shapes, "plain") != nil {
		t.Errorf("a VML shape without ink prints")
	}
	if shapes[len(shapes)-1] != marker {
		t.Errorf("ink does not print over the slide's shapes")
	}
	if pen.Box.X != 38 || pen.Box.Y != 39 || pen.Box.Width != 168 || pen.Box.Height != 71 {
		t.Errorf("pen box %+v, want 38, 39, 168 by 71", pen.Box)
	}
	if marker.Box.X != 72 {
		t.Errorf("marker at %v, want 72 (1in)", marker.Box.X)
	}
	if pen.Geometry == nil || pen.Geometry.Custom == nil || pen.Fill.Kind != slides.FillNone {
		t.Errorf("pen geometry %+v fill %+v, want a path unfilled", pen.Geometry, pen.Fill)
	}
	if l := pen.Line; l.Width != 1.5 || l.Cap != slides.CapRound || l.Color.R < 0.43 || l.Color.R > 0.44 || l.Color.A != 1 {
		t.Errorf("pen line %+v, want 1.5 points of 7030A0, round caps", l)
	}
	if l := marker.Line; l.Width != 10 || l.Cap != slides.CapSquare || l.Color.R != 1 || l.Color.G != 1 || l.Color.A < 0.333 || l.Color.A > 0.334 {
		t.Errorf("marker line %+v, want 10 points of yellow a third opaque, square caps", l)
	}
}

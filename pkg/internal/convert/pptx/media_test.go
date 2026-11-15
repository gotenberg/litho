package pptx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestPicture_MediaPosterStretches locks that the poster frame of a video
// prints as any picture, stretched over its whole box, with no frame
// around it.
func TestPicture_MediaPosterStretches(t *testing.T) {
	t.Parallel()
	pic := `<p:pic><p:nvPicPr><p:cNvPr id="4" name="Clip"/><p:cNvPicPr/><p:nvPr><a:videoFile r:link="rIdV"/></p:nvPr></p:nvPicPr><p:blipFill><a:blip r:embed="rIdP"/><a:stretch><a:fillRect/></a:stretch></p:blipFill>` +
		`<p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="508000" cy="254000"/></a:xfrm><a:prstGeom prst="rect"/></p:spPr></p:pic>`
	doc, _ := readDeck(t, testDeck{
		slides: []testSlide{{tree: pic, rels: `<Relationship Id="rIdP" Type="` + tRelImage + `" Target="../media/image1.png"/>` +
			`<Relationship Id="rIdV" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/video" Target="file:///clip.mp4" TargetMode="External"/>`}},
		parts: map[string]string{"ppt/media/image1.png": pngBytes(t)},
	})
	p := shapeNamed(doc.Slides[0].Shapes, "Clip")
	if p == nil || p.Kind != slides.KindPicture || p.Picture == nil {
		t.Fatalf("poster %+v", p)
	}
	if p.Box != (typeset.Rect{Width: 40, Height: 20}) || p.Fill.Kind != slides.FillNone {
		t.Errorf("poster box %+v, fill %+v; want the 40 by 20 box, no fill", p.Box, p.Fill)
	}
}

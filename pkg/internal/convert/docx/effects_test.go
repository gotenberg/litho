package docx

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// firstPath returns the bounds of the first filled path of doc's first page.
func firstPath(t *testing.T, doc *typeset.Document) typeset.Rect {
	t.Helper()
	var r *typeset.Rect
	var walk func(frags []typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Path:
				if r == nil && v.Fill != nil {
					x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
					for _, op := range v.Ops {
						if op.Kind == typeset.ClosePath {
							continue
						}
						p := op.P[0]
						if op.Kind == typeset.CurveTo {
							p = op.P[2]
						}
						x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
					}
					r = &typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(doc.Pages[0].Content)
	if r == nil {
		t.Fatal("no filled path")
	}
	return *r
}

// shapeDoc returns a document holding one inline 72 point square of the
// accent colour, its effect list effects.
func shapeDoc(t *testing.T, effects string) *typeset.Document {
	t.Helper()
	sp := `<wps:wsp><wps:cNvPr id="1" name="s"/><wps:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="914400" cy="914400"/></a:xfrm>` +
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="4472C4"/></a:solidFill><a:ln><a:noFill/></a:ln>` +
		effects + `</wps:spPr><wps:bodyPr/></wps:wsp>`
	body := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="914400" cy="914400"/><wp:docPr id="1" name="s"/>` +
		`<a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` + sp +
		`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	return convertBytes(t, docx(t, body, nil), office.Options{})
}

// TestShapeEffectsFollowWord converts one square with one effect and fits
// the edge of the effect's soft mask above the square against the rows
// fitted on Word's soft masks. The bitmap belongs to the shape's
// structure element, as the shape's paths do.
//
// # Corpus evidence
//
//   - litho/docx shapes-effects-3d: Word leaves the bitmaps of a shape's
//     effects out of any Artifact sequence, where it marks a run's.
//   - litho/docx shapes-effects-3d: Word's 11 point glow fits a Gaussian of
//     1.81 points with its half 5.5 points out of the shape; its 6 point
//     shadow a Gaussian of 1.88 points centred on the outline; its 5 point
//     soft edge a Gaussian of 1.66 points.
func TestShapeEffectsFollowWord(t *testing.T) {
	for _, tc := range []struct {
		name    string
		effects string
		// out is how far above the square's top the mask's half lies,
		// sigma the Gaussian's deviation, both in points, as Word draws.
		out, sigma float64
		replaces   bool
	}{
		{"glow", `<a:effectLst><a:glow rad="139700"><a:srgbClr val="ED7D31"><a:alpha val="40000"/></a:srgbClr></a:glow></a:effectLst>`, 5.5, 1.81, false},
		{"shadow", `<a:effectLst><a:outerShdw blurRad="76200" dist="0" algn="ctr"><a:prstClr val="black"><a:alpha val="40000"/></a:prstClr></a:outerShdw></a:effectLst>`, 0, 1.88, false},
		{"soft edge", `<a:effectLst><a:softEdge rad="63500"/></a:effectLst>`, -5, 1.66, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := shapeDoc(t, tc.effects)
			imgs := effectImages(doc)
			if len(imgs) != 1 {
				t.Fatalf("%d images, want 1", len(imgs))
			}
			img := imgs[0]
			if img.Parent == nil || img.Parent.Role != "P" {
				t.Errorf("effect image tagged %v, want the shape's paragraph, as Word tags it", img.Parent)
			}
			var sq typeset.Rect
			if tc.replaces {
				sq = typeset.Rect{X: img.Rect.X, Y: img.Rect.Y, Width: img.Rect.Width, Height: img.Rect.Height}
			} else {
				sq = firstPath(t, doc)
			}
			ys, as := alphaColumn(t, img, sq.X+sq.Width/2)
			edge, sigma := fitEdge(ys, as, sq.Y-tc.out-4*tc.sigma, sq.Y-tc.out+4*tc.sigma)
			if got := sq.Y - edge; math.Abs(got-tc.out) > 0.3 {
				t.Errorf("half %.2f points out, want %.2f", got, tc.out)
			}
			if math.Abs(sigma-tc.sigma) > 0.15*tc.sigma {
				t.Errorf("deviation %.2f points, want %.2f within 15%%", sigma, tc.sigma)
			}
		})
	}
}

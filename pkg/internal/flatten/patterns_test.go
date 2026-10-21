package flatten

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// pageResource returns the resource name of category on data's first page.
func pageResource(t *testing.T, data []byte, category, name ast.PdfName) (*document.Document, ast.PdfObject) {
	t.Helper()
	ctx := context.Background()
	doc, pg := open(t, data)
	res, ok := pg.Dict["Resources"].(ast.PdfDict)
	if !ok {
		t.Fatalf("page resources %T", pg.Dict["Resources"])
	}
	all, err := document.ResolveDict(ctx, doc, res[category])
	if err != nil {
		t.Fatal(err)
	}
	return doc, all[name]
}

func TestFlattenEmptiesATransparentTilingPattern(t *testing.T) {
	t.Parallel()
	// The pattern's cell paints at ca 0.5: the fill it paints is
	// composited, and the pattern the colour operator still names becomes
	// an empty cell rather than keep its transparency. The renderer lays
	// tiles on whole device pixels, as poppler does, so a cell row the 300
	// dpi composite puts a fraction of a pixel away shades a row of the 72
	// dpi page a few levels apart.
	out, report := checkFlattenedWithin(t, fixture{
		resources: "/Pattern << /P 7 0 R >>",
		content:   "/Pattern cs /P scn 0 0 72 72 re f",
		objs: []string{stream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 24 24] /XStep 24 /YStep 24 "+
			"/Resources << /ExtGState << /G 6 0 R >> >>", "/G gs 1 0 0 rg 0 0 12 12 re f")},
	}, 16)
	if pr := report.Pages[0]; pr.Regions != 1 {
		t.Errorf("report %+v, want the pattern fill composited", pr)
	}
	doc, obj := pageResource(t, out, "Pattern", "P")
	pattern, err := document.ResolveStream(context.Background(), doc, obj)
	if err != nil {
		t.Fatal(err)
	}
	data, err := doc.DecodeStream(context.Background(), pattern)
	if err != nil || len(data) != 0 {
		t.Errorf("pattern cell %q, %v; want it empty", data, err)
	}
}

func TestFlattenDropsAnSMaskThatMasksNothing(t *testing.T) {
	t.Parallel()
	out, report := checkFlattened(t, fixture{
		bare:      true,
		resources: "/XObject << /Im 7 0 R >>",
		content:   "q 50 0 0 50 10 10 cm /Im Do Q",
		objs:      []string{stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask /None", "\x80")},
	})
	if len(report.Pages) != 1 {
		t.Fatalf("report %+v, want the page changed", report)
	}
	doc, obj := pageResource(t, out, "XObject", "Im")
	image, err := document.ResolveStream(context.Background(), doc, obj)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := image.Dict["SMask"]; ok {
		t.Error("the image keeps its /SMask")
	}
}

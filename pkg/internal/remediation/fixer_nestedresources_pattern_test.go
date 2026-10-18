package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// nestedPatternPDF builds a page whose /Resources declare a graphics state
// /GS0 and a tiling pattern /P0 (object 4) whose cell sets /GS0, which the
// pattern's own /Resources lack. patternRes is the pattern's /Resources entry,
// "" for none at all.
func nestedPatternPDF(patternRes string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R "+
			"/Resources << /ExtGState << /GS0 5 0 R >> /Pattern << /P0 4 0 R >> >> >>",
		"<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] "+
			"/XStep 10 /YStep 10 "+patternRes+" /Length 20 >>\nstream\n/GS0 gs 0 0 5 5 re f\nendstream",
		"<< /Type /ExtGState /CA 1 >>",
		"<< /Length 36 >>\nstream\n/Pattern cs /P0 scn 0 0 100 100 re f\nendstream",
	)
}

// TestFixNestedResources_GivesATilingPatternItsOwnResources pins the pattern
// arm. A tiling pattern's cell is a content stream (ISO 32000-1, 8.7.3.1) with
// a /Resources entry of its own (Table 75), and veraPDF 1.30.2 fails ISO
// 19005-2, 6.2.2 t2 at PDF/A-2b and ISO 19005-4, 6.2.2 t2 at PDF/A-4 when a
// name in it resolves only through the page, with the pattern's /Resources
// empty or absent alike. The fixer gave form XObjects and Type 3 fonts their
// names and left a pattern's unrepaired.
func TestFixNestedResources_GivesATilingPatternItsOwnResources(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		res  string
	}{
		{"an empty /Resources", "/Resources << >>"},
		{"no /Resources", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, nestedPatternPDF(tc.res))
			fixes, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
			if err != nil {
				t.Fatalf("fixNestedResources: %v", err)
			}
			if len(fixes) == 0 {
				t.Fatal("the repair reported nothing")
			}
			got, ok := pendingStream(t, alloc, 4)
			if !ok {
				t.Fatal("the tiling pattern was not rewritten")
			}
			res, ok := got.Dict["Resources"].(ast.PdfDict)
			if !ok {
				t.Fatalf("/Resources is %v, want a dictionary", got.Dict["Resources"])
			}
			gs, ok := res["ExtGState"].(ast.PdfDict)
			if !ok {
				t.Fatalf("/Resources/ExtGState is %v, want a dictionary", res["ExtGState"])
			}
			if gs["GS0"] != (ast.PdfIndirectRef{ObjectNum: 5}) {
				t.Errorf("/Resources/ExtGState/GS0 is %v, want 5 0 R", gs["GS0"])
			}
			if got.Dict["PatternType"] != ast.PdfInt(1) {
				t.Errorf("the pattern dictionary lost its entries: %v", got.Dict)
			}
		})
	}
}

// TestFixNestedResources_CopiesNoColourNameIntoAnUncolouredCell pins the
// colour operators of an uncoloured pattern's cell, which it ignores (ISO
// 32000-1, 8.7.3.3): veraPDF reads the name of no cs there, so the /CS0 the
// cell selects is no inherited reference, while its /GS0 is.
//
// # Corpus evidence
//
//   - litho colouroff_2b.pdf: its PUnc cell selects /CS0 only the page
//     defines, and veraPDF 1.30.2 fails no 6.2.2 t2 for it.
func TestFixNestedResources_CopiesNoColourNameIntoAnUncolouredCell(t *testing.T) {
	t.Parallel()

	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /ExtGState << /GS0 5 0 R >> /ColorSpace << /CS0 /DeviceRGB >> /Pattern << /P0 4 0 R >> >> >>",
		"<< /Type /Pattern /PatternType 1 /PaintType 2 /TilingType 1 /BBox [0 0 10 10] "+
			"/XStep 10 /YStep 10 /Resources << >> /Length 28 >>\nstream\n/CS0 cs /GS0 gs 0 0 5 5 re f\nendstream",
		"<< /Type /ExtGState /CA 1 >>",
	)

	doc, alloc := newTestDoc(t, data)
	if _, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	got, ok := pendingStream(t, alloc, 4)
	if !ok {
		t.Fatal("the uncoloured pattern was not rewritten for its /GS0")
	}
	res, _ := got.Dict["Resources"].(ast.PdfDict)
	if _, ok := res["ExtGState"]; !ok {
		t.Errorf("/Resources is %v, want the /GS0 the cell sets", res)
	}
	if _, ok := res["ColorSpace"]; ok {
		t.Errorf("/Resources is %v, want no /CS0: the cell ignores its cs", res)
	}
}

// TestFixNestedResources_LeavesASharedPatternAlone is the pattern half of
// [TestFixNestedResources_LeavesASharedXObjectAlone]: two pages may bind the
// name the cell uses to different objects.
func TestFixNestedResources_LeavesASharedPatternAlone(t *testing.T) {
	t.Parallel()

	shared := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /ExtGState << /GS0 5 0 R >> /Pattern << /P0 4 0 R >> >> >>",
		"<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] "+
			"/XStep 10 /YStep 10 /Resources << >> /Length 20 >>\nstream\n/GS0 gs 0 0 5 5 re f\nendstream",
		"<< /Type /ExtGState /CA 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /ExtGState << /GS0 7 0 R >> /Pattern << /P0 4 0 R >> >> >>",
		"<< /Type /ExtGState /CA 0.5 >>",
	)

	doc, alloc := newTestDoc(t, shared)
	if _, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	if _, rewritten := alloc.Pending()[4]; rewritten {
		t.Error("a pattern referenced by two pages was rewritten; the pages bind /GS0 differently")
	}
}

// TestFixNestedResources_LeavesAShadingPatternAlone pins that only a tiling
// pattern is a content stream: a shading pattern is a dictionary with no cell
// to resolve names in (ISO 32000-1, 8.7.4.1).
func TestFixNestedResources_LeavesAShadingPatternAlone(t *testing.T) {
	t.Parallel()

	shading := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /ExtGState << /GS0 5 0 R >> /Pattern << /P0 4 0 R >> >> >>",
		"<< /Type /Pattern /PatternType 2 /Shading << /ShadingType 2 /ColorSpace /DeviceGray "+
			"/Coords [0 0 1 0] /Function << /FunctionType 2 /Domain [0 1] /N 1 >> >> /ExtGState 5 0 R >>",
		"<< /Type /ExtGState /CA 1 >>",
	)

	doc, alloc := newTestDoc(t, shading)
	if _, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	if _, rewritten := alloc.Pending()[4]; rewritten {
		t.Error("a shading pattern was rewritten")
	}
}

// TestFixNestedResources_GivesAFormThePatternItPaints pins the Pattern
// category. An scn or SCN operator whose last operand is a name selects the
// pattern of that name (ISO 32000-1, 8.6.8, Table 74), and veraPDF 1.30.2
// fails ISO 19005-2, 6.2.2 t2 at PDF/A-2b on a form XObject whose "/Pattern cs
// /P0 scn" finds /P0 only on the page. The fixer copied no pattern name.
func TestFixNestedResources_GivesAFormThePatternItPaints(t *testing.T) {
	t.Parallel()

	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /X0 4 0 R >> /Pattern << /P0 5 0 R >> >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << >> /Length 32 >>\n"+
			"stream\n/Pattern cs /P0 scn 0 0 5 5 re f\nendstream",
		"<< /Type /Pattern /PatternType 1 /PaintType 2 /TilingType 1 /BBox [0 0 10 10] "+
			"/XStep 10 /YStep 10 /Resources << >> /Length 12 >>\nstream\n0 0 5 5 re f\nendstream",
	)

	doc, alloc := newTestDoc(t, data)
	if _, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	got, ok := pendingStream(t, alloc, 4)
	if !ok {
		t.Fatal("the form XObject was not rewritten")
	}
	res, _ := got.Dict["Resources"].(ast.PdfDict)
	pattern, ok := res["Pattern"].(ast.PdfDict)
	if !ok || pattern["P0"] != (ast.PdfIndirectRef{ObjectNum: 5}) {
		t.Errorf("/Resources is %v, want /Pattern << /P0 5 0 R >>", res)
	}
}

// TestResourcesForStream_ReadsThePatternAnScnNames pins which operand of scn
// and SCN names a pattern: the last, after the components an uncoloured
// pattern takes, and only when it is a name.
func TestResourcesForStream_ReadsThePatternAnScnNames(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, nestedFormPDF())
	page := ast.PdfDict{"Pattern": ast.PdfDict{"P0": ast.PdfDict{}}}

	for _, tc := range []struct {
		name    string
		content string
		want    int
	}{
		{"a coloured pattern's scn", "/Pattern cs /P0 scn", 1},
		{"an uncoloured pattern's SCN", "/CSp CS 0.5 /P0 SCN", 1},
		{"a numeric scn names no pattern", "0.5 0.5 0.5 scn", 0},
		{"an scn with no operand", "scn", 0},
		{"a pattern the page does not define", "/Pattern cs /P9 scn", 0},
	} {
		_, added := resourcesForStream(context.Background(), alloc, doc, []byte(tc.content), nil, page)
		if added != tc.want {
			t.Errorf("%s: added %d, want %d", tc.name, added, tc.want)
		}
	}
}

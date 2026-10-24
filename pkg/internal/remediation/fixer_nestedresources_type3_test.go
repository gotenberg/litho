package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// t3Stream writes a stream object body holding data.
func t3Stream(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// t3ScopePDF builds a page showing glyph B of the resourceless Type 3 font 4,
// whose description paints /Bar, and, when formRes is set, a form 6 the page
// draws, with formRes as its /Resources, showing the same font. The page binds
// /Bar to form 5; form 9 is a second bar a form may bind it to.
func t3ScopePDF(formRes string) []byte {
	pageXObj := "/Bar 5 0 R"
	content := "BT /T3 40 Tf (B) Tj ET"
	if formRes != "" {
		pageXObj += " /Fm 6 0 R"
		content += " /Fm Do"
	}
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Contents 8 0 R "+
			"/Resources << /Font << /T3 4 0 R >> /XObject << "+pageXObj+" >> >> >>",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] "+
			"/CharProcs << /B 7 0 R >> /Encoding << /Differences [66 /B] >> /FirstChar 66 /LastChar 66 /Widths [1000] >>",
		t3Stream("/Type /XObject /Subtype /Form /BBox [0 0 1000 700] /Resources << >>", "0 0 1000 700 re f"),
		t3Stream("/Type /XObject /Subtype /Form /BBox [0 0 400 400] /Resources "+formRes, "BT /T3 40 Tf (B) Tj ET"),
		t3Stream("", "1000 0 d0 /Bar Do"),
		t3Stream("", content),
		t3Stream("/Type /XObject /Subtype /Form /BBox [0 0 1000 1500] /Resources << >>", "0 0 1000 1500 re f"),
	)
}

// t3FontBar returns the object the Type 3 font named /T3 in res binds /Bar
// to, and the font's object number, read through the allocator.
func t3FontBar(t *testing.T, alloc *writer.ObjAllocator, res ast.PdfDict) (bar, font int) {
	t.Helper()
	fonts, _ := res["Font"].(ast.PdfDict)
	ref, ok := fonts["T3"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("/T3 is %v", fonts["T3"])
	}
	fd, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("/T3 names object %d, which was not written", ref.ObjectNum)
	}
	fontRes, _ := fd["Resources"].(ast.PdfDict)
	xobj, _ := fontRes["XObject"].(ast.PdfDict)
	barRef, _ := xobj["Bar"].(ast.PdfIndirectRef)
	return barRef.ObjectNum, ref.ObjectNum
}

// TestFixNestedResources_Type3BoundApartGetsACopyPerScope pins that a
// resourceless Type 3 font a page and a form show, each binding the glyph's
// /Bar to its own form, gets a copy per scope carrying that scope's /Bar (ISO
// 32000-1, 9.6.5, Table 112: the glyph took the showing stream's resources),
// rather than the page's binding everywhere.
func TestFixNestedResources_Type3BoundApartGetsACopyPerScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, t3ScopePDF("<< /Font << /T3 4 0 R >> /XObject << /Bar 9 0 R >> >>"))
	if _, err := fixNestedResources(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}

	page := pendingDict(t, alloc, 3)
	pageRes, _ := page["Resources"].(ast.PdfDict)
	pageBar, pageFont := t3FontBar(t, alloc, pageRes)
	if pageBar != 5 {
		t.Errorf("the page's copy binds /Bar to %d, want 5", pageBar)
	}

	form, ok := alloc.Pending()[6].(ast.PdfStream)
	if !ok {
		t.Fatal("the form was not repointed")
	}
	formRes, _ := form.Dict["Resources"].(ast.PdfDict)
	formBar, formFont := t3FontBar(t, alloc, formRes)
	if formBar != 9 {
		t.Errorf("the form's copy binds /Bar to %d, want 9", formBar)
	}
	if pageFont == formFont || pageFont == 4 || formFont == 4 {
		t.Errorf("the page shows font %d and the form %d, want two copies of font 4", pageFont, formFont)
	}
	if _, written := alloc.Pending()[4]; written {
		t.Error("the font both scopes shared was rewritten in place")
	}
}

// TestFixNestedResources_Type3BoundAlikeStaysOneObject pins that a
// resourceless Type 3 font every scope binds alike gets the names in place,
// with no copy: one shown from the page alone, and one a form shows with the
// page's /Bar.
func TestFixNestedResources_Type3BoundAlikeStaysOneObject(t *testing.T) {
	t.Parallel()

	for name, formRes := range map[string]string{
		"page only":     "",
		"form alike":    "<< /Font << /T3 4 0 R >> /XObject << /Bar 5 0 R >> >>",
		"form inherits": "<< /Font << /T3 4 0 R >> >>",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, alloc := newTestDoc(t, t3ScopePDF(formRes))
			next := alloc.NextObjNum()
			if _, err := fixNestedResources(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
				t.Fatalf("fixNestedResources: %v", err)
			}
			font := pendingDict(t, alloc, 4)
			res, _ := font["Resources"].(ast.PdfDict)
			xobj, _ := res["XObject"].(ast.PdfDict)
			if got := xobj["Bar"]; got != (ast.PdfIndirectRef{ObjectNum: 5}) {
				t.Errorf("/Bar is %v, want 5 0 R", got)
			}
			if alloc.NextObjNum() != next {
				t.Errorf("allocated %d new objects, want none", alloc.NextObjNum()-next)
			}
		})
	}
}

// TestFixNestedResources_Type3BoundPerPageIsLeftAlone pins that a form two
// pages draw, showing a resourceless Type 3 font whose /Bar it takes from
// each page apart, leaves the font as it was: one form cannot name a copy per
// page, and giving it either page's /Bar would change what the other draws.
func TestFixNestedResources_Type3BoundPerPageIsLeftAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 10 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Contents 8 0 R "+
			"/Resources << /XObject << /Bar 5 0 R /Fm 6 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] "+
			"/CharProcs << /B 7 0 R >> /Encoding << /Differences [66 /B] >> /FirstChar 66 /LastChar 66 /Widths [1000] >>",
		t3Stream("/Type /XObject /Subtype /Form /BBox [0 0 1000 700] /Resources << >>", "0 0 1000 700 re f"),
		t3Stream("/Type /XObject /Subtype /Form /BBox [0 0 400 400] /Resources << /Font << /T3 4 0 R >> >>", "BT /T3 40 Tf (B) Tj ET"),
		t3Stream("", "1000 0 d0 /Bar Do"),
		t3Stream("", "/Fm Do"),
		t3Stream("/Type /XObject /Subtype /Form /BBox [0 0 1000 1500] /Resources << >>", "0 0 1000 1500 re f"),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Contents 8 0 R "+
			"/Resources << /XObject << /Bar 9 0 R /Fm 6 0 R >> >> >>",
	))
	if _, err := fixNestedResources(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	if _, written := alloc.Pending()[4]; written {
		t.Error("the font was given one page's /Bar")
	}
	if form, ok := alloc.Pending()[6].(ast.PdfStream); ok {
		res, _ := form.Dict["Resources"].(ast.PdfDict)
		fonts, _ := res["Font"].(ast.PdfDict)
		if got := fonts["T3"]; got != (ast.PdfIndirectRef{ObjectNum: 4}) {
			t.Errorf("the form's /T3 was repointed to %v", got)
		}
	}
}

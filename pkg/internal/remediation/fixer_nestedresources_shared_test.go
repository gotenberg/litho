package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// sharedCopyColourSpace returns the colour space family the form a rewritten
// page draws under name binds /CS0 to, read from the allocator: the page's
// inline /Resources, its /XObject entry, and that form's /Resources.
func sharedCopyColourSpace(t *testing.T, alloc *writer.ObjAllocator, pageObj int, name ast.PdfName) string {
	t.Helper()
	page, ok := alloc.Pending()[pageObj].(ast.PdfDict)
	if !ok {
		t.Fatalf("page %d was not rewritten", pageObj)
	}
	res, _ := page["Resources"].(ast.PdfDict)
	xobj, _ := res["XObject"].(ast.PdfDict)
	ref, ok := xobj[name].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("page %d /XObject /%s is %v", pageObj, name, xobj[name])
	}
	form, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("page %d draws object %d, which is no pending stream", pageObj, ref.ObjectNum)
	}
	formRes, _ := form.Dict["Resources"].(ast.PdfDict)
	cs, _ := formRes["ColorSpace"].(ast.PdfDict)
	arr, _ := cs["CS0"].(ast.PdfArray)
	if len(arr) == 0 {
		return ""
	}
	family, _ := arr[0].(ast.PdfName)
	return string(family)
}

// TestFixNestedResources_SharedFormBoundAlike pins that a form two pages bind
// alike gets the names once, in place, with no copy.
func TestFixNestedResources_SharedFormBoundAlike(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 7 0 R >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length 16 >>\nstream\nq /CS0 cs 0.5 sc\nendstream",
		"<< /WhitePoint [0.9505 1 1.089] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /X1 4 0 R >> /ColorSpace << /CS0 8 0 R >> >> >>",
		"<< /XObject << /X0 4 0 R >> /ColorSpace << /CS0 8 0 R >> >>",
		"[/CalGray 5 0 R]",
	))
	next := alloc.NextObjNum()
	fixes, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("the repair reported nothing")
	}
	form, ok := alloc.Pending()[4].(ast.PdfStream)
	if !ok {
		t.Fatal("the shared form was not given its resources")
	}
	res, _ := form.Dict["Resources"].(ast.PdfDict)
	cs, _ := res["ColorSpace"].(ast.PdfDict)
	if got := cs["CS0"]; got != (ast.PdfIndirectRef{ObjectNum: 8}) {
		t.Errorf("/CS0 is %v, want 8 0 R", got)
	}
	if alloc.NextObjNum() != next {
		t.Errorf("allocated %d new objects, want none", alloc.NextObjNum()-next)
	}
}

// TestFixNestedResources_SharedSubdictionaryIsCopied pins that a /XObject
// dictionary two differently bound pages share is copied for each, so
// repointing one page never repoints the other.
func TestFixNestedResources_SharedSubdictionaryIsCopied(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject 7 0 R /ColorSpace << /CS0 [/CalGray 5 0 R] >> >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length 16 >>\nstream\nq /CS0 cs 0.5 sc\nendstream",
		"<< /WhitePoint [0.9505 1 1.089] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject 7 0 R /ColorSpace << /CS0 [/CalRGB 5 0 R] >> >> >>",
		"<< /X0 4 0 R >>",
	))
	if _, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	if _, rewritten := alloc.Pending()[7]; rewritten {
		t.Error("the shared /XObject dictionary was written in place")
	}
	for page, want := range map[int]string{3: "CalGray", 6: "CalRGB"} {
		pd, ok := alloc.Pending()[page].(ast.PdfDict)
		if !ok {
			t.Fatalf("page %d was not rewritten", page)
		}
		res, _ := pd["Resources"].(ast.PdfDict)
		subRef, ok := res["XObject"].(ast.PdfIndirectRef)
		if !ok || subRef.ObjectNum == 7 {
			t.Fatalf("page %d /XObject is %v, want a fresh indirect copy", page, res["XObject"])
		}
		sub, _ := alloc.Pending()[subRef.ObjectNum].(ast.PdfDict)
		formRef, _ := sub["X0"].(ast.PdfIndirectRef)
		form, _ := alloc.Pending()[formRef.ObjectNum].(ast.PdfStream)
		formRes, _ := form.Dict["Resources"].(ast.PdfDict)
		cs, _ := formRes["ColorSpace"].(ast.PdfDict)
		arr, _ := cs["CS0"].(ast.PdfArray)
		if len(arr) == 0 || arr[0] != ast.PdfName(want) {
			t.Errorf("page %d draws a copy binding /CS0 to %v, want %s", page, cs["CS0"], want)
		}
	}
}

// TestFixNestedResources_SharedFormInTheStructureTreeIsNotCopied pins that a
// form carrying /StructParents is left alone when its pages disagree: two
// copies would claim one parent tree entry (ISO 32000-1, 14.7.4.4).
func TestFixNestedResources_SharedFormInTheStructureTreeIsNotCopied(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /X0 4 0 R >> /ColorSpace << /CS0 [/CalGray 5 0 R] >> >> >>",
		"<< /Type /XObject /Subtype /Form /StructParents 0 /BBox [0 0 10 10] /Length 16 >>\nstream\nq /CS0 cs 0.5 sc\nendstream",
		"<< /WhitePoint [0.9505 1 1.089] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /XObject << /X0 4 0 R >> /ColorSpace << /CS0 [/CalRGB 5 0 R] >> >> >>",
	))
	next := alloc.NextObjNum()
	if _, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	if len(alloc.Pending()) != 0 || alloc.NextObjNum() != next {
		t.Errorf("wrote %d objects, want none", len(alloc.Pending()))
	}
}

// TestFixNestedResources_SharedPatternBoundDifferently pins the pattern arm:
// a tiling pattern two pages bind differently is copied per page.
func TestFixNestedResources_SharedPatternBoundDifferently(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Pattern << /P0 4 0 R >> /ColorSpace << /CS0 [/CalGray 5 0 R] >> >> >>",
		"<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 "+
			"/Length 16 >>\nstream\nq /CS0 cs 0.5 sc\nendstream",
		"<< /WhitePoint [0.9505 1 1.089] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Pattern << /P0 4 0 R >> /ColorSpace << /CS0 [/CalRGB 5 0 R] >> >> >>",
	))
	if _, err := fixNestedResources(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	if _, rewritten := alloc.Pending()[4]; rewritten {
		t.Error("the shared pattern was rewritten in place")
	}
	for _, page := range []int{3, 6} {
		pd, ok := alloc.Pending()[page].(ast.PdfDict)
		if !ok {
			t.Fatalf("page %d was not rewritten", page)
		}
		res, _ := pd["Resources"].(ast.PdfDict)
		pat, _ := res["Pattern"].(ast.PdfDict)
		if ref, _ := pat["P0"].(ast.PdfIndirectRef); ref.ObjectNum == 4 || ref.ObjectNum == 0 {
			t.Errorf("page %d still draws pattern %v", page, pat["P0"])
		}
	}
}

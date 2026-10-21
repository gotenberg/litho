package mutations

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// bakeDoc opens data and returns its document, first page and an allocator
// numbering after its objects.
func bakeDoc(t *testing.T, data []byte) (*document.Document, *document.Page, *writer.ObjAllocator) {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	return doc, pages[0], writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
}

// bakeObjs is a page holding a square annotation at object 4, whose
// appearance (object 5) paints under a constant alpha of 0.5, and a direct
// annotation in /Annots. extra goes into the catalog.
func bakeObjs(extra string) []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R " + extra + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 6 0 R " +
			"/Annots [4 0 R << /Type /Annot /Subtype /Square /Rect [0 0 10 10] /CA 0.5 /AP << /N 5 0 R >> >>] >>",
		"<< /Type /Annot /Subtype /Square /Rect [20 20 80 80] /F 4 /CA 0.5 /AS /On /AP << /N 5 0 R /D 5 0 R >> /StructParent 3 >>",
		"<< /Type /XObject /Subtype /Form /BBox [20 20 80 80] /Resources << /ExtGState << /G << /ca 0.5 >> >> >> /Length 31 >>\nstream\n/G gs 1 0 0 rg 20 20 60 60 re f\nendstream",
		"<< /Length 24 >>\nstream\n0 0 1 rg 0 0 50 200 re f\nendstream",
	}
}

func pageContent(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) string {
	t.Helper()
	data, err := LatestPageContents(context.Background(), alloc, doc, page)
	if err != nil {
		t.Fatalf("LatestPageContents: %v", err)
	}
	return string(data)
}

// TestBakeAnnotationsDrawsAndEmpties pins that a baked annotation's
// appearance is drawn after the page's content, placed by the matrix given,
// and that the annotation stays with an appearance that paints nothing.
func TestBakeAnnotationsDrawsAndEmpties(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page, alloc := bakeDoc(t, fieldObjsPDF(bakeObjs("")))
	ap, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5})
	if err != nil {
		t.Fatal(err)
	}
	err = BakeAnnotations(ctx, alloc, doc, page, []AnnotationBake{
		{Index: 0, Appearance: ap, AppearanceRef: ast.PdfIndirectRef{ObjectNum: 5}, Matrix: [6]float64{1, 0, 0, 1, 0, 0}},
		{Index: 1},
	})
	if err != nil {
		t.Fatalf("BakeAnnotations: %v", err)
	}

	got := pageContent(t, alloc, doc, page)
	if !strings.Contains(got, "q 1 0 0 1 0 0 cm /FlatAnnot5 Do Q") {
		t.Errorf("content %q does not draw the appearance", got)
	}
	if strings.Contains(got, "Artifact") {
		t.Errorf("content %q marks an artifact in an untagged document", got)
	}
	if strings.Index(got, "0 0 50 200 re f") > strings.Index(got, "FlatAnnot5") {
		t.Errorf("content %q draws the appearance before the page", got)
	}

	annot, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("annotation 4 not rewritten: %T", alloc.Pending()[4])
	}
	for _, key := range []ast.PdfName{"CA", "AS"} {
		if _, has := annot[key]; has {
			t.Errorf("annotation keeps /%s", key)
		}
	}
	if _, has := annot["StructParent"]; !has {
		t.Error("annotation lost its /StructParent")
	}
	apDict, _ := annot["AP"].(ast.PdfDict)
	if len(apDict) != 1 {
		t.Fatalf("annotation /AP = %v, want a single /N", annot["AP"])
	}
	empty, ok := alloc.Pending()[apDict["N"].(ast.PdfIndirectRef).ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("/N is not a new stream")
	}
	if data, _ := empty.Bytes(ctx); len(data) != 0 {
		t.Errorf("empty appearance paints %q", data)
	}
	res, _ := empty.Dict["Resources"].(ast.PdfDict)
	if _, has := res["ExtGState"].(ast.PdfDict); !has {
		t.Error("empty appearance has no /ExtGState, so poppler draws a highlight anew")
	}

	pg, _ := alloc.Pending()[3].(ast.PdfDict)
	annots, _ := pg["Annots"].(ast.PdfArray)
	if len(annots) != 2 {
		t.Fatalf("page /Annots = %v", pg["Annots"])
	}
	direct, _ := annots[1].(ast.PdfDict)
	if _, has := direct["CA"]; has || direct["AP"] == nil {
		t.Errorf("direct annotation not emptied: %v", direct)
	}
}

// TestBakeAnnotationsMarksArtifacts pins that a tagged document's drawing is
// an artifact (ISO 14289-1, 7.1): the structure tree reaches the annotation,
// which stays, by its object reference.
func TestBakeAnnotationsMarksArtifacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page, alloc := bakeDoc(t, fieldObjsPDF(bakeObjs("/StructTreeRoot << /Type /StructTreeRoot >>")))
	ap, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := BakeAnnotations(ctx, alloc, doc, page, []AnnotationBake{
		{Index: 0, Appearance: ap, AppearanceRef: ast.PdfIndirectRef{ObjectNum: 5}, Matrix: [6]float64{1, 0, 0, 1, 0, 0}},
	}); err != nil {
		t.Fatalf("BakeAnnotations: %v", err)
	}
	if got := pageContent(t, alloc, doc, page); !strings.Contains(got, "/Artifact BMC\nq 1 0 0 1 0 0 cm /FlatAnnot5 Do Q\nEMC") {
		t.Errorf("content %q does not mark the drawing as an artifact", got)
	}
}

// TestBakeAnnotationsHoistsBuiltStreams pins that an appearance built in
// memory, which holds a form directly in its resources, is written with that
// form as its own object: a stream is always indirect (ISO 32000-1, 7.3.8.1).
func TestBakeAnnotationsHoistsBuiltStreams(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page, alloc := bakeDoc(t, fieldObjsPDF(bakeObjs("")))
	inner := ast.NewStream(ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"),
		"BBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(1)}}, []byte("0 0 1 1 re f"))
	built := ast.NewStream(ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"),
		"BBox":      ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(1)},
		"Resources": ast.PdfDict{"XObject": ast.PdfDict{"Fm0": inner}}}, []byte("/Fm0 Do"))
	if err := BakeAnnotations(ctx, alloc, doc, page, []AnnotationBake{{Index: 0, Appearance: built, Matrix: [6]float64{1, 0, 0, 1, 0, 0}}}); err != nil {
		t.Fatalf("BakeAnnotations: %v", err)
	}
	for num, obj := range alloc.Pending() {
		s, ok := obj.(ast.PdfStream)
		if !ok {
			continue
		}
		res, _ := s.Dict["Resources"].(ast.PdfDict)
		xobjects, _ := res["XObject"].(ast.PdfDict)
		if fm, has := xobjects["Fm0"]; has {
			if _, isRef := fm.(ast.PdfIndirectRef); !isRef {
				t.Errorf("object %d holds /Fm0 directly as %T", num, fm)
			}
			return
		}
	}
	t.Error("no baked form holds /Fm0")
}

// TestBakeAnnotationsRefusesAMissingAnnotation pins the error for an index
// /Annots does not hold.
func TestBakeAnnotationsRefusesAMissingAnnotation(t *testing.T) {
	t.Parallel()
	doc, page, alloc := bakeDoc(t, fieldObjsPDF(bakeObjs("")))
	err := BakeAnnotations(context.Background(), alloc, doc, page, []AnnotationBake{{Index: 7}})
	if !errors.Is(err, ErrInvalidAnnotation) {
		t.Fatalf("err = %v, want ErrInvalidAnnotation", err)
	}
}

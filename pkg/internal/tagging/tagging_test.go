package tagging

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// buildTaggedTestPDF assembles a valid single-page PDF whose page /Contents is
// the given uncompressed content stream, computing correct xref offsets. The
// page is object 3; the content stream is object 5.
func buildTaggedTestPDF(contentStream string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(contentStream), contentStream),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f \r\n")
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefStart)
	return buf.Bytes()
}

func TestEmit_StructuredTree(t *testing.T) {
	ctx := context.Background()

	block0 := "BT /F1 24 Tf 72 700 Td (Heading One) Tj ET"
	block1 := "BT /F1 12 Tf 72 680 Td (First paragraph.) Tj ET"
	block2 := "BT /F1 12 Tf 72 660 Td (Second paragraph.) Tj ET"
	contentStream := block0 + "\n" + block1 + "\n" + block2 + "\n"

	s0, e0 := 0, len(block0)
	s1, e1 := e0+1, e0+1+len(block1)
	s2, e2 := e1+1, e1+1+len(block2)

	data := buildTaggedTestPDF(contentStream)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(5) // objects 1..5 exist

	root := &Element{Role: RoleDocument, Kids: []*Element{
		{Role: RoleH1, Span: &ContentSpan{PageIndex: 0, Start: s0, End: e0}},
		{Role: RoleP, Span: &ContentSpan{PageIndex: 0, Start: s1, End: e1}},
		{Role: RoleP, Span: &ContentSpan{PageIndex: 0, Start: s2, End: e2}},
	}}

	descs, err := Emit(ctx, alloc, doc, root)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("descriptions: got %d, want 1: %v", len(descs), descs)
	}

	pending := alloc.Pending()

	// Catalog (obj 1): /StructTreeRoot + /MarkInfo /Marked true.
	catalog := mustDict(t, pending[1], "catalog")
	if mi := mustDict(t, catalog["MarkInfo"], "MarkInfo"); mi["Marked"] != ast.PdfBool(true) {
		t.Errorf("MarkInfo /Marked: got %v, want true", mi["Marked"])
	}
	stRef := mustRef(t, catalog["StructTreeRoot"], "catalog /StructTreeRoot")

	// StructTreeRoot → Document.
	st := mustDict(t, pending[stRef.ObjectNum], "StructTreeRoot")
	if st["Type"] != ast.PdfName("StructTreeRoot") {
		t.Errorf("StructTreeRoot /Type: got %v", st["Type"])
	}
	docRef := mustRef(t, st["K"], "StructTreeRoot /K")
	docElem := mustDict(t, pending[docRef.ObjectNum], "Document element")
	if docElem["S"] != ast.PdfName("Document") {
		t.Errorf("Document /S: got %v", docElem["S"])
	}
	if mustRef(t, docElem["P"], "Document /P") != stRef {
		t.Errorf("Document /P: got %v, want StructTreeRoot %v", docElem["P"], stRef)
	}

	// Document's children: H1, P, P, each a leaf with a distinct MCID.
	kids, ok := docElem["K"].(ast.PdfArray)
	if !ok || len(kids) != 3 {
		t.Fatalf("Document /K: got %v, want array of 3", docElem["K"])
	}
	wantRoles := []ast.PdfName{"H1", "P", "P"}
	pageRef := ast.PdfIndirectRef{ObjectNum: 3} // the page is object 3 in buildTaggedTestPDF
	for i, kref := range kids {
		ref := mustRef(t, kref, fmt.Sprintf("Document /K[%d]", i))
		leaf := mustDict(t, pending[ref.ObjectNum], fmt.Sprintf("leaf %d", i))
		if leaf["S"] != wantRoles[i] {
			t.Errorf("leaf %d /S: got %v, want %v", i, leaf["S"], wantRoles[i])
		}
		if mustRef(t, leaf["P"], "leaf /P") != docRef {
			t.Errorf("leaf %d /P: got %v, want Document %v", i, leaf["P"], docRef)
		}
		if leaf["K"] != ast.PdfInt(i) {
			t.Errorf("leaf %d /K (MCID): got %v, want %d", i, leaf["K"], i)
		}
		if mustRef(t, leaf["Pg"], "leaf /Pg") != pageRef {
			t.Errorf("leaf %d /Pg: got %v, want page %v", i, leaf["Pg"], pageRef)
		}
	}

	// ParentTree: Nums = [0, [h1, p, p]].
	pt := mustDict(t, st["ParentTree"], "ParentTree")
	nums, ok := pt["Nums"].(ast.PdfArray)
	if !ok || len(nums) != 2 || nums[0] != ast.PdfInt(0) {
		t.Fatalf("ParentTree /Nums: got %v", pt["Nums"])
	}
	if arr, ok := nums[1].(ast.PdfArray); !ok || len(arr) != 3 {
		t.Errorf("ParentTree /Nums[1]: got %v, want array of 3 element refs", nums[1])
	}

	// Page (obj 3): rewritten /Contents + /StructParents 0.
	page := mustDict(t, pending[3], "page")
	if page["StructParents"] != ast.PdfInt(0) {
		t.Errorf("page /StructParents: got %v, want 0", page["StructParents"])
	}
	csRef := mustRef(t, page["Contents"], "page /Contents")
	cs, ok := pending[csRef.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("content stream: got %T, want PdfStream", pending[csRef.ObjectNum])
	}
	marked, err := cs.Bytes(ctx)
	if err != nil {
		t.Fatalf("content bytes: %v", err)
	}
	got := string(marked)
	if n := strings.Count(got, " BDC\n"); n != 3 {
		t.Errorf("BDC count: got %d, want 3\n%s", n, got)
	}
	if n := strings.Count(got, "\nEMC\n"); n != 3 {
		t.Errorf("EMC count: got %d, want 3", n)
	}
	for _, want := range []string{"/H1 <</MCID 0>>", "/P <</MCID 1>>", "/P <</MCID 2>>",
		"(Heading One)", "(First paragraph.)", "(Second paragraph.)"} {
		if !strings.Contains(got, want) {
			t.Errorf("marked content missing %q\n%s", want, got)
		}
	}

	// The marked stream must still parse as valid content.
	if _, err := content.ParseStreamData(ctx, marked); err != nil {
		t.Errorf("re-parse marked content: %v", err)
	}
}

func TestEmit_RejectsNonDocumentRoot(t *testing.T) {
	ctx := context.Background()
	data := buildTaggedTestPDF("BT /F1 12 Tf 72 700 Td (x) Tj ET\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(5)
	if _, err := Emit(ctx, alloc, doc, &Element{Role: RoleP}); err == nil {
		t.Error("expected error for non-Document root")
	}
}

func mustDict(t *testing.T, v any, what string) ast.PdfDict {
	t.Helper()
	d, ok := v.(ast.PdfDict)
	if !ok {
		t.Fatalf("%s: got %T, want ast.PdfDict", what, v)
	}
	return d
}

func mustRef(t *testing.T, v any, what string) ast.PdfIndirectRef {
	t.Helper()
	r, ok := v.(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("%s: got %T, want ast.PdfIndirectRef", what, v)
	}
	return r
}

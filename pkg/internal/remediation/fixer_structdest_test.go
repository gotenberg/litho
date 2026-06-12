package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// structDestPDF has a structure tree (Document with one page-mapped
// element), an outline whose item targets the page explicitly, and an
// explicit /OpenAction destination.
func structDestPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Outlines 6 0 R /OpenAction [3 0 R /XYZ null null 0] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [5 0 R]] >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
		"<< /Type /Outlines /First 7 0 R /Count 1 >>",
		"<< /Title (A) /Parent 6 0 R /Dest [3 0 R /Fit] >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestFixStructDest pins the structure-destination rewrite: outline
// /Dest and catalog /OpenAction arrays become dictionary destinations
// whose /SD targets the page's structure element from the ParentTree.
func TestFixStructDest(t *testing.T) {
	ctx := context.Background()
	data := structDestPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(7)

	fixes, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixStructDest: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v, want one description", fixes)
	}

	outline, ok := alloc.Pending()[7].(ast.PdfDict)
	if !ok {
		t.Fatal("outline item not rewritten")
	}
	destDict, ok := outline["Dest"].(ast.PdfDict)
	if !ok {
		t.Fatalf("outline /Dest = %T, want dictionary destination", outline["Dest"])
	}
	sd, ok := destDict["SD"].(ast.PdfArray)
	if !ok || len(sd) != 2 {
		t.Fatalf("/SD = %v, want two-element structure destination", destDict["SD"])
	}
	if ref, ok := sd[0].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 5 {
		t.Errorf("/SD[0] = %v, want the page's structure element 5 0 R", sd[0])
	}
	if _, ok := destDict["D"].(ast.PdfArray); !ok {
		t.Error("original array must survive under /D")
	}

	catalog, ok := alloc.Pending()[1].(ast.PdfDict)
	if !ok {
		t.Fatal("catalog not rewritten for /OpenAction")
	}
	if _, ok := catalog["OpenAction"].(ast.PdfDict); !ok {
		t.Errorf("OpenAction = %T, want dictionary destination", catalog["OpenAction"])
	}
}

// TestFixStructDest_NoStructTree pins the guard: without a structure
// tree nothing is rewritten.
func TestFixStructDest_NoStructTree(t *testing.T) {
	ctx := context.Background()
	data := cyclePDFRemediation(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(3)
	fixes, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil || fixes != nil {
		t.Fatalf("want no-op without StructTreeRoot, got fixes=%v err=%v", fixes, err)
	}
}

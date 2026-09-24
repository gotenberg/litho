package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// misrootedCatalogPDF assembles a document whose trailer /Root points at the
// /Info dictionary in object 1 while the real catalog sits in object 2, the
// shape a damaged incremental update leaves.
//
// # Corpus evidence
//
//   - pdfjs issue9418.pdf: the fixture reproduces this file's shape after its
//     damaged incremental update.
func misrootedCatalogPDF() []byte {
	objs := []string{
		"<< /Producer (Aspose) /CreationDate (D:20180130101459Z) >>",
		"<< /Type /Catalog /Pages 3 0 R >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 612 792] >>",
	}

	out := "%PDF-1.4\n"
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = len(out)
		out += fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	start := len(out)
	out += fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		out += fmt.Sprintf("%010d 00000 n \r\n", offsets[i])
	}
	out += fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return []byte(out)
}

// TestFixMarkInfo_RepairsTheRecoveredCatalog pins that a fixer writes its repair
// into the object the reader and the validator treat as the catalog, not into
// whatever object a damaged trailer /Root happens to name.
//
// Per ISO 32000-1 §7.5.5 Table 15 /Root shall be the catalog, so a file naming
// something else is malformed and [document.Document.CatalogRef] recovers from
// it. Taking /Root at face value made every catalog fixer patch the wrong
// object: the catalog repairs were merged into the document information
// dictionary while the catalog kept none of them. Remediation reported those
// repairs as applied and then listed the same violations as residual, so it
// never converged: a second pass over its own output reported the identical
// fixes again.
//
// # Corpus evidence
//
//   - pdfjs issue9418.pdf: /MarkInfo, /Lang, /Metadata, /StructTreeRoot and
//     /ViewerPreferences were merged into the document information
//     dictionary while the catalog kept none of them.
func TestFixMarkInfo_RepairsTheRecoveredCatalog(t *testing.T) {
	doc, alloc := newTestDoc(t, misrootedCatalogPDF())

	rootRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok || rootRef.ObjectNum != 1 {
		t.Fatalf("fixture precondition: trailer /Root must name object 1, got %v", doc.Index.Trailer["Root"])
	}

	if _, err := fixMarkInfo(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixMarkInfo: %v", err)
	}

	catalog := pendingDict(t, alloc, 2)
	mi, ok := catalog["MarkInfo"].(ast.PdfDict)
	if !ok || mi["Marked"] != ast.PdfBool(true) {
		t.Errorf("catalog (object 2) /MarkInfo: got %v, want << /Marked true >>", catalog["MarkInfo"])
	}

	if info, written := alloc.Pending()[1]; written {
		t.Errorf("the /Info dictionary named by the broken /Root was rewritten: %v", info)
	}
}

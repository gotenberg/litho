package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// misrootedDoc builds a one-page document whose catalog is object 2 while the
// trailer /Root points at the /Info dictionary in object 1, the shape of pdfjs
// issue9418.pdf after its damaged incremental update.
func misrootedDoc(t *testing.T) (*document.Document, *writer.ObjAllocator) {
	t.Helper()

	objs := []string{
		"<< /Producer (Aspose) /CreationDate (D:20180130101459Z) >>",
		"<< /Type /Catalog /Pages 3 0 R >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 612 792] >>",
	}
	out := "%PDF-1.7\n"
	offs := make([]int, len(objs)+1)
	for i, body := range objs {
		offs[i+1] = len(out)
		out += fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	start := len(out)
	out += fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		out += fmt.Sprintf("%010d 00000 n \r\n", offs[i])
	}
	out += fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R /Info 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, start)

	data := []byte(out)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc, writer.NewObjAllocator(len(objs))
}

// TestCatalogRefRecoversFromAMisrootedTrailer pins the shared lookup: the
// catalog is the object the reader resolves, not whatever the trailer names.
//
// Per ISO 32000-1 §7.5.5 Table 15 /Root shall be the catalog, so a file naming
// something else is malformed and [document.Document.CatalogRef] recovers from
// it. Every catalog-editing mutation used the raw /Root instead.
func TestCatalogRefRecoversFromAMisrootedTrailer(t *testing.T) {
	t.Parallel()

	doc, _ := misrootedDoc(t)
	if raw, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef); !ok || raw.ObjectNum != 1 {
		t.Fatalf("fixture precondition: trailer /Root must name object 1, got %v", doc.Index.Trailer["Root"])
	}

	ref, ok := CatalogRef(context.Background(), doc)
	if !ok {
		t.Fatal("CatalogRef found no catalog")
	}
	if ref.ObjectNum != 2 {
		t.Errorf("CatalogRef = object %d, want the real catalog at object 2", ref.ObjectNum)
	}
}

// TestAddAttachmentTargetsTheRecoveredCatalog pins that an attachment lands in
// the catalog rather than in the object a damaged /Root names.
//
// `litho attach` on pdfjs issue9418.pdf exited 0 and reported success while
// writing /Names into the /Info dictionary. The real catalog kept none of it, so
// `litho attachments` on that same output reported no attachments: the file was
// silently lost and the information dictionary corrupted in one round-trip.
func TestAddAttachmentTargetsTheRecoveredCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := misrootedDoc(t)

	if err := AddAttachment(ctx, alloc, doc, "att.txt", []byte("litho"), "text/plain"); err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	catalog, ok := alloc.Pending()[2].(ast.PdfDict)
	if !ok {
		t.Fatalf("catalog (object 2) not written")
	}
	if _, hasNames := catalog["Names"]; !hasNames {
		t.Errorf("catalog missing /Names after attaching: %v", catalog)
	}

	if info, written := alloc.Pending()[1]; written {
		if d, isDict := info.(ast.PdfDict); isDict {
			if _, leaked := d["Names"]; leaked {
				t.Errorf("/Names leaked into the /Info dictionary named by the broken /Root: %v", d)
			}
		}
	}
}

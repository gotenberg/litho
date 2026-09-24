package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// misdirectedCatalogPDF builds a one-page document whose cross-reference stream
// marks the catalog's entry free. Resolving the catalog reference therefore
// yields an empty dictionary with no error, while the real catalog body sits in
// the file for the reader's recovery scan to find.
//
// # Corpus evidence
//
//   - pdfjs issue18986.pdf: the fixture is this file in miniature. A UTF-8
//     round-trip replaced every byte above 0x7F in its cross-reference stream
//     with EF BF BD, which desynchronises the fixed-width rows and decodes the
//     catalog's entry as free. The file uses a cross-reference STREAM.
func misdirectedCatalogPDF() []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	offsets := make([]int, 6)
	write := func(num int, body string) {
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	write(1, "<< /Type /Pages /Count 1 /Kids [3 0 R] >>")
	write(2, "<<>>")
	write(3, "<< /Type /Page /Parent 1 0 R /MediaBox [0 0 612 792] >>")
	write(4, "<< /Type /Catalog /Pages 1 0 R /PageMode /UseNone >>")

	// A cross-reference STREAM: the offset repair that rescues a misdirected
	// classic table does not run here.
	xrefOffset := buf.Len()
	var rows bytes.Buffer
	row := func(t, offset int) {
		rows.Write([]byte{byte(t)})
		rows.Write([]byte{byte(offset >> 24), byte(offset >> 16), byte(offset >> 8), byte(offset)})
		rows.Write([]byte{0, 0})
	}
	row(0, 0)
	row(1, offsets[1])
	row(1, offsets[2])
	row(1, offsets[3])
	// The damage: the catalog's entry is marked free, so resolving the
	// reference yields nothing while the object itself sits in the file.
	row(0, 0)
	row(1, xrefOffset)
	fmt.Fprintf(&buf, "5 0 obj\n<< /Type /XRef /Size 6 /W [1 4 2] /Index [0 6] /Root 4 0 R /Length %d >>\nstream\n",
		rows.Len())
	buf.Write(rows.Bytes())
	buf.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefOffset)
	return buf.Bytes()
}

// TestWriteOutlineKeepsTheCatalogItEdits pins that adding an outline preserves
// the catalog's other entries when the cross-reference table loses it.
//
// WriteOutline copied the dictionary it resolved at the catalog reference and
// added /Outlines. Resolving a wrongly-freed reference yields an empty dictionary
// and no error, so the copy contributed nothing and the catalog was replaced by
// "<< /Outlines 9 0 R >>". Per ISO 32000-1, 7.7.2 a catalog shall carry /Type
// and /Pages, so the output was no longer a document: pikepdf rejected it with
// "root of pages tree has no /Kids array" and litho's own `bookmarks` read the
// result as empty, having reported success.
func TestWriteOutlineKeepsTheCatalogItEdits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := misdirectedCatalogPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		t.Fatal("no catalog reference")
	}
	if dict, derr := document.ResolveDict(ctx, doc, catalogRef); derr != nil || len(dict) != 0 {
		t.Fatalf("fixture is wrong: resolving the catalog reference must yield an empty dictionary, got %v (err %v)", dict, derr)
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	pageRefs := map[int]ast.PdfIndirectRef{1: {ObjectNum: 3}}
	if err := WriteOutline(ctx, alloc, doc, pageRefs, []models.Bookmark{{Title: "Probe", PageNumber: 1}}); err != nil {
		t.Fatalf("WriteOutline: %v", err)
	}

	written, ok := alloc.Pending()[catalogRef.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("catalog object %d not written", catalogRef.ObjectNum)
	}
	if _, ok := written["Outlines"]; !ok {
		t.Error("catalog gained no /Outlines entry")
	}
	if written["Type"] != ast.PdfName("Catalog") {
		t.Errorf("catalog lost /Type: %v", written)
	}
	if _, ok := written["Pages"]; !ok {
		t.Errorf("catalog lost /Pages, leaving a document with no page tree: %v", written)
	}
}

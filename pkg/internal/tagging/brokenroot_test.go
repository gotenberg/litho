package tagging

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// assemblePDFRootAt writes the given object bodies into a valid PDF whose
// trailer /Root names rootObjNum, letting a test point /Root at something that
// is not the catalog. Object 1 is the /Info dictionary and object 2 the
// catalog, mirroring a real damaged file.
//
// # Corpus evidence
//
//   - pdfjs issue9418.pdf: the file this layout mirrors.
func assemblePDFRootAt(objs []string, rootObjNum int) []byte {
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, rootObjNum, xrefStart)
	return buf.Bytes()
}

// brokenRootDoc builds a one-page document whose catalog is object 2 while the
// trailer /Root points at the /Info dictionary in object 1. The catalog carries
// the given extra entries verbatim.
func brokenRootDoc(t *testing.T, catalogExtras string) *document.Document {
	t.Helper()
	stream := "BT /F1 12 Tf 72 700 Td (Body text.) Tj ET\n"
	data := assemblePDFRootAt([]string{
		"<< /Producer (Aspose) /CreationDate (D:20180130101459Z) >>",
		"<< /Type /Catalog /Pages 3 0 R " + catalogExtras + " >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}, 1)

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestAlreadyTaggedFollowsTheRecoveredCatalog pins that a document whose real
// catalog is tagged is recognized as tagged even when the trailer /Root points
// somewhere else.
//
// Per ISO 32000-1, 7.5.5, Table 15 /Root shall be the catalog, so such a file is
// malformed and the reader recovers by scanning. Reading the raw /Root instead
// made alreadyTagged inspect a non-catalog, find no structure and report an
// untagged document, which lets AutoTag overwrite an existing structure tree.
// That is exactly what alreadyTagged exists to prevent.
func TestAlreadyTaggedFollowsTheRecoveredCatalog(t *testing.T) {
	t.Parallel()

	doc := brokenRootDoc(t, "/StructTreeRoot 99 0 R /MarkInfo << /Marked true >>")
	tagged, err := alreadyTagged(context.Background(), doc)
	if err != nil {
		t.Fatalf("alreadyTagged: %v", err)
	}
	if !tagged {
		t.Error("a tagged catalog must be seen as tagged even when the trailer /Root names another object")
	}
}

// TestAutoTagWiresTheRecoveredCatalog pins that the structure tree is wired into
// the real catalog, and that the object the broken /Root names is left alone.
//
// When the trailer names the /Info dictionary, tagging merged /StructTreeRoot
// and /MarkInfo into the document information dictionary while the catalog
// kept neither: the repair was reported as applied and the same violations came
// back as residual, round after round.
//
// # Corpus evidence
//
//   - pdfjs issue9418.pdf: its trailer names the /Info dictionary, and tagging
//     wrote into /Info round after round while the catalog kept neither entry.
func TestAutoTagWiresTheRecoveredCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc := brokenRootDoc(t, "")
	alloc := writer.NewObjAllocator(6) // objects 1..6 exist

	if _, err := AutoTag(ctx, alloc, doc); err != nil {
		t.Fatalf("AutoTag: %v", err)
	}

	pending := alloc.Pending()
	catalog, ok := pending[2].(ast.PdfDict)
	if !ok {
		t.Fatalf("catalog (object 2) not written; pending objects: %v", pendingNums(pending))
	}
	if _, ok := catalog["StructTreeRoot"]; !ok {
		t.Errorf("catalog missing /StructTreeRoot: %v", catalog)
	}
	mi, ok := catalog["MarkInfo"].(ast.PdfDict)
	if !ok || mi["Marked"] != ast.PdfBool(true) {
		t.Errorf("catalog /MarkInfo: got %v, want << /Marked true >>", catalog["MarkInfo"])
	}

	if info, written := pending[1]; written {
		t.Errorf("the /Info dictionary named by the broken /Root was rewritten: %v", info)
	}
}

func pendingNums(pending map[int]ast.PdfObject) []int {
	nums := make([]int, 0, len(pending))
	for n := range pending {
		nums = append(nums, n)
	}
	return nums
}

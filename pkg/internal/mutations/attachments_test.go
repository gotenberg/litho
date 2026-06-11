package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// minimalPDF returns a handcrafted minimal 1-page PDF for tests.
func minimalPDF() []byte {
	return []byte(
		"%PDF-1.7\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n" +
			"0000000000 65535 f \n" +
			"0000000009 00000 n \n" +
			"0000000058 00000 n \n" +
			"0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\n" +
			"startxref\n198\n%%EOF\n")
}

func TestAddAttachmentWithOptions_AFRelationship(t *testing.T) {
	ctx := context.Background()
	data := minimalPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(3)

	err = AddAttachmentWithOptions(ctx, alloc, doc, "invoice.xml", []byte("<?xml version=\"1.0\"?><invoice/>"),
		AttachmentOptions{
			MimeType:       "application/xml",
			AFRelationship: "Alternative",
			AddToCatalogAF: true,
		})
	if err != nil {
		t.Fatalf("AddAttachmentWithOptions: %v", err)
	}

	// Verify the filespec dict has /AFRelationship.
	foundAFRel := false
	foundCatalogAF := false
	for _, obj := range alloc.Pending() {
		s := obj.String()
		if strings.Contains(s, "AFRelationship") && strings.Contains(s, "Alternative") {
			foundAFRel = true
		}
		if strings.Contains(s, "/AF") && strings.Contains(s, "/Catalog") {
			foundCatalogAF = true
		}
	}
	if !foundAFRel {
		t.Error("expected /AFRelationship /Alternative in pending allocations")
	}
	if !foundCatalogAF {
		t.Error("expected /AF array in catalog")
	}
}

// attachPDF builds a PDF from the given object bodies (object 1 is the
// catalog) with correct xref offsets.
func attachPDF(objs ...string) []byte {
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

func attachDoc(t *testing.T, objs ...string) *document.Document {
	t.Helper()
	data := attachPDF(objs...)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// pendingDict chases obj through the allocator's pending map until a
// dict is reached.
func pendingDict(t *testing.T, alloc *writer.ObjAllocator, obj ast.PdfObject) ast.PdfDict {
	t.Helper()
	if d, ok := obj.(ast.PdfDict); ok {
		return d
	}
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("expected dict or ref, got %T", obj)
	}
	d, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("pending object %d is not a dict", ref.ObjectNum)
	}
	return d
}

// efPairs returns the final EmbeddedFiles [name, ref, ...] array the
// allocator would write for the catalog.
func efPairs(t *testing.T, alloc *writer.ObjAllocator, catalogNum int) ast.PdfArray {
	t.Helper()
	catalog, ok := alloc.Pending()[catalogNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("catalog %d not pending", catalogNum)
	}
	names := pendingDict(t, alloc, catalog["Names"])
	efDict := pendingDict(t, alloc, names["EmbeddedFiles"])
	arr, ok := efDict["Names"].(ast.PdfArray)
	if !ok {
		t.Fatalf("EmbeddedFiles /Names is %T, want array", efDict["Names"])
	}
	return arr
}

func TestAddAttachment_SecondRunReplacesNotDuplicates(t *testing.T) {
	ctx := context.Background()
	doc := attachDoc(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	alloc := writer.NewObjAllocator(3)
	opts := AttachmentOptions{MimeType: "text/xml", AFRelationship: "Data", AddToCatalogAF: true}

	for run := range 2 {
		if err := AddAttachmentWithOptions(ctx, alloc, doc, "factur-x.xml", []byte("<x/>"), opts); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}

	arr := efPairs(t, alloc, 1)
	if len(arr) != 2 {
		t.Fatalf("EmbeddedFiles has %d items, want one [name, ref] pair: %v", len(arr), arr)
	}
	catalog := alloc.Pending()[1].(ast.PdfDict)
	af, ok := catalog["AF"].(ast.PdfArray)
	if !ok || len(af) != 1 {
		t.Fatalf("catalog /AF = %v, want exactly one entry", catalog["AF"])
	}
}

func TestAddAttachment_KeepsLexicalOrderAndKidsEntries(t *testing.T) {
	ctx := context.Background()
	// EmbeddedFiles tree with a /Kids intermediate node: the old code
	// dropped every existing entry when /Kids was present.
	doc := attachDoc(t,
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 4 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Kids [5 0 R] >>",
		"<< /Limits [(a.txt) (z.txt)] /Names [(a.txt) 6 0 R (z.txt) 7 0 R] >>",
		"<< /Type /Filespec /F (a.txt) >>",
		"<< /Type /Filespec /F (z.txt) >>",
	)
	alloc := writer.NewObjAllocator(7)

	err := AddAttachmentWithOptions(ctx, alloc, doc, "m.xml", []byte("<x/>"), AttachmentOptions{MimeType: "text/xml"})
	if err != nil {
		t.Fatalf("AddAttachmentWithOptions: %v", err)
	}

	arr := efPairs(t, alloc, 1)
	var got []string
	for i := 0; i+1 < len(arr); i += 2 {
		got = append(got, string(arr[i].(ast.PdfString)))
	}
	want := []string{"a.txt", "m.xml", "z.txt"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v (ISO 32000-2 §7.9.6 lexical order)", got, want)
		}
	}
}

func TestRemoveAttachment_KidsTreeAndAFPruned(t *testing.T) {
	ctx := context.Background()
	doc := attachDoc(t,
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 4 0 R >> /AF [6 0 R 7 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Kids [5 0 R] >>",
		"<< /Limits [(a.txt) (z.txt)] /Names [(a.txt) 6 0 R (z.txt) 7 0 R] >>",
		"<< /Type /Filespec /F (a.txt) >>",
		"<< /Type /Filespec /F (z.txt) >>",
	)
	alloc := writer.NewObjAllocator(7)

	if err := RemoveAttachment(ctx, alloc, doc, "a.txt"); err != nil {
		t.Fatalf("RemoveAttachment: %v", err)
	}

	arr := efPairs(t, alloc, 1)
	if len(arr) != 2 || string(arr[0].(ast.PdfString)) != "z.txt" {
		t.Fatalf("EmbeddedFiles = %v, want only z.txt", arr)
	}
	catalog := alloc.Pending()[1].(ast.PdfDict)
	af, ok := catalog["AF"].(ast.PdfArray)
	if !ok || len(af) != 1 {
		t.Fatalf("catalog /AF = %v, want only z.txt filespec", catalog["AF"])
	}
	if ref, ok := af[0].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 7 {
		t.Fatalf("catalog /AF[0] = %v, want 7 0 R", af[0])
	}
}

func TestAddAttachment_BackwardCompat(t *testing.T) {
	ctx := context.Background()
	data := minimalPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(3)

	err = AddAttachment(ctx, alloc, doc, "data.csv", []byte("a,b,c\n1,2,3"), "text/csv")
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	// Old API should NOT add /AF or /AFRelationship.
	for _, obj := range alloc.Pending() {
		s := obj.String()
		if strings.Contains(s, "AFRelationship") {
			t.Error("legacy AddAttachment should not set /AFRelationship")
		}
	}
}

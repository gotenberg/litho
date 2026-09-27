package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// cyclePDF builds a PDF from the given object bodies (object 1 is the
// catalog) with correct xref offsets. Used for crafted-cycle DoS fixtures.
func cyclePDF(objs ...string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

func cycleDoc(t *testing.T, objs ...string) *document.Document {
	t.Helper()
	data := cyclePDF(objs...)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// outlineCycleObjs is a document whose two outline items point at each other
// through /Next. Before the visited-set guard, walking this chain recursed
// forever and exhausted the stack, which Go cannot recover.
func outlineCycleObjs() []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Outlines /First 5 0 R >>",
		"<< /Title (A) /Parent 4 0 R /Next 6 0 R /A << /S /JavaScript /JS (x) >> >>",
		"<< /Title (B) /Parent 4 0 R /Next 5 0 R >>",
	}
}

func TestCheckOutlineActions_NextCycleTerminates(t *testing.T) {
	doc := cycleDoc(t, outlineCycleObjs()...)
	cat, err := document.ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}

	// Must terminate and still see item A's JavaScript action exactly once.
	violations := checkOutlineActions(context.Background(), doc, cat.Dict, "PDF/UA-1", 1, make(map[int]bool))
	if len(violations) == 0 {
		t.Error("expected the JavaScript action on the cyclic outline to be flagged")
	}
}

func TestWalkOutlineStructDest_NextCycleTerminates(t *testing.T) {
	doc := cycleDoc(t, outlineCycleObjs()...)
	cat, err := document.ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	outlines, err := document.ResolveDict(context.Background(), doc, cat.Dict["Outlines"])
	if err != nil {
		t.Fatalf("resolving outlines: %v", err)
	}

	var results []Violation
	walkOutlineStructDest(context.Background(), doc, outlines["First"], &results, 0, make(map[int]bool), make(map[int]bool))
}

func TestWalkNSCheck_KCycleTerminates(t *testing.T) {
	// Two structure elements referencing each other through /K. Before the
	// visited-set guard this recursed forever.
	doc := cycleDoc(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructElem /S /Document /K 5 0 R >>",
		"<< /Type /StructElem /S /P /K 4 0 R >>",
	)
	elem, err := document.ResolveDict(context.Background(), doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving element: %v", err)
	}

	var results []Violation
	walkNSCheck(context.Background(), doc, elem, "", nil, nil, &results, make(map[int]bool), 0)
}

// TestUA1Header pins the PDF/UA-1 header rule (PDFUA-1.xml 6.1 t1): the
// header must match %PDF-1.[0-7] exactly, so a PDF 2.0 header fails under
// UA-1 and passes under UA-2 (which has no header rule).
func TestUA1Header(t *testing.T) {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	data := bytes.Replace(cyclePDF(objs...), []byte("%PDF-1.7"), []byte("%PDF-2.0"), 1)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	errs, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA(UA-1): %v", err)
	}
	found := false
	for _, e := range errs {
		if v, ok := e.(Violation); ok && v.Rule == Rule_14289_1_6_1_t01 {
			found = true
		}
	}
	if !found {
		t.Error("PDF 2.0 header not flagged under PDF/UA-1")
	}

	errs2, err := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if err != nil {
		t.Fatalf("ValidatePDFUA(UA-2): %v", err)
	}
	for _, e := range errs2 {
		if v, ok := e.(Violation); ok && v.Rule == Rule_14289_1_6_1_t01 {
			t.Error("UA-1 header rule emitted under PDF/UA-2")
		}
	}
}

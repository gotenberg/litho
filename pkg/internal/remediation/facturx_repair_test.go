package remediation

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

// repairPDF builds a PDF whose factur-x.xml attachment has every
// container defect RepairHybridInvoice handles: no catalog /AF, no
// /AFRelationship, /F without /UF, and a wrong stream /Subtype.
func repairPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(factur-x.xml) 4 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Filespec /F (factur-x.xml) /EF << /F 5 0 R >> >>",
		"<< /Type /EmbeddedFile /Subtype /application#2Foctet-stream /Length 5 >>\nstream\n<x/>\n\nendstream",
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

func TestRepairHybridInvoice(t *testing.T) {
	ctx := context.Background()
	data := repairPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(5)

	fixes, err := RepairHybridInvoice(ctx, doc, alloc)
	if err != nil {
		t.Fatalf("RepairHybridInvoice: %v", err)
	}
	joined := strings.Join(fixes, "; ")
	for _, want := range []string{
		"set invoice /AFRelationship to Data",
		"mirrored invoice filespec /F and /UF",
		"set invoice stream /Subtype to text/xml",
		"added invoice filespec to catalog /AF",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing fix %q in %v", want, fixes)
		}
	}

	spec, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatal("filespec not queued")
	}
	if rel, _ := spec["AFRelationship"].(ast.PdfName); rel != "Data" {
		t.Errorf("AFRelationship = %v, want Data", spec["AFRelationship"])
	}
	if _, hasUF := spec["UF"]; !hasUF {
		t.Error("missing mirrored /UF")
	}
	stream, ok := alloc.Pending()[5].(ast.PdfStream)
	if !ok {
		t.Fatal("invoice stream not queued")
	}
	if st, _ := stream.Dict["Subtype"].(ast.PdfName); st != "text/xml" {
		t.Errorf("stream Subtype = %v, want text/xml", stream.Dict["Subtype"])
	}
	body, err := stream.Bytes(ctx)
	if err != nil || !bytes.Contains(body, []byte("<x/>")) {
		t.Errorf("invoice payload not preserved: %q err=%v", body, err)
	}
	catalog, ok := alloc.Pending()[1].(ast.PdfDict)
	if !ok {
		t.Fatal("catalog not queued")
	}
	af, ok := catalog["AF"].(ast.PdfArray)
	if !ok || len(af) != 1 {
		t.Fatalf("catalog /AF = %v, want one entry", catalog["AF"])
	}

	// Second run must be a no-op: the repair is idempotent.
	fixes2, err := RepairHybridInvoice(ctx, doc, alloc)
	if err != nil {
		t.Fatalf("second RepairHybridInvoice: %v", err)
	}
	if len(fixes2) != 0 {
		t.Errorf("second run must report no fixes, got %v", fixes2)
	}
}

func TestRepairHybridInvoice_NoInvoice(t *testing.T) {
	ctx := context.Background()
	data := cyclePDFRemediation(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(3)
	fixes, err := RepairHybridInvoice(ctx, doc, alloc)
	if err != nil {
		t.Fatalf("RepairHybridInvoice: %v", err)
	}
	if fixes != nil {
		t.Errorf("document without invoice attachment must be untouched, got %v", fixes)
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("no objects may be queued, got %d", len(alloc.Pending()))
	}
}

// cyclePDFRemediation builds a minimal invoice-free document.
func cyclePDFRemediation(t *testing.T) []byte {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
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

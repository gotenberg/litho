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

func signedFormPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /FT /Sig /T (Sig1) /V 5 0 R /Rect [0 0 0 0] /Subtype /Widget /Type /Annot >>",
		"<< /Type /Sig /Filter /Adobe.PPKLite /ByteRange [0 100 200 50] /Contents <00> >>",
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

// TestFixSignatureStrip pins the aggressive signature removal: the /V
// value disappears while the field survives, and preserve-content mode
// leaves the document untouched.
func TestFixSignatureStrip(t *testing.T) {
	ctx := context.Background()
	data := signedFormPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	conservative := writer.NewObjAllocator(5)
	fixes, err := fixSignatureStrip(ctx, conservative, doc, Options{PreserveContent: true}, nil, "PDF/A-2b")
	if err != nil || fixes != nil || len(conservative.Pending()) != 0 {
		t.Fatalf("preserve-content mode must be a no-op, got fixes=%v pending=%d err=%v", fixes, len(conservative.Pending()), err)
	}

	alloc := writer.NewObjAllocator(5)
	fixes, err = fixSignatureStrip(ctx, alloc, doc, Options{PreserveContent: false}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixSignatureStrip: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v, want one description", fixes)
	}
	field, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatal("signature field not rewritten")
	}
	if _, hasV := field["V"]; hasV {
		t.Error("/V must be removed")
	}
	if ft, _ := field["FT"].(ast.PdfName); ft != "Sig" {
		t.Error("field itself must survive")
	}
}
